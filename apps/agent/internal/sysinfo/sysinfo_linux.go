//go:build linux

package sysinfo

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"xpanel/apps/agent/internal/run"

	"xpanel/pkg/agentproto"
)

// Collect fills the dynamic parts of SystemInfo from /proc and statfs.
func Collect(info *agentproto.SystemInfo) error {
	info.Hostname, _ = os.Hostname()
	info.IPAddresses = []string{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.IsGlobalUnicast() {
				info.IPAddresses = append(info.IPAddresses, ip.String())
			}
		}
	}
	sort.Strings(info.IPAddresses)
	info.CPUCount = runtime.NumCPU()
	info.Architecture = runtime.GOARCH
	if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		info.BootID = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if key, v, ok := strings.Cut(line, ":"); ok && (strings.TrimSpace(key) == "model name" || strings.TrimSpace(key) == "Hardware") {
				info.CPUModel = strings.TrimSpace(v)
				break
			}
		}
	}
	total1, idle1 := cpuTimes()
	time.Sleep(150 * time.Millisecond)
	total2, idle2 := cpuTimes()
	if total2 > total1 && idle2 >= idle1 {
		info.CPUPercent = 100 * (1 - float64(idle2-idle1)/float64(total2-total1))
	}
	if b, err := os.ReadFile("/proc/net/dev"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			name, v, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(name) == "lo" {
				continue
			}
			f := strings.Fields(v)
			if len(f) >= 16 {
				rx, _ := strconv.ParseUint(f[0], 10, 64)
				tx, _ := strconv.ParseUint(f[8], 10, 64)
				info.NetworkRXBytes += rx
				info.NetworkTXBytes += tx
			}
		}
	}

	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		info.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			info.UptimeSec = uint64(up)
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		for i := 0; i < 3 && i < len(f); i++ {
			info.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}

	mem, err := readMeminfo()
	if err != nil {
		return err
	}
	info.MemTotalMB = mem["MemTotal"] / 1024
	info.MemUsedMB = (mem["MemTotal"] - mem["MemAvailable"]) / 1024
	info.SwapTotalMB = mem["SwapTotal"] / 1024
	info.SwapUsedMB = (mem["SwapTotal"] - mem["SwapFree"]) / 1024

	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return err
	}
	bs := uint64(st.Bsize)
	info.DiskTotalMB = st.Blocks * bs / 1024 / 1024
	info.DiskUsedMB = (st.Blocks - st.Bfree) * bs / 1024 / 1024
	info.DiskAvailableMB = st.Bavail * bs / 1024 / 1024
	return nil
}

func cpuTimes() (total, idle uint64) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	for i := 1; i < len(f) && i <= 8; i++ {
		n, _ := strconv.ParseUint(f[i], 10, 64)
		total += n
		if i == 4 || i == 5 {
			idle += n
		}
	}
	return
}

// Scheduling lets the API return and persist its audit entry before shutdown.
func Reboot(ctx context.Context) error {
	return scheduleReboot(ctx, run.Output)
}

func scheduleReboot(ctx context.Context, command func(context.Context, string, ...string) (string, error)) error {
	out, err := command(ctx, "systemd-run", "--unit=xpanel-reboot", "--on-active=10s", "--timer-property=AccuracySec=1s", "/usr/bin/systemctl", "reboot")
	if err != nil {
		return fmt.Errorf("schedule reboot: %w (%s)", err, out)
	}
	return nil
}

// readMeminfo returns /proc/meminfo values in kB.
func readMeminfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		out[k] = n
	}
	return out, sc.Err()
}
