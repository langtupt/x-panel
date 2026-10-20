// Package pma installs phpMyAdmin under https://<server>:8888/phpmyadmin/,
// served by a dedicated PHP-FPM pool running as the "xpanel-pma" user.
package pma

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/nginxctl"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

const (
	Dir         = "/opt/xpanel/phpmyadmin"
	tmpDir      = "/var/lib/xpanel/pma-tmp"
	sysUser     = "xpanel-pma"
	socket      = "/run/php/xpanel-pma.sock"
	snippetDir  = "/etc/xpanel/nginx/panel.d"
	downloadURL = "https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz"
)

func poolFile(v string) string { return filepath.Join("/etc/php", v, "fpm/pool.d/xpanel-pma.conf") }

// Status reports whether phpMyAdmin is installed and which PHP version serves it.
func Status(d distro.Distro) agentproto.PMAStatus {
	var st agentproto.PMAStatus
	if _, err := os.Stat(filepath.Join(Dir, "index.php")); err != nil {
		return st
	}
	for _, v := range php.Installed(d) {
		if _, err := os.Stat(poolFile(v)); err == nil {
			st.Installed, st.PHPVersion = true, v
		}
	}
	return st
}

// Install downloads the latest release (verifying its SHA-256), writes the
// config, the FPM pool on the newest installed PHP and the nginx snippet.
func Install(ctx context.Context, d distro.Distro) (string, error) {
	return InstallVersion(ctx, d, "latest")
}

func InstallVersion(ctx context.Context, d distro.Distro, version string) (string, error) {
	downloadURL := downloadURL
	if version != "latest" {
		for _, c := range version {
			if (c < '0' || c > '9') && c != '.' {
				return "", fmt.Errorf("invalid phpMyAdmin version")
			}
		}
		if version == "" {
			return "", fmt.Errorf("empty version")
		}
		downloadURL = "https://files.phpmyadmin.net/phpMyAdmin/" + version + "/phpMyAdmin-" + version + "-all-languages.tar.gz"
	}
	var log strings.Builder
	logf := func(f string, a ...any) { fmt.Fprintf(&log, f+"\n", a...) }

	if err := EnsurePool(ctx, d, logf); err != nil {
		return log.String(), err
	}

	logf("Tải %s", downloadURL)
	archive, err := download(ctx, downloadURL)
	if err != nil {
		return log.String(), err
	}
	defer os.Remove(archive)
	sum, err := fetchText(ctx, downloadURL+".sha256")
	if err != nil {
		return log.String(), err
	}
	if err := verify(archive, sum); err != nil {
		return log.String(), err
	}
	logf("Đã kiểm tra SHA-256")

	stage := Dir + ".new"
	_ = os.RemoveAll(stage)
	if err := extract(archive, stage); err != nil {
		return log.String(), err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return log.String(), err
	}
	if err := os.WriteFile(filepath.Join(stage, "config.inc.php"), []byte(config(hex.EncodeToString(secret))), 0o644); err != nil {
		return log.String(), err
	}
	_ = os.RemoveAll(filepath.Join(stage, "setup")) // web installer is not needed
	_ = os.RemoveAll(Dir + ".old")
	if _, err := os.Stat(Dir); err == nil {
		if err := os.Rename(Dir, Dir+".old"); err != nil {
			return log.String(), err
		}
	}
	if err := os.Rename(stage, Dir); err != nil {
		return log.String(), err
	}
	_ = os.RemoveAll(Dir + ".old")

	if err := InstallTool(ctx, "phpmyadmin", nginxSnippet); err != nil {
		return log.String(), err
	}
	logf("phpMyAdmin đã sẵn sàng tại https://<máy chủ>:%s/phpmyadmin/", ToolsPort)
	return log.String(), nil
}

