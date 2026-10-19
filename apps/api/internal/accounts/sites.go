package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	TaskSSLIssue   = "ssl.issue"
	TaskPHPInstall = "php.install"
	maxAliases     = 20
)

type SiteInput struct {
	AppType    string            `json:"app_type"`
	Runtime    string            `json:"runtime"`
	Env        map[string]string `json:"env"`
	Sample     bool              `json:"sample"`
	UserID     *int64            `json:"user_id"` // owner; managers only, users always own the site
	Domain     string            `json:"domain"`
	Aliases    []string          `json:"aliases"`
	PHPVersion string            `json:"php_version"`
	DocRoot    string            `json:"doc_root"`
}

// SiteResult is a site plus non-fatal notes from the agent (e.g. missing certificate).
type SiteResult struct {
	*sites.Site
	Warnings []string    `json:"warnings,omitempty"`
	Task     *tasks.Task `json:"task,omitempty"`
}

// RegisterTasks attaches the long-running task handlers to the queue.
func (s *Service) RegisterTasks(q *tasks.Queue) {
	s.tasks = q
	q.Handle(TaskAppInstall, s.runAppInstall)
	q.Handle(TaskAppRemove, s.runAppRemove)
	q.Handle(TaskSiteBuild, s.runSiteBuild)
	q.Handle(TaskSSLIssue, s.runSSLIssue)
	q.Handle(TaskPanelSSL, s.runPanelSSL)
	q.Handle(TaskPHPInstall, s.runPHPInstall)
	q.Handle(TaskPMAInstall, s.runPMAInstall)
}

// syncWeb pushes the account's complete site list to the agent. Run it
// inside the transaction that changed the sites so a failure rolls back.
func (s *Service) syncWeb(ctx context.Context, tx pgx.Tx, owner *users.User) ([]string, error) {
	list, err := s.sites.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	p := agentproto.WebSyncParams{Username: owner.Username, Suspended: owner.Status == users.StatusSuspended,
		Sites: []agentproto.WebSite{}}
	if owner.PackageID != nil {
		pkg, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		p.CPUPct, p.MemMB = pkg.CPUPct, pkg.MemMB
	}
	for _, st := range list {
		p.Sites = append(p.Sites, agentproto.WebSite{Domain: st.Domain, Aliases: st.Aliases,
			AppType: st.AppType, Runtime: st.Runtime, Port: st.Port, Env: st.Env, PHPVersion: st.PHPVersion, DocRoot: st.DocRoot, SSL: st.SSLStatus == sites.SSLActive})
	}
	var res agentproto.WebSyncResult
	if err := s.agent.Call(ctx, agentproto.ActionWebSync, p, &res); err != nil {
		return nil, apperr.Agent(err)
	}
	return res.Warnings, nil
}

// siteOwner resolves who will own a new site and checks the actor may act for them.
func (s *Service) siteOwner(ctx context.Context, actor *users.User, userID *int64) (*users.User, error) {
	if actor.Role == users.RoleUser {
		// A hosting account acts only for itself; naming another account is refused.
		if userID != nil && *userID != actor.ID {
			return nil, apperr.NotFound("không tìm thấy tài khoản")
		}
		return actor, nil
	}
	if userID == nil {
		return nil, apperr.Invalid("phải chọn tài khoản sở hữu")
	}
	owner, err := s.managed(ctx, s.users, actor, *userID)
	if err != nil {
		return nil, err
	}
	if owner.Role != users.RoleUser || owner.LinuxUID == nil {
		return nil, apperr.Invalid("chỉ tài khoản người dùng (hosting) mới có website và database")
	}
	return owner, nil
}

// accessibleSite loads a site the actor may manage (its own or a managed account's).
func (s *Service) accessibleSite(ctx context.Context, r *sites.Repo, actor *users.User, id int64) (*sites.Site, *users.User, error) {
	site, err := r.ByID(ctx, id)
	if errors.Is(err, sites.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy website")
	}
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.users.ByID(ctx, site.UserID)
	if err != nil {
		return nil, nil, err
	}
	if owner.ID != actor.ID && !users.CanManage(actor, owner) {
		return nil, nil, apperr.NotFound("không tìm thấy website")
	}
	return site, owner, nil
}

func normalizeNames(domain string, aliases []string) (string, []string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if err := validate.Domain(domain); err != nil {
		return "", nil, apperr.Invalid("%s", err.Error())
	}
	out := []string{}
	for _, a := range aliases {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || a == domain || slices.Contains(out, a) {
			continue
		}
		if err := validate.Domain(a); err != nil {
			return "", nil, apperr.Invalid("%s", err.Error())
		}
		out = append(out, a)
	}
	if len(out) > maxAliases {
		return "", nil, apperr.Invalid("tối đa %d tên miền phụ", maxAliases)
	}
	return domain, out, nil
}

