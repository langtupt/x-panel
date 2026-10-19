// Package quota sets and reports Linux user disk quotas on the filesystem
// holding /home (ext4 "usrquota"/"quota" or XFS "uquota"/"usrquota").
package quota

import (
	"bufio"
	"context"
	"encoding/csv"
	"io"
	"os"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

var quotaOpts = map[string]bool{"usrquota": true, "quota": true, "uquota": true, "uqnoenforce": true}

// HomeMount returns the mount point containing /home and whether user
// quotas are enabled on it.
func HomeMount() (mount string, enabled bool) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", false
	}
	defer f.Close()
	return homeMount(f)
}

func homeMount(r io.Reader) (mount string, enabled bool) {
	target := accounts.HomeBase
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		mp := f[1]
		if !(mp == "/" || mp == target || strings.HasPrefix(target, mp+"/")) || len(mp) < len(mount) {
			continue
		}
		mount, enabled = mp, false
		for _, o := range strings.Split(f[3], ",") {
			if quotaOpts[o] || strings.HasPrefix(o, "usrjquota=") {
				enabled = true
			}
		}
	}
	return mount, enabled
}

// Set applies a hard limit (soft = hard). Returns false if quotas are off.
func Set(ctx context.Context, p agentproto.QuotaSetParams) (bool, error) {
	if _, err := accounts.Managed(p.Username); err != nil {
		return false, err
	}
	mount, on := HomeMount()
	if !on {
		return false, nil
	}
	blocks := strconv.Itoa(p.DiskMB * 1024) // 1 KiB blocks
	inodes := strconv.Itoa(p.Inodes)
	return true, run.Cmd(ctx, "setquota", "-u", p.Username, blocks, blocks, inodes, inodes, mount)
}

func Report(ctx context.Context) (agentproto.QuotaReport, error) {
	rep := agentproto.QuotaReport{Users: map[string]agentproto.QuotaUsage{}}
	rep.Mount, rep.Enabled = HomeMount()
	if !rep.Enabled {
		return rep, nil
	}
	out, err := run.Output(ctx, "repquota", "-u", "-O", "csv", rep.Mount)
	if err != nil {
		return rep, err
	}
	rep.Users, err = parseCSV(out)
	return rep, err
}

// parseCSV reads `repquota -O csv`: User,BlockStatus,FileStatus,BlockUsed(KiB),...,FileUsed,...
func parseCSV(out string) (map[string]agentproto.QuotaUsage, error) {
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		return nil, err
	}
	users := map[string]agentproto.QuotaUsage{}
	for i, r := range rows {
		if i == 0 || len(r) < 8 {
			continue
		}
		kb, _ := strconv.Atoi(r[3])
		files, _ := strconv.Atoi(r[7])
		users[r[0]] = agentproto.QuotaUsage{DiskUsedMB: (kb + 1023) / 1024, InodesUsed: files}
	}
	return users, nil
}
