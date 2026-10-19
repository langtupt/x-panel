// Package ftpaccounts stores Pure-FTPd virtual users of hosting accounts.
package ftpaccounts

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xpanel/apps/api/internal/db"
)

type Account struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Login     string    `json:"login"`
	Dir       string    `json:"dir"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrNotFound = errors.New("ftp account not found")
	ErrTaken    = errors.New("tên FTP đã tồn tại")
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const selectCols = `SELECT f.id, f.user_id, u.username, f.login, f.dir, f.created_at
	FROM ftp_accounts f JOIN users u ON u.id = f.user_id`

func scan(row pgx.Row) (*Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.UserID, &a.Username, &a.Login, &a.Dir, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func collect(rows pgx.Rows, err error) ([]Account, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repo) ByID(ctx context.Context, id int64) (*Account, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE f.id=$1`, id))
}

func (r *Repo) ByUser(ctx context.Context, userID int64) ([]Account, error) {
	return collect(r.q.Query(ctx, selectCols+` WHERE f.user_id=$1 ORDER BY f.login`, userID))
}

// Visible lists FTP accounts an account may see: all (admin), its customers' (reseller), its own (user).
func (r *Repo) Visible(ctx context.Context, userID int64, role string) ([]Account, error) {
	switch role {
	case "admin":
		return collect(r.q.Query(ctx, selectCols+` ORDER BY f.login`))
	case "reseller":
		return collect(r.q.Query(ctx, selectCols+` WHERE u.parent_id=$1 ORDER BY f.login`, userID))
	}
	return r.ByUser(ctx, userID)
}

func (r *Repo) CountByUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM ftp_accounts WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

func (r *Repo) Create(ctx context.Context, a *Account) error {
	err := r.q.QueryRow(ctx, `INSERT INTO ftp_accounts (user_id, login, dir) VALUES ($1,$2,$3)
		RETURNING id, created_at`, a.UserID, a.Login, a.Dir).Scan(&a.ID, &a.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM ftp_accounts WHERE id=$1`, id)
	return err
}
