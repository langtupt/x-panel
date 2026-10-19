// Package pgdb manages customer databases in the dedicated PostgreSQL
// cluster ("customers", port 5433), never in the panel's own cluster.
// SQL is fed to psql over stdin as the postgres OS user; names and
// passwords are validated against strict charsets first.
package pgdb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/apps"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

var ErrNotInstalled = errors.New("PostgreSQL cho khách hàng chưa được cài (trang Ứng dụng)")

func query(ctx context.Context, sql string) (string, error) {
	if apps.PGVersion() == "" {
		return "", ErrNotInstalled
	}
	return run.With(ctx, run.Opts{Stdin: sql}, "runuser", "-u", "postgres", "--",
		"psql", "-p", strconv.Itoa(apps.PGPort), "-X", "-q", "-At", "-v", "ON_ERROR_STOP=1", "-d", "postgres")
}

func ident(s string) string { return `"` + s + `"` }

func lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func checkNames(p agentproto.DBParams) error {
	if err := validate.DBName(p.Owner, p.Name); err != nil {
		return err
	}
	return validate.DBName(p.Owner, p.User)
}

func exists(ctx context.Context, sql string) (bool, error) {
	out, err := query(ctx, sql)
	return strings.TrimSpace(out) == "1", err
}

// Create makes a login role and a database owned by it. Other roles lose the
// default CONNECT right, so customers cannot open each other's databases.
func Create(ctx context.Context, p agentproto.DBParams) error {
	if err := checkNames(p); err != nil {
		return err
	}
	if err := validate.DBPassword(p.Password); err != nil {
		return err
	}
	if ok, err := exists(ctx, "SELECT 1 FROM pg_database WHERE datname = "+lit(p.Name)); err != nil || ok {
		if ok {
			err = fmt.Errorf("database %s already exists", p.Name)
		}
		return err
	}
	if ok, err := exists(ctx, "SELECT 1 FROM pg_roles WHERE rolname = "+lit(p.User)); err != nil || ok {
		if ok {
			err = fmt.Errorf("role %s already exists", p.User)
		}
		return err
	}
	role := fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;",
		ident(p.User), lit(p.Password))
	if _, err := query(ctx, role); err != nil {
		return err
	}
	// CREATE DATABASE cannot run inside a transaction block: separate statements.
	steps := []string{
		fmt.Sprintf("CREATE DATABASE %s OWNER %s ENCODING 'UTF8' TEMPLATE template0;", ident(p.Name), ident(p.User)),
		fmt.Sprintf("REVOKE ALL ON DATABASE %s FROM PUBLIC;", ident(p.Name)),
		fmt.Sprintf("GRANT ALL ON DATABASE %s TO %s;", ident(p.Name), ident(p.User)),
	}
	for _, s := range steps {
		if _, err := query(ctx, s); err != nil {
			_, _ = query(context.WithoutCancel(ctx), dropSQL(p))
			return err
		}
	}
	return nil
}

func dropSQL(p agentproto.DBParams) string {
	return fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);\nDROP ROLE IF EXISTS %s;\n", ident(p.Name), ident(p.User))
}

func Delete(ctx context.Context, p agentproto.DBParams) error {
	if err := checkNames(p); err != nil {
		return err
	}
	_, err := query(ctx, dropSQL(p))
	if errors.Is(err, ErrNotInstalled) {
		return nil
	}
	return err
}

func SetPassword(ctx context.Context, p agentproto.DBParams) error {
	if err := validate.DBName(p.Owner, p.User); err != nil {
		return err
	}
	if err := validate.DBPassword(p.Password); err != nil {
		return err
	}
	_, err := query(ctx, fmt.Sprintf("ALTER ROLE %s PASSWORD %s;", ident(p.User), lit(p.Password)))
	return err
}

// Lock disables (or re-enables) logins and ends open sessions of the roles.
func Lock(ctx context.Context, p agentproto.DBLockParams) error {
	if len(p.Users) == 0 {
		return nil
	}
	var sql strings.Builder
	for _, u := range p.Users {
		if err := validate.DBName(p.Owner, u); err != nil {
			return err
		}
		if p.Locked {
			fmt.Fprintf(&sql, "ALTER ROLE %s NOLOGIN;\nSELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = %s;\n", ident(u), lit(u))
		} else {
			fmt.Fprintf(&sql, "ALTER ROLE %s LOGIN;\n", ident(u))
		}
	}
	_, err := query(ctx, sql.String())
	return err
}

// Sizes returns the size of every database in the customer cluster.
func Sizes(ctx context.Context) (map[string]int64, error) {
	out, err := query(ctx, "SELECT datname || '|' || pg_database_size(datname) FROM pg_database WHERE NOT datistemplate;")
	if err != nil {
		return nil, err
	}
	sizes := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, n, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		if v, err := strconv.ParseInt(n, 10, 64); err == nil {
			sizes[name] = v
		}
	}
	return sizes, nil
}
