package apps

import (
	"strings"
	"testing"
)

// Adminer connects before it calls login(), so the server must be pinned
// before adminer.php runs and in credentials(), never only in login().
func TestAdminerWrapperPinsServerBeforeConnecting(t *testing.T) {
	w := adminerWrapper
	include := strings.Index(w, "include __DIR__")
	for _, want := range []string{
		"const XPANEL_PG = '127.0.0.1:5433';",
		"$_GET['pgsql'] = XPANEL_PG;",
		"$_POST['auth']['server'] = XPANEL_PG;",
		"unset($_GET[$k]);",
	} {
		i := strings.Index(w, want)
		if i < 0 || i > include {
			t.Errorf("wrapper must contain %q before the include", want)
		}
	}
	if !strings.Contains(w, "function credentials() {\n            return [XPANEL_PG,") {
		t.Error("credentials() must return the pinned server")
	}
	for _, driver := range []string{"'server'", "'sqlite'", "'mssql'", "'oracle'", "'mongo'", "'elastic'"} {
		if !strings.Contains(w, driver) {
			t.Errorf("driver %s is not dropped", driver)
		}
	}
}
