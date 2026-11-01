// Package mailer sends the panel's e-mails to customers through an SMTP
// server configured by the administrator (Gmail, Zoho, Amazon SES, the
// provider's relay…).
package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config is the SMTP account. Password is plain here; callers store it sealed.
type Config struct {
	// Mode "local" sends with this server's own mail server (Postfix signs
	// with the DKIM key of the sender's mail domain); "smtp" uses Host….
	Mode     string `json:"mode"`
	Enabled  bool   `json:"enabled"` // = Mode == "smtp" (older settings)
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // starttls | tls | none
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`      // sender address
	FromName string `json:"from_name"` // display name
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

// Validate checks the settings before they are stored.
func (c *Config) Validate() error {
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.From = strings.TrimSpace(c.From)
	c.FromName = strings.TrimSpace(c.FromName)
	if c.Mode != "smtp" {
		c.Mode, c.Enabled = "local", false
	} else {
		c.Enabled = true
	}
	if len(c.FromName) > 100 || strings.ContainsAny(c.FromName, "\r\n") {
		return errors.New("tên người gửi không hợp lệ")
	}
	if c.Mode == "local" {
		if c.From != "" {
			if a, err := mail.ParseAddress(c.From); err != nil || a.Address != c.From {
				return errors.New("địa chỉ gửi (From) không hợp lệ")
			}
		}
		return nil
	}
	if !hostRe.MatchString(c.Host) {
		return errors.New("máy chủ SMTP không hợp lệ")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("cổng SMTP 1–65535")
	}
	switch c.Security {
	case "starttls", "tls", "none":
	default:
		return errors.New("bảo mật phải là starttls, tls hoặc none")
	}
	if a, err := mail.ParseAddress(c.From); err != nil || a.Address != c.From {
		return errors.New("địa chỉ gửi (From) không hợp lệ")
	}
	if len(c.Username) > 200 || len(c.Password) > 500 || len(c.FromName) > 100 ||
		strings.ContainsAny(c.FromName+c.Username, "\r\n") {
		return errors.New("tên đăng nhập, mật khẩu hoặc tên người gửi không hợp lệ")
	}
	return nil
}

// Message is a plain-text e-mail.
type Message struct {
	To      string
	Subject string
	Body    string
}

func encodeHeader(s string) string { return mime.QEncoding.Encode("utf-8", s) }

func (c *Config) build(m Message) ([]byte, error) {
	to, err := mail.ParseAddress(m.To)
	if err != nil || strings.ContainsAny(m.To, "\r\n") {
		return nil, fmt.Errorf("địa chỉ nhận không hợp lệ: %q", m.To)
	}
	from := mail.Address{Name: c.FromName, Address: c.From}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeHeader(strings.ReplaceAll(m.Subject, "\n", " ")))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%d.%s>\r\n", time.Now().UnixNano(), c.From)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	body := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n")))
	for len(body) > 76 {
		b.WriteString(body[:76] + "\r\n")
		body = body[76:]
	}
	b.WriteString(body + "\r\n")
	return []byte(b.String()), nil
}

// Send delivers m through the configured server.
func (c *Config) Send(ctx context.Context, m Message) error {
	msg, err := c.build(m)
	if err != nil {
		return err
	}
	to, _ := mail.ParseAddress(m.To)
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d := net.Dialer{}
	var conn net.Conn
	if c.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: c.Host}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("không kết nối được %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP: %w", err)
	}
	defer cl.Close()
	if err := cl.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP HELO: %w", err)
	}
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("máy chủ SMTP không hỗ trợ STARTTLS")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if c.Username != "" {
		// net/smtp refuses PLAIN auth without TLS except to localhost.
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("đăng nhập SMTP thất bại: %w", err)
		}
	}
	if err := cl.Mail(c.From); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := cl.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP RCPT TO: %w", err)
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	return cl.Quit()
}
