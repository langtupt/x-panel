package siteapp

import (
	"strings"
	"testing"
	"xpanel/pkg/agentproto"
)

func TestRejectUnsafeSettings(t *testing.T) {
	for _, s := range []agentproto.WebSite{
		{Domain: "app.example.com", AppType: "nodejs", Port: 80},
		{Domain: "app.example.com", AppType: "php", PHPVersion: "8.3;id"},
		{Domain: "app.example.com", AppType: "go", Port: 12000, Runtime: "../../bin"},
		{Domain: "app.example.com", AppType: "html", Env: map[string]string{"PORT": "42"}},
		{Domain: "app.example.com", AppType: "html", Env: map[string]string{"X": "a\nExecStart=/bin/sh"}},
	} {
		if Validate(s) == nil {
			t.Fatalf("accepted unsafe settings: %+v", s)
		}
	}
}
func TestUnitAndEnvironment(t *testing.T) {
	if Unit("alice", "a.example.com") == Unit("alice-a", "example.com") {
		t.Fatal("unit collision")
	}
	if len(Socket("abcdefghijklmnop", strings.Repeat("a", 63)+".example.com")) >= 108 {
		t.Fatal("unix socket path too long")
	}
	got := unitValue(`a"b%c$HOME\d`)
	if got != `"a\"b%%c$HOME\\d"` {
		t.Fatalf("wrong escaping %s", got)
	}
}
