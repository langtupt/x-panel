package phpconfig

import "testing"

func TestAccountWorkerBudget(t *testing.T) {
	for _, c := range []struct{ ram, pools, want int }{{1024, 1, 10}, {1024, 4, 2}, {256, 2, 1}, {0, 4, 2}, {8192, 1, 50}} {
		if got := Workers(c.ram, c.pools); got != c.want {
			t.Fatalf("Workers(%d,%d)=%d, want %d", c.ram, c.pools, got, c.want)
		}
	}
	// Four sites no longer receive four independent account-sized budgets.
	if Workers(1024, 4)*4 > Workers(1024, 1) {
		t.Fatal("multisite worker budget exceeds single-account budget")
	}
}
