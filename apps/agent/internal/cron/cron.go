// Package cron runs a hosting account's scheduled commands as systemd
// timers instead of a crontab, so each job gets the same sandbox as the
// account's apps: its CPU/RAM slice, a private /tmp, no view of other
// accounts' processes and a read-only system. Timers of a suspended account
// are disabled.
package cron

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	StateRoot = "/etc/xpanel-cron"
	unitDir   = "/etc/systemd/system"
)

var dowNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// field expands one cron field into its values; star reports an unrestricted field.
func field(s string, lo, hi int) (vals []int, star bool, err error) {
	set := map[int]bool{}
	for _, item := range strings.Split(s, ",") {
		step := 1
		if base, st, ok := strings.Cut(item, "/"); ok {
			if step, err = strconv.Atoi(st); err != nil || step < 1 {
				return nil, false, fmt.Errorf("bước %q không hợp lệ", st)
			}
			item = base
		}
		from, to := lo, hi
		switch {
		case item == "*":
			if step == 1 && len(strings.Split(s, ",")) == 1 {
				return nil, true, nil
			}
		case strings.Contains(item, "-"):
			a, b, _ := strings.Cut(item, "-")
			if from, err = strconv.Atoi(a); err != nil {
				return nil, false, err
			}
			if to, err = strconv.Atoi(b); err != nil {
				return nil, false, err
			}
		default:
			if from, err = strconv.Atoi(item); err != nil {
				return nil, false, err
			}
			to = from
			if step > 1 { // "5/15" means from 5 to the end
				to = hi
			}
		}
		if from < lo || to > hi || from > to {
			return nil, false, fmt.Errorf("giá trị %q ngoài khoảng %d-%d", item, lo, hi)
		}
		for v := from; v <= to; v += step {
			set[v] = true
		}
	}
	for v := range set {
		vals = append(vals, v)
	}
	sort.Ints(vals)
	return vals, false, nil
}

func join(vals []int, format func(int) string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = format(v)
	}
	return strings.Join(parts, ",")
}

// OnCalendar converts a crontab schedule into systemd OnCalendar expressions
// (several when cron's "day-of-month OR day-of-week" rule applies).
func OnCalendar(schedule string) ([]string, error) {
	switch strings.TrimSpace(schedule) {
	case "@hourly":
		return []string{"*-*-* *:00:00"}, nil
	case "@daily", "@midnight":
		return []string{"*-*-* 00:00:00"}, nil
	case "@weekly":
		return []string{"Sun *-*-* 00:00:00"}, nil
	case "@monthly":
		return []string{"*-*-01 00:00:00"}, nil
	case "@yearly", "@annually":
		return []string{"*-01-01 00:00:00"}, nil
	}
	f := strings.Fields(schedule)
	if len(f) != 5 {
		return nil, fmt.Errorf("lịch chạy phải có 5 trường")
	}
	num := strconv.Itoa
	str := func(vals []int, star bool) string {
		if star {
			return "*"
		}
		return join(vals, num)
	}
	min, minStar, err := field(f[0], 0, 59)
	if err != nil {
		return nil, err
	}
	hour, hourStar, err := field(f[1], 0, 23)
	if err != nil {
		return nil, err
	}
	dom, domStar, err := field(f[2], 1, 31)
	if err != nil {
		return nil, err
	}
	mon, monStar, err := field(f[3], 1, 12)
	if err != nil {
		return nil, err
	}
	dow, dowStar, err := field(f[4], 0, 7)
	if err != nil {
		return nil, err
	}
	days := map[string]bool{}
	for _, d := range dow {
		days[dowNames[d%7]] = true
	}
	var dowList []string
	for _, n := range dowNames {
		if days[n] {
			dowList = append(dowList, n)
		}
	}
	timePart := fmt.Sprintf("%s:%s:00", str(hour, hourStar), str(min, minStar))
	date := func(domPart string) string { return fmt.Sprintf("*-%s-%s", str(mon, monStar), domPart) }
	switch {
	case domStar && dowStar:
		return []string{date("*") + " " + timePart}, nil
	case dowStar:
		return []string{date(str(dom, false)) + " " + timePart}, nil
	case domStar:
		return []string{strings.Join(dowList, ",") + " " + date("*") + " " + timePart}, nil
	default: // cron runs when EITHER day field matches
		return []string{
			date(str(dom, false)) + " " + timePart,
			strings.Join(dowList, ",") + " " + date("*") + " " + timePart,
		}, nil
	}
}

func unitName(user string, i int, j agentproto.CronJob) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", i, j.Schedule, j.Command)))
	return fmt.Sprintf("xpanel-cron-%s-%x", user, h[:6])
}

