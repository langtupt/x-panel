package mail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/fetch"
	"xpanel/apps/agent/internal/nginxctl"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/pma"
	"xpanel/apps/agent/internal/run"
)

// Roundcube is served at https://<server>:8888/webmail/ by its own PHP-FPM
// pool running as "xpanel-webmail"; its SQLite database, temp files and logs
// live in a directory only that user can read.
const (
	WebmailDir        = "/opt/xpanel/roundcube"
	webmailData       = "/var/lib/xpanel/webmail"
	webmailUser       = "xpanel-webmail"
	webmailSocket     = "/run/php/xpanel-webmail.sock"
	webmailSnippet    = "webmail.conf"
	webmailServerConf = "/etc/nginx/conf.d/xpanel-webmail.conf"
	WebmailPort       = "2096"
)

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:<hex>"
	} `json:"assets"`
}

// WebmailAvailable returns the latest stable Roundcube version.
func WebmailAvailable(ctx context.Context) ([]string, error) {
	var rel ghRelease
	if err := fetch.JSON(ctx, "https://api.github.com/repos/roundcube/roundcubemail/releases/latest", &rel); err != nil {
		return nil, err
	}
	if !versionRe.MatchString(rel.Tag) {
		return nil, fmt.Errorf("invalid Roundcube version %q", rel.Tag)
	}
	return []string{rel.Tag}, nil
}

// WebmailInstalled returns the installed version, if any.
func WebmailInstalled() []string {
	b, err := os.ReadFile(filepath.Join(WebmailDir, "program/include/iniset.php"))
	if err != nil {
		return nil
	}
	if m := regexp.MustCompile(`RCMAIL_VERSION',\s*'([0-9.]+)'`).FindSubmatch(b); m != nil {
		return []string{string(m[1])}
	}
	return []string{"?"}
}

func webmailPoolFile(d distro.Distro, v string) string {
	return filepath.Join(d.PHPFPMPoolDir(v), "xpanel-webmail.conf")
}

