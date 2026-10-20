package web

import (
	"context"

	"xpanel/apps/agent/internal/nginxctl"
)

// ReloadNginx reloads nginx and waits until the new configuration serves.
func ReloadNginx(ctx context.Context) error { return nginxctl.Reload(ctx) }

// waitSocket waits until a PHP-FPM socket accepts connections.
func waitSocket(path string) error { return nginxctl.WaitSocket(path) }
