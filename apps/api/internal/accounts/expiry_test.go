package accounts

import "testing"

func TestReminderFor(t *testing.T) {
	for left, want := range map[int]string{30: "", 8: "", 7: "7", 5: "7", 3: "3", 2: "3", 1: "1", 0: "1"} {
		if got := reminderFor(left); got != want {
			t.Errorf("reminderFor(%d) = %q, want %q", left, got, want)
		}
	}
}
