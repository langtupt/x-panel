package firewall

import (
	"strings"
	"testing"

	"xpanel/pkg/agentproto"
)

func TestValidateCanonical(t *testing.T) {
	c := Defaults()
	c.Allow = []string{" 203.0.113.7 ", "10.0.0.5/8", "::ffff:198.51.100.1", "2001:db8::1", "203.0.113.7"}
	if err := Validate(&c); err != nil {
		t.Fatal(err)
	}
	want := "10.0.0.0/8,198.51.100.1,2001:db8::1,203.0.113.7"
	if got := strings.Join(c.Allow, ","); got != want {
		t.Fatalf("allow = %s, want %s", got, want)
	}
	for _, bad := range []string{"1.2.3.4; drop", "1.2.3.4/33", "evil}", "1.2.3"} {
		c := Defaults()
		c.Block = []string{bad}
		if Validate(&c) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	c = Defaults()
	c.HTTPRate = -1
	if Validate(&c) == nil {
		t.Error("negative rate accepted")
	}
}

func TestRuleset(t *testing.T) {
	c := Defaults()
	c.Allow = []string{"203.0.113.7", "2001:db8::/32"}
	c.Block = []string{"198.51.100.0/24"}
	rs := ruleset(c)
	for _, want := range []string{
		"add element inet xpanel_fw allow4 { 203.0.113.7 }",
		"add element inet xpanel_fw allow6 { 2001:db8::/32 }",
		"add element inet xpanel_fw deny4 { 198.51.100.0/24 }",
		"ip saddr @allow4 accept",
		"ip saddr @ban4 drop",
		"limit rate over 100/second",
		"ct count over 300",
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("ruleset lacks %q", want)
		}
	}
	// Allow comes before every drop.
	if strings.Index(rs, "@allow4 accept") > strings.Index(rs, "@deny4 drop") {
		t.Error("allow list must be checked first")
	}
	// The ban sets are never flushed (bans survive a re-apply).
	if strings.Contains(rs, "flush set inet xpanel_fw ban") {
		t.Error("bans flushed")
	}
	c.SynRate, c.ConnLimit = 0, 0
	if rs := ruleset(c); strings.Contains(rs, "limit rate") || strings.Contains(rs, "ct count") {
		t.Error("disabled limits still present")
	}
}

func TestNginxConf(t *testing.T) {
	c := agentproto.DefaultFirewall()
	c.Allow = []string{"203.0.113.7"}
	conf := nginxRateConf(c)
	for _, want := range []string{"203.0.113.7 0;", "rate=30r/s", "burst=100", "limit_conn xp_conn 100"} {
		if !strings.Contains(conf, want) {
			t.Errorf("nginx conf lacks %q", want)
		}
	}
	c.HTTPRate, c.HTTPConn = 0, 0
	if nginxRateConf(c) != "" {
		t.Error("expected no nginx limits")
	}
	if !strings.Contains(jailConf(agentproto.DefaultFirewall()), "banaction = xpanel") {
		t.Error("fail2ban does not ban into the firewall")
	}
	c = Defaults()
	c.HTTPBurst = 0
	if err := Validate(&c); err != nil {
		t.Fatal(err)
	}
	conf = nginxRateConf(c)
	if strings.Contains(conf, "burst=0") || !strings.Contains(conf, "limit_req zone=xp_req;") {
		t.Fatalf("zero burst must produce a valid limit without a burst parameter: %s", conf)
	}
}
