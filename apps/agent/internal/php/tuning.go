package php

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/run"
)

// WritePerformanceDefaults stays below the admin's 99-xpanel.ini overrides.
// Each site's separate master has its own OPcache, so keep this modest.
func WritePerformanceDefaults(version string) error {
	if _, err := os.Stat("/var/lib/xpanel/performance-profile.txt"); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	ram := 0
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[0] == "MemTotal:" {
				ram, _ = strconv.Atoi(f[1])
				ram /= 1024
			}
		}
	}
	cache := 32
	if ram >= 8192 {
		cache = 64
	}
	path := filepath.Join("/etc/php", version, "fpm/conf.d/98-xpanel-performance.ini")
	return os.WriteFile(path, []byte(fmt.Sprintf("; Managed by X-Panel; admin settings in 99-xpanel.ini take precedence.\nopcache.enable=1\nopcache.memory_consumption=%d\nopcache.interned_strings_buffer=4\nopcache.max_accelerated_files=10000\nopcache.validate_timestamps=1\nopcache.revalidate_freq=2\n", cache)), 0644)
}

// hostingIni lifts PHP's desktop-sized defaults (2 MB uploads, 30 s) to
// what hosting customers expect, matching nginx's 128 MB body limit and
// 300 s FastCGI timeout. The admin's 99-xpanel.ini and each site's own
// settings still take precedence.
const hostingIni = `; Managed by X-Panel: hosting defaults (99-xpanel.ini and site settings take precedence).
upload_max_filesize = 128M
post_max_size = 128M
memory_limit = 256M
max_execution_time = 300
max_input_time = 300
max_input_vars = 5000
`

func hostingIniPath(version string) string {
	return filepath.Join("/etc/php", version, "fpm/conf.d/97-xpanel-hosting.ini")
}

// WriteHostingDefaults installs the defaults for one version; it reports
// whether the file changed (running PHP masters must restart to see it).
func WriteHostingDefaults(version string) (bool, error) {
	path := hostingIniPath(version)
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return false, nil // FPM of this version is not installed
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == hostingIni {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(hostingIni), 0o644)
}

// MigrateHostingDefaults adds the defaults to PHP versions installed by
// older releases and restarts the PHP masters that must pick them up.
func MigrateHostingDefaults(ctx context.Context, d distro.Distro) error {
	changed := false
	for _, v := range Installed(d) {
		ch, err := WriteHostingDefaults(v)
		if err != nil {
			return err
		}
		if ch {
			changed = true
			_ = run.Cmd(ctx, "systemctl", "try-reload-or-restart", d.PHPFPMService(v))
		}
	}
	if !changed {
		return nil
	}
	out, err := run.Output(ctx, "systemctl", "list-units", "--type=service", "--no-legend", "--plain", "xpanel-app-*")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		// Per-site PHP masters are described as "X-Panel PHP <domain>".
		if len(f) > 0 && strings.Contains(line, "X-Panel PHP ") {
			_ = run.Cmd(ctx, "systemctl", "try-restart", f[0])
		}
	}
	return nil
}
