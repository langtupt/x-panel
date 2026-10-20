// Package accounts is the business logic for hosting accounts and their
// packages: it keeps the database and the Linux side (via the agent) in step.
//
// Only "user" accounts are hosting accounts with a Linux user and disk quota;
// admins and resellers are management-only logins.
package accounts

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/cronjobs"
	"xpanel/apps/api/internal/databases"
	"xpanel/apps/api/internal/db"
	"xpanel/apps/api/internal/ftpaccounts"
	"xpanel/apps/api/internal/mailrepo"
	"xpanel/apps/api/internal/packages"
	"xpanel/apps/api/internal/secretbox"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

type Service struct {
	pool    *pgxpool.Pool
	users   *users.Repo
	pkgs    *packages.Repo
	sites   *sites.Repo
	dbs     *databases.Repo
	ftp     *ftpaccounts.Repo
	cron    *cronjobs.Repo
	tasks   *tasks.Queue // set by RegisterTasks
	agent   agentproto.Caller
	auth    *auth.Service
	secrets *secretbox.Box
	log     *slog.Logger
}

func NewService(pool *pgxpool.Pool, u *users.Repo, p *packages.Repo, st *sites.Repo, dbs *databases.Repo, ftp *ftpaccounts.Repo, cron *cronjobs.Repo, agent agentproto.Caller, a *auth.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, users: u, pkgs: p, sites: st, dbs: dbs, ftp: ftp, cron: cron, agent: agent, auth: a, log: log}
}

type CreateInput struct {
	Username  string     `json:"username"`
	Password  string     `json:"password"`
	Email     string     `json:"email"`
	Role      users.Role `json:"role"`
	PackageID *int64     `json:"package_id"`
}

