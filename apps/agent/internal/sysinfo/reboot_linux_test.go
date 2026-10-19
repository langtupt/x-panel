//go:build linux

package sysinfo

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestScheduleRebootDelayedAndReportsFailure(t *testing.T) {
	calls := 0
	command := func(_ context.Context, name string, args ...string) (string, error) {
		calls++
		if name != "systemd-run" || !slices.Contains(args, "--on-active=10s") || !slices.Equal(args[len(args)-2:], []string{"/usr/bin/systemctl", "reboot"}) {
			t.Fatalf("unsafe schedule %s %v", name, args)
		}
		return "timer already exists", errors.New("duplicate")
	}
	if err := scheduleReboot(context.Background(), command); err == nil {
		t.Fatal("scheduling failure must reach caller")
	}
	if calls != 1 {
		t.Fatalf("unexpected retries: %d", calls)
	}
}
