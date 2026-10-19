package apps

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/fetch"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/run"
)

// Customer databases live in their own cluster, never in the panel's.
const (
	PGCluster = "customers"
	PGPort    = 5433
)

var aptOpts = run.Opts{Env: []string{"DEBIAN_FRONTEND=noninteractive"}, Timeout: 20 * time.Minute}

// postgresInstalled returns the major version of the customer cluster, if any.
func postgresInstalled() string {
	out, err := run.Output(context.Background(), "pg_lsclusters", "--no-header")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == PGCluster {
			return f[0]
		}
	}
	return ""
}

// PGVersion is the customer cluster's major version ("" if not installed).
func PGVersion() string { return postgresInstalled() }

type pgVersion struct {
	Major     string `json:"major"`
	Supported bool   `json:"supported"`
}

// pgAvailable lists supported PostgreSQL majors, newest first.
func pgAvailable(ctx context.Context) ([]string, error) {
	var all []pgVersion
	if err := fetch.JSON(ctx, "https://www.postgresql.org/versions.json", &all); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range all {
		if n, err := strconv.Atoi(v.Major); err == nil && v.Supported && n >= 14 {
			out = append(out, v.Major)
		}
	}
	sortVersionsDesc(out)
	return out, nil
}

func installPostgres(ctx context.Context, d distro.Distro, major string, logf func(string, ...any)) error {
	if _, err := strconv.Atoi(major); err != nil {
		return fmt.Errorf("invalid PostgreSQL version %q", major)
	}
	if cur := postgresInstalled(); cur != "" {
		return fmt.Errorf("đã có PostgreSQL %s cho khách hàng (cổng %d); nâng cấp phiên bản chưa được hỗ trợ", cur, PGPort)
	}
	step := func(name string, args ...string) error {
		if name == "apt-get" {
			args = append([]string{"-o", "DPkg::Lock::Timeout=600"}, args...)
		}
		logf("$ %s %s", name, strings.Join(args, " "))
		out, err := run.With(ctx, aptOpts, name, args...)
		logf("%s", lastLines(out, 8))
		return err
	}

	// Official PGDG repository (script shipped by postgresql-common).
	if _, err := os.Stat("/etc/apt/sources.list.d/pgdg.list"); err != nil {
		if err := step("/usr/share/postgresql-common/pgdg/apt.postgresql.org.sh", "-y"); err != nil {
			return err
		}
	}
	// Do not let the package create a "main" cluster; we create our own.
	const conf = "/etc/postgresql-common/createcluster.conf"
	if b, err := os.ReadFile(conf); err == nil && !strings.Contains(string(b), "\ncreate_main_cluster = false") {
		if err := os.WriteFile(conf, append(b, []byte("\n# X-Panel\ncreate_main_cluster = false\n")...), 0o644); err != nil {
			return err
		}
	}
	if err := step("apt-get", "install", "-y", "-q", "postgresql-"+major); err != nil {
		return err
	}
	if err := step("pg_createcluster", major, PGCluster, "--port", strconv.Itoa(PGPort), "--start",
		"--", "--auth-local=peer", "--auth-host=scram-sha-256"); err != nil {
		return err
	}
	if err := step("systemctl", "enable", fmt.Sprintf("postgresql@%s-%s", major, PGCluster)); err != nil {
		return err
	}
	// PHP sites need the pgsql extension to talk to it.
	if err := installPHPPgsql(ctx, d, php.Installed(d), logf); err != nil {
		return err
	}
	logf("PostgreSQL %s sẵn sàng: 127.0.0.1:%d (chỉ nghe localhost)", major, PGPort)
	return nil
}

func installPHPPgsql(ctx context.Context, d distro.Distro, versions []string, logf func(string, ...any)) error {
	if len(versions) == 0 {
		return nil
	}
	pkgs := []string{"-o", "DPkg::Lock::Timeout=600", "install", "-y", "-q"}
	for _, v := range versions {
		pkgs = append(pkgs, "php"+v+"-pgsql")
	}
	logf("$ apt-get %s", strings.Join(pkgs, " "))
	out, err := run.With(ctx, aptOpts, "apt-get", pkgs...)
	logf("%s", lastLines(out, 4))
	if err != nil {
		return err
	}
	for _, v := range versions {
		_ = run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(v))
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
