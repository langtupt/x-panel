// Package httpapi wires the REST API and serves the built Next.js UI.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"xpanel/apps/api/internal/accounts"
	"xpanel/apps/api/internal/apperr"
	"xpanel/apps/api/internal/audit"
	"xpanel/apps/api/internal/auth"
	"xpanel/apps/api/internal/config"
	"xpanel/apps/api/internal/cronjobs"
	"xpanel/apps/api/internal/databases"
	"xpanel/apps/api/internal/ftpaccounts"
	"xpanel/apps/api/internal/packages"
	"xpanel/apps/api/internal/secretbox"
	"xpanel/apps/api/internal/sites"
	"xpanel/apps/api/internal/tasks"
	"xpanel/apps/api/internal/users"
	"xpanel/pkg/agentproto"
)

type Server struct {
	cfg      config.Config
	log      *slog.Logger
	pool     *pgxpool.Pool
	users    *users.Repo
	auth     *auth.Service
	accounts *accounts.Service
	audit    *audit.Logger
	agent    agentproto.Caller
	tasks    *tasks.Queue
}

func New(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) *Server {
	var agent agentproto.Caller = agentproto.NewClient(cfg.AgentSocket)
	if cfg.AgentDryRun {
		log.Warn("XPANEL_AGENT_DRY_RUN is on: no system changes will be made (development only)")
		agent = &agentproto.DryRun{Log: log}
	}
	repo := users.NewRepo(pool)
	authSvc := auth.NewService(pool, repo, cfg.JWTSecret)
	svc := accounts.NewService(pool, repo, packages.NewRepo(pool), sites.NewRepo(pool), databases.NewRepo(pool), ftpaccounts.NewRepo(pool), cronjobs.NewRepo(pool), agent, authSvc, log)
	queue := tasks.NewQueue(pool, log)
	svc.SetSecrets(secretbox.New(cfg.JWTSecret, "settings"))
	svc.RegisterTasks(queue)
	return &Server{
		cfg:      cfg,
		log:      log,
		pool:     pool,
		users:    repo,
		auth:     authSvc,
		accounts: svc,
		tasks:    queue,
		audit:    audit.New(pool, log),
		agent:    agent,
	}
}

