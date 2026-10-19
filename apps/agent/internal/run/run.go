// Package run executes system binaries with an argument vector (never a
// shell) and turns failures into errors that include the command output.
package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const DefaultTimeout = 2 * time.Minute

type Opts struct {
	Timeout time.Duration // 0 = DefaultTimeout
	Env     []string      // added to the agent's environment
	Stdin   string        // fed to the command's standard input
}

func With(ctx context.Context, o Opts, name string, args ...string) (string, error) {
	if o.Timeout == 0 {
		o.Timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if len(o.Env) > 0 {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %v: %s", name, err, tail(strings.TrimSpace(string(out)), 2000))
	}
	return string(out), nil
}

func Output(ctx context.Context, name string, args ...string) (string, error) {
	return With(ctx, Opts{}, name, args...)
}

func Cmd(ctx context.Context, name string, args ...string) error {
	_, err := Output(ctx, name, args...)
	return err
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// CmdStdin runs a command feeding stdin (e.g. secrets that must not appear in argv).
func CmdStdin(ctx context.Context, stdin, name string, args ...string) error {
	_, err := With(ctx, Opts{Stdin: stdin}, name, args...)
	return err
}
