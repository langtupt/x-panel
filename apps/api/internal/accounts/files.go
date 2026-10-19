package accounts

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

const TaskPMAInstall = "pma.install"

var fileOps = []string{"list", "read", "write", "mkdir", "rename", "delete", "chmod", "extract", "compress"}

// fileOwner resolves whose home the actor is browsing.
func (s *Service) fileOwner(ctx context.Context, actor *users.User, userID *int64) (*users.User, error) {
	if userID == nil || *userID == actor.ID {
		if actor.Role != users.RoleUser {
			return nil, apperr.Invalid("phải chọn tài khoản")
		}
		return actor, nil
	}
	// A hosting account may only ever address its own home: asking for another
	// account is refused outright instead of silently falling back to its own.
	if actor.Role == users.RoleUser {
		return nil, apperr.NotFound("không tìm thấy tài khoản")
	}
	return s.siteOwner(ctx, actor, userID)
}

func (s *Service) FileOp(ctx context.Context, actor *users.User, userID *int64, req agentproto.FSRequest) (*agentproto.FSResult, error) {
	if !slices.Contains(fileOps, req.Op) {
		return nil, apperr.Invalid("thao tác %q không hợp lệ", req.Op)
	}
	owner, err := s.fileOwner(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	var res agentproto.FSResult
	if err := s.agent.Call(ctx, agentproto.ActionFSOp, agentproto.FSOpParams{Owner: owner.Username, Request: req}, &res); err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return &res, nil
}

func (s *Service) streamer() (agentproto.Streamer, error) {
	st, ok := s.agent.(agentproto.Streamer)
	if !ok {
		return nil, apperr.New(http.StatusNotImplemented, "tải file không khả dụng ở chế độ dry-run")
	}
	return st, nil
}

func (s *Service) Upload(ctx context.Context, actor *users.User, userID *int64, path string, body io.Reader) (*users.User, error) {
	st, err := s.streamer()
	if err != nil {
		return nil, err
	}
	owner, err := s.fileOwner(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	if err := st.Upload(ctx, owner.Username, path, body); err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return owner, nil
}

func (s *Service) Download(ctx context.Context, actor *users.User, userID *int64, path string) (*http.Response, error) {
	st, err := s.streamer()
	if err != nil {
		return nil, err
	}
	owner, err := s.fileOwner(ctx, actor, userID)
	if err != nil {
		return nil, err
	}
	resp, err := st.Download(ctx, owner.Username, path)
	if err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return resp, nil
}

func (s *Service) PMAStatus(ctx context.Context) (*agentproto.PMAStatus, error) {
	var st agentproto.PMAStatus
	if err := s.agent.Call(ctx, agentproto.ActionPMAStatus, nil, &st); err != nil {
		return nil, apperr.Agent(err)
	}
	return &st, nil
}

func (s *Service) InstallPMA(ctx context.Context, actor *users.User) (*tasks.Task, error) {
	return s.InstallApp(ctx, actor, agentproto.AppInstallParams{ID: "phpmyadmin", Version: "latest"})
}

func (s *Service) runPMAInstall(ctx context.Context, _ *tasks.Task, logf func(string, ...any)) error {
	logf("Tải và cài phpMyAdmin bản mới nhất…")
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var res agentproto.TaskLog
	if err := s.agent.Call(callCtx, agentproto.ActionPMAInstall, nil, &res); err != nil {
		return err
	}
	logf("%s", strings.TrimSpace(res.Log))
	return nil
}
