// Package audit records every state-changing action in audit_logs.
package audit

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ActorID    *int64
	Action     string // e.g. "auth.login", "user.create"
	TargetType string
	TargetID   *int64
	IP         string
	Meta       map[string]any
}

type Logger struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Logger { return &Logger{pool: pool, log: log} }

// Record never fails the caller's request; a write error is only logged.
func (l *Logger) Record(ctx context.Context, e Entry) {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	_, err := l.pool.Exec(ctx,
		`INSERT INTO audit_logs (actor_id, action, target_type, target_id, ip, meta) VALUES ($1,$2,$3,$4,$5,$6)`,
		e.ActorID, e.Action, e.TargetType, e.TargetID, e.IP, e.Meta)
	if err != nil {
		l.log.Error("audit write failed", "action", e.Action, "err", err)
	}
}
