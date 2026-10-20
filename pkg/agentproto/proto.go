// Package agentproto defines the contract between xpanel-api and xpanel-agent.
// The agent only executes actions listed here; it never runs raw shell strings.
package agentproto

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"xpanel/pkg/phpconfig"

	"xpanel/pkg/gdrive"
)

const DefaultSocket = "/run/xpanel/agent.sock"

const (
	ActionPing         = "ping"
	ActionSystemInfo   = "system.info"
	ActionSystemReboot = "system.reboot"
	ActionUserCreate   = "user.create"   // UserParams -> UserCreateResult
	ActionUserDelete   = "user.delete"   // UserParams
	ActionUserLock     = "user.lock"     // UserLockParams
	ActionQuotaSet     = "quota.set"     // QuotaSetParams -> QuotaSetResult
	ActionQuotaReport  = "quota.report"  // -> QuotaReport
	ActionWebSync      = "web.sync"      // WebSyncParams -> WebSyncResult
	ActionPHPList      = "php.list"      // -> PHPList
	ActionPHPInstall   = "php.install"   // PHPInstallParams -> TaskLog
	ActionSSLIssue     = "ssl.issue"     // SSLIssueParams -> SSLIssueResult
	ActionDBCreate     = "db.create"     // DBParams (with Password)
	ActionDBDelete     = "db.delete"     // DBParams
	ActionDBPassword   = "db.password"   // DBParams (with Password)
	ActionDBLock       = "db.lock"       // DBLockParams
	ActionDBSizes      = "db.sizes"      // -> DBSizes
	ActionUserPassword = "user.password" // UserPasswordParams (SFTP login)
	ActionFTPCreate    = "ftp.create"    // FTPParams (with Password)
	ActionFTPPassword  = "ftp.password"  // FTPParams (with Password)
	ActionFTPDelete    = "ftp.delete"    // FTPParams
	ActionFTPLock      = "ftp.lock"      // FTPLockParams
	ActionFSOp         = "fs.op"         // FSOpParams -> FSResult
	ActionPMAInstall   = "pma.install"   // -> TaskLog
	ActionPMAStatus    = "pma.status"    // -> PMAStatus
)

// Streaming file transfer endpoints (query: owner, path); not part of /v1/call.
const (
	PathFSUpload   = "/v1/fs/upload"
	PathFSDownload = "/v1/fs/download"
)

type UserPasswordParams struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// FTPParams describes a Pure-FTPd virtual user "<Owner>_<name>" confined to
// Dir (relative to the owner's home, "" = whole home).
type FTPParams struct {
	Owner    string `json:"owner"`
	Login    string `json:"login"`
	Dir      string `json:"dir"`
	Password string `json:"password,omitempty"`
}

type FTPLockParams struct {
	Owner  string   `json:"owner"`
	Logins []string `json:"logins"`
	Locked bool     `json:"locked"`
}

// FSRequest is one file-manager operation, executed as the owner inside a
// chroot of their home directory. Paths are absolute within that home.
type FSRequest struct {
	Op      string   `json:"op"` // list read write mkdir rename move copy delete chmod extract compress upload download
	Path    string   `json:"path,omitempty"`
	To      string   `json:"to,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Content string   `json:"content,omitempty"`
	Mode    string   `json:"mode,omitempty"` // octal, e.g. "644"
}

type FSOpParams struct {
	Owner   string    `json:"owner"`
	Request FSRequest `json:"request"`
}

type FSEntry struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"` // dir | file | link
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mtime"`
	Target  string    `json:"target,omitempty"`
}

type FSResult struct {
	Entries []FSEntry `json:"entries,omitempty"`
	Content string    `json:"content,omitempty"`
}

type PMAStatus struct {
	Installed  bool   `json:"installed"`
	PHPVersion string `json:"php_version"`
}

// DBParams identifies a customer database and its MariaDB user. Both names
// must start with "<Owner>_"; the agent enforces it.
type DBParams struct {
	Engine   string `json:"engine,omitempty"` // "mariadb" (default) | "postgresql"
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
}

type DBLockParams struct {
	Engine string   `json:"engine,omitempty"`
	Owner  string   `json:"owner"`
	Users  []string `json:"users"`
	Locked bool     `json:"locked"`
}

// DBSizes maps database name → size in bytes.
type DBSizes struct {
	Sizes map[string]int64 `json:"sizes"`
}

// PHPVersions the agent knows how to install and configure.
var PHPVersions = []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

