package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/settings"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
	"xpanel/pkg/version"
)

const (
	updateAutoKey   = "update.auto"
	latestReleaseAt = "https://api.github.com/repos/langtupt/x-panel/releases/latest"
)

// UpdateState is what the admin update page shows.
type UpdateState struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Available bool      `json:"available"`
	Auto      bool      `json:"auto"`
	CheckedAt time.Time `json:"checked_at"`
	CheckErr  string    `json:"check_error,omitempty"`
	// Release notes of Latest (GitHub markdown), when known.
	Notes       string     `json:"notes,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	URL         string     `json:"url,omitempty"`
	Running     bool       `json:"running"`
	Log         string     `json:"log"`
}

// latestCache keeps the GitHub answer for an hour (unauthenticated API
// calls are rate limited).
var latestCache struct {
	sync.Mutex
	rel release
	at  time.Time
	err string
}

type release struct {
	Tag       string     `json:"tag_name"`
	Body      string     `json:"body"`
	Published *time.Time `json:"published_at"`
	URL       string     `json:"html_url"`
}

func latestRelease(ctx context.Context, force bool) (string, time.Time, string) {
	rel, at, err := latestReleaseInfo(ctx, force)
	return rel.Tag, at, err
}

func latestReleaseInfo(ctx context.Context, force bool) (release, time.Time, string) {
	latestCache.Lock()
	defer latestCache.Unlock()
	if !force && time.Since(latestCache.at) < time.Hour {
		return latestCache.rel, latestCache.at, latestCache.err
	}
	rel, err := fetchLatestRelease(ctx)
	latestCache.at, latestCache.err = time.Now(), ""
	if err != nil {
		latestCache.err = err.Error()
	} else {
		latestCache.rel = rel
	}
	return latestCache.rel, latestCache.at, latestCache.err
}

func fetchLatestRelease(ctx context.Context) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAt, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("GitHub trả về %s", res.Status)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&rel); err != nil {
		return release{}, err
	}
	if !version.Valid(rel.Tag) {
		return release{}, fmt.Errorf("bản phát hành không hợp lệ %q", rel.Tag)
	}
	if len(rel.Body) > 20000 {
		rel.Body = rel.Body[:20000] + "…"
	}
	if !strings.HasPrefix(rel.URL, "https://github.com/") {
		rel.URL = ""
	}
	return rel, nil
}

func (s *Service) Update(ctx context.Context, actor *users.User, check bool) (*UpdateState, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	st := &UpdateState{Current: version.Version}
	var rel release
	rel, st.CheckedAt, st.CheckErr = latestReleaseInfo(ctx, check)
	st.Latest, st.Notes, st.PublishedAt, st.URL = rel.Tag, rel.Body, rel.Published, rel.URL
	st.Available = version.Newer(st.Latest, st.Current)
	auto, err := settings.NewRepo(s.pool).Get(ctx, updateAutoKey)
	if err != nil {
		return nil, err
	}
	st.Auto = auto == "1"
	var us agentproto.UpdateStatus
	if err := s.agent.Call(ctx, agentproto.ActionUpdateStatus, nil, &us); err != nil {
		return nil, apperr.Agent(err)
	}
	st.Running, st.Log = us.Running, us.Log
	return st, nil
}

// StartUpdate installs the latest release. The panel restarts while it runs.
func (s *Service) StartUpdate(ctx context.Context, actor *users.User) (string, error) {
	if err := adminOnly(actor); err != nil {
		return "", err
	}
	latest, _, checkErr := latestRelease(ctx, true)
	if checkErr != "" {
		return "", apperr.Conflict("không kiểm tra được bản mới: %s", checkErr)
	}
	if !version.Newer(latest, version.Version) {
		return "", apperr.Conflict("đang dùng bản mới nhất (%s)", version.Version)
	}
	if err := s.agent.Call(ctx, agentproto.ActionUpdateStart, agentproto.UpdateParams{Version: latest}, nil); err != nil {
		return "", apperr.Agent(err)
	}
	return latest, nil
}

func (s *Service) SetAutoUpdate(ctx context.Context, actor *users.User, on bool) error {
	if err := adminOnly(actor); err != nil {
		return err
	}
	v := "0"
	if on {
		v = "1"
	}
	return settings.NewRepo(s.pool).Set(ctx, updateAutoKey, v)
}

// RunAutoUpdate installs a newer release between 03:00 and 05:00 (server
// time) when the administrator turned automatic updates on.
func (s *Service) RunAutoUpdate(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if h := now.Hour(); h < 3 || h >= 5 || !version.Valid(version.Version) {
				continue
			}
			if auto, err := settings.NewRepo(s.pool).Get(ctx, updateAutoKey); err != nil || auto != "1" {
				continue
			}
			latest, _, checkErr := latestRelease(ctx, true)
			if checkErr != "" || !version.Newer(latest, version.Version) {
				continue
			}
			s.log.Info("automatic update", "from", version.Version, "to", latest)
			if err := s.agent.Call(ctx, agentproto.ActionUpdateStart, agentproto.UpdateParams{Version: latest}, nil); err != nil {
				s.log.Error("automatic update", "err", err)
			}
		}
	}
}
