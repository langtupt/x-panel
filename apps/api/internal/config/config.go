// Package config loads xpanel-api settings from environment variables
// (written to /etc/xpanel/xpanel.env by the installer).
package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Listen        string // XPANEL_LISTEN, default 127.0.0.1:8889 (nginx terminates TLS on :8888)
	DatabaseURL   string // XPANEL_DATABASE_URL
	JWTSecret     []byte // XPANEL_JWT_SECRET, >= 32 bytes
	AgentSocket   string // XPANEL_AGENT_SOCKET
	WebDir        string // XPANEL_WEB_DIR, built Next.js export; empty = API only
	SecureCookies bool   // XPANEL_SECURE_COOKIES, default true
	AgentDryRun   bool   // XPANEL_AGENT_DRY_RUN, development without an agent
}

func Load() (Config, error) {
	c := Config{
		Listen:        env("XPANEL_LISTEN", "127.0.0.1:8889"),
		DatabaseURL:   os.Getenv("XPANEL_DATABASE_URL"),
		JWTSecret:     []byte(os.Getenv("XPANEL_JWT_SECRET")),
		AgentSocket:   env("XPANEL_AGENT_SOCKET", "/run/xpanel/agent.sock"),
		WebDir:        os.Getenv("XPANEL_WEB_DIR"),
		SecureCookies: true,
	}
	for name, dst := range map[string]*bool{
		"XPANEL_SECURE_COOKIES": &c.SecureCookies,
		"XPANEL_AGENT_DRY_RUN":  &c.AgentDryRun,
	} {
		if v := os.Getenv(name); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return c, errors.New(name + " must be true/false")
			}
			*dst = b
		}
	}
	if c.DatabaseURL == "" {
		return c, errors.New("XPANEL_DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 32 {
		return c, errors.New("XPANEL_JWT_SECRET must be at least 32 characters")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
