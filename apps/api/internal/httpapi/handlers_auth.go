package httpapi

import (
	"errors"
	"net/http"
	"time"

	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/version"
)

const (
	accessCookie  = "xp_at"
	refreshCookie = "xp_rt"
	refreshPath   = "/api/auth"
	// Holds the real session's refresh token while impersonating.
	origRefreshCookie = "xp_rt_orig"
)

func (s *Server) setAuthCookies(w http.ResponseWriter, t auth.Tokens) {
	http.SetCookie(w, &http.Cookie{
		Name: accessCookie, Value: t.Access, Path: "/", MaxAge: int(auth.AccessTTL / time.Second),
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: t.Refresh, Path: refreshPath, MaxAge: int(auth.RefreshTTL / time.Second),
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearAuthCookies(w http.ResponseWriter) {
	for _, c := range []struct{ name, path string }{{accessCookie, "/"}, {refreshCookie, refreshPath}} {
		http.SetCookie(w, &http.Cookie{Name: c.name, Path: c.path, MaxAge: -1, HttpOnly: true,
			Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"` // TOTP, when two-factor login is enabled
	}
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	u, tokens, err := s.auth.Login(r.Context(), req.Username, req.Password, req.Code, ip, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrTOTPRequired):
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error(), "totp_required": true})
		return
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrSuspended), errors.Is(err, auth.ErrTOTPInvalid):
		s.audit.Record(r.Context(), audit.Entry{Action: "auth.login_failed", IP: ip, Meta: map[string]any{"username": req.Username}})
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	case errors.Is(err, auth.ErrTooManyAttempts):
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		s.internalError(w, err)
		return
	}
	s.setAuthCookies(w, tokens)
	action := "auth.login"
	if u.RecoveryCodeUsed {
		action = "auth.login_recovery_code"
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &u.ID, Action: action, IP: ip})
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "chưa đăng nhập")
		return
	}
	u, tokens, err := s.auth.Refresh(r.Context(), c.Value, clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrInvalidSession) || errors.Is(err, auth.ErrSuspended) {
		s.clearAuthCookies(w)
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.setAuthCookies(w, tokens)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// While impersonating, the parked original session ends too.
	for _, name := range []string{refreshCookie, origRefreshCookie} {
		if c, err := r.Cookie(name); err == nil {
			if err := s.auth.Logout(r.Context(), c.Value); err != nil {
				s.internalError(w, err)
				return
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: origRefreshCookie, Path: refreshPath, MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	s.clearAuthCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	*users.User
	PanelVersion string `json:"panel_version"`
	Impersonator *struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"impersonator"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	res := meResponse{User: currentUser(r), PanelVersion: version.Version}
	if imp := impersonatorID(r); imp != 0 {
		if u, err := s.users.ByID(r.Context(), imp); err == nil {
			res.Impersonator = &struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			}{u.ID, u.Username}
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// notImpersonating rejects account-security changes made while acting as someone else.
func notImpersonating(w http.ResponseWriter, r *http.Request) bool {
	if impersonatorID(r) != 0 {
		writeErr(w, http.StatusForbidden, "không thể thay đổi bảo mật khi đang đăng nhập thay")
		return false
	}
	return true
}

// POST /api/auth/impersonate {user_id} — act as a managed account. The
// caller's refresh token is parked in xp_rt_orig to return later.
func (s *Server) handleImpersonate(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	if impersonatorID(r) != 0 {
		writeErr(w, http.StatusForbidden, "đang đăng nhập thay, hãy quay lại tài khoản gốc trước")
		return
	}
	var in struct {
		UserID int64 `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	target, err := s.users.ByID(r.Context(), in.UserID)
	if err != nil || !users.CanManage(me, target) {
		writeErr(w, http.StatusNotFound, "không tìm thấy tài khoản")
		return
	}
	orig, err := r.Cookie(refreshCookie)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "chưa đăng nhập")
		return
	}
	tokens, err := s.auth.Impersonate(r.Context(), me, target, clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrSuspended) {
		writeErr(w, http.StatusConflict, "tài khoản đang bị khóa")
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: origRefreshCookie, Value: orig.Value, Path: refreshPath,
		MaxAge: int(auth.RefreshTTL / time.Second), HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	s.setAuthCookies(w, tokens)
	s.audit.Record(r.Context(), audit.Entry{ActorID: &me.ID, Action: "auth.impersonate", TargetType: "user",
		TargetID: &target.ID, IP: clientIP(r), Meta: map[string]any{"username": target.Username}})
	writeJSON(w, http.StatusOK, target)
}

// POST /api/auth/unimpersonate — end the impersonated session and restore the original one.
func (s *Server) handleUnimpersonate(w http.ResponseWriter, r *http.Request) {
	orig, err := r.Cookie(origRefreshCookie)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "không có phiên gốc để quay lại")
		return
	}
	if c, err := r.Cookie(refreshCookie); err == nil {
		_ = s.auth.Logout(r.Context(), c.Value)
	}
	u, tokens, err := s.auth.Refresh(r.Context(), orig.Value, clientIP(r), r.UserAgent())
	http.SetCookie(w, &http.Cookie{Name: origRefreshCookie, Path: refreshPath, MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	if err != nil {
		s.clearAuthCookies(w)
		writeErr(w, http.StatusUnauthorized, "phiên gốc đã hết hạn, hãy đăng nhập lại")
		return
	}
	s.setAuthCookies(w, tokens)
	writeJSON(w, http.StatusOK, u)
}

// POST /api/me/password {current, new}
func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	if !notImpersonating(w, r) {
		return
	}
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	if !auth.CheckPassword(me.PasswordHash, in.Current) {
		writeErr(w, http.StatusBadRequest, "mật khẩu hiện tại không đúng")
		return
	}
	if err := s.accounts.SetOwnPassword(r.Context(), me, in.New); err != nil {
		s.fail(w, err)
		return
	}
	// All sessions were revoked; keep this browser signed in with a fresh one.
	tokens, err := s.auth.Reissue(r.Context(), me, clientIP(r), r.UserAgent())
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.setAuthCookies(w, tokens)
	s.audit.Record(r.Context(), audit.Entry{ActorID: &me.ID, Action: "auth.password_change", IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/me/2fa/setup — a new secret to scan; nothing is stored yet.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	if !notImpersonating(w, r) {
		return
	}
	if currentUser(r).TOTPSecret != nil {
		writeErr(w, http.StatusConflict, auth.ErrTOTPEnabled.Error())
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "url": auth.TOTPURL(secret, currentUser(r).Username)})
}

// POST /api/me/2fa/enable {secret, code}
func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	if !notImpersonating(w, r) {
		return
	}
	var in struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	codes, err := s.auth.EnableTOTP(r.Context(), me, in.Secret, in.Code)
	if errors.Is(err, auth.ErrTOTPEnabled) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if errors.Is(err, auth.ErrTOTPInvalid) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &me.ID, Action: "auth.2fa_enable", IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// POST /api/me/2fa/disable {password, code}
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if !notImpersonating(w, r) {
		return
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	err := s.auth.DisableTOTP(r.Context(), me, in.Password, in.Code)
	if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrTOTPInvalid) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		s.internalError(w, err)
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &me.ID, Action: "auth.2fa_disable", IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/me/2fa/recovery-codes -> number of unused recovery codes
func (s *Server) handleRecoveryCodesLeft(w http.ResponseWriter, r *http.Request) {
	n, err := s.auth.RecoveryCodesLeft(r.Context(), currentUser(r).ID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"remaining": n})
}

// POST /api/me/2fa/recovery-codes {password, code} -> a new set (old ones stop working)
func (s *Server) handleRegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if !notImpersonating(w, r) {
		return
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	codes, err := s.auth.RegenerateRecoveryCodes(r.Context(), me, in.Password, in.Code)
	if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrTOTPInvalid) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit.Record(r.Context(), audit.Entry{ActorID: &me.ID, Action: "auth.2fa_recovery_regenerate", IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}
