package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/mailer"
	"xpanel/apps/api/internal/settings"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

// ---------------------------------------------------------------- SMTP

const smtpKey = "smtp"

// smtpConfig returns the stored SMTP settings with the password opened.
func (s *Service) smtpConfig(ctx context.Context) (mailer.Config, error) {
	c := mailer.Config{Mode: "local", Port: 587, Security: "starttls"}
	v, err := settings.NewRepo(s.pool).Get(ctx, smtpKey)
	if err != nil || v == "" {
		return c, err
	}
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return c, err
	}
	if c.Mode == "" { // settings saved before the mode existed
		c.Mode = "local"
		if c.Enabled {
			c.Mode = "smtp"
		}
	}
	if c.Password != "" {
		if c.Password, err = s.secrets.Open(c.Password); err != nil {
			return c, err
		}
	}
	return c, nil
}

// SMTPSettings is what the administrator sees (never the password).
type SMTPSettings struct {
	mailer.Config
	PasswordSet bool `json:"password_set"`
}

func (s *Service) SMTPSettings(ctx context.Context, actor *users.User) (*SMTPSettings, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	c, err := s.smtpConfig(ctx)
	if err != nil {
		return nil, err
	}
	st := &SMTPSettings{Config: c, PasswordSet: c.Password != ""}
	st.Password = ""
	return st, nil
}

// SMTPInput replaces the settings; a nil Password keeps the stored one.
type SMTPInput struct {
	mailer.Config
	Password *string `json:"password"`
}

func (s *Service) SaveSMTP(ctx context.Context, actor *users.User, in SMTPInput) (*SMTPSettings, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	old, err := s.smtpConfig(ctx)
	if err != nil {
		return nil, err
	}
	c := in.Config
	c.Password = old.Password
	if in.Password != nil {
		c.Password = *in.Password
	}
	if err := c.Validate(); err != nil {
		return nil, apperr.Invalid("%s", err.Error())
	}
	stored := c
	if stored.Password, err = s.seal(c.Password); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(stored)
	if err := settings.NewRepo(s.pool).Set(ctx, smtpKey, string(b)); err != nil {
		return nil, err
	}
	return s.SMTPSettings(ctx, actor)
}

// TestSMTP sends a test message with the stored settings.
func (s *Service) TestSMTP(ctx context.Context, actor *users.User, to string) error {
	if err := adminOnly(actor); err != nil {
		return err
	}
	c, err := s.smtpConfig(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(to) == "" {
		to = actor.Email
	}
	err = s.sendWith(ctx, c, strings.TrimSpace(to), "[X-Panel] Email thử",
		"Đây là email thử từ X-Panel: cấu hình gửi email đang hoạt động.\n\nThis is a test e-mail from X-Panel.")
	if err != nil {
		return apperr.Invalid("%s", err.Error())
	}
	return nil
}

// sendEmail mails a customer through the administrator's SMTP server when it
// is enabled, otherwise through the server's own mail system.
func (s *Service) sendEmail(ctx context.Context, to, subject, body string) error {
	c, err := s.smtpConfig(ctx)
	if err != nil {
		return err
	}
	return s.sendWith(ctx, c, to, subject, body)
}

// sendWith sends through an external SMTP server ("smtp") or this server's
// own mail system ("local", signed with the sender domain's DKIM key).
func (s *Service) sendWith(ctx context.Context, c mailer.Config, to, subject, body string) error {
	if c.Mode == "smtp" {
		if c.Host == "" {
			return apperr.Conflict("chưa cấu hình máy chủ SMTP")
		}
		return c.Send(ctx, mailer.Message{To: to, Subject: subject, Body: body})
	}
	err := s.agent.Call(ctx, agentproto.ActionNotifyEmail, agentproto.NotifyEmailParams{
		To: to, Subject: subject, Body: body, From: c.From, FromName: c.FromName}, nil)
	if err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "agent: "))
	}
	return nil
}

// ---------------------------------------------------------------- expiry

// today is the current date in the server's time zone, as stored dates are.
func today() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func fmtDate(t time.Time) string { return t.Format("02/01/2006") }

// SetExpiry sets (date "YYYY-MM-DD") or clears ("" / nil) an account's
// hosting expiry. Renewing an account suspended for expiry re-activates it.
func (s *Service) SetExpiry(ctx context.Context, actor *users.User, id int64, date *string) (*users.User, error) {
	t, err := s.managed(ctx, s.users, actor, id)
	if err != nil {
		return nil, err
	}
	if t.Role != users.RoleUser {
		return nil, apperr.Invalid("chỉ tài khoản hosting mới có ngày hết hạn")
	}
	var at *time.Time
	if date != nil && strings.TrimSpace(*date) != "" {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(*date))
		if err != nil {
			return nil, apperr.Invalid("ngày hết hạn không hợp lệ (YYYY-MM-DD)")
		}
		if d.Year() < 2000 || d.Year() > 2200 {
			return nil, apperr.Invalid("ngày hết hạn không hợp lệ")
		}
		at = &d
	}
	if err := s.users.SetExpiry(ctx, id, at); err != nil {
		return nil, err
	}
	stillExpired := at != nil && at.Before(today())
	if t.Status == users.StatusSuspended && t.SuspendReason == users.SuspendExpired && !stillExpired {
		if _, err := s.setSuspension(ctx, id, users.StatusActive, ""); err != nil {
			return nil, err
		}
		s.notify(ctx, t, "expiry", "Hosting đã được gia hạn",
			s.renewedBody(t, at), "/dashboard/")
	} else if stillExpired && t.Status == users.StatusActive {
		// A date in the past takes effect now.
		if err := s.expire(ctx, t); err != nil {
			return nil, err
		}
	}
	return s.users.ByID(ctx, id)
}

