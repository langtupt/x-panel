package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/databases"
)

func (s *Server) auditDatabase(r *http.Request, action string, d *databases.Database) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "database",
		TargetID: &d.ID, IP: clientIP(r), Meta: map[string]any{"name": d.Name}})
}

// GET /api/databases
func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListDatabases(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/databases
func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var in accounts.DatabaseInput
	if !decode(w, r, &in) {
		return
	}
	d, err := s.accounts.CreateDatabase(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditDatabase(r, "database.create", d)
	writeJSON(w, http.StatusCreated, d)
}

// PATCH /api/databases/{id}/password
func (s *Server) handleDatabasePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		Password string `json:"password"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	d, err := s.accounts.SetDatabasePassword(r.Context(), currentUser(r), id, in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditDatabase(r, "database.password", d)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/databases/{id}
func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.accounts.DeleteDatabase(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditDatabase(r, "database.delete", d)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/databases/{id}/backups
func (s *Server) handleDatabaseDumps(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.accounts.DatabaseDumps(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/databases/{id}/backups — queue a dump into ~/backups/databases.
func (s *Server) handleBackupDatabase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, d, err := s.accounts.BackupDatabase(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditDatabase(r, "database.backup", d)
	writeJSON(w, http.StatusAccepted, t)
}

// POST /api/databases/{id}/restore {file}
func (s *Server) handleRestoreDatabase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		File string `json:"file"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	t, d, err := s.accounts.RestoreDatabase(r.Context(), currentUser(r), id, in.File)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "database.restore", TargetType: "database",
		TargetID: &d.ID, IP: clientIP(r), Meta: map[string]any{"name": d.Name, "file": in.File}})
	writeJSON(w, http.StatusAccepted, t)
}

// DELETE /api/databases/{id}/backups/{file}
func (s *Server) handleDeleteDatabaseDump(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	file := chi.URLParam(r, "file")
	d, err := s.accounts.DeleteDatabaseDump(r.Context(), currentUser(r), id, file)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "database.backup_delete", TargetType: "database",
		TargetID: &d.ID, IP: clientIP(r), Meta: map[string]any{"name": d.Name, "file": file}})
	w.WriteHeader(http.StatusNoContent)
}
