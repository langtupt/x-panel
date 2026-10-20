package apps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"xpanel/apps/agent/internal/mail"
	"xpanel/apps/agent/internal/pma"
	"xpanel/apps/agent/internal/run"
)

// Remove only accepts installed catalog identifiers, never arbitrary paths or packages.
func (c *Catalog) Remove(ctx context.Context, id, version string) (string, error) {
	i := slices.IndexFunc(c.apps, func(a app) bool { return a.info.ID == id })
	if i < 0 || !slices.Contains(c.apps[i].installed(), version) {
		return "", fmt.Errorf("phiên bản chưa được cài")
	}
	switch id {
	case "nodejs", "go":
		root := NodeRoot
		if id == "go" {
			root = GoRoot
		}
		if filepath.Base(version) != version || strings.ContainsAny(version, `/\\`) || version == "." || version == ".." {
			return "", fmt.Errorf("phiên bản không hợp lệ")
		}
		target := filepath.Join(root, version)
		if filepath.Dir(target) != root {
			return "", fmt.Errorf("đường dẫn không hợp lệ")
		}
		services, _ := filepath.Glob("/etc/systemd/system/*.service")
		for _, service := range services {
			body, err := os.ReadFile(service)
			if err != nil {
				return "", err
			}
			if strings.Contains(string(body), target+"/") {
				return "", fmt.Errorf("runtime đang được sử dụng bởi service %s", filepath.Base(service))
			}
		}
		if err := os.RemoveAll(target); err != nil {
			return "", err
		}
		return "Đã xóa runtime " + version, nil
	case "phpmyadmin", "adminer":
		dir := "/opt/xpanel/" + id
		if err := pma.RemoveTool(ctx, id); err != nil {
			return "", err
		}
		return "Đã gỡ " + id, os.RemoveAll(dir)
	case "roundcube":
		return "Đã gỡ Roundcube", mail.RemoveWebmail(ctx, c.D)
	case "composer":
		return "Đã gỡ Composer", os.Remove(ComposerPath)
	case "php":
		// Every non-default FPM pool may serve a website or a panel tool.
		pools, err := filepath.Glob("/etc/php/" + version + "/fpm/pool.d/*.conf")
		if err != nil {
			return "", err
		}
		for _, pool := range pools {
			if filepath.Base(pool) != "www.conf" {
				return "", fmt.Errorf("PHP %s đang được sử dụng bởi pool %s", version, filepath.Base(pool))
			}
		}
		output, err := run.Output(ctx, "dpkg-query", "-W", "-f=${binary:Package} ${Status}\n", "php"+version+"*")
		if err != nil {
			return output, err
		}
		var packages []string
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 4 && strings.Join(fields[1:], " ") == "install ok installed" && strings.HasPrefix(fields[0], "php"+version) {
				packages = append(packages, fields[0])
			}
		}
		if len(packages) == 0 {
			return "", fmt.Errorf("không tìm thấy gói PHP")
		}
		return removePackages(ctx, packages)
	case "redis", "memcached":
		pkg := "memcached"
		if id == "redis" {
			pkg = "redis-server"
		}
		return removePackages(ctx, []string{pkg})
	default:
		return "", fmt.Errorf("ứng dụng được bảo vệ; chưa hỗ trợ gỡ an toàn")
	}
}

func removePackages(ctx context.Context, packages []string) (string, error) {
	// Refuse a removal plan that would remove other applications.
	plan, err := run.Output(ctx, "apt-get", append([]string{"-s", "remove", "--"}, packages...)...)
	if err != nil {
		return plan, err
	}
	for _, line := range strings.Split(plan, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == "Remv" && !slices.Contains(packages, fields[1]) {
			return plan, fmt.Errorf("không thể gỡ vì ảnh hưởng gói %s", fields[1])
		}
	}
	return run.With(ctx, run.Opts{Timeout: 15 * time.Minute, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}, "apt-get", append([]string{"-y", "remove", "--"}, packages...)...)
}
