// xpanel-agent runs as root and performs privileged system changes on
// behalf of xpanel-api. It only listens on a local unix socket.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"xpanel/apps/agent/internal/accounts"
	"xpanel/apps/agent/internal/acme"
	"xpanel/apps/agent/internal/apps"
	"xpanel/apps/agent/internal/backup"
	"xpanel/apps/agent/internal/cron"
	"xpanel/apps/agent/internal/distro"
	"xpanel/apps/agent/internal/files"
	"xpanel/apps/agent/internal/firewall"
	"xpanel/apps/agent/internal/ftp"
	"xpanel/apps/agent/internal/mail"
	"xpanel/apps/agent/internal/malware"
	"xpanel/apps/agent/internal/mysqldb"
	"xpanel/apps/agent/internal/netguard"
	"xpanel/apps/agent/internal/pgdb"
	"xpanel/apps/agent/internal/php"
	"xpanel/apps/agent/internal/pma"
	"xpanel/apps/agent/internal/quota"
	"xpanel/apps/agent/internal/server"
	"xpanel/apps/agent/internal/siteapp"
	"xpanel/apps/agent/internal/stats"
	"xpanel/apps/agent/internal/sysinfo"
	"xpanel/apps/agent/internal/web"
	"xpanel/pkg/agentproto"
)

