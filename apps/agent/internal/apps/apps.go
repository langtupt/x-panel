// Package apps is the server application catalog shown on the panel's
// "Ứng dụng" page: language runtimes, database servers and admin tools.
package apps

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/mail"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/pma"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

type app struct {
	info      agentproto.AppInfo
	available func(ctx context.Context) ([]string, error) // installable versions, newest first
	installed func() []string
	install   func(ctx context.Context, version string, logf func(string, ...any)) error
}

type Catalog struct {
	D    distro.Distro
	apps []app

	mu    sync.Mutex
	cache map[string]cached // available versions per app (remote lookups)
}

type cached struct {
	at       time.Time
	versions []string
	err      error
	expired  bool
}

func New(d distro.Distro) *Catalog {
	c := &Catalog{D: d, cache: map[string]cached{}}
	c.apps = []app{
		{
			info: agentproto.AppInfo{ID: "php", Name: "PHP", Category: "runtime", MultiVersion: true,
				Description: "PHP-FPM, chạy song song nhiều phiên bản (Debian: sury, Ubuntu: ondrej PPA)."},
			available: func(ctx context.Context) ([]string, error) { return phpAvailable(ctx, d) },
			installed: func() []string { return php.Installed(d) },
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				out, err := php.Install(ctx, d, v)
				logf("%s", out)
				if err == nil && postgresInstalled() != "" {
					err = installPHPPgsql(ctx, d, []string{v}, logf)
				}
				return err
			},
		},
		{
			info: agentproto.AppInfo{ID: "composer", Name: "Composer", Category: "runtime",
				Description: "Trình quản lý thư viện PHP (cần cho Laravel)."},
			available: composerAvailable,
			installed: composerInstalled,
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				return installComposer(ctx, v, logf)
			},
		},
		{
			info: agentproto.AppInfo{ID: "nodejs", Name: "Node.js", Category: "runtime", MultiVersion: true,
				Description: "Bản chính thức từ nodejs.org, cài song song theo dòng phiên bản (LTS và Current)."},
			available: nodeAvailable,
			installed: NodeInstalled,
			install:   installNode,
		},
		{
			info: agentproto.AppInfo{ID: "go", Name: "Go", Category: "runtime", MultiVersion: true,
				Description: "Trình biên dịch Go chính thức từ go.dev."},
			available: goAvailable,
			installed: GoInstalled,
			install:   installGo,
		},
		{
			info: agentproto.AppInfo{ID: "mariadb", Name: "MariaDB", Category: "database", Builtin: true,
				Description: "Cài sẵn cùng panel; database MySQL/MariaDB cho website."},
			available: func(context.Context) ([]string, error) { return nil, nil },
			installed: func() []string { return mariadbVersion() },
			install: func(context.Context, string, func(string, ...any)) error {
				return fmt.Errorf("MariaDB được cài sẵn cùng panel")
			},
		},
		{
			info: agentproto.AppInfo{ID: "postgresql", Name: "PostgreSQL", Category: "database",
				Description: "Từ kho chính thức PGDG; cluster riêng cho khách hàng (tách khỏi database của panel), cổng 5433."},
			available: pgAvailable,
			installed: func() []string {
				if v := postgresInstalled(); v != "" {
					return []string{v}
				}
				return nil
			},
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				return installPostgres(ctx, d, v, logf)
			},
		},
		{
			info: agentproto.AppInfo{ID: "phpmyadmin", Name: "phpMyAdmin", Category: "tool",
				Description: "Quản lý MariaDB qua web tại https://<máy chủ>:8888/phpmyadmin/."},
			available: pmaAvailable,
			installed: func() []string {
				if st := pma.Status(d); st.Installed {
					return detectedFileVersion(pma.Dir+"/package.json", `"version"\s*:\s*"([0-9.]+)"`)
				}
				return nil
			},
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				out, err := pma.InstallVersion(ctx, d, v)
				logf("%s", out)
				return err
			},
		},
		{
			info: agentproto.AppInfo{ID: "adminer", Name: "Adminer (PostgreSQL)", Category: "tool",
				Description: "Quản lý PostgreSQL qua web tại https://<máy chủ>:8888/adminer/ — thay cho phpPgAdmin (đã ngừng phát triển, không chạy trên PHP 8)."},
			available: adminerAvailable,
			installed: adminerInstalled,
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				return installAdminer(ctx, d, v, logf)
			},
		},
		{
			info: agentproto.AppInfo{ID: "mailserver", Name: "Mail server", Category: "mail",
				Description: "Postfix (SMTP) + Dovecot (IMAP/POP3) + OpenDKIM: hộp thư theo tên miền, chuyển tiếp, ký DKIM. Mở cổng 25, 465, 587, 110, 143, 993, 995."},
			available: func(ctx context.Context) ([]string, error) {
				v, err := aptCandidate(ctx, "postfix")
				if err != nil {
					return nil, err
				}
				return []string{v}, nil
			},
			installed: func() []string {
				if !mail.Installed() {
					return nil
				}
				return aptInstalled("postfix")
			},
			install: func(ctx context.Context, _ string, logf func(string, ...any)) error {
				return mail.Install(ctx, logf)
			},
		},
		{
			info: agentproto.AppInfo{ID: "roundcube", Name: "Roundcube Webmail", Category: "mail",
				Description: "Đọc/gửi thư qua trình duyệt tại https://<máy chủ>:2096/webmail/ (cổng riêng, tách khỏi phiên đăng nhập panel; cần Mail server và PHP)."},
			available: mail.WebmailAvailable,
			installed: mail.WebmailInstalled,
			install: func(ctx context.Context, v string, logf func(string, ...any)) error {
				return mail.InstallWebmail(ctx, d, v, logf)
			},
		},
	}
	// Redis/Memcached (cacheApp) are not offered: one shared instance lets any
	// account read every other account's keys (sessions, cache), which breaks
	// account isolation. They come back as per-account instances.
	return c
}

