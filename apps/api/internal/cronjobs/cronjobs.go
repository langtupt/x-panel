// Package cronjobs stores scheduled commands of hosting accounts.
package cronjobs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/db"
)

type Job struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Schedule  string    `json:"schedule"`
	Command   string    `json:"command"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

var ErrNotFound = errors.New("cron job not found")

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const selectCols = `SELECT c.id, c.user_id, u.username, c.schedule, c.command, c.enabled, c.created_at
	FROM cron_jobs c JOIN users u ON u.id = c.user_id`

func scan(row pgx.Row) (*Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.UserID, &j.Username, &j.Schedule, &j.Command, &j.Enabled, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &j, err
}

func collect(rows pgx.Rows, err error) ([]Job, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *Repo) ByID(ctx context.Context, id int64) (*Job, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE c.id=$1`, id))
}

func (r *Repo) ByUser(ctx context.Context, userID int64) ([]Job, error) {
	return collect(r.q.Query(ctx, selectCols+` WHERE c.user_id=$1 ORDER BY c.id`, userID))
}

// Visible lists jobs an account may see: all (admin), its customers' (reseller), its own (user).
func (r *Repo) Visible(ctx context.Context, userID int64, role string) ([]Job, error) {
	switch role {
	case "admin":
		return collect(r.q.Query(ctx, selectCols+` ORDER BY u.username, c.id`))
	case "reseller":
		return collect(r.q.Query(ctx, selectCols+` WHERE u.parent_id=$1 ORDER BY u.username, c.id`, userID))
	}
	return r.ByUser(ctx, userID)
}

func (r *Repo) CountByUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM cron_jobs WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

func (r *Repo) Create(ctx context.Context, j *Job) error {
	return r.q.QueryRow(ctx, `INSERT INTO cron_jobs (user_id, schedule, command, enabled) VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`, j.UserID, j.Schedule, j.Command, j.Enabled).Scan(&j.ID, &j.CreatedAt)
}

func (r *Repo) Update(ctx context.Context, j *Job) error {
	_, err := r.q.Exec(ctx, `UPDATE cron_jobs SET schedule=$2, command=$3, enabled=$4 WHERE id=$1`,
		j.ID, j.Schedule, j.Command, j.Enabled)
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM cron_jobs WHERE id=$1`, id)
	return err
}