// Sync replaces the account's timers with jobs (empty = remove all).
func Sync(ctx context.Context, p agentproto.CronSyncParams) error {
	u, err := accounts.Managed(p.Username)
	if err != nil {
		return err
	}
	// Jobs from earlier versions lived in the account's crontab.
	_ = run.Cmd(ctx, "crontab", "-u", p.Username, "-r")

	// Job scripts often carry secrets (tokens, DB passwords): only root and the
	// account itself may read them, never the other hosting accounts.
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	// The jobs run as the account, which must be able to traverse StateRoot
	// to reach its own directory; 0711 still hides the other accounts' names.
	if err := os.MkdirAll(StateRoot, 0o711); err != nil {
		return err
	}
	if err := os.Chmod(StateRoot, 0o711); err != nil {
		return err
	}
	dir := filepath.Join(StateRoot, p.Username)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.Chown(dir, 0, gid); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return err
	}
	want := map[string]bool{}
	for i, j := range p.Jobs {
		if err := validate.CronSchedule(j.Schedule); err != nil {
			return err
		}
		if err := validate.CronCommand(j.Command); err != nil {
			return err
		}
		cal, err := OnCalendar(j.Schedule)
		if err != nil {
			return err
		}
		name := unitName(p.Username, i, j)
		want[name] = true
		script := filepath.Join(dir, name+".sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+j.Command+"\n"), 0o640); err != nil {
			return err
		}
		if err := os.Chown(script, 0, gid); err != nil {
			return err
		}
		if err := os.Chmod(script, 0o640); err != nil {
			return err
		}
		service := fmt.Sprintf(`# Managed by X-Panel: cron job of %[1]s.
[Unit]
Description=X-Panel cron job of %[1]s
[Service]
Type=oneshot
User=%[1]s
Group=%[1]s
Slice=xpanel-%[1]s.slice
WorkingDirectory=%[2]s
Environment=HOME=%[2]s PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/bin/sh %[3]s
TimeoutStartSec=6h
NoNewPrivileges=true
PrivateTmp=true
ProtectProc=invisible
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%[2]s
UMask=0027
`, p.Username, u.HomeDir, script)
		var timer strings.Builder
		fmt.Fprintf(&timer, "# Managed by X-Panel: cron job of %s.\n[Unit]\nDescription=X-Panel cron timer of %s\n[Timer]\n", p.Username, p.Username)
		for _, c := range cal {
			fmt.Fprintf(&timer, "OnCalendar=%s\n", c)
		}
		timer.WriteString("AccuracySec=1s\n[Install]\nWantedBy=timers.target\n")
		if err := os.WriteFile(filepath.Join(unitDir, name+".service"), []byte(service), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(unitDir, name+".timer"), []byte(timer.String()), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			return err
		}
	}
	if err := run.Cmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	// Remove timers that are no longer wanted.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".") || want[name] {
			continue
		}
		_ = run.Cmd(ctx, "systemctl", "disable", "--now", name+".timer")
		_ = run.Cmd(ctx, "systemctl", "stop", name+".service")
		for _, f := range []string{filepath.Join(unitDir, name+".timer"), filepath.Join(unitDir, name+".service"),
			filepath.Join(dir, name+".sh"), filepath.Join(dir, name)} {
			_ = os.Remove(f)
		}
	}
	if err := run.Cmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	return SetActive(ctx, p.Username, !accounts.Locked(p.Username))
}

// SetActive enables or disables (suspension) every timer of the account.
func SetActive(ctx context.Context, username string, active bool) error {
	entries, _ := os.ReadDir(filepath.Join(StateRoot, username))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".") {
			continue
		}
		args := []string{"disable", "--now", e.Name() + ".timer"}
		if active {
			args[0] = "enable"
		}
		if err := run.Cmd(ctx, "systemctl", args...); err != nil {
			return err
		}
	}
	return nil
}

// unitOf checks that ref names a timer this account has (as last synced) and
// returns its unit name.
func unitOf(ref agentproto.CronJobRef) (string, error) {
	if _, err := accounts.Managed(ref.Username); err != nil {
		return "", err
	}
	if ref.Index < 0 || ref.Index >= len(ref.Jobs) {
		return "", fmt.Errorf("không tìm thấy cron job")
	}
	name := unitName(ref.Username, ref.Index, ref.Jobs[ref.Index])
	if _, err := os.Stat(filepath.Join(StateRoot, ref.Username, name)); err != nil {
		return "", fmt.Errorf("cron job chưa được cài, hãy lưu lại")
	}
	return name, nil
}

// unixTime turns systemctl's "@<seconds>" timestamps into RFC 3339.
func unixTime(v string) string {
	n, err := strconv.ParseInt(strings.TrimPrefix(v, "@"), 10, 64)
	if err != nil || n == 0 {
		return ""
	}
	return time.Unix(n, 0).UTC().Format(time.RFC3339)
}

// Status reports the last run of a job and the end of its output.
func Status(ctx context.Context, ref agentproto.CronJobRef) (agentproto.CronStatus, error) {
	var st agentproto.CronStatus
	name, err := unitOf(ref)
	if err != nil {
		return st, err
	}
	out, err := run.Output(ctx, "systemctl", "show", "--timestamp=unix", name+".service",
		"-p", "ActiveState,Result,ExecMainStatus,ExecMainStartTimestamp,ExecMainExitTimestamp")
	if err != nil {
		return st, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	st.Running = props["ActiveState"] == "activating" || props["ActiveState"] == "active"
	st.LastRun = unixTime(props["ExecMainStartTimestamp"])
	st.ExitCode, _ = strconv.Atoi(props["ExecMainStatus"])
	if st.LastRun != "" && !st.Running {
		st.Finished = unixTime(props["ExecMainExitTimestamp"])
		st.Result = "failed"
		if props["Result"] == "success" && st.ExitCode == 0 {
			st.Result = "success"
		}
	}
	// Only the job's own output (and systemd's messages about it).
	logs, _ := run.Output(ctx, "journalctl", "-u", name+".service", "-n", "60", "-o", "short-iso", "--no-pager", "-q")
	if len(logs) > 16384 {
		logs = logs[len(logs)-16384:]
	}
	st.Log = logs
	return st, nil
}

// Run starts a job now in the background, in its usual sandbox.
func Run(ctx context.Context, ref agentproto.CronJobRef) error {
	name, err := unitOf(ref)
	if err != nil {
		return err
	}
	if accounts.Locked(ref.Username) {
		return fmt.Errorf("tài khoản đang bị khóa")
	}
	return run.Cmd(ctx, "systemctl", "start", "--no-block", name+".service")
}
