package accounts

import "testing"

func TestNormalizeAppInput(t *testing.T) {
	in := SiteInput{AppType: "laravel", PHPVersion: "8.3"}
	if err := normalizeAppInput(&in); err != nil || in.DocRoot != "public" {
		t.Fatalf("Laravel defaults: %+v %v", in, err)
	}
	for _, in := range []SiteInput{{AppType: "unknown"}, {AppType: "php"}, {AppType: "nodejs", Env: map[string]string{"HOST": "0.0.0.0"}}, {AppType: "go", Runtime: "../../bin"}} {
		if normalizeAppInput(&in) == nil {
			t.Fatalf("accepted invalid input %+v", in)
		}
	}
}
