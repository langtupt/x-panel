package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/audit"
)

// GET /api/system/update[?check=1] (admin) -> UpdateState
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.Update(r.Context(), currentUser(r), r.URL.Query().Get("check") == "1")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/system/update (admin): install the latest release now.
func (s *Server) handleStartUpdate(w http.ResponseWriter, r *http.Request) {
	v, err := s.accounts.StartUpdate(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "system.update", TargetType: "system",
		IP: clientIP(r), Meta: map[string]any{"version": v}})
	writeJSON(w, http.StatusAccepted, map[string]string{"version": v})
}

// PUT /api/system/update {"auto":bool} (admin)
func (s *Server) handleSaveUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Auto bool `json:"auto"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.accounts.SetAutoUpdate(r.Context(), currentUser(r), in.Auto); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "system.update.auto", TargetType: "system",
		IP: clientIP(r), Meta: map[string]any{"auto": in.Auto}})
	w.WriteHeader(http.StatusNoContent)
}
