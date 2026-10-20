// Package web renders a Linux user's websites into nginx vhosts and PHP-FPM
// pools. Sync is declarative: it receives the user's complete site list and
// makes the generated files match it, rolling back if a config test fails.
package web

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/fsutil"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/run"
	"xpanel/apps/agent/internal/siteapp"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/phpconfig"
	"xpanel/pkg/validate"
)

const (
	ACMERoot = "/var/lib/xpanel/acme"
	CertBase = "/etc/xpanel/ssl/sites"
	LogDir   = "/var/log/nginx/xpanel"
)

func CertFiles(domain string) (cert, key string) {
	dir := filepath.Join(CertBase, domain)
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
}

type Syncer struct {
	D distro.Distro
}

func (s *Syncer) Sync(ctx context.Context, p agentproto.WebSyncParams) (agentproto.WebSyncResult, error) {
	var res agentproto.WebSyncResult
	u, err := accounts.Managed(p.Username)
	if err != nil {
		return res, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	installed := php.Installed(s.D)

	if err := validateSites(p.Sites, installed); err != nil {
		return res, err
	}

	// Customer directories.
	for _, d := range []struct {
		rel  string
		mode os.FileMode
	}{{"www", 0o750}, {"tmp", 0o700}, {"logs", 0o750}} {
		if _, err := fsutil.SafeMkdirAll(u.HomeDir, d.rel, uid, gid, d.mode); err != nil {
			return res, err
		}
	}
	for _, site := range p.Sites {
		rel := "www/" + site.Domain
		if site.DocRoot != "" {
			rel += "/" + site.DocRoot
		}
		root, err := fsutil.SafeMkdirAll(u.HomeDir, rel, uid, gid, 0o750)
		if err != nil {
			return res, err
		}
		if !siteapp.Dynamic(site.AppType) && site.AppType != "laravel" {
			if err := fsutil.CreateIfEmpty(root, "index.html", []byte(welcomePage), uid, gid); err != nil {
				return res, err
			}
		}
	}
	// nginx must traverse the customer's 0750 home: put it in the user's group.
	if err := run.Cmd(ctx, "gpasswd", "-a", s.D.NginxUser(), p.Username); err != nil {
		return res, err
	}

	if err := s.syncPools(ctx, p, u.HomeDir, installed); err != nil {
		return res, err
	}
	if err := siteapp.Sync(ctx, s.D, p); err != nil {
		return res, err
	}
	warn, err := s.syncVhosts(ctx, p, u.HomeDir)
	res.Warnings = append(res.Warnings, warn...)
	return res, err
}

func validateSites(sites []agentproto.WebSite, installed []string) error {
	seen := map[string]bool{}
	for _, s := range sites {
		if err := siteapp.Validate(s); err != nil {
			return err
		}
		for _, n := range append([]string{s.Domain}, s.Aliases...) {
			if err := validate.Domain(n); err != nil {
				return err
			}
			if seen[n] {
				return fmt.Errorf("duplicate domain %s", n)
			}
			seen[n] = true
		}
		if err := validate.DocRoot(s.DocRoot); err != nil {
			return err
		}
		if s.PHPVersion != "" && !slices.Contains(installed, s.PHPVersion) {
			return fmt.Errorf("PHP %s chưa được cài trên máy chủ", s.PHPVersion)
		}
	}
	return nil
}

// syncPools writes one pool per PHP version the user needs and removes the
// user's pools from every other installed version.
func (s *Syncer) syncPools(ctx context.Context, p agentproto.WebSyncParams, home string, installed []string) error {
	want := map[string]bool{}
	if !p.Suspended {
		for _, site := range p.Sites {
			if site.PHPVersion != "" && site.AppType == "" {
				want[site.PHPVersion] = true
			}
		}
	}
	if err := setSliceLimits(ctx, p.Username, p.CPUPct, p.MemMB); err != nil {
		return err
	}
	for _, v := range installed {
		if err := s.removeLegacyPool(ctx, v, p.Username); err != nil {
			return err
		}
		unit := s.D.FPMUnit(v, p.Username)
		conf := s.D.FPMConfig(v, p.Username)
		if !want[v] {
			if exists(conf) {
				_ = run.Cmd(ctx, "systemctl", "disable", "--now", unit)
				if err := os.Remove(conf); err != nil {
					return err
				}
			}
			continue
		}

		if err := s.ensureUnitTemplate(ctx, v); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := poolTmpl.Execute(&buf, poolData{RunDir: s.D.FPMRunDir(v, p.Username), Unit: strings.TrimSuffix(unit, ".service"),
			Username: p.Username, Home: home, Socket: s.D.PHPFPMSocket(v, p.Username), NginxUser: s.D.NginxUser(),
			MaxChildren: phpconfig.Workers(p.MemMB, agentproto.PHPPoolCount(p.Sites))}); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
			return err
		}
		changed, restore, err := fileSet{conf: buf.Bytes()}.apply()
		if err != nil {
			return err
		}
		if changed {
			if out, err := run.Output(ctx, s.D.PHPFPMBinary(v), "-t", "--fpm-config", conf); err != nil {
				restore()
				return fmt.Errorf("php-fpm %s config test failed: %s", v, out)
			}
		}
		if err := run.Cmd(ctx, "systemctl", "enable", unit); err != nil {
			return err
		}
		action := "start" // no-op when already running
		if changed {
			action = "reload-or-restart"
		}
		if err := run.Cmd(ctx, "systemctl", action, unit); err != nil {
			return err
		}
		if err := waitSocket(s.D.PHPFPMSocket(v, p.Username)); err != nil {
			return err
		}
	}
	return nil
}

