#!/usr/bin/env bash
# Isolated installer failure-path checks on supported Debian/Ubuntu, no packages installed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/mock" "$TMP/release/bin" "$TMP/release/web"
cp "$ROOT/installer/install.sh" "$TMP/release/install.sh"
touch "$TMP/release/bin/xpanel-api" "$TMP/release/bin/xpanel-agent"
printf '#!/bin/sh\necho 0\n' >"$TMP/mock/id"
printf '#!/bin/sh\nexit 2\n' >"$TMP/mock/getent"
cat >"$TMP/mock/apt-get" <<'EOF'
#!/bin/sh
echo "$*" >>"$APT_CALLS"
exit 100
EOF
chmod +x "$TMP/mock/"*
export APT_CALLS="$TMP/apt-calls"
export PATH="$TMP/mock:$PATH"
if XPANEL_FORCE=1 bash "$ROOT/install.sh" >"$TMP/dns.log" 2>&1; then
  echo 'FAIL: installer accepted broken DNS'; exit 1
fi
grep -q 'Không phân giải được github.com' "$TMP/dns.log"
test ! -e "$APT_CALLS"
if bash "$TMP/release/install.sh" >"$TMP/apt.log" 2>&1; then
  echo 'FAIL: installer accepted failed APT update'; exit 1
fi
grep -q 'Không cập nhật được APT' "$TMP/apt.log"
grep -q 'APT::Update::Error-Mode=any' "$APT_CALLS"
test "$(wc -l <"$APT_CALLS")" -eq 1
echo 'PASS: DNS failure stops before downloads; APT update failure stops before installation'
