package cron

import (
	"slices"
	"testing"
)

func TestOnCalendar(t *testing.T) {
	cases := []struct {
		cron string
		want []string
	}{
		{"* * * * *", []string{"*-*-* *:*:00"}},
		{"*/15 * * * *", []string{"*-*-* *:0,15,30,45:00"}},
		{"0 3 * * *", []string{"*-*-* 3:0:00"}},
		{"30 2 * * 1-5", []string{"Mon,Tue,Wed,Thu,Fri *-*-* 2:30:00"}},
		{"0 0 * * 0,7", []string{"Sun *-*-* 0:0:00"}},
		{"0 12 1,15 * *", []string{"*-*-1,15 12:0:00"}},
		{"0 0 1 */6 *", []string{"*-1,7-1 0:0:00"}},
		{"5/20 * * * *", []string{"*-*-* *:5,25,45:00"}},
		// day-of-month OR day-of-week, like cron.
		{"0 9 1 * 1", []string{"*-*-1 9:0:00", "Mon *-*-* 9:0:00"}},
		{"@daily", []string{"*-*-* 00:00:00"}},
		{"@weekly", []string{"Sun *-*-* 00:00:00"}},
	}
	for _, c := range cases {
		got, err := OnCalendar(c.cron)
		if err != nil {
			t.Errorf("%q: %v", c.cron, err)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.cron, got, c.want)
		}
	}
	for _, bad := range []string{"60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "5-1 * * * *"} {
		if _, err := OnCalendar(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
