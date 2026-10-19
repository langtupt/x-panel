// Package mailrepo stores hosted mail domains, mailboxes and forwarders.
package mailrepo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xpanel/apps/api/internal/db"
)

type Domain struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Domain    string    `json:"domain"`
	DKIMName  string    `json:"dkim_name"`
	DKIMValue string    `json:"dkim_value"`
	Mailboxes int       `json:"mailboxes"`
	Aliases   int       `json:"aliases"`
	CreatedAt time.Time `json:"created_at"`
}

type Mailbox struct {
	ID           int64     `json:"id"`
	DomainID     int64     `json:"domain_id"`
	Address      string    `json:"address"`
	LocalPart    string    `json:"local_part"`
	PasswordHash string    `json:"-"`
	QuotaMB      int       `json:"quota_mb"`
	CreatedAt    time.Time `json:"created_at"`
}

type Alias struct {
	ID           int64     `json:"id"`
	DomainID     int64     `json:"domain_id"`
	Address      string    `json:"address"`
	LocalPart    string    `json:"local_part"`
	Destinations []string  `json:"destinations"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	ErrNotFound = errors.New("not found")
	ErrTaken    = errors.New("địa chỉ đã tồn tại")
)

type Repo struct {
	q db.Querier
}

func NewRepo(q db.Querier) *Repo       { return &Repo{q: q} }
func (r *Repo) WithTx(tx pgx.Tx) *Repo { return &Repo{q: tx} }

func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTaken
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

const domainCols = `SELECT d.id, d.user_id, u.username, d.domain, d.dkim_name, d.dkim_value,
	(SELECT count(*) FROM mailboxes m WHERE m.domain_id=d.id), (SELECT count(*) FROM mail_aliases a WHERE a.domain_id=d.id),
	d.created_at FROM mail_domains d JOIN users u ON u.id=d.user_id`

func scanDomain(row pgx.Row) (*Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.UserID, &d.Username, &d.Domain, &d.DKIMName, &d.DKIMValue, &d.Mailboxes, &d.Aliases, &d.CreatedAt)
	return &d, mapErr(err)
}

func (r *Repo) Domain(ctx context.Context, id int64) (*Domain, error) {
	return scanDomain(r.q.QueryRow(ctx, domainCols+` WHERE d.id=$1`, id))
}

// Domains lists what the actor may see: everything (admin), its customers'
// (reseller) or its own.
func (r *Repo) Domains(ctx context.Context, userID int64, role string) ([]Domain, error) {
	var rows pgx.Rows
	var err error
	switch role {
	case "admin":
		rows, err = r.q.Query(ctx, domainCols+` ORDER BY d.domain`)
	case "reseller":
		rows, err = r.q.Query(ctx, domainCols+` WHERE u.parent_id=$1 OR u.id=$1 ORDER BY d.domain`, userID)
	default:
		rows, err = r.q.Query(ctx, domainCols+` WHERE d.user_id=$1 ORDER BY d.domain`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *Repo) AllDomains(ctx context.Context) ([]Domain, error) {
	return r.Domains(ctx, 0, "admin")
}

func (r *Repo) CreateDomain(ctx context.Context, d *Domain) error {
	return mapErr(r.q.QueryRow(ctx, `INSERT INTO mail_domains (user_id, domain) VALUES ($1,$2) RETURNING id, created_at`,
		d.UserID, d.Domain).Scan(&d.ID, &d.CreatedAt))
}

func (r *Repo) DeleteDomain(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM mail_domains WHERE id=$1`, id)
	return err
}

func (r *Repo) SetDKIM(ctx context.Context, domain, name, value string) error {
	_, err := r.q.Exec(ctx, `UPDATE mail_domains SET dkim_name=$2, dkim_value=$3 WHERE domain=$1
		AND (dkim_name<>$2 OR dkim_value<>$3)`, domain, name, value)
	return err
}

func (r *Repo) DomainsByUser(ctx context.Context, userID int64) ([]Domain, error) {
	return r.Domains(ctx, userID, "user")
}

// CountMailboxes counts every mailbox of an account (package limit).
func (r *Repo) CountMailboxes(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `SELECT count(*) FROM mailboxes m JOIN mail_domains d ON d.id=m.domain_id
		WHERE d.user_id=$1`, userID).Scan(&n)
	return n, err
}

const boxCols = `SELECT m.id, m.domain_id, m.local_part || '@' || d.domain, m.local_part, m.password_hash, m.quota_mb,
	m.created_at FROM mailboxes m JOIN mail_domains d ON d.id=m.domain_id`

func scanBox(row pgx.Row) (*Mailbox, error) {
	var m Mailbox
	err := row.Scan(&m.ID, &m.DomainID, &m.Address, &m.LocalPart, &m.PasswordHash, &m.QuotaMB, &m.CreatedAt)
	return &m, mapErr(err)
}

