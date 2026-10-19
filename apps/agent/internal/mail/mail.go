// Package mail runs the mail service: Postfix (SMTP), Dovecot (IMAP/POP3,
// LMTP delivery, SASL) and OpenDKIM. All mail is stored under one system
// user (vmail) that customers never run as; logins are virtual users from a
// passwd-file the agent regenerates from the panel's database on every change.
package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	VmailRoot   = "/var/mail/vhosts"
	mapDir      = "/etc/postfix/xpanel"
	dovecotUsrs = "/etc/dovecot/xpanel-users"
	dkimDir     = "/etc/opendkim"
	dkimKeys    = dkimDir + "/keys"
	selector    = "xpanel"
	installMark = "/etc/xpanel/mail.installed"
	panelCert   = "/etc/xpanel/ssl/panel.crt"
	panelKey    = "/etc/xpanel/ssl/panel.key"
)

var (
	mu          sync.Mutex
	aptOpts     = run.Opts{Env: []string{"DEBIAN_FRONTEND=noninteractive"}, Timeout: 20 * time.Minute}
	quotedValue = regexp.MustCompile(`"([^"]*)"`)
)

func Installed() bool {
	_, err := os.Stat(installMark)
	return err == nil
}

func hostname(ctx context.Context) string {
	out, err := run.Output(ctx, "hostname", "-f")
	h := strings.TrimSpace(out)
	if err != nil || validate.Domain(h) != nil {
		return "localhost.localdomain"
	}
	return h
}

// Install sets up Postfix, Dovecot and OpenDKIM for virtual mailboxes.
func Install(ctx context.Context, logf func(string, ...any)) error {
	mu.Lock()
	defer mu.Unlock()
	host := hostname(ctx)
	step := func(o run.Opts, name string, args ...string) error {
		logf("$ %s %s", name, strings.Join(args, " "))
		out, err := run.With(ctx, o, name, args...)
		if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) > 0 && lines[0] != "" {
			logf("%s", strings.Join(lines[max(0, len(lines)-6):], "\n"))
		}
		return err
	}
	// Postfix asks questions on install; answer them up front.
	preseed := "postfix postfix/main_mailer_type select Internet Site\npostfix postfix/mailname string " + host + "\n"
	if _, err := run.With(ctx, run.Opts{Stdin: preseed}, "debconf-set-selections"); err != nil {
		return err
	}
	if err := step(aptOpts, "apt-get", "-o", "DPkg::Lock::Timeout=600", "install", "-y", "-q",
		"postfix", "dovecot-core", "dovecot-imapd", "dovecot-pop3d", "dovecot-lmtpd", "opendkim", "opendkim-tools"); err != nil {
		return err
	}
	if _, err := user.Lookup("vmail"); err != nil {
		if err := step(run.Opts{}, "useradd", "--system", "--user-group", "--home-dir", VmailRoot,
			"--shell", "/usr/sbin/nologin", "vmail"); err != nil {
			return err
		}
	}
	vmail, err := user.Lookup("vmail")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(VmailRoot, 0o700); err != nil {
		return err
	}
	if err := run.Cmd(ctx, "chown", vmail.Uid+":"+vmail.Gid, VmailRoot); err != nil {
		return err
	}
	if err := os.Chmod(VmailRoot, 0o700); err != nil {
		return err
	}

	if err := configurePostfix(ctx, host); err != nil {
		return err
	}
	if err := configureDovecot(ctx, vmail); err != nil {
		return err
	}
	if err := configureDKIM(); err != nil {
		return err
	}
	// Empty maps so Postfix starts before the first sync.
	if err := writeMaps(ctx, agentproto.MailSyncParams{}); err != nil {
		return err
	}
	// Postfix creates /var/spool/postfix/private on its first start; Dovecot
	// puts its LMTP and auth sockets there, so it must start afterwards.
	for _, svc := range []string{"opendkim", "postfix", "dovecot"} {
		if err := step(run.Opts{}, "systemctl", "enable", svc); err != nil {
			return err
		}
		if err := step(run.Opts{}, "systemctl", "restart", svc); err != nil {
			return err
		}
	}
	openFirewall(ctx, logf)
	protectLogins(ctx, logf)
	if err := os.WriteFile(installMark, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return err
	}
	logf("Mail server sẵn sàng (hostname %s): SMTP 25, submission 587/465, IMAP 143/993, POP3 110/995", host)
	return nil
}

