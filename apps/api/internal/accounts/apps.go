package accounts

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

const TaskAppInstall = "app.install"

// ListApps returns the server application catalog; refresh re-checks the
// upstream version lists instead of using the agent's cache.
func (s *Service) ListApps(ctx context.Context, refresh bool) (*agentproto.AppList, error) {
	var list agentproto.AppList
	if err := s.agent.Call(ctx, agentproto.ActionAppsList, agentproto.AppListParams{Refresh: refresh}, &list); err != nil {
		return nil, apperr.Agent(err)
	}
	if list.Apps == nil {
		list.Apps = []agentproto.AppInfo{}
	}
	return &list, nil
}

// InstallApp queues the installation of one catalog app version.
func (s *Service) InstallApp(ctx context.Context, actor *users.User, p agentproto.AppInstallParams) (*tasks.Task, error) {
	list, err := s.ListApps(ctx, false)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(list.Apps, func(a agentproto.AppInfo) bool { return a.ID == p.ID })
	if i < 0 {
		return nil, apperr.Invalid("không có ứng dụng %q", p.ID)
	}
	a := list.Apps[i]
	if a.Builtin {
		return nil, apperr.Invalid("%s được cài sẵn cùng panel", a.Name)
	}
	// The legacy POST /phpmyadmin endpoint has no version parameter. Resolve
	// its alias against the live catalog now that versions are explicit.
	if p.ID == "phpmyadmin" && p.Version == "latest" && len(a.Available) > 0 {
		p.Version = a.Available[0]
	}
	if !slices.Contains(a.Available, p.Version) {
		return nil, apperr.Invalid("phiên bản %q của %s không có trong danh sách", p.Version, a.Name)
	}
	if a.MultiVersion && slices.Contains(a.Installed, p.Version) {
		return nil, apperr.Conflict("%s %s đã được cài", a.Name, p.Version)
	}
	target := strings.TrimSuffix(a.Name+" "+p.Version, " latest")
	if open, err := s.tasks.HasOpen(ctx, TaskAppInstall, target); err != nil {
		return nil, err
	} else if open {
		return nil, apperr.Conflict("đang cài %s", target)
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, TaskAppInstall, target, p)
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

func (s *Service) runAppInstall(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p agentproto.AppInstallParams
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	logf("Bắt đầu cài %s (có thể mất vài phút)…", t.Target)
	callCtx, cancel := context.WithTimeout(ctx, 40*time.Minute)
	defer cancel()
	var res agentproto.TaskLog
	if err := s.agent.Call(callCtx, agentproto.ActionAppsInstall, p, &res); err != nil {
		return err
	}
	logf("%s", strings.TrimSpace(res.Log))
	logf("Đã cài xong %s", t.Target)
	return nil
}

const TaskAppRemove = "app.remove"

func (s *Service) checkAppRemoval(ctx context.Context, p agentproto.AppInstallParams) error {
	var used bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE
 ($1='php' AND php_version=$2) OR
 ($1='nodejs' AND app_type IN ('nodejs','nextjs') AND (runtime=$2 OR runtime='')) OR
 ($1='go' AND app_type='go' AND (runtime=$2 OR runtime='')) OR
 ($1='composer' AND app_type='laravel'))`, p.ID, p.Version).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return apperr.Conflict("Phiên bản đang được website sử dụng; hãy chuyển website sang phiên bản khác trước")
	}
	return nil
}

func (s *Service) RemoveApp(ctx context.Context, actor *users.User, p agentproto.AppInstallParams) (*tasks.Task, error) {
	list, err := s.ListApps(ctx, false)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(list.Apps, func(a agentproto.AppInfo) bool { return a.ID == p.ID })
	if i < 0 || !slices.Contains(list.Apps[i].Installed, p.Version) {
		return nil, apperr.Invalid("Phiên bản chưa được cài đặt")
	}
	if !slices.Contains([]string{"php", "nodejs", "go", "composer", "redis", "memcached", "adminer", "phpmyadmin"}, p.ID) || list.Apps[i].Builtin {
		return nil, apperr.Conflict("Database được bảo vệ để giữ dữ liệu và hoạt động của panel")
	}
	if err := s.checkAppRemoval(ctx, p); err != nil {
		return nil, err
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, TaskAppRemove, list.Apps[i].Name+" "+p.Version, p)
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

func (s *Service) runAppRemove(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	var p agentproto.AppInstallParams
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return err
	}
	if err := s.checkAppRemoval(ctx, p); err != nil {
		return err
	}
	logf("Đang gỡ %s…", t.Target)
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	var res agentproto.TaskLog
	if err := s.agent.Call(callCtx, agentproto.ActionAppsRemove, p, &res); err != nil {
		return err
	}
	logf("%s", res.Log)
	logf("Đã gỡ %s", t.Target)
	return nil
}
