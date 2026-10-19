//go:build linux

package files

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"xpanel/pkg/agentproto"
)

// HelperMain is the entry point of "xpanel-agent fshelper <home> <uid> <gid>".
// It confines itself before reading any customer-controlled input:
// chroot(home), then drop all groups and switch to uid/gid for good.
func HelperMain(args []string) {
	if err := helper(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func helper(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: fshelper <home> <uid> <gid>")
	}
	uid, err1 := strconv.Atoi(args[1])
	gid, err2 := strconv.Atoi(args[2])
	if err1 != nil || err2 != nil || uid < 1000 || gid < 1000 {
		return fmt.Errorf("refusing to run as uid %s gid %s", args[1], args[2])
	}
	if err := syscall.Chroot(args[0]); err != nil {
		return fmt.Errorf("chroot: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return err
	}
	// Go applies these to every thread of the process (Go ≥ 1.16).
	if err := syscall.Setgroups([]int{}); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	if os.Getuid() != uid || os.Geteuid() != uid {
		return fmt.Errorf("failed to drop privileges")
	}
	syscall.Umask(0o022)

	in := bufio.NewReaderSize(os.Stdin, 64<<10)
	line, err := in.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	var req agentproto.FSRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	out := bufio.NewWriterSize(os.Stdout, 64<<10)
	res, err := dispatch(req, in, out)
	if err != nil {
		return err
	}
	if res != nil {
		if err := json.NewEncoder(out).Encode(res); err != nil {
			return err
		}
	}
	return out.Flush()
}
