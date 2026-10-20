package agentproto

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"
)

// DryRun pretends to be the agent: it logs each action and returns plausible
// results. Only for developing the API/UI on machines without the agent
// (e.g. Windows); enabled with XPANEL_AGENT_DRY_RUN=true.
type DryRun struct {
	Log     *slog.Logger
	nextUID atomic.Int64
}

func (d *DryRun) Call(_ context.Context, action string, params, out any) error {
	p, _ := json.Marshal(params)
	d.Log.Warn("agent dry-run", "action", action, "params", string(p))

	var res any
	switch action {
	case ActionUserCreate:
		var in UserParams
		_ = json.Unmarshal(p, &in)
		res = UserCreateResult{UID: 20000 + int(d.nextUID.Add(1)), Home: "/home/" + in.Username}
	case ActionQuotaSet:
		res = QuotaSetResult{Applied: false}
	case ActionQuotaReport:
		res = QuotaReport{Users: map[string]QuotaUsage{}}
	case ActionPHPList:
		res = PHPList{Supported: PHPVersions, Installed: []string{"8.2", "8.3"}}
	case ActionSiteDetect:
		res = SiteDetectResult{AppType: "html"}
	case ActionAppsList:
		res = AppList{Apps: []AppInfo{
			{ID: "php", Name: "PHP", Category: "runtime", MultiVersion: true, Installed: []string{"8.2", "8.3"}, Available: PHPVersions},
			{ID: "nodejs", Name: "Node.js", Category: "runtime", MultiVersion: true, Installed: []string{}, Available: []string{"24", "22"}},
			{ID: "go", Name: "Go", Category: "runtime", Installed: []string{}, Available: []string{"1.26.0"}},
			{ID: "postgresql", Name: "PostgreSQL", Category: "database", Installed: []string{}, Available: []string{"18"}},
			{ID: "mariadb", Name: "MariaDB", Category: "database", Builtin: true, Installed: []string{"installed"}, Available: []string{}},
			{ID: "adminer", Name: "Adminer", Category: "tool", Installed: []string{}, Available: []string{"latest"}},
			{ID: "composer", Name: "Composer", Category: "runtime", Installed: []string{}, Available: []string{"latest"}},
			{ID: "phpmyadmin", Name: "phpMyAdmin", Category: "tool", Installed: []string{}, Available: []string{"latest"}},
			{ID: "mailserver", Name: "Mail server", Category: "mail", Installed: []string{"3.8"}, Available: []string{"3.8"}},
			{ID: "roundcube", Name: "Roundcube Webmail", Category: "mail", Installed: []string{}, Available: []string{"1.6.11"}},
		}}
	case ActionSiteBuild, ActionSiteLogs, ActionAppsInstall, ActionPHPInstall:
		res = TaskLog{Log: "dry-run: nothing installed"}
	case ActionSSLIssue:
		res = SSLIssueResult{ExpiresAt: time.Now().Add(90 * 24 * time.Hour)}
	case ActionDBSizes:
		res = DBSizes{Sizes: map[string]int64{}}
	case ActionWebSync:
		res = WebSyncResult{}
	case ActionMailSync:
		res = MailSyncResult{DKIM: map[string]DNSRecord{}, ServerIP: "127.0.0.1"}
	case ActionMailUsage:
		res = MailUsage{Bytes: map[string]int64{}}
	case ActionMailStatus:
		res = MailStatus{Installed: true, Hostname: "mail.dry-run"}
	case ActionPanelSSLInfo:
		res = PanelSSLInfo{Subject: "dry-run", Issuer: "dry-run", NotAfter: time.Now().Add(365 * 24 * time.Hour), SelfSigned: true}
	case ActionStatsCollect:
		res = StatsResult{At: time.Now(), Accounts: map[string]AccountStatSample{}}
	case ActionPHPManageList:
		res = PHPManagement{Modules: []PHPModule{}, Settings: []PHPSetting{}}
	case ActionSystemInfo:
		res = SystemInfo{Hostname: "dry-run", OSName: "Dry-run (no agent)", CPUCount: 1}
	default:
		return nil
	}
	if out == nil {
		return nil
	}
	raw, _ := json.Marshal(res)
	return json.Unmarshal(raw, out)
}
