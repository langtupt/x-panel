package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"os"
	"os/exec"
	"strings"
	"time"

	"xpanel/apps/agent/internal/run"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// defaultSender is noreply@ the mail server's own name (myhostname), which
// is a real DNS name, unlike the machine's short host name.
func defaultSender(ctx context.Context) string {
	if h, err := run.Output(ctx, "postconf", "-h", "myhostname"); err == nil {
		if h = strings.TrimSpace(h); strings.Contains(h, ".") {
			return "noreply@" + h
		}
	}
	host, _ := os.Hostname()
	return "noreply@" + host
}

// Notify sends a panel e-mail with the local sendmail (root may submit).
// Postfix signs it with DKIM when the sender's domain is a mail domain of
// this server. Without a mail server nothing is sent.
func Notify(ctx context.Context, p agentproto.NotifyEmailParams) error {
	if err := validate.MailAddress(p.To); err != nil {
		return err
	}
	if strings.ContainsAny(p.Subject+p.FromName, "\r\n") || len(p.Subject) > 300 || len(p.FromName) > 100 || len(p.Body) > 50000 {
		return errors.New("bad message")
	}
	if _, err := os.Stat("/usr/sbin/sendmail"); err != nil {
		return errors.New("máy chủ chưa cài mail server (trang Ứng dụng → Mail server)")
	}
	from := p.From
	if from == "" {
		from = defaultSender(ctx)
	} else if err := validate.MailAddress(from); err != nil {
		return err
	}
	name := p.FromName
	if name == "" {
		name = "X-Panel"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", (&mail.Address{Name: name, Address: from}).String())
	fmt.Fprintf(&b, "To: <%s>\r\n", p.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", p.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\nAuto-Submitted: auto-generated\r\n\r\n")
	body := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(p.Body, "\n", "\r\n")))
	for len(body) > 76 {
		b.WriteString(body[:76] + "\r\n")
		body = body[76:]
	}
	b.WriteString(body + "\r\n")
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/sendmail", "-oi", "-f", from, "--", p.To)
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sendmail: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
