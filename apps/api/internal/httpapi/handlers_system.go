package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/audit"
	"xpanel/pkg/agentproto"
)

// GET /api/system/info (admin)
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var info agentproto.SystemInfo
	if err := s.agent.Call(r.Context(), agentproto.ActionSystemInfo, nil, &info); err != nil {
		s.log.Warn("agent call failed", "err", err)
		writeErr(w, http.StatusBadGateway, "không kết nối được xpanel-agent")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleSystemReboot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Confirmation != "REBOOT" {
		writeErr(w, http.StatusBadRequest, "nhập REBOOT để xác nhận khởi động lại máy chủ")
		return
	}
	if !notImpersonating(w, r) {
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(783621)`); err != nil {
		s.fail(w, err)
		return
	}
	var busy bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tasks WHERE status IN ('pending','running'))`).Scan(&busy); err != nil {
		s.fail(w, err)
		return
	}
	if busy {
		writeErr(w, http.StatusConflict, "đang có tác vụ chạy; chờ hoàn tất trước khi reboot")
		return
	}
	if err = s.agent.Call(r.Context(), agentproto.ActionSystemReboot, nil, nil); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "system.reboot", TargetType: "server", IP: clientIP(r)})
	writeJSON(w, http.StatusAccepted, map[string]any{"scheduled": true, "delay_seconds": 10})
}