func postconf(ctx context.Context, args ...string) error {
	return run.Cmd(ctx, "postconf", args...)
}

func configurePostfix(ctx context.Context, host string) error {
	if err := os.MkdirAll(mapDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mapDir, "local.cidr"), []byte("127.0.0.0/8 xpanel_local\n::1/128 xpanel_local\n"), 0o644); err != nil {
		return err
	}
	settings := []string{
		"myhostname = " + host,
		"myorigin = $myhostname",
		"mydestination = localhost",
		"mynetworks = 127.0.0.0/8 [::1]/128",
		"inet_interfaces = all",
		"inet_protocols = all",
		"smtpd_banner = $myhostname ESMTP",
		"biff = no",
		"append_dot_mydomain = no",
		"message_size_limit = 52428800",
		"mailbox_size_limit = 0",
		"virtual_mailbox_domains = hash:" + mapDir + "/domains",
		"virtual_mailbox_maps = hash:" + mapDir + "/mailboxes",
		"virtual_alias_maps = hash:" + mapDir + "/aliases",
		"smtpd_sender_login_maps = hash:" + mapDir + "/senders",
		"virtual_transport = lmtp:unix:private/dovecot-lmtp",
		"smtpd_tls_cert_file = " + panelCert,
		"smtpd_tls_key_file = " + panelKey,
		"smtpd_tls_security_level = may",
		"smtpd_tls_auth_only = yes",
		"smtp_tls_security_level = may",
		"smtpd_tls_mandatory_protocols = !SSLv2, !SSLv3, !TLSv1, !TLSv1.1",
		"smtpd_tls_protocols = !SSLv2, !SSLv3, !TLSv1, !TLSv1.1",
		"smtpd_sasl_type = dovecot",
		"smtpd_sasl_path = private/auth",
		"smtpd_sasl_auth_enable = no",
		"smtpd_helo_required = yes",
		"disable_vrfy_command = yes",
		// No relaying for local processes: every account's PHP runs on this
		// machine, so localhost is not trusted. Local connections must not
		// claim a hosted address without logging in as it (cross-account spoofing).
		"smtpd_relay_restrictions = permit_sasl_authenticated, defer_unauth_destination",
		"smtpd_recipient_restrictions = permit_sasl_authenticated, reject_unauth_destination",
		"smtpd_restriction_classes = xpanel_local",
		"xpanel_local = check_sender_access hash:" + mapDir + "/local_senders",
		"smtpd_sender_restrictions = check_client_access cidr:" + mapDir + "/local.cidr",
		// The sendmail command cannot authenticate a sender: root only.
		"authorized_submit_users = root",
		"milter_default_action = accept",
		"milter_protocol = 6",
		"smtpd_milters = inet:127.0.0.1:8891",
		"non_smtpd_milters = inet:127.0.0.1:8891",
	}
	if err := postconf(ctx, append([]string{"-e"}, settings...)...); err != nil {
		return err
	}
	// Authenticated submission: 587 (STARTTLS) and 465 (implicit TLS). A login
	// may only send as its own address or aliases that deliver to it.
	for _, svc := range []string{"submission/inet=submission inet n - y - - smtpd", "submissions/inet=submissions inet n - y - - smtpd"} {
		if err := postconf(ctx, "-M", svc); err != nil {
			return err
		}
	}
	for _, name := range []string{"submission", "submissions"} {
		opts := []string{
			"syslog_name=postfix/" + name,
			"smtpd_sasl_auth_enable=yes",
			"smtpd_client_restrictions=permit_sasl_authenticated,reject",
			"smtpd_sender_restrictions=reject_sender_login_mismatch,permit_sasl_authenticated,reject",
			"smtpd_recipient_restrictions=permit_sasl_authenticated,reject",
			"smtpd_relay_restrictions=permit_sasl_authenticated,reject",
			"milter_macro_daemon_name=ORIGINATING",
		}
		if name == "submission" {
			opts = append(opts, "smtpd_tls_security_level=encrypt")
		} else {
			opts = append(opts, "smtpd_tls_wrappermode=yes")
		}
		for _, o := range opts {
			if err := postconf(ctx, "-P", name+"/inet/"+o); err != nil {
				return err
			}
		}
	}
	return nil
}

