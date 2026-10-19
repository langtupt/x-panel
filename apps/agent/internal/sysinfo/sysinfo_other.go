//go:build !linux

package sysinfo

import (
	"context"
	"errors"

	"xpanel/pkg/agentproto"
)

// Collect is only implemented on Linux; other platforms build for development only.
func Collect(*agentproto.SystemInfo) error {
	return errors.New("sysinfo: only supported on linux")
}

func Reboot(context.Context) error { return errors.New("reboot: only supported on Linux") }
