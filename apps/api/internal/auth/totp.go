package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): SHA-1, 30-second steps, 6 digits — what every
// authenticator app (Google Authenticator, Authy, 1Password…) expects.
const (
	totpStep   = 30
	totpDigits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPURL is the otpauth:// URI shown as a QR code.
func TOTPURL(secret, account string) string {
	v := url.Values{"secret": {secret}, "issuer": {"X-Panel"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape("X-Panel:"+account) + "?" + v.Encode()
}

func totpCode(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// VerifyTOTP accepts the current code and one step either side (clock drift).
func VerifyTOTP(secret, code string, now time.Time) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return false
	}
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil || len(key) < 20 {
		return false
	}
	step := uint64(now.Unix() / totpStep)
	for _, c := range []uint64{step - 1, step, step + 1} {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, c)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// secretBox encrypts TOTP secrets at rest with a key derived from the JWT secret.
type secretBox struct{ aead cipher.AEAD }

func newSecretBox(jwtSecret []byte) secretBox {
	key := sha256.Sum256(append([]byte("xpanel-totp:"), jwtSecret...))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return secretBox{aead: aead}
}

func (b secretBox) seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (b secretBox) open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("bad sealed secret")
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	return string(plain), err
}
