package backup

import (
	"testing"

	"xpanel/pkg/agentproto"
)

func TestDumpFileName(t *testing.T) {
	db := agentproto.BackupDB{Name: "cust_shop", Engine: "mariadb", User: "cust_shop"}
	for name, ok := range map[string]bool{
		"cust_shop-20261101-101010.sql.gz":             true,
		"cust_shop-20261101-101010-pre-restore.sql.gz": true,
		"cust_shop-upload-1.sql":                       true,
		"cust_shop_v2-20261101-101010.sql.gz":          false, // another database
		"cust_shop-x.sql.zip":                          false,
		"cust_shop-../../etc/passwd.sql":               false,
		"cust_shop-a b.sql":                            false,
		".cust_shop-1.sql.gz.part":                     false,
		"other-1.sql":                                  false,
	} {
		if got := dumpFile(agentproto.DBDumpParams{DB: db, File: name}) == nil; got != ok {
			t.Errorf("%q: got %v want %v", name, got, ok)
		}
	}
}