func (s *Service) Create(ctx context.Context, actor *users.User, in CreateInput) (*users.User, error) {
	if !users.CanCreate(actor, in.Role) {
		return nil, apperr.Forbidden("không được tạo tài khoản loại này")
	}
	if err := users.ValidateNew(in.Username, in.Password, in.Email); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ur := s.users.WithTx(tx)

	pkg, err := s.resolvePackage(ctx, tx, actor, in.Role, in.PackageID)
	if err != nil {
		return nil, err
	}
	if actor.Role == users.RoleReseller {
		if err := s.checkResellerCapacity(ctx, tx, actor, 0, pkg); err != nil {
			return nil, err
		}
	}

	u := &users.User{Username: in.Username, Email: in.Email, PasswordHash: hash, Role: in.Role,
		ParentID: &actor.ID, PackageID: in.PackageID}
	if err := ur.Create(ctx, u); errors.Is(err, users.ErrTaken) {
		return nil, apperr.Conflict("%s", err.Error())
	} else if err != nil {
		return nil, err
	}

	committed := false
	if in.Role == users.RoleUser {
		var res agentproto.UserCreateResult
		if err := s.agent.Call(ctx, agentproto.ActionUserCreate, agentproto.UserParams{Username: u.Username}, &res); err != nil {
			return nil, apperr.Agent(err)
		}
		// Roll the Linux user back if anything below fails.
		defer func() {
			if !committed {
				if err := s.agent.Call(context.WithoutCancel(ctx), agentproto.ActionUserDelete,
					agentproto.UserParams{Username: u.Username}, nil); err != nil {
					s.log.Error("rollback linux user failed", "user", u.Username, "err", err)
				}
			}
		}()
		if err := s.applyQuota(ctx, tx, u.Username, pkg); err != nil {
			return nil, err
		}
		if err := s.agent.Call(ctx, agentproto.ActionUserPassword,
			agentproto.UserPasswordParams{Username: u.Username, Password: in.Password}, nil); err != nil {
			return nil, apperr.Agent(err)
		}
		if err := ur.SetLinuxUID(ctx, u.ID, res.UID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return s.users.ByID(ctx, u.ID)
}

// managed loads target and checks actor may manage it; hides foreign accounts as 404.
func (s *Service) managed(ctx context.Context, r *users.Repo, actor *users.User, id int64) (*users.User, error) {
	t, err := r.ByID(ctx, id)
	if errors.Is(err, users.ErrNotFound) || (err == nil && !users.CanManage(actor, t)) {
		return nil, apperr.NotFound("không tìm thấy tài khoản")
	}
	return t, err
}

func (s *Service) SetStatus(ctx context.Context, actor *users.User, id int64, status users.Status) (*users.User, error) {
	if status != users.StatusActive && status != users.StatusSuspended {
		return nil, apperr.Invalid("status phải là active hoặc suspended")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ur := s.users.WithTx(tx)
	t, err := s.managed(ctx, ur, actor, id)
	if err != nil {
		return nil, err
	}
	if err := ur.SetStatus(ctx, id, status); err != nil {
		return nil, err
	}
	if t.LinuxUID != nil {
		p := agentproto.UserLockParams{Username: t.Username, Locked: status == users.StatusSuspended}
		if err := s.agent.Call(ctx, agentproto.ActionUserLock, p, nil); err != nil {
			return nil, apperr.Agent(err)
		}
		// Suspended accounts serve a "suspended" page and lose their PHP pools.
		t.Status = status
		if _, err := s.syncWeb(ctx, tx, t); err != nil {
			return nil, err
		}
		if err := s.lockDBUsers(ctx, tx, t, status == users.StatusSuspended); err != nil {
			return nil, err
		}
		if err := s.lockFTP(ctx, tx, t, status == users.StatusSuspended); err != nil {
			return nil, err
		}
		if err := s.mailAccountChanged(ctx, tx, t, nil); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if status == users.StatusSuspended {
		if err := s.auth.RevokeAll(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.users.ByID(ctx, id)
}

// SetPackage changes an account's package. Only the direct parent may do it,
// since packages belong to whoever created the account.
func (s *Service) SetPackage(ctx context.Context, actor *users.User, id int64, pkgID *int64) (*users.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ur := s.users.WithTx(tx)
	t, err := s.managed(ctx, ur, actor, id)
	if err != nil {
		return nil, err
	}
	if t.ParentID == nil || *t.ParentID != actor.ID {
		return nil, apperr.Forbidden("chỉ tài khoản cha mới đổi được gói")
	}
	pkg, err := s.resolvePackage(ctx, tx, actor, t.Role, pkgID)
	if err != nil {
		return nil, err
	}
	if pkg != nil && pkg.DiskMB > 0 {
		if err := checkMailFits(ctx, tx, t.Username, pkg); err != nil {
			return nil, err
		}
	}
	if actor.Role == users.RoleReseller {
		if err := s.checkResellerCapacity(ctx, tx, actor, t.ID, pkg); err != nil {
			return nil, err
		}
	}
	if err := ur.SetPackage(ctx, id, pkgID); err != nil {
		return nil, err
	}
	if t.LinuxUID != nil {
		if err := s.applyQuota(ctx, tx, t.Username, pkg); err != nil {
			return nil, err
		}
		// New CPU/RAM limits for the account's PHP-FPM slice.
		t.PackageID = pkgID
		if _, err := s.syncWeb(ctx, tx, t); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.users.ByID(ctx, id)
}

func (s *Service) ResetPassword(ctx context.Context, actor *users.User, id int64, password string) error {
	t, err := s.managed(ctx, s.users, actor, id)
	if err != nil {
		return err
	}
	return s.setPassword(ctx, t, password)
}

// SetOwnPassword changes the caller's password (the handler verified the current one).
func (s *Service) SetOwnPassword(ctx context.Context, me *users.User, password string) error {
	return s.setPassword(ctx, me, password)
}

// setPassword updates the panel password (and SFTP password of hosting
// accounts) and ends every session of the account.
func (s *Service) setPassword(ctx context.Context, t *users.User, password string) error {
	if len(password) < users.MinPasswordLen {
		return apperr.Invalid("mật khẩu tối thiểu %d ký tự", users.MinPasswordLen)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	// Hosting accounts log in to SFTP with the same password.
	if t.LinuxUID != nil {
		if err := s.agent.Call(ctx, agentproto.ActionUserPassword,
			agentproto.UserPasswordParams{Username: t.Username, Password: password}, nil); err != nil {
			return apperr.Agent(err)
		}
	}
	if err := s.users.SetPassword(ctx, t.ID, hash); err != nil {
		return err
	}
	return s.auth.RevokeAll(ctx, t.ID)
}

// Delete removes an account and, for hosting accounts, its Linux user and home directory.
func (s *Service) Delete(ctx context.Context, actor *users.User, id int64) (*users.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ur := s.users.WithTx(tx)
	t, err := s.managed(ctx, ur, actor, id)
	if err != nil {
		return nil, err
	}
	if n, err := ur.CountChildren(ctx, id); err != nil {
		return nil, err
	} else if n > 0 {
		return nil, apperr.Conflict("đại lý còn %d tài khoản, hãy xóa hoặc chuyển chúng trước", n)
	}
	// Databases must be dropped while their rows still exist (they cascade with the account).
	if err := s.dropAllDatabases(ctx, tx, t); err != nil {
		return nil, err
	}
	mailDomains, err := mailrepo.NewRepo(tx).DomainsByUser(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if err := s.deleteAllFTP(ctx, tx, t); err != nil {
		return nil, err
	}
	if err := ur.Delete(ctx, id); err != nil {
		return nil, err
	}
	if len(mailDomains) > 0 {
		if err := s.mailAccountChanged(ctx, tx, t, mailDomains); err != nil {
			return nil, err
		}
	}
	if t.LinuxUID != nil {
		if err := s.agent.Call(ctx, agentproto.ActionCronSync, agentproto.CronSyncParams{Username: t.Username}, nil); err != nil {
			return nil, apperr.Agent(err)
		}
		// Sites were cascade-deleted above, so this removes every vhost and pool of the user.
		if _, err := s.syncWeb(ctx, tx, t); err != nil {
			return nil, err
		}
		if err := s.agent.Call(ctx, agentproto.ActionUserDelete, agentproto.UserParams{Username: t.Username}, nil); err != nil {
			return nil, apperr.Agent(err)
		}
	}
	return t, tx.Commit(ctx)
}

// resolvePackage checks that pkgID belongs to actor and fits the account role.
// Resellers must always pick one of their packages; admins may leave it empty (unlimited).
func (s *Service) resolvePackage(ctx context.Context, tx pgx.Tx, actor *users.User, role users.Role, pkgID *int64) (*packages.Package, error) {
	if pkgID == nil {
		if actor.Role == users.RoleReseller {
			return nil, apperr.Invalid("phải chọn gói dịch vụ")
		}
		return nil, nil
	}
	p, err := s.pkgs.WithTx(tx).ByID(ctx, *pkgID)
	if errors.Is(err, packages.ErrNotFound) || (err == nil && p.OwnerID != actor.ID) {
		return nil, apperr.Invalid("gói dịch vụ không tồn tại")
	}
	if err != nil {
		return nil, err
	}
	if p.Kind != role {
		return nil, apperr.Invalid("gói %q dành cho loại tài khoản khác", p.Name)
	}
	return p, nil
}

// checkResellerCapacity locks the reseller row and verifies one more account
// on pkg (replacing excludeUserID's current allocation, if any) fits.
func (s *Service) checkResellerCapacity(ctx context.Context, tx pgx.Tx, reseller *users.User, excludeUserID int64, pkg *packages.Package) error {
	if err := s.users.WithTx(tx).LockForUpdate(ctx, reseller.ID); err != nil {
		return err
	}
	if reseller.PackageID == nil {
		return nil
	}
	pr := s.pkgs.WithTx(tx)
	rp, err := pr.ByID(ctx, *reseller.PackageID)
	if err != nil {
		return err
	}
	if err := packages.WithinParent(pkg.Limits, rp.Limits); err != nil {
		return apperr.Conflict("%s", err.Error())
	}
	n, disk, err := pr.Allocation(ctx, reseller.ID, excludeUserID, 0)
	if err != nil {
		return err
	}
	if err := packages.CheckCapacity(rp.Limits, rp.Oversell, n, disk, 1, pkg.DiskMB); err != nil {
		return apperr.Conflict("%s", err.Error())
	}
	return nil
}

func (s *Service) applyQuota(ctx context.Context, q db.Querier, username string, pkg *packages.Package) error {
	p := agentproto.QuotaSetParams{Username: username}
	if pkg != nil {
		mailMB, err := mailAllocMB(ctx, q, username)
		if err != nil {
			return err
		}
		p.DiskMB, p.Inodes = filesQuotaMB(pkg.DiskMB, mailMB), pkg.Inodes
	}
	var res agentproto.QuotaSetResult
	if err := s.agent.Call(ctx, agentproto.ActionQuotaSet, p, &res); err != nil {
		return apperr.Agent(err)
	}
	if !res.Applied && (p.DiskMB > 0 || p.Inodes > 0) {
		s.log.Warn("disk quota not enforced: quotas are off on /home filesystem", "user", username)
	}
	return nil
}

// AttachUsage fills DiskUsedMB from the agent's quota report. It is best
// effort: if the agent is unreachable the list is returned without usage.
func (s *Service) AttachUsage(ctx context.Context, list []users.User) (quotaEnabled bool) {
	var rep agentproto.QuotaReport
	if err := s.agent.Call(ctx, agentproto.ActionQuotaReport, nil, &rep); err != nil {
		s.log.Warn("quota report unavailable", "err", err)
		return false
	}
	for i := range list {
		if u, ok := rep.Users[list[i].Username]; ok && list[i].LinuxUID != nil {
			used := u.DiskUsedMB
			list[i].DiskUsedMB = &used
		}
	}
	return rep.Enabled
}
