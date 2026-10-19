#!/usr/bin/env bash
# Build a Linux release tarball: dist/xpanel-linux-<arch>.tar.gz
#   ./scripts/build-release.sh [amd64|arm64]
set -euo pipefail

ARCH="${1:-amd64}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist/xpanel-linux-$ARCH"

rm -rf "$OUT" && mkdir -p "$OUT/bin"

echo "==> Go binaries ($ARCH)"
cd "$ROOT"
for cmd in xpanel-api xpanel-agent; do
  app="${cmd#xpanel-}"
  GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o "$OUT/bin/$cmd" "./apps/$app/cmd/$cmd"
done

echo "==> Web UI"
cd "$ROOT/apps/web"
npm ci --silent
npm run build
cp -r out "$OUT/web"

echo "==> Installer"
cp -r "$ROOT/deploy" "$OUT/deploy"
rm -f "$OUT/deploy/docker-compose.dev.yml"
install -m 0755 "$ROOT/installer/install.sh" "$ROOT/installer/enable-quota.sh" "$OUT/"

tar -C "$ROOT/dist" -czf "$OUT.tar.gz" "xpanel-linux-$ARCH"
(cd "$ROOT/dist" && sha256sum "xpanel-linux-$ARCH.tar.gz" >"xpanel-linux-$ARCH.tar.gz.sha256")
echo "==> $OUT.tar.gz"
