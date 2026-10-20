package pma

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xpanel/apps/agent/internal/nginxctl"
	"xpanel/apps/agent/internal/run"
)

// The web database tools (phpMyAdmin, Adminer) get their own port, so their
// pages are a different browser origin from the panel: a script injected
// through database content cannot use the administrator's panel session.
const (
	ToolsPort       = "8890"
	ToolsDir        = "/etc/xpanel/nginx/dbtools.d"
	toolsServerConf = "/etc/nginx/conf.d/xpanel-dbtools.conf"
)

const toolsServer = `# Managed by X-Panel: phpMyAdmin / Adminer on their own origin.
server {
    listen ` + ToolsPort + ` ssl;
    listen [::]:` + ToolsPort + ` ssl;
    server_name _;
    ssl_certificate     /etc/xpanel/ssl/panel.crt;
    ssl_certificate_key /etc/xpanel/ssl/panel.key;
    ssl_protocols       TLSv1.2 TLSv1.3;
    client_max_body_size 256m;
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Frame-Options SAMEORIGIN always;
    add_header Referrer-Policy same-origin always;
    include ` + ToolsDir + `/*.conf;
}
`

func redirect(name string) string {
	return "# Managed by X-Panel: " + name + " runs on port " + ToolsPort + ".\n" +
		"location ^~ /" + name + " { return 302 https://$host:" + ToolsPort + "/" + name + "/; }\n"
}

// InstallTool serves a tool's nginx locations on the tools port and makes
// /<name>/ on the panel port redirect there.
func InstallTool(ctx context.Context, name, locations string) error {
	for _, d := range []string{ToolsDir, snippetDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	files := map[string]string{
		toolsServerConf:                         toolsServer,
		filepath.Join(ToolsDir, name+".conf"):   locations,
		filepath.Join(snippetDir, name+".conf"): redirect(name),
	}
	backup := map[string][]byte{}
	for path, content := range files {
		if old, err := os.ReadFile(path); err == nil {
			backup[path] = old
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		for path := range files {
			if old, ok := backup[path]; ok {
				_ = os.WriteFile(path, old, 0o644)
			} else {
				_ = os.Remove(path)
			}
		}
		return fmt.Errorf("nginx config test failed: %s", out)
	}
	openToolsPort(ctx)
	return nginxctl.Reload(ctx)
}

// RemoveTool drops a tool's locations and redirect.
func RemoveTool(ctx context.Context, name string) error {
	_ = os.Remove(filepath.Join(ToolsDir, name+".conf"))
	_ = os.Remove(filepath.Join(snippetDir, name+".conf"))
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		return fmt.Errorf("nginx config test failed: %s", out)
	}
	return nginxctl.Reload(ctx)
}

func openToolsPort(ctx context.Context) {
	if out, err := run.Output(ctx, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
		_ = run.Cmd(ctx, "ufw", "allow", ToolsPort+"/tcp")
	}
}

// MigrateTools moves tools installed by older versions (served on the panel
// port) to the tools port. Called when the agent starts.
func MigrateTools(ctx context.Context) error {
	for _, name := range []string{"phpmyadmin", "adminer"} {
		old := filepath.Join(snippetDir, name+".conf")
		b, err := os.ReadFile(old)
		if err != nil || !strings.Contains(string(b), "fastcgi_pass") {
			continue
		}
		if err := InstallTool(ctx, name, string(b)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// WaitSocket waits until a PHP-FPM pool accepts connections: reloading
// php-fpm returns before the new master has created its sockets, and the
// first request would otherwise get a 502.
func WaitSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("PHP-FPM chưa sẵn sàng (%s): %w", path, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
