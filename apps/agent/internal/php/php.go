// Package php installs PHP-FPM versions side by side:
// Debian → packages.sury.org, Ubuntu → ppa:ondrej/php.
package php

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
)

const installTimeout = 20 * time.Minute

func Supported(v string) bool { return agentproto.ValidPHPVersion(v) }

func Installed(d distro.Distro) []string {
	var out []string
	paths, _ := filepath.Glob("/usr/sbin/php-fpm*")
	for _, path := range paths {
		v := strings.TrimPrefix(filepath.Base(path), "php-fpm")
		if Supported(v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// Install adds the third-party repository if needed, installs the version's
// packages and starts its php-fpm service. Returns the combined log.
func Install(ctx context.Context, d distro.Distro, version string) (string, error) {
	if !Supported(version) {
		return "", fmt.Errorf("unsupported PHP version %q", version)
	}
	var log strings.Builder
	step := func(o run.Opts, name string, args ...string) error {
		if name == "apt-get" {
			args = append([]string{"-o", "DPkg::Lock::Timeout=600"}, args...)
		}
		fmt.Fprintf(&log, "$ %s %s\n", name, strings.Join(args, " "))
		if o.Timeout == 0 {
			o.Timeout = installTimeout
		}
		out, err := run.With(ctx, o, name, args...)
		log.WriteString(lastLines(out, 15))
		return err
	}
	apt := run.Opts{Env: []string{"DEBIAN_FRONTEND=noninteractive"}}

	err := addAptRepo(d, step, apt)
	if err == nil {
		err = step(apt, "apt-get", "update", "-q")
	}
	if err == nil {
		err = step(apt, "apt-get", append([]string{"install", "-y", "-q"}, d.PHPPackages(version)...)...)
	}
	if err == nil {
		err = step(run.Opts{Timeout: time.Minute}, "systemctl", "enable", "--now", d.PHPFPMService(version))
	}
	return log.String(), err
}

type stepFn func(o run.Opts, name string, args ...string) error

func addAptRepo(d distro.Distro, step stepFn, apt run.Opts) error {
	if d.ID == "ubuntu" {
		if hasFile("/etc/apt/sources.list.d", "ondrej") {
			return nil
		}
		if err := step(apt, "apt-get", "install", "-y", "-q", "software-properties-common"); err != nil {
			return err
		}
		return step(apt, "add-apt-repository", "-y", "ppa:ondrej/php")
	}
	const list = "/etc/apt/sources.list.d/xpanel-php.list"
	if _, err := os.Stat(list); err == nil {
		return nil
	}
	if d.Codename == "" {
		return fmt.Errorf("cannot determine Debian codename")
	}
	if err := step(apt, "apt-get", "install", "-y", "-q", "curl", "ca-certificates"); err != nil {
		return err
	}
	const keyDeb = "/tmp/debsuryorg-archive-keyring.deb"
	if err := step(run.Opts{}, "curl", "-fsSLo", keyDeb, "https://packages.sury.org/debsuryorg-archive-keyring.deb"); err != nil {
		return err
	}
	if err := step(apt, "dpkg", "-i", keyDeb); err != nil {
		return err
	}
	line := "deb [signed-by=/usr/share/keyrings/deb.sury.org-php.gpg] https://packages.sury.org/php/ " + d.Codename + " main\n"
	return os.WriteFile(list, []byte(line), 0o644)
}

func hasFile(dir, substr string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), substr) {
			return true
		}
	}
	return false
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{"…"}, lines[len(lines)-n:]...)
	}
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// RepoConfigured reports whether the third-party PHP repository (sury on
// Debian, ondrej PPA on Ubuntu) was added; until then APT only knows the
// distribution's own PHP version.
func RepoConfigured(d distro.Distro) bool {
	if d.ID == "ubuntu" {
		return hasFile("/etc/apt/sources.list.d", "ondrej")
	}
	_, err := os.Stat("/etc/apt/sources.list.d/xpanel-php.list")
	return err == nil
}
