package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"xpanel/apps/api/internal/audit"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/phpconfig"
)

func (s *Server) handlePHPManagement(w http.ResponseWriter, r *http.Request) {
	out, err := s.accounts.PHPManagement(r.Context(), chi.URLParam(r, "version"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handlePHPManagementApply(w http.ResponseWriter, r *http.Request) {
	var p agentproto.PHPManageParams
	if !decode(w, r, &p) {
		return
	}
	p.Version = chi.URLParam(r, "version")
	t, err := s.accounts.ApplyPHPManagement(r.Context(), currentUser(r), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "php.manage", TargetType: "task", TargetID: &t.ID, IP: clientIP(r), Meta: map[string]any{"version": p.Version, "module": p.Module, "remove": p.Remove}})
	writeJSON(w, http.StatusAccepted, t)
}

func (s *Server) handleSitePHP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var c phpconfig.Site
	if !decode(w, r, &c) {
		return
	}
	if err := s.accounts.SaveSitePHP(r.Context(), currentUser(r), id, c); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "site.php_config", TargetType: "site", TargetID: &id, IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPLogs(w http.ResponseWriter, r *http.Request) {
	out, err := s.accounts.PHPLogs(r.Context(), chi.URLParam(r, "version"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSitePHP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	out, err := s.accounts.SitePHP(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