// ValidPHPVersion accepts a major.minor line, never paths or command fragments.
func ValidPHPVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 2 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return v == "7.4" || (len(parts[0]) == 1 && parts[0] >= "8" && (parts[1] == "0" || parts[1][0] != '0'))
}

// WebSyncParams is the complete desired web state of one Linux user. The
// agent rewrites that user's nginx vhosts and PHP-FPM pools to match it and
// removes anything else it previously generated for the user.
type WebSyncParams struct {
	Username  string `json:"username"`
	Suspended bool   `json:"suspended"`
	// The month's bandwidth allowance is used up: sites answer 503 until
	// the next month or a bigger package (cron and apps keep running).
	OverBandwidth bool      `json:"over_bandwidth,omitempty"`
	Sites         []WebSite `json:"sites"`
	// Package limits for the account's PHP-FPM slice; 0 = unlimited.
	CPUPct int `json:"cpu_pct"`
	MemMB  int `json:"mem_mb"`
}

type WebSite struct {
	PHPConfig  phpconfig.Site    `json:"php_config"`
	AppType    string            `json:"app_type"`
	Runtime    string            `json:"runtime"`
	Port       int               `json:"port"`
	Env        map[string]string `json:"env,omitempty"`
	Domain     string            `json:"domain"`
	Aliases    []string          `json:"aliases"`
	PHPVersion string            `json:"php_version"` // "" = static site
	DocRoot    string            `json:"doc_root"`    // relative to /home/<user>/www/<domain>
	SSL        bool              `json:"ssl"`
}

const (
	ActionSiteDetect = "site.detect"
	ActionSiteBuild  = "site.build"
	ActionSiteLogs   = "site.logs"
)

type SiteAppParams struct {
	Username string  `json:"username"`
	Site     WebSite `json:"site"`
	Sample   bool    `json:"sample"`
}
type SiteDetectResult struct {
	AppType string `json:"app_type"`
}

type WebSyncResult struct {
	Warnings []string `json:"warnings"`
}

type PHPList struct {
	Supported []string `json:"supported"`
	Installed []string `json:"installed"`
}

type PHPInstallParams struct {
	Version string `json:"version"`
}

// TaskLog carries the output of a long-running action.
type TaskLog struct {
	Log string `json:"log"`
}

type SSLIssueParams struct {
	Domain string   `json:"domain"`
	Names  []string `json:"names"` // all names on the certificate, Domain first
	Email  string   `json:"email"`
}

type SSLIssueResult struct {
	ExpiresAt time.Time `json:"expires_at"`
}

// Caller is implemented by Client and by DryRun (development without an agent).
type Caller interface {
	Call(ctx context.Context, action string, params, out any) error
}

type UserParams struct {
	Username string `json:"username"`
}

type UserCreateResult struct {
	UID  int    `json:"uid"`
	Home string `json:"home"`
}

type UserLockParams struct {
	Username string `json:"username"`
	Locked   bool   `json:"locked"`
}

// QuotaSetParams limits are in MB / inode count; 0 means unlimited.
type QuotaSetParams struct {
	Username string `json:"username"`
	DiskMB   int    `json:"disk_mb"`
	Inodes   int    `json:"inodes"`
}

// QuotaSetResult.Applied is false when quotas are not enabled on /home's filesystem.
type QuotaSetResult struct {
	Applied bool `json:"applied"`
}

type QuotaUsage struct {
	DiskUsedMB int `json:"disk_used_mb"`
	InodesUsed int `json:"inodes_used"`
}

type QuotaReport struct {
	Enabled bool                  `json:"enabled"`
	Mount   string                `json:"mount"`
	Users   map[string]QuotaUsage `json:"users"`
}

type Request struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

type SystemInfo struct {
	IPAddresses     []string   `json:"ip_addresses"`
	CPUModel        string     `json:"cpu_model"`
	CPUPercent      float64    `json:"cpu_percent"`
	Architecture    string     `json:"architecture"`
	BootID          string     `json:"boot_id"`
	SwapTotalMB     uint64     `json:"swap_total_mb"`
	SwapUsedMB      uint64     `json:"swap_used_mb"`
	DiskAvailableMB uint64     `json:"disk_available_mb"`
	NetworkRXBytes  uint64     `json:"network_rx_bytes"`
	NetworkTXBytes  uint64     `json:"network_tx_bytes"`
	Hostname        string     `json:"hostname"`
	OSID            string     `json:"os_id"`
	OSName          string     `json:"os_name"`
	OSVersion       string     `json:"os_version"`
	Kernel          string     `json:"kernel"`
	CPUCount        int        `json:"cpu_count"`
	UptimeSec       uint64     `json:"uptime_sec"`
	Load            [3]float64 `json:"load"`
	MemTotalMB      uint64     `json:"mem_total_mb"`
	MemUsedMB       uint64     `json:"mem_used_mb"`
	DiskTotalMB     uint64     `json:"disk_total_mb"`
	DiskUsedMB      uint64     `json:"disk_used_mb"`
	QuotaOn         bool       `json:"quota_enabled"`
	QuotaMount      string     `json:"quota_mount"`
}

