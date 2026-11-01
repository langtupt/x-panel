package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

// matchList returns the first entry of list (address or network) covering a.
func matchList(a netip.Addr, list []string) string {
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(a) {
			return s
		}
		if b, err := netip.ParseAddr(s); err == nil && b.Unmap() == a {
			return s
		}
	}
	return ""
}

func savedConfig() (agentproto.FirewallConfig, bool) {
	var c agentproto.FirewallConfig
	b, err := os.ReadFile(ConfigFile)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}

var jailListRe = regexp.MustCompile(`Jail list:\s*(.*)`)

// fail2banJails lists the jails that currently hold a ban on ip.
func fail2banJails(ctx context.Context, ip string) []string {
	if _, err := exec.LookPath("fail2ban-client"); err != nil {
		return nil
	}
	out, err := run.With(ctx, run.Opts{Timeout: 15 * time.Second}, "fail2ban-client", "status")
	if err != nil {
		return nil
	}
	m := jailListRe.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	var jails []string
	for _, j := range strings.Split(m[1], ",") {
		j = strings.TrimSpace(j)
		if j == "" || strings.ContainsAny(j, " /\\") {
			continue
		}
		st, err := run.With(ctx, run.Opts{Timeout: 15 * time.Second}, "fail2ban-client", "status", j)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(st, "\n") {
			if i := strings.Index(line, "Banned IP list:"); i >= 0 {
				for _, b := range strings.Fields(line[i+len("Banned IP list:"):]) {
					if b == ip {
						jails = append(jails, j)
					}
				}
			}
		}
	}
	return jails
}

var tableRe = regexp.MustCompile(`(?m)^table (\S+) (\S+)`)

// otherTables lists nftables tables other than X-Panel's that mention ip
// (fail2ban's own nftables action, ufw, rules added by hand…).
func otherTables(ctx context.Context, ip string) []string {
	out, err := run.Output(ctx, "nft", "list", "tables")
	if err != nil {
		return nil
	}
	token := regexp.MustCompile(`(^|[^0-9a-fA-F.:])` + regexp.QuoteMeta(ip) + `($|[^0-9a-fA-F.:/])`)
	var found []string
	for _, m := range tableRe.FindAllStringSubmatch(out, -1) {
		if m[2] == table {
			continue
		}
		body, err := run.Output(ctx, "nft", "list", "table", m[1], m[2])
		if err == nil && token.MatchString(body) {
			found = append(found, m[1]+" "+m[2])
		}
	}
	return found
}

// Check reports everything on this server that blocks or exempts ip.
func Check(ctx context.Context, p agentproto.FirewallBanParams) (agentproto.FirewallCheck, error) {
	a, err := parseIP(p.IP)
	if err != nil {
		return agentproto.FirewallCheck{}, err
	}
	res := agentproto.FirewallCheck{IP: a.String(), Fail2ban: []string{}, OtherTables: []string{}}
	if _, err := run.Output(ctx, "nft", "list", "table", "inet", table); err == nil {
		res.FirewallActive = true
		if _, err := run.Output(ctx, "nft", "get", "element", "inet", table, setFor(a), "{ "+a.String()+" }"); err == nil {
			res.Banned = true
			res.BanReason = readBan(a.String()).Reason
		}
	}
	if c, ok := savedConfig(); ok {
		res.DenyMatch = matchList(a, c.Block)
		res.AllowMatch = matchList(a, c.Allow)
	}
	if j := fail2banJails(ctx, a.String()); j != nil {
		res.Fail2ban = j
	}
	if t := otherTables(ctx, a.String()); t != nil {
		res.OtherTables = t
	}
	return res, nil
}

// clearMeters drops ip from the SYN-rate and connection-count meters and
// forgets its tracked connections, so it starts from a clean slate.
func clearMeters(ctx context.Context, a netip.Addr) {
	v := "4"
	if !a.Is4() {
		v = "6"
	}
	for _, set := range []string{"syn" + v, "conn" + v} {
		_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "element", "inet", table, set, "{ "+a.String()+" }")
	}
	if _, err := exec.LookPath("conntrack"); err == nil {
		_, _ = run.With(ctx, run.Opts{Timeout: 10 * time.Second}, "conntrack", "-D", "-s", a.String())
	}
}

// Unban lifts every ban on ip: the X-Panel sets and record, fail2ban (all
// jails, which also removes it from fail2ban's database and its own tables)
// and the flood meters. It returns what still blocks the address afterwards
// (e.g. an "always block" network).
func Unban(ctx context.Context, p agentproto.FirewallBanParams) (agentproto.FirewallCheck, error) {
	a, err := parseIP(p.IP)
	if err != nil {
		return agentproto.FirewallCheck{}, err
	}
	_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "element", "inet", table, setFor(a), "{ "+a.String()+" }")
	if err := os.Remove(filepath.Join(reasonsDir, a.String())); err != nil && !errors.Is(err, os.ErrNotExist) {
		return agentproto.FirewallCheck{}, err
	}
	if _, err := exec.LookPath("fail2ban-client"); err == nil {
		_, _ = run.With(ctx, run.Opts{Timeout: 20 * time.Second}, "fail2ban-client", "unban", a.String())
	}
	clearMeters(ctx, a)
	return Check(ctx, p)
}
