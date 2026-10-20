#!/usr/bin/env bash
# Failure-path and repeat-purge regression checks. Run on the Debian test VPS.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP=$(mktemp -d)
trap '/bin/rm -rf "$TMP"' EXIT
mkdir "$TMP/bin"
export CALLS="$TMP/calls"
printf '#!/bin/sh\necho 0\n' >"$TMP/bin/id"
for cmd in runuser nginx; do
  printf '#!/bin/sh\nexit 1\n' >"$TMP/bin/$cmd"
done
printf '#!/bin/sh\ncase "$1" in cat) exit 1 ;; *) exit 0 ;; esac\n' >"$TMP/bin/systemctl"
for cmd in nft sysctl pg_lsclusters pkill groupdel; do
  printf '#!/bin/sh\nexit 0\n' >"$TMP/bin/$cmd"
done
cat >"$TMP/bin/getent" <<'EOF'
#!/bin/sh
if [ "$*" = 'passwd xpanel' ]; then
  echo 'xpanel:x:1000:1000::/home/xpanel:/bin/bash'
else
  exit 2
fi
EOF
cat >"$TMP/bin/dpkg-query" <<'EOF'
#!/bin/sh
printf 'nginx ii \nphp8.3-fpm rc \n'
EOF
cat >"$TMP/bin/apt-get" <<'EOF'
#!/bin/sh
echo "apt-get $*" >>"$CALLS"
exit "${APT_FAIL:-0}"
EOF
for cmd in rm userdel sed mv sshd postconf; do
  printf '#!/bin/sh\necho "%s $*" >>"$CALLS"\n' "$cmd" >"$TMP/bin/$cmd"
done
chmod +x "$TMP/bin/"*
export PATH="$TMP/bin:$PATH" XPANEL_PURGE_PACKAGES=1 XPANEL_CONFIRM=XOA-SACH
if APT_FAIL=100 bash "$ROOT/uninstall.sh" >"$TMP/fail.log" 2>&1; then
  echo 'FAIL: ignored APT failure'; exit 1
fi
grep -q 'APT purge thất bại' "$TMP/fail.log"
if grep -q '/var/lib/mysql' "$CALLS"; then
  echo 'FAIL: deleted database directory after failed purge'; exit 1
fi
>"$CALLS"
bash "$ROOT/uninstall.sh" >"$TMP/success.log" 2>&1
grep -q '^apt-get purge -y -q nginx php8.3-fpm$' "$CALLS"
grep -q '^apt-get autoremove --purge -y -q$' "$CALLS"
if grep -qx 'userdel xpanel' "$CALLS"; then
  echo 'FAIL: removed existing interactive account'; exit 1
fi
if XPANEL_KEEP_DATA=1 bash "$ROOT/uninstall.sh" >"$TMP/conflict.log" 2>&1; then
  echo 'FAIL: accepted keep-data with purge'; exit 1
fi
echo 'PASS: repeat purge, rc packages, APT failure, existing account, keep-data conflict'
