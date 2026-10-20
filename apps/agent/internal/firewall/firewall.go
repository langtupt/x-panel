// Package firewall protects the server from floods and password guessing:
// an nftables table with allow/deny lists, timed bans and per-IP connection
// limits; nginx request limits per IP; fail2ban jails that ban into the same
// table. Panel login bans come from the API through Ban.
package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/nginxctl"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

const (
	table      = "xpanel_fw"
	ConfigFile = "/etc/xpanel/firewall.json"
	reasonsDir = "/var/lib/xpanel/bans"
	nginxConf  = "/etc/nginx/conf.d/xpanel-ratelimit.conf"
	jailFile   = "/etc/fail2ban/jail.d/xpanel-firewall.local"
	actionFile = "/etc/fail2ban/action.d/xpanel.conf"
	agentBin   = "/opt/xpanel/bin/xpanel-agent"
)

var mu sync.Mutex

// Defaults is the configuration of a new install.
func Defaults() agentproto.FirewallConfig { return agentproto.DefaultFirewall() }

func clampInt(v, lo, hi int) error {
	if v < lo || v > hi {
		return fmt.Errorf("giá trị %d nằm ngoài khoảng %d–%d", v, lo, hi)
	}
	return nil
}

// Validate checks limits and turns every address into canonical form.
func Validate(c *agentproto.FirewallConfig) error {
	for _, r := range []struct{ v, lo, hi int }{
		{c.SynRate, 0, 100000}, {c.ConnLimit, 0, 100000}, {c.HTTPRate, 0, 10000}, {c.HTTPBurst, 0, 100000},
		{c.HTTPConn, 0, 10000}, {c.MaxRetry, 1, 1000}, {c.FindMin, 1, 10080}, {c.BanMin, 1, 525600},
		{c.PanelIPFails, 0, 1000}, {c.PanelBanMin, 1, 525600}, {c.AccountFails, 0, 1000}, {c.AccountLockMin, 1, 525600},
	} {
		if err := clampInt(r.v, r.lo, r.hi); err != nil {
			return err
		}
	}
	var err error
	if c.Allow, err = canonical(c.Allow); err != nil {
		return err
	}
	if c.Block, err = canonical(c.Block); err != nil {
		return err
	}
	return nil
}

func canonical(list []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		var v string
		if strings.Contains(s, "/") {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("địa chỉ không hợp lệ: %q", s)
			}
			v = p.Masked().String()
		} else {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("địa chỉ không hợp lệ: %q", s)
			}
			v = a.Unmap().String()
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
		if len(out) > 5000 {
			return nil, errors.New("danh sách quá dài (tối đa 5000)")
		}
	}
	sort.Strings(out)
	return out, nil
}

func split(list []string) (v4, v6 []string) {
	for _, s := range list {
		if strings.Contains(s, ":") {
			v6 = append(v6, s)
		} else {
			v4 = append(v4, s)
		}
	}
	return
}

func elements(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return " { " + strings.Join(list, ", ") + " }"
}

