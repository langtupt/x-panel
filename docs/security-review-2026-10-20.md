# Security review — 2026-10-20

Reviewed authentication, tenant authorization, backup restore, privileged command execution, filesystem confinement, dependencies and deployed listeners. This is a targeted code review, not a complete penetration test.

## Fixed and deployed

- Backup SQL was imported through privileged database clients. Imports now use random temporary database logins scoped to the target database/customer role, and run under the customer OS identity. MariaDB disables local infile and client commands in batch binary mode. PostgreSQL temporary roles expire, are disabled, have sessions terminated and ownership reassigned before deletion. Cleanup errors propagate.
- Mail restore now uses os.Root confinement, rejects a symlink domain directory and prevents nested symlink/path traversal outside its domain.
- Home restore no longer treats tar exit status 1/2 as success; stream/close failures propagate.
- Backup manifest must be the first entry and unique; gzip and encrypted stream tails are checked. Restore is streaming, not transactional: failure can leave partially restored data. Retain a backup before restoring.
- Enabling 2FA cannot replace an already enabled factor, including concurrent enable requests (conditional database update). Disable requires password and the existing code.
- TOTP replay cache normalizes spaces consistently; weak secrets under 160 bits are rejected. Replay cache remains process-local and resets on restart.
- Minimum Go version raised from 1.26.0 to 1.26.9 to address standard library vulnerabilities reported by govulncheck.
- shadcn CLI moved to development dependencies. npm production audit: zero findings. Seven high development dependency warnings remain in the upstream CLI chain; it is not used by the exported runtime.

## Validation

- go test ./... and go vet ./... passed.
- govulncheck: zero reachable vulnerabilities; one required-module advisory without a called vulnerable path.
- Added regression tests for TOTP replay/overwrite/weak keys and mail restore containment.
- Backup cryptography and mail confinement tests passed on the Linux VPS.
- Direct deployment with old binaries backed up under /root/xpanel-security-backup-*; no Docker.
- VPS API, agent and nginx active; nginx configuration valid. Login, live server metrics and referenced dashboard/login assets verified.
- Database and Redis listeners bind localhost. Port 8890 is the intended isolated nginx database-tools origin; 8891 is loopback OpenDKIM.

## Limits

No destructive restore of a real customer database was performed. Database restore privilege isolation needs a dedicated disposable MariaDB/PostgreSQL integration suite. No claim is made that all security flaws have been eliminated.