// dovecot24 reports whether Dovecot 2.4 (new configuration syntax, Debian 13+) is installed.
func dovecot24(ctx context.Context) bool {
	out, err := run.Output(ctx, "dovecot", "--version")
	return err == nil && strings.HasPrefix(strings.TrimSpace(out), "2.4")
}

// quotaField is the passwd-file extra field limiting one mailbox.
func quotaField(v24 bool, mb int) string {
	if v24 {
		return "userdb_quota_storage_size=" + strconv.Itoa(mb) + "M"
	}
	return "userdb_quota_rule=*:storage=" + strconv.Itoa(mb) + "M"
}

const dovecotListeners = `service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    mode = 0600
    user = postfix
    group = postfix
  }
}
service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
`

func configureDovecot(ctx context.Context, vmail *user.User) error {
	// Replace the stock auth config: it enables PAM, which would let Linux
	// accounts log in to IMAP with their system password.
	var auth, main string
	if dovecot24(ctx) {
		auth = `# Managed by X-Panel: virtual mailboxes only (no system users).
auth_allow_cleartext = no
auth_mechanisms = plain login
passdb passwd-file {
  passwd_file_path = ` + dovecotUsrs + `
}
userdb passwd-file {
  passwd_file_path = ` + dovecotUsrs + `
}
`
		main = `# Managed by X-Panel.
protocols = imap pop3 lmtp
mail_driver = maildir
mail_home = ` + VmailRoot + `/%{user | domain}/%{user | username}
mail_path = ~/Maildir
# Debian 13 sets an mbox INBOX in /var/mail; INBOX lives in the Maildir.
mail_inbox_path =
mail_uid = vmail
mail_gid = vmail
mail_privileged_group = vmail
first_valid_uid = ` + vmail.Uid + `
last_valid_uid = ` + vmail.Uid + `
ssl = required
ssl_server_cert_file = ` + panelCert + `
ssl_server_key_file = ` + panelKey + `
ssl_min_protocol = TLSv1.2
mail_plugins {
  quota = yes
}
protocol imap {
  mail_plugins {
    imap_quota = yes
  }
}
# Debian's 20-lmtp.conf strips the domain for system users; mailboxes here
# are full addresses.
protocol lmtp {
  auth_username_format = %{user | lower}
}
quota "User quota" {
}
` + dovecotListeners
	} else {
		auth = `# Managed by X-Panel: virtual mailboxes only (no system users).
disable_plaintext_auth = yes
auth_mechanisms = plain login
passdb {
  driver = passwd-file
  args = scheme=BLF-CRYPT username_format=%u ` + dovecotUsrs + `
}
userdb {
  driver = passwd-file
  args = username_format=%u ` + dovecotUsrs + `
  default_fields = uid=vmail gid=vmail home=` + VmailRoot + `/%d/%n
}
`
		main = `# Managed by X-Panel.
protocols = imap pop3 lmtp
mail_location = maildir:~/Maildir
mail_privileged_group = vmail
first_valid_uid = ` + vmail.Uid + `
last_valid_uid = ` + vmail.Uid + `
ssl = required
ssl_cert = <` + panelCert + `
ssl_key = <` + panelKey + `
ssl_min_protocol = TLSv1.2
mail_plugins = $mail_plugins quota
protocol imap {
  mail_plugins = $mail_plugins imap_quota
}
plugin {
  quota = maildir:User quota
}
` + dovecotListeners
	}
	if _, err := os.Stat("/etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel"); errors.Is(err, os.ErrNotExist) {
		if b, err := os.ReadFile("/etc/dovecot/conf.d/10-auth.conf"); err == nil {
			_ = os.WriteFile("/etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel", b, 0o644)
		}
	}
	if err := os.WriteFile("/etc/dovecot/conf.d/10-auth.conf", []byte(auth), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/dovecot/conf.d/99-xpanel.conf", []byte(main), 0o644); err != nil {
		return err
	}
	// The user list holds password hashes: readable by Dovecot only.
	if _, err := os.Stat(dovecotUsrs); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dovecotUsrs, nil, 0o640); err != nil {
			return err
		}
	}
	return run.Cmd(context.Background(), "chown", "root:dovecot", dovecotUsrs)
}

