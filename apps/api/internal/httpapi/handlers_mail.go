package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/audit"
)

func (s *Server) auditMail(r *http.Request, action, targetType string, id int64, name string) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: targetType,
		TargetID: &id, IP: clientIP(r), Meta: map[string]any{"name": name}})
}

// GET /api/mail/status
func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.MailStatus(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GET /api/mail/domains
func (s *Server) handleListMailDomains(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListMailDomains(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/mail/domains/{id}
func (s *Server) handleMailDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.accounts.MailDomain(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// POST /api/mail/domains {user_id, domain}
func (s *Server) handleCreateMailDomain(w http.ResponseWriter, r *http.Request) {
	var in accounts.MailDomainInput
	if !decode(w, r, &in) {
		return
	}
	d, err := s.accounts.CreateMailDomain(r.Context(), currentUser(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.domain_create", "mail_domain", d.ID, d.Domain)
	writeJSON(w, http.StatusCreated, d)
}

// DELETE /api/mail/domains/{id}
func (s *Server) handleDeleteMailDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.accounts.DeleteMailDomain(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.domain_delete", "mail_domain", d.ID, d.Domain)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/mail/domains/{id}/mailboxes {local_part, password, quota_mb}
func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.MailboxInput
	if !ok || !decode(w, r, &in) {
		return
	}
	m, err := s.accounts.CreateMailbox(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.mailbox_create", "mailbox", m.ID, m.Address)
	writeJSON(w, http.StatusCreated, m)
}

// PATCH /api/mail/mailboxes/{id} {password?, quota_mb?}
func (s *Server) handleUpdateMailbox(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.MailboxUpdate
	if !ok || !decode(w, r, &in) {
		return
	}
	m, err := s.accounts.UpdateMailbox(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.mailbox_update", "mailbox", m.ID, m.Address)
	writeJSON(w, http.StatusOK, m)
}

// DELETE /api/mail/mailboxes/{id}
func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.accounts.DeleteMailbox(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.mailbox_delete", "mailbox", m.ID, m.Address)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/mail/domains/{id}/aliases {local_part, destinations}
func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in accounts.AliasInput
	if !ok || !decode(w, r, &in) {
		return
	}
	a, err := s.accounts.CreateAlias(r.Context(), currentUser(r), id, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.alias_create", "mail_alias", a.ID, a.Address)
	writeJSON(w, http.StatusCreated, a)
}

// DELETE /api/mail/aliases/{id}
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.accounts.DeleteAlias(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditMail(r, "mail.alias_delete", "mail_alias", a.ID, a.Address)
	w.WriteHeader(http.StatusNoContent)
}