// ensureUnitTemplate writes the systemd template for a PHP version.
func (s *Syncer) ensureUnitTemplate(ctx context.Context, v string) error {
	var buf bytes.Buffer
	if err := unitTmpl.Execute(&buf, unitData{Version: v, Binary: s.D.PHPFPMBinary(v)}); err != nil {
		return err
	}
	changed, _, err := fileSet{s.D.FPMTemplate(v): buf.Bytes()}.apply()
	if err != nil || !changed {
		return err
	}
	return run.Cmd(ctx, "systemctl", "daemon-reload")
}

// removeLegacyPool drops a pool left in the shared php-fpm service by
// versions of X-Panel before per-account masters.
func (s *Syncer) removeLegacyPool(ctx context.Context, v, username string) error {
	legacy := filepath.Join(s.D.PHPFPMPoolDir(v), "xpanel-"+username+".conf")
	if !exists(legacy) {
		return nil
	}
	if err := os.Remove(legacy); err != nil {
		return err
	}
	return run.Cmd(ctx, "systemctl", "reload-or-restart", s.D.PHPFPMService(v))
}

// setSliceLimits applies the package CPU/RAM limits to xpanel-<user>.slice
// (persistent drop-in, effective immediately). 0 removes a limit.
func setSliceLimits(ctx context.Context, username string, cpuPct, memMB int) error {
	cpu, mem := "CPUQuota=", "MemoryMax=infinity"
	if cpuPct > 0 {
		cpu = fmt.Sprintf("CPUQuota=%d%%", cpuPct)
	}
	if memMB > 0 {
		mem = fmt.Sprintf("MemoryMax=%dM", memMB)
	}
	return run.Cmd(ctx, "systemctl", "set-property", "xpanel-"+username+".slice", cpu, mem, "MemorySwapMax=0")
}

// RemoveSlice deletes the persistent limits of a deleted account.
func RemoveSlice(username string) {
	_ = os.RemoveAll("/etc/systemd/system.control/xpanel-" + username + ".slice.d")
}

func (s *Syncer) syncVhosts(ctx context.Context, p agentproto.WebSyncParams, home string) ([]string, error) {
	var warnings []string
	dir := filepath.Join(s.D.NginxConf, p.Username)
	for _, d := range []string{dir, LogDir, ACMERoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	fs := fileSet{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".conf") {
			fs[filepath.Join(dir, e.Name())] = nil // removed unless still wanted
		}
	}
	for _, site := range p.Sites {
		root := filepath.Join(home, "www", site.Domain)
		if site.DocRoot != "" {
			root = filepath.Join(root, site.DocRoot)
		}
		v := vhostData{Username: p.Username, Domain: site.Domain, Root: root, Suspended: p.Suspended, OverBandwidth: p.OverBandwidth,
			Names: strings.Join(append([]string{site.Domain}, site.Aliases...), " "), ACMERoot: ACMERoot, LogDir: LogDir}
		if siteapp.Dynamic(site.AppType) {
			v.Port = site.Port
		}
		if site.PHPVersion != "" {
			v.PHPSocket = s.D.PHPFPMSocket(site.PHPVersion, p.Username)
			if site.AppType == "php" || site.AppType == "laravel" {
				v.PHPSocket = siteapp.Socket(p.Username, site.Domain)
			}
		}
		if site.SSL {
			v.CertFile, v.KeyFile = CertFiles(site.Domain)
			if exists(v.CertFile) && exists(v.KeyFile) {
				v.SSL = true
			} else {
				warnings = append(warnings, "chưa có chứng chỉ SSL cho "+site.Domain+", tạm phục vụ HTTP")
			}
		}
		var buf bytes.Buffer
		if err := vhostTmpl.Execute(&buf, v); err != nil {
			return warnings, err
		}
		fs[filepath.Join(dir, site.Domain+".conf")] = buf.Bytes()
	}

	changed, restore, err := fs.apply()
	if err != nil || !changed {
		return warnings, err
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		restore()
		return warnings, fmt.Errorf("nginx config test failed: %s", out)
	}
	if err := ReloadNginx(ctx); err != nil {
		return warnings, err
	}
	// Certificates of sites that no longer exist for this user.
	for path, content := range fs {
		if content == nil {
			domain := strings.TrimSuffix(filepath.Base(path), ".conf")
			if validate.Domain(domain) == nil {
				_ = os.RemoveAll(filepath.Join(CertBase, domain))
			}
		}
	}
	return warnings, nil
}

// fileSet maps path → desired content (nil = file must not exist).
type fileSet map[string][]byte

// apply writes the set and returns a func restoring the previous contents.
func (fs fileSet) apply() (changed bool, restore func(), err error) {
	old := map[string][]byte{}
	for path, want := range fs {
		cur, rerr := os.ReadFile(path)
		existed := rerr == nil
		if existed && want != nil && bytes.Equal(cur, want) || !existed && want == nil {
			continue
		}
		if existed {
			old[path] = cur
		} else {
			old[path] = nil
		}
		changed = true
		if want == nil {
			err = os.Remove(path)
		} else {
			err = os.WriteFile(path, want, 0o644)
		}
		if err != nil {
			break
		}
	}
	restore = func() {
		for path, content := range old {
			if content == nil {
				_ = os.Remove(path)
			} else {
				_ = os.WriteFile(path, content, 0o644)
			}
		}
	}
	if err != nil {
		restore()
	}
	return changed, restore, err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
