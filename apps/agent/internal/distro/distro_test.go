package distro

import (
	"slices"
	"strings"
	"testing"
)

func TestPHPPackagesIntegratedOPcache(t *testing.T) {
	d := Distro{}
	for _, v := range []string{"8.5", "8.6", "9.0"} {
		if slices.Contains(d.PHPPackages(v), "php"+v+"-opcache") {
			t.Errorf("PHP %s has no separate OPcache package", v)
		}
	}
	if !slices.Contains(d.PHPPackages("8.4"), "php8.4-opcache") {
		t.Fatal("PHP 8.4 still needs OPcache package")
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name, osRelease, id, codename string
	}{
		{"debian", "ID=debian\nVERSION_ID=\"12\"\nVERSION_CODENAME=bookworm\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"", "debian", "bookworm"},
		{"ubuntu", "ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble", "ubuntu", "noble"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Parse(strings.NewReader(c.osRelease))
			if err != nil {
				t.Fatal(err)
			}
			if d.ID != c.id || d.Codename != c.codename {
				t.Fatalf("got %+v", d)
			}
		})
	}
}

func TestPHPPaths(t *testing.T) {
	d := Distro{}
	if got := d.PHPFPMSocket("8.3", "cust1"); got != "/run/xpanel-php8.3-cust1/php.sock" {
		t.Error(got)
	}
	if got := d.PHPFPMPoolDir("8.3"); got != "/etc/php/8.3/fpm/pool.d" {
		t.Error(got)
	}
	if got := d.PHPPackages("8.3"); !slices.Contains(got, "php8.3-fpm") || !slices.Contains(got, "php8.3-sqlite3") {
		t.Error(got)
	}
}

func TestParseUnsupported(t *testing.T) {
	for _, id := range []string{"arch", "almalinux", "centos"} {
		if _, err := Parse(strings.NewReader("ID=" + id + "\n")); err == nil {
			t.Errorf("expected error for %s", id)
		}
	}
}
