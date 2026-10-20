package agentproto

import "testing"

func TestPHPPoolCount(t *testing.T) {
	sites := []WebSite{
		{AppType: "php", PHPVersion: "8.5"},
		{AppType: "laravel", PHPVersion: "8.5"},
		{PHPVersion: "8.3"}, {PHPVersion: "8.3"}, {PHPVersion: "8.4"},
		{AppType: "html"}, {AppType: "nodejs"},
	}
	if n := PHPPoolCount(sites); n != 4 {
		t.Fatalf("got %d masters, want 4", n)
	}
}
