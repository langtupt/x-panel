// Package acme obtains Let's Encrypt certificates with the HTTP-01 challenge.
// Challenge files go to web.ACMERoot, which every generated vhost serves on
// port 80 under /.well-known/acme-challenge/.
package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/http/webroot"
	"github.com/go-acme/lego/v4/registration"

	"xpanel/apps/agent/internal/web"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

const (
	LetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"
	accountBase = "/var/lib/xpanel/acme-accounts"
)

type Issuer struct {
	DirectoryURL string // ACME directory, Let's Encrypt production by default
	Insecure     bool   // skip TLS verification of the ACME server (test CAs like Pebble only)

	mu sync.Mutex // one issuance at a time keeps account registration race-free
}

type account struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	key          crypto.PrivateKey
}

func (a *account) GetEmail() string                        { return a.Email }
func (a *account) GetRegistration() *registration.Resource { return a.Registration }
func (a *account) GetPrivateKey() crypto.PrivateKey        { return a.key }

func (i *Issuer) Issue(p agentproto.SSLIssueParams) (agentproto.SSLIssueResult, error) {
	var res agentproto.SSLIssueResult
	if len(p.Names) == 0 || p.Names[0] != p.Domain {
		return res, errors.New("names must start with the site domain")
	}
	for _, n := range p.Names {
		if err := validate.Domain(n); err != nil {
			return res, err
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	dirURL := i.DirectoryURL
	if dirURL == "" {
		dirURL = LetsEncrypt
	}
	acctDir := filepath.Join(accountBase, shortHash(dirURL))
	acct, err := loadAccount(acctDir, p.Email)
	if err != nil {
		return res, err
	}

	cfg := lego.NewConfig(acct)
	cfg.CADirURL = dirURL
	cfg.Certificate.KeyType = certcrypto.EC256
	if i.Insecure {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	}
	client, err := lego.NewClient(cfg)
	if err != nil {
		return res, err
	}
	provider, err := webroot.NewHTTPProvider(web.ACMERoot)
	if err != nil {
		return res, err
	}
	if err := client.Challenge.SetHTTP01Provider(provider); err != nil {
		return res, err
	}
	if acct.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return res, fmt.Errorf("acme register: %w", err)
		}
		acct.Registration = reg
		if err := saveAccount(acctDir, acct); err != nil {
			return res, err
		}
	}

	cert, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: p.Names, Bundle: true})
	if err != nil {
		return res, err
	}
	certFile, keyFile := web.CertFiles(p.Domain)
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return res, err
	}
	if err := os.WriteFile(keyFile, cert.PrivateKey, 0o600); err != nil {
		return res, err
	}
	if err := os.WriteFile(certFile, cert.Certificate, 0o644); err != nil {
		return res, err
	}
	res.ExpiresAt, err = notAfter(cert.Certificate)
	return res, err
}

func loadAccount(dir, email string) (*account, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, "account.key"))
	if errors.Is(err, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		return &account{Email: email, key: key}, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("bad account key")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	acct := &account{key: key}
	raw, err := os.ReadFile(filepath.Join(dir, "account.json"))
	if err != nil {
		return nil, err
	}
	return acct, json.Unmarshal(raw, acct)
}

func saveAccount(dir string, a *account) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	der, err := x509.MarshalECPrivateKey(a.key.(*ecdsa.PrivateKey))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "account.key"),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "account.json"), raw, 0o600)
}

func notAfter(bundle []byte) (time.Time, error) {
	block, _ := pem.Decode(bundle)
	if block == nil {
		return time.Time{}, errors.New("no certificate in bundle")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return c.NotAfter, nil
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}
