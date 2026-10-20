package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/audit"
	"xpanel/pkg/agentproto"
)

// GET /api/firewall (admin)
func (s *Server) handleFirewall(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.Firewall(r.Context(), currentUser(r), clientIP(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/firewall (admin) FirewallConfig
func (s *Server) handleSaveFirewall(w http.ResponseWriter, r *http.Request) {
	var in agentproto.FirewallConfig
	if !decode(w, r, &in) {
		return
	}
	st, err := s.accounts.SaveFirewall(r.Context(), currentUser(r), clientIP(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "firewall.update", IP: clientIP(r),
		Meta: map[string]any{"enabled": in.Enabled, "allow": len(in.Allow), "block": len(in.Block)}})
	writeJSON(w, http.StatusOK, st)
}

// POST /api/firewall/ban {ip, minutes, reason} (admin)
func (s *Server) handleFirewallBan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IP      string `json:"ip"`
		Minutes int    `json:"minutes"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.accounts.BanIP(r.Context(), currentUser(r), clientIP(r), in.IP, in.Minutes, in.Reason); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "firewall.ban", IP: clientIP(r),
		Meta: map[string]any{"ip": in.IP, "minutes": in.Minutes}})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/firewall/unban {ip} (admin)
func (s *Server) handleFirewallUnban(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IP string `json:"ip"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.accounts.UnbanIP(r.Context(), currentUser(r), in.IP); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "firewall.unban", IP: clientIP(r),
		Meta: map[string]any{"ip": in.IP}})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/firewall/unlock {username} (admin): lift a panel login lock.
func (s *Server) handleFirewallUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.accounts.UnlockAccount(currentUser(r), in.Username); err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "firewall.unlock", IP: clientIP(r),
		Meta: map[string]any{"username": in.Username}})
	w.WriteHeader(http.StatusNoContent)
}
