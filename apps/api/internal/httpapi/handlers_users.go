package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/users"
)

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id không hợp lệ")
		return 0, false
	}
	return id, true
}

func (s *Server) auditUser(r *http.Request, action string, target *users.User, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["username"] = target.Username
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "user",
		TargetID: &target.ID, IP: clientIP(r), Meta: meta})
}

// GET /api/users — admin sees every account, reseller sees own users.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	var parent *int64
	if me.Role == users.RoleReseller {
		parent = &me.ID
	}
	list, err := s.users.List(r.Context(), parent)
	if err != nil {
		s.fail(w, err)
		return
	}
	quotaOn := s.accounts.AttachUsage(r.Context(), list)
	writeJSON(w, http.StatusOK, map[string]any{"users": list, "quota_enabled": quotaOn})
}

// POST /api/users
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in accounts.CreateInput
	if !decode(w, r, &in) {
		return
	}
	u, err := s.accounts.Create(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditUser(r, "user.create", u, map[string]any{"role": u.Role, "package_id": u.PackageID})
	writeJSON(w, http.StatusCreated, u)
}

// PATCH /api/users/{id}/status
func (s *Server) handleSetUserStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		Status users.Status `json:"status"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	u, err := s.accounts.SetStatus(r.Context(), currentUser(r), id, in.Status)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditUser(r, "user.status", u, map[string]any{"status": in.Status})
	writeJSON(w, http.StatusOK, u)
}

// PATCH /api/users/{id}/package
func (s *Server) handleSetUserPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		PackageID *int64 `json:"package_id"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	u, err := s.accounts.SetPackage(r.Context(), currentUser(r), id, in.PackageID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditUser(r, "user.package", u, map[string]any{"package_id": in.PackageID})
	writeJSON(w, http.StatusOK, u)
}

// PATCH /api/users/{id}/password
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		Password string `json:"password"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	if err := s.accounts.ResetPassword(r.Context(), currentUser(r), id, in.Password); err != nil {
		s.fail(w, err)
		return
	}
	s.auditUser(r, "user.password", &users.User{ID: id}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/users/{id}
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.accounts.Delete(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditUser(r, "user.delete", u, nil)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/me/overview — own package and usage (reseller, user).
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := s.accounts.Overview(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}
