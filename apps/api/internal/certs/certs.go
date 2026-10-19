// Package certs validates uploaded TLS certificates and creates self-signed ones.
package certs

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const maxPEM = 64 << 10

// Info describes a validated certificate.
type Info struct {
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Names     []string  `json:"names"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	// Names of the site that the certificate does not cover (served anyway).
	Uncovered []string `json:"uncovered,omitempty"`
}

func issuerName(c *x509.Certificate) string {
	if len(c.Issuer.Organization) > 0 {
		return c.Issuer.Organization[0]
	}
	if c.Issuer.CommonName != "" {
		return c.Issuer.CommonName
	}
	return c.Issuer.String()
}

// Validate checks an uploaded certificate chain and private key for a site:
// PEM syntax, key ↔ certificate match, validity period and that the main
// domain is covered. It returns the normalized PEMs (leaf first, then chain).
func Validate(certPEM, keyPEM string, domain string, aliases []string, now time.Time) (string, string, *Info, error) {
	if len(certPEM) > maxPEM || len(keyPEM) > maxPEM {
		return "", "", nil, errors.New("chứng chỉ hoặc khóa quá lớn")
	}
	var chain []*x509.Certificate
	var certOut bytes.Buffer
	rest := []byte(strings.TrimSpace(certPEM))
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return "", "", nil, fmt.Errorf("chứng chỉ không hợp lệ: %v", err)
		}
		chain = append(chain, c)
		_ = pem.Encode(&certOut, &pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})
	}
	if len(chain) == 0 {
		return "", "", nil, errors.New("không tìm thấy chứng chỉ (cần định dạng PEM: -----BEGIN CERTIFICATE-----)")
	}
	leaf := chain[0]

	key, keyDER, keyType, err := parseKey(keyPEM)
	if err != nil {
		return "", "", nil, err
	}
	pub, ok := key.(interface{ Public() crypto.PublicKey })
	if !ok {
		return "", "", nil, errors.New("khóa riêng không được hỗ trợ")
	}
	if !samePublicKey(leaf.PublicKey, pub.Public()) {
		return "", "", nil, errors.New("khóa riêng không khớp với chứng chỉ")
	}
	if now.After(leaf.NotAfter) {
		return "", "", nil, fmt.Errorf("chứng chỉ đã hết hạn ngày %s", leaf.NotAfter.Format("02/01/2006"))
	}
	if now.Before(leaf.NotBefore) {
		return "", "", nil, fmt.Errorf("chứng chỉ chưa có hiệu lực (từ %s)", leaf.NotBefore.Format("02/01/2006"))
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return "", "", nil, fmt.Errorf("chứng chỉ không dành cho tên miền %s", domain)
	}
	info := &Info{Issuer: issuerName(leaf), Subject: leaf.Subject.CommonName, Names: leaf.DNSNames,
		NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}
	for _, a := range aliases {
		if leaf.VerifyHostname(a) != nil {
			info.Uncovered = append(info.Uncovered, a)
		}
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: keyType, Bytes: keyDER})
	return certOut.String(), string(keyOut), info, nil
}

func parseKey(keyPEM string) (any, []byte, string, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(keyPEM)))
	if block == nil {
		return nil, nil, "", errors.New("không tìm thấy khóa riêng (cần định dạng PEM: -----BEGIN PRIVATE KEY-----)")
	}
	if strings.Contains(block.Type, "ENCRYPTED") || block.Headers["Proc-Type"] != "" {
		return nil, nil, "", errors.New("khóa riêng đang được đặt mật khẩu; hãy tải lên khóa không mã hóa")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		return k, block.Bytes, block.Type, err
	case "EC PRIVATE KEY":
		k, err := x509.ParseECPrivateKey(block.Bytes)
		return k, block.Bytes, block.Type, err
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		return k, block.Bytes, block.Type, err
	}
	return nil, nil, "", fmt.Errorf("loại khóa %q không được hỗ trợ", block.Type)
}

func samePublicKey(a, b crypto.PublicKey) bool {
	switch k := a.(type) {
	case *rsa.PublicKey:
		return k.Equal(b)
	case *ecdsa.PublicKey:
		return k.Equal(b)
	case ed25519.PublicKey:
		return k.Equal(b)
	}
	return false
}

// SelfSigned creates a one-year ECDSA certificate for names (first = subject).
func SelfSigned(names []string, now time.Time) (string, string, time.Time, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", time.Time{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", time.Time{}, err
	}
	notAfter := now.AddDate(1, 0, 0)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: names[0], Organization: []string{"X-Panel self-signed"}},
		Issuer:                pkix.Name{CommonName: names[0], Organization: []string{"X-Panel self-signed"}},
		DNSNames:              names,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", time.Time{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), notAfter, nil
}
