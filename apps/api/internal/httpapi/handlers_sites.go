package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
)

func (s *Server) auditSite(r *http.Request, action string, site *sites.Site, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["domain"] = site.Domain
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "site",
		TargetID: &site.ID, IP: clientIP(r), Meta: meta})
}

// GET /api/sites
func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListSites(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/sites
func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var in accounts.SiteInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.accounts.CreateSite(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.create", res.Site, map[string]any{"php": res.PHPVersion})
	writeJSON(w, http.StatusCreated, res)
}

// PUT /api/sites/{id}
func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.SiteInput
	if !ok || !decode(w, r, &in) {
		return
	}
	res, err := s.accounts.UpdateSite(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.update", res.Site, map[string]any{"php": res.PHPVersion, "aliases": res.Aliases})
	writeJSON(w, http.StatusOK, res)
}

// DELETE /api/sites/{id}
func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	site, err := s.accounts.DeleteSite(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.delete", site, nil)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/sites/{id}/ssl — queue a Let's Encrypt certificate.
func (s *Server) handleIssueSSL(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.accounts.IssueSSL(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.ssl_issue", &sites.Site{ID: id, Domain: t.Target}, nil)
	writeJSON(w, http.StatusAccepted, t)
}

// DELETE /api/sites/{id}/ssl
func (s *Server) handleDisableSSL(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.accounts.DisableSSL(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditSite(r, "site.ssl_disable", res.Site, nil)
	writeJSON(w, http.StatusOK, res)
}

// GET /api/php
func (s *Server) handleListPHP(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListPHP(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/php/{version} (admin) — queue installation.
func (s *Server) handleInstallPHP(w http.ResponseWriter, r *http.Request) {
	t, err := s.accounts.InstallPHP(r.Context(), currentUser(r), chi.URLParam(r, "version"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "php.install", TargetType: "task",
		TargetID: &t.ID, IP: clientIP(r), Meta: map[string]any{"version": chi.URLParam(r, "version")}})
	writeJSON(w, http.StatusAccepted, t)
}

// GET /api/tasks — admin sees everything, others the tasks they started.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	var uid *int64
	if me.Role != users.RoleAdmin {
		uid = &me.ID
	}
	list, err := s.tasks.List(r.Context(), uid, 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/tasks/{id}
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	me := currentUser(r)
	t, err := s.tasks.Get(r.Context(), id)
	if errors.Is(err, tasks.ErrNotFound) || (err == nil && me.Role != users.RoleAdmin && (t.UserID == nil || *t.UserID != me.ID)) {
		writeErr(w, http.StatusNotFound, "không tìm thấy tác vụ")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
