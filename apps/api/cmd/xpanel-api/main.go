// xpanel-api is the panel's HTTP API. It runs as the unprivileged "xpanel"
// user and delegates every system change to xpanel-agent.
//
//	xpanel-api [serve]                     run the HTTP server (migrates first)
//	xpanel-api migrate                     apply database migrations
//	xpanel-api create-admin -username u -password p [-email e]
//	xpanel-api reset-password -username u -password p
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/config"
	"xpanel/apps/api/internal/db"
	"xpanel/apps/api/internal/httpapi"
	"xpanel/apps/api/internal/users"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if err := run(cmd, args, log); err != nil {
		log.Error(cmd+" failed", "err", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string, log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch cmd {
	case "migrate":
		applied, err := db.Migrate(ctx, pool)
		log.Info("migrations applied", "versions", applied)
		return err

	case "create-admin":
		fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
		username := fs.String("username", "admin", "admin username")
		password := fs.String("password", "", "admin password (min 10 chars)")
		email := fs.String("email", "", "admin email")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if _, err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		if err := users.ValidateNew(*username, *password, *email); err != nil {
			return err
		}
		hash, err := auth.HashPassword(*password)
		if err != nil {
			return err
		}
		u := &users.User{Username: *username, Email: *email, PasswordHash: hash, Role: users.RoleAdmin}
		if err := users.NewRepo(pool).Create(ctx, u); err != nil {
			return err
		}
		fmt.Printf("admin %q created (id %d)\n", u.Username, u.ID)
		return nil

	case "reset-password":
		fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
		username := fs.String("username", "admin", "account username")
		password := fs.String("password", "", "new password (min 10 chars)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if len(*password) < users.MinPasswordLen {
			return fmt.Errorf("password must be at least %d characters", users.MinPasswordLen)
		}
		repo := users.NewRepo(pool)
		u, err := repo.ByUsername(ctx, *username)
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(*password)
		if err != nil {
			return err
		}
		if err := repo.SetPassword(ctx, u.ID, hash); err != nil {
			return err
		}
		if err := auth.NewService(pool, repo, cfg.JWTSecret).RevokeAll(ctx, u.ID); err != nil {
			return err
		}
		fmt.Printf("password of %q updated, existing sessions revoked\n", u.Username)
		return nil

	case "serve":
		applied, err := db.Migrate(ctx, pool)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			log.Info("migrations applied", "versions", applied)
		}
		api := httpapi.New(cfg, log, pool)
		api.Background(ctx)
		srv := &http.Server{
			Addr:              cfg.Listen,
			Handler:           api.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
		ln, err := listen(cfg.Listen)
		if err != nil {
			return err
		}
		log.Info("xpanel-api listening", "addr", cfg.Listen)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown command %q (serve | migrate | create-admin | reset-password)", cmd)
}

// listen opens "host:port" or "unix:/path". In production the API listens on
// a unix socket that only nginx may connect to (group www-data, 0660):
// customers' processes on the same machine cannot reach the API directly or
// forge the X-Real-IP header the login rate limit relies on.
func listen(addr string) (net.Listener, error) {
	path, ok := strings.CutPrefix(addr, "unix:")
	if !ok {
		return net.Listen("tcp", addr)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if g, err := user.LookupGroup("www-data"); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(path, -1, gid); err != nil {
			ln.Close()
			return nil, fmt.Errorf("chown %s: %w", path, err)
		}
	}
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