// ruleset rebuilds everything but keeps the ban sets (and their timers).
func ruleset(c agentproto.FirewallConfig) string {
	a4, a6 := split(c.Allow)
	d4, d6 := split(c.Block)
	var b strings.Builder
	t := "inet " + table
	fmt.Fprintf(&b, "add table %s\n", t)
	for _, s := range []struct{ name, typ, flags string }{
		{"allow4", "ipv4_addr", "interval"}, {"allow6", "ipv6_addr", "interval"},
		{"deny4", "ipv4_addr", "interval"}, {"deny6", "ipv6_addr", "interval"},
		{"ban4", "ipv4_addr", "timeout"}, {"ban6", "ipv6_addr", "timeout"},
		{"syn4", "ipv4_addr", "dynamic,timeout"}, {"syn6", "ipv6_addr", "dynamic,timeout"},
		{"conn4", "ipv4_addr", "dynamic"}, {"conn6", "ipv6_addr", "dynamic"},
	} {
		extra := ""
		if strings.HasPrefix(s.name, "syn") {
			extra = " timeout 1m;"
		}
		fmt.Fprintf(&b, "add set %s %s { type %s; flags %s;%s }\n", t, s.name, s.typ, s.flags, extra)
	}
	for _, s := range []string{"allow4", "allow6", "deny4", "deny6"} {
		fmt.Fprintf(&b, "flush set %s %s\n", t, s)
	}
	for name, list := range map[string][]string{"allow4": a4, "allow6": a6, "deny4": d4, "deny6": d6} {
		if len(list) > 0 {
			fmt.Fprintf(&b, "add element %s %s%s\n", t, name, elements(list))
		}
	}
	fmt.Fprintf(&b, "add chain %s input { type filter hook input priority -5; policy accept; }\n", t)
	fmt.Fprintf(&b, "flush chain %s input\n", t)
	rule := func(r string) { fmt.Fprintf(&b, "add rule %s input %s\n", t, r) }
	rule("iif lo accept")
	rule("ip saddr @allow4 accept")
	rule("ip6 saddr @allow6 accept")
	rule("ip saddr @deny4 drop")
	rule("ip6 saddr @deny6 drop")
	rule("ip saddr @ban4 drop")
	rule("ip6 saddr @ban6 drop")
	rule("ct state invalid drop")
	if c.SynRate > 0 {
		burst := c.SynRate * 2
		rule(fmt.Sprintf("tcp flags & (syn|ack) == syn ct state new update @syn4 { ip saddr limit rate over %d/second burst %d packets } drop", c.SynRate, burst))
		rule(fmt.Sprintf("tcp flags & (syn|ack) == syn ct state new update @syn6 { ip6 saddr limit rate over %d/second burst %d packets } drop", c.SynRate, burst))
	}
	if c.ConnLimit > 0 {
		rule(fmt.Sprintf("tcp dport { 80, 443 } ct state new add @conn4 { ip saddr ct count over %d } reject with tcp reset", c.ConnLimit))
		rule(fmt.Sprintf("tcp dport { 80, 443 } ct state new add @conn6 { ip6 saddr ct count over %d } reject with tcp reset", c.ConnLimit))
	}
	return b.String()
}

func nginxRateConf(c agentproto.FirewallConfig) string {
	if c.HTTPRate == 0 && c.HTTPConn == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Managed by X-Panel (Tường lửa): per-IP request limits.\ngeo $xp_limited {\n    default 1;\n    127.0.0.0/8 0;\n    ::1 0;\n")
	for _, a := range c.Allow {
		fmt.Fprintf(&b, "    %s 0;\n", a)
	}
	b.WriteString("}\nmap $xp_limited $xp_limit_key {\n    0 \"\";\n    1 $binary_remote_addr;\n}\n")
	if c.HTTPRate > 0 {
		fmt.Fprintf(&b, "limit_req_zone $xp_limit_key zone=xp_req:20m rate=%dr/s;\nlimit_req zone=xp_req", c.HTTPRate)
		// nginx rejects an explicit burst=0; omitting it means no burst.
		if c.HTTPBurst > 0 {
			fmt.Fprintf(&b, " burst=%d nodelay", c.HTTPBurst)
		}
		b.WriteString(";\n")
		b.WriteString("limit_req_status 429;\nlimit_req_log_level warn;\n")
	}
	if c.HTTPConn > 0 {
		fmt.Fprintf(&b, "limit_conn_zone $xp_limit_key zone=xp_conn:20m;\nlimit_conn xp_conn %d;\n", c.HTTPConn)
		b.WriteString("limit_conn_status 429;\nlimit_conn_log_level warn;\n")
	}
	return b.String()
}

const actionConf = `# Managed by X-Panel: fail2ban bans go into the X-Panel firewall.
[Definition]
actionstart =
actionstop =
actioncheck =
actionban = ` + agentBin + ` fwban ban <ip> <bantime> f2b-<name>
actionunban = ` + agentBin + ` fwban unban <ip>
`

func jailConf(c agentproto.FirewallConfig) string {
	var b strings.Builder
	b.WriteString("# Managed by X-Panel (Tường lửa)\n[DEFAULT]\nbanaction = xpanel\nbanaction_allports = xpanel\n")
	fmt.Fprintf(&b, "maxretry = %d\nfindtime = %dm\nbantime = %dm\n", c.MaxRetry, c.FindMin, c.BanMin)
	if c.Increment {
		b.WriteString("bantime.increment = true\nbantime.factor = 2\nbantime.maxtime = 1w\n")
	}
	b.WriteString("ignoreip = 127.0.0.1/8 ::1")
	for _, a := range c.Allow {
		b.WriteString(" " + a)
	}
	b.WriteString("\n")
	if c.FloodBan && c.HTTPRate > 0 {
		b.WriteString("\n[nginx-limit-req]\nenabled = true\nbackend = auto\nport = http,https\n" +
			"logpath = /var/log/nginx/error.log\n          /var/log/nginx/xpanel/*.error.log\n" +
			"maxretry = 20\nfindtime = 2m\nbantime = 30m\n")
	}
	return b.String()
}

func writeIfChanged(path, content string, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && string(old) == content {
		return false, nil
	}
	if content == "" {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), mode)
}

