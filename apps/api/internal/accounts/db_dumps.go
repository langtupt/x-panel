package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/databases"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

// Per-database backup and restore. Dumps live in the owner's
// ~/backups/databases (counted in the account's disk space), so they can
// also be downloaded or uploaded with the file manager.
const (
	TaskDBDump    = "db.backup"
	TaskDBRestore = "db.restore"
)

type dbDumpJob struct {
	DatabaseID int64  `json:"database_id"`
	File       string `json:"file,omitempty"`
}

func dumpParams(owner *users.User, d *databases.Database, file string) agentproto.DBDumpParams {
	return agentproto.DBDumpParams{Username: owner.Username, File: file,
		DB: agentproto.BackupDB{Name: d.Name, Engine: d.Engine, User: d.DBUser}}
}

// DatabaseDumps lists the backup files of one database.
func (s *Service) DatabaseDumps(ctx context.Context, actor *users.User, id int64) (*agentproto.DBDumpList, error) {
	d, owner, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	var res agentproto.DBDumpList
	if err := s.agent.Call(ctx, agentproto.ActionDBDumpList, dumpParams(owner, d, ""), &res); err != nil {
		return nil, apperr.Agent(err)
	}
	if res.Files == nil {
		res.Files = []agentproto.DBDumpFile{}
	}
	return &res, nil
}

func (s *Service) enqueueDBJob(ctx context.Context, actor *users.User, kind string, d *databases.Database, job dbDumpJob) (*tasks.Task, error) {
	for _, k := range []string{TaskDBDump, TaskDBRestore} {
		if open, err := s.tasks.HasOpen(ctx, k, d.Name); err != nil {
			return nil, err
		} else if open {
			return nil, apperr.Conflict("database %s đang được sao lưu hoặc khôi phục", d.Name)
		}
	}
	t, err := s.tasks.Enqueue(ctx, s.pool, &actor.ID, kind, d.Name, job)
	if err != nil {
		return nil, err
	}
	s.tasks.Notify()
	return t, nil
}

// BackupDatabase queues a dump of one database.
func (s *Service) BackupDatabase(ctx context.Context, actor *users.User, id int64) (*tasks.Task, *databases.Database, error) {
	d, _, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, nil, err
	}
	t, err := s.enqueueDBJob(ctx, actor, TaskDBDump, d, dbDumpJob{DatabaseID: d.ID})
	return t, d, err
}

// RestoreDatabase queues the import of one of the database's dump files.
func (s *Service) RestoreDatabase(ctx context.Context, actor *users.User, id int64, file string) (*tasks.Task, *databases.Database, error) {
	d, owner, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, nil, err
	}
	if owner.Status != users.StatusActive {
		return nil, nil, apperr.Conflict("tài khoản đang bị khóa")
	}
	file = strings.TrimSpace(file)
	if file == "" || strings.ContainsAny(file, "/\\") || !strings.HasPrefix(file, d.Name+"-") {
		return nil, nil, apperr.Invalid("file sao lưu không thuộc database %s", d.Name)
	}
	t, err := s.enqueueDBJob(ctx, actor, TaskDBRestore, d, dbDumpJob{DatabaseID: d.ID, File: file})
	return t, d, err
}

// DeleteDatabaseDump removes one dump file of the database.
func (s *Service) DeleteDatabaseDump(ctx context.Context, actor *users.User, id int64, file string) (*databases.Database, error) {
	d, owner, err := s.accessibleDatabase(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if err := s.agent.Call(ctx, agentproto.ActionDBDumpDelete, dumpParams(owner, d, file), nil); err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return d, nil
}

func (s *Service) dbJob(ctx context.Context, t *tasks.Task) (*databases.Database, *users.User, dbDumpJob, error) {
	var job dbDumpJob
	if err := json.Unmarshal(t.Payload, &job); err != nil {
		return nil, nil, job, err
	}
	d, err := s.dbs.ByID(ctx, job.DatabaseID)
	if errors.Is(err, databases.ErrNotFound) {
		return nil, nil, job, errors.New("database không còn tồn tại")
	}
	if err != nil {
		return nil, nil, job, err
	}
	owner, err := s.users.ByID(ctx, d.UserID)
	return d, owner, job, err
}

func (s *Service) runDBDump(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	d, owner, _, err := s.dbJob(ctx, t)
	if err != nil {
		return err
	}
	tool := "mysqldump"
	if d.Engine == "postgresql" {
		tool = "pg_dump"
	}
	logf("Sao lưu %s (%s, %s)…", d.Name, d.Engine, tool)
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	var f agentproto.DBDumpFile
	if err := s.agent.Call(callCtx, agentproto.ActionDBDumpCreate, dumpParams(owner, d, ""), &f); err != nil {
		return err
	}
	logf("Đã lưu ~%s (%d byte, nén gzip)", f.Path, f.Size)
	return nil
}

func (s *Service) runDBRestore(ctx context.Context, t *tasks.Task, logf func(string, ...any)) error {
	d, owner, job, err := s.dbJob(ctx, t)
	if err != nil {
		return err
	}
	if owner.Status != users.StatusActive {
		return errors.New("tài khoản đang bị khóa")
	}
	logf("Khôi phục %s từ %s…", d.Name, job.File)
	callCtx, cancel := context.WithTimeout(ctx, 4*time.Hour)
	defer cancel()
	var res agentproto.TaskLog
	err = s.agent.Call(callCtx, agentproto.ActionDBDumpRestore, dumpParams(owner, d, job.File), &res)
	if strings.TrimSpace(res.Log) != "" {
		logf("%s", strings.TrimSpace(res.Log))
	}
	return err
}
