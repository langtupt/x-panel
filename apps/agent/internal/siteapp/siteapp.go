// Package siteapp manages customer processes. All source-code commands run as
// the customer inside the same cgroup slice as their PHP processes.
package siteapp

import (
	"context"
	"crypto/sha256"

	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/apps"
	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/netguard"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/phpconfig"
	"xpanel/pkg/validate"
)

const ConfigRoot = "/etc/xpanel-site-apps"

var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var versionRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

func Unit(user, domain string) string {
	h := sha256.Sum256([]byte(domain))
	return fmt.Sprintf("xpanel-app-%s-%x.service", user, h[:8])
}
func Socket(user, domain string) string {
	return "/run/" + strings.TrimSuffix(Unit(user, domain), ".service") + "/fpm.sock"
}
func stopUnit(ctx context.Context, unit string) error {
	err := run.Cmd(ctx, "systemctl", "disable", "--now", unit)
	if err != nil {
		if active := run.Cmd(ctx, "systemctl", "is-active", "--quiet", unit); active == nil {
			return err
		}
	}
	return nil
}
func Managed(t string) bool { return Dynamic(t) || t == "php" || t == "laravel" }
func ready(ctx context.Context, user string, site agentproto.WebSite) error {
	network, address := "tcp", fmt.Sprintf("127.0.0.1:%d", site.Port)
	if site.AppType == "php" || site.AppType == "laravel" {
		network, address = "unix", Socket(user, site.Domain)
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		conn, err := net.DialTimeout(network, address, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("ứng dụng chưa mở %s; kiểm tra Logs", address)
		case <-tick.C:
		}
	}
}
func Dynamic(t string) bool { return t == "nodejs" || t == "nextjs" || t == "go" }
func Validate(s agentproto.WebSite) error {
	if err := s.PHPConfig.Validate(); err != nil {
		return err
	}
	if err := validate.Domain(s.Domain); err != nil {
		return err
	}
	if err := validate.DocRoot(s.DocRoot); err != nil {
		return err
	}
	switch s.AppType {
	case "", "html", "php", "laravel", "nodejs", "nextjs", "go":
	default:
		return fmt.Errorf("invalid application type")
	}
	if s.Runtime != "" && !versionRE.MatchString(s.Runtime) {
		return fmt.Errorf("invalid runtime version")
	}
	if (s.AppType == "php" || s.AppType == "laravel") && !agentproto.ValidPHPVersion(s.PHPVersion) {
		return fmt.Errorf("invalid PHP version")
	}
	if Dynamic(s.AppType) && (s.Port < 10000 || s.Port > 32767) {
		return fmt.Errorf("invalid application port")
	}
	if len(s.Env) > 100 {
		return fmt.Errorf("too many environment variables")
	}
	for k, v := range s.Env {
		if !nameRE.MatchString(k) || strings.ContainsAny(v, "\x00\r\n") || len(v) > 8192 {
			return fmt.Errorf("invalid environment variable")
		}
		if slices.Contains([]string{"PORT", "HOST", "PATH", "HOME"}, k) {
			return fmt.Errorf("reserved environment variable %s", k)
		}
	}
	return nil
}
func root(p agentproto.SiteAppParams) (string, error) {
	if err := Validate(p.Site); err != nil {
		return "", err
	}
	u, err := accounts.Managed(p.Username)
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, "www", p.Site.Domain), nil
}

