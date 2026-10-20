# Roadmap 2 validation

Validation date: 2026-10-20. Application code under test: commit `4c6d49c`,
with the test-fixture corrections in `289be04` and `7de5aac` applied in targeted
reruns. Documentation changes do not change the application binaries.

## Static checks and builds

- `go test ./...` and `go vet ./...` passed.
- Web TypeScript check and Next.js production build passed.
- Bash syntax checks passed for the end-to-end scripts.
- API and agent cross-compilation passed for Linux amd64 and arm64.
- Malware rule tests were also executed as a Linux test binary.

## Debian 12 integration results

Tests ran in the existing project's privileged systemd-container test harness,
using a fresh Debian 12 environment. This is test infrastructure; no customer
VPS was deployed, reinstalled or uninstalled during this continuation.

| Suite | Final result |
|---|---|
| web | Passed |
| db | Passed |
| ftp-files-pma | Passed, including the legacy phpMyAdmin installation endpoint |
| files | Passed |
| auth-cron | Passed |
| limits | Passed |
| applications | Passed: six website types, both database engines, suspend/reactivate |
| isolation | Passed: sockets, app ports, files, processes, API access and rule cleanup |
| ssl | Passed with the Pebble test CA |
| mail | Passed, including authenticated submission, access controls and Roundcube |
| backup | Passed with fake Drive, including account recreation and retained DB/mailbox passwords |
| stats | Passed |
| limits2 | Passed on targeted rerun after correcting a sample timestamp collision |
| malware | Passed on targeted rerun, including the explicit 1,000-finding truncation regression |
| firewall | Passed, including zero-burst nginx settings and ban/unban |
| uninstall | Passed: removed panel/customer data and firewall rules; reinstall and admin login worked |

The aggregate run exited with status 1 because its original `limits2` invocation
used an invalid fixture: multiple resource samples were moved to the same
primary-key timestamp. Its corrected rerun passed before uninstall. The new
malware overflow fixture initially tried to write into a root-owned home
directory; it was moved into the customer's writable `www` directory, made to
fail immediately if setup fails, and rerun successfully. Do not describe the
original aggregate run as having exited successfully.

The application code remained fixed throughout this final run and its targeted
reruns. Earlier exploratory results were superseded after application fixes.

## Regression fixes verified

- Resolve the legacy phpMyAdmin `latest` alias using the live catalog.
- Omit an explicit `burst=0`, which nginx rejects.
- Synchronize persisted bandwidth blocks across a month boundary; refresh a
  stale marker if the new month is already over its limit.
- Preserve unseen open findings when a malware scan is truncated.
- Validate site/task IDs and application ports before isolation assertions;
  query exact nftables elements instead of matching an empty string.
- Include the new suites in the default runner and snapshot scripts before
  executing them, so editing mounted sources cannot corrupt a running script.

## Coverage limits

Google Drive and OpenAI responses were simulated; this run did not verify live
OAuth, real OpenAI model behavior/billing or optional ClamAV signature downloads.
SSL issuance used Pebble rather than public Let's Encrypt. Mail tests do not
establish external deliverability or IP reputation. Some legacy suites include
observational smoke checks in addition to assertions. ARM64 was compiled, not
executed. The new feature set still requires deployment-specific verification
on a real VPS and broader OS coverage before claiming production readiness.

Local detailed build and test logs are kept in the ignored `dist` directory;
they may contain temporary test credentials and must not be published.