const ActionCronSync = "cron.sync" // CronSyncParams

// CronSyncParams is the complete crontab of a Linux user (empty = remove it).
type CronSyncParams struct {
	Username string    `json:"username"`
	Jobs     []CronJob `json:"jobs"`
}

type CronJob struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

const (
	ActionAppsList    = "apps.list" // -> AppList
	ActionAppsRemove  = "apps.remove"
	ActionAppsInstall = "apps.install" // AppInstallParams -> TaskLog
)

// AppInfo describes one entry of the server application catalog.
type AppInfo struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Category        string            `json:"category"` // runtime | database | tool
	Description     string            `json:"description"`
	MultiVersion    bool              `json:"multi_version"` // several versions side by side
	Builtin         bool              `json:"builtin"`       // installed with the panel, not installable
	Installed       []string          `json:"installed"`
	Available       []string          `json:"available"` // newest first
	Error           string            `json:"error,omitempty"`
	CheckedAt       string            `json:"checked_at,omitempty"`
	Source          string            `json:"source,omitempty"`
	VersionLabels   map[string]string `json:"version_labels,omitempty"`
	InstalledLabels map[string]string `json:"installed_labels,omitempty"`
}

type AppListParams struct {
	Refresh bool `json:"refresh"`
}

type AppList struct {
	Apps []AppInfo `json:"apps"`
}

type AppInstallParams struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

const (
	ActionSSLInstall    = "ssl.install"     // SSLInstallParams
	ActionPanelSSLInfo  = "panel.ssl_info"  // -> PanelSSLInfo
	ActionPanelSSLIssue = "panel.ssl_issue" // PanelSSLParams -> SSLIssueResult
)

// SSLInstallParams installs an already validated certificate for a site.
type SSLInstallParams struct {
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"` // PEM, leaf first
	PrivateKey  string `json:"private_key"` // PEM
}

// PanelSSLInfo describes the certificate served on the panel port (and FTPS).
type PanelSSLInfo struct {
	Subject    string    `json:"subject"`
	Issuer     string    `json:"issuer"`
	Names      []string  `json:"names"`
	NotAfter   time.Time `json:"not_after"`
	SelfSigned bool      `json:"self_signed"`
}

type PanelSSLParams struct {
	Domain string `json:"domain"`
	Email  string `json:"email"`
}

const (
	ActionMailSync      = "mail.sync"       // MailSyncParams -> MailSyncResult
	ActionMailDeleteBox = "mail.delete_box" // MailDeleteParams
	ActionMailStatus    = "mail.status"     // -> MailStatus
	ActionMailUsage     = "mail.usage"      // MailUsageParams -> MailUsage
)

// MailSyncParams is the complete mail configuration of the server; the agent
// rewrites Postfix maps, the Dovecot user list and the DKIM tables from it.
type MailSyncParams struct {
	Hostname  string        `json:"hostname"`
	Domains   []MailDomain  `json:"domains"`
	Mailboxes []MailBox     `json:"mailboxes"`
	Aliases   []MailForward `json:"aliases"`
}

type MailDomain struct {
	Domain string `json:"domain"`
}

type MailBox struct {
	Address      string `json:"address"`       // local@domain
	PasswordHash string `json:"password_hash"` // bcrypt ($2a$…)
	QuotaMB      int    `json:"quota_mb"`      // 0 = unlimited
	Suspended    bool   `json:"suspended"`
}

type MailForward struct {
	Source       string   `json:"source"` // local@domain
	Destinations []string `json:"destinations"`
}

type MailSyncResult struct {
	// DKIM public records keyed by domain: name and TXT value to publish.
	DKIM     map[string]DNSRecord `json:"dkim"`
	ServerIP string               `json:"server_ip"`
}

type DNSRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MailDeleteParams removes stored mail: a mailbox ("local@domain") or a whole domain.
type MailDeleteParams struct {
	Address string `json:"address,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

type MailStatus struct {
	Installed bool   `json:"installed"`
	Webmail   bool   `json:"webmail"`
	Hostname  string `json:"hostname"`
}

const ActionStatsCollect = "stats.collect" // StatsParams -> StatsResult

// StatsParams names the hosting accounts to measure and what they own.
type StatsParams struct {
	Accounts []StatsAccount `json:"accounts"`
}

type StatsAccount struct {
	Username    string   `json:"username"`
	Domains     []string `json:"domains"`      // websites (access logs)
	MailDomains []string `json:"mail_domains"` // stored mail
}

type StatsResult struct {
	At       time.Time                    `json:"at"`
	Accounts map[string]AccountStatSample `json:"accounts"`
}

type AccountStatSample struct {
	CPUUsageUsec uint64 `json:"cpu_usage_usec"` // cumulative, from the account's cgroup
	MemoryBytes  uint64 `json:"memory_bytes"`
	Tasks        int    `json:"tasks"`
	MailBytes    int64  `json:"mail_bytes"`
	// Bytes sent to visitors since the previous collection.
	BandwidthBytes int64 `json:"bandwidth_bytes"`
	Requests       int64 `json:"requests"`
}

const (
	ActionBackupCreate   = "backup.create"   // BackupCreateParams -> BackupCreateResult
	ActionBackupRestore  = "backup.restore"  // BackupRestoreParams -> TaskLog
	ActionBackupManifest = "backup.manifest" // BackupManifestParams -> BackupManifest
)

// BackupDB is a customer database inside a backup.
type BackupDB struct {
	Name   string `json:"name"`
	Engine string `json:"engine"` // mariadb | postgresql
	User   string `json:"user"`   // owning database user
}

// BackupCreateParams: archive one account (home, databases, mail), encrypt
// it with Passphrase and stream it to Google Drive.
type BackupCreateParams struct {
	Username    string          `json:"username"`
	Databases   []BackupDB      `json:"databases"`
	MailDomains []string        `json:"mail_domains"`
	Drive       gdrive.Creds    `json:"drive"`
	FolderID    string          `json:"folder_id"`
	FileName    string          `json:"file_name"`
	Passphrase  string          `json:"passphrase"`
	Manifest    json.RawMessage `json:"manifest"` // panel records of the account, stored as manifest.json
}

type BackupCreateResult struct {
	FileID string `json:"file_id"`
	Size   int64  `json:"size"`
	Log    string `json:"log"`
}

// BackupRestoreParams: restore the selected parts of a backup into the same
// account. Only the listed databases and mail domains (owned by the account
// now) are written.
type BackupRestoreParams struct {
	Username    string       `json:"username"`
	Drive       gdrive.Creds `json:"drive"`
	FileID      string       `json:"file_id"`
	Passphrase  string       `json:"passphrase"`
	Files       bool         `json:"files"`
	Databases   []BackupDB   `json:"databases"`
	MailDomains []string     `json:"mail_domains"`
}

// MailUsageParams asks for the stored size of each mailbox of a domain.
type MailUsageParams struct {
	Domain string `json:"domain"`
}

// MailUsage maps a mailbox's local part to the bytes it stores.
type MailUsage struct {
	Bytes map[string]int64 `json:"bytes"`
}

// BackupManifestParams reads the manifest (panel records) of a backup.
type BackupManifestParams struct {
	Username   string       `json:"username"`
	Drive      gdrive.Creds `json:"drive"`
	FileID     string       `json:"file_id"`
	Passphrase string       `json:"passphrase"`
}

type BackupManifest struct {
	Manifest json.RawMessage `json:"manifest"`
}

// Malware scanning (see apps/agent/internal/malware).
const (
	ActionMalwareStatus     = "malware.status"     // -> MalwareStatus
	ActionMalwareInstall    = "malware.install"    // -> TaskLog (installs ClamAV)
	ActionMalwareScan       = "malware.scan"       // MalwareScanParams -> MalwareScanResult
	ActionMalwareRead       = "malware.read"       // MalwareFileParams -> MalwareFile
	ActionMalwareQuarantine = "malware.quarantine" // MalwareQuarantineParams -> MalwareFile (without content)
	ActionMalwareRestore    = "malware.restore"    // MalwareQuarantineParams
	ActionMalwareWrite      = "malware.write"      // MalwareWriteParams
	ActionMalwareDrop       = "malware.drop"       // MalwareQuarantineParams: delete a quarantined copy
)

type MalwareStatus struct {
	ClamAV    bool   `json:"clamav"`     // clamscan or clamdscan installed
	Clamd     bool   `json:"clamd"`      // the daemon answers (fast scans)
	DBVersion string `json:"db_version"` // signature database
}

// MalwareScanParams scans an account's home; Since (unix seconds) limits
// the scan to files changed after it.
type MalwareScanParams struct {
	Username string `json:"username"`
	Since    int64  `json:"since,omitempty"`
}

type MalwareFinding struct {
	Path     string    `json:"path"` // relative to the home, starting with "/"
	Engine   string    `json:"engine"`
	Rule     string    `json:"rule"`
	Severity string    `json:"severity"` // high | medium
	Snippet  string    `json:"snippet"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	SHA256   string    `json:"sha256"`
}

