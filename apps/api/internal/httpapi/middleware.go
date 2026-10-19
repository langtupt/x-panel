package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/users"
)

type ctxKey struct{}
type impKey struct{}

func currentUser(r *http.Request) *users.User {
	u, _ := r.Context().Value(ctxKey{}).(*users.User)
	return u
}

// impersonatorID is the admin/reseller acting as the current user (0 if none).
func impersonatorID(r *http.Request) int64 {
	id, _ := r.Context().Value(impKey{}).(int64)
	return id
}

// requireCSRFHeader blocks cross-site form posts: browsers cannot attach a
// custom header cross-origin without a CORS preflight, which we never allow.
func requireCSRFHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-XPanel") != "1" {
			writeErr(w, http.StatusForbidden, "missing X-XPanel header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate loads the user from the access cookie (or Bearer header for API clients).
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(accessCookie); err == nil {
			token = c.Value
		} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
		id, imp, err := auth.ParseAccess(s.auth.Secret(), token)
		if token == "" || err != nil {
			writeErr(w, http.StatusUnauthorized, "chưa đăng nhập")
			return
		}
		u, err := s.users.ByID(r.Context(), id)
		if err != nil || u.Status != users.StatusActive {
			writeErr(w, http.StatusUnauthorized, "chưa đăng nhập")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, u)
		ctx = context.WithValue(ctx, impKey{}, imp)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireRole(roles ...users.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(roles, currentUser(r).Role) {
				writeErr(w, http.StatusForbidden, "không có quyền")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
