package agentproto

import "testing"

func TestValidPHPVersion(t *testing.T) {
	for _, v := range []string{"7.4", "8.5", "8.6", "9.0"} {
		if !ValidPHPVersion(v) {
			t.Errorf("rejected %q", v)
		}
	}
	for _, v := range []string{"5.6", "7.3", "8.05", "8.5.1", "../8.5", "8.5;id", "8.5\n", "08.5"} {
		if ValidPHPVersion(v) {
			t.Errorf("accepted %q", v)
		}
	}
}
