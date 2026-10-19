// Package databases stores customer MariaDB databases.
package databases

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xpanel/apps/api/internal/db"
)

type Database struct {
	Engine    string    `json:"engine"`
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	DBUser    string    `json:"db_user"`
	CreatedAt time.Time `json:"created_at"`

	// Filled from the agent, not stored.
	SizeBytes *int64 `json:"size_bytes"`
}

var (
	ErrNotFound = errors.New("database not found")
	ErrTaken    = errors.New("tên database đã tồn tại")
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const selectCols = `SELECT d.id, d.user_id, u.username, d.name, d.db_user, d.created_at, d.engine
	FROM databases d JOIN users u ON u.id = d.user_id`

func scan(row pgx.Row) (*Database, error) {
	var d Database
	err := row.Scan(&d.ID, &d.UserID, &d.Username, &d.Name, &d.DBUser, &d.CreatedAt, &d.Engine)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func collect(rows pgx.Rows, err error) ([]Database, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *Repo) ByID(ctx context.Context, id int64) (*Database, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE d.id=$1`, id))
}

func (r *Repo) ByUser(ctx context.Context, userID int64) ([]Database, error) {
	return collect(r.q.Query(ctx, selectCols+` WHERE d.user_id=$1 ORDER BY d.name`, userID))
}

// Visible lists databases an account may see: all (admin), its customers' (reseller), its own (user).
func (r *Repo) Visible(ctx context.Context, userID int64, role string) ([]Database, error) {
	switch role {
	case "admin":
		return collect(r.q.Query(ctx, selectCols+` ORDER BY d.name`))
	case "reseller":
		return collect(r.q.Query(ctx, selectCols+` WHERE u.parent_id=$1 ORDER BY d.name`, userID))
	}
	return r.ByUser(ctx, userID)
}

func (r *Repo) CountByUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM databases WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

func (r *Repo) Create(ctx context.Context, d *Database) error {
	err := r.q.QueryRow(ctx, `INSERT INTO databases (user_id, name, db_user, engine) VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`, d.UserID, d.Name, d.DBUser, d.Engine).Scan(&d.ID, &d.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM databases WHERE id=$1`, id)
	return err
}