// unitValue escapes systemd specifiers, environment expansion and quoting.
func unitValue(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(v) + `"`
}
func runtimePath(s agentproto.WebSite) (string, error) {
	var installed []string
	base := ""
	if s.AppType == "nodejs" || s.AppType == "nextjs" {
		installed = apps.NodeInstalled()
		base = apps.NodeRoot
	}
	if s.AppType == "go" {
		installed = apps.GoInstalled()
		base = apps.GoRoot
	}
	if base == "" {
		return "/usr/local/bin:/usr/bin:/bin", nil
	}
	v := s.Runtime
	if v == "" && len(installed) > 0 {
		v = installed[0]
	}
	if !slices.Contains(installed, v) {
		return "", fmt.Errorf("runtime chưa cài; cài từ Applications trước")
	}
	return filepath.Join(base, v, "bin") + ":/usr/local/bin:/usr/bin:/bin", nil
}
func environment(s agentproto.WebSite, home, path string) string {
	env := map[string]string{"HOME": home, "PATH": path, "PORT": strconv.Itoa(s.Port), "HOST": "127.0.0.1", "NODE_ENV": "production", "XDG_CACHE_HOME": filepath.Join(home, "tmp/.cache"), "npm_config_cache": filepath.Join(home, "tmp/.npm"), "GOPATH": filepath.Join(home, "tmp/go"), "GOCACHE": filepath.Join(home, "tmp/go-build"), "COMPOSER_HOME": filepath.Join(home, "tmp/composer"), "COMPOSER_CACHE_DIR": filepath.Join(home, "tmp/composer-cache")}
	for k, v := range s.Env {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=\"%s\"\n", k, strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(env[k]))
	}
	return b.String()
}
func command(s agentproto.WebSite) string {
	switch s.AppType {
	case "nextjs":
		return "/usr/bin/env npm run start -- --hostname 127.0.0.1 --port " + strconv.Itoa(s.Port)
	case "nodejs":
		return "/usr/bin/env npm run start"
	case "go":
		return "/usr/bin/env ./.xpanel-app"
	}
	return ""
}

