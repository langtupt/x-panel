// Package gdrive is a small Google Drive v3 client: OAuth device flow (for
// a server without a browser), resumable chunked uploads of streams of
// unknown length, downloads and deletes. It only uses the drive.file scope,
// so it sees nothing but the files X-Panel itself created.
package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Scope       = "https://www.googleapis.com/auth/drive.file openid email"
	folderMime  = "application/vnd.google-apps.folder"
	maxAttempts = 5
)

// Endpoints (variables for tests).
var (
	deviceURL = "https://oauth2.googleapis.com/device/code"
	tokenURL  = "https://oauth2.googleapis.com/token"
	revokeURL = "https://oauth2.googleapis.com/revoke"
	apiURL    = "https://www.googleapis.com/drive/v3"
	uploadURL = "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&fields=id,name,size"
	// ChunkSize must be a multiple of 256 KiB.
	ChunkSize = 16 << 20
)

var (
	ErrPending  = errors.New("authorization pending")
	ErrSlowDown = errors.New("slow down")
	ErrDenied   = errors.New("access denied by the user")
	ErrExpired  = errors.New("device code expired")
)

// Creds are what is needed to act for the connected Google account.
type Creds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

type File struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        string `json:"size"`
	CreatedTime string `json:"createdTime,omitempty"`
}

type Client struct {
	creds Creds
	http  *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(c Creds) *Client {
	return &Client{creds: c, http: &http.Client{Timeout: 10 * time.Minute}}
}

type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func postForm(ctx context.Context, hc *http.Client, u string, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e oauthError
		_ = json.Unmarshal(body, &e)
		switch e.Error {
		case "authorization_pending":
			return ErrPending
		case "slow_down":
			return ErrSlowDown
		case "access_denied":
			return ErrDenied
		case "expired_token":
			return ErrExpired
		}
		if e.Error != "" {
			return fmt.Errorf("google: %s: %s", e.Error, e.Description)
		}
		return fmt.Errorf("google: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// DeviceAuth is what the administrator must open and type to grant access.
type DeviceAuth struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func StartDevice(ctx context.Context, clientID string) (*DeviceAuth, error) {
	var d DeviceAuth
	err := postForm(ctx, http.DefaultClient, deviceURL, url.Values{"client_id": {clientID}, "scope": {Scope}}, &d)
	if d.Interval == 0 {
		d.Interval = 5
	}
	return &d, err
}

// PollDevice exchanges an approved device code for a refresh token.
func PollDevice(ctx context.Context, clientID, clientSecret, deviceCode string) (string, error) {
	var t struct {
		RefreshToken string `json:"refresh_token"`
	}
	err := postForm(ctx, http.DefaultClient, tokenURL, url.Values{
		"client_id": {clientID}, "client_secret": {clientSecret}, "device_code": {deviceCode},
		"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &t)
	if err == nil && t.RefreshToken == "" {
		err = errors.New("google did not return a refresh token")
	}
	return t.RefreshToken, err
}

func Revoke(ctx context.Context, refreshToken string) error {
	return postForm(ctx, http.DefaultClient, revokeURL, url.Values{"token": {refreshToken}}, nil)
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expires) > 2*time.Minute {
		return c.token, nil
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := postForm(ctx, c.http, tokenURL, url.Values{
		"client_id": {c.creds.ClientID}, "client_secret": {c.creds.ClientSecret},
		"refresh_token": {c.creds.RefreshToken}, "grant_type": {"refresh_token"},
	}, &t); err != nil {
		return "", err
	}
	c.token, c.expires = t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)
	return c.token, nil
}

// do sends an authorized request; body may be nil.
func (c *Client) do(ctx context.Context, method, u string, header http.Header, body []byte) (*http.Response, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return c.http.Do(req)
}

func apiErr(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("google drive: %s (%s)", e.Error.Message, resp.Status)
	}
	return fmt.Errorf("google drive: %s", resp.Status)
}

// Email returns the connected account's address.
func (c *Client) Email(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, apiURL+"/about?fields=user(emailAddress)", nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apiErr(resp)
	}
	var a struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
	}
	return a.User.EmailAddress, json.NewDecoder(resp.Body).Decode(&a)
}

// EnsureFolder returns the id of a folder created by this app with the given
// name under parent ("" = My Drive), creating it when missing.
func (c *Client) EnsureFolder(ctx context.Context, name, parent string) (string, error) {
	q := fmt.Sprintf("name = '%s' and mimeType = '%s' and trashed = false", strings.ReplaceAll(name, "'", `\'`), folderMime)
	if parent != "" {
		q += fmt.Sprintf(" and '%s' in parents", parent)
	}
	resp, err := c.do(ctx, http.MethodGet, apiURL+"/files?fields=files(id)&q="+url.QueryEscape(q), nil, nil)
	if err != nil {
		return "", err
	}
	var list struct {
		Files []File `json:"files"`
	}
	err = func() error {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return apiErr(resp)
		}
		return json.NewDecoder(resp.Body).Decode(&list)
	}()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].ID, nil
	}
	meta := map[string]any{"name": name, "mimeType": folderMime}
	if parent != "" {
		meta["parents"] = []string{parent}
	}
	b, _ := json.Marshal(meta)
	resp, err = c.do(ctx, http.MethodPost, apiURL+"/files?fields=id", http.Header{"Content-Type": {"application/json"}}, b)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apiErr(resp)
	}
	var f File
	return f.ID, json.NewDecoder(resp.Body).Decode(&f)
}

