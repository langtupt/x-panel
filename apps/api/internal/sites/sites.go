// Package sites stores websites (one nginx vhost each) owned by hosting accounts.
package sites

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"xpanel/apps/api/internal/db"
	"xpanel/pkg/phpconfig"
)

type SSLStatus string

const (
	SSLNone    SSLStatus = "none"
	SSLPending SSLStatus = "pending"
	SSLActive  SSLStatus = "active"
	SSLFailed  SSLStatus = "failed"
)

type Site struct {
	PHPConfig    phpconfig.Site    `json:"php_config"`
	AppType      string            `json:"app_type"`
	Runtime      string            `json:"runtime"`
	Port         int               `json:"port"`
	Env          map[string]string `json:"env"`
	ID           int64             `json:"id"`
	UserID       int64             `json:"user_id"`
	Username     string            `json:"username"`
	Domain       string            `json:"domain"`
	Aliases      []string          `json:"aliases"`
	PHPVersion   string            `json:"php_version"`
	DocRoot      string            `json:"doc_root"`
	SSLStatus    SSLStatus         `json:"ssl_status"`
	SSLExpiresAt *time.Time        `json:"ssl_expires_at"`
	SSLType      string            `json:"ssl_type"` // letsencrypt | custom | selfsigned
	SSLIssuer    string            `json:"ssl_issuer"`
	CreatedAt    time.Time         `json:"created_at"`
}

var ErrNotFound = errors.New("site not found")

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

const selectCols = `SELECT s.id, s.user_id, u.username, s.domain, s.aliases, s.php_version, s.doc_root,
	s.ssl_status, s.ssl_expires_at, s.created_at, s.app_type, s.runtime, COALESCE(s.app_port,0), s.env, s.ssl_type, s.ssl_issuer, s.php_config FROM sites s JOIN users u ON u.id = s.user_id`

func scan(row pgx.Row) (*Site, error) {
	var s Site
	err := row.Scan(&s.ID, &s.UserID, &s.Username, &s.Domain, &s.Aliases, &s.PHPVersion, &s.DocRoot,
		&s.SSLStatus, &s.SSLExpiresAt, &s.CreatedAt, &s.AppType, &s.Runtime, &s.Port, &s.Env, &s.SSLType, &s.SSLIssuer, &s.PHPConfig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func collect(rows pgx.Rows, err error) ([]Site, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Repo) ByID(ctx context.Context, id int64) (*Site, error) {
	return scan(r.q.QueryRow(ctx, selectCols+` WHERE s.id=$1`, id))
}

func (r *Repo) ByUser(ctx context.Context, userID int64) ([]Site, error) {
	return collect(r.q.Query(ctx, selectCols+` WHERE s.user_id=$1 ORDER BY s.domain`, userID))
}

// Visible lists sites an account may see: all (admin), its customers' (reseller), its own (user).
func (r *Repo) Visible(ctx context.Context, userID int64, role string) ([]Site, error) {
	switch role {
	case "admin":
		return collect(r.q.Query(ctx, selectCols+` ORDER BY s.domain`))
	case "reseller":
		return collect(r.q.Query(ctx, selectCols+` WHERE u.parent_id=$1 ORDER BY s.domain`, userID))
	}
	return r.ByUser(ctx, userID)
}

func (r *Repo) CountByUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM sites WHERE user_id=$1`, userID).Scan(&n)
	return n, err
}

// NameTaken reports whether any of names is already a domain or alias of
// another site (excludeID is the site being edited, 0 for none).
func (r *Repo) NameTaken(ctx context.Context, names []string, excludeID int64) (string, error) {
	var taken string
	err := r.q.QueryRow(ctx, `SELECT n FROM unnest($1::text[]) n
		WHERE EXISTS (SELECT 1 FROM sites WHERE id<>$2 AND (domain = n OR n = ANY(aliases))) LIMIT 1`,
		names, excludeID).Scan(&taken)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return taken, err
}

func (r *Repo) Create(ctx context.Context, s *Site) error {
	return r.q.QueryRow(ctx, `INSERT INTO sites (user_id, domain, aliases, php_version, doc_root, app_type, runtime, app_port, env)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,0),$9) RETURNING id, ssl_status, created_at`,
		s.UserID, s.Domain, s.Aliases, s.PHPVersion, s.DocRoot, s.AppType, s.Runtime, s.Port, s.Env).Scan(&s.ID, &s.SSLStatus, &s.CreatedAt)
}

func (r *Repo) Update(ctx context.Context, s *Site) error {
	_, err := r.q.Exec(ctx, `UPDATE sites SET aliases=$2, php_version=$3, doc_root=$4, app_type=$5, runtime=$6, app_port=NULLIF($7,0), env=$8, updated_at=now() WHERE id=$1`,
		s.ID, s.Aliases, s.PHPVersion, s.DocRoot, s.AppType, s.Runtime, s.Port, s.Env)
	return err
}

func (r *Repo) SetSSL(ctx context.Context, id int64, st SSLStatus, expires *time.Time) error {
	_, err := r.q.Exec(ctx, `UPDATE sites SET ssl_status=$2, ssl_expires_at=$3,
		ssl_type = CASE WHEN $2 = 'none' THEN '' ELSE ssl_type END,
		ssl_issuer = CASE WHEN $2 = 'none' THEN '' ELSE ssl_issuer END, updated_at=now() WHERE id=$1`, id, st, expires)
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM sites WHERE id=$1`, id)
	return err
}

// ExpiringSSL returns active certificates expiring before t.
func (r *Repo) ExpiringSSL(ctx context.Context, before time.Time) ([]Site, error) {
	return collect(r.q.Query(ctx, selectCols+` WHERE s.ssl_status='active' AND s.ssl_type IN ('letsencrypt', '') AND s.ssl_expires_at < $1`, before))
}

// SetCertificate records an installed certificate and switches the site to HTTPS.
func (r *Repo) SetCertificate(ctx context.Context, id int64, typ, issuer string, expires time.Time) error {
	_, err := r.q.Exec(ctx, `UPDATE sites SET ssl_status='active', ssl_type=$2, ssl_issuer=$3, ssl_expires_at=$4,
		updated_at=now() WHERE id=$1`, id, typ, issuer, expires)
	return err
}

func (r *Repo) SetPHPConfig(ctx context.Context, id int64, c phpconfig.Site) error {
	_, err := r.q.Exec(ctx, `UPDATE sites SET php_config=$2, updated_at=now() WHERE id=$1`, id, c)
	return err
}
