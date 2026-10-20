#!/usr/bin/env bash
# End-to-end tests: install a release into a systemd container and drive the
# real API (websites, PHP, SSL via Pebble, databases, FTP/SFTP, files, cron, 2FA…).
#
#   ./scripts/build-release.sh amd64
#   ./tests/e2e/run.sh debian      # or: ubuntu
#
# Needs Docker with privileged containers (loop-mounted ext4 /home for quotas).
set -euo pipefail
DISTRO="${1:-debian}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
# Docker Desktop on Windows (Git Bash) needs native paths.
hostpath() { if command -v cygpath >/dev/null; then cygpath -m "$1"; else echo "$1"; fi; }
export MSYS_NO_PATHCONV=1
NAME="xpanel-e2e-$DISTRO"
IMG="xpanel-e2e-sysd-$DISTRO"

# Fake Google Drive for backup.sh.
(cd "$ROOT" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$(hostpath "$HERE")/fakedrive/fakedrive" ./tests/e2e/fakedrive)
docker build -q -t "$IMG" -f "$(hostpath "$HERE")/Dockerfile.$DISTRO" "$(hostpath "$HERE")" >/dev/null
docker network create xpanel-e2e >/dev/null 2>&1 || true
docker rm -f xpanel-e2e-pebble "$NAME" >/dev/null 2>&1 || true
# Let's Encrypt's test CA; VA_ALWAYS_VALID skips HTTP-01 reachability checks.
docker run -d --name xpanel-e2e-pebble --network xpanel-e2e --network-alias pebble \
  -e PEBBLE_VA_ALWAYS_VALID=1 -e PEBBLE_VA_NOSLEEP=1 ghcr.io/letsencrypt/pebble:latest >/dev/null
docker run -d --name "$NAME" --network xpanel-e2e --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw -v "$(hostpath "$ROOT")/dist:/dist:ro" -v "$(hostpath "$HERE"):/e2e:ro" "$IMG" >/dev/null
sleep 5

docker exec "$NAME" bash -c '
set -e
truncate -s 1G /homefs.img && mkfs.ext4 -q /homefs.img && mkdir -p /home && mount -o loop /homefs.img /home
echo "/homefs.img /home ext4 defaults,loop 0 2" >> /etc/fstab
cp -r /dist/xpanel-linux-amd64 /root/rel && cd /root/rel
ADMIN_PASSWORD="Admin@123!" ./install.sh >/tmp/install.log 2>&1 || { tail -20 /tmp/install.log; exit 1; }
./enable-quota.sh >/dev/null
printf "XPANEL_ACME_DIR=https://pebble:14000/dir\nXPANEL_ACME_INSECURE=true\n" > /etc/xpanel/agent.env
systemctl restart xpanel-agent
apt-get install -y -q jq >/dev/null 2>&1'

# Read a stable snapshot even if a developer edits the mounted test sources.
docker exec "$NAME" cp -a /e2e /root/e2e
for t in ${TESTS:-web db ftp-files-pma files auth-cron limits applications isolation ssl mail backup stats limits2 malware firewall}; do
  echo "======== $t"
  if docker exec "$NAME" bash "/root/e2e/$t.sh"; then
    echo "SUITE PASS: $t"
  else
    FAILED="${FAILED:-} $t"
    echo "SUITE FAIL: $t"
  fi
done
echo "Container $NAME is left running for inspection (docker rm -f $NAME xpanel-e2e-pebble)."
if [ -n "${FAILED:-}" ]; then echo "SUITES FAILED:$FAILED"; exit 1; fi
echo "ALL SUITES PASSED"