func (r *Repo) Mailbox(ctx context.Context, id int64) (*Mailbox, error) {
	return scanBox(r.q.QueryRow(ctx, boxCols+` WHERE m.id=$1`, id))
}

func (r *Repo) Mailboxes(ctx context.Context, domainID int64) ([]Mailbox, error) {
	return collectBoxes(r.q.Query(ctx, boxCols+` WHERE m.domain_id=$1 ORDER BY m.local_part`, domainID))
}

// MailboxesForSync returns every mailbox with whether its owner is suspended.
func (r *Repo) MailboxesForSync(ctx context.Context) ([]Mailbox, map[int64]bool, error) {
	rows, err := r.q.Query(ctx, `SELECT m.id, m.domain_id, m.local_part || '@' || d.domain, m.local_part, m.password_hash,
		m.quota_mb, m.created_at, u.status='suspended' FROM mailboxes m JOIN mail_domains d ON d.id=m.domain_id
		JOIN users u ON u.id=d.user_id ORDER BY 3`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Mailbox
	suspended := map[int64]bool{}
	for rows.Next() {
		var m Mailbox
		var s bool
		if err := rows.Scan(&m.ID, &m.DomainID, &m.Address, &m.LocalPart, &m.PasswordHash, &m.QuotaMB, &m.CreatedAt, &s); err != nil {
			return nil, nil, err
		}
		suspended[m.ID] = s
		out = append(out, m)
	}
	return out, suspended, rows.Err()
}

func collectBoxes(rows pgx.Rows, err error) ([]Mailbox, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		m, err := scanBox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// LocalPartTaken reports whether a mailbox or alias already uses the name.
func (r *Repo) LocalPartTaken(ctx context.Context, domainID int64, local string) (bool, error) {
	var taken bool
	err := r.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mailboxes WHERE domain_id=$1 AND local_part=$2)
		OR EXISTS (SELECT 1 FROM mail_aliases WHERE domain_id=$1 AND local_part=$2)`, domainID, local).Scan(&taken)
	return taken, err
}

func (r *Repo) CreateMailbox(ctx context.Context, m *Mailbox) error {
	return mapErr(r.q.QueryRow(ctx, `INSERT INTO mailboxes (domain_id, local_part, password_hash, quota_mb)
		VALUES ($1,$2,$3,$4) RETURNING id, created_at`, m.DomainID, m.LocalPart, m.PasswordHash, m.QuotaMB).Scan(&m.ID, &m.CreatedAt))
}

func (r *Repo) UpdateMailbox(ctx context.Context, m *Mailbox) error {
	_, err := r.q.Exec(ctx, `UPDATE mailboxes SET password_hash=$2, quota_mb=$3 WHERE id=$1`, m.ID, m.PasswordHash, m.QuotaMB)
	return err
}

func (r *Repo) DeleteMailbox(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, id)
	return err
}

const aliasCols = `SELECT a.id, a.domain_id, a.local_part || '@' || d.domain, a.local_part, a.destinations, a.created_at
	FROM mail_aliases a JOIN mail_domains d ON d.id=a.domain_id`

func scanAlias(row pgx.Row) (*Alias, error) {
	var a Alias
	err := row.Scan(&a.ID, &a.DomainID, &a.Address, &a.LocalPart, &a.Destinations, &a.CreatedAt)
	return &a, mapErr(err)
}

func (r *Repo) Alias(ctx context.Context, id int64) (*Alias, error) {
	return scanAlias(r.q.QueryRow(ctx, aliasCols+` WHERE a.id=$1`, id))
}

func (r *Repo) Aliases(ctx context.Context, domainID int64) ([]Alias, error) {
	q := aliasCols + ` WHERE a.domain_id=$1 ORDER BY a.local_part`
	args := []any{domainID}
	if domainID == 0 {
		q, args = aliasCols+` ORDER BY 3`, nil
	}
	rows, err := r.q.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alias{}
	for rows.Next() {
		a, err := scanAlias(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repo) CreateAlias(ctx context.Context, a *Alias) error {
	return mapErr(r.q.QueryRow(ctx, `INSERT INTO mail_aliases (domain_id, local_part, destinations) VALUES ($1,$2,$3)
		RETURNING id, created_at`, a.DomainID, a.LocalPart, a.Destinations).Scan(&a.ID, &a.CreatedAt))
}

func (r *Repo) DeleteAlias(ctx context.Context, id int64) error {
	_, err := r.q.Exec(ctx, `DELETE FROM mail_aliases WHERE id=$1`, id)
	return err
}

// SiteOwnedByOther reports whether another account hosts a website on the domain.
func (r *Repo) SiteOwnedByOther(ctx context.Context, domain string, userID int64) (bool, error) {
	var taken bool
	err := r.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sites WHERE user_id<>$2 AND (domain=$1 OR $1=ANY(aliases)))`,
		domain, userID).Scan(&taken)
	return taken, err
}
