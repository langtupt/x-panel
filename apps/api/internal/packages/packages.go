// Package packages holds resource plans. Admins own reseller and user
// packages; resellers own user packages for their customers.
package packages

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xpanel/apps/api/internal/db"
	"xpanel/apps/api/internal/users"
)

// Limits: 0 in any numeric field means unlimited.
type Limits struct {
	DiskMB       int      `json:"disk_mb"`
	Inodes       int      `json:"inodes"`
	BandwidthGB  int      `json:"bandwidth_gb"`
	MaxSites     int      `json:"max_sites"`
	MaxDBs       int      `json:"max_dbs"`
	MaxFTP       int      `json:"max_ftp"`
	MaxCron      int      `json:"max_cron"`
	MaxMailboxes int      `json:"max_mailboxes"`
	MaxUsers     int      `json:"max_users"` // reseller packages only
	CPUPct       int      `json:"cpu_pct"`
	MemMB        int      `json:"mem_mb"`
	PHPVersions  []string `json:"php_versions"`
}

type Package struct {
	ID       int64      `json:"id"`
	OwnerID  int64      `json:"owner_id"`
	Name     string     `json:"name"`
	Kind     users.Role `json:"kind"`
	Oversell bool       `json:"oversell"`
	Limits
	UsersCount int       `json:"users_count"`
	CreatedAt  time.Time `json:"created_at"`
}

var (
	ErrNotFound = errors.New("package not found")
	ErrTaken    = errors.New("tên gói đã tồn tại")
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const cols = `p.id, p.owner_id, p.name, p.kind, p.oversell, p.disk_mb, p.inodes, p.bandwidth_gb, p.max_sites,
	p.max_dbs, p.max_ftp, p.max_cron, p.max_users, p.cpu_pct, p.mem_mb, p.php_versions, p.created_at, p.max_mailboxes,
	(SELECT count(*) FROM users u WHERE u.package_id = p.id)`

func scan(row pgx.Row) (*Package, error) {
	var p Package
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Kind, &p.Oversell, &p.DiskMB, &p.Inodes, &p.BandwidthGB,
		&p.MaxSites, &p.MaxDBs, &p.MaxFTP, &p.MaxCron, &p.MaxUsers, &p.CPUPct, &p.MemMB, &p.PHPVersions,
		&p.CreatedAt, &p.MaxMailboxes, &p.UsersCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (r *Repo) ByID(ctx context.Context, id int64) (*Package, error) {
	return scan(r.q.QueryRow(ctx, `SELECT `+cols+` FROM packages p WHERE p.id=$1`, id))
}

func (r *Repo) ListByOwner(ctx context.Context, ownerID int64) ([]Package, error) {
	rows, err := r.q.Query(ctx, `SELECT `+cols+` FROM packages p WHERE p.owner_id=$1 ORDER BY p.kind, p.name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Package{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

func (r *Repo) Create(ctx context.Context, p *Package) error {
	err := r.q.QueryRow(ctx, `INSERT INTO packages (owner_id, name, kind, oversell, disk_mb, inodes, bandwidth_gb,
		max_sites, max_dbs, max_ftp, max_cron, max_users, cpu_pct, mem_mb, php_versions, max_mailboxes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id, created_at`,
		p.OwnerID, p.Name, p.Kind, p.Oversell, p.DiskMB, p.Inodes, p.BandwidthGB, p.MaxSites, p.MaxDBs,
		p.MaxFTP, p.MaxCron, p.MaxUsers, p.CPUPct, p.MemMB, p.PHPVersions, p.MaxMailboxes,
	).Scan(&p.ID, &p.CreatedAt)
	return mapErr(err)
}

func (r *Repo) Update(ctx context.Context, p *Package) error {
	_, err := r.q.Exec(ctx, `UPDATE packages SET name=$2, oversell=$3, disk_mb=$4, inodes=$5, bandwidth_gb=$6,
		max_sites=$7, max_dbs=$8, max_ftp=$9, max_cron=$10, max_users=$11, cpu_pct=$12, mem_mb=$13, php_versions=$14,
		max_mailboxes=$15 WHERE id=$1`,
		p.ID, p.Name, p.Oversell, p.DiskMB, p.Inodes, p.BandwidthGB, p.MaxSites, p.MaxDBs, p.MaxFTP,
		p.MaxCron, p.MaxUsers, p.CPUPct, p.MemMB, p.PHPVersions, p.MaxMailboxes)
	return mapErr(err)
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM packages WHERE id=$1`, id)
	return err
}

// Allocation sums what a reseller has handed out to its users, skipping excludeUserID
// (pass 0 to include everyone) and users on excludePkgID.
func (r *Repo) Allocation(ctx context.Context, resellerID, excludeUserID, excludePkgID int64) (count, diskMB int, err error) {
	err = r.q.QueryRow(ctx, `SELECT count(*), COALESCE(sum(p.disk_mb), 0)
		FROM users u LEFT JOIN packages p ON p.id = u.package_id
		WHERE u.parent_id=$1 AND u.id<>$2 AND ($3::bigint IS NULL OR u.package_id IS DISTINCT FROM $3)`,
		resellerID, excludeUserID, nullID(excludePkgID)).Scan(&count, &diskMB)
	return
}

// Usernames returns the accounts currently on a package.
func (r *Repo) Usernames(ctx context.Context, pkgID int64) ([]string, error) {
	rows, err := r.q.Query(ctx, `SELECT username FROM users WHERE package_id=$1 ORDER BY id`, pkgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
