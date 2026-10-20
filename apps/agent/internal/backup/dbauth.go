package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/apps"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

// db-auth.json keeps the password hashes of the account's database users,
// so a restore on another server gives them their old passwords back and
// the websites' configuration keeps working. Only hashes are stored, and
// the whole archive is encrypted.
const dbAuthName = "db-auth.json"

var (
	mariaHashRe = regexp.MustCompile(`^\*[0-9A-F]{40}$`)
	pgHashRe    = regexp.MustCompile(`^(SCRAM-SHA-256\$[0-9]+:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+|md5[0-9a-f]{32})$`)
)

func dbAuthKey(engine, user string) string { return engine + ":" + user }

// dbAuth reads the password hash of each database's owning user.
func dbAuth(ctx context.Context, dbs []agentproto.BackupDB) map[string]string {
	out := map[string]string{}
	for _, d := range dbs {
		key := dbAuthKey(d.Engine, d.User)
		if d.User == "" || out[key] != "" {
			continue
		}
		var hash string
		if d.Engine == "postgresql" {
			res, err := run.With(ctx, run.Opts{Stdin: "SELECT rolpassword FROM pg_authid WHERE rolname = '" + d.User + "';"},
				"runuser", "-u", "postgres", "--", "psql", "-p", strconv.Itoa(apps.PGPort), "-X", "-q", "-t", "-A")
			if err == nil {
				hash = strings.TrimSpace(res)
			}
			if !pgHashRe.MatchString(hash) {
				continue
			}
		} else {
			res, err := run.With(ctx, run.Opts{Stdin: "SELECT JSON_VALUE(Priv, '$.authentication_string') FROM mysql.global_priv WHERE User = '" +
				d.User + "' AND Host = 'localhost';"}, "mariadb", "--batch", "--skip-column-names")
			if err == nil {
				hash = strings.TrimSpace(res)
			}
			if !mariaHashRe.MatchString(hash) {
				continue
			}
		}
		out[key] = hash
	}
	return out
}

func parseDBAuth(b []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(b, &m)
	return m
}

// applyDBAuth gives a restored database's user its password from the backup.
func applyDBAuth(ctx context.Context, d agentproto.BackupDB, auth map[string]string) error {
	hash := auth[dbAuthKey(d.Engine, d.User)]
	if hash == "" || d.User == "" {
		return nil
	}
	if d.Engine == "postgresql" {
		if !pgHashRe.MatchString(hash) {
			return fmt.Errorf("invalid password hash for %s", d.User)
		}
		_, err := run.With(ctx, run.Opts{Stdin: `ALTER ROLE "` + d.User + `" PASSWORD '` + hash + `';`},
			"runuser", "-u", "postgres", "--", "psql", "-p", strconv.Itoa(apps.PGPort), "-X", "-q", "-v", "ON_ERROR_STOP=1")
		return err
	}
	if !mariaHashRe.MatchString(hash) {
		return fmt.Errorf("invalid password hash for %s", d.User)
	}
	var sql string
	for _, host := range []string{"localhost", "127.0.0.1"} {
		sql += "ALTER USER IF EXISTS '" + d.User + "'@'" + host + "' IDENTIFIED BY PASSWORD '" + hash + "';"
	}
	_, err := run.With(ctx, run.Opts{Stdin: sql}, "mariadb", "--batch")
	return err
}