type MalwareScanResult struct {
	Files     int              `json:"files"`
	Engines   []string         `json:"engines"`
	Findings  []MalwareFinding `json:"findings"`
	Truncated bool             `json:"truncated"`
	Log       string           `json:"log"`
}

type MalwareFileParams struct {
	Username string `json:"username"`
	Path     string `json:"path"`
}

type MalwareFile struct {
	Content []byte `json:"content,omitempty"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// MalwareQuarantineParams: ID names the quarantined copy (hex).
type MalwareQuarantineParams struct {
	Username string `json:"username"`
	Path     string `json:"path"`
	ID       string `json:"id"`
	SHA256   string `json:"sha256,omitempty"` // quarantine only this content
}

// MalwareWriteParams replaces a file with cleaned content after keeping the
// original in quarantine as BackupID; ExpectSHA256 must match the file.
type MalwareWriteParams struct {
	Username     string `json:"username"`
	Path         string `json:"path"`
	Content      []byte `json:"content"`
	ExpectSHA256 string `json:"expect_sha256"`
	BackupID     string `json:"backup_id"`
}

// ActionNotifyEmail sends a plain-text alert through the local MTA.
const ActionNotifyEmail = "notify.email" // NotifyEmailParams

type NotifyEmailParams struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Firewall (see apps/agent/internal/firewall).
const (
	ActionFirewallApply = "firewall.apply" // FirewallConfig
	ActionFirewallBans  = "firewall.bans"  // -> FirewallBans
	ActionFirewallBan   = "firewall.ban"   // FirewallBanParams
	ActionFirewallUnban = "firewall.unban" // FirewallBanParams
)

// FirewallConfig is the whole server firewall; 0 turns a limit off.
type FirewallConfig struct {
	Enabled bool `json:"enabled"`
	// Network level (nftables), per source IP.
	SynRate   int `json:"syn_rate"`   // new TCP connections per second
	ConnLimit int `json:"conn_limit"` // concurrent connections to 80/443
	// HTTP level (nginx), per source IP.
	HTTPRate  int `json:"http_rate"`  // requests per second
	HTTPBurst int `json:"http_burst"` // extra requests allowed in a burst
	HTTPConn  int `json:"http_conn"`  // concurrent requests
	// fail2ban (SSH, FTP, mail, HTTP flood).
	MaxRetry  int  `json:"max_retry"`
	FindMin   int  `json:"find_min"`
	BanMin    int  `json:"ban_min"`
	Increment bool `json:"increment"` // repeat offenders: longer bans (up to a week)
	FloodBan  bool `json:"flood_ban"` // ban IPs that keep hitting the HTTP limits
	// Panel login (enforced by the API).
	PanelIPFails   int      `json:"panel_ip_fails"`   // failures per IP in 15 minutes -> firewall ban
	PanelBanMin    int      `json:"panel_ban_min"`    //
	AccountFails   int      `json:"account_fails"`    // failures per account in 15 minutes -> account locked
	AccountLockMin int      `json:"account_lock_min"` //
	Allow          []string `json:"allow"`            // IPs/CIDRs never limited or banned
	Block          []string `json:"block"`            // IPs/CIDRs always dropped
}

type FirewallBan struct {
	IP        string     `json:"ip"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"` // nil = until removed
}

type FirewallBans struct {
	Active bool          `json:"active"` // the nftables table is loaded
	Bans   []FirewallBan `json:"bans"`
}

type FirewallBanParams struct {
	IP      string `json:"ip"`
	Seconds int    `json:"seconds"` // 0 = until removed
	Reason  string `json:"reason"`
}

// DefaultFirewall is the firewall of a new install.
func DefaultFirewall() FirewallConfig {
	return FirewallConfig{Enabled: true, SynRate: 100, ConnLimit: 300, HTTPRate: 30, HTTPBurst: 100, HTTPConn: 100,
		MaxRetry: 6, FindMin: 10, BanMin: 60, Increment: true, FloodBan: true,
		PanelIPFails: 10, PanelBanMin: 60, AccountFails: 10, AccountLockMin: 15, Allow: []string{}, Block: []string{}}
}
