// Package tasks is a small PostgreSQL-backed job queue for long-running
// work (installing PHP, issuing certificates). Workers claim jobs with
// FOR UPDATE SKIP LOCKED, so several API processes can share the queue.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xpanel/apps/api/internal/db"
)

type Task struct {
	ID         int64           `json:"id"`
	UserID     *int64          `json:"user_id"`
	Type       string          `json:"type"`
	Target     string          `json:"target"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"` // pending | running | done | failed
	Log        string          `json:"log"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}

// Handler runs one task. logf appends a line to the task log visible in the UI.
type Handler func(ctx context.Context, t *Task, logf func(format string, args ...any)) error

var ErrNotFound = errors.New("task not found")

type Queue struct {
	pool     *pgxpool.Pool
	log      *slog.Logger
	handlers map[string]Handler
	wake     chan struct{}

	mu      sync.Mutex
	running map[int64]context.CancelFunc // tasks executing in this process
	stopped map[int64]bool               // cancelled by a user
}

func NewQueue(pool *pgxpool.Pool, log *slog.Logger) *Queue {
	return &Queue{pool: pool, log: log, handlers: map[string]Handler{}, wake: make(chan struct{}, 1),
		running: map[int64]context.CancelFunc{}, stopped: map[int64]bool{}}
}

func (q *Queue) Handle(typ string, h Handler) { q.handlers[typ] = h }

const cols = `id, user_id, type, target, payload, status, log, created_at, started_at, finished_at`

func scan(row pgx.Row) (*Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.UserID, &t.Type, &t.Target, &t.Payload, &t.Status, &t.Log, &t.CreatedAt, &t.StartedAt, &t.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// Enqueue adds a task; pass a transaction as q to enqueue atomically with other changes.
func (q *Queue) Enqueue(ctx context.Context, dbq db.Querier, userID *int64, typ, target string, payload any) (*Task, error) {
	if _, ok := q.handlers[typ]; !ok {
		return nil, fmt.Errorf("no handler for task type %q", typ)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	t, err := scan(dbq.QueryRow(ctx, `INSERT INTO tasks (user_id, type, target, payload) VALUES ($1,$2,$3,$4)
		RETURNING `+cols, userID, typ, target, raw))
	if err == nil {
		q.Notify()
	}
	return t, err
}

// Notify wakes an idle worker (call after committing a transaction that enqueued).
func (q *Queue) Notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// HasOpen reports whether a pending/running task of typ exists for target.
func (q *Queue) HasOpen(ctx context.Context, typ, target string) (bool, error) {
	var ok bool
	err := q.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE type=$1 AND target=$2
		AND status IN ('pending','running'))`, typ, target).Scan(&ok)
	return ok, err
}

func (q *Queue) Get(ctx context.Context, id int64) (*Task, error) {
	return scan(q.pool.QueryRow(ctx, `SELECT `+cols+` FROM tasks WHERE id=$1`, id))
}

// List returns recent tasks; userID nil = all users.
func (q *Queue) List(ctx context.Context, userID *int64, limit int) ([]Task, error) {
	rows, err := q.pool.Query(ctx, `SELECT `+cols+` FROM tasks WHERE $1::bigint IS NULL OR user_id=$1
		ORDER BY id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// Run starts n workers and blocks until ctx is cancelled. Tasks left
// "running" by a previous process are marked failed first.
func (q *Queue) Run(ctx context.Context, n int) {
	if _, err := q.pool.Exec(ctx, `UPDATE tasks SET status='failed', finished_at=now(),
		log = log || E'\nBị gián đoạn do panel khởi động lại.' WHERE status='running'`); err != nil {
		q.log.Error("reset running tasks", "err", err)
	}
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			q.worker(ctx)
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
}

func (q *Queue) worker(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		for {
			t, err := q.claim(ctx)
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, ErrNotFound) {
					q.log.Error("claim task", "err", err)
				}
				break
			}
			q.execute(ctx, t)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-tick.C:
		}
	}
}

func (q *Queue) claim(ctx context.Context) (*Task, error) {
	return scan(q.pool.QueryRow(ctx, `UPDATE tasks SET status='running', started_at=now()
		WHERE id = (SELECT id FROM tasks WHERE status='pending' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+cols))
}

// Interruptible lists task types that may be stopped while running. The
// others (apt installs and removals, PHP management…) can only be cancelled
// before they start: killing dpkg half way leaves the system broken.
var Interruptible = []string{"site.build", "backup.create", "malware.scan", "ssl.issue", "db.backup", "db.restore"}

// ErrNotCancellable is returned for a task that has finished or must not be
// interrupted.
var ErrNotCancellable = errors.New("task cannot be cancelled")

// Cancel stops a pending task, or a running one of an interruptible type.
func (q *Queue) Cancel(ctx context.Context, t *Task) error {
	tag, err := q.pool.Exec(ctx, `UPDATE tasks SET status='failed', finished_at=now(),
		log = log || $2 WHERE id=$1 AND status='pending'`, t.ID, time.Now().Format("15:04:05")+" Đã hủy trước khi chạy.\n")
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	if !slices.Contains(Interruptible, t.Type) {
		return ErrNotCancellable
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	cancel, ok := q.running[t.ID]
	if !ok {
		return ErrNotCancellable
	}
	q.stopped[t.ID] = true
	cancel()
	return nil
}

func (q *Queue) execute(parent context.Context, t *Task) {
	ctx, cancel := context.WithCancel(parent)
	q.mu.Lock()
	q.running[t.ID] = cancel
	q.mu.Unlock()
	defer func() {
		cancel()
		q.mu.Lock()
		delete(q.running, t.ID)
		delete(q.stopped, t.ID)
		q.mu.Unlock()
	}()
	logf := func(format string, args ...any) {
		line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...) + "\n"
		if _, err := q.pool.Exec(context.WithoutCancel(ctx), `UPDATE tasks SET log = log || $2 WHERE id=$1`, t.ID, line); err != nil {
			q.log.Error("task log", "task", t.ID, "err", err)
		}
	}
	status := "done"
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return q.handlers[t.Type](ctx, t, logf)
	}()
	q.mu.Lock()
	stopped := q.stopped[t.ID]
	q.mu.Unlock()
	if stopped {
		status = "failed"
		logf("Đã hủy bởi người dùng.")
	} else if err != nil {
		status = "failed"
		logf("LỖI: %v", err)
		q.log.Warn("task failed", "task", t.ID, "type", t.Type, "err", err)
	}
	if _, err := q.pool.Exec(context.WithoutCancel(ctx), `UPDATE tasks SET status=$2, finished_at=now() WHERE id=$1`, t.ID, status); err != nil {
		q.log.Error("task finish", "task", t.ID, "err", err)
	}
}
