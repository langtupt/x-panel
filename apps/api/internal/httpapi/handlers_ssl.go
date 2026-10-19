package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/audit"
)

// POST /api/sites/{id}/ssl/custom {certificate, private_key}
func (s *Server) handleUploadSSL(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"private_key"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	res, err := s.accounts.UploadSSL(r.Context(), currentUser(r), id, in.Certificate, in.PrivateKey)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.ssl_upload", res.Site, map[string]any{"issuer": res.SSLIssuer})
	writeJSON(w, http.StatusOK, res)
}

// POST /api/sites/{id}/ssl/selfsigned
func (s *Server) handleSelfSignedSSL(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.accounts.SelfSignedSSL(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.ssl_selfsigned", res.Site, nil)
	writeJSON(w, http.StatusOK, res)
}

// GET /api/system/panel-ssl (admin)
func (s *Server) handlePanelSSL(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.PanelSSL(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/system/panel-ssl {domain} (admin) — queue a Let's Encrypt certificate.
func (s *Server) handleIssuePanelSSL(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := s.accounts.IssuePanelSSL(r.Context(), currentUser(r), in.Domain)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "system.panel_ssl", TargetType: "task",
		TargetID: &t.ID, IP: clientIP(r), Meta: map[string]any{"domain": t.Target}})
	writeJSON(w, http.StatusAccepted, t)
}