// InstallWebmail downloads Roundcube (checking the SHA-256 GitHub publishes
// for the release asset), configures it for the local Dovecot/Postfix and
// adds /webmail/ to the panel.
func InstallWebmail(ctx context.Context, d distro.Distro, version string, logf func(string, ...any)) error {
	if !Installed() {
		return errors.New("cần cài Mail server trước")
	}
	versions := php.Installed(d)
	if len(versions) == 0 {
		return errors.New("cần cài ít nhất một phiên bản PHP trước")
	}
	phpV := versions[len(versions)-1]
	var rel ghRelease
	if err := fetch.JSON(ctx, "https://api.github.com/repos/roundcube/roundcubemail/releases/tags/"+version, &rel); err != nil {
		return err
	}
	if !versionRe.MatchString(rel.Tag) || rel.Tag != version {
		return fmt.Errorf("phiên bản Roundcube không hợp lệ")
	}
	name := "roundcubemail-" + version + "-complete.tar.gz"
	var url, sum string
	for _, a := range rel.Assets {
		if a.Name == name {
			url, sum = a.URL, strings.TrimPrefix(a.Digest, "sha256:")
		}
	}
	if url == "" || len(sum) != 64 {
		return fmt.Errorf("không tìm thấy %s kèm SHA-256 trong bản phát hành", name)
	}
	logf("Tải %s", url)
	archive, err := fetch.File(ctx, url, sum)
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	logf("Đã kiểm tra SHA-256")

	// System user and private data directory.
	if _, err := user.Lookup(webmailUser); err != nil {
		if err := run.Cmd(ctx, "useradd", "--system", "--no-create-home", "--home-dir", "/nonexistent",
			"--shell", "/usr/sbin/nologin", webmailUser); err != nil {
			return err
		}
	}
	u, err := user.Lookup(webmailUser)
	if err != nil {
		return err
	}
	for _, dir := range []string{webmailData, webmailData + "/temp", webmailData + "/logs"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chown(dir, atoi(u.Uid), atoi(u.Gid)); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	secret := make([]byte, 12)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	desKey := hex.EncodeToString(secret) // 24 characters
	if old, err := os.ReadFile(filepath.Join(WebmailDir, "config/config.inc.php")); err == nil {
		// Keep the existing key so saved session data stays readable.
		if m := regexp.MustCompile(`des_key'\]\s*=\s*'([0-9a-f]{24})'`).FindSubmatch(old); m != nil {
			desKey = string(m[1])
		}
	}
	if err := fetch.Replace(WebmailDir, func(tmp string) error {
		if err := fetch.UntarGz(archive, tmp, 1); err != nil {
			return err
		}
		_ = os.RemoveAll(filepath.Join(tmp, "installer"))
		cfg := filepath.Join(tmp, "config/config.inc.php")
		if err := os.WriteFile(cfg, []byte(webmailConfig(desKey)), 0o640); err != nil {
			return err
		}
		return os.Chown(cfg, 0, atoi(u.Gid))
	}); err != nil {
		return err
	}
	logf("Đã giải nén vào %s", WebmailDir)

	// Database: create on first install, migrate on upgrade.
	phpBin := "/usr/bin/php" + phpV
	dbArgs := func(script string, extra ...string) []string {
		return append([]string{"-u", webmailUser, "--", phpBin, filepath.Join(WebmailDir, script),
			"--dir=" + filepath.Join(WebmailDir, "SQL")}, extra...)
	}
	if _, err := os.Stat(webmailData + "/roundcube.db"); err != nil {
		if out, err := run.With(ctx, run.Opts{}, "runuser", dbArgs("bin/initdb.sh")...); err != nil && !strings.Contains(out, "already exists") {
			return fmt.Errorf("khởi tạo database Roundcube: %s", out)
		}
	}
	// Brings an existing (or auto-created) schema up to this release.
	if out, err := run.With(ctx, run.Opts{}, "runuser", dbArgs("bin/updatedb.sh", "--package=roundcube")...); err != nil {
		return fmt.Errorf("cập nhật database Roundcube: %s", out)
	}

	// PHP-FPM pool on the newest PHP only.
	for _, other := range versions {
		if other != phpV {
			if err := os.Remove(webmailPoolFile(d, other)); err == nil {
				_ = run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(other))
			}
		}
	}
	if err := os.WriteFile(webmailPoolFile(d, phpV), []byte(webmailPool), 0o644); err != nil {
		return err
	}
	if out, err := run.Output(ctx, d.PHPFPMBinary(phpV), "-t"); err != nil {
		return fmt.Errorf("php-fpm config test failed: %s", out)
	}
	if err := run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(phpV)); err != nil {
		return err
	}
	if err := pma.WaitSocket(webmailSocket, 20*time.Second); err != nil {
		return err
	}
	// Own port = own browser origin: script injected through a message cannot
	// call the panel API with the viewer's panel session.
	if err := os.WriteFile(webmailServerConf, []byte(webmailServer), 0o644); err != nil {
		return err
	}
	if err := pma.InstallSnippet(ctx, filepath.Join(pma.SnippetDir, webmailSnippet), webmailRedirect); err != nil {
		_ = os.Remove(webmailServerConf)
		return err
	}
	openPorts(ctx, logf, WebmailPort)
	logf("Roundcube %s (PHP %s) sẵn sàng tại https://<máy chủ>:%s/webmail/", version, phpV, WebmailPort)
	return nil
}