// Upload streams r to a new file in folder. The length need not be known:
// data goes up in ChunkSize pieces of a resumable session, each retried on
// transient errors, so a backup never needs local disk space.
func (c *Client) Upload(ctx context.Context, name, folder string, r io.Reader, progress func(int64)) (*File, error) {
	meta, _ := json.Marshal(map[string]any{"name": name, "parents": []string{folder}})
	resp, err := c.do(ctx, http.MethodPost, uploadURL, http.Header{"Content-Type": {"application/json; charset=UTF-8"}}, meta)
	if err != nil {
		return nil, err
	}
	session := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusOK || session == "" {
		defer resp.Body.Close()
		return nil, apiErr(resp)
	}
	resp.Body.Close()

	buf := make([]byte, ChunkSize)
	var offset int64
	for {
		n, rerr := io.ReadFull(r, buf)
		last := rerr == io.EOF || rerr == io.ErrUnexpectedEOF
		if rerr != nil && !last {
			return nil, rerr
		}
		chunk := buf[:n]
		var rng string
		switch {
		case last && n == 0:
			rng = fmt.Sprintf("bytes */%d", offset)
		case last:
			rng = fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(n)-1, offset+int64(n))
		default:
			rng = fmt.Sprintf("bytes %d-%d/*", offset, offset+int64(n)-1)
		}
		f, err := c.putChunk(ctx, session, rng, chunk, last)
		if err != nil {
			return nil, err
		}
		offset += int64(n)
		if progress != nil {
			progress(offset)
		}
		if last {
			return f, nil
		}
	}
}

func (c *Client) putChunk(ctx context.Context, session, rng string, chunk []byte, last bool) (*File, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		resp, err := c.do(ctx, http.MethodPut, session, http.Header{"Content-Range": {rng}}, chunk)
		if err != nil {
			lastErr = err
			continue
		}
		switch {
		case resp.StatusCode == 308 && !last:
			resp.Body.Close()
			return nil, nil
		case (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated) && last:
			defer resp.Body.Close()
			var f File
			return &f, json.NewDecoder(resp.Body).Decode(&f)
		case resp.StatusCode >= 500 || resp.StatusCode == 429:
			lastErr = apiErr(resp)
			resp.Body.Close()
		default:
			defer resp.Body.Close()
			return nil, apiErr(resp)
		}
	}
	return nil, lastErr
}

// Download opens a file's content.
func (c *Client) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, apiURL+"/files/"+url.PathEscape(id)+"?alt=media", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, apiErr(resp)
	}
	return resp.Body, nil
}

// Delete removes a file; a file that is already gone is not an error.
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, apiURL+"/files/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return apiErr(resp)
}

// Size parses File.Size (Drive returns it as a string).
func (f *File) SizeBytes() int64 {
	n, _ := strconv.ParseInt(f.Size, 10, 64)
	return n
}

// XPANEL_GDRIVE_ENDPOINT points every Google endpoint at a test server
// (end-to-end tests only; set in the root-owned service environment).
func init() {
	base := strings.TrimRight(os.Getenv("XPANEL_GDRIVE_ENDPOINT"), "/")
	if base == "" {
		return
	}
	deviceURL, tokenURL, revokeURL = base+"/device/code", base+"/token", base+"/revoke"
	apiURL = base + "/drive/v3"
	uploadURL = base + "/upload/drive/v3/files?uploadType=resumable&fields=id,name,size"
}

// List returns the files (not folders) directly inside folder. With the
// drive.file scope Drive shows only files created by the same OAuth client,
// which is what lets another server find an account's backups.
func (c *Client) List(ctx context.Context, folder string) ([]File, error) {
	q := fmt.Sprintf("'%s' in parents and mimeType != '%s' and trashed = false", strings.ReplaceAll(folder, "'", `\'`), folderMime)
	var out []File
	token := ""
	for {
		u := apiURL + "/files?pageSize=1000&fields=nextPageToken,files(id,name,size,createdTime)&q=" + url.QueryEscape(q)
		if token != "" {
			u += "&pageToken=" + url.QueryEscape(token)
		}
		resp, err := c.do(ctx, http.MethodGet, u, nil, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Files         []File `json:"files"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = func() error {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return apiErr(resp)
			}
			return json.NewDecoder(resp.Body).Decode(&page)
		}()
		if err != nil {
			return nil, err
		}
		out = append(out, page.Files...)
		if page.NextPageToken == "" || len(out) > 100000 {
			return out, nil
		}
		token = page.NextPageToken
	}
}
