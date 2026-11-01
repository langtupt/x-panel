package mailer

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := Config{Mode: "smtp", Host: "smtp.gmail.com", Port: 587, Security: "starttls", From: "noreply@example.com"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{
		{Mode: "smtp", Host: "smtp.example.com\r\nX", Port: 587, Security: "starttls", From: "a@b.c"},
		{Mode: "smtp", Host: "smtp.example.com", Port: 0, Security: "starttls", From: "a@b.c"},
		{Mode: "smtp", Host: "smtp.example.com", Port: 25, Security: "ssl", From: "a@b.c"},
		{Mode: "smtp", Host: "smtp.example.com", Port: 25, Security: "none", From: "Name <a@b.c>"},
		{Mode: "smtp", Host: "smtp.example.com", Port: 25, Security: "none", From: "a@b.c", FromName: "x\r\nBcc: e@v.il"},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := (&Config{}).Validate(); err != nil {
		t.Errorf("empty local config: %v", err)
	}
	local := Config{Mode: "local", From: "noreply@congty.vn", FromName: "Hosting"}
	if err := local.Validate(); err != nil || local.Enabled {
		t.Errorf("local config: %v %+v", err, local)
	}
	if (&Config{Mode: "local", From: "x y"}).Validate() == nil {
		t.Error("bad local From accepted")
	}
}

func TestBuild(t *testing.T) {
	c := Config{From: "noreply@example.com", FromName: "Nhà cung cấp"}
	msg, err := c.build(Message{To: "khach@example.com", Subject: "Hosting sắp hết hạn", Body: "Xin chào\nDòng 2"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	head, body, _ := strings.Cut(s, "\r\n\r\n")
	if !strings.Contains(head, "To: <khach@example.com>") || !strings.Contains(head, "Subject: =?utf-8?q?") {
		t.Errorf("headers:\n%s", head)
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil || string(dec) != "Xin chào\r\nDòng 2" {
		t.Errorf("body %q %v", dec, err)
	}
	if _, err := c.build(Message{To: "a@b.c\r\nBcc: x@y.z", Subject: "x", Body: "y"}); err == nil {
		t.Error("header injection in To accepted")
	}
}