func configureDKIM() error {
	for _, d := range []string{dkimDir, dkimKeys} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	conf := `# Managed by X-Panel.
Syslog yes
UMask 007
Mode s
Canonicalization relaxed/simple
OversignHeaders From
KeyTable file:` + dkimDir + `/KeyTable
SigningTable refile:` + dkimDir + `/SigningTable
Socket inet:8891@127.0.0.1
MTA ORIGINATING
# Only authenticated submissions are signed, never mail from local processes.
InternalHosts ` + dkimDir + `/InternalHosts
UserID opendkim
`
	if err := os.WriteFile("/etc/opendkim.conf", []byte(conf), 0o644); err != nil {
		return err
	}
	// Run in the foreground with our config (the packaged unit passes its own socket).
	dropin := "/etc/systemd/system/opendkim.service.d"
	if err := os.MkdirAll(dropin, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dropin, "xpanel.conf"), []byte("[Service]\nType=simple\nPIDFile=\nExecStart=\nExecStart=/usr/sbin/opendkim -f -x /etc/opendkim.conf\n"), 0o644); err != nil {
		return err
	}
	for _, f := range []string{"KeyTable", "SigningTable"} {
		p := filepath.Join(dkimDir, f)
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(p, nil, 0o640); err != nil {
				return err
			}
		}
	}
	// Replaces the default (127.0.0.1) with a documentation-only address, so
	// no client is "internal" and only authenticated mail gets signed.
	if err := os.WriteFile(filepath.Join(dkimDir, "InternalHosts"), []byte("192.0.2.1\n"), 0o640); err != nil {
		return err
	}
	if err := run.Cmd(context.Background(), "chown", "-R", "opendkim:opendkim", dkimDir); err != nil {
		return err
	}
	return run.Cmd(context.Background(), "systemctl", "daemon-reload")
}

func openFirewall(ctx context.Context, logf func(string, ...any)) {
	openPorts(ctx, logf, "25", "465", "587", "110", "143", "993", "995")
}

// openPorts allows TCP ports when ufw is active.
func openPorts(ctx context.Context, logf func(string, ...any), ports ...string) {
	out, err := run.Output(ctx, "ufw", "status")
	if err != nil || !strings.Contains(out, "Status: active") {
		return
	}
	for _, p := range ports {
		_ = run.Cmd(ctx, "ufw", "allow", p+"/tcp")
	}
	logf("Đã mở cổng %s trên tường lửa (ufw)", strings.Join(ports, ", "))
}

// writeAtomic replaces a file only when its content changed. With a group,
// the new file gets it before the swap: a daemon re-reading the file in
// between must never see an unreadable file (Dovecot then keeps stale users).
func writeAtomic(path string, data []byte, mode os.FileMode, group ...string) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	tmp := path + ".new"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return false, err
	}
	if len(group) > 0 {
		g, err := user.LookupGroup(group[0])
		if err != nil {
			return false, err
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(tmp, 0, gid); err != nil {
			return false, err
		}
	}
	return true, os.Rename(tmp, path)
}

