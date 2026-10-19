package apps

import (
	"context"
	"fmt"
	"strings"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

func aptInstalled(pkg string) []string {
	out, err := run.Output(context.Background(), "dpkg-query", "-W", "-f=${Status}\t${Version}", pkg)
	if err != nil || !strings.HasPrefix(out, "install ok installed\t") {
		return nil
	}
	return []string{strings.TrimSpace(strings.SplitN(out, "\t", 2)[1])}
}

func cacheApp(id, name, pkg string) app {
	return app{
		info: agentproto.AppInfo{ID: id, Name: name, Category: "cache", Description: "Cache dùng chung trên localhost; cài bản mới nhất từ kho hệ điều hành. Không có phân tách dữ liệu theo khách hàng, không dùng cho dữ liệu cần cách ly."},
		available: func(ctx context.Context) ([]string, error) {
			v, err := aptCandidate(ctx, pkg)
			if err != nil {
				return nil, err
			}
			return []string{v}, nil
		},
		installed: func() []string { return aptInstalled(pkg) },
		install: func(ctx context.Context, version string, logf func(string, ...any)) error {
			for _, args := range [][]string{{"-o", "DPkg::Lock::Timeout=600", "update", "-q"}, {"-o", "DPkg::Lock::Timeout=600", "install", "-y", "-q", pkg + "=" + version}} {
				out, err := run.With(ctx, aptOpts, "apt-get", args...)
				logf("%s", out)
				if err != nil {
					return err
				}
			}
			out, err := run.Output(ctx, "systemctl", "enable", "--now", pkg)
			logf("%s", out)
			if err != nil {
				return fmt.Errorf("start %s: %w", pkg, err)
			}
			return nil
		},
	}
}
