package accounts

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/ftpaccounts"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

type FTPInput struct {
	UserID   *int64 `json:"user_id"` // owner; managers only
	Name     string `json:"name"`    // suffix; the login is "<owner>_<name>"
	Dir      string `json:"dir"`     // relative to the owner's home, "" = whole home
	Password string `json:"password"`
}

func ftpParams(owner string, a *ftpaccounts.Account) agentproto.FTPParams {
	return agentproto.FTPParams{Owner: owner, Login: a.Login, Dir: a.Dir}
}

func (s *Service) accessibleFTP(ctx context.Context, actor *users.User, id int64) (*ftpaccounts.Account, *users.User, error) {
	a, err := s.ftp.ByID(ctx, id)
	if errors.Is(err, ftpaccounts.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy tài khoản FTP")
	}
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.users.ByID(ctx, a.UserID)
	if err != nil {
		return nil, nil, err
	}
	if owner.ID != actor.ID && !users.CanManage(actor, owner) {
		return nil, nil, apperr.NotFound("không tìm thấy tài khoản FTP")
	}
	return a, owner, nil
}

func (s *Service) ListFTP(ctx context.Context, actor *users.User) ([]ftpaccounts.Account, error) {
	return s.ftp.Visible(ctx, actor.ID, string(actor.Role))
}

func (s *Service) CreateFTP(ctx context.Context, actor *users.User, in FTPInput) (*ftpaccounts.Account, error) {
	owner, err := s.siteOwner(ctx, actor, in.UserID)
	if err != nil {
		return nil, err
	}
	login := owner.Username + "_" + in.Name
	if err := validate.FTPLogin(owner.Username, login); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if err := validate.DocRoot(in.Dir); err != nil {
		return nil, apperr.Invalid("thư mục FTP không hợp lệ (đường dẫn tương đối trong thư mục home, ví dụ www/example.com)")
	}
	if err := validate.DBPassword(in.Password); err != nil {
		return nil, apperr.Invalid("mật khẩu FTP: 10–64 ký tự, gồm chữ, số và !@#$%%^&*()_+=.,:;?~-")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	fr := s.ftp.WithTx(tx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if owner.PackageID != nil {
		p, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		n, err := fr.CountByUser(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		if p.MaxFTP > 0 && n >= p.MaxFTP {
			return nil, apperr.Conflict("gói %q chỉ cho phép %d tài khoản FTP", p.Name, p.MaxFTP)
		}
	}
	a := &ftpaccounts.Account{UserID: owner.ID, Username: owner.Username, Login: login, Dir: in.Dir}
	if err := fr.Create(ctx, a); errors.Is(err, ftpaccounts.ErrTaken) {
		return nil, apperr.Conflict("%s", err.Error())
	} else if err != nil {
		return nil, err
	}
	p := ftpParams(owner.Username, a)
	p.Password = in.Password
	if err := s.agent.Call(ctx, agentproto.ActionFTPCreate, p, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.agent.Call(context.WithoutCancel(ctx), agentproto.ActionFTPDelete, ftpParams(owner.Username, a), nil)
		}
	}()
	if owner.Status == users.StatusSuspended {
		if err := s.lockFTP(ctx, tx, owner, true); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return a, nil
}

func (s *Service) SetFTPPassword(ctx context.Context, actor *users.User, id int64, password string) (*ftpaccounts.Account, error) {
	a, owner, err := s.accessibleFTP(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if err := validate.DBPassword(password); err != nil {
		return nil, apperr.Invalid("mật khẩu FTP: 10–64 ký tự, gồm chữ, số và !@#$%%^&*()_+=.,:;?~-")
	}
	p := ftpParams(owner.Username, a)
	p.Password = password
	if err := s.agent.Call(ctx, agentproto.ActionFTPPassword, p, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return a, nil
}

func (s *Service) DeleteFTP(ctx context.Context, actor *users.User, id int64) (*ftpaccounts.Account, error) {
	a, owner, err := s.accessibleFTP(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.ftp.WithTx(tx).Delete(ctx, id); err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionFTPDelete, ftpParams(owner.Username, a), nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return a, tx.Commit(ctx)
}

func (s *Service) lockFTP(ctx context.Context, tx pgx.Tx, owner *users.User, locked bool) error {
	list, err := s.ftp.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil || len(list) == 0 {
		return err
	}
	p := agentproto.FTPLockParams{Owner: owner.Username, Locked: locked}
	for _, a := range list {
		p.Logins = append(p.Logins, a.Login)
	}
	if err := s.agent.Call(ctx, agentproto.ActionFTPLock, p, nil); err != nil {
		return apperr.Agent(err)
	}
	return nil
}

func (s *Service) deleteAllFTP(ctx context.Context, tx pgx.Tx, owner *users.User) error {
	list, err := s.ftp.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil {
		return err
	}
	for i := range list {
		if err := s.agent.Call(ctx, agentproto.ActionFTPDelete, ftpParams(owner.Username, &list[i]), nil); err != nil {
			return apperr.Agent(err)
		}
	}
	return nil
}
