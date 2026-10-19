package accounts

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/certs"
	"xpanel/apps/api/internal/settings"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const TaskPanelSSL = "panel.ssl"

// installCertificate stores a certificate on the server and switches the site to HTTPS.
func (s *Service) installCertificate(ctx context.Context, site *sites.Site, owner *users.User, typ, issuer, certPEM, keyPEM string, expires time.Time) (*SiteResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionSSLInstall,
		agentproto.SSLInstallParams{Domain: site.Domain, Certificate: certPEM, PrivateKey: keyPEM}, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	if err := s.sites.WithTx(tx).SetCertificate(ctx, site.ID, typ, issuer, expires); err != nil {
		return nil, err
	}
	warnings, err := s.syncWeb(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	updated, err := s.sites.ByID(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	return &SiteResult{Site: updated, Warnings: warnings}, nil
}

// UploadSSL installs a certificate bought elsewhere (PEM chain + private key).
func (s *Service) UploadSSL(ctx context.Context, actor *users.User, id int64, certPEM, keyPEM string) (*SiteResult, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	certOut, keyOut, info, err := certs.Validate(certPEM, keyPEM, site.Domain, site.Aliases, time.Now())
	if err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	res, err := s.installCertificate(ctx, site, owner, "custom", info.Issuer, certOut, keyOut, info.NotAfter)
	if err != nil {
		return nil, err
	}
	if len(info.Uncovered) > 0 {
		res.Warnings = append(res.Warnings, "chứng chỉ không bao gồm: "+strings.Join(info.Uncovered, ", ")+" (trình duyệt sẽ cảnh báo trên các tên miền này)")
	}
	return res, nil
}

// SelfSignedSSL creates a one-year self-signed certificate (for testing or
// internal sites; browsers will warn).
func (s *Service) SelfSignedSSL(ctx context.Context, actor *users.User, id int64) (*SiteResult, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, expires, err := certs.SelfSigned(append([]string{site.Domain}, site.Aliases...), time.Now())
	if err != nil {
		return nil, err
	}
	return s.installCertificate(ctx, site, owner, "selfsigned", "Tự ký (X-Panel)", certPEM, keyPEM, expires)
}

// PanelSSL describes the panel certificate and the configured hostname.
type PanelSSL struct {
	Domain string                  `json:"domain"`
	Info   agentproto.PanelSSLInfo `json:"info"`
}

func (s *Service) PanelSSL(ctx context.Context) (*PanelSSL, error) {
	var out PanelSSL
	var err error
	if out.Domain, err = settings.NewRepo(s.pool).Get(ctx, settings.PanelDomain); err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionPanelSSLInfo, nil, &out.Info); err != nil {
		return nil, apperr.Agent(err)
	}
	return &out, nil
}

// IssuePanelSSL queues a Let's Encrypt certificate for the panel hostname.
func (s *Service) IssuePanelSSL(ctx context.Context, actor *users.User, domain string) (*tasks.Task, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if err := validate.Domain(domain); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if taken, err := s.sites.NameTaken(ctx, []string{domain}, 0); err != nil {
		return nil, err
	} else if taken != "" {
		return nil, apperr.Conflict("tên miền %s đang được một website sử dụng", domain)
	}
	if open, err := s.tasks.HasOpen(ctx, TaskPanelSSL, domain); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang cấp SSL cho %s", domain)
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, TaskPanelSSL, domain, agentproto.PanelSSLParams{Domain: domain, Email: actor.Email})
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

func (s *Service) runPanelSSL(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p agentproto.PanelSSLParams
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	logf("Yêu cầu chứng chỉ Let's Encrypt cho trang quản trị: %s", p.Domain)
	logf("Tên miền phải trỏ (A/AAAA) về máy chủ này và mở cổng 80.")
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var res agentproto.SSLIssueResult
	if err := s.agent.Call(callCtx, agentproto.ActionPanelSSLIssue, p, &res); err != nil {
		return err
	}
	if err := settings.NewRepo(s.pool).Set(ctx, settings.PanelDomain, p.Domain); err != nil {
		return err
	}
	logf("Đã cài chứng chỉ cho trang quản trị và FTPS, hết hạn %s", res.ExpiresAt.Format("02/01/2006"))
	logf("Mở panel tại https://%s:8888", p.Domain)
	return nil
}

// renewPanel queues renewal of the panel certificate within 30 days of expiry.
func (s *Service) renewPanel(ctx context.Context) error {
	st, err := s.PanelSSL(ctx)
	if err != nil || st.Domain == "" || st.Info.SelfSigned || time.Until(st.Info.NotAfter) > 30*24*time.Hour {
		return err
	}
	if open, err := s.tasks.HasOpen(ctx, TaskPanelSSL, st.Domain); err != nil || open {
		return err
	}
	_, err = s.tasks.Enqueue(ctx, s.pool, nil, TaskPanelSSL, st.Domain, agentproto.PanelSSLParams{Domain: st.Domain})
	return err
}

// checkNotPanelDomain keeps customers from claiming the panel hostname.
func (s *Service) checkNotPanelDomain(ctx context.Context, names []string) error {
	panel, err := settings.NewRepo(s.pool).Get(ctx, settings.PanelDomain)
	if err != nil {
		return err
	}
	if panel != "" && slices.Contains(names, panel) {
		return apperr.Conflict("tên miền %s là tên miền của trang quản trị", panel)
	}
	return nil
}
