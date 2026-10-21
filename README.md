# X-Panel

**A lightweight hosting control panel for Debian and Ubuntu.** Manage websites, applications, databases, email and hosting accounts from one interface. Install directly on your VPS; Docker is not required for production.

**English** · [Tiếng Việt](README.vi.md)

## Requirements

- A fresh **Ubuntu 22.04/24.04** or **Debian 12/13** VPS with root access.
- **amd64 or arm64**, at least **1 GB RAM**, working Internet access and DNS.
- Allow TCP **22, 80, 443 and 8888** through your provider's firewall.

Ubuntu 24.04 is the recommended starting point. The installer checks the operating system, memory and disk space and rejects conflicting panels or web servers.

## Installation

Connect to the VPS as root and run:

```bash
apt-get -o Acquire::Retries=3 -o APT::Update::Error-Mode=any update && apt-get -o Acquire::Retries=3 install -y curl ca-certificates && curl -fSL --retry 3 -o /tmp/xpanel-install.sh https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh && bash /tmp/xpanel-install.sh
```

Installation usually takes 3–10 minutes, depending on your server and package mirrors. The bootstrap downloads the latest release for your architecture and verifies its SHA-256 checksum. At completion it displays:

```text
Panel URL : https://YOUR-SERVER-IP:8888
Username  : admin
Password  : <generated password>
```

Save the password when it appears. The initial certificate is self-signed, so your browser will show a certificate warning.

To choose an admin password, download the installer as above, then run:

```bash
ADMIN_PASSWORD='REPLACE-WITH-A-STRONG-UNIQUE-PASSWORD' bash /tmp/xpanel-install.sh
```

Setting `ADMIN_PASSWORD` on an existing installation resets the admin password. Install PHP and other runtimes afterward from **Applications**; PHP is optional on a fresh server.

### Updating

Run the installation command again to update while retaining configuration and data. Back up your server first. Schedule a maintenance window: applying performance settings can restart nginx and databases and reload PHP.

### Uninstalling

Download the uninstaller:

```bash
curl -fSL --retry 3 -o /tmp/xpanel-uninstall.sh https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh
```

| Action | Command |
|---|---|
| **Remove everything**: panel, customer data and hosting service packages (default) | `bash /tmp/xpanel-uninstall.sh` |
| Remove panel and customer data; retain service packages | `XPANEL_KEEP_PACKAGES=1 bash /tmp/xpanel-uninstall.sh` |
| Remove panel while retaining website/database/email data | `XPANEL_KEEP_DATA=1 bash /tmp/xpanel-uninstall.sh` |
| Clean up packages left by an earlier uninstall | `XPANEL_PURGE_PACKAGES=1 bash /tmp/xpanel-uninstall.sh` |

Deletion requires interactive confirmation. **The default removal permanently purges nginx, PHP, MariaDB, PostgreSQL, mail, FTP, fail2ban, ClamAV, Redis and Memcached with their data, including services installed outside X-Panel.** Swap and disk quota settings are retained.

Swap, quota settings, network configuration and downloaded files in `/root` remain. Uninstalling is not equivalent to reinstalling the operating system.

## Features

| Area | Capabilities |
|---|---|
| Accounts | Admin → reseller → customer hierarchy, hosting packages, suspension, audit logs, TOTP 2FA with one-time recovery codes |
| Resources | Separate Linux users, disk quotas (mailboxes count towards the package), account-level CPU/RAM limits, monthly bandwidth limit (sites pause when used up) |
| Websites | HTML, PHP, Laravel, Node.js, Next.js and Go; nginx virtual hosts and reverse proxies |
| Applications | Live version discovery, background installation tasks, installed versions and supported uninstall actions |
| PHP | Parallel versions, extensions, global FPM settings, site overrides and logs |
| Databases | MariaDB and a separate customer PostgreSQL cluster, passwords and size reporting |
| Files | Browser file manager, code editor, uploads, FTP and SFTP — upload guide per app type: [docs/TAI-MA-NGUON.md](docs/TAI-MA-NGUON.md) |
| TLS | Let's Encrypt certificates |
| Email | Postfix, Dovecot, DKIM and Roundcube webmail |
| Backups | Encrypted backups to each account's Google Drive; restore or recreate an account on another server. Database and supported mailbox passwords are retained; FTP passwords must be reset — see [docs/SAO-LUU.md](docs/SAO-LUU.md) |
| Security | Malware rules, optional ClamAV, quarantine and opt-in OpenAI analysis/cleaning with the account's own API key; nftables, nginx limits, fail2ban, login lockout and alerts — see [docs/BAO-MAT.md](docs/BAO-MAT.md) |
| Operations | Cron jobs, task logs, server metrics and administrator-controlled reboot |
| Languages | Vietnamese and English with flag selectors |

### Application catalog

The catalog includes PHP, Composer, Node.js, Go, MariaDB, PostgreSQL, phpMyAdmin, Adminer, Redis and Memcached. Versions come from official sources or configured repositories instead of a fixed list in this document.

