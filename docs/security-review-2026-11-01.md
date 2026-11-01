# Security review — 2026-11-01

Targeted code review of the API routes and tenant authorization, the agent's privileged commands, and files that hold secrets. It is not a full penetration test.

## Fixed

- **Root code execution from a site's environment variables (critical).** A site build passed the customer's environment variables to `systemd-run` through that root process's own environment, so `LD_PRELOAD`/`LD_LIBRARY_PATH` set by a customer were loaded as root. The build now gives the variables to its unit through a root-only temporary `EnvironmentFile` in `/run`. systemd reads that file before it switches to the customer account.
- **Mail interception through an arbitrary mail domain (high).** Any account could add mail hosting for a domain that no other account's website used (`gmail.com`, the panel's domain, a customer's apex domain…). Postfix then delivered locally all mail that any website on the server sent to that domain. Non-admin accounts can now only host mail for one of their own website domains or aliases, or a subdomain of one. Administrators are not restricted.
- **Cron commands readable by other accounts (medium).** Job scripts in `/etc/xpanel-cron/<user>/` were mode 0644 inside a 0755 directory. These commands often contain tokens or database passwords. The directory is now 0750 and the scripts 0640, owned by `root:<account group>`.
- **phpMyAdmin `blowfish_secret` was world-readable (low).** `config.inc.php` was 0644. It is now 0640, owned by `root:xpanel-pma`.

## Reviewed, no change

- Authentication: HS256 JWTs (15 min), rotating refresh tokens with reuse detection, per-IP and per-account login limits, a TOTP replay cache, and an `X-XPanel` CSRF header on every non-GET request. `X-Real-IP` is trusted only from the nginx unix socket or loopback.
- Tenant checks on files, tasks, audit, backups, malware, mail, FTP, and firewall (admin only).
- File manager: chroot plus privilege drop to the account before any request is read.
- Database names and passwords: validated against a strict charset before they are used in SQL.
- PHP per-site directives: newlines, quotes and `;` are rejected, so nothing can be injected into the FPM pool.
- Self-update: the repository is fixed and only a validated release tag is accepted.

## Limits

No dynamic testing was done on a live server for these changes. The e2e suites `mail` and `limits2` were updated for the new mail-domain rule.
