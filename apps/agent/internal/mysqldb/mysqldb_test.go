package mysqldb

import (
	"strings"
	"testing"

	"xpanel/pkg/agentproto"
)

func TestQuoting(t *testing.T) {
	if got := grantIdent("cust1_shop_db"); got != "`cust1\\_shop\\_db`" {
		t.Errorf("grantIdent = %s", got)
	}
	if got := str(`a'b\c`); got != `'a\'b\\c'` {
		t.Errorf("str = %s", got)
	}
	if got := account("cust1_shop", "localhost"); got != "'cust1_shop'@'localhost'" {
		t.Errorf("account = %s", got)
	}
}

func TestDropSQL(t *testing.T) {
	sql := dropSQL(agentproto.DBParams{Owner: "cust1", Name: "cust1_shop", User: "cust1_shop"})
	for _, want := range []string{"DROP DATABASE IF EXISTS `cust1_shop`;", "DROP USER IF EXISTS 'cust1_shop'@'localhost';",
		"DROP USER IF EXISTS 'cust1_shop'@'127.0.0.1';"} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
}

func TestRejectsForeignNames(t *testing.T) {
	ctx := t.Context()
	bad := []agentproto.DBParams{
		{Owner: "cust1", Name: "cust2_shop", User: "cust1_shop"},
		{Owner: "cust1", Name: "cust1_shop", User: "root"},
		{Owner: "cust1", Name: "cust1_a`; DROP DATABASE mysql; --", User: "cust1_a"},
	}
	for _, p := range bad {
		if err := Delete(ctx, p); err == nil {
			t.Errorf("Delete(%+v) should be rejected before running SQL", p)
		}
	}
	if err := SetPassword(ctx, agentproto.DBParams{Owner: "cust1", User: "cust1_a", Password: "x' OR '1'='1"}); err == nil {
		t.Error("password with quotes must be rejected")
	}
}
