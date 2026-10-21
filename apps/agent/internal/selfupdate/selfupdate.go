// Package selfupdate upgrades X-Panel to a release by running its installer.
// The installer restarts xpanel-agent and xpanel-api, so it runs in its own
// transient systemd unit, never as a child of either.
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/version"
)

const (
	// Repo is fixed here: the API only chooses which release tag to install.
	Repo    = "langtupt/x-panel"
	Unit    = "xpanel-update"
	LogFile = "/var/log/xpanel-update.log"
)

func running(ctx context.Context) bool {
	return run.Cmd(ctx, "systemctl", "is-active", "--quiet", Unit) == nil
}

// Start installs release tag v (e.g. v1.2.6) in the background.
func Start(ctx context.Context, v string) error {
	if !version.Valid(v) {
		return errors.New("phiên bản không hợp lệ")
	}
	if running(ctx) {
		return errors.New("đang cập nhật")
	}
	if err := os.WriteFile(LogFile, nil, 0o600); err != nil {
		return err
	}
	// The installer and its checksum check come from the same release tag.
	script := fmt.Sprintf(`set -o pipefail
echo "== $(date -Is) X-Panel %[1]s -> %[2]s"
curl -fsSL --retry 3 https://raw.githubusercontent.com/%[3]s/%[2]s/install.sh | XPANEL_VERSION=%[2]s bash
echo "XPANEL_UPDATE_EXIT=$?"
`, version.Version, v, Repo)
	out, err := run.Output(ctx, "systemd-run", "--unit="+Unit, "--collect",
		"--property=StandardOutput=append:"+LogFile, "--property=StandardError=append:"+LogFile,
		"/bin/bash", "-c", script)
	if err != nil {
		return fmt.Errorf("systemd-run: %w (%s)", err, out)
	}
	return nil
}

// Status reports whether an update runs and the end of its log.
func Status(ctx context.Context) agentproto.UpdateStatus {
	st := agentproto.UpdateStatus{Running: running(ctx)}
	f, err := os.Open(LogFile)
	if err != nil {
		return st
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > 16384 {
		_, _ = f.Seek(-16384, io.SeekEnd)
	}
	b, _ := io.ReadAll(io.LimitReader(f, 16384))
	st.Log = string(b)
	return st
}
