package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	Running   bool      `json:"running"`
	Log       string    `json:"log"`
}

// latestCache keeps the GitHub answer for an hour (unauthenticated API
// calls are rate limited).
var latestCache struct {
	sync.Mutex
	tag string
	at  time.Time
	err string
}

func latestRelease(ctx context.Context, force bool) (string, time.Time, string) {
	latestCache.Lock()
	defer latestCache.Unlock()
	if !force && time.Since(latestCache.at) < time.Hour {
		return latestCache.tag, latestCache.at, latestCache.err
	}
	tag, err := fetchLatestRelease(ctx)
	latestCache.at, latestCache.err = time.Now(), ""
	if err != nil {
		latestCache.err = err.Error()
	} else {
		latestCache.tag = tag
	}
	return latestCache.tag, latestCache.at, latestCache.err
}

func fetchLatestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAt, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub trả về %s", res.Status)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return "", err
	}
	if !version.Valid(rel.Tag) {
		return "", fmt.Errorf("bản phát hành không hợp lệ %q", rel.Tag)
	}
	return rel.Tag, nil
}

func (s *Service) Update(ctx context.Context, actor *users.User, check bool) (*UpdateState, error) {
	if err := adminOnly(actor); err != nil {
		return nil, err
	}
	st := &UpdateState{Current: version.Version}
	st.Latest, st.CheckedAt, st.CheckErr = latestRelease(ctx, check)
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