// RunExpiry suspends expired accounts and mails reminders, every 15 minutes.
func (s *Service) RunExpiry(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		if err := s.checkExpiry(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("hosting expiry check", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// reminderDays are the days before expiry a reminder is sent.
var reminderDays = []int{7, 3, 1}

func (s *Service) checkExpiry(ctx context.Context) error {
	now := today()
	list, err := s.users.Expiring(ctx, now.AddDate(0, 0, reminderDays[0]))
	if err != nil {
		return err
	}
	for i := range list {
		u := &list[i]
		exp := dateOf(*u.ExpiresAt)
		if exp.Before(now) {
			if u.Status == users.StatusActive {
				if err := s.expire(ctx, u); err != nil {
					s.log.Error("expire account", "user", u.Username, "err", err)
				}
			}
			continue
		}
		left := int(exp.Sub(now).Hours() / 24)
		notice := reminderFor(left)
		if notice == "" || notice == u.ExpiryNotice || u.Status != users.StatusActive {
			continue
		}
		if err := s.users.SetExpiryNotice(ctx, u.ID, notice); err != nil {
			return err
		}
		title := fmt.Sprintf("Hosting sắp hết hạn (%s)", fmtDate(exp))
		s.notify(ctx, u, "expiry", title, s.reminderBody(u, exp, left), "/dashboard/")
	}
	return nil
}

// expire suspends an account whose hosting has expired and tells the owner.
func (s *Service) expire(ctx context.Context, u *users.User) error {
	if _, err := s.setSuspension(ctx, u.ID, users.StatusSuspended, users.SuspendExpired); err != nil {
		return err
	}
	if err := s.users.SetExpiryNotice(ctx, u.ID, "expired"); err != nil {
		return err
	}
	s.log.Info("hosting expired, account suspended", "user", u.Username)
	s.notify(ctx, u, "expiry", "Hosting đã hết hạn", s.expiredBody(u), "")
	return nil
}

// contact is how the customer reaches whoever sells them the hosting.
func (s *Service) contact(u *users.User) string {
	if u.ParentID != nil {
		if p, err := s.users.ByID(context.Background(), *u.ParentID); err == nil && p.Email != "" {
			return p.Email
		}
	}
	return ""
}

func (s *Service) footer(u *users.User) string {
	c := s.contact(u)
	if c == "" {
		return "\nVui lòng liên hệ quản trị viên để gia hạn.\n"
	}
	return fmt.Sprintf("\nVui lòng liên hệ quản trị viên (%s) để gia hạn.\n", c)
}

func (s *Service) reminderBody(u *users.User, exp time.Time, left int) string {
	when := fmt.Sprintf("sau %d ngày", left)
	if left == 0 {
		when = "hôm nay"
	}
	return fmt.Sprintf("Xin chào %s,\n\nGói hosting của tài khoản %s sẽ hết hạn %s (ngày %s).\n"+
		"Sau ngày này, website, email, FTP và database của tài khoản sẽ tạm ngưng cho tới khi được gia hạn.\n",
		u.Username, u.Username, when, fmtDate(exp)) + s.footer(u)
}

func (s *Service) expiredBody(u *users.User) string {
	exp := ""
	if u.ExpiresAt != nil {
		exp = " ngày " + fmtDate(*u.ExpiresAt)
	}
	return fmt.Sprintf("Xin chào %s,\n\nGói hosting của tài khoản %s đã hết hạn%s.\n"+
		"Website, email, FTP và database của tài khoản đã tạm ngưng; dữ liệu vẫn được giữ nguyên.\n",
		u.Username, u.Username, exp) + s.footer(u)
}

func (s *Service) renewedBody(u *users.User, at *time.Time) string {
	until := "không thời hạn"
	if at != nil {
		until = "tới ngày " + fmtDate(*at)
	}
	return fmt.Sprintf("Xin chào %s,\n\nGói hosting của tài khoản %s đã được gia hạn %s. Website và các dịch vụ đã hoạt động trở lại.\n",
		u.Username, u.Username, until)
}

// reminderFor is the reminder due with left days to go: the smallest
// threshold reached ("7", "3", "1"), "" when none is.
func reminderFor(left int) string {
	notice := ""
	for _, d := range reminderDays {
		if left <= d {
			notice = fmt.Sprint(d)
		}
	}
	return notice
}
