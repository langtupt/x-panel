package mail

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// Notify sends a panel alert with the local sendmail (root may submit).
// Without a mail server nothing is sent.
func Notify(ctx context.Context, p agentproto.NotifyEmailParams) error {
	if err := validate.MailAddress(p.To); err != nil {
		return err
	}
	if strings.ContainsAny(p.Subject, "\r\n") || len(p.Subject) > 200 || len(p.Body) > 20000 {
		return errors.New("bad message")
	}
	if _, err := os.Stat("/usr/sbin/sendmail"); err != nil {
		return errors.New("no local mail server")
	}
	host, _ := os.Hostname()
	msg := "From: X-Panel <noreply@" + host + ">\r\nTo: " + p.To + "\r\nSubject: " + p.Subject +
		"\r\nContent-Type: text/plain; charset=utf-8\r\nAuto-Submitted: auto-generated\r\n\r\n" + p.Body + "\r\n"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/sendmail", "-oi", "-f", "noreply@"+host, "--", p.To)
	cmd.Stdin = strings.NewReader(msg)
	return cmd.Run()
}