// Apply installs the configuration (and saves it for the next boot); it
// returns the configuration with addresses in canonical form.
func Apply(ctx context.Context, c agentproto.FirewallConfig) (agentproto.FirewallConfig, error) {
	if err := Validate(&c); err != nil {
		return c, err
	}
	mu.Lock()
	defer mu.Unlock()
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(ConfigFile, b, 0o600); err != nil {
		return c, err
	}
	return c, apply(ctx, c)
}

// Restore re-applies the saved configuration at agent start.
func Restore(ctx context.Context) error {
	b, err := os.ReadFile(ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	var c agentproto.FirewallConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	if err := Validate(&c); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return apply(ctx, c)
}

func apply(ctx context.Context, c agentproto.FirewallConfig) error {
	if !c.Enabled {
		_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "table", "inet", table)
		changed, err := writeIfChanged(nginxConf, "", 0o644)
		if err != nil {
			return err
		}
		if changed {
			if err := nginxctl.Reload(ctx); err != nil {
				return err
			}
		}
		if ch, _ := writeIfChanged(jailFile, "", 0o644); ch {
			_ = run.Cmd(ctx, "systemctl", "reload-or-restart", "fail2ban")
		}
		return nil
	}
	if _, err := run.With(ctx, run.Opts{Stdin: ruleset(c)}, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("nftables: %w", err)
	}
	if changed, err := writeIfChanged(nginxConf, nginxRateConf(c), 0o644); err != nil {
		return err
	} else if changed {
		if err := nginxctl.Reload(ctx); err != nil {
			// A bad limit must never take the websites down.
			_ = os.Remove(nginxConf)
			_ = nginxctl.Reload(ctx)
			return fmt.Errorf("nginx: %w", err)
		}
	}
	if _, err := os.Stat("/etc/fail2ban/jail.d"); err == nil {
		ch1, err := writeIfChanged(actionFile, actionConf, 0o644)
		if err != nil {
			return err
		}
		ch2, err := writeIfChanged(jailFile, jailConf(c), 0o644)
		if err != nil {
			return err
		}
		if ch1 || ch2 {
			if err := run.Cmd(ctx, "systemctl", "restart", "fail2ban"); err != nil {
				// e.g. no nginx error log yet: keep the other jails running.
				_ = os.WriteFile(jailFile, []byte(jailConf(agentproto.FirewallConfig{MaxRetry: c.MaxRetry, FindMin: c.FindMin,
					BanMin: c.BanMin, Increment: c.Increment, Allow: c.Allow})), 0o644)
				_ = run.Cmd(ctx, "systemctl", "restart", "fail2ban")
			}
		}
	}
	return nil
}

func parseIP(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return a, fmt.Errorf("IP không hợp lệ: %q", s)
	}
	return a.Unmap(), nil
}

func setFor(a netip.Addr) string {
	if a.Is4() {
		return "ban4"
	}
	return "ban6"
}

