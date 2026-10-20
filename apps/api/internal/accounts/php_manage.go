package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/phpconfig"
)

func (s *Service) PHPManagement(ctx context.Context, v string) (*agentproto.PHPManagement, error) {
	if !agentproto.ValidPHPVersion(v) {
		return nil, apperr.Invalid("invalid PHP version")
	}
	var out agentproto.PHPManagement
	if err := s.agent.Call(ctx, agentproto.ActionPHPManageList, agentproto.PHPManageParams{Version: v}, &out); err != nil {
		return nil, apperr.Agent(err)
	}
	return &out, nil
}
func (s *Service) ApplyPHPManagement(ctx context.Context, actor *users.User, p agentproto.PHPManageParams) (*tasks.Task, error) {
	if err := p.Validate(); err != nil {
		return nil, apperr.Invalid("%s", err)
	}
	if !agentproto.ValidPHPVersion(p.Version) {
		return nil, apperr.Invalid("invalid PHP version")
	}
	target := "PHP " + p.Version
	if open, err := s.tasks.HasOpen(ctx, "php.manage", target); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("PHP operation already running")
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, "php.manage", target, p)
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}
func (s *Service) runPHPManage(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p agentproto.PHPManageParams
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	logf("Applying PHP %s changes…", p.Version)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Minute)
	defer cancel()
	var out agentproto.TaskLog
	if err := s.agent.Call(ctx, agentproto.ActionPHPManageApply, p, &out); err != nil {
		return err
	}
	logf("%s", out.Log)
	return nil
}

func (s *Service) SaveSitePHP(ctx context.Context, actor *users.User, id int64, c phpconfig.Site) error {
	if err := c.Validate(); err != nil {
		return apperr.Invalid("%s", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	site, owner, err := s.accessibleSite(ctx, s.sites.WithTx(tx), actor, id)
	if err != nil {
		return err
	}
	if site.AppType != "php" && site.AppType != "laravel" {
		return apperr.Invalid("requires a PHP/Laravel site")
	}
	if actor.Role != users.RoleAdmin && c.Workers != site.PHPConfig.Workers {
		return apperr.Invalid("custom worker count requires admin")
	}
	if err = s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return err
	}
	if err = s.sites.WithTx(tx).SetPHPConfig(ctx, id, c); err != nil {
		return err
	}
	if _, err = s.syncWeb(ctx, tx, owner); err != nil {
		_ = tx.Rollback(ctx)
		repairCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		repairTx, repairErr := s.pool.Begin(repairCtx)
		if repairErr == nil {
			defer repairTx.Rollback(repairCtx)
			if repairErr = s.users.WithTx(repairTx).LockForUpdate(repairCtx, owner.ID); repairErr == nil {
				_, repairErr = s.syncWeb(repairCtx, repairTx, owner)
			}
			if repairErr == nil {
				repairErr = repairTx.Commit(repairCtx)
			}
		}
		if repairErr != nil {
			return fmt.Errorf("%w; rollback sync failed: %v", err, repairErr)
		}
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) PHPLogs(ctx context.Context, v string) (agentproto.TaskLog, error) {
	var out agentproto.TaskLog
	if !agentproto.ValidPHPVersion(v) {
		return out, apperr.Invalid("invalid PHP version")
	}
	err := s.agent.Call(ctx, agentproto.ActionPHPManageLogs, agentproto.PHPManageParams{Version: v}, &out)
	return out, err
}

func (s *Service) SitePHP(ctx context.Context, actor *users.User, id int64) (any, error) {
	site, _, err := s.accessibleSite(ctx, s.sites, actor, id)
	if err != nil {
		return nil, err
	}
	if site.AppType != "php" && site.AppType != "laravel" {
		return nil, apperr.Invalid("requires PHP/Laravel")
	}
	defaults, err := s.PHPManagement(ctx, site.PHPVersion)
	if err != nil {
		return nil, err
	}
	filtered := []agentproto.PHPSetting{}
	for _, setting := range defaults.Settings {
		if phpconfig.SiteAllowed(setting.Name) {
			filtered = append(filtered, setting)
		}
	}
	return struct {
		Config   phpconfig.Site          `json:"config"`
		Defaults []agentproto.PHPSetting `json:"defaults"`
	}{site.PHPConfig, filtered}, nil
}