// Sync starts/stops only root-owned units from this account's state directory.
func Sync(ctx context.Context, distroForPHP distro.Distro, p agentproto.WebSyncParams) error {
	u, err := accounts.Managed(p.Username)
	if err != nil {
		return err
	}
	dir := filepath.Join(ConfigRoot, p.Username)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.Chmod(ConfigRoot, 0755); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0755); err != nil {
		return err
	}
	want := map[string]bool{}
	poolCount := agentproto.PHPPoolCount(p.Sites)
	for _, site := range p.Sites {
		if err := Validate(site); err != nil {
			return err
		}
		if !Managed(site.AppType) {
			continue
		}
		unit := Unit(p.Username, site.Domain)
		want[unit] = true
		if p.Suspended {
			if err := run.Cmd(ctx, "systemctl", "disable", "--now", unit); err != nil {
				if _, e := os.Stat(filepath.Join(dir, unit)); e == nil {
					return err
				}
			}
			continue
		}
		path, err := runtimePath(site)
		if err != nil {
			return err
		}
		appRoot := filepath.Join(u.HomeDir, "www", site.Domain)
		envPath := filepath.Join(dir, unit+".env")
		envData := environment(site, u.HomeDir, path)
		oldEnv, _ := os.ReadFile(envPath)
		fpmChanged := false // PHP-FPM pool settings (e.g. max_children from the package RAM)
		if err := os.WriteFile(envPath, []byte(envData), 0600); err != nil {
			return err
		}
		envDirective := "EnvironmentFile=" + envPath + "\n"
		content := fmt.Sprintf("[Unit]\nDescription=X-Panel app %s\nAfter=network.target\n[Service]\nType=simple\nUser=%s\nGroup=%s\nSlice=xpanel-%s.slice\nWorkingDirectory=%s\n%sExecStart=%s\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\nProtectSystem=strict\nProtectHome=read-only\nReadWritePaths=%s\nPrivateTmp=true\nProtectProc=invisible\nUMask=0027\n[Install]\nWantedBy=multi-user.target\n", site.Domain, p.Username, p.Username, p.Username, appRoot, envDirective, command(site), unitValue(u.HomeDir))
		if site.AppType == "php" || site.AppType == "laravel" {
			if !agentproto.ValidPHPVersion(site.PHPVersion) {
				return fmt.Errorf("invalid PHP version")
			}
			// The socket belongs to the customer's own group: nginx is a member of
			// every customer group, other customers are not, so no account can
			// talk FastCGI to another account's PHP.
			fpm := fmt.Sprintf("[global]\ndaemonize=no\nerror_log=syslog\n[www]\nlisten=%s\nlisten.group=%s\nlisten.mode=0660\npm=ondemand\npm.max_children=%d\npm.process_idle_timeout=30s\npm.max_requests=500\nlisten.backlog=511\nclear_env=no\ncatch_workers_output=yes\nphp_admin_value[open_basedir]=%s/:/tmp/\n", Socket(p.Username, site.Domain), p.Username, phpconfig.Workers(p.MemMB, poolCount), u.HomeDir)
			workers := phpconfig.Workers(p.MemMB, poolCount)
			if site.PHPConfig.Workers > 0 {
				workers = min(workers, site.PHPConfig.Workers)
			}
			fpm = strings.Replace(fpm, fmt.Sprintf("pm.max_children=%d", phpconfig.Workers(p.MemMB, poolCount)), fmt.Sprintf("pm.max_children=%d", workers), 1)
			fpm += fmt.Sprintf("request_terminate_timeout=%ds\n", site.PHPConfig.RequestTimeout)
			// Root-owned directory prevents customer symlink replacement of trace files.
			slowPath, err := prepareSlowLog(unit, u.Uid, u.Gid)
			if err != nil {
				return err
			}
			fpm += "slowlog=" + slowPath + "\n"
			fpm += fmt.Sprintf("request_slowlog_timeout=%ds\n", site.PHPConfig.SlowTimeout)
			keys := []string{}
			for k := range site.PHPConfig.Settings {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fpm += fmt.Sprintf("php_admin_value[%s]=\"%s\"\n", k, site.PHPConfig.Settings[k])
			}
			// The FPM config contains no secrets; the unit environment stays root-only.
			credential := filepath.Join(dir, unit+".fpm")
			oldFPM, _ := os.ReadFile(credential)
			if string(oldFPM) != fpm {
				fpmChanged = true
			}
			if err := os.WriteFile(credential, []byte(fpm), 0644); err != nil {
				return err
			}
			if _, err := run.Output(ctx, "runuser", "-u", p.Username, "--", distroForPHP.PHPFPMBinary(site.PHPVersion), "-t", "--fpm-config", credential); err != nil {
				_ = os.WriteFile(credential, oldFPM, 0644)
				return err
			}
			if err := os.Chmod(credential, 0644); err != nil {
				return err
			}
			// No SupplementaryGroups: customer code must never run in the shared
			// www-data group (it would reach every other account's sockets).
			content = fmt.Sprintf("[Unit]\nDescription=X-Panel PHP %s\nAfter=network.target\n[Service]\nType=simple\nUser=%s\nGroup=%s\nSlice=xpanel-%s.slice\nRuntimeDirectory=%s\nRuntimeDirectoryMode=0755\n%sExecStart=%s --nodaemonize --force-stderr --fpm-config %s\nOOMPolicy=continue\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\nPrivateTmp=true\nProtectProc=invisible\nProtectSystem=strict\nProtectHome=read-only\nReadWritePaths=%s\n[Install]\nWantedBy=multi-user.target\n", site.Domain, p.Username, p.Username, p.Username, strings.TrimSuffix(unit, ".service"), envDirective, distroForPHP.PHPFPMBinary(site.PHPVersion), credential, unitValue(u.HomeDir)+" "+slowPath)
		}
		conf := filepath.Join("/etc/systemd/system", unit)
		old, _ := os.ReadFile(conf)
		if string(old) != content || string(oldEnv) != envData || fpmChanged {
			if err := os.WriteFile(conf, []byte(content), 0600); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, unit), []byte(site.Domain), 0600); err != nil {
				return err
			}
			if err := run.Cmd(ctx, "systemctl", "daemon-reload"); err != nil {
				return err
			}
			// Build & restart activates a changed deployment explicitly.
			if err := stopUnit(ctx, unit); err != nil {
				return err
			}
		}
		if _, err := os.Stat(filepath.Join(dir, unit+".built")); err == nil || !Dynamic(site.AppType) {
			if err := run.Cmd(ctx, "systemctl", "enable", unit); err != nil {
				return err
			}
			if err := run.Cmd(ctx, "systemctl", "start", unit); err != nil {
				return err
			}
			if err := ready(ctx, p.Username, site); err != nil {
				return err
			}
		}
	}
	// Only nginx and this account may connect to its application ports.
	var ports []int
	for _, site := range p.Sites {
		if Dynamic(site.AppType) {
			ports = append(ports, site.Port)
		}
	}
	if err := netguard.WritePorts(dir, ports); err != nil {
		return err
	}
	if err := netguard.Rebuild(ctx, ConfigRoot, distroForPHP.NginxUser()); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if want[e.Name()] || !strings.HasSuffix(e.Name(), ".service") {
			continue
		}
		if err := stopUnit(ctx, e.Name()); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join("/etc/systemd/system", e.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(filepath.Join(dir, e.Name()+".built"))
		_ = os.Remove(filepath.Join(dir, e.Name()+".fpm"))
		_ = os.Remove(filepath.Join(dir, e.Name()+".env"))
		_ = os.Remove(filepath.Join("/var/log/xpanel/php", e.Name()+".slow.log"))
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
		if err := run.Cmd(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}
func Detect(ctx context.Context, p agentproto.SiteAppParams) (any, error) {
	r, err := root(p)
	if err != nil {
		return nil, err
	}
	// Read as the customer: root must never follow an uploaded symlink.
	out, err := run.Output(ctx, "runuser", "-u", p.Username, "--", "/bin/sh", "-c", `cd "$1" || exit; if [ -f artisan ]; then echo laravel; elif [ -f package.json ]; then if grep -q '"next"' package.json; then echo nextjs; else echo nodejs; fi; elif [ -f go.mod ]; then echo go; elif [ -f index.php ]; then echo php; else echo html; fi`, "detect", r)
	return agentproto.SiteDetectResult{AppType: strings.TrimSpace(out)}, err
}
func Build(ctx context.Context, d distro.Distro, p agentproto.SiteAppParams) (any, error) {
	r, err := root(p)
	if err != nil {
		return nil, err
	}
	u, err := accounts.Managed(p.Username)
	if err != nil {
		return nil, err
	}
	path, err := runtimePath(p.Site)
	if err != nil {
		return nil, err
	}
	script := "set -eu\n"
	if p.Sample {
		script += `if [ "$(find . -mindepth 1 -maxdepth 1 | wc -l)" = 1 ] && [ -f index.html ] && grep -q 'Trang này được tạo bởi X-Panel' index.html; then rm index.html; fi` + "\n"
	}
	if p.Sample {
		if p.Site.AppType == "laravel" {
			script += `rmdir public 2>/dev/null || true; [ -z "$(ls -A)" ] || { echo 'Sample requires an empty directory'; exit 1; }; /usr/bin/php` + p.Site.PHPVersion + ` /usr/local/bin/composer create-project laravel/laravel . --no-interaction` + "\n"
		} else {
			script += sampleScript(p.Site.AppType)
		}
	}
	switch p.Site.AppType {
	case "nodejs", "nextjs":
		script += "if [ -f package-lock.json ]; then npm ci --include=dev; else npm install --include=dev; fi\nnpm run build --if-present\n"
	case "go":
		script += "go build -o .xpanel-app .\n"
	case "laravel":
		script += "/usr/bin/php" + p.Site.PHPVersion + " /usr/local/bin/composer install --no-dev --no-interaction --optimize-autoloader\n/usr/bin/php" + p.Site.PHPVersion + " artisan config:cache\n/usr/bin/php" + p.Site.PHPVersion + " artisan view:cache\n"
	}
	// The customer's own deploy commands (migrations, asset builds…), with
	// "php" and "composer" bound to the site's PHP version. They run as the
	// account in the same confined unit as the build.
	if s := strings.TrimSpace(p.DeployScript); s != "" {
		if len(s) > 8000 || strings.ContainsRune(s, 0) {
			return nil, fmt.Errorf("deploy script too long")
		}
		if agentproto.ValidPHPVersion(p.Site.PHPVersion) {
			php := "/usr/bin/php" + p.Site.PHPVersion
			script += "php() { " + php + " \"$@\"; }\ncomposer() { " + php + " /usr/local/bin/composer \"$@\"; }\n"
		}
		script += "echo '--- Lệnh triển khai ---'\nset -x\n" + s + "\nset +x\n"
	}
	// A transient service bounds the whole process tree and kills it on timeout.
	args := []string{"--quiet", "--wait", "--pipe", "--collect", "--uid=" + p.Username, "--gid=" + p.Username, "--slice=xpanel-" + p.Username + ".slice", "--working-directory=" + r, "--property=RuntimeMaxSec=1800", "--property=NoNewPrivileges=yes", "--property=ProtectSystem=strict", "--property=ProtectHome=read-only", "--property=ReadWritePaths=" + u.HomeDir, "--property=PrivateTmp=yes", "--property=ProtectProc=invisible"}
	env := map[string]string{"HOME": u.HomeDir, "PATH": path, "PORT": strconv.Itoa(p.Site.Port), "HOST": "127.0.0.1", "NODE_ENV": "production", "XDG_CACHE_HOME": filepath.Join(u.HomeDir, "tmp/.cache"), "npm_config_cache": filepath.Join(u.HomeDir, "tmp/.npm"), "GOPATH": filepath.Join(u.HomeDir, "tmp/go"), "GOCACHE": filepath.Join(u.HomeDir, "tmp/go-build"), "COMPOSER_HOME": filepath.Join(u.HomeDir, "tmp/composer"), "COMPOSER_CACHE_DIR": filepath.Join(u.HomeDir, "tmp/composer-cache")}
	for k, v := range p.Site.Env {
		env[k] = v
	}
	var buildEnv []string
	for k, v := range env {
		args = append(args, "--setenv="+k)
		buildEnv = append(buildEnv, k+"="+v)
	}
	args = append(args, "/bin/sh", "-c", script)
	out, err := run.With(ctx, run.Opts{Timeout: 30 * time.Minute, Env: buildEnv}, "systemd-run", args...)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, out)
	}
	if Managed(p.Site.AppType) && accounts.Locked(p.Username) {
		return agentproto.TaskLog{Log: out + "\nTài khoản đã bị khóa trong lúc build: không khởi động ứng dụng"}, nil
	}
	if Managed(p.Site.AppType) {
		unit := Unit(p.Username, p.Site.Domain)
		if err := os.WriteFile(filepath.Join(ConfigRoot, p.Username, unit+".built"), []byte("built"), 0600); err != nil {
			return nil, err
		}
		if err := run.Cmd(ctx, "systemctl", "enable", unit); err != nil {
			return nil, err
		}
		if err := run.Cmd(ctx, "systemctl", "restart", unit); err != nil {
			return nil, err
		}
		if err := ready(ctx, p.Username, p.Site); err != nil {
			return nil, err
		}
	}
	return agentproto.TaskLog{Log: out + "\nBuild & restart hoàn tất"}, nil
}

