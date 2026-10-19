package validate

import "testing"

func TestDomain(t *testing.T) {
	for _, ok := range []string{"example.com", "www.example.com", "a.b.c.vn", "xn--tm-hia.vn", "my-site.io"} {
		if err := Domain(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "localhost", "Example.com", "-a.com", "a-.com", "a..com", "a.com.", "a_b.com",
		"a.c", "evil.com;rm", "a.com/x", "*.a.com", "a com"} {
		if Domain(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestDocRoot(t *testing.T) {
	for _, ok := range []string{"", "public", "app/public", "web_root", "v1.2"} {
		if err := DocRoot(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"/etc", "..", "a/../b", "a//b", "a/", "./a", "a b", "a;b"} {
		if DocRoot(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestDB(t *testing.T) {
	if err := DBName("cust1", "cust1_shop"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"cust1_", "cust2_shop", "cust1shop", "cust1_Shop", "cust1_a-b", "cust1_a`b", "cust1_" + string(make([]byte, 60))} {
		if DBName("cust1", bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if err := DBPassword("Abc123!@#xyz"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"short1!", "has'quote12345", `back\slash123`, "has space 12345", "x\";DROP12345"} {
		if DBPassword(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestFTPLogin(t *testing.T) {
	if err := FTPLogin("cust1", "cust1_site"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"cust1_", "cust2_x", "cust1_" + "abcdefghijklmnopqrstuvwxyz0", "cust1_a b"} {
		if FTPLogin("cust1", bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestCron(t *testing.T) {
	for _, ok := range []string{"* * * * *", "*/5 * * * *", "0 3 * * 1-5", "15,45 0-6/2 1 * *", "@daily"} {
		if err := CronSchedule(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "* * * *", "@reboot", "* * * * * *", "a * * * *", "* * * * mon;id"} {
		if CronSchedule(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if CronCommand("php artisan schedule:run\n* * * * * evil") == nil {
		t.Error("multi-line command accepted")
	}
}

func TestMail(t *testing.T) {
	for _, ok := range []string{"info@example.com", "a.b-c_d@shop.example.vn", "x@a.io"} {
		if err := MailAddress(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"Info@example.com", "a..b@example.com", ".a@example.com", "a@", "a:b@example.com", "a b@example.com", "a@example.com\nx"} {
		if MailAddress(bad) == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if err := ForwardAddress("John.Doe+tag@gmail.com"); err != nil {
		t.Error(err)
	}
	if ForwardAddress("a,b@gmail.com") == nil || ForwardAddress("a@b@c.com") == nil {
		t.Error("bad forward accepted")
	}
	if BcryptHash("$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0") != nil {
		t.Error("valid bcrypt rejected")
	}
	if BcryptHash("plaintext") == nil || BcryptHash("$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXY:0") == nil {
		t.Error("bad hash accepted")
	}
}
