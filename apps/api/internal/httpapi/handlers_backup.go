package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
)

func (s *Server) auditBackup(r *http.Request, action string, id int64, meta map[string]any) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "backup",
		TargetID: &id, IP: clientIP(r), Meta: meta})
}

// GET /api/backups/settings (the hosting account itself)
func (s *Server) handleBackupSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.BackupSettings(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/backups/settings (the hosting account itself) {schedule, hour, keep, passphrase?}
func (s *Server) handleSaveBackupSettings(w http.ResponseWriter, r *http.Request) {
	var in accounts.BackupSettingsInput
	if !decode(w, r, &in) {
		return
	}
	st, err := s.accounts.SaveBackupSettings(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "backup.settings", IP: clientIP(r),
		Meta: map[string]any{"schedule": in.Schedule, "hour": in.Hour, "keep": in.Keep, "passphrase_changed": in.Passphrase != nil}})
	writeJSON(w, http.StatusOK, st)
}

// POST /api/backups/drive (the hosting account itself) {client_id, client_secret}
func (s *Server) handleConnectDrive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.accounts.ConnectDrive(r.Context(), currentUser(r), in.ClientID, in.ClientSecret)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "backup.drive_connect", IP: clientIP(r)})
	writeJSON(w, http.StatusOK, p)
}

// DELETE /api/backups/drive (the hosting account itself)
func (s *Server) handleDisconnectDrive(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.DisconnectDrive(r.Context(), currentUser(r)); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "backup.drive_disconnect", IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/backups
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListBackups(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/backups {user_id}
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.UserID == 0 {
		in.UserID = currentUser(r).ID
	}
	t, err := s.accounts.CreateBackup(r.Context(), currentUser(r), in.UserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "backup.create", TargetType: "user",
		TargetID: &in.UserID, IP: clientIP(r)})
	writeJSON(w, http.StatusAccepted, t)
}

// POST /api/backups/{id}/restore {files, databases, mail, passphrase?}
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.RestoreInput
	if !ok || !decode(w, r, &in) {
		return
	}
	t, err := s.accounts.RestoreBackup(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditBackup(r, "backup.restore", id, map[string]any{"files": in.Files, "databases": in.Databases, "mail": in.Mail})
	writeJSON(w, http.StatusAccepted, t)
}

// DELETE /api/backups/{id}
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := s.accounts.DeleteBackup(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditBackup(r, "backup.delete", id, map[string]any{"file": b.FileName})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/backups/import -> {"added": n}: backups found on Drive that this
// server does not list yet (made on another server).
func (s *Server) handleImportBackups(w http.ResponseWriter, r *http.Request) {
	n, err := s.accounts.ImportBackups(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": n})
}