func (c *Catalog) Refresh() {
	aptIndex.Lock()
	aptIndex.checked = time.Time{}
	aptIndex.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, hit := range c.cache {
		hit.expired = true
		c.cache[id] = hit
	}
}

// List returns every app with its installed and installable versions.
func (c *Catalog) List(ctx context.Context) []agentproto.AppInfo {
	out := make([]agentproto.AppInfo, 0, len(c.apps))
	for _, a := range c.apps {
		info := a.info
		info.Installed = a.installed()
		if info.Installed == nil {
			info.Installed = []string{}
		}
		info.Available, info.Error = c.availableCached(ctx, a)
		c.mu.Lock()
		checked := c.cache[a.info.ID]
		c.mu.Unlock()
		if !checked.at.IsZero() {
			info.CheckedAt = checked.at.UTC().Format(time.RFC3339)
		}
		info.Source = appSource(a.info.ID)
		if a.info.ID == "php" {
			info.VersionLabels = map[string]string{}
			info.InstalledLabels = map[string]string{}
			for _, v := range info.Available {
				if candidate, err := aptCandidate(ctx, "php"+v+"-fpm"); err == nil {
					info.VersionLabels[v] = v + " → " + candidate
				}
			}
			for _, v := range info.Installed {
				if installed := aptInstalled("php" + v + "-fpm"); len(installed) > 0 {
					info.InstalledLabels[v] = installed[0]
				}
			}
		}
		out = append(out, info)
	}
	return out
}

func (c *Catalog) availableCached(ctx context.Context, a app) ([]string, string) {
	c.mu.Lock()
	hit, ok := c.cache[a.info.ID]
	c.mu.Unlock()
	if ok && !hit.expired && time.Since(hit.at) < 5*time.Minute && hit.err == nil {
		return hit.versions, ""
	}
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	v, err := a.available(lctx)
	if v == nil {
		v = []string{}
	}
	c.mu.Lock()
	if err == nil {
		c.cache[a.info.ID] = cached{at: time.Now(), versions: v}
	}
	c.mu.Unlock()
	if err != nil {
		if ok {
			return hit.versions, "Không làm mới được; đang hiển thị dữ liệu cũ: " + err.Error()
		}
		return v, "không lấy được danh sách phiên bản: " + err.Error()
	}
	return v, ""
}

// Install installs one app version; the returned log is shown in the task.
func (c *Catalog) Install(ctx context.Context, id, version string) (string, error) {
	idx := slices.IndexFunc(c.apps, func(a app) bool { return a.info.ID == id })
	if idx < 0 {
		return "", fmt.Errorf("unknown app %q", id)
	}
	a := c.apps[idx]
	versions, errMsg := c.availableCached(ctx, a)
	if errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	if !slices.Contains(versions, version) {
		return "", fmt.Errorf("phiên bản %q của %s không có trong danh sách", version, a.info.Name)
	}
	var log strings.Builder
	logf := func(f string, args ...any) {
		fmt.Fprintf(&log, f, args...)
		if !strings.HasSuffix(log.String(), "\n") {
			log.WriteString("\n")
		}
	}
	err := a.install(ctx, version, logf)
	return log.String(), err
}

func goArch() string { return runtime.GOARCH } // amd64 | arm64

func mariadbVersion() []string {
	if installed := aptInstalled("mariadb-server"); len(installed) > 0 {
		return installed
	}
	out, err := run.Output(context.Background(), "mariadb", "--version")
	if err != nil {
		return nil
	}
	f := strings.Fields(out)
	for i, w := range f {
		if w == "Distrib" && i+1 < len(f) {
			v := strings.TrimSuffix(f[i+1], ",")
			if j := strings.Index(v, "-"); j > 0 {
				v = v[:j]
			}
			if strings.Count(v, ".") >= 1 && v[0] >= '1' && v[0] <= '9' {
				return []string{v}
			}
		}
	}
	return []string{"installed"}
}

// sortVersionsDesc sorts dotted numeric versions newest first.
func sortVersionsDesc(v []string) {
	sort.Slice(v, func(i, j int) bool { return compareVersions(v[i], v[j]) > 0 })
}

func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