// Ban drops all traffic from ip for seconds (0 = until unbanned).
func Ban(ctx context.Context, p agentproto.FirewallBanParams) error {
	a, err := parseIP(p.IP)
	if err != nil {
		return err
	}
	if a.IsLoopback() {
		return errors.New("không chặn địa chỉ loopback")
	}
	reason := p.Reason
	if len(reason) > 80 || strings.ContainsAny(reason, "\n\r") {
		reason = reason[:min(len(reason), 80)]
	}
	elem := a.String()
	if p.Seconds > 0 {
		elem += " timeout " + strconv.Itoa(p.Seconds) + "s"
	}
	// Replace an existing ban (new timeout).
	_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "element", "inet", table, setFor(a), "{ "+a.String()+" }")
	if _, err := run.With(ctx, run.Opts{}, "nft", "add", "element", "inet", table, setFor(a), "{ "+elem+" }"); err != nil {
		return fmt.Errorf("tường lửa chưa bật: %w", err)
	}
	_ = os.MkdirAll(reasonsDir, 0o700)
	_ = os.WriteFile(filepath.Join(reasonsDir, a.String()), []byte(strings.ToValidUTF8(reason, "")), 0o600)
	return nil
}

func Unban(ctx context.Context, p agentproto.FirewallBanParams) error {
	a, err := parseIP(p.IP)
	if err != nil {
		return err
	}
	_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "element", "inet", table, setFor(a), "{ "+a.String()+" }")
	_ = os.Remove(filepath.Join(reasonsDir, a.String()))
	// fail2ban would otherwise re-add it on its next restart.
	if _, err := os.Stat("/usr/bin/fail2ban-client"); err == nil {
		_, _ = run.With(ctx, run.Opts{Timeout: 20 * time.Second}, "fail2ban-client", "unban", a.String())
	}
	return nil
}

// Bans lists the active timed and permanent bans.
func Bans(ctx context.Context) (agentproto.FirewallBans, error) {
	out := agentproto.FirewallBans{Bans: []agentproto.FirewallBan{}}
	for _, set := range []string{"ban4", "ban6"} {
		raw, err := run.Output(ctx, "nft", "-j", "list", "set", "inet", table, set)
		if err != nil {
			continue
		}
		out.Active = true
		var doc struct {
			Nftables []struct {
				Set *struct {
					Elem []json.RawMessage `json:"elem"`
				} `json:"set"`
			} `json:"nftables"`
		}
		if json.Unmarshal([]byte(raw), &doc) != nil {
			continue
		}
		now := time.Now()
		for _, n := range doc.Nftables {
			if n.Set == nil {
				continue
			}
			for _, e := range n.Set.Elem {
				var ip string
				var ex *time.Time
				var timed struct {
					Elem struct {
						Val     string `json:"val"`
						Expires int64  `json:"expires"`
					} `json:"elem"`
				}
				if json.Unmarshal(e, &ip) != nil {
					if json.Unmarshal(e, &timed) != nil || timed.Elem.Val == "" {
						continue
					}
					ip = timed.Elem.Val
					if timed.Elem.Expires > 0 {
						t := now.Add(time.Duration(timed.Elem.Expires) * time.Second)
						ex = &t
					}
				}
				reason, _ := os.ReadFile(filepath.Join(reasonsDir, filepath.Base(ip)))
				out.Bans = append(out.Bans, agentproto.FirewallBan{IP: ip, Reason: string(reason), ExpiresAt: ex})
			}
		}
	}
	sort.Slice(out.Bans, func(i, j int) bool { return out.Bans[i].IP < out.Bans[j].IP })
	return out, nil
}

// CLIMain is "xpanel-agent fwban ban <ip> <seconds> <reason>" or
// "xpanel-agent fwban unban <ip>", used by the fail2ban action.
func CLIMain(args []string) {
	ctx := context.Background()
	var err error
	switch {
	case len(args) >= 3 && args[0] == "ban":
		secs, _ := strconv.Atoi(args[2])
		if secs < 0 {
			secs = 0 // fail2ban: -1 = permanent
		}
		reason := "fail2ban"
		if len(args) >= 4 {
			reason = args[3]
		}
		err = Ban(ctx, agentproto.FirewallBanParams{IP: args[1], Seconds: secs, Reason: reason})
	case len(args) >= 2 && args[0] == "unban":
		a, perr := parseIP(args[1])
		if perr != nil {
			err = perr
			break
		}
		_, _ = run.With(ctx, run.Opts{}, "nft", "delete", "element", "inet", table, setFor(a), "{ "+a.String()+" }")
		_ = os.Remove(filepath.Join(reasonsDir, a.String()))
	default:
		err = errors.New("usage: fwban ban <ip> <seconds> [reason] | fwban unban <ip>")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
