package accounts

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/cronjobs"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

type CronInput struct {
	UserID   *int64 `json:"user_id"` // owner; managers only, on create
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
}

func validateCron(in *CronInput) error {
	in.Schedule = strings.Join(strings.Fields(in.Schedule), " ")
	if err := validate.CronSchedule(in.Schedule); err != nil {
		return apperr.Invalid("%s", err.Error())
	}
	if err := validate.CronCommand(in.Command); err != nil {
		return apperr.Invalid("%s", err.Error())
	}
	return nil
}

// enabledCron lists the owner's enabled jobs in the order the agent numbers
// their timers.
func enabledCron(owner *users.User, list []cronjobs.Job) agentproto.CronSyncParams {
	p := agentproto.CronSyncParams{Username: owner.Username, Jobs: []agentproto.CronJob{}}
	for _, j := range list {
		if j.Enabled {
			p.Jobs = append(p.Jobs, agentproto.CronJob{Schedule: j.Schedule, Command: j.Command})
		}
	}
	return p
}

// syncCron writes the owner's enabled jobs as systemd timers.
func (s *Service) syncCron(ctx context.Context, tx pgx.Tx, owner *users.User) error {
	list, err := s.cron.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil {
		return err
	}
	p := enabledCron(owner, list)
	if err := s.agent.Call(ctx, agentproto.ActionCronSync, p, nil); err != nil {
		return apperr.Agent(err)
	}
	return nil
}

func (s *Service) accessibleCron(ctx context.Context, actor *users.User, id int64) (*cronjobs.Job, *users.User, error) {
	j, err := s.cron.ByID(ctx, id)
	if errors.Is(err, cronjobs.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy cron job")
	}
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.users.ByID(ctx, j.UserID)
	if err != nil {
		return nil, nil, err
	}
	if owner.ID != actor.ID && !users.CanManage(actor, owner) {
		return nil, nil, apperr.NotFound("không tìm thấy cron job")
	}
	return j, owner, nil
}

// cronRef locates job j among the owner's timers.
func (s *Service) cronRef(ctx context.Context, owner *users.User, j *cronjobs.Job) (agentproto.CronJobRef, error) {
	list, err := s.cron.ByUser(ctx, owner.ID)
	if err != nil {
		return agentproto.CronJobRef{}, err
	}
	ref := agentproto.CronJobRef{CronSyncParams: enabledCron(owner, list), Index: -1}
	n := 0
	for _, x := range list {
		if !x.Enabled {
			continue
		}
		if x.ID == j.ID {
			ref.Index = n
		}
		n++
	}
	return ref, nil
}

// CronStatus reports the last run of a job and the end of its output.
func (s *Service) CronStatus(ctx context.Context, actor *users.User, id int64) (*agentproto.CronStatus, error) {
	j, owner, err := s.accessibleCron(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	ref, err := s.cronRef(ctx, owner, j)
	if err != nil {
		return nil, err
	}
	if ref.Index < 0 {
		return &agentproto.CronStatus{}, nil // disabled: no timer
	}
	var st agentproto.CronStatus
	if err := s.agent.Call(ctx, agentproto.ActionCronStatus, ref, &st); err != nil {
		return nil, apperr.Agent(err)
	}
	return &st, nil
}

// RunCron starts a job now, in the background, with its usual sandbox.
func (s *Service) RunCron(ctx context.Context, actor *users.User, id int64) (*cronjobs.Job, error) {
	j, owner, err := s.accessibleCron(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if owner.Status == users.StatusSuspended {
		return nil, apperr.Conflict("tài khoản đang bị khóa")
	}
	ref, err := s.cronRef(ctx, owner, j)
	if err != nil {
		return nil, err
	}
	if ref.Index < 0 {
		return nil, apperr.Conflict("cron job đang tắt, hãy bật trước khi chạy")
	}
	if err := s.agent.Call(ctx, agentproto.ActionCronRun, ref, nil); err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return j, nil
}

func (s *Service) ListCron(ctx context.Context, actor *users.User) ([]cronjobs.Job, error) {
	return s.cron.Visible(ctx, actor.ID, string(actor.Role))
}

func (s *Service) CreateCron(ctx context.Context, actor *users.User, in CronInput) (*cronjobs.Job, error) {
	owner, err := s.siteOwner(ctx, actor, in.UserID)
	if err != nil {
		return nil, err
	}
	if err := validateCron(&in); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	cr := s.cron.WithTx(tx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if owner.PackageID != nil {
		p, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		n, err := cr.CountByUser(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		if p.MaxCron > 0 && n >= p.MaxCron {
			return nil, apperr.Conflict("gói %q chỉ cho phép %d cron job", p.Name, p.MaxCron)
		}
	}
	j := &cronjobs.Job{UserID: owner.ID, Username: owner.Username, Schedule: in.Schedule, Command: in.Command, Enabled: in.Enabled}
	if err := cr.Create(ctx, j); err != nil {
		return nil, err
	}
	if err := s.syncCron(ctx, tx, owner); err != nil {
		return nil, err
	}
	return j, tx.Commit(ctx)
}

func (s *Service) UpdateCron(ctx context.Context, actor *users.User, id int64, in CronInput) (*cronjobs.Job, error) {
	j, owner, err := s.accessibleCron(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if err := validateCron(&in); err != nil {
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
	j.Schedule, j.Command, j.Enabled = in.Schedule, in.Command, in.Enabled
	if err := s.cron.WithTx(tx).Update(ctx, j); err != nil {
		return nil, err
	}
	if err := s.syncCron(ctx, tx, owner); err != nil {
		return nil, err
	}
	return j, tx.Commit(ctx)
}

func (s *Service) DeleteCron(ctx context.Context, actor *users.User, id int64) (*cronjobs.Job, error) {
	j, owner, err := s.accessibleCron(ctx, actor, id)
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
	if err := s.cron.WithTx(tx).Delete(ctx, id); err != nil {
		return nil, err
	}
	if err := s.syncCron(ctx, tx, owner); err != nil {
		return nil, err
	}
	return j, tx.Commit(ctx)
}
