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

// adminerWrapper only allows logins to the local customer PostgreSQL
// cluster, so the tool cannot be used to reach other hosts.
var adminerWrapper = `<?php
// Managed by X-Panel.
function adminer_object() {
    class XPanelAdminer extends Adminer\Adminer {
        function login($login, $password) {
            return Adminer\DRIVER === 'pgsql'
                && in_array(Adminer\SERVER, ['127.0.0.1:` + fmt.Sprint(PGPort) + `', 'localhost:` + fmt.Sprint(PGPort) + `'], true)
                && $password !== '';
        }
    }
    return new XPanelAdminer;
}
include __DIR__ . '/adminer.php';
`

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