// checkPHP verifies the version is installed and allowed by the owner's package.
func (s *Service) checkPHP(ctx context.Context, owner *users.User, version string) error {
	if version == "" {
		return nil
	}
	var list agentproto.PHPList
	if err := s.agent.Call(ctx, agentproto.ActionPHPList, nil, &list); err != nil {
		return apperr.Agent(err)
	}
	if !slices.Contains(list.Installed, version) {
		return apperr.Invalid("PHP %s chưa được cài trên máy chủ", version)
	}
	if owner.PackageID != nil {
		p, err := s.pkgs.ByID(ctx, *owner.PackageID)
		if err != nil {
			return err
		}
		if len(p.PHPVersions) > 0 && !slices.Contains(p.PHPVersions, version) {
			return apperr.Invalid("gói %q không cho phép PHP %s", p.Name, version)
		}
	}
	return nil
}

func (s *Service) ListSites(ctx context.Context, actor *users.User) ([]sites.Site, error) {
	return s.sites.Visible(ctx, actor.ID, string(actor.Role))
}

func (s *Service) CreateSite(ctx context.Context, actor *users.User, in SiteInput) (*SiteResult, error) {
	owner, err := s.siteOwner(ctx, actor, in.UserID)
	if err != nil {
		return nil, err
	}
	domain, aliases, err := normalizeNames(in.Domain, in.Aliases)
	if err != nil {
		return nil, err
	}
	if err := validate.DocRoot(in.DocRoot); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if err := normalizeAppInput(&in); err != nil {
		return nil, err
	}
	if err := s.checkPHP(ctx, owner, in.PHPVersion); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	sr := s.sites.WithTx(tx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if owner.PackageID != nil {
		p, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		n, err := sr.CountByUser(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		if p.MaxSites > 0 && n >= p.MaxSites {
			return nil, apperr.Conflict("gói %q chỉ cho phép %d website", p.Name, p.MaxSites)
		}
	}
	if err := s.checkNotPanelDomain(ctx, append([]string{domain}, aliases...)); err != nil {
		return nil, err
	}
	if taken, err := sr.NameTaken(ctx, append([]string{domain}, aliases...), 0); err != nil {
		return nil, err
	} else if taken != "" {
		return nil, apperr.Conflict("tên miền %s đã được dùng cho website khác", taken)
	}

	site := &sites.Site{UserID: owner.ID, Username: owner.Username, Domain: domain, Aliases: aliases,
		PHPVersion: in.PHPVersion, DocRoot: in.DocRoot, AppType: in.AppType, Runtime: in.Runtime, Env: in.Env}
	if err := allocateAppPort(ctx, tx, site); err != nil {
		return nil, err
	}
	if err := sr.Create(ctx, site); err != nil {
		return nil, err
	}
	warnings, err := s.syncWeb(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	res := &SiteResult{Site: site, Warnings: warnings}
	if in.Sample {
		res.Task, err = s.tasks.Enqueue(ctx, tx, &actor.ID, TaskSiteBuild, site.Domain, siteBuildPayload{SiteID: site.ID, Sample: true})
		if err != nil {
			return nil, err
		}
	}
	return res, tx.Commit(ctx)
}

func (s *Service) UpdateSite(ctx context.Context, actor *users.User, id int64, in SiteInput) (*SiteResult, error) {
	cur, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	_, aliases, err := normalizeNames(cur.Domain, in.Aliases)
	if err != nil {
		return nil, err
	}
	if err := validate.DocRoot(in.DocRoot); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if in.AppType == "" {
		in.AppType = cur.AppType
	}
	if err := normalizeAppInput(&in); err != nil {
		return nil, err
	}
	if in.PHPVersion != cur.PHPVersion {
		if err := normalizeAppInput(&in); err != nil {
			return nil, err
		}
		if err := s.checkPHP(ctx, owner, in.PHPVersion); err != nil {
			return nil, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	sr := s.sites.WithTx(tx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if err := s.checkNotPanelDomain(ctx, aliases); err != nil {
		return nil, err
	}
	if taken, err := sr.NameTaken(ctx, aliases, id); err != nil {
		return nil, err
	} else if taken != "" {
		return nil, apperr.Conflict("tên miền %s đã được dùng cho website khác", taken)
	}
	aliasesChanged := !slices.Equal(cur.Aliases, aliases)
	cur.Aliases, cur.PHPVersion, cur.DocRoot = aliases, in.PHPVersion, in.DocRoot
	cur.AppType, cur.Runtime, cur.Env = in.AppType, in.Runtime, in.Env
	if err := allocateAppPort(ctx, tx, cur); err != nil {
		return nil, err
	}
	if err := sr.Update(ctx, cur); err != nil {
		return nil, err
	}
	warnings, err := s.syncWeb(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	res := &SiteResult{Site: cur, Warnings: warnings}
	// The certificate must cover the new names: re-issue it.
	if aliasesChanged && cur.SSLStatus == sites.SSLActive {
		if res.Task, err = s.tasks.Enqueue(ctx, tx, &actor.ID, TaskSSLIssue, cur.Domain, sslPayload{SiteID: id}); err != nil {
			return nil, err
		}
	}
	return res, tx.Commit(ctx)
}

func (s *Service) DeleteSite(ctx context.Context, actor *users.User, id int64) (*sites.Site, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if err := s.sites.WithTx(tx).Delete(ctx, id); err != nil {
		return nil, err
	}
	if _, err := s.syncWeb(ctx, tx, owner); err != nil {
		return nil, err
	}
	return site, tx.Commit(ctx)
}

type sslPayload struct {
	SiteID int64 `json:"site_id"`
}

// IssueSSL queues a Let's Encrypt certificate request for the site.
func (s *Service) IssueSSL(ctx context.Context, actor *users.User, id int64) (*tasks.Task, error) {
	site, _, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	if open, err := s.tasks.HasOpen(ctx, TaskSSLIssue, site.Domain); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang cấp SSL cho %s", site.Domain)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if site.SSLStatus != sites.SSLActive {
		if err := s.sites.WithTx(tx).SetSSL(ctx, id, sites.SSLPending, nil); err != nil {
			return nil, err
		}
	}
	t, err := s.tasks.Enqueue(ctx, tx, &actor.ID, TaskSSLIssue, site.Domain, sslPayload{SiteID: id})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

// DisableSSL switches the site back to plain HTTP (the certificate files are kept).
func (s *Service) DisableSSL(ctx context.Context, actor *users.User, id int64) (*SiteResult, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.sites.WithTx(tx).SetSSL(ctx, id, sites.SSLNone, nil); err != nil {
		return nil, err
	}
	warnings, err := s.syncWeb(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	site.SSLStatus, site.SSLExpiresAt = sites.SSLNone, nil
	res := &SiteResult{Site: site, Warnings: warnings}
	return res, tx.Commit(ctx)
}

func (s *Service) runSSLIssue(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p sslPayload
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	site, err := s.sites.ByID(ctx, p.SiteID)
	if err != nil {
		return err
	}
	owner, err := s.users.ByID(ctx, site.UserID)
	if err != nil {
		return err
	}
	names := append([]string{site.Domain}, site.Aliases...)
	logf("Yêu cầu chứng chỉ Let's Encrypt cho: %s", strings.Join(names, ", "))
	logf("Các tên miền phải trỏ (A/AAAA) về máy chủ này và mở cổng 80.")

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var res agentproto.SSLIssueResult
	err = s.agent.Call(callCtx, agentproto.ActionSSLIssue,
		agentproto.SSLIssueParams{Domain: site.Domain, Names: names, Email: owner.Email}, &res)
	if err != nil {
		if site.SSLStatus != sites.SSLActive { // keep a working certificate on a failed renewal
			_ = s.sites.SetSSL(context.WithoutCancel(ctx), site.ID, sites.SSLFailed, nil)
		}
		return err
	}
	logf("Đã nhận chứng chỉ, hết hạn %s", res.ExpiresAt.Format("02/01/2006"))

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := s.sites.WithTx(tx).SetCertificate(ctx, site.ID, "letsencrypt", "Let's Encrypt", res.ExpiresAt); err != nil {
		return err
	}
	if _, err := s.syncWeb(ctx, tx, owner); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	logf("Đã bật HTTPS cho %s", site.Domain)
	return nil
}

func (s *Service) ListPHP(ctx context.Context) (*agentproto.PHPList, error) {
	var list agentproto.PHPList
	if err := s.agent.Call(ctx, agentproto.ActionPHPList, nil, &list); err != nil {
		return nil, apperr.Agent(err)
	}
	return &list, nil
}

type phpPayload struct {
	Version string `json:"version"`
}

func (s *Service) InstallPHP(ctx context.Context, actor *users.User, version string) (*tasks.Task, error) {
	return s.InstallApp(ctx, actor, agentproto.AppInstallParams{ID: "php", Version: version})
}

func (s *Service) runPHPInstall(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p phpPayload
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	logf("Bắt đầu cài PHP %s (có thể mất vài phút)…", p.Version)
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var res agentproto.TaskLog
	if err := s.agent.Call(callCtx, agentproto.ActionPHPInstall, agentproto.PHPInstallParams{Version: p.Version}, &res); err != nil {
		return err
	}
	logf("%s", strings.TrimSpace(res.Log))
	logf("Đã cài xong PHP %s", p.Version)
	return nil
}

// RunRenewals re-issues certificates that expire within 30 days, twice a day.
func (s *Service) RunRenewals(ctx context.Context) {
	tick := time.NewTicker(12 * time.Hour)
	defer tick.Stop()
	for {
		if err := s.renewDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("ssl renewal scan", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Service) renewDue(ctx context.Context) error {
	if err := s.renewPanel(ctx); err != nil {
		s.log.Warn("panel certificate renewal check", "err", err)
	}
	due, err := s.sites.ExpiringSSL(ctx, time.Now().Add(30*24*time.Hour))
	if err != nil {
		return err
	}
	for _, site := range due {
		open, err := s.tasks.HasOpen(ctx, TaskSSLIssue, site.Domain)
		if err != nil {
			return err
		}
		if open {
			continue
		}
		if _, err := s.tasks.Enqueue(ctx, s.pool, nil, TaskSSLIssue, site.Domain, sslPayload{SiteID: site.ID}); err != nil {
			return fmt.Errorf("enqueue renewal for %s: %w", site.Domain, err)
		}
		s.log.Info("ssl renewal queued", "domain", site.Domain)
	}
	return nil
}