// RemoveWebmail deletes Roundcube, its pool, data and nginx snippet.
func RemoveWebmail(ctx context.Context, d distro.Distro) error {
	_ = os.Remove(filepath.Join(pma.SnippetDir, webmailSnippet))
	_ = os.Remove(webmailServerConf)
	_ = nginxctl.Reload(ctx)
	for _, v := range php.Installed(d) {
		if err := os.Remove(webmailPoolFile(d, v)); err == nil {
			_ = run.Cmd(ctx, "systemctl", "reload-or-restart", d.PHPFPMService(v))
		}
	}
	if err := os.RemoveAll(WebmailDir); err != nil {
		return err
	}
	return os.RemoveAll(webmailData)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func webmailConfig(desKey string) string {
	return `<?php
// Managed by X-Panel: changes will be overwritten.
$config = [];
$config['db_dsnw'] = 'sqlite:///` + webmailData + `/roundcube.db?mode=0600';
// Loopback connections count as secure for Dovecot; SMTP submission requires
// STARTTLS, whose certificate is issued for the public hostname.
$config['imap_host'] = 'localhost:143';
$config['smtp_host'] = 'tls://localhost:587';
$config['smtp_user'] = '%u';
$config['smtp_pass'] = '%p';
$config['smtp_conn_options'] = ['ssl' => ['verify_peer' => false, 'verify_peer_name' => false]];
$config['imap_conn_options'] = $config['smtp_conn_options'];
$config['username_domain_forced'] = false;
$config['login_lc'] = 2;
$config['support_url'] = '';
$config['product_name'] = 'X-Panel Webmail';
$config['des_key'] = '` + desKey + `';
$config['plugins'] = ['archive', 'zipdownload'];
$config['skin'] = 'elastic';
$config['language'] = 'vi_VN';
$config['temp_dir'] = '` + webmailData + `/temp/';
$config['log_dir'] = '` + webmailData + `/logs/';
$config['enable_installer'] = false;
$config['use_https'] = true;
$config['session_lifetime'] = 30;
$config['ip_check'] = true;
$config['login_rate_limit'] = 5;
$config['max_message_size'] = '25M';
$config['request_path'] = '/webmail/';
` + identitiesLine + `
`
}

// identitiesLine keeps one identity whose address is the login: webmail
// users cannot write another mailbox's address as sender.
const identitiesLine = "$config['identities_level'] = 3;"

const webmailPool = `; Managed by X-Panel: changes will be overwritten.
[xpanel-webmail]
user = ` + webmailUser + `
group = ` + webmailUser + `
listen = ` + webmailSocket + `
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 10
pm.process_idle_timeout = 30s
php_admin_value[open_basedir] = ` + WebmailDir + `/:` + webmailData + `/:/tmp/
php_admin_value[upload_tmp_dir] = ` + webmailData + `/temp
php_admin_value[session.save_path] = ` + webmailData + `/temp
php_admin_value[upload_max_filesize] = 25M
php_admin_value[post_max_size] = 30M
php_admin_value[memory_limit] = 256M
php_admin_flag[display_errors] = off
php_admin_flag[log_errors] = on
`

const webmailRedirect = `# Managed by X-Panel: Roundcube runs on its own port.
location ^~ /webmail { return 302 https://$host:` + WebmailPort + `/webmail/; }
`

const webmailServer = `# Managed by X-Panel: Roundcube Webmail.
server {
    listen ` + WebmailPort + ` ssl;
    listen [::]:` + WebmailPort + ` ssl;
    server_name _;
    ssl_certificate     ` + panelCert + `;
    ssl_certificate_key ` + panelKey + `;
    ssl_protocols       TLSv1.2 TLSv1.3;
    client_max_body_size 30m;
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Frame-Options SAMEORIGIN always;
    add_header Referrer-Policy same-origin always;
    add_header X-Content-Type-Options nosniff always;
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;
    add_header Content-Security-Policy "frame-ancestors 'self'; object-src 'none'; base-uri 'self'; form-action 'self'" always;
    location = / { return 301 /webmail/; }
location = /webmail { return 301 /webmail/; }
location ^~ /webmail/ {
    alias ` + WebmailDir + `/public_html/;
    index index.php;
    location ~ ^/webmail/(config|temp|logs|bin|SQL|installer|vendor|program/include|program/lib)/ { deny all; }
    location ~ /\. { deny all; }
    # Roundcube 1.7 serves its CSS/JS through static.php/<path> (PATH_INFO).
    location ~ ^/webmail/([A-Za-z0-9_./-]+?\.php)(/.*)?$ {
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME ` + WebmailDir + `/public_html/$1;
        fastcgi_param SCRIPT_NAME /webmail/$1;
        fastcgi_param PATH_INFO $2;
        fastcgi_param HTTPS on;
        fastcgi_pass unix:` + webmailSocket + `;
        fastcgi_read_timeout 300s;
    }
}
}
`

// MigrateWebmail rewrites the webmail server block written by older versions
// (no PATH_INFO: Roundcube 1.7 loaded without CSS/JS). Run at agent start.
func MigrateWebmail(ctx context.Context) error {
	if err := migrateIdentities(); err != nil {
		return err
	}
	old, err := os.ReadFile(webmailServerConf)
	if err != nil || string(old) == webmailServer {
		return nil
	}
	if err := os.WriteFile(webmailServerConf, []byte(webmailServer), 0o644); err != nil {
		return err
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		_ = os.WriteFile(webmailServerConf, old, 0o644)
		return fmt.Errorf("nginx config test failed: %s", out)
	}
	return nginxctl.Reload(ctx)
}

// migrateIdentities adds identitiesLine to the Roundcube configuration of
// older installs (opening without O_CREATE/chmod keeps its owner and mode).
func migrateIdentities() error {
	path := filepath.Join(WebmailDir, "config/config.inc.php")
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), "identities_level") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(identitiesLine + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
