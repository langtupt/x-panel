// Package stats measures resource use per hosting account: CPU, memory and
// tasks of the account's systemd slice (every PHP-FPM pool, app, build and
// cron job of the account runs there), stored mail, and the bytes its
// websites served (read incrementally from the nginx access logs).
package stats

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	cgroupRoot  = "/sys/fs/cgroup"
	logDir      = "/var/log/nginx/xpanel"
	mailRoot    = "/var/mail/vhosts"
	offsetsFile = "/var/lib/xpanel/stats-offsets.json"
	// A single collection never reads more than this from one log, so a
	// huge backlog cannot stall the agent; the rest is read next time.
	maxReadPerLog = 256 << 20
)

var mu sync.Mutex

type logOffset struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

// Collect measures every listed account.
func Collect(ctx context.Context, p agentproto.StatsParams) (agentproto.StatsResult, error) {
	mu.Lock()
	defer mu.Unlock()
	res := agentproto.StatsResult{At: time.Now().UTC(), Accounts: map[string]agentproto.AccountStatSample{}}
	offsets := loadOffsets()
	seen := map[string]bool{}
	for _, a := range p.Accounts {
		if err := validate.Username(a.Username); err != nil {
			return res, err
		}
		var s agentproto.AccountStatSample
		if dir := sliceDir(ctx, a.Username); dir != "" {
			s.CPUUsageUsec = cpuUsage(dir)
			s.MemoryBytes = readUint(filepath.Join(dir, "memory.current"))
			s.Tasks = int(readUint(filepath.Join(dir, "pids.current")))
		}
		for _, d := range a.MailDomains {
			if validate.Domain(d) == nil {
				s.MailBytes += dirSize(filepath.Join(mailRoot, d))
			}
		}
		for _, d := range a.Domains {
			if validate.Domain(d) != nil {
				continue
			}
			{
				name := d + ".access.log"
				path := filepath.Join(logDir, name)
				seen[path] = true
				bytes, reqs := readLog(path, offsets)
				s.BandwidthBytes += bytes
				s.Requests += reqs
			}
		}
		res.Accounts[a.Username] = s
	}
	// Forget logs of deleted sites.
	for path := range offsets {
		if !seen[path] {
			delete(offsets, path)
		}
	}
	saveOffsets(offsets)
	return res, nil
}

func sliceDir(ctx context.Context, username string) string {
	out, err := run.Output(ctx, "systemctl", "show", "-p", "ControlGroup", "--value", "xpanel-"+username+".slice")
	cg := strings.TrimSpace(out)
	if err != nil || cg == "" || strings.Contains(cg, "..") {
		return ""
	}
	dir := filepath.Join(cgroupRoot, cg)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

func cpuUsage(dir string) uint64 {
	f, err := os.Open(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "usage_usec "); ok {
			n, _ := strconv.ParseUint(v, 10, 64)
			return n
		}
	}
	return 0
}

func readUint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// readLog sums the response bytes of log lines written since the last call.
// A new file (rotation) starts from its beginning; a file seen for the first
// time starts at its end, so old history is not counted as new traffic.
func readLog(path string, offsets map[string]logOffset) (bytes, requests int64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, 0
	}
	inode := inodeOf(info)
	prev, known := offsets[path]
	start := info.Size()
	switch {
	case known && prev.Inode == inode && prev.Offset <= info.Size():
		start = prev.Offset
	case known: // rotated or truncated
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, 0
	}
	r := bufio.NewReaderSize(io.LimitReader(f, maxReadPerLog), 64<<10)
	pos := start
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			// An incomplete last line is read again next time.
			break
		}
		pos += int64(len(line))
		if n, ok := responseBytes(line); ok {
			bytes += n
			requests++
		}
	}
	offsets[path] = logOffset{Inode: inode, Offset: pos}
	return bytes, requests
}

// responseBytes extracts $body_bytes_sent from a "combined" log line:
// ip - user [time] "request" status bytes "referer" "agent"
func responseBytes(line string) (int64, bool) {
	parts := strings.SplitN(line, `"`, 4)
	if len(parts) < 3 {
		return 0, false
	}
	fields := strings.Fields(parts[2])
	if len(fields) < 2 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, fields[1] == "-"
	}
	return n, true
}

func loadOffsets() map[string]logOffset {
	out := map[string]logOffset{}
	if b, err := os.ReadFile(offsetsFile); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func saveOffsets(m map[string]logOffset) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	tmp := offsetsFile + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		if err := os.Rename(tmp, offsetsFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = os.Remove(tmp)
		}
	}
}
