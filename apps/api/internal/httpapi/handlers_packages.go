package httpapi

import (
	"net/http"

	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/packages"
	"xpanel/apps/api/internal/users"
)

// packageInput is the writable part of a package.
type packageInput struct {
	Name     string     `json:"name"`
	Kind     users.Role `json:"kind"`
	Oversell bool       `json:"oversell"`
	packages.Limits
}

func (in packageInput) toPackage() packages.Package {
	return packages.Package{Name: in.Name, Kind: in.Kind, Oversell: in.Oversell, Limits: in.Limits}
}

func (s *Server) auditPackage(r *http.Request, action string, p *packages.Package) {
	s.audit.Record(r.Context(), audit.Entry{ActorID: &currentUser(r).ID, Action: action, TargetType: "package",
		TargetID: &p.ID, IP: clientIP(r), Meta: map[string]any{"name": p.Name}})
}

// GET /api/packages — packages owned by the caller.
func (s *Server) handleListPackages(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.ListPackages(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": list, "php_versions": packages.PHPVersions})
}

// POST /api/packages
func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	var in packageInput
	if !decode(w, r, &in) {
		return
	}
	p, err := s.accounts.CreatePackage(r.Context(), currentUser(r), in.toPackage())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditPackage(r, "package.create", p)
	writeJSON(w, http.StatusCreated, p)
}

// PUT /api/packages/{id}
func (s *Server) handleUpdatePackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	var in packageInput
	if !ok || !decode(w, r, &in) {
		return
	}
	p, err := s.accounts.UpdatePackage(r.Context(), currentUser(r), id, in.toPackage())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditPackage(r, "package.update", p)
	writeJSON(w, http.StatusOK, p)
}

// DELETE /api/packages/{id}
func (s *Server) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.accounts.DeletePackage(r.Context(), currentUser(r), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.auditPackage(r, "package.delete", p)
	w.WriteHeader(http.StatusNoContent)
}