// Background runs the task workers and the SSL renewal scan until ctx ends.
func (s *Server) Background(ctx context.Context) {
	go s.tasks.Run(ctx, 2)
	go s.accounts.RunRenewals(ctx)
	go s.accounts.RunStats(ctx)
	go s.accounts.RunBackups(ctx)
	go s.accounts.RunMalwareScans(ctx)
	go s.accounts.InitFirewall(ctx)
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(timeoutExceptTransfers(120 * time.Second))
	r.Use(securityHeaders)

	r.Route("/api", func(r chi.Router) {
		r.Use(requireCSRFHeader)
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)
		r.Post("/auth/logout", s.handleLogout)
		r.Post("/auth/unimpersonate", s.handleUnimpersonate)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/me", s.handleMe)
			r.Get("/me/overview", s.handleOverview)
			r.Post("/me/password", s.handleChangeOwnPassword)
			r.Post("/me/2fa/setup", s.handleTOTPSetup)
			r.Post("/me/2fa/enable", s.handleTOTPEnable)
			r.Post("/me/2fa/disable", s.handleTOTPDisable)
			r.Get("/me/2fa/recovery-codes", s.handleRecoveryCodesLeft)
			r.Post("/me/2fa/recovery-codes", s.handleRegenerateRecoveryCodes)

			r.Post("/sites/{id}/build", s.handleBuildSite)
			r.Get("/sites/{id}/detect", s.handleDetectSite)
			r.Get("/sites/{id}/logs", s.handleSiteLogs)
			r.Put("/sites/{id}/php", s.handleSitePHP)
			r.Get("/sites/{id}/php", s.handleGetSitePHP)
			r.Get("/sites", s.handleListSites)
			r.Post("/sites", s.handleCreateSite)
			r.Put("/sites/{id}", s.handleUpdateSite)
			r.Delete("/sites/{id}", s.handleDeleteSite)
			r.Post("/sites/{id}/ssl", s.handleIssueSSL)
			r.Delete("/sites/{id}/ssl", s.handleDisableSSL)
			r.Post("/sites/{id}/ssl/custom", s.handleUploadSSL)
			r.Post("/sites/{id}/ssl/selfsigned", s.handleSelfSignedSSL)
			r.Get("/php", s.handleListPHP)
			r.Get("/tasks", s.handleListTasks)
			r.Get("/tasks/{id}", s.handleGetTask)
			r.Get("/databases", s.handleListDatabases)
			r.Post("/databases", s.handleCreateDatabase)
			r.Patch("/databases/{id}/password", s.handleDatabasePassword)
			r.Delete("/databases/{id}", s.handleDeleteDatabase)
			r.Get("/cron", s.handleListCron)
			r.Post("/cron", s.handleCreateCron)
			r.Put("/cron/{id}", s.handleUpdateCron)
			r.Delete("/cron/{id}", s.handleDeleteCron)
			r.Get("/audit", s.handleListAudit)
			r.Get("/ftp", s.handleListFTP)
			r.Post("/ftp", s.handleCreateFTP)
			r.Patch("/ftp/{id}/password", s.handleFTPPassword)
			r.Delete("/ftp/{id}", s.handleDeleteFTP)
			r.Get("/stats/accounts", s.handleAccountStats)
			r.Get("/firewall", s.handleFirewall)
			r.Put("/firewall", s.handleSaveFirewall)
			r.Post("/firewall/ban", s.handleFirewallBan)
			r.Post("/firewall/unban", s.handleFirewallUnban)
			r.Post("/firewall/unlock", s.handleFirewallUnlock)
			r.Get("/malware/status", s.handleMalwareStatus)
			r.Post("/malware/install", s.handleMalwareInstall)
			r.Get("/malware/settings", s.handleMalwareSettings)
			r.Put("/malware/settings", s.handleSaveMalwareSettings)
			r.Get("/malware", s.handleMalwareOverview)
			r.Post("/malware/scan", s.handleMalwareScan)
			r.Post("/malware/findings/{id}/{action}", s.handleFindingAction)
			r.Get("/notifications", s.handleNotifications)
			r.Post("/notifications/read", s.handleNotificationsRead)
			r.Get("/backups/settings", s.handleBackupSettings)
			r.Put("/backups/settings", s.handleSaveBackupSettings)
			r.Post("/backups/drive", s.handleConnectDrive)
			r.Delete("/backups/drive", s.handleDisconnectDrive)
			r.Get("/backups", s.handleListBackups)
			r.Post("/backups", s.handleCreateBackup)
			r.Post("/backups/import", s.handleImportBackups)
			r.Post("/backups/{id}/restore", s.handleRestoreBackup)
			r.Delete("/backups/{id}", s.handleDeleteBackup)
			r.Get("/mail/status", s.handleMailStatus)
			r.Get("/mail/domains", s.handleListMailDomains)
			r.Post("/mail/domains", s.handleCreateMailDomain)
			r.Get("/mail/domains/{id}", s.handleMailDomain)
			r.Delete("/mail/domains/{id}", s.handleDeleteMailDomain)
			r.Post("/mail/domains/{id}/mailboxes", s.handleCreateMailbox)
			r.Patch("/mail/mailboxes/{id}", s.handleUpdateMailbox)
			r.Delete("/mail/mailboxes/{id}", s.handleDeleteMailbox)
			r.Post("/mail/domains/{id}/aliases", s.handleCreateAlias)
			r.Patch("/mail/aliases/{id}", s.handleUpdateAlias)
			r.Delete("/mail/aliases/{id}", s.handleDeleteAlias)
			r.Get("/files", s.handleListFiles)
			r.Post("/files/op", s.handleFileOp)
			r.Post("/files/upload", s.handleUpload)
			r.Get("/files/download", s.handleDownload)
			r.Get("/phpmyadmin", s.handlePMAStatus)

			r.Group(func(r chi.Router) {
				r.Use(requireRole(users.RoleAdmin, users.RoleReseller))
				r.Get("/users", s.handleListUsers)
				r.Post("/auth/impersonate", s.handleImpersonate)
				r.Post("/users", s.handleCreateUser)
				r.Patch("/users/{id}/status", s.handleSetUserStatus)
				r.Patch("/users/{id}/package", s.handleSetUserPackage)
				r.Patch("/users/{id}/password", s.handleResetPassword)
				r.Delete("/users/{id}", s.handleDeleteUser)

				r.Get("/packages", s.handleListPackages)
				r.Post("/packages", s.handleCreatePackage)
				r.Put("/packages/{id}", s.handleUpdatePackage)
				r.Delete("/packages/{id}", s.handleDeletePackage)
			})

			r.Group(func(r chi.Router) {
				r.Use(requireRole(users.RoleAdmin))
				r.Get("/system/info", s.handleSystemInfo)
				r.Get("/system/panel-ssl", s.handlePanelSSL)
				r.Post("/system/panel-ssl", s.handleIssuePanelSSL)
				r.Post("/system/reboot", s.handleSystemReboot)
				r.Get("/apps", s.handleListApps)
				r.Post("/apps/{app}/install", s.handleInstallApp)
				r.Post("/apps/{app}/remove", s.handleRemoveApp)
				r.Post("/php/{version}", s.handleInstallPHP)
				r.Get("/php/{version}/manage", s.handlePHPManagement)
				r.Get("/php/{version}/logs", s.handlePHPLogs)
				r.Post("/php/{version}/manage", s.handlePHPManagementApply)
				r.Post("/phpmyadmin", s.handleInstallPMA)
			})
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	})

	if s.cfg.WebDir != "" {
		r.NotFound(staticHandler(s.cfg.WebDir).ServeHTTP)
	}
	return r
}

// fail writes an apperr with its status, anything else as a logged 500.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		if ae.Status >= 500 {
			s.log.Error("request failed", "err", ae.Msg)
		}
		writeErr(w, ae.Status, ae.Msg)
		return
	}
	s.internalError(w, err)
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("internal error", "err", err)
	writeErr(w, http.StatusInternalServerError, "lỗi máy chủ")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// The UI is a static export with inline bootstrap scripts; everything
		// else must come from the panel itself.
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; "+
			"connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// timeoutExceptTransfers bounds request time, except file uploads/downloads
// which may legitimately stream for a long time.
func timeoutExceptTransfers(d time.Duration) func(http.Handler) http.Handler {
	timeout := middleware.Timeout(d)
	return func(next http.Handler) http.Handler {
		limited := timeout(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/files/upload" || r.URL.Path == "/api/files/download" {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}
