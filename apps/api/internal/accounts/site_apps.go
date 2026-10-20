package accounts

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
	"time"
	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

const TaskSiteBuild = "site.build"

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var runtimeVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

func normalizeAppInput(in *SiteInput) error {
	if in.DeployScript != nil {
		v := strings.TrimSpace(strings.ReplaceAll(*in.DeployScript, "\r\n", "\n"))
		if len(v) > 8000 || strings.ContainsRune(v, 0) {
			return apperr.Invalid("lệnh triển khai tối đa 8000 ký tự")
		}
		in.DeployScript = &v
	}
	if in.AppType == "" {
		if in.PHPVersion == "" {
			in.AppType = "html"
		} else {
			in.AppType = "php"
		}
	}
	switch in.AppType {
	case "html", "php", "laravel", "nodejs", "nextjs", "go":
	default:
		return apperr.Invalid("loại ứng dụng không hợp lệ")
	}
	if in.AppType != "php" && in.AppType != "laravel" {
		in.PHPVersion = ""
	}
	if (in.AppType == "php" || in.AppType == "laravel") && in.PHPVersion == "" {
		return apperr.Invalid("phải chọn PHP")
	}
	if in.Runtime != "" && !runtimeVersion.MatchString(in.Runtime) {
		return apperr.Invalid("phiên bản runtime không hợp lệ")
	}
	if in.AppType == "laravel" && in.DocRoot == "" {
		in.DocRoot = "public"
	}
	if in.Env == nil {
		in.Env = map[string]string{}
	}
	if len(in.Env) > 100 {
		return apperr.Invalid("tối đa 100 biến môi trường")
	}
	for k, v := range in.Env {
		if !envName.MatchString(k) || len(v) > 8192 || containsControl(v) {
			return apperr.Invalid("biến môi trường không hợp lệ: %s", k)
		}
		if k == "PORT" || k == "HOST" || k == "PATH" || k == "HOME" {
			return apperr.Invalid("biến %s do hệ thống quản lý", k)
		}
	}
	return nil
}
func containsControl(s string) bool {
	for _, r := range s {
		if r == '\x00' || r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}
func allocateAppPort(ctx context.Context, tx pgx.Tx, site *sites.Site) error {
	if site.AppType != "nodejs" && site.AppType != "nextjs" && site.AppType != "go" {
		site.Port = 0
		return nil
	}
	if site.Port != 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(783622)`); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `SELECT p FROM generate_series(10000,29999) p WHERE NOT EXISTS(SELECT 1 FROM sites WHERE app_port=p) ORDER BY p LIMIT 1`).Scan(&site.Port)
}
func appSite(site *sites.Site) agentproto.WebSite {
	return agentproto.WebSite{PHPConfig: site.PHPConfig, Domain: site.Domain, PHPVersion: site.PHPVersion, DocRoot: site.DocRoot, AppType: site.AppType, Runtime: site.Runtime, Port: site.Port, Env: site.Env}
}

type siteBuildPayload struct {
	SiteID int64 `json:"site_id"`
	Sample bool  `json:"sample"`
}

func (s *Service) BuildSite(ctx context.Context, actor *users.User, id int64) (*tasks.Task, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	if owner.Status != users.StatusActive {
		return nil, apperr.Conflict("tài khoản đang bị khóa")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(783623, $1::integer)`, id); err != nil {
		return nil, err
	}
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE type=$1 AND target=$2 AND status IN ('pending','running'))`, TaskSiteBuild, site.Domain).Scan(&open); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang build website")
	}
	t, err := s.tasks.Enqueue(ctx, tx, &actor.ID, TaskSiteBuild, site.Domain, siteBuildPayload{SiteID: id})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}
func (s *Service) SiteAppInfo(ctx context.Context, actor *users.User, id int64, action string) (any, error) {
	site, owner, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	p := agentproto.SiteAppParams{Username: owner.Username, Site: appSite(site)}
	if action == agentproto.ActionSiteDetect {
		var out agentproto.SiteDetectResult
		err = s.agent.Call(ctx, action, p, &out)
		return out, err
	}
	var out agentproto.TaskLog
	err = s.agent.Call(ctx, action, p, &out)
	return out, err
}
func (s *Service) runSiteBuild(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var in siteBuildPayload
	if err := json.Unmarshal(t.Payload, &in); err != nil {
		return err
	}
	site, err := s.sites.ByID(ctx, in.SiteID)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := s.users.WithTx(tx).LockForUpdate(ctx, site.UserID); err != nil {
		return err
	}
	site, err = s.sites.WithTx(tx).ByID(ctx, in.SiteID)
	if err != nil {
		return err
	}
	owner, err := s.users.ByID(ctx, site.UserID)
	if err != nil {
		return err
	}
	if owner.Status != users.StatusActive {
		return apperr.Conflict("tài khoản đang bị khóa")
	}
	// Release the account lock before the (up to 30 min) build so the account
	// stays manageable; suspending it meanwhile kills the build's slice and the
	// agent refuses to start the app of a locked account.
	if err := tx.Rollback(ctx); err != nil {
		return err
	}
	logf("Build %s (%s)", site.Domain, site.AppType)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var out agentproto.TaskLog
	err = s.agent.Call(ctx, agentproto.ActionSiteBuild, agentproto.SiteAppParams{Username: owner.Username, Site: appSite(site), Sample: in.Sample, DeployScript: site.DeployScript}, &out)
	if out.Log != "" {
		logf("%s", out.Log)
	}
	return err
}