- Version information is cached for five minutes and can be refreshed manually. Source failures and stale data are shown; installation is blocked until the source is verified again.
- PHP stable releases are checked against php.net and packages available from Sury on Debian or Ondřej's PPA on Ubuntu. Legacy PHP lines remain for compatibility even after upstream support ends.
- Redis and Memcached use distribution packages and security updates; these may differ from the newest upstream versions.
- Downloads are checked against published checksums where available. Adminer does not publish a project checksum.
- Installation runs in the background with status and logs. One application installation task runs at a time.
- Major MariaDB/PostgreSQL upgrades are not currently available through the interface.

Redis and Memcached are shared localhost services without per-customer data isolation. Use them with an appropriate trust model.

### Website applications

Upload source code under `/home/<user>/www/<domain>`, select the application type or use source detection, and optionally create a sample project in an empty directory. Install the required runtime first.

PHP/Laravel websites and process-based applications have separate systemd services inside the customer's resource slice. Static HTML is served by nginx. Node.js, Next.js and Go receive an automatic localhost port and reverse proxy. Custom applications must respect `PORT`; Node.js apps should also respect `HOST` and bind to localhost.

**Build & restart** runs the appropriate npm, Composer/Laravel or Go build as the customer's Linux user, with a 30-minute timeout. Database migrations are not run automatically. Laravel uses the `public` document root; Node.js apps need a `start` script.

Each site supports environment variables and service logs. `PORT`, `HOST`, `PATH` and `HOME` are reserved. Environment files are root-owned with mode `0600`; values remain visible to the site owner and authorized managing accounts.

### PHP management

The **PHP** page provides installed-version selection, extensions, FPM settings and version logs. Extension actions run as background tasks with log dialogs. Core packages and built-in extensions are protected; removal is blocked when APT would remove additional dependencies.

Global settings include memory/upload limits, timeouts, timezone, sessions and OPcache. Overrides are written atomically to `/etc/php/<version>/fpm/conf.d/99-xpanel.ini`, validated before application and rolled back on failure. CLI configuration is separate. Relevant FPM masters reload their configuration.

Sites can override supported directives, request timeouts and slow-log thresholds. Workers are bounded by the hosting package's memory budget; only administrators can change worker counts. Slow logs have restricted permissions and rotation.

### Databases

| Purpose | Address | Management |
|---|---|---|
| Customer MariaDB | localhost:3306 | phpMyAdmin after installation |
| Customer PostgreSQL | 127.0.0.1:5433, separate `customers` cluster | Adminer after installation |
| Panel metadata | PostgreSQL main cluster, normally port 5432 | Internal use |

Customer PostgreSQL access is restricted between accounts. Suspension locks database users and stops application services. Database names are unique across both engines. Shared databases do not currently have separate CPU/RAM limits per customer.

## Automatic server tuning

Since v1.1.4, installation and upgrades calculate a shared-hosting profile from available CPU and RAM. No separate tuning command is needed.

| RAM / CPU | nginx connections/worker | MariaDB buffer / connections | PostgreSQL shared buffer / connections per cluster | OPcache per PHP master |
|---|---:|---:|---:|---:|
| 1 GB / 1 CPU | 2048 | 128 MB / 16 | 32 MB / 16 | 32 MB |
| 2 GB / 1 CPU | 4096 | 256 MB / 32 | 64 MB / 28 | 32 MB |
| 4 GB / 1 CPU | 4096 | 512 MB / 32 | 128 MB / 28 | 32 MB |
| 8 GB / 1 CPU | 8192 | 1024 MB / 32 | 256 MB / 28 | 64 MB |

Values use RAM reported by Linux: a VPS reporting 3922 MB gets a 490 MB MariaDB buffer and 122 MB PostgreSQL shared buffer per cluster. Connection counts also depend on CPU and memory caps.

The profile configures file descriptor limits, bounded database connection pools, slow-query logs, rotation and kernel connection queues. PHP uses on-demand workers, dividing the account's memory budget between sites and pools. Newly installed PHP versions receive the defaults and reload afterward (v1.1.5).

The calculated profile is saved at `/var/lib/xpanel/performance-profile.txt`. Rerun installation after resizing the VPS. Set `XPANEL_AUTO_TUNE=0` when running the installer to skip a new profile; this does not undo previous settings or disable existing swap/journal setup.

These are starting budgets, not hard database memory limits or a capacity guarantee. Queries and applications can consume more memory. Avoid overallocating hosting packages and measure real workloads before increasing limits. Shared Redis or full-page caching is not enabled automatically.

See [detailed formulas, rollback behavior and test methodology (Vietnamese)](docs/TUNING.md).

### Validation

Direct and repeat installation were verified on Debian 13 without Docker. A 1-vCPU/3922-MB VPS completed 27,000 requests across login HTML, an authenticated PostgreSQL-backed API and a PHP hashing workload at concurrency 1, 8 and 32, with no observed HTTP/content errors. At concurrency 32, p95 latency was approximately 22–38 ms.

