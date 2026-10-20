package php

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/phpconfig"
)

var managementMu sync.Mutex
var packageName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var settingKinds = phpconfig.Kinds

func requireVersion(v string) error {
	if !Supported(v) {
		return errors.New("invalid PHP version")
	}
	if _, err := os.Stat("/usr/sbin/php-fpm" + v); err != nil {
		return errors.New("PHP version is not installed")
	}
	return nil
}
func overridePath(v string) string { return "/etc/php/" + v + "/fpm/conf.d/99-xpanel.ini" }
func readOverrides(v string) (map[string]string, error) {
	data, err := os.ReadFile(overridePath(v))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, val, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if ok && settingKinds[k] != "" {
			out[k] = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return out, nil
}
func modulePackages(ctx context.Context, v string) ([]agentproto.PHPModule, error) {
	out, err := run.Output(ctx, "apt-cache", "pkgnames", "php"+v+"-")
	if err != nil {
		return nil, err
	}
	installed, err := run.Output(ctx, "dpkg-query", "-W", "-f=${binary:Package}\t${db:Status-Status}\t${Version}\n")
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, line := range strings.Split(installed, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) == 3 && parts[1] == "installed" {
			versions[strings.Split(parts[0], ":")[0]] = parts[2]
		}
	}
	blocked := map[string]bool{"cli": true, "fpm": true, "cgi": true, "phpdbg": true, "dev": true, "common": true, "xsl": false}
	seen := map[string]bool{}
	modules := []agentproto.PHPModule{}
	for _, name := range strings.Fields(out) {
		suffix := strings.TrimPrefix(name, "php"+v+"-")
		if seen[name] || blocked[suffix] || !packageName.MatchString(suffix) || strings.HasSuffix(suffix, "-dbgsym") {
			continue
		}
		seen[name] = true
		ver := versions[name]
		item := agentproto.PHPModule{Name: name, Version: ver, Installed: ver != "", Removable: ver != ""}
		files, _ := filepath.Glob("/var/lib/dpkg/info/" + name + ".list")
		for _, file := range files {
			data, _ := os.ReadFile(file)
			for _, line := range strings.Split(string(data), "\n") {
				if (strings.HasPrefix(line, "/etc/php/"+v+"/mods-available/") || strings.HasPrefix(line, "/usr/share/"+name+"/")) && strings.HasSuffix(line, ".ini") {
					module := strings.TrimSuffix(filepath.Base(line), ".ini")
					item.Modules = append(item.Modules, module)
					enabled, _ := filepath.Glob("/etc/php/" + v + "/fpm/conf.d/*-" + module + ".ini")
					if len(enabled) > 0 {
						item.Enabled = true
					}
				}
			}
		}
		modules = append(modules, item)
	}
	packages := []string{}
	for _, m := range modules {
		packages = append(packages, m.Name)
	}
	if len(packages) > 0 {
		args := append([]string{"policy"}, packages...)
		if policy, e := run.With(ctx, run.Opts{Env: []string{"LC_ALL=C"}}, "apt-cache", args...); e == nil {
			candidates := parseCandidates(policy)
			for i := range modules {
				modules[i].Candidate = candidates[modules[i].Name]
				if modules[i].Installed && modules[i].Candidate != "" && modules[i].Candidate != modules[i].Version {
					_, compareErr := run.Output(ctx, "dpkg", "--compare-versions", modules[i].Version, "lt", modules[i].Candidate)
					modules[i].Upgradable = compareErr == nil
				}
			}
		}
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules, nil
}
func Management(ctx context.Context, p agentproto.PHPManageParams) (agentproto.PHPManagement, error) {
	managementMu.Lock()
	defer managementMu.Unlock()
	var result agentproto.PHPManagement
	if err := requireVersion(p.Version); err != nil {
		return result, err
	}
	modules, err := modulePackages(ctx, p.Version)
	if err != nil {
		return result, err
	}
	loaded, err := run.Output(ctx, "/usr/sbin/php-fpm"+p.Version, "-m")
	if err != nil {
		return result, err
	}
	active := map[string]bool{}
	for _, line := range strings.Split(loaded, "\n") {
		active[strings.ToLower(strings.TrimSpace(line))] = true
	}
	packaged := map[string]bool{}
	for i := range modules {
		for _, name := range modules[i].Modules {
			if active[name] || name == "opcache" && active["zend opcache"] {
				modules[i].Loaded = true
			}
			packaged[name] = true
		}
	}
	for name := range active {
		if name == "" || strings.HasPrefix(name, "[") {
			continue
		}
		key := name
		if key == "zend opcache" {
			key = "opcache"
		}
		if !packaged[key] {
			modules = append(modules, agentproto.PHPModule{Name: key, Installed: true, Enabled: true, Loaded: true, Builtin: true})
		}
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	result.Modules = modules
	out, err := run.Output(ctx, "/usr/sbin/php-fpm"+p.Version, "-i")
	if err != nil {
		return result, err
	}
	values := parseFPMInfo(out)
	overrides, err := readOverrides(p.Version)
	if err != nil {
		return result, err
	}
	result.Settings = []agentproto.PHPSetting{}
	for name, kind := range settingKinds {
		value, ok := values[name]
		if !ok {
			continue
		}
		_, custom := overrides[name]
		result.Settings = append(result.Settings, agentproto.PHPSetting{Name: name, Value: value, Kind: kind, Custom: custom})
	}
	sort.Slice(result.Settings, func(i, j int) bool { return result.Settings[i].Name < result.Settings[j].Name })
	return result, nil
}
func validateSetting(name, value string) error { return phpconfig.ValidateSetting(name, value) }

func ApplyManagement(ctx context.Context, p agentproto.PHPManageParams) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	managementMu.Lock()
	defer managementMu.Unlock()
	if err := requireVersion(p.Version); err != nil {
		return "", err
	}
	if p.Operation == "refresh" {
		return run.With(ctx, run.Opts{Timeout: 5 * time.Minute, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}, "apt-get", "-o", "DPkg::Lock::Timeout=600", "-o", "APT::Update::Error-Mode=any", "update", "-q")
	}
	if p.Module != "" {
		if len(p.Settings) > 0 || len(p.Reset) > 0 {
			return "", errors.New("mixed operation")
		}
		modules, err := modulePackages(ctx, p.Version)
		if err != nil {
			return "", err
		}
		found := false
		var selected agentproto.PHPModule
		for _, m := range modules {
			if m.Name == p.Module {
				found = true
				selected = m
				if m.Builtin {
					return "", errors.New("built-in modules cannot be modified")
				}
				if p.Remove && !m.Removable {
					return "", errors.New("module is not installed")
				}
			}
		}
		if !found {
			return "", errors.New("unknown PHP module")
		}
		if p.Operation == "enable" || p.Operation == "disable" {
			if !selected.Installed || len(selected.Modules) == 0 {
				return "", errors.New("no configurable modules in package")
			}
			tool := "phpenmod"
			if p.Operation == "disable" {
				tool = "phpdismod"
			}
			previous := map[string]bool{}
			for _, name := range selected.Modules {
				paths, _ := filepath.Glob("/etc/php/" + p.Version + "/fpm/conf.d/*-" + name + ".ini")
				previous[name] = len(paths) > 0
			}
			rollback := func() error {
				var failures []error
				for _, name := range selected.Modules {
					restore := "phpdismod"
					if previous[name] {
						restore = "phpenmod"
					}
					_, err := run.Output(context.WithoutCancel(ctx), restore, "-v", p.Version, "-s", "fpm", name)
					if err != nil {
						failures = append(failures, err)
					}
				}
				return errors.Join(failures...)
			}
			args := append([]string{"-v", p.Version, "-s", "fpm"}, selected.Modules...)
			out, err := run.Output(ctx, tool, args...)
			if err != nil {
				return out, errors.Join(err, rollback())
			}
			if check, e := run.Output(ctx, "/usr/sbin/php-fpm"+p.Version, "-m"); e != nil || strings.Contains(check, "PHP Warning:") || strings.Contains(check, "PHP Fatal error:") {
				return out, errors.Join(fmt.Errorf("PHP module validation failed: %v %s", e, check), rollback())
			}
			if err = reloadVersion(ctx, p.Version); err != nil {
				repairErr := rollback()
				return out, errors.Join(err, repairErr, reloadVersion(context.WithoutCancel(ctx), p.Version))
			}
			return out, nil
		}
		if p.Operation != "" && p.Operation != "update" {
			return "", errors.New("unknown module operation")
		}
		if p.Operation == "update" && !selected.Installed {
			return "", errors.New("module not installed")
		}
		action := "install"
		if p.Remove {
			action = "remove"
		}
		args := []string{"-o", "DPkg::Lock::Timeout=600", action, "-y", "--no-install-recommends", p.Module}
		if p.Operation == "update" {
			args = append(args, "--only-upgrade")
		}
		if p.Remove {
			out, err := run.With(ctx, run.Opts{Env: []string{"LC_ALL=C"}}, "apt-get", "-s", "remove", p.Module)
			if err != nil {
				return out, err
			}
			for _, line := range strings.Split(out, "\n") {
				parts := strings.Fields(line)
				if len(parts) > 1 && parts[0] == "Remv" && parts[1] != p.Module {
					return "", fmt.Errorf("removal would also remove %s; operation blocked", parts[1])
				}
			}
		} else {
			args = append([]string{"--no-remove"}, args...)
		}
		out, err := run.With(ctx, run.Opts{Timeout: 20 * time.Minute, Env: []string{"DEBIAN_FRONTEND=noninteractive"}}, "apt-get", args...)
		if err != nil {
			return out, err
		}
		if _, err = run.Output(ctx, "/usr/sbin/php-fpm"+p.Version, "-t"); err != nil {
			return out, err
		}
		err = reloadVersion(ctx, p.Version)
		return out, err
	}
	if len(p.Settings) == 0 && len(p.Reset) == 0 {
		return "", errors.New("empty settings update")
	}
	values, err := readOverrides(p.Version)
	if err != nil {
		return "", err
	}
	for name, value := range p.Settings {
		if err := validateSetting(name, value); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		values[name] = value
	}
	for _, name := range p.Reset {
		if settingKinds[name] == "" {
			return "", errors.New("unknown directive")
		}
		delete(values, name)
	}
	keys := []string{}
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var data strings.Builder
	data.WriteString("; Managed by X-Panel. PHP-FPM only.\n")
	for _, k := range keys {
		fmt.Fprintf(&data, "%s = \"%s\"\n", k, values[k])
	}
	path := overridePath(p.Version)
	old, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	if err = atomicConfig(path, []byte(data.String())); err != nil {
		return "", err
	}
	rollback := func() {
		if errors.Is(readErr, os.ErrNotExist) {
			_ = os.Remove(path)
		} else {
			_ = atomicConfig(path, old)
		}
	}
	if _, err = run.Output(ctx, "/usr/sbin/php-fpm"+p.Version, "-t"); err != nil {
		rollback()
		return "", err
	}
	if err = reloadVersion(ctx, p.Version); err != nil {
		rollback()
		_ = reloadVersion(context.WithoutCancel(ctx), p.Version)
		return "", err
	}
	return "PHP-FPM configuration validated and reloaded successfully.", nil
}
func atomicConfig(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".xpanel-*.ini")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// Read the FPM SAPI, not CLI defaults (which differ for execution limits).
func parseFPMInfo(out string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, " => ")
		if len(parts) != 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		kind := settingKinds[name]
		if kind == "" {
			continue
		}
		value := strings.TrimSpace(parts[2])
		if value == "no value" {
			value = ""
		}
		if kind == "bool" {
			if value == "On" || value == "1" {
				value = "1"
			} else {
				value = "0"
			}
		}
		values[name] = value
	}
	return values
}

