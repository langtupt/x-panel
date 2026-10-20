package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/settings"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

const firewallKey = "firewall"

func (s *Service) firewallConfig(ctx context.Context) (agentproto.FirewallConfig, bool, error) {
	c := agentproto.DefaultFirewall()
	v, err := settings.NewRepo(s.pool).Get(ctx, firewallKey)
	if err != nil || v == "" {
		return c, false, err
	}
	err = json.Unmarshal([]byte(v), &c)
	return c, true, err
}

// InitFirewall applies the stored firewall (the defaults on a new install)
// and the panel login policy. Retried until the agent answers.
func (s *Service) InitFirewall(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		err := s.initFirewall(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		if attempt == 0 {
			s.log.Warn("firewall", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (s *Service) initFirewall(ctx context.Context) error {
	c, stored, err := s.firewallConfig(ctx)
	if err != nil {
		return err
	}
	s.setLoginPolicy(c)
	if stored {
		return nil // the agent restores its own copy at boot
	}
	applied, err := s.applyFirewall(ctx, c)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(applied)
	return settings.NewRepo(s.pool).Set(ctx, firewallKey, string(b))
}

func (s *Service) applyFirewall(ctx context.Context, c agentproto.FirewallConfig) (agentproto.FirewallConfig, error) {
	var out agentproto.FirewallConfig
	err := s.agent.Call(ctx, agentproto.ActionFirewallApply, c, &out)
	return out, err
}

func (s *Service) setLoginPolicy(c agentproto.FirewallConfig) {
	p := auth.LoginPolicy{IPFails: c.PanelIPFails, IPBan: time.Duration(c.PanelBanMin) * time.Minute,
		AccountFails: c.AccountFails, AccountLock: time.Duration(c.AccountLockMin) * time.Minute}
	enabled := c.Enabled
	s.auth.SetLoginPolicy(p, func(ip string, d time.Duration) {
		if !enabled {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.agent.Call(ctx, agentproto.ActionFirewallBan, agentproto.FirewallBanParams{IP: ip,
			Seconds: int(d.Seconds()), Reason: "panel-login"}, nil); err != nil {
			s.log.Warn("firewall ban", "ip", ip, "err", err)
		} else {
			s.log.Info("banned after failed panel logins", "ip", ip, "for", d)
		}
	}, func(username string, d time.Duration) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		u, err := s.users.ByUsername(ctx, username)
		if err != nil {
			return
		}
		s.notify(ctx, u, "security", "Tài khoản tạm khóa đăng nhập",
			fmt.Sprintf("Tài khoản %s bị khóa đăng nhập %d phút do nhập sai mật khẩu nhiều lần. Nếu không phải bạn, hãy đổi mật khẩu và bật xác thực 2 lớp.",
				username, int(d.Minutes())), "/account/")
	})
}

type FirewallState struct {
	Config         agentproto.FirewallConfig `json:"config"`
	Active         bool                      `json:"active"`
	Bans           []agentproto.FirewallBan  `json:"bans"`
	LockedAccounts []auth.Lock               `json:"locked_accounts"`
	YourIP         string                    `json:"your_ip"`
}

func adminOnly(actor *users.User) error {
	if actor.Role != users.RoleAdmin {
		return apperr.Forbidden("chỉ admin quản lý tường lửa")
	}
	return nil
}

func (s *Service) Firewall(ctx context.Context, actor *users.User, ip string) (*FirewallState, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	c, _, err := s.firewallConfig(ctx)
	if err != nil {
		return nil, err
	}
	st := &FirewallState{Config: c, LockedAccounts: s.auth.LockedAccounts(), YourIP: ip}
	var bans agentproto.FirewallBans
	if err := s.agent.Call(ctx, agentproto.ActionFirewallBans, nil, &bans); err != nil {
		return nil, apperr.Agent(err)
	}
	st.Active, st.Bans = bans.Active, bans.Bans
	return st, nil
}

// inList reports whether ip is one of the addresses or networks.
func inList(ip string, list []string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, s := range list {
		s = strings.TrimSpace(s)
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(a) {
			return true
		}
		if b, err := netip.ParseAddr(s); err == nil && b.Unmap() == a {
			return true
		}
	}
	return false
}

func (s *Service) SaveFirewall(ctx context.Context, actor *users.User, ip string, c agentproto.FirewallConfig) (*FirewallState, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	// Never let the administrator lock themselves out.
	if inList(ip, c.Block) && !inList(ip, c.Allow) {
		return nil, apperr.Invalid("danh sách chặn chứa IP bạn đang dùng (%s)", ip)
	}
	applied, err := s.applyFirewall(ctx, c)
	if err != nil {
		return nil, apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	b, _ := json.Marshal(applied)
	if err := settings.NewRepo(s.pool).Set(ctx, firewallKey, string(b)); err != nil {
		return nil, err
	}
	s.setLoginPolicy(applied)
	return s.Firewall(ctx, actor, ip)
}

func (s *Service) BanIP(ctx context.Context, actor *users.User, myIP, ip string, minutes int, reason string) error {
	if err := adminOnly(actor); err != nil {
		return err
	}
	if inList(myIP, []string{ip}) {
		return apperr.Invalid("không thể chặn IP bạn đang dùng")
	}
	if minutes < 0 || minutes > 525600 {
		return apperr.Invalid("thời gian chặn 0–525600 phút (0 = vĩnh viễn)")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "thủ công"
	}
	if err := s.agent.Call(ctx, agentproto.ActionFirewallBan, agentproto.FirewallBanParams{IP: ip, Seconds: minutes * 60,
		Reason: "admin: " + reason}, nil); err != nil {
		return apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return nil
}

func (s *Service) UnbanIP(ctx context.Context, actor *users.User, ip string) error {
	if err := adminOnly(actor); err != nil {
		return err
	}
	s.auth.UnlockIP(ip)
	if err := s.agent.Call(ctx, agentproto.ActionFirewallUnban, agentproto.FirewallBanParams{IP: ip}, nil); err != nil {
		return apperr.Invalid("%s", strings.TrimPrefix(err.Error(), "agent: "))
	}
	return nil
}

func (s *Service) UnlockAccount(actor *users.User, username string) error {
	if err := adminOnly(actor); err != nil {
		return err
	}
	if !s.auth.UnlockAccount(username) {
		return apperr.NotFound("tài khoản không bị khóa")
	}
	return nil
}