func writeMaps(ctx context.Context, p agentproto.MailSyncParams) error {
	var domains, boxes, aliases, senders, localSenders strings.Builder
	for _, d := range p.Domains {
		fmt.Fprintf(&domains, "%s OK\n", d.Domain)
		// Local (unauthenticated) clients must not use hosted senders.
		fmt.Fprintf(&localSenders, "%s REJECT Authenticate on port 587 to send as this domain\n", d.Domain)
	}
	owner := map[string]string{} // address -> mailbox allowed to send as it
	for _, b := range p.Mailboxes {
		local, domain, _ := strings.Cut(b.Address, "@")
		fmt.Fprintf(&boxes, "%s %s/%s/\n", b.Address, domain, local)
		owner[b.Address] = b.Address
	}
	for _, a := range p.Aliases {
		fmt.Fprintf(&aliases, "%s %s\n", a.Source, strings.Join(a.Destinations, ","))
		// An alias may be used as sender by the local mailboxes it delivers to.
		var logins []string
		for _, d := range a.Destinations {
			if _, ok := owner[d]; ok {
				logins = append(logins, d)
			}
		}
		if len(logins) > 0 {
			if _, isBox := owner[a.Source]; !isBox {
				fmt.Fprintf(&senders, "%s %s\n", a.Source, strings.Join(logins, ","))
			}
		}
	}
	addrs := make([]string, 0, len(owner))
	for a := range owner {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	for _, a := range addrs {
		fmt.Fprintf(&senders, "%s %s\n", a, a)
	}
	changed := false
	for name, content := range map[string]string{"domains": domains.String(), "mailboxes": boxes.String(),
		"aliases": aliases.String(), "senders": senders.String(), "local_senders": localSenders.String()} {
		path := filepath.Join(mapDir, name)
		c, err := writeAtomic(path, []byte(content), 0o644)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path + ".db"); c || errors.Is(err, os.ErrNotExist) {
			if err := run.Cmd(ctx, "postmap", path); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed && Installed() {
		return run.Cmd(ctx, "postfix", "reload")
	}
	return nil
}

func validateSync(p agentproto.MailSyncParams) error {
	domains := map[string]bool{}
	for _, d := range p.Domains {
		if err := validate.Domain(d.Domain); err != nil {
			return err
		}
		domains[d.Domain] = true
	}
	for _, b := range p.Mailboxes {
		if err := validate.MailAddress(b.Address); err != nil {
			return err
		}
		if _, d, _ := strings.Cut(b.Address, "@"); !domains[d] {
			return fmt.Errorf("mailbox %s: domain not enabled", b.Address)
		}
		if err := validate.BcryptHash(b.PasswordHash); err != nil {
			return err
		}
		if b.QuotaMB < 0 {
			return errors.New("invalid quota")
		}
	}
	for _, a := range p.Aliases {
		if err := validate.MailAddress(a.Source); err != nil {
			return err
		}
		if _, d, _ := strings.Cut(a.Source, "@"); !domains[d] {
			return fmt.Errorf("alias %s: domain not enabled", a.Source)
		}
		if len(a.Destinations) == 0 || len(a.Destinations) > 50 {
			return errors.New("alias needs 1–50 destinations")
		}
		for _, d := range a.Destinations {
			if err := validate.ForwardAddress(d); err != nil {
				return err
			}
		}
	}
	if p.Hostname != "" {
		return validate.Domain(p.Hostname)
	}
	return nil
}

// Sync applies the complete mail configuration.
func Sync(ctx context.Context, p agentproto.MailSyncParams) (agentproto.MailSyncResult, error) {
	res := agentproto.MailSyncResult{DKIM: map[string]agentproto.DNSRecord{}}
	if !Installed() {
		return res, errors.New("mail server chưa được cài (trang Ứng dụng)")
	}
	if err := validateSync(p); err != nil {
		return res, err
	}
	mu.Lock()
	defer mu.Unlock()

	if p.Hostname != "" {
		if out, _ := run.Output(ctx, "postconf", "-h", "myhostname"); strings.TrimSpace(out) != p.Hostname {
			if err := postconf(ctx, "-e", "myhostname = "+p.Hostname); err != nil {
				return res, err
			}
		}
	}
	if err := writeMaps(ctx, p); err != nil {
		return res, err
	}

	// Dovecot users: address:{BLF-CRYPT}hash:::::extra fields
	v24 := dovecot24(ctx)
	var users strings.Builder
	for _, b := range p.Mailboxes {
		hash := "{BLF-CRYPT}" + b.PasswordHash
		if b.Suspended {
			// No password can match; the entry stays so mail is still delivered.
			hash = "{CRYPT}!"
		}
		fmt.Fprintf(&users, "%s:%s::::::", b.Address, hash)
		var extra []string
		if b.QuotaMB > 0 {
			extra = append(extra, quotaField(v24, b.QuotaMB))
		}
		if b.Suspended {
			extra = append(extra, "nologin=y", "reason=Account suspended")
		}
		users.WriteString(strings.Join(extra, " "))
		users.WriteString("\n")
	}
	changed, err := writeAtomic(dovecotUsrs, []byte(users.String()), 0o640, "dovecot")
	if err != nil {
		return res, err
	}
	if changed {
		// Dovecot notices a new user list by mtime (seconds) and size, so two
		// same-size changes within a second could be missed: reload so new
		// passwords and suspensions apply at once (open sessions stay).
		if err := run.Cmd(ctx, "doveadm", "reload"); err != nil {
			return res, err
		}
		// Suspension also ends sessions that are already open.
		for _, b := range p.Mailboxes {
			if b.Suspended {
				_ = run.Cmd(ctx, "doveadm", "kick", b.Address)
			}
		}
	}

	if err := syncDKIM(ctx, p, &res); err != nil {
		return res, err
	}
	res.ServerIP = serverIP(ctx)
	return res, nil
}

func syncDKIM(ctx context.Context, p agentproto.MailSyncParams, res *agentproto.MailSyncResult) error {
	var keyTable, signTable strings.Builder
	for _, d := range p.Domains {
		dir := filepath.Join(dkimKeys, d.Domain)
		private := filepath.Join(dir, selector+".private")
		if _, err := os.Stat(private); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return err
			}
			if err := run.Cmd(ctx, "opendkim-genkey", "-b", "2048", "-d", d.Domain, "-s", selector, "-D", dir); err != nil {
				return err
			}
			if err := run.Cmd(ctx, "chown", "-R", "opendkim:opendkim", dir); err != nil {
				return err
			}
			if err := os.Chmod(private, 0o600); err != nil {
				return err
			}
		}
		txt, err := os.ReadFile(filepath.Join(dir, selector+".txt"))
		if err != nil {
			return err
		}
		var value strings.Builder
		for _, m := range quotedValue.FindAllStringSubmatch(string(txt), -1) {
			value.WriteString(m[1])
		}
		res.DKIM[d.Domain] = agentproto.DNSRecord{Name: selector + "._domainkey." + d.Domain, Value: value.String()}
		fmt.Fprintf(&keyTable, "%s._domainkey.%s %s:%s:%s\n", selector, d.Domain, d.Domain, selector, private)
		fmt.Fprintf(&signTable, "*@%s %s._domainkey.%s\n", d.Domain, selector, d.Domain)
	}
	c1, err := writeAtomic(filepath.Join(dkimDir, "KeyTable"), []byte(keyTable.String()), 0o640, "opendkim")
	if err != nil {
		return err
	}
	c2, err := writeAtomic(filepath.Join(dkimDir, "SigningTable"), []byte(signTable.String()), 0o640, "opendkim")
	if err != nil {
		return err
	}
	if c1 || c2 {
		_ = run.Cmd(ctx, "chown", "opendkim:opendkim", filepath.Join(dkimDir, "KeyTable"), filepath.Join(dkimDir, "SigningTable"))
		return run.Cmd(ctx, "systemctl", "restart", "opendkim")
	}
	return nil
}

