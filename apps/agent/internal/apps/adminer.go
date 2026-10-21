package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/fetch"
	"xpanel/apps/agent/internal/pma"
)

func adminerInstalled() []string {
	if _, err := os.Stat(filepath.Join(pma.AdminerDir, "adminer.php")); err == nil {
		return detectedFileVersion(filepath.Join(pma.AdminerDir, "adminer.php"), `@version\s+([0-9.]+)`)
	}
	return nil
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// installAdminer downloads the latest full Adminer build (GitHub release over
// TLS; the project publishes no checksums) and serves it at /adminer/,
// restricted to the customer PostgreSQL cluster.
func installAdminer(ctx context.Context, d distro.Distro, selected string, logf func(string, ...any)) error {
	if postgresInstalled() == "" {
		return errors.New("cần cài PostgreSQL trước")
	}
	if err := pma.EnsurePool(ctx, d, logf); err != nil {
		return err
	}
	var rel ghRelease
	if err := fetch.JSON(ctx, "https://api.github.com/repos/vrana/adminer/releases/latest", &rel); err != nil {
		return err
	}
	version := strings.TrimPrefix(rel.Tag, "v")
	if version != selected {
		return fmt.Errorf("Adminer vừa có bản mới %s; vui lòng làm mới danh sách", version)
	}
	want := "adminer-" + version + ".php" // full build with every driver, all languages
	url := ""
	for _, a := range rel.Assets {
		if a.Name == want {
			url = a.URL
		}
	}
	if url == "" {
		return fmt.Errorf("không tìm thấy %s trong bản phát hành %s", want, rel.Tag)
	}
	logf("Tải %s", url)
	body, err := fetch.Text(ctx, url)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(body, "<?php") {
		return errors.New("tệp Adminer tải về không hợp lệ")
	}
	if err := os.MkdirAll(pma.AdminerDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pma.AdminerDir, "adminer.php"), []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pma.AdminerDir, "index.php"), []byte(adminerWrapper), 0o644); err != nil {
		return err
	}
	if err := pma.InstallTool(ctx, "adminer", adminerSnippet); err != nil {
		return err
	}
	logf("Adminer %s sẵn sàng tại /adminer/", version)
	return nil
}

// adminerWrapper only lets Adminer reach the local customer PostgreSQL
// cluster. Adminer connects before it calls login(), so the server is
// forced before the include and in credentials(): a crafted ?pgsql=<host>
// or login form can never send the password to another host.
var adminerWrapper = `<?php
// Managed by X-Panel.
const XPANEL_PG = '127.0.0.1:` + fmt.Sprint(PGPort) + `';
foreach (['server', 'sqlite', 'oracle', 'mssql', 'mongo', 'elastic', 'clickhouse', 'firebird', 'simpledb', 'imap'] as $k) {
    unset($_GET[$k]);
}
if (isset($_GET['pgsql'])) {
    $_GET['pgsql'] = XPANEL_PG;
}
if (isset($_POST['auth']) && is_array($_POST['auth'])) {
    $_POST['auth']['driver'] = 'pgsql';
    $_POST['auth']['server'] = XPANEL_PG;
}
function adminer_object() {
    class XPanelAdminer extends Adminer\Adminer {
        function credentials() {
            return [XPANEL_PG, $_GET['username'], Adminer\get_password()];
        }
        function login($login, $password) {
            if (Adminer\DRIVER !== 'pgsql' || Adminer\SERVER !== XPANEL_PG) {
                return 'Chỉ đăng nhập được PostgreSQL của X-Panel (' . XPANEL_PG . ').';
            }
            return parent::login($login, $password);
        }
        function loginFormField($name, $heading, $value) {
            if ($name == 'driver') {
                return '<input type="hidden" name="auth[driver]" value="pgsql">';
            }
            if ($name == 'server') {
                return $heading . '<input value="' . XPANEL_PG . '" disabled><input type="hidden" name="auth[server]" value="' . XPANEL_PG . '">' . "\n";
            }
            return parent::loginFormField($name, $heading, $value);
        }
    }
    return new XPanelAdminer;
}
include __DIR__ . '/adminer.php';
`

// RefreshAdminer rewrites the wrapper of an installed Adminer, so a fix to
// it reaches servers that installed Adminer with an older X-Panel.
func RefreshAdminer() error {
	if _, err := os.Stat(filepath.Join(pma.AdminerDir, "adminer.php")); err != nil {
		return nil
	}
	path := filepath.Join(pma.AdminerDir, "index.php")
	if old, err := os.ReadFile(path); err == nil && string(old) == adminerWrapper {
		return nil
	}
	return os.WriteFile(path, []byte(adminerWrapper), 0o644)
}

const adminerSnippet = `# Managed by X-Panel: Adminer (PostgreSQL) on the panel port.
location = /adminer { return 301 /adminer/; }
location ^~ /adminer/ {
    alias ` + pma.AdminerDir + `/;
    index index.php;
    location ~ ^/adminer/adminer\.php$ { deny all; }
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $request_filename;
        fastcgi_param HTTPS on;
        fastcgi_pass unix:` + pma.Socket + `;
    }
}
`
