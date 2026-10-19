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

// syncCron writes the owner's enabled jobs to their crontab.
func (s *Service) syncCron(ctx context.Context, tx pgx.Tx, owner *users.User) error {
	list, err := s.cron.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil {
		return err
	}
	p := agentproto.CronSyncParams{Username: owner.Username, Jobs: []agentproto.CronJob{}}
	for _, j := range list {
		if j.Enabled {
			p.Jobs = append(p.Jobs, agentproto.CronJob{Schedule: j.Schedule, Command: j.Command})
		}
	}
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