// Every site can have its own master. Signalling only the distro service
// leaves those sites running stale ini values/extensions.
func reloadVersion(ctx context.Context, v string) error {
	out, err := run.Output(ctx, "systemctl", "list-units", "--type=service", "--state=running", "--no-legend", "--plain")
	if err != nil {
		return err
	}
	binary := regexp.MustCompile(regexp.QuoteMeta("/usr/sbin/php-fpm"+v) + `(?:[ ;}]|$)`)
	var failures []error
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := fields[0]
		if !strings.HasPrefix(unit, "xpanel-") && unit != "php"+v+"-fpm.service" {
			continue
		}
		command, err := run.Output(ctx, "systemctl", "show", unit, "--property=ExecStart", "--value")
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !binary.MatchString(command) {
			continue
		}
		if _, err := run.Output(ctx, "systemctl", "kill", "--kill-whom=main", "--signal=USR2", unit); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func parseCandidates(out string) map[string]string {
	result := map[string]string{}
	name := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			name = strings.TrimSuffix(line, ":")
		}
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate: "); ok && value != "(none)" {
			result[name] = value
		}
	}
	return result
}

func Logs(ctx context.Context, p agentproto.PHPManageParams) (agentproto.TaskLog, error) {
	if err := requireVersion(p.Version); err != nil {
		return agentproto.TaskLog{}, err
	}
	out, err := run.Output(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain")
	if err != nil {
		return agentproto.TaskLog{}, err
	}
	units := []string{"php" + p.Version + "-fpm.service"}
	match := regexp.MustCompile(regexp.QuoteMeta("/usr/sbin/php-fpm"+p.Version) + `(?:[ ;}]|$)`)
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasPrefix(f[0], "xpanel-") {
			continue
		}
		command, e := run.Output(ctx, "systemctl", "show", f[0], "--property=ExecStart", "--value")
		if e == nil && match.MatchString(command) {
			units = append(units, f[0])
		}
	}
	args := []string{"--no-pager", "-n", "200", "--output=short-iso"}
	for _, unit := range units {
		args = append(args, "-u", unit)
	}
	log, err := run.Output(ctx, "journalctl", args...)
	return agentproto.TaskLog{Log: log}, err
}
