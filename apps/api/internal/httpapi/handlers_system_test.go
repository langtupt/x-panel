package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"xpanel/apps/api/internal/users"
)

// No pool or agent is supplied: malformed confirmations must stop before either is used.
func TestRebootRequiresExactConfirmation(t *testing.T) {
	for _, body := range []string{`{}`, `{"confirmation":"reboot"}`, `{"confirmation":"REBOOT "}`, `{"confirmation":true}`} {
		req := httptest.NewRequest("POST", "/api/system/reboot", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		(&Server{}).handleSystemReboot(res, req)
		if res.Code != 400 {
			t.Errorf("%s: status %d", body, res.Code)
		}
	}
}

func TestRebootRejectsNonAdminAndImpersonation(t *testing.T) {
	for _, role := range []users.Role{users.RoleUser, users.RoleReseller} {
		req := httptest.NewRequest("POST", "/api/system/reboot", strings.NewReader(`{"confirmation":"REBOOT"}`))
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, &users.User{Role: role}))
		res := httptest.NewRecorder()
		requireRole(users.RoleAdmin)(http.HandlerFunc((&Server{}).handleSystemReboot)).ServeHTTP(res, req)
		if res.Code != 403 {
			t.Errorf("%s: got %d", role, res.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/system/reboot", strings.NewReader(`{"confirmation":"REBOOT"}`))
	req = req.WithContext(context.WithValue(req.Context(), impKey{}, int64(1)))
	res := httptest.NewRecorder()
	(&Server{}).handleSystemReboot(res, req)
	if res.Code != 403 {
		t.Errorf("impersonation: got %d", res.Code)
	}
}
