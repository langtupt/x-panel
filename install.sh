#!/usr/bin/env bash
# X-Panel installation and upgrade bootstrap. Run as root:
# curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | bash
# Options (environment variables before bash):
# ADMIN_PASSWORD='...'   Set/reset admin password; otherwise generated for new installs.
# XPANEL_VERSION=v1.1.5  Select a release; defaults to latest.
# XPANEL_FORCE=1         Override conflicting server/panel checks.
# Downloads and verifies a release, installs it, enables quotas and tunes the VPS.
# Running again upgrades the installation while retaining data and configuration.
set -euo pipefail

# Everything runs inside main(), called on the last line: with "curl | bash" a
# truncated download then does nothing instead of running half a script.
main() {

REPO="${XPANEL_REPO:-langtupt/x-panel}"
XP_VERSION="${XPANEL_VERSION:-latest}"
LOG=/var/log/xpanel-install.log

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx %s\033[0m\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- checks
[ "$(id -u)" -eq 0 ] || die "Root access is required. Log in as root (or run sudo -i), then run the command again."
[ -r /etc/os-release ] || die "Unable to identify the operating system."
# Read in a subshell: os-release defines VERSION, NAME… which must not
# overwrite this script's variables.
OS_ID=$(. /etc/os-release && echo "${ID:-}")
OS_VER=$(. /etc/os-release && echo "${VERSION_ID:-}")
PRETTY_NAME=$(. /etc/os-release && echo "${PRETTY_NAME:-$OS_ID $OS_VER}")
case "$OS_ID:$OS_VER" in
  debian:12*|debian:13*|ubuntu:22.04|ubuntu:24.04) ;;
  *) die "Unsupported operating system: ${PRETTY_NAME:-unknown}. X-Panel requires a fresh Debian 12/13 or Ubuntu 22.04/24.04 installation." ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "Unsupported CPU architecture: $(uname -m). Supported architectures: x86_64 and arm64." ;;
esac

MEM_MB=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)
DISK_MB=$(df -Pm / | awk 'NR==2 {print $4}')
[ "$MEM_MB" -ge 700 ] || die "Insufficient memory (${MEM_MB} MB). Use a VPS with at least 1 GB RAM."
[ "$DISK_MB" -ge 3000 ] || die "Insufficient free disk space (${DISK_MB} MB). At least 5 GB is recommended."
[ "$MEM_MB" -ge 1500 ] || warn "Detected ${MEM_MB} MB RAM. At least 2 GB is recommended for email and multiple websites."

UPGRADE=0
[ -f /etc/xpanel/xpanel.env ] && UPGRADE=1
# A fresh install takes over ports 80/443 and the web server: refuse to run
# on a machine that already serves sites with something else.
if [ "$UPGRADE" = 0 ] && [ "${XPANEL_FORCE:-0}" != 1 ]; then
  for svc in apache2 httpd lsws litespeed caddy; do
    if systemctl is-active -q "$svc" 2>/dev/null; then
      die "This VPS is running $svc. X-Panel requires a fresh server and manages nginx, PHP, MariaDB and mail. Reinstall the OS first, or use XPANEL_FORCE=1 to explicitly override this check."
    fi
  done
  for dir in /usr/local/cpanel /www/server/panel /usr/local/directadmin /usr/local/hestia /usr/local/vesta /usr/local/CyberCP /usr/local/fastpanel2 /opt/fastpanel2; do
    [ -e "$dir" ] && die "Another hosting panel was detected ($dir). Install X-Panel on a fresh operating system."
  done
fi

# ---------------------------------------------------------------- download
export DEBIAN_FRONTEND=noninteractive
getent ahosts github.com >/dev/null 2>&1 \
  || die "Unable to resolve github.com. Check VPS connectivity and DNS (getent hosts github.com). See DNS troubleshooting in the README; this script does not change system DNS."
if ! command -v curl >/dev/null || ! command -v sha256sum >/dev/null; then
  say "Installing download prerequisites"
  apt-get -o Acquire::Retries=3 -o APT::Update::Error-Mode=any update -q </dev/null \
    || die "APT update failed. Check the DNS, connectivity or repository errors above."
  apt-get -o Acquire::Retries=3 install -y -q curl ca-certificates coreutils </dev/null \
    || die "Unable to install curl and CA certificates. Check package repository connectivity above."
fi

if [ "$XP_VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$XP_VERSION"
fi
FILE="xpanel-linux-$ARCH.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo
say "X-Panel — ${PRETTY_NAME}, ${ARCH}, RAM ${MEM_MB} MB$( [ "$UPGRADE" = 1 ] && echo ", UPGRADING existing installation")"
say "Downloading release ($XP_VERSION)..."
curl -fsSL --retry 3 -o "$TMP/$FILE" "$BASE/$FILE" || die "Unable to download $BASE/$FILE. Check the VPS Internet connection."
curl -fsSL --retry 3 -o "$TMP/$FILE.sha256" "$BASE/$FILE.sha256" || die "Unable to download the SHA-256 checksum file."
(cd "$TMP" && sha256sum -c --quiet "$FILE.sha256") || die "SHA-256 verification failed. The download may be corrupted; run the command again."
tar -C "$TMP" -xzf "$TMP/$FILE"

# ---------------------------------------------------------------- install
say "Installing X-Panel. This usually takes 3-10 minutes (log: $LOG)..."
cd "$TMP/xpanel-linux-$ARCH"
set +e
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}" XPANEL_ENABLE_QUOTA=1 ./install.sh </dev/null 2>&1 | tee "$LOG"
rc=${PIPESTATUS[0]}
set -e
chmod 600 "$LOG"
# The log must not keep the admin password shown at the end.
sed -i -E 's/^(  (Password  |Mật khẩu  ): ).*$/\1(displayed on screen)/' "$LOG" 2>/dev/null || true
if [ "$rc" != 0 ]; then
  echo
  die "Installation failed (exit code $rc). Review the errors above or in $LOG, resolve the issue and run the installer again."
fi
}

main "$@"
