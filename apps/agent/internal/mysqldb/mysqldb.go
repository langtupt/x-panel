// Package mysqldb manages customer databases on the local MariaDB server.
// SQL goes to the `mariadb` client over stdin as root (unix_socket auth);
// every name and password is validated against a strict charset first, so
// identifiers and literals cannot break out of their quoting.
package mysqldb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// Hosts each customer DB user is created for: socket and TCP to localhost.
var hosts = []string{"localhost", "127.0.0.1"}

func query(ctx context.Context, sql string) (string, error) {
	return run.With(ctx, run.Opts{Stdin: sql}, "mariadb", "--batch", "--skip-column-names")
}

func ident(name string) string { return "`" + name + "`" }

// grantIdent escapes `_`, which is a wildcard in database-level GRANTs.
func grantIdent(name string) string { return "`" + strings.ReplaceAll(name, "_", `\_`) + "`" }

func str(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func account(user, host string) string { return str(user) + "@" + str(host) }

func checkNames(p agentproto.DBParams) error {
	if err := validate.DBName(p.Owner, p.Name); err != nil {
		return err
	}
	return validate.DBName(p.Owner, p.User)
}

func count(ctx context.Context, sql string) (int, error) {
	out, err := query(ctx, sql)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// Create makes the database and its user. It refuses to touch a database or
// user that already exists, so cleanup on failure only removes what it made.
func Create(ctx context.Context, p agentproto.DBParams) error {
	if err := checkNames(p); err != nil {
		return err
	}
	if err := validate.DBPassword(p.Password); err != nil {
		return err
	}
	n, err := count(ctx, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = "+str(p.Name))
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("database %s already exists", p.Name)
	}
	if n, err = count(ctx, "SELECT COUNT(*) FROM mysql.global_priv WHERE User = "+str(p.User)); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("database user %s already exists", p.User)
	}

	var sql strings.Builder
	fmt.Fprintf(&sql, "CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n", ident(p.Name))
	for _, h := range hosts {
		fmt.Fprintf(&sql, "CREATE USER %s IDENTIFIED BY %s;\n", account(p.User, h), str(p.Password))
		fmt.Fprintf(&sql, "GRANT ALL PRIVILEGES ON %s.* TO %s;\n", grantIdent(p.Name), account(p.User, h))
	}
	if _, err := query(ctx, sql.String()); err != nil {
		_, _ = query(context.WithoutCancel(ctx), dropSQL(p))
		return err
	}
	return nil
}

func dropSQL(p agentproto.DBParams) string {
	var sql strings.Builder
	fmt.Fprintf(&sql, "DROP DATABASE IF EXISTS %s;\n", ident(p.Name))
	for _, h := range hosts {
		fmt.Fprintf(&sql, "DROP USER IF EXISTS %s;\n", account(p.User, h))
	}
	return sql.String()
}

// Delete drops the database and its user (missing ones are ignored).
func Delete(ctx context.Context, p agentproto.DBParams) error {
	if err := checkNames(p); err != nil {
		return err
	}
	_, err := query(ctx, dropSQL(p))
	return err
}

func SetPassword(ctx context.Context, p agentproto.DBParams) error {
	if err := validate.DBName(p.Owner, p.User); err != nil {
		return err
	}
	if err := validate.DBPassword(p.Password); err != nil {
		return err
	}
	var sql strings.Builder
	for _, h := range hosts {
		fmt.Fprintf(&sql, "ALTER USER %s IDENTIFIED BY %s;\n", account(p.User, h), str(p.Password))
	}
	_, err := query(ctx, sql.String())
	return err
}

// Lock blocks (or re-allows) logins of a suspended account's DB users.
func Lock(ctx context.Context, p agentproto.DBLockParams) error {
	if len(p.Users) == 0 {
		return nil
	}
	action := "ACCOUNT UNLOCK"
	if p.Locked {
		action = "ACCOUNT LOCK"
	}
	var sql strings.Builder
	for _, u := range p.Users {
		if err := validate.DBName(p.Owner, u); err != nil {
			return err
		}
		for _, h := range hosts {
			fmt.Fprintf(&sql, "ALTER USER IF EXISTS %s %s;\n", account(u, h), action)
		}
	}
	_, err := query(ctx, sql.String())
	return err
}

// Sizes returns the on-disk size of every database in bytes.
func Sizes(ctx context.Context) (agentproto.DBSizes, error) {
	res := agentproto.DBSizes{Sizes: map[string]int64{}}
	out, err := query(ctx, `SELECT table_schema, COALESCE(SUM(data_length + index_length), 0)
		FROM information_schema.tables GROUP BY table_schema;`)
	if err != nil {
		return res, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, size, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
		if err != nil {
			return res, errors.New("unexpected size output: " + line)
		}
		res.Sizes[name] = n
	}
	return res, nil
}
