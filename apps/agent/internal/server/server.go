// Package server exposes whitelisted agent actions on a unix socket.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"time"

	"xpanel/pkg/agentproto"
)

// Handler executes one action. params is the raw JSON sent by the API and
// must be validated by the handler before touching the system.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

type Server struct {
	handlers map[string]Handler
	raw      map[string]http.HandlerFunc
	log      *slog.Logger
}

func New(log *slog.Logger) *Server {
	return &Server{handlers: map[string]Handler{}, raw: map[string]http.HandlerFunc{}, log: log}
}

func (s *Server) Register(action string, h Handler) {
	s.handlers[action] = h
}

// Typed adapts a handler taking a decoded params struct. Unknown fields are rejected.
func Typed[P any](fn func(ctx context.Context, p P) (any, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if len(raw) > 0 {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}
		return fn(ctx, p)
	}
}

// Listen creates the socket with mode 0660 and, if group is set, hands it to
// that group so only the panel's service user can connect.
func (s *Server) Listen(ctx context.Context, socket, group string) error {
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(socket, 0o660); err != nil {
		return err
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return fmt.Errorf("lookup group %q: %w", group, err)
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(socket, 0, gid); err != nil {
			return err
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/call", s.handleCall)
	for pattern, h := range s.raw {
		mux.HandleFunc(pattern, h)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("agent listening", "socket", socket)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	var req agentproto.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		writeResp(w, nil, errors.New("invalid request"))
		return
	}
	h, ok := s.handlers[req.Action]
	if !ok {
		writeResp(w, nil, fmt.Errorf("unknown action %q", req.Action))
		return
	}
	start := time.Now()
	data, err := h(r.Context(), req.Params)
	s.log.Info("action", "action", req.Action, "ok", err == nil, "dur", time.Since(start))
	writeResp(w, data, err)
}

func writeResp(w http.ResponseWriter, data any, err error) {
	resp := agentproto.Response{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	} else if data != nil {
		raw, mErr := json.Marshal(data)
		if mErr != nil {
			resp = agentproto.Response{Error: mErr.Error()}
		} else {
			resp.Data = raw
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleRaw registers a plain HTTP handler (streaming endpoints) before Listen.
func (s *Server) HandleRaw(pattern string, h http.HandlerFunc) {
	s.raw[pattern] = h
}
