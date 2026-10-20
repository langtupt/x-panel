package php

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
