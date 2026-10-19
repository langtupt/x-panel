package agentproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

// Client talks to xpanel-agent over its unix socket.
type Client struct {
	hc *http.Client
}

func NewClient(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	// No client timeout: long actions (php.install) are bounded by the caller's context.
	return &Client{hc: &http.Client{Transport: tr}}
}

// Call runs an action on the agent and decodes its data into out (may be nil).
func (c *Client) Call(ctx context.Context, action string, params, out any) error {
	req := Request{Action: action}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/call", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return fmt.Errorf("agent unreachable: %w", err)
	}
	defer resp.Body.Close()

	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("agent: bad response: %w", err)
	}
	if !r.OK {
		return errors.New("agent: " + r.Error)
	}
	if out != nil && len(r.Data) > 0 {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

// Streamer is implemented by Client (not DryRun): raw file transfer.
type Streamer interface {
	Upload(ctx context.Context, owner, path string, body io.Reader) error
	Download(ctx context.Context, owner, path string) (*http.Response, error)
}

func streamURL(p, owner, path string) string {
	return "http://agent" + p + "?" + url.Values{"owner": {owner}, "path": {path}}.Encode()
}

func (c *Client) Upload(ctx context.Context, owner, path string, body io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL(PathFSUpload, owner, path), body)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("agent unreachable: %w", err)
	}
	defer resp.Body.Close()
	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("agent: bad response: %w", err)
	}
	if !r.OK {
		return errors.New("agent: " + r.Error)
	}
	return nil
}

// Download returns the agent response; on success the body is the file.
func (c *Client) Download(ctx context.Context, owner, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL(PathFSDownload, owner, path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent unreachable: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var r Response
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return nil, errors.New("agent: " + r.Error)
	}
	return resp, nil
}
