package firewall

import (
	"net/netip"
	"testing"
)

func TestMatchList(t *testing.T) {
	list := []string{"10.0.0.0/8", "203.0.113.7", "2001:db8::/32"}
	for ip, want := range map[string]string{
		"10.1.2.3":    "10.0.0.0/8",
		"203.0.113.7": "203.0.113.7",
		"203.0.113.8": "",
		"2001:db8::1": "2001:db8::/32",
		"2001:db9::1": "",
	} {
		if got := matchList(netip.MustParseAddr(ip), list); got != want {
			t.Errorf("%s: got %q want %q", ip, got, want)
		}
	}
}
