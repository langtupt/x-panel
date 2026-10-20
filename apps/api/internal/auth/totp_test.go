package auth

import (
	"context"
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"time"
	"xpanel/apps/api/internal/users"
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

func TestTOTPReplayNormalized(t *testing.T) {
	s := NewService(nil, nil, []byte("test-secret"))
	secret, _ := NewTOTPSecret()
	sealed, _ := s.box.seal(secret)
	u := &users.User{ID: 42, TOTPSecret: &sealed}
	key, _ := b32.DecodeString(secret)
	code := totpCode(key, uint64(time.Now().Unix()/30))
	if err := s.checkTOTP(u, code); err != nil {
		t.Fatal(err)
	}
	if err := s.checkTOTP(u, code[:3]+" "+code[3:]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("replayed code accepted: %v", err)
	}
	if _, err := s.EnableTOTP(context.Background(), u, secret, code); !errors.Is(err, ErrTOTPEnabled) {
		t.Fatalf("existing factor replaced: %v", err)
	}
}
func TestTOTPRejectWeakSecret(t *testing.T) {
	if VerifyTOTP("", totpCode(nil, uint64(time.Now().Unix()/30)), time.Now()) {
		t.Fatal("empty key accepted")
	}
}

func TestRecoveryCodeShape(t *testing.T) {
	s := NewService(nil, nil, []byte("test-secret"))
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := newRecoveryCode()
		if err != nil || !IsRecoveryCode(c) || seen[c] {
			t.Fatalf("bad or repeated code %q: %v", c, err)
		}
		seen[c] = true
		// Users may type it in capitals or without the dash.
		if s.recoveryHash(c) != s.recoveryHash(strings.ToUpper(strings.ReplaceAll(c, "-", ""))) {
			t.Fatal("normalisation differs")
		}
	}
	if IsRecoveryCode("123456") || IsRecoveryCode("abcde-fghij-k") {
		t.Fatal("non-recovery input accepted")
	}
	if NewService(nil, nil, []byte("other")).recoveryHash("abcde-fghij") == s.recoveryHash("abcde-fghij") {
		t.Fatal("hash does not depend on the server secret")
	}
}
