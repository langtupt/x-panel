package apps

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
	"xpanel/pkg/agentproto"
)

func TestRefreshFailurePreservesLastSuccessfulVersions(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	c := &Catalog{cache: map[string]cached{"php": {at: at, versions: []string{"8.5"}}}}
	c.Refresh()
	a := app{info: agentproto.AppInfo{ID: "php"}, available: func(context.Context) ([]string, error) { return nil, errors.New("offline") }}
	v, warning := c.availableCached(context.Background(), a)
	if !slices.Equal(v, []string{"8.5"}) || warning == "" || !c.cache["php"].at.Equal(at) {
		t.Fatalf("lost stale versions or freshness: %v %q", v, warning)
	}
	a.available = func(context.Context) ([]string, error) { return []string{"8.6", "8.5"}, nil }
	v, warning = c.availableCached(context.Background(), a)
	if len(v) != 2 || warning != "" || c.cache["php"].expired {
		t.Fatalf("refresh failed: %v %q", v, warning)
	}
}

func TestEmptyVersionsRemainArrayOnCacheHit(t *testing.T) {
	c := &Catalog{cache: map[string]cached{}}
	calls := 0
	a := app{info: agentproto.AppInfo{ID: "builtin"}, available: func(context.Context) ([]string, error) {
		calls++
		return nil, nil
	}}
	for i := 0; i < 2; i++ {
		versions, err := c.availableCached(context.Background(), a)
		if versions == nil || len(versions) != 0 || err != "" {
			t.Fatalf("lookup %d: versions=%#v, error=%q", i, versions, err)
		}
	}
	if calls != 1 {
		t.Fatalf("lookups=%d, want 1", calls)
	}
}

func TestPickNodeLines(t *testing.T) {
	var rel []nodeRelease
	_ = json.Unmarshal([]byte(`[
		{"version":"v26.12.0","lts":false},{"version":"v26.11.1","lts":false},
		{"version":"v25.9.0","lts":false},
		{"version":"v24.22.0","lts":"Krypton"},{"version":"v24.21.0","lts":"Krypton"},
		{"version":"v22.23.3","lts":"Jod"},{"version":"v20.20.2","lts":"Iron"},{"version":"v18.20.8","lts":"Hydrogen"}
	]`), &rel)
	got := pickNodeLines(rel)
	if want := []string{"26", "24", "22", "20"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if latestNode(rel)["24"].Version != "v24.22.0" {
		t.Fatal("latest of a line must be the first (newest) entry")
	}
}

func TestSortVersionsDesc(t *testing.T) {
	v := []string{"1.9.1", "1.27.2", "1.26.10", "1.26.9"}
	sortVersionsDesc(v)
	if want := []string{"1.27.2", "1.26.10", "1.26.9", "1.9.1"}; !slices.Equal(v, want) {
		t.Fatalf("got %v", v)
	}
}
