package acme

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"xpanel/apps/agent/internal/run"
	"xpanel/apps/agent/internal/web"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/validate"
)

// The panel (port 8888) and FTPS serve this certificate; the installer
// creates a self-signed one, IssuePanel replaces it with Let's Encrypt.
const (
	PanelCert   = "/etc/xpanel/ssl/panel.crt"
	PanelKey    = "/etc/xpanel/ssl/panel.key"
	ftpdPEM     = "/etc/ssl/private/pure-ftpd.pem"
	panelVhost  = "/etc/nginx/conf.d/xpanel/_panel/acme.conf"
	panelPrefix = "# Managed by X-Panel: HTTP-01 challenges for the panel hostname.\n"
)

// PanelInfo reads the certificate currently served on the panel port.
func PanelInfo() (agentproto.PanelSSLInfo, error) {
	var info agentproto.PanelSSLInfo
	b, err := os.ReadFile(PanelCert)
	if err != nil {
		return info, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return info, errors.New("panel certificate is not PEM")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return info, err
	}
	info.Subject, info.Names, info.NotAfter = c.Subject.CommonName, c.DNSNames, c.NotAfter
	info.Issuer = c.Issuer.CommonName
	if len(c.Issuer.Organization) > 0 {
		info.Issuer = c.Issuer.Organization[0]
	}
	info.SelfSigned = c.Issuer.String() == c.Subject.String()
	return info, nil
}

// IssuePanel obtains a Let's Encrypt certificate for the panel hostname and
// installs it for the panel port and FTPS. The hostname must point to this
// server and port 80 must be reachable.
func (i *Issuer) IssuePanel(ctx context.Context, p agentproto.PanelSSLParams) (agentproto.SSLIssueResult, error) {
	var res agentproto.SSLIssueResult
	if err := validate.Domain(p.Domain); err != nil {
		return res, err
	}
	vhost := fmt.Sprintf(`%sserver {
    listen 80;
    listen [::]:80;
    server_name %s;
    location ^~ /.well-known/acme-challenge/ {
        root %s;
        default_type text/plain;
    }
    location / {
        return 301 https://$host:8888$request_uri;
    }
}
`, panelPrefix, p.Domain, web.ACMERoot)
	if err := os.MkdirAll(filepath.Dir(panelVhost), 0o755); err != nil {
		return res, err
	}
	old, _ := os.ReadFile(panelVhost)
	if err := os.WriteFile(panelVhost, []byte(vhost), 0o644); err != nil {
		return res, err
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		if old != nil {
			_ = os.WriteFile(panelVhost, old, 0o644)
		} else {
			_ = os.Remove(panelVhost)
		}
		return res, fmt.Errorf("nginx config test failed: %s", out)
	}
	if err := web.ReloadNginx(ctx); err != nil {
		return res, err
	}

	res, err := i.Issue(agentproto.SSLIssueParams{Domain: p.Domain, Names: []string{p.Domain}, Email: p.Email})
	if err != nil {
		return res, err
	}
	cert, key := web.CertFiles(p.Domain)
	certPEM, err := os.ReadFile(cert)
	if err != nil {
		return res, err
	}
	keyPEM, err := os.ReadFile(key)
	if err != nil {
		return res, err
	}
	// Keep the installer's self-signed certificate once, as a fallback.
	if _, err := os.Stat(PanelCert + ".selfsigned"); errors.Is(err, os.ErrNotExist) {
		if info, err := PanelInfo(); err == nil && info.SelfSigned {
			_ = copyFile(PanelCert, PanelCert+".selfsigned", 0o644)
			_ = copyFile(PanelKey, PanelKey+".selfsigned", 0o600)
		}
	}
	if err := os.WriteFile(PanelKey, keyPEM, 0o600); err != nil {
		return res, err
	}
	if err := os.WriteFile(PanelCert, certPEM, 0o644); err != nil {
		return res, err
	}
	if out, err := run.Output(ctx, "nginx", "-t"); err != nil {
		return res, fmt.Errorf("nginx config test failed: %s", out)
	}
	if err := web.ReloadNginx(ctx); err != nil {
		return res, err
	}
	// FTPS (Pure-FTPd) wants key and certificate in one file.
	if _, err := os.Stat(ftpdPEM); err == nil {
		if err := os.WriteFile(ftpdPEM, append(append([]byte{}, keyPEM...), certPEM...), 0o600); err == nil {
			_ = run.Cmd(ctx, "systemctl", "restart", "pure-ftpd")
		}
	}
	// Mail services use the panel certificate too.
	_ = run.Cmd(ctx, "systemctl", "try-reload-or-restart", "postfix", "dovecot")
	return res, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}
