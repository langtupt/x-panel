package netguard

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	s := Render([]owned{{10001, "1000"}, {10002, "1001"}}, "33")
	for _, want := range []string{
		"delete table inet xpanel_apps",
		"elements = { 10001, 10002 }",
		"elements = { 10001 . 1000, 10002 . 1001 }",
		"meta skuid 33 accept",
		`oifname "lo" tcp dport @ports jump guard`,
		"reject with tcp reset",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// No ports: sets must be declared without an (invalid) empty element list.
	empty := Render(nil, "33")
	if strings.Contains(empty, "elements") {
		t.Errorf("empty ruleset must not list elements:\n%s", empty)
	}
}
