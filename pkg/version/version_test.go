package version

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v1.2.6", "v1.2.5", true},
		{"v1.10.0", "v1.9.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.2.5", "v1.2.5", false},
		{"v1.2.4", "v1.2.5", false},
		{"v1.2.6", "dev", true},
		{"dev", "v1.2.5", false},
		{"v1.2", "v1.1.0", false},
		{"v1.2.6; rm -rf /", "v1.2.5", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
