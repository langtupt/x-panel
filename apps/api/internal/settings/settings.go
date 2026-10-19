// Package settings stores server-wide key/value settings.
package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/db"
)

const (
	PanelDomain = "panel_domain" // hostname of the panel certificate (also the mail hostname)
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo { return &Repo{q: q} }

// Get returns "" for an unset key.
func (r *Repo) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := r.q.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (r *Repo) Set(ctx context.Context, key, value string) error {
	_, err := r.q.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1,$2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, key, value)
	return err
}