func serverIP(ctx context.Context) string {
	out, _ := run.Output(ctx, "hostname", "-I")
	for _, f := range strings.Fields(out) {
		if strings.Count(f, ".") == 3 && !strings.HasPrefix(f, "127.") {
			return f
		}
	}
	return ""
}

// DeleteStored removes the maildir of one mailbox or of a whole domain (and its DKIM key).
func DeleteStored(p agentproto.MailDeleteParams) error {
	mu.Lock()
	defer mu.Unlock()
	switch {
	case p.Address != "":
		if err := validate.MailAddress(p.Address); err != nil {
			return err
		}
		local, domain, _ := strings.Cut(p.Address, "@")
		return os.RemoveAll(filepath.Join(VmailRoot, domain, local))
	case p.Domain != "":
		if err := validate.Domain(p.Domain); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(VmailRoot, p.Domain)); err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(dkimKeys, p.Domain))
	}
	return errors.New("nothing to delete")
}

func Status(ctx context.Context) agentproto.MailStatus {
	st := agentproto.MailStatus{Installed: Installed()}
	if st.Installed {
		out, _ := run.Output(ctx, "postconf", "-h", "myhostname")
		st.Hostname = strings.TrimSpace(out)
	}
	_, err := os.Stat(WebmailDir + "/index.php")
	st.Webmail = err == nil
	return st
}

// protectLogins adds fail2ban jails for SMTP AUTH and IMAP/POP3 password
// guessing when fail2ban is installed; a jail the local filters cannot run
// is dropped again so SSH protection keeps working.
func protectLogins(ctx context.Context, logf func(string, ...any)) {
	const jail = "/etc/fail2ban/jail.d/xpanel-mail.conf"
	if _, err := os.Stat("/etc/fail2ban/jail.d"); err != nil {
		return
	}
	conf := "# Managed by X-Panel\n[postfix-sasl]\nenabled = true\nbackend = systemd\n\n[dovecot]\nenabled = true\nbackend = systemd\n"
	if err := os.WriteFile(jail, []byte(conf), 0o644); err != nil {
		return
	}
	if err := run.Cmd(ctx, "systemctl", "restart", "fail2ban"); err != nil {
		_ = os.Remove(jail)
		_ = run.Cmd(ctx, "systemctl", "restart", "fail2ban")
		return
	}
	logf("Đã bật fail2ban cho SMTP/IMAP/POP3")
}
