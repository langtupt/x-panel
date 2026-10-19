package apps

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/fetch"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

var aptIndex struct {
	sync.Mutex
	checked time.Time
	err     error
}

func refreshAptIndex(ctx context.Context) error {
	aptIndex.Lock()
	defer aptIndex.Unlock()
	if time.Since(aptIndex.checked) < 5*time.Minute {
		return aptIndex.err
	}
	_, err := run.With(ctx, run.Opts{Timeout: 45 * time.Second, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}, "apt-get", "-o", "DPkg::Lock::Timeout=15", "-o", "APT::Update::Error-Mode=any", "update", "-q")
	aptIndex.checked = time.Now()
	aptIndex.err = err
	return err
}

// PHP's stable release feed excludes RC/beta packages present in the PPA.
func phpAvailable(ctx context.Context, d distro.Distro) ([]string, error) {
	var release struct {
		Supported []string `json:"supported_versions"`
		Version   string   `json:"version"`
	}
	if err := fetch.JSON(ctx, "https://www.php.net/releases/index.php?json&version=8", &release); err != nil {
		return nil, err
	}
	if len(release.Supported) == 0 {
		return nil, fmt.Errorf("PHP release feed is empty")
	}
	versions := append([]string{"7.4", "8.0", "8.1"}, release.Supported...)
	var out []string
	for _, v := range versions {
		if !agentproto.ValidPHPVersion(v) {
			continue
		}
		// Installing the first version adds the repository that carries all of them.
		if !php.RepoConfigured(d) {
			out = append(out, v)
			continue
		}
		if _, err := aptCandidate(ctx, "php"+v+"-fpm"); err == nil {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("không tìm thấy bản PHP stable trong chỉ mục APT hiện tại")
	}
	sortVersionsDesc(out)
	return slices.Compact(out), nil
}

func aptCandidate(ctx context.Context, pkg string) (string, error) {
	if err := refreshAptIndex(ctx); err != nil {
		return "", fmt.Errorf("không cập nhật được chỉ mục APT: %w", err)
	}
	text, err := run.Output(ctx, "apt-cache", "policy", pkg)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "Candidate:" && f[1] != "(none)" {
			return f[1], nil
		}
	}
	return "", fmt.Errorf("%s chưa có trong kho APT; cần cấu hình/cập nhật kho gói", pkg)
}

func appSource(id string) string {
	switch id {
	case "php":
		return "php.net + APT (Sury/Ondřej); bản vá mới nhất của dòng PHP, loại trừ RC/beta"
	case "nodejs":
		return "nodejs.org; bản vá mới nhất của dòng Current/LTS đã chọn"
	case "go":
		return "go.dev; các bản stable đang được phát hành"
	case "postgresql":
		return "postgresql.org / PGDG; bản vá mới nhất của dòng đã chọn"
	case "composer":
		return "getcomposer.org"
	case "phpmyadmin":
		return "phpmyadmin.net"
	case "adminer":
		return "github.com/vrana/adminer"
	default:
		return "Kho APT của hệ điều hành (bao gồm bản vá bảo mật); có thể khác bản upstream"
	}
}

var numericVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}$`)

func composerAvailable(ctx context.Context) ([]string, error) {
	var feed struct {
		Stable []struct {
			Version string `json:"version"`
		} `json:"stable"`
	}
	if err := fetch.JSON(ctx, "https://getcomposer.org/versions", &feed); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range feed.Stable {
		if numericVersion.MatchString(r.Version) {
			out = append(out, r.Version)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty Composer feed")
	}
	return out, nil
}

func pmaAvailable(ctx context.Context) ([]string, error) {
	var feed struct {
		Version string `json:"version"`
	}
	if err := fetch.JSON(ctx, "https://www.phpmyadmin.net/home_page/version.json", &feed); err != nil {
		return nil, err
	}
	if !numericVersion.MatchString(feed.Version) {
		return nil, fmt.Errorf("invalid phpMyAdmin version")
	}
	return []string{feed.Version}, nil
}

func adminerAvailable(ctx context.Context) ([]string, error) {
	var feed ghRelease
	if err := fetch.JSON(ctx, "https://api.github.com/repos/vrana/adminer/releases/latest", &feed); err != nil {
		return nil, err
	}
	v := strings.TrimPrefix(feed.Tag, "v")
	if !numericVersion.MatchString(v) {
		return nil, fmt.Errorf("invalid Adminer version")
	}
	return []string{v}, nil
}

func detectedFileVersion(path, pattern string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{"installed (version unknown)"}
	}
	if m := regexp.MustCompile(pattern).FindSubmatch(b); len(m) > 1 {
		return []string{string(m[1])}
	}
	return []string{"installed (version unknown)"}
}
