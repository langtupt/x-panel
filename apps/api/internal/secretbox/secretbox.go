// Package secretbox encrypts secrets kept in the panel database (OAuth
// tokens, the backup passphrase) with a key derived from the server's JWT
// secret, so a copy of the database alone does not reveal them.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

type Box struct{ aead cipher.AEAD }

func New(serverSecret []byte, purpose string) *Box {
	key := sha256.Sum256(append([]byte("xpanel-"+purpose+":"), serverSecret...))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return &Box{aead: aead}
}

func (b *Box) Seal(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("bad sealed secret")
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	return string(plain), err
}
