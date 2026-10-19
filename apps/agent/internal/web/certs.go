package web

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"xpanel/pkg/validate"
)

// InstallCertificate stores a site's certificate (validated by the API) where
// the vhost template expects it; the caller re-syncs the site afterwards.
func InstallCertificate(domain, certPEM, keyPEM string) error {
	if err := validate.Domain(domain); err != nil {
		return err
	}
	if b, _ := pem.Decode([]byte(certPEM)); b == nil || b.Type != "CERTIFICATE" {
		return errors.New("invalid certificate PEM")
	}
	if _, err := x509.ParseCertificate(mustDER(certPEM)); err != nil {
		return err
	}
	if b, _ := pem.Decode([]byte(keyPEM)); b == nil {
		return errors.New("invalid private key PEM")
	}
	cert, key := CertFiles(domain)
	if err := os.MkdirAll(filepath.Dir(cert), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(key+".new", []byte(keyPEM), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(cert+".new", []byte(certPEM), 0o644); err != nil {
		return err
	}
	if err := os.Rename(key+".new", key); err != nil {
		return err
	}
	return os.Rename(cert+".new", cert)
}

func mustDER(certPEM string) []byte {
	b, _ := pem.Decode([]byte(certPEM))
	return b.Bytes
}
