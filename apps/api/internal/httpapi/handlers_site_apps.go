package httpapi

import (
	"net/http"
	"xpanel/apps/api/internal/audit"
	"xpanel/pkg/agentproto"
)

func (s *Server) handleBuildSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := s.accounts.BuildSite(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "site.build", TargetType: "site", TargetID: &id, IP: clientIP(r)})
	writeJSON(w, http.StatusAccepted, t)
}
func (s *Server) handleRestartSite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	out, err := s.accounts.RestartSite(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: "site.restart", TargetType: "site", TargetID: &id, IP: clientIP(r)})
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleDetectSite(w http.ResponseWriter, r *http.Request) {
	s.siteAppInfo(w, r, agentproto.ActionSiteDetect)
}
func (s *Server) handleSiteLogs(w http.ResponseWriter, r *http.Request) {
	s.siteAppInfo(w, r, agentproto.ActionSiteLogs)
}
func (s *Server) siteAppInfo(w http.ResponseWriter, r *http.Request, action string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	out, err := s.accounts.SiteAppInfo(r.Context(), currentUser(r), id, action)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
