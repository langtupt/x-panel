package httpapi

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/ftpaccounts"
	"xpanel/pkg/agentproto"
)

const maxUpload = 1 << 30

// queryUserID reads the optional ?user_id= of the account whose files are used.
func queryUserID(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	v := r.URL.Query().Get("user_id")
	if v == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "user_id không hợp lệ")
		return nil, false
	}
	return &id, true
}

// GET /api/files?user_id=&path=
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryUserID(w, r)
	if !ok {
		return
	}
	res, err := s.accounts.FileOp(r.Context(), currentUser(r), uid,
		agentproto.FSRequest{Op: "list", Path: r.URL.Query().Get("path")})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /api/files/op {user_id, request}
func (s *Server) handleFileOp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID  *int64               `json:"user_id"`
		Request agentproto.FSRequest `json:"request"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.accounts.FileOp(r.Context(), currentUser(r), in.UserID, in.Request)
	if err != nil {
		s.fail(w, err)
		return
	}
	if in.Request.Op != "list" && in.Request.Op != "read" {
		s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "file." + in.Request.Op,
			TargetType: "user", TargetID: in.UserID, IP: clientIP(r),
			Meta: map[string]any{"path": in.Request.Path, "to": in.Request.To, "paths": in.Request.Paths}})
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /api/files/upload?user_id=&path= (raw body)
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryUserID(w, r)
	if !ok {
		return
	}
	p := r.URL.Query().Get("path")
	owner, err := s.accounts.Upload(r.Context(), currentUser(r), uid, p, http.MaxBytesReader(w, r.Body, maxUpload))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "file.upload", TargetType: "user",
		TargetID: &owner.ID, IP: clientIP(r), Meta: map[string]any{"path": p}})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/files/download?user_id=&path=
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	uid, ok := queryUserID(w, r)
	if !ok {
		return
	}
	p := r.URL.Query().Get("path")
	resp, err := s.accounts.Download(r.Context(), currentUser(r), uid, p)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer resp.Body.Close()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(p)}))
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		h.Set("Content-Length", cl)
	}
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) auditFTP(r *http.Request, action string, a *ftpaccounts.Account) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "ftp",
		TargetID: &a.ID, IP: clientIP(r), Meta: map[string]any{"login": a.Login, "dir": a.Dir}})
}

// GET /api/ftp
func (s *Server) handleListFTP(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListFTP(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/ftp
func (s *Server) handleCreateFTP(w http.ResponseWriter, r *http.Request) {
	var in accounts.FTPInput
	if !decode(w, r, &in) {
		return
	}
	a, err := s.accounts.CreateFTP(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditFTP(r, "ftp.create", a)
	writeJSON(w, http.StatusCreated, a)
}

// PATCH /api/ftp/{id}/password
func (s *Server) handleFTPPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in struct {
		Password string `json:"password"`
	}
	if !ok || !decode(w, r, &in) {
		return
	}
	a, err := s.accounts.SetFTPPassword(r.Context(), currentUser(r), id, in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditFTP(r, "ftp.password", a)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/ftp/{id}
func (s *Server) handleDeleteFTP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.accounts.DeleteFTP(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditFTP(r, "ftp.delete", a)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/phpmyadmin
func (s *Server) handlePMAStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.PMAStatus(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/phpmyadmin (admin) — queue installation / update.
func (s *Server) handleInstallPMA(w http.ResponseWriter, r *http.Request) {
	t, err := s.accounts.InstallPMA(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "pma.install", TargetType: "task",
		TargetID: &t.ID, IP: clientIP(r)})
	writeJSON(w, http.StatusAccepted, t)
}
