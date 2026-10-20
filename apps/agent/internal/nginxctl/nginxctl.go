// Package nginxctl reloads nginx and waits for the new configuration.
package nginxctl

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"xpanel/apps/agent/internal/run"
)

const reloadWait = 10 * time.Second

// Reload reloads nginx and returns once the master has started workers
// with the new configuration. `systemctl reload` only sends SIGHUP, so
// without this the API would report success while the old config still serves.
func Reload(ctx context.Context) error {
	master := nginxMaster()
	before := map[int]bool{}
	for _, pid := range children(master, "nginx: worker process") {
		before[pid] = true
	}
	if err := run.Cmd(ctx, "systemctl", "reload", "nginx"); err != nil {
		return err
	}
	if master == 0 {
		return nil // nginx was not running; systemctl started it
	}
	// Done when new workers run and every old worker has either exited or
	// switched to "is shutting down" (it has closed its listening sockets).
	deadline := time.Now().Add(reloadWait)
	for time.Now().Before(deadline) {
		fresh, stale := false, false
		for _, pid := range children(master, "nginx: worker process") {
			if !before[pid] {
				fresh = true
			} else if !strings.Contains(cmdline(pid), "shutting down") {
				stale = true
			}
		}
		if fresh && !stale {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("nginx did not pick up the new configuration within %s", reloadWait)
}

func cmdline(pid int) string {
	b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	return string(b)
}

// WaitSocket waits for a PHP-FPM pool socket to appear after a reload.
// WaitSocket waits until a unix socket accepts connections.
func WaitSocket(path string) error {
	deadline := time.Now().Add(reloadWait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("php-fpm socket %s did not appear", path)
}

func nginxMaster() int {
	b, err := os.ReadFile("/run/nginx.pid")
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// children lists processes whose parent is ppid and whose cmdline starts with prefix.
func children(ppid int, prefix string) []int {
	if ppid == 0 {
		return nil
	}
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Fields after the ")" that closes the command name: state ppid ...
		rest := string(stat)[strings.LastIndexByte(string(stat), ')')+1:]
		f := strings.Fields(rest)
		if len(f) < 2 || f[1] != strconv.Itoa(ppid) {
			continue
		}
		cmd, _ := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if strings.HasPrefix(string(cmd), prefix) {
			out = append(out, pid)
		}
	}
	return out
}