// Restart restarts the site's service without building. A Node.js/Next.js/Go
// app must have been built once; Next.js and Go still need Build & restart
// to pick up source changes.
func Restart(ctx context.Context, p agentproto.SiteAppParams) (any, error) {
	if _, err := root(p); err != nil {
		return nil, err
	}
	if !Managed(p.Site.AppType) {
		return nil, fmt.Errorf("website tĩnh không có service để restart")
	}
	if accounts.Locked(p.Username) {
		return nil, fmt.Errorf("tài khoản đang bị khóa")
	}
	unit := Unit(p.Username, p.Site.Domain)
	if _, err := os.Stat(filepath.Join(ConfigRoot, p.Username, unit+".built")); err != nil && Dynamic(p.Site.AppType) {
		return nil, fmt.Errorf("ứng dụng chưa được build: bấm Build & restart")
	}
	if err := run.Cmd(ctx, "systemctl", "restart", unit); err != nil {
		return nil, err
	}
	if err := ready(ctx, p.Username, p.Site); err != nil {
		return nil, err
	}
	return agentproto.TaskLog{Log: "Đã restart " + p.Site.Domain}, nil
}
func Logs(ctx context.Context, p agentproto.SiteAppParams) (any, error) {
	if _, err := root(p); err != nil {
		return nil, err
	}
	out, err := run.Output(ctx, "journalctl", "--no-pager", "-n", "200", "-u", Unit(p.Username, p.Site.Domain))
	if p.Site.AppType == "php" || p.Site.AppType == "laravel" {
		path := filepath.Join("/var/log/xpanel/php", Unit(p.Username, p.Site.Domain)+".slow.log")
		if f, e := os.Open(path); e == nil {
			defer f.Close()
			if st, e := f.Stat(); e == nil {
				if st.Size() > 65536 {
					_, _ = f.Seek(-65536, 2)
				}
				data, _ := io.ReadAll(io.LimitReader(f, 65536))
				out += "\n--- PHP slow log ---\n" + string(data)
			}
		}
	}
	return agentproto.TaskLog{Log: out}, err
}
func prepareSlowLog(unit, uidText, gidText string) (string, error) {
	const dir = "/var/log/xpanel/php"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, unit+".slow.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	uid, err := strconv.Atoi(uidText)
	if err != nil {
		return "", err
	}
	gid, err := strconv.Atoi(gidText)
	if err != nil {
		return "", err
	}
	if err = f.Chown(uid, gid); err != nil {
		return "", err
	}
	// copytruncate avoids restarting customer masters during rotation.
	rotation := "/var/log/xpanel/php/*.slow.log {\n daily\n maxsize 10M\n rotate 7\n missingok\n notifempty\n compress\n copytruncate\n su root root\n}\n"
	if err = os.WriteFile("/etc/logrotate.d/xpanel-php", []byte(rotation), 0644); err != nil {
		return "", err
	}
	return path, nil
}
func sampleScript(kind string) string {
	files := map[string]string{}
	switch kind {
	case "html":
		files["index.html"] = "<!doctype html><meta charset=utf-8><h1>Hello from X-Panel</h1>"
	case "php":
		files["index.php"] = "<?php echo 'Hello from X-Panel';"
	case "nodejs":
		files["package.json"] = `{"private":true,"scripts":{"start":"node server.js"}}`
		files["server.js"] = `require('http').createServer((req,res)=>res.end('Hello from X-Panel')).listen(process.env.PORT, process.env.HOST || '127.0.0.1');`
	case "nextjs":
		files["package.json"] = `{"private":true,"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"latest","react":"latest","react-dom":"latest"}}`
		files["app/page.js"] = `export default function Page(){return <h1>Hello from X-Panel</h1>}`
		files["app/layout.js"] = `export default function Layout({children}){return <html><body>{children}</body></html>}`
	case "go":
		files["go.mod"] = "module example.com/xpanel-app\n\ngo 1.22\n"
		files["main.go"] = "package main\nimport (\"net/http\";\"os\";\"log\")\nfunc main(){http.HandleFunc(\"/\",func(w http.ResponseWriter,r *http.Request){w.Write([]byte(\"Hello from X-Panel\"))});log.Fatal(http.ListenAndServe(\"127.0.0.1:\"+os.Getenv(\"PORT\"),nil))}\n"
	}
	script := `[ -z "$(ls -A)" ] || { echo 'Sample requires an empty directory'; exit 1; }` + "\n"
	if kind == "nextjs" {
		script += "mkdir app\n"
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {

		script += "cat > '" + k + "' <<'XPANEL_SAMPLE'\n" + files[k] + "\nXPANEL_SAMPLE\n"
	}
	return script
}