func main() {
	// Confined file-manager helper, spawned by the agent itself (see package files).
	if len(os.Args) > 1 && os.Args[1] == "fshelper" {
		files.HelperMain(os.Args[2:])
		return
	}
	// fail2ban action: ban / unban an IP in the X-Panel firewall.
	if len(os.Args) > 1 && os.Args[1] == "fwban" {
		firewall.CLIMain(os.Args[2:])
		return
	}
	// Confined malware scanner helper (see package malware).
	if len(os.Args) > 1 && os.Args[1] == "malscan" {
		malware.HelperMain(os.Args[2:])
		return
	}

	socket := flag.String("socket", agentproto.DefaultSocket, "unix socket path")
	group := flag.String("group", "xpanel", "group allowed to use the socket (empty = root only)")
	call := flag.String("call", "", "act as a client: run this action against a running agent and print the result")
	acmeDir := flag.String("acme-dir", envOr("XPANEL_ACME_DIR", acme.LetsEncrypt), "ACME directory URL")
	acmeInsecure := flag.Bool("acme-insecure", os.Getenv("XPANEL_ACME_INSECURE") == "true", "skip TLS verification of the ACME server (test CAs only)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *call != "" {
		var out json.RawMessage
		if err := agentproto.NewClient(*socket).Call(context.Background(), *call, nil, &out); err != nil {
			log.Error("call failed", "err", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
		return
	}

	d, err := distro.Detect()
	if err != nil {
		log.Error("detect distro", "err", err)
		os.Exit(1)
	}
	log.Info("detected", "os", d.Name)

	// nftables rules do not survive a reboot: restore the app-port isolation.
	if err := netguard.Rebuild(context.Background(), siteapp.ConfigRoot, d.NginxUser()); err != nil {
		log.Error("restore application port isolation", "err", err)
	}
	// Database tools from older versions ran on the panel's origin.
	if err := pma.MigrateTools(context.Background()); err != nil {
		log.Error("move database tools to their own port", "err", err)
	}
	if err := mail.MigrateWebmail(context.Background()); err != nil {
		log.Error("update webmail nginx config", "err", err)
	}
	if err := mail.MigrateDovecot(context.Background()); err != nil {
		log.Error("update dovecot config", "err", err)
	}
	if err := firewall.Restore(context.Background()); err != nil {
		log.Error("restore firewall", "err", err)
	}

	srv := server.New(log)
	srv.Register(agentproto.ActionSystemReboot, func(ctx context.Context, _ json.RawMessage) (any, error) { return nil, sysinfo.Reboot(ctx) })
	srv.Register(agentproto.ActionPing, func(context.Context, json.RawMessage) (any, error) {
		return map[string]string{"pong": "ok"}, nil
	})
	srv.Register(agentproto.ActionSystemInfo, func(context.Context, json.RawMessage) (any, error) {
		info := agentproto.SystemInfo{
			OSID: d.ID, OSName: d.Name, OSVersion: d.Version,
		}
		if err := sysinfo.Collect(&info); err != nil {
			return nil, err
		}
		info.QuotaMount, info.QuotaOn = quota.HomeMount()
		return info, nil
	})

	srv.Register(agentproto.ActionUserCreate, server.Typed(func(ctx context.Context, p agentproto.UserParams) (any, error) {
		uid, home, err := accounts.Create(ctx, p.Username)
		return agentproto.UserCreateResult{UID: uid, Home: home}, err
	}))
	srv.Register(agentproto.ActionFirewallApply, server.Typed(func(ctx context.Context, p agentproto.FirewallConfig) (any, error) {
		return firewall.Apply(ctx, p)
	}))
	srv.Register(agentproto.ActionFirewallBans, func(ctx context.Context, _ json.RawMessage) (any, error) {
		return firewall.Bans(ctx)
	})
	srv.Register(agentproto.ActionFirewallBan, server.Typed(func(ctx context.Context, p agentproto.FirewallBanParams) (any, error) {
		return nil, firewall.Ban(ctx, p)
	}))
	srv.Register(agentproto.ActionFirewallUnban, server.Typed(func(ctx context.Context, p agentproto.FirewallBanParams) (any, error) {
		return nil, firewall.Unban(ctx, p)
	}))
	srv.Register(agentproto.ActionNotifyEmail, server.Typed(func(ctx context.Context, p agentproto.NotifyEmailParams) (any, error) {
		return nil, mail.Notify(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareStatus, func(ctx context.Context, _ json.RawMessage) (any, error) {
		return malware.Status(ctx), nil
	})
	srv.Register(agentproto.ActionMalwareInstall, func(ctx context.Context, _ json.RawMessage) (any, error) {
		return malware.Install(ctx)
	})
	srv.Register(agentproto.ActionMalwareScan, server.Typed(func(ctx context.Context, p agentproto.MalwareScanParams) (any, error) {
		return malware.Scan(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareRead, server.Typed(func(ctx context.Context, p agentproto.MalwareFileParams) (any, error) {
		return malware.Read(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareQuarantine, server.Typed(func(ctx context.Context, p agentproto.MalwareQuarantineParams) (any, error) {
		return malware.Quarantine(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareRestore, server.Typed(func(ctx context.Context, p agentproto.MalwareQuarantineParams) (any, error) {
		return nil, malware.Restore(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareWrite, server.Typed(func(ctx context.Context, p agentproto.MalwareWriteParams) (any, error) {
		return nil, malware.Write(ctx, p)
	}))
	srv.Register(agentproto.ActionMalwareDrop, server.Typed(func(_ context.Context, p agentproto.MalwareQuarantineParams) (any, error) {
		return nil, malware.Drop(p)
	}))
	srv.Register(agentproto.ActionUserDelete, server.Typed(func(ctx context.Context, p agentproto.UserParams) (any, error) {
		if err := accounts.Delete(ctx, p.Username); err != nil {
			return nil, err
		}
		web.RemoveSlice(p.Username)
		_ = malware.RemoveAccount(p.Username)
		return nil, nil
	}))
	srv.Register(agentproto.ActionUserLock, server.Typed(func(ctx context.Context, p agentproto.UserLockParams) (any, error) {
		if err := accounts.Lock(ctx, p.Username, p.Locked); err != nil {
			return nil, err
		}
		// Cron jobs are systemd timers (no PAM expiry check): switch them explicitly.
		return nil, cron.SetActive(ctx, p.Username, !p.Locked)
	}))
	srv.Register(agentproto.ActionQuotaSet, server.Typed(func(ctx context.Context, p agentproto.QuotaSetParams) (any, error) {
		if p.DiskMB < 0 || p.Inodes < 0 {
			return nil, fmt.Errorf("quota limits must be >= 0")
		}
		applied, err := quota.Set(ctx, p)
		return agentproto.QuotaSetResult{Applied: applied}, err
	}))
	srv.Register(agentproto.ActionQuotaReport, func(ctx context.Context, _ json.RawMessage) (any, error) {
		return quota.Report(ctx)
	})

	syncer := &web.Syncer{D: d}
	srv.Register(agentproto.ActionWebSync, server.Typed(func(ctx context.Context, p agentproto.WebSyncParams) (any, error) {
		return syncer.Sync(ctx, p)
	}))
	srv.Register(agentproto.ActionPHPManageLogs, server.Typed(func(ctx context.Context, p agentproto.PHPManageParams) (any, error) { return php.Logs(ctx, p) }))
	srv.Register(agentproto.ActionPHPManageList, server.Typed(func(ctx context.Context, p agentproto.PHPManageParams) (any, error) { return php.Management(ctx, p) }))
	srv.Register(agentproto.ActionPHPManageApply, server.Typed(func(ctx context.Context, p agentproto.PHPManageParams) (any, error) {
		out, err := php.ApplyManagement(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("%w\n%s", err, out)
		}
		return agentproto.TaskLog{Log: out}, nil
	}))
	srv.Register(agentproto.ActionPHPList, func(context.Context, json.RawMessage) (any, error) {
		return agentproto.PHPList{Supported: agentproto.PHPVersions, Installed: php.Installed(d)}, nil
	})
	srv.Register(agentproto.ActionPHPInstall, server.Typed(func(ctx context.Context, p agentproto.PHPInstallParams) (any, error) {
		out, err := php.Install(ctx, d, p.Version)
		if err != nil {
			return nil, fmt.Errorf("%w\n--- log ---\n%s", err, out)
		}
		return agentproto.TaskLog{Log: out}, nil
	}))
	srv.Register(agentproto.ActionDBCreate, server.Typed(func(ctx context.Context, p agentproto.DBParams) (any, error) {
		if p.Engine == "postgresql" {
			return nil, pgdb.Create(ctx, p)
		}
		return nil, mysqldb.Create(ctx, p)
	}))
	srv.Register(agentproto.ActionDBDelete, server.Typed(func(ctx context.Context, p agentproto.DBParams) (any, error) {
		if p.Engine == "postgresql" {
			return nil, pgdb.Delete(ctx, p)
		}
		return nil, mysqldb.Delete(ctx, p)
	}))
	srv.Register(agentproto.ActionDBPassword, server.Typed(func(ctx context.Context, p agentproto.DBParams) (any, error) {
		if p.Engine == "postgresql" {
			return nil, pgdb.SetPassword(ctx, p)
		}
		return nil, mysqldb.SetPassword(ctx, p)
	}))
	srv.Register(agentproto.ActionDBLock, server.Typed(func(ctx context.Context, p agentproto.DBLockParams) (any, error) {
		if p.Engine == "postgresql" {
			return nil, pgdb.Lock(ctx, p)
		}
		return nil, mysqldb.Lock(ctx, p)
	}))
	srv.Register(agentproto.ActionDBSizes, func(ctx context.Context, _ json.RawMessage) (any, error) {
		// Both engines; database names are unique across them.
		res, err := mysqldb.Sizes(ctx)
		if err != nil {
			return nil, err
		}
		if pg, err := pgdb.Sizes(ctx); err == nil {
			for k, v := range pg {
				res.Sizes[k] = v
			}
		}
		return res, nil
	})
	srv.Register(agentproto.ActionUserPassword, server.Typed(func(ctx context.Context, p agentproto.UserPasswordParams) (any, error) {
		return nil, accounts.SetPassword(ctx, p.Username, p.Password)
	}))
	srv.Register(agentproto.ActionFTPCreate, server.Typed(func(ctx context.Context, p agentproto.FTPParams) (any, error) {
		return nil, ftp.Create(ctx, p)
	}))
	srv.Register(agentproto.ActionFTPPassword, server.Typed(func(ctx context.Context, p agentproto.FTPParams) (any, error) {
		return nil, ftp.SetPassword(ctx, p)
	}))
	srv.Register(agentproto.ActionFTPDelete, server.Typed(func(ctx context.Context, p agentproto.FTPParams) (any, error) {
		return nil, ftp.Delete(ctx, p)
	}))
	srv.Register(agentproto.ActionFTPLock, server.Typed(func(ctx context.Context, p agentproto.FTPLockParams) (any, error) {
		return nil, ftp.Lock(ctx, p)
	}))
	srv.Register(agentproto.ActionFSOp, server.Typed(func(ctx context.Context, p agentproto.FSOpParams) (any, error) {
		return files.Op(ctx, p)
	}))
	srv.HandleRaw("POST "+agentproto.PathFSUpload, files.HandleUpload)
	srv.HandleRaw("GET "+agentproto.PathFSDownload, files.HandleDownload)
	srv.Register(agentproto.ActionPMAStatus, func(context.Context, json.RawMessage) (any, error) {
		return pma.Status(d), nil
	})
	srv.Register(agentproto.ActionPMAInstall, func(ctx context.Context, _ json.RawMessage) (any, error) {
		out, err := pma.Install(ctx, d)
		if err != nil {
			return nil, fmt.Errorf("%w\n--- log ---\n%s", err, out)
		}
		return agentproto.TaskLog{Log: out}, nil
	})
	srv.Register(agentproto.ActionCronSync, server.Typed(func(ctx context.Context, p agentproto.CronSyncParams) (any, error) {
		return nil, cron.Sync(ctx, p)
	}))
	srv.Register(agentproto.ActionSiteDetect, server.Typed(siteapp.Detect))
	srv.Register(agentproto.ActionSiteLogs, server.Typed(siteapp.Logs))
	srv.Register(agentproto.ActionSiteBuild, server.Typed(func(ctx context.Context, p agentproto.SiteAppParams) (any, error) { return siteapp.Build(ctx, d, p) }))
	catalog := apps.New(d)
	srv.Register(agentproto.ActionAppsList, server.Typed(func(ctx context.Context, p agentproto.AppListParams) (any, error) {
		if p.Refresh {
			catalog.Refresh()
		}
		return agentproto.AppList{Apps: catalog.List(ctx)}, nil
	}))
	srv.Register(agentproto.ActionAppsInstall, server.Typed(func(ctx context.Context, p agentproto.AppInstallParams) (any, error) {
		out, err := catalog.Install(ctx, p.ID, p.Version)
		if err != nil {
			return nil, fmt.Errorf("%w\n--- log ---\n%s", err, out)
		}
		return agentproto.TaskLog{Log: out}, nil
	}))
	srv.Register(agentproto.ActionAppsRemove, server.Typed(func(ctx context.Context, p agentproto.AppInstallParams) (any, error) {
		out, err := catalog.Remove(ctx, p.ID, p.Version)
		if err != nil {
			return nil, fmt.Errorf("%w\n--- log ---\n%s", err, out)
		}
		return agentproto.TaskLog{Log: out}, nil
	}))
	issuer := &acme.Issuer{DirectoryURL: *acmeDir, Insecure: *acmeInsecure}
	srv.Register(agentproto.ActionSSLIssue, server.Typed(func(_ context.Context, p agentproto.SSLIssueParams) (any, error) {
		return issuer.Issue(p)
	}))
	srv.Register(agentproto.ActionSSLInstall, server.Typed(func(ctx context.Context, p agentproto.SSLInstallParams) (any, error) {
		if err := web.InstallCertificate(p.Domain, p.Certificate, p.PrivateKey); err != nil {
			return nil, err
		}
		// A replaced certificate leaves the vhost unchanged, so the web sync
		// would not reload nginx; reload here so the new one is served.
		return nil, web.ReloadNginx(ctx)
	}))
	srv.Register(agentproto.ActionPanelSSLInfo, func(context.Context, json.RawMessage) (any, error) {
		return acme.PanelInfo()
	})
	srv.Register(agentproto.ActionPanelSSLIssue, server.Typed(func(ctx context.Context, p agentproto.PanelSSLParams) (any, error) {
		return issuer.IssuePanel(ctx, p)
	}))
	srv.Register(agentproto.ActionBackupCreate, server.Typed(func(ctx context.Context, p agentproto.BackupCreateParams) (any, error) {
		return backup.Create(ctx, p)
	}))
	srv.Register(agentproto.ActionBackupManifest, server.Typed(func(ctx context.Context, p agentproto.BackupManifestParams) (any, error) {
		return backup.ReadManifest(ctx, p)
	}))
	srv.Register(agentproto.ActionBackupRestore, server.Typed(func(ctx context.Context, p agentproto.BackupRestoreParams) (any, error) {
		return backup.Restore(ctx, p)
	}))
	srv.Register(agentproto.ActionStatsCollect, server.Typed(func(ctx context.Context, p agentproto.StatsParams) (any, error) {
		return stats.Collect(ctx, p)
	}))
	srv.Register(agentproto.ActionMailSync, server.Typed(func(ctx context.Context, p agentproto.MailSyncParams) (any, error) {
		return mail.Sync(ctx, p)
	}))
	srv.Register(agentproto.ActionMailUsage, server.Typed(func(_ context.Context, p agentproto.MailUsageParams) (any, error) {
		return mail.Usage(p)
	}))
	srv.Register(agentproto.ActionMailDeleteBox, server.Typed(func(_ context.Context, p agentproto.MailDeleteParams) (any, error) {
		return nil, mail.DeleteStored(p)
	}))
	srv.Register(agentproto.ActionMailStatus, func(ctx context.Context, _ json.RawMessage) (any, error) {
		return mail.Status(ctx), nil
	})

	if *acmeInsecure {
		log.Warn("ACME TLS verification disabled", "dir", *acmeDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Listen(ctx, *socket, *group); err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
