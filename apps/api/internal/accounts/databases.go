package accounts

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/databases"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

type DatabaseInput struct {
	Engine   string `json:"engine"`
	UserID   *int64 `json:"user_id"` // owner; managers only
	Name     string `json:"name"`    // suffix; the full name is "<owner>_<name>"
	Password string `json:"password"`
}

func dbParams(owner string, d *databases.Database) agentproto.DBParams {
	return agentproto.DBParams{Engine: d.Engine, Owner: owner, Name: d.Name, User: d.DBUser}
}

// accessibleDatabase loads a database the actor may manage.
func (s *Service) accessibleDatabase(ctx context.Context, actor *users.User, id int64) (*databases.Database, *users.User, error) {
	d, err := s.dbs.ByID(ctx, id)
	if errors.Is(err, databases.ErrNotFound) {
		return nil, nil, apperr.NotFound("không tìm thấy database")
	}
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.users.ByID(ctx, d.UserID)
	if err != nil {
		return nil, nil, err
	}
	if owner.ID != actor.ID && !users.CanManage(actor, owner) {
		return nil, nil, apperr.NotFound("không tìm thấy database")
	}
	return d, owner, nil
}

// ListDatabases returns visible databases with their size (best effort).
func (s *Service) ListDatabases(ctx context.Context, actor *users.User) ([]databases.Database, error) {
	list, err := s.dbs.Visible(ctx, actor.ID, string(actor.Role))
	if err != nil {
		return nil, err
	}
	var sizes agentproto.DBSizes
	if err := s.agent.Call(ctx, agentproto.ActionDBSizes, nil, &sizes); err != nil {
		s.log.Warn("database sizes unavailable", "err", err)
		return list, nil
	}
	for i := range list {
		if n, ok := sizes.Sizes[list[i].Name]; ok {
			list[i].SizeBytes = &n
		}
	}
	return list, nil
}

func (s *Service) CreateDatabase(ctx context.Context, actor *users.User, in DatabaseInput) (*databases.Database, error) {
	owner, err := s.siteOwner(ctx, actor, in.UserID)
	if err != nil {
		return nil, err
	}
	if in.Engine == "" {
		in.Engine = "mariadb"
	}
	if in.Engine != "mariadb" && in.Engine != "postgresql" {
		return nil, apperr.Invalid("engine phải là mariadb hoặc postgresql")
	}
	full := owner.Username + "_" + in.Name
	if err := validate.DBName(owner.Username, full); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	if err := validate.DBPassword(in.Password); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	dr := s.dbs.WithTx(tx)
	if err := s.users.WithTx(tx).LockForUpdate(ctx, owner.ID); err != nil {
		return nil, err
	}
	if owner.PackageID != nil {
		p, err := s.pkgs.WithTx(tx).ByID(ctx, *owner.PackageID)
		if err != nil {
			return nil, err
		}
		n, err := dr.CountByUser(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		if p.MaxDBs > 0 && n >= p.MaxDBs {
			return nil, apperr.Conflict("gói %q chỉ cho phép %d database", p.Name, p.MaxDBs)
		}
	}
	d := &databases.Database{UserID: owner.ID, Username: owner.Username, Name: full, DBUser: full, Engine: in.Engine}
	if err := dr.Create(ctx, d); errors.Is(err, databases.ErrTaken) {
		return nil, apperr.Conflict("%s", err.Error())
	} else if err != nil {
		return nil, err
	}

	p := dbParams(owner.Username, d)
	p.Password = in.Password
	if err := s.agent.Call(ctx, agentproto.ActionDBCreate, p, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.agent.Call(context.WithoutCancel(ctx), agentproto.ActionDBDelete, dbParams(owner.Username, d), nil)
		}
	}()
	if owner.Status == users.StatusSuspended {
		if err := s.lockDBUsers(ctx, tx, owner, true); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	zero := int64(0)
	d.SizeBytes = &zero
	return d, nil
}

func (s *Service) SetDatabasePassword(ctx context.Context, actor *users.User, id int64, password string) (*databases.Database, error) {
	d, owner, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if err := validate.DBPassword(password); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	p := dbParams(owner.Username, d)
	p.Password = password
	if err := s.agent.Call(ctx, agentproto.ActionDBPassword, p, nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return d, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, actor *users.User, id int64) (*databases.Database, error) {
	d, owner, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := s.dbs.WithTx(tx).Delete(ctx, id); err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionDBDelete, dbParams(owner.Username, d), nil); err != nil {
		return nil, apperr.Agent(err)
	}
	return d, tx.Commit(ctx)
}

// lockDBUsers locks or unlocks every MariaDB user of an account.
func (s *Service) lockDBUsers(ctx context.Context, tx pgx.Tx, owner *users.User, locked bool) error {
	list, err := s.dbs.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil || len(list) == 0 {
		return err
	}
	for _, engine := range []string{"mariadb", "postgresql"} {
		p := agentproto.DBLockParams{Engine: engine, Owner: owner.Username, Locked: locked}
		for _, d := range list {
			if d.Engine == engine {
				p.Users = append(p.Users, d.DBUser)
			}
		}
		if len(p.Users) == 0 {
			continue
		}
		if err := s.agent.Call(ctx, agentproto.ActionDBLock, p, nil); err != nil {
			return apperr.Agent(err)
		}
	}
	return nil
}

// dropAllDatabases removes an account's databases from MariaDB (rows go with the account).
func (s *Service) dropAllDatabases(ctx context.Context, tx pgx.Tx, owner *users.User) error {
	list, err := s.dbs.WithTx(tx).ByUser(ctx, owner.ID)
	if err != nil {
		return err
	}
	for i := range list {
		if err := s.agent.Call(ctx, agentproto.ActionDBDelete, dbParams(owner.Username, &list[i]), nil); err != nil {
			return apperr.Agent(err)
		}
	}
	return nil
}
