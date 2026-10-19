package certs

import (
	"strings"
	"testing"
	"time"
)

func TestSelfSignedRoundTrip(t *testing.T) {
	now := time.Now()
	cert, key, exp, err := SelfSigned([]string{"example.com", "www.example.com"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(now.AddDate(0, 11, 0)) {
		t.Fatalf("expiry too soon: %v", exp)
	}
	gotCert, gotKey, info, err := Validate(cert, key, "example.com", []string{"www.example.com", "shop.example.com"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotCert, "BEGIN CERTIFICATE") || !strings.Contains(gotKey, "PRIVATE KEY") {
		t.Fatal("normalized PEM missing")
	}
	if len(info.Uncovered) != 1 || info.Uncovered[0] != "shop.example.com" {
		t.Fatalf("uncovered = %v", info.Uncovered)
	}
}

func TestValidateRejects(t *testing.T) {
	now := time.Now()
	certA, keyA, _, _ := SelfSigned([]string{"a.example.com"}, now)
	_, keyB, _, _ := SelfSigned([]string{"a.example.com"}, now)
	cases := map[string]struct {
		cert, key, domain string
		at                time.Time
	}{
		"key mismatch":   {certA, keyB, "a.example.com", now},
		"wrong domain":   {certA, keyA, "b.example.com", now},
		"expired":        {certA, keyA, "a.example.com", now.AddDate(2, 0, 0)},
		"not yet valid":  {certA, keyA, "a.example.com", now.AddDate(0, 0, -2)},
		"no certificate": {"hello", keyA, "a.example.com", now},
		"no key":         {certA, "hello", "a.example.com", now},
		"encrypted key":  {certA, "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----", "a.example.com", now},
	}
	for name, c := range cases {
		if _, _, _, err := Validate(c.cert, c.key, c.domain, nil, c.at); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
