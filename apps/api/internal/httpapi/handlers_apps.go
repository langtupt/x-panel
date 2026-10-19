package httpapi

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"xpanel/apps/api/internal/audit"
	"xpanel/pkg/agentproto"
)

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListApps(r.Context(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleInstallApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := agentproto.AppInstallParams{ID: chi.URLParam(r, "app"), Version: in.Version}
	t, err := s.accounts.InstallApp(r.Context(), currentUser(r), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "app.install", TargetType: "task", TargetID: &t.ID, IP: clientIP(r), Meta: map[string]any{"app": p.ID, "version": p.Version}})
	writeJSON(w, http.StatusAccepted, t)
}

func (s *Server) handleRemoveApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := agentproto.AppInstallParams{ID: chi.URLParam(r, "app"), Version: in.Version}
	t, err := s.accounts.RemoveApp(r.Context(), currentUser(r), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "app.remove", TargetType: "task", TargetID: &t.ID, IP: clientIP(r), Meta: map[string]any{"app": p.ID, "version": p.Version}})
	writeJSON(w, http.StatusAccepted, t)
}
