package php

import "testing"

func TestValidateSetting(t *testing.T) {
	for _, c := range []struct {
		name, value string
		valid       bool
	}{
		{"memory_limit", "256M", true}, {"memory_limit", "-1", true}, {"max_execution_time", "60", true}, {"display_errors", "0", true}, {"date.timezone", "Asia/Ho_Chi_Minh", true}, {"session.cookie_samesite", "Lax", true},
		{"extension", "/tmp/code.so", false}, {"memory_limit", "256M\nextension=bad", false}, {"memory_limit", "1; extension=bad", false}, {"display_errors", "yes", false}, {"max_execution_time", "-2", false}, {"default_charset", `x"`, false}, {"date.timezone", "invalid/timezone", false}, {"session.cookie_samesite", "invalid", false},
	} {
		err := validateSetting(c.name, c.value)
		if (err == nil) != c.valid {
			t.Errorf("%s=%q: %v", c.name, c.value, err)
		}
	}
}

func TestParseFPMInfo(t *testing.T) {
	v := parseFPMInfo("max_execution_time => 30 => 30\ndisplay_errors => Off => Off\nallow_url_fopen => On => On\nsession.cookie_samesite => no value => no value\n")
	if v["max_execution_time"] != "30" || v["display_errors"] != "0" || v["allow_url_fopen"] != "1" || v["session.cookie_samesite"] != "" {
		t.Fatalf("unexpected FPM values: %v", v)
	}
}

func TestCandidates(t *testing.T) {
	m := parseCandidates("php8.5-curl:\n  Installed: 8.5.1\n  Candidate: 8.5.2\nphp8.5-unknown:\n  Candidate: (none)\n")
	if m["php8.5-curl"] != "8.5.2" || m["php8.5-unknown"] != "" {
		t.Fatalf("bad candidates: %v", m)
	}
}