These were short local smoke tests with the client sharing server CPU. They do not establish Internet latency, sustained stability or WordPress/Laravel capacity. Ubuntu 24.04 testing also covered runtimes, sample sites, database isolation, suspension/reactivation and PHP management. This does not mean every feature was tested on every supported OS version.

## Security and operations

Host firewall limits can reduce connection floods and password guessing; they
cannot stop a volumetric DDoS attack that saturates the VPS network connection.
Use upstream provider protection where needed. Malware rules and AI analysis can
produce false positives and false negatives; review findings before changing files.
AI analysis sends up to 60 KiB of flagged file content to OpenAI and uses the
hosting account's API credits. ClamAV signature-download and scan failures must
be checked in task logs.

- The API runs as the unprivileged `xpanel` user; a root agent accepts allowlisted operations over a restricted Unix socket.
- Authentication includes short-lived access tokens, rotating refresh tokens, role permissions, two-factor authentication and audit logs.
- Hosting users have separate Linux accounts and resource slices. File permissions and database access restrict cross-customer access.
- Databases listen locally by default. Backup credentials and two-factor secrets are encrypted using a server-specific key.
- fail2ban protects supported authentication services. Mail configuration includes authenticated submission and DKIM controls.
- Reboot requires administrator confirmation, rejects impersonated sessions and pending/running tasks, and records an audit event.

The dashboard shows server IP, CPU, RAM, swap, storage, uptime, kernel and network totals. Metrics refresh every five seconds. Network totals are since boot; the CPU chart retains up to 30 samples in the current viewing session.

Optional ports: **8890** for database tools, **21 and 30000–30100** for FTP, **25/465/587/993/995** for mail, **2096** for webmail. Open only the ports needed. Mail delivery also depends on provider policies, DNS and IP reputation.

## Troubleshooting installation DNS

`Temporary failure resolving 'deb.debian.org'` means the VPS cannot resolve repository names. If curl installation fails here, the panel script has not run yet. `--fix-missing` does not repair DNS.

```bash
ip route
cat /etc/resolv.conf
getent hosts deb.debian.org security.debian.org raw.githubusercontent.com github.com
```

If the default route is missing, correct the gateway using the provider console. If routing works but DNS does not, configure your provider's DNS in the VPS network manager. Public resolvers `1.1.1.1` and `8.8.8.8` are alternatives where allowed.

With active systemd-resolved, set DNS temporarily for the outgoing interface:

```bash
iface=$(ip -4 route show default | awk '/default/ {print $5; exit}')
test -n "$iface" && resolvectl dns "$iface" 1.1.1.1 8.8.8.8
resolvectl flush-caches
```

Without systemd-resolved, back up the resolver before a temporary change:

```bash
cp -L /etc/resolv.conf "/root/resolv.conf.before-xpanel-$(date +%Y%m%d-%H%M%S)" && printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > /etc/resolv.conf
```

Verify resolution and retry installation. DHCP/cloud-init may overwrite temporary settings; persist DNS in the actual network configuration. The installer does not overwrite system DNS. If resolution still fails, check provider connectivity and DNS firewall rules.

## Architecture and development

```text
Browser → HTTPS :8888 → nginx → unprivileged Go API → PostgreSQL
                                  │
                                  └→ restricted Unix socket → root Go agent
```

| Path | Purpose |
|---|---|
| `apps/api` | Go REST API, authentication, permissions and migrations |
| `apps/agent` | Linux services, users, quotas, websites and runtimes |
| `apps/web` | Next.js 16 / Tailwind 4 UI, exported as static files |
| `pkg/agentproto` | Agent protocol and client |
| `installer` | Direct installation and tuning |
| `deploy` | systemd, nginx and development tooling |
| `tests` | Unit, installer, integration and bounded load checks |

Use the Go version in `go.mod` and a Node.js version supported by the web dependencies. Build a release:

```bash
./scripts/build-release.sh amd64
```

Output: `dist/xpanel-linux-amd64.tar.gz`; use `arm64` for that architecture. Copy and extract the archive on a supported clean server and run its `install.sh` as root.

Development checks:

```bash
go test ./...
go vet ./...
cd apps/web && npm install && npm run lint && npm run build
```

The agent requires Linux. Optional systemd-container integration tools are available for development; production runs directly on the host. More workflows: [Vietnamese developer guide](README.vi.md#dành-cho-nhà-phát-triển) and [installation guide (Vietnamese)](docs/CAI-DAT.md).

English UI messages are in `apps/web/lib/locales/en.json`. The language preference is shared between browser tabs. Stored identifiers and server-generated logs retain their original values.

## License

X-Panel is available under the [X-Panel Non-Commercial License 1.0](LICENSE).
Individuals and organizations may use, modify and redistribute it for
**non-commercial purposes only**, retaining the license and attribution.
Commercial use, including paid hosting, resale, SaaS and internal infrastructure
supporting revenue-generating business activities, requires a separate written
license from the applicable copyright holders. Third-party components retain
their own licenses. This is a source-available license, not an OSI-approved
open-source license. See [LICENSE](LICENSE) for the governing terms.

---

X-Panel · Hosting control panel — a product of [socsmart.com](https://socsmart.com).
