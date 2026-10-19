package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/cronjobs"
	"xpanel/apps/api/internal/users"
)

func (s *Server) auditCron(r *http.Request, action string, j *cronjobs.Job) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "cron",
		TargetID: &j.ID, IP: clientIP(r), Meta: map[string]any{"schedule": j.Schedule, "command": j.Command, "user": j.Username}})
}

// GET /api/cron
func (s *Server) handleListCron(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListCron(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/cron
func (s *Server) handleCreateCron(w http.ResponseWriter, r *http.Request) {
	var in accounts.CronInput
	if !decode(w, r, &in) {
		return
	}
	j, err := s.accounts.CreateCron(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditCron(r, "cron.create", j)
	writeJSON(w, http.StatusCreated, j)
}

// PUT /api/cron/{id}
func (s *Server) handleUpdateCron(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.CronInput
	if !ok || !decode(w, r, &in) {
		return
	}
	j, err := s.accounts.UpdateCron(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditCron(r, "cron.update", j)
	writeJSON(w, http.StatusOK, j)
}

// DELETE /api/cron/{id}
func (s *Server) handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	j, err := s.accounts.DeleteCron(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditCron(r, "cron.delete", j)
	w.WriteHeader(http.StatusNoContent)
}

type auditRow struct {
	ID         int64          `json:"id"`
	Actor      *string        `json:"actor"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   *int64         `json:"target_id"`
	IP         string         `json:"ip"`
	Meta       map[string]any `json:"meta"`
	CreatedAt  time.Time      `json:"created_at"`
}

// GET /api/audit?before=<id> — admin sees everything; others see their own
// actions plus (resellers) those of their customers. 100 rows per page.
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	before := int64(1 << 62)
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			before = n
		}
	}
	q := `SELECT a.id, u.username, a.action, a.target_type, a.target_id, a.ip, a.meta, a.created_at
		FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_id WHERE a.id < $1`
	args := []any{before}
	switch me.Role {
	case users.RoleReseller:
		q += ` AND (a.actor_id = $2 OR u.parent_id = $2)`
		args = append(args, me.ID)
	case users.RoleUser:
		q += ` AND a.actor_id = $2`
		args = append(args, me.ID)
	}
	rows, err := s.pool.Query(r.Context(), q+` ORDER BY a.id DESC LIMIT 100`, args...)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	out := []auditRow{}
	for rows.Next() {
		var a auditRow
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.TargetType, &a.TargetID, &a.IP, &a.Meta, &a.CreatedAt); err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, out)
}
