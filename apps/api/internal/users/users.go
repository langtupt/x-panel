// Package users holds panel accounts (admin, reseller, user) and their storage.
package users

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xpanel/apps/api/internal/db"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleReseller Role = "reseller"
	RoleUser     Role = "user"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	ParentID     *int64    `json:"parent_id"`
	ParentName   *string   `json:"parent_name"`
	PackageID    *int64    `json:"package_id"`
	PackageName  *string   `json:"package_name"`
	DiskLimitMB  *int      `json:"disk_limit_mb"`
	LinuxUID     *int      `json:"linux_uid"`
	TOTPSecret   *string   `json:"-"` // sealed by package auth
	TOTPEnabled  bool      `json:"totp_enabled"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`

	// Filled from the agent's quota report, not stored.
	DiskUsedMB *int `json:"disk_used_mb"`
}

var (
	ErrNotFound = errors.New("user not found")
	ErrTaken    = errors.New("tên đăng nhập đã tồn tại")
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const selectCols = `SELECT u.id, u.username, u.email, u.password_hash, u.role, u.parent_id, par.username,
	u.package_id, p.name, p.disk_mb, u.linux_uid, u.totp_secret, u.status, u.created_at
	FROM users u
	LEFT JOIN users par ON par.id = u.parent_id
	LEFT JOIN packages p ON p.id = u.package_id`

func scan(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.ParentID, &u.ParentName,
		&u.PackageID, &u.PackageName, &u.DiskLimitMB, &u.LinuxUID, &u.TOTPSecret, &u.Status, &u.CreatedAt)
	u.TOTPEnabled = u.TOTPSecret != nil
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (r *Repo) ByID(ctx context.Context, id int64) (*User, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE u.id=$1`, id))
}

func (r *Repo) ByUsername(ctx context.Context, username string) (*User, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE u.username=$1`, username))
}

// List returns all accounts when parentID is nil, otherwise the direct children.
func (r *Repo) List(ctx context.Context, parentID *int64) ([]User, error) {
	q := selectCols
	var args []any
	if parentID != nil {
		q += ` WHERE u.parent_id=$1`
		args = append(args, *parentID)
	}
	rows, err := r.q.Query(ctx, q+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (r *Repo) Create(ctx context.Context, u *User) error {
	err := r.q.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash, role, parent_id, package_id)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, status, created_at`,
		u.Username, u.Email, u.PasswordHash, u.Role, u.ParentID, u.PackageID,
	).Scan(&u.ID, &u.Status, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

// LockForUpdate serialises concurrent changes that depend on this account
// (e.g. a reseller's capacity checks).
func (r *Repo) LockForUpdate(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, id)
	return err
}

func (r *Repo) SetStatus(ctx context.Context, id int64, s Status) error {
	_, err := r.q.Exec(ctx, `UPDATE users SET status=$2, updated_at=now() WHERE id=$1`, id, s)
	return err
}

func (r *Repo) SetPackage(ctx context.Context, id int64, pkgID *int64) error {
	_, err := r.q.Exec(ctx, `UPDATE users SET package_id=$2, updated_at=now() WHERE id=$1`, id, pkgID)
	return err
}

func (r *Repo) SetLinuxUID(ctx context.Context, id int64, uid int) error {
	_, err := r.q.Exec(ctx, `UPDATE users SET linux_uid=$2, updated_at=now() WHERE id=$1`, id, uid)
	return err
}

func (r *Repo) SetPassword(ctx context.Context, id int64, hash string) error {
	_, err := r.q.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, hash)
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

func (r *Repo) CountChildren(ctx context.Context, id int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM users WHERE parent_id=$1`, id).Scan(&n)
	return n, err
}

// SetTOTP stores the sealed TOTP secret; nil disables two-factor login.
func (r *Repo) SetTOTP(ctx context.Context, id int64, sealed *string) error {
	_, err := r.q.Exec(ctx, `UPDATE users SET totp_secret=$2, updated_at=now() WHERE id=$1`, id, sealed)
	return err
}

// EnableTOTP atomically refuses replacement of an existing second factor.
func (r *Repo) EnableTOTP(ctx context.Context, id int64, sealed string) (bool, error) {
	tag, err := r.q.Exec(ctx, `UPDATE users SET totp_secret=$2, updated_at=now() WHERE id=$1 AND totp_secret IS NULL`, id, sealed)
	return tag.RowsAffected() == 1, err
}
