package mail

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"xpanel/apps/agent/internal/run"
)

// Inbound mail on port 25 goes through OpenDMARC after OpenDKIM: it checks
// SPF itself, reads OpenDKIM's verification result, rejects a message that
// fails DMARC for a domain publishing p=reject and adds an
// Authentication-Results header to the others. Authenticated submission is
// not checked here (fromcheck covers it) and local clients are skipped.
const (
	dmarcConf  = "/etc/opendmarc.conf"
	dmarcAddr  = "127.0.0.1:8893"
	portMilter = "inet:127.0.0.1:8891, inet:" + dmarcAddr
)

func dmarcConfig(host string) string {
	return `# Managed by X-Panel.
AuthservID ` + host + `
TrustedAuthservIDs ` + host + `
Socket inet:8893@127.0.0.1
UserID opendmarc
UMask 007
Syslog true
IgnoreAuthenticatedClients true
RejectFailures true
RequiredHeaders false
SPFSelfValidate true
SPFIgnoreResults true
FailureReports false
PublicSuffixList /usr/share/publicsuffix/public_suffix_list.dat
`
}

// installDMARC adds the packages; dbconfig-no-thanks keeps the package from
// creating a MySQL history database. (Preseeding opendmarc/dbconfig-install
// does not work: the question is unknown until the package is installed.)
func installDMARC(ctx context.Context) (string, error) {
	return run.With(ctx, aptOpts, "apt-get", "-o", "DPkg::Lock::Timeout=600", "install", "-y", "-q",
		"--no-install-recommends", "opendmarc", "dbconfig-no-thanks")
}

func configureDMARC(ctx context.Context, host string) error {
	if err := os.WriteFile(dmarcConf, []byte(dmarcConfig(host)), 0o644); err != nil {
		return err
	}
	// Foreground with our config: the packaged unit passes its own socket.
	dropin := "/etc/systemd/system/opendmarc.service.d"
	if err := os.MkdirAll(dropin, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dropin, "xpanel.conf"), []byte("[Service]\nType=simple\nPIDFile=\nExecStart=\nExecStart=/usr/sbin/opendmarc -f -c "+dmarcConf+"\n"), 0o644); err != nil {
		return err
	}
	if err := run.Cmd(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	return postconf(ctx, "-e", "smtpd_milters = "+portMilter)
}

// MigrateDMARC adds OpenDMARC (and OpenDKIM verification) to mail servers
// set up by older versions. It installs packages, so it runs in the
// background at agent start; a failure is retried at the next start.
func MigrateDMARC(ctx context.Context, log *slog.Logger) {
	if !Installed() {
		return
	}
	if b, err := os.ReadFile(dmarcConf); err == nil && strings.Contains(string(b), "Managed by X-Panel") {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	host := hostname(ctx)
	if out, err := installDMARC(ctx); err != nil {
		log.Error("install opendmarc", "err", err, "out", lastLine(out))
		return
	}
	if err := configureDKIM(host); err != nil {
		log.Error("opendkim verification", "err", err)
		return
	}
	if err := configureDMARC(ctx, host); err != nil {
		log.Error("configure opendmarc", "err", err)
		return
	}
	for _, args := range [][]string{{"enable", "opendmarc"}, {"restart", "opendkim"}, {"restart", "opendmarc"}} {
		if err := run.Cmd(ctx, "systemctl", args...); err != nil {
			log.Error("opendmarc service", "cmd", args, "err", err)
			return
		}
	}
	if err := run.Cmd(ctx, "postfix", "reload"); err != nil {
		log.Error("postfix reload", "err", err)
		return
	}
	log.Info("inbound DMARC checks enabled")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
