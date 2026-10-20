package accounts

import "testing"

func TestFilesQuotaMB(t *testing.T) {
	for _, c := range []struct{ disk, mail, want int }{
		{0, 500, 0},      // unlimited package stays unlimited
		{1000, 0, 1000},  // no mail
		{1000, 300, 700}, // mail taken off the files quota
		{1000, 950, 100}, // never below the website minimum
	} {
		if got := filesQuotaMB(c.disk, c.mail); got != c.want {
			t.Errorf("filesQuotaMB(%d, %d) = %d, want %d", c.disk, c.mail, got, c.want)
		}
	}
}
