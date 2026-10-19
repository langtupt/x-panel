#!/usr/bin/env bash
# X-Panel one-line installer:
#   curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/installer/bootstrap.sh | sudo bash
# Options (environment variables):
#   ADMIN_PASSWORD=...   admin password (default: random, printed at the end)
#   XPANEL_VERSION=v1.0.0  release tag (default: latest)
# Downloads the release for this CPU from GitHub, checks its SHA-256 and runs
# install.sh. Running it again upgrades an existing installation.
set -euo pipefail

REPO="${XPANEL_REPO:-langtupt/x-panel}"
VERSION="${XPANEL_VERSION:-latest}"

die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "Cần chạy bằng root: curl -fsSL ... | sudo bash"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "Kiến trúc CPU chưa hỗ trợ: $(uname -m)" ;;
esac
command -v curl >/dev/null || { apt-get update -q && apt-get install -y -q curl ca-certificates; }

if [ "$VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
FILE="xpanel-linux-$ARCH.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> Tải $BASE/$FILE"
curl -fL --retry 3 -o "$TMP/$FILE" "$BASE/$FILE" || die "Không tải được bản phát hành"
curl -fsSL --retry 3 -o "$TMP/$FILE.sha256" "$BASE/$FILE.sha256" || die "Không tải được tệp SHA-256"
(cd "$TMP" && sha256sum -c "$FILE.sha256") || die "SHA-256 không khớp, dừng cài đặt"

tar -C "$TMP" -xzf "$TMP/$FILE"
cd "$TMP/xpanel-linux-$ARCH"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}" ./install.sh
# Turn on disk quotas when /home supports it (needs a reboot-free remount).
./enable-quota.sh || echo "!! Chưa bật được quota tự động; xem hướng dẫn trong README."
