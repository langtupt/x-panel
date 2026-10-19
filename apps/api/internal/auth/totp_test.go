package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA-1, 8 digits) truncated to 6 digits.
func TestTOTPVectors(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		if got := totpCode(key, uint64(c.unix/30)); got != c.want {
			t.Errorf("t=%d: got %s want %s", c.unix, got, c.want)
		}
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	now := time.Unix(1234567890, 0)
	if !VerifyTOTP(secret, "005924", now) {
		t.Error("current code rejected")
	}
	if !VerifyTOTP(secret, "005924", now.Add(30*time.Second)) {
		t.Error("previous-step code should be accepted")
	}
	if VerifyTOTP(secret, "005924", now.Add(90*time.Second)) {
		t.Error("stale code accepted")
	}
	if VerifyTOTP(secret, "12345", now) {
		t.Error("short code accepted")
	}
}

func TestSecretBox(t *testing.T) {
	b := newSecretBox([]byte("a-very-long-jwt-secret-for-testing-only"))
	sealed, err := b.seal("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.open(sealed)
	if err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := newSecretBox([]byte("another-secret-xxxxxxxxxxxxxxxxxxxx")).open(sealed); err == nil {
		t.Error("opened with wrong key")
	}
}