var httpClient = &http.Client{Timeout: 5 * time.Minute}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func download(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp("", "pma-*.tar.gz")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

func fetchText(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(b), err
}

func verify(file, sumLine string) error {
	fields := strings.Fields(sumLine)
	if len(fields) == 0 {
		return errors.New("empty checksum file")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, fields[0]) {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", got, fields[0])
	}
	return nil
}

// extract unpacks the release, dropping its top-level directory.
func extract(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		_, rel, ok := strings.Cut(hdr.Name, "/")
		if !ok || rel == "" {
			continue
		}
		p := filepath.Join(dest, filepath.Clean("/"+rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

func config(secretHex string) string {
	return `<?php
// Managed by X-Panel: changes will be overwritten.
$cfg['blowfish_secret'] = sodium_hex2bin('` + secretHex + `');
$cfg['Servers'][1]['auth_type'] = 'cookie';
$cfg['Servers'][1]['host'] = 'localhost';
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['Servers'][1]['AllowRoot'] = false;
$cfg['TempDir'] = '` + tmpDir + `';
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';
`
}

const pool = `; Managed by X-Panel: changes will be overwritten.
[xpanel-pma]
user = ` + sysUser + `
group = ` + sysUser + `
listen = ` + socket + `
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 30s
php_admin_value[open_basedir] = ` + Dir + `/:` + AdminerDir + `/:` + tmpDir + `/:/tmp/
php_admin_value[session.save_path] = ` + tmpDir + `
php_admin_value[upload_tmp_dir] = ` + tmpDir + `
php_admin_value[upload_max_filesize] = 256M
php_admin_value[post_max_size] = 256M
`

const nginxSnippet = `# Managed by X-Panel: phpMyAdmin on the panel port.
location = /phpmyadmin { return 301 /phpmyadmin/; }
location ^~ /phpmyadmin/ {
    alias ` + Dir + `/;
    index index.php;
    location ~ ^/phpmyadmin/(libraries|templates|vendor|sql|setup)/ { deny all; }
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $request_filename;
        fastcgi_param HTTPS on;
        fastcgi_pass unix:` + socket + `;
        fastcgi_read_timeout 300s;
    }
}
`

// AdminerDir holds Adminer, served by the same pool on the panel port.
const AdminerDir = "/opt/xpanel/adminer"

// EnsurePool creates the "xpanel-pma" system user, its temp directory and the
// PHP-FPM pool (on the newest installed PHP) that serves the panel's web
// database tools (phpMyAdmin, Adminer).
func EnsurePool(ctx context.Context, d distro.Distro, logf func(string, ...any)) error {
	versions := php.Installed(d)
	if len(versions) == 0 {
		return errors.New("cần cài ít nhất một phiên bản PHP trước")
	}
	v := versions[len(versions)-1]
	logf("Dùng PHP %s", v)

	if _, err := user.Lookup(sysUser); err != nil {
		if err := run.Cmd(ctx, "useradd", "--system", "--no-create-home", "--home-dir", "/nonexistent",
			"--shell", "/usr/sbin/nologin", sysUser); err != nil {
			return err
		}
	}
	pmaUser, err := user.Lookup(sysUser)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return err
	}
	if err := run.Cmd(ctx, "chown", pmaUser.Uid+":"+pmaUser.Gid, tmpDir); err != nil {
		return err
	}
	// One pool only, on the newest PHP.
	for _, other := range versions {
		if other != v {
			if err := os.Remove(poolFile(other)); err == nil {
				_ = run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(other))
			}
		}
	}
	if cur, err := os.ReadFile(poolFile(v)); err == nil && string(cur) == pool {
		return nil
	}
	if err := os.WriteFile(poolFile(v), []byte(pool), 0o644); err != nil {
		return err
	}
	if out, err := run.Output(ctx, d.PHPFPMBinary(v), "-t"); err != nil {
		return fmt.Errorf("php-fpm config test failed: %s", out)
	}
	if err := run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(v)); err != nil {
		return err
	}
	return WaitSocket(socket, 20*time.Second)
}

// InstallSnippet adds an nginx snippet to the panel server block and reloads.
func InstallSnippet(ctx context.Context, file, content string) error {
	if err := os.MkdirAll(snippetDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return err
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		_ = os.Remove(file)
		return fmt.Errorf("nginx config test failed: %s", out)
	}
	return nginxctl.Reload(ctx)
}

// Socket is where the tools pool listens.
const Socket = socket

// SnippetDir is where panel add-on nginx snippets live.
const SnippetDir = snippetDir
