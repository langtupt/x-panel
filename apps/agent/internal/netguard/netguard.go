// Package netguard keeps customers away from each other's application ports.
// Node.js/Next.js/Go apps listen on 127.0.0.1:<port>; without a rule any local
// account (PHP, cron…) could connect to them directly. An nftables table lets
// only nginx, the port's owner and root (agent health checks) reach a port.
package netguard

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"xpanel/apps/agent/internal/run"
)

const table = "xpanel_apps"

var mu sync.Mutex

// PortsFile is where siteapp records an account's application ports.
func PortsFile(stateDir string) string { return filepath.Join(stateDir, "ports") }

// WritePorts stores the account's ports (one per line).
func WritePorts(stateDir string, ports []int) error {
	sort.Ints(ports)
	var b strings.Builder
	for _, p := range ports {
		fmt.Fprintf(&b, "%d\n", p)
	}
	return os.WriteFile(PortsFile(stateDir), []byte(b.String()), 0o600)
}

type owned struct {
	port int
	uid  string
}

// collect reads every account's ports under root/<username>/ports.
func collect(root string) ([]owned, error) {
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []owned
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		u, err := user.Lookup(e.Name())
		if err != nil {
			continue // account deleted
		}
		b, err := os.ReadFile(PortsFile(filepath.Join(root, e.Name())))
		if err != nil {
			continue
		}
		for _, line := range strings.Fields(string(b)) {
			if p, err := strconv.Atoi(line); err == nil && p > 0 && p < 65536 {
				out = append(out, owned{port: p, uid: u.Uid})
			}
		}
	}
	return out, nil
}

// Render builds the nftables script that replaces the whole table atomically.
func Render(ports []owned, nginxUID string) string {
	var set, own []string
	seen := map[int]bool{}
	for _, o := range ports {
		if !seen[o.port] {
			seen[o.port] = true
			set = append(set, strconv.Itoa(o.port))
		}
		own = append(own, fmt.Sprintf("%d . %s", o.port, o.uid))
	}
	elems := func(e []string) string {
		if len(e) == 0 {
			return ""
		}
		return "\n\t\telements = { " + strings.Join(e, ", ") + " }"
	}
	return fmt.Sprintf(`add table inet %[1]s
delete table inet %[1]s
table inet %[1]s {
	set ports {
		type inet_service%[2]s
	}
	set owners {
		typeof tcp dport . meta skuid%[3]s
	}
	chain output {
		type filter hook output priority 0; policy accept;
		oifname "lo" tcp dport @ports jump guard
	}
	chain guard {
		meta skuid 0 accept
		meta skuid %[4]s accept
		tcp dport . meta skuid @owners accept
		counter reject with tcp reset
	}
}
`, table, elems(set), elems(own), nginxUID)
}

// Rebuild applies the rules for every account found under root.
func Rebuild(ctx context.Context, root, nginxUser string) error {
	mu.Lock()
	defer mu.Unlock()
	ports, err := collect(root)
	if err != nil {
		return err
	}
	ng, err := user.Lookup(nginxUser)
	if err != nil {
		return err
	}
	if _, err := run.Output(ctx, "nft", "--version"); err != nil {
		return fmt.Errorf("cần nftables để cách ly cổng ứng dụng giữa các tài khoản (apt install nftables): %w", err)
	}
	return run.CmdStdin(ctx, Render(ports, ng.Uid), "nft", "-f", "-")
}
