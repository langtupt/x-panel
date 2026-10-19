package apps

import (
	"context"
	"strings"
	"testing"
	"xpanel/pkg/agentproto"
)

func TestRemoveRejectsUnknownOrUninstalled(t *testing.T) {
	c := &Catalog{}
	if _, err := c.Remove(context.Background(), "php", "8.5"); err == nil {
		t.Fatal("accepted unknown application")
	}
	c.apps = []app{{info: agentproto.AppInfo{ID: "nodejs"}, installed: func() []string { return []string{"22"} }}}
	if _, err := c.Remove(context.Background(), "nodejs", "24"); err == nil {
		t.Fatal("accepted uninstalled version")
	}
}

func TestRemoveRejectsTraversalAndProtectedDatabase(t *testing.T) {
	for _, tc := range []struct{ id, version, message string }{
		{"nodejs", "../outside", "không hợp lệ"},
		{"go", "..", "không hợp lệ"},
		{"mariadb", "11.4", "bảo vệ"},
		{"postgresql", "18", "bảo vệ"},
	} {
		t.Run(tc.id+tc.version, func(t *testing.T) {
			c := &Catalog{apps: []app{{info: agentproto.AppInfo{ID: tc.id}, installed: func() []string { return []string{tc.version} }}}}
			_, err := c.Remove(context.Background(), tc.id, tc.version)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
