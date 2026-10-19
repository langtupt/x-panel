// Package distro detects the supported distributions (Debian, Ubuntu) and
// knows their file layout.
package distro

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Distro struct {
	ID        string // debian | ubuntu
	Name      string
	Version   string // VERSION_ID, e.g. "12", "24.04"
	Codename  string // VERSION_CODENAME, e.g. "bookworm", "noble"
	NginxConf string // directory for xpanel-managed vhosts
}

// PHPFPMPoolDir returns where pool files for a PHP version live
// (packages.sury.org on Debian, ppa:ondrej/php on Ubuntu).
func (d Distro) PHPFPMPoolDir(phpVersion string) string {
	return "/etc/php/" + phpVersion + "/fpm/pool.d"
}

// PHPFPMService returns the systemd unit name for a PHP version.
func (d Distro) PHPFPMService(phpVersion string) string {
	return "php" + phpVersion + "-fpm"
}

// PHPFPMBinary is the php-fpm executable; its presence means the version is installed.
func (d Distro) PHPFPMBinary(phpVersion string) string {
	return "/usr/sbin/php-fpm" + phpVersion
}

// Each hosting account runs its own PHP-FPM master per version as the systemd
// unit "xpanel-php<ver>@<user>" inside the slice "xpanel-<user>.slice", so
// package CPU/RAM limits apply to that account only.

// FPMUnit is the systemd unit of a user's PHP-FPM master for phpVersion.
func (d Distro) FPMUnit(phpVersion, username string) string {
	return "xpanel-php" + phpVersion + "@" + username + ".service"
}

// FPMTemplate is the systemd template unit file for phpVersion.
func (d Distro) FPMTemplate(phpVersion string) string {
	return "/etc/systemd/system/xpanel-php" + phpVersion + "@.service"
}

// FPMConfig is the php-fpm configuration of a user's master for phpVersion.
func (d Distro) FPMConfig(phpVersion, username string) string {
	return "/etc/xpanel/php/" + phpVersion + "/" + username + ".conf"
}

// FPMRunDir is the runtime directory (socket, pid) systemd creates for the unit.
func (d Distro) FPMRunDir(phpVersion, username string) string {
	return "/run/xpanel-php" + phpVersion + "-" + username
}

// PHPFPMSocket is where a user's PHP-FPM master for phpVersion listens.
func (d Distro) PHPFPMSocket(phpVersion, username string) string {
	return d.FPMRunDir(phpVersion, username) + "/php.sock"
}

// PHPPackages lists the packages installed for one PHP version.
func (d Distro) PHPPackages(phpVersion string) []string {
	var pkgs []string
	parts := strings.Split(phpVersion, ".")
	major, minor := 0, 0
	if len(parts) == 2 {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
	}
	for _, e := range []string{"fpm", "cli", "mysql", "sqlite3", "curl", "gd", "mbstring", "xml", "zip", "intl", "bcmath", "opcache"} {
		// PHP 8.5+ integrates OPcache; Sury no longer ships a separate package.
		if e == "opcache" && (major > 8 || major == 8 && minor >= 5) {
			continue
		}
		pkgs = append(pkgs, "php"+phpVersion+"-"+e)
	}
	return pkgs
}

// NginxUser is the account nginx workers run as.
func (d Distro) NginxUser() string { return "www-data" }

func Detect() (Distro, error) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return Distro{}, err
	}
	defer f.Close()
	return Parse(f)
}

func Parse(r io.Reader) (Distro, error) {
	kv := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		kv[k] = strings.Trim(v, `"'`)
	}
	if err := sc.Err(); err != nil {
		return Distro{}, err
	}

	id := kv["ID"]
	if id != "debian" && id != "ubuntu" {
		return Distro{}, fmt.Errorf("unsupported distribution %q (supported: debian, ubuntu)", id)
	}
	return Distro{
		ID:        id,
		Name:      kv["PRETTY_NAME"],
		Version:   kv["VERSION_ID"],
		Codename:  kv["VERSION_CODENAME"],
		NginxConf: "/etc/nginx/conf.d/xpanel",
	}, nil
}
