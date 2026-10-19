package files

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/pkg/agentproto"
)

const opTimeout = 10 * time.Minute

// command prepares the confined helper for owner's home.
func command(ctx context.Context, owner string) (*exec.Cmd, error) {
	u, err := accounts.Managed(owner)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, self, "fshelper", u.HomeDir, u.Uid, u.Gid)
	cmd.Env = []string{"PATH=/nonexistent"}
	return cmd, nil
}

func helperErr(err error, stderr *bytes.Buffer) error {
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		// Strip the chroot-relative "open /x: " noise down to the reason only when useful.
		return errors.New(msg)
	}
	return err
}

func header(req agentproto.FSRequest) (*bytes.Reader, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(append(b, '\n')), nil
}

// Op runs a non-streaming operation.
func Op(ctx context.Context, p agentproto.FSOpParams) (*agentproto.FSResult, error) {
	if p.Request.Op == "upload" || p.Request.Op == "download" {
		return nil, errors.New("use the streaming endpoints for upload/download")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd, err := command(ctx, p.Owner)
	if err != nil {
		return nil, err
	}
	h, err := header(p.Request)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = h, &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, helperErr(err, &stderr)
	}
	res := &agentproto.FSResult{}
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func writeErr(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(agentproto.Response{Error: err.Error()})
}

// HandleUpload streams the request body into owner:path.
func HandleUpload(w http.ResponseWriter, r *http.Request) {
	cmd, err := command(r.Context(), r.URL.Query().Get("owner"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	h, err := header(agentproto.FSRequest{Op: "upload", Path: r.URL.Query().Get("path")})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stderr = io.MultiReader(h, r.Body), &stderr
	if err := cmd.Run(); err != nil {
		writeErr(w, http.StatusBadRequest, helperErr(err, &stderr))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentproto.Response{OK: true})
}

// HandleDownload streams owner:path to the response.
func HandleDownload(w http.ResponseWriter, r *http.Request) {
	cmd, err := command(r.Context(), r.URL.Query().Get("owner"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	h, err := header(agentproto.FSRequest{Op: "download", Path: r.URL.Query().Get("path")})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stderr = h, &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := cmd.Start(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	br := bufio.NewReader(stdout)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "OK ") {
		_ = cmd.Wait()
		writeErr(w, http.StatusBadRequest, helperErr(fmt.Errorf("download failed"), &stderr))
		return
	}
	if size, err := strconv.ParseInt(strings.TrimSpace(line[3:]), 10, 64); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, br)
	_ = cmd.Wait()
}
