#!/usr/bin/env bash
# X-Panel installer.
# Supported: Debian 12/13, Ubuntu 22.04/24.04.
#
# Run from an extracted release (bin/, web/, deploy/ next to this script):
#   sudo ./install.sh
# Re-running upgrades binaries/UI and keeps config, database and admin account.
set -euo pipefail

SRC="$(cd "$(dirname "$0")" && pwd)"
PREFIX=/opt/xpanel
ETC=/etc/xpanel
ENV_FILE=$ETC/xpanel.env
PANEL_PORT=8888
API_SOCK=/run/xpanel-api/api.sock

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "Cần chạy bằng root (sudo)."
[ -f "$SRC/bin/xpanel-api" ] && [ -f "$SRC/bin/xpanel-agent" ] && [ -d "$SRC/web" ] \
  || die "Không tìm thấy bin/ và web/ cạnh install.sh. Hãy chạy từ thư mục release."

# ---------------------------------------------------------------- detect OS
. /etc/os-release
MAJOR="${VERSION_ID%%.*}"
case "$ID" in
  debian) [ "$MAJOR" -ge 12 ] || die "Cần Debian 12 trở lên." ;;
  ubuntu) case "$VERSION_ID" in 22.04|24.04) ;; *) die "Cần Ubuntu 22.04 hoặc 24.04." ;; esac ;;
  *)      die "Hệ điều hành chưa hỗ trợ: $ID (chỉ hỗ trợ Debian và Ubuntu)." ;;
esac
log "Hệ điều hành: $PRETTY_NAME"

# ---------------------------------------------------------------- packages
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q nginx postgresql mariadb-server quota openssl curl ca-certificates \
  pure-ftpd openssh-server cron nftables
systemctl enable --now postgresql nginx mariadb cron

# ---------------------------------------------------------------- VPS tuning
MEM_MB=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)
log "RAM: ${MEM_MB} MB"
# Small VPS without swap: add a swap file so builds (npm, composer) and
# MariaDB do not get OOM-killed. Skipped in containers and when swap exists.
if [ "$MEM_MB" -lt 4096 ] && [ "$(swapon --noheadings 2>/dev/null | wc -l)" = 0 ] && [ ! -e /swapfile ] \
   && ! systemd-detect-virt --container -q 2>/dev/null; then
  SWAP_MB=$(( MEM_MB < 2048 ? 2048 : 1024 ))
  if fallocate -l "${SWAP_MB}M" /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count="$SWAP_MB" status=none; then
    chmod 600 /swapfile && mkswap -q /swapfile && swapon /swapfile \
      && echo "/swapfile none swap sw 0 0" >>/etc/fstab && log "Đã tạo swap ${SWAP_MB} MB" \
      || { warn "Không tạo được swap"; rm -f /swapfile; }
  fi
fi
install -m 0644 /dev/stdin /etc/sysctl.d/90-xpanel.conf <<'EOF'
# Managed by X-Panel
vm.swappiness = 10
net.core.somaxconn = 1024
net.ipv4.tcp_max_syn_backlog = 2048
fs.inotify.max_user_watches = 524288
EOF
sysctl -q --system >/dev/null 2>&1 || true
# Keep the journal from filling small disks.
install -d /etc/systemd/journald.conf.d
printf '[Journal]\nSystemMaxUse=300M\nMaxRetentionSec=1month\n' >/etc/systemd/journald.conf.d/90-xpanel.conf
systemctl restart systemd-journald 2>/dev/null || true

# Customer databases: MariaDB on localhost only, no anonymous users / test DB.
# Buffer pool sized to the machine: 1/8 of RAM, 64 MB – 2 GB.
POOL_MB=$(( MEM_MB / 8 )); [ "$POOL_MB" -lt 64 ] && POOL_MB=64; [ "$POOL_MB" -gt 2048 ] && POOL_MB=2048
install -m 0644 /dev/stdin /etc/mysql/mariadb.conf.d/90-xpanel.cnf <<EOF
# Managed by X-Panel
[mysqld]
bind-address = 127.0.0.1
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci
innodb_buffer_pool_size = ${POOL_MB}M
performance_schema = OFF
max_connections = 150
EOF
systemctl restart mariadb
mariadb <<'EOF'
DELETE FROM mysql.global_priv WHERE User = '';
DROP DATABASE IF EXISTS test;
FLUSH PRIVILEGES;
EOF

# ---------------------------------------------------------------- SFTP / FTP
# SFTP: hosting users (group xpanel-sftp) are chrooted into their home, sftp only.
getent group xpanel-sftp >/dev/null || groupadd --system xpanel-sftp
if ! grep -q "BEGIN X-Panel" /etc/ssh/sshd_config; then
  cat >>/etc/ssh/sshd_config <<'EOF'
# BEGIN X-Panel (managed)
Match Group xpanel-sftp
    ChrootDirectory %h
    ForceCommand internal-sftp -u 0022
    PasswordAuthentication yes
    AllowTcpForwarding no
    X11Forwarding no
# END X-Panel
EOF
fi
mkdir -p /run/sshd
sshd -t
systemctl reload ssh 2>/dev/null || systemctl restart ssh

# FTP: Pure-FTPd with X-Panel virtual users only (no system logins), chrooted, TLS optional.
PF=/etc/pure-ftpd/conf
echo yes >"$PF/ChrootEveryone"
echo yes >"$PF/NoAnonymous"
echo no >"$PF/PAMAuthentication"
echo no >"$PF/UnixAuthentication"
echo 1000 >"$PF/MinUID"
echo "30000 30100" >"$PF/PassivePortRange"
echo 1 >"$PF/TLS"
echo /etc/pure-ftpd/pureftpd.pdb >"$PF/PureDB"
ln -sf ../conf/PureDB /etc/pure-ftpd/auth/50pure
touch /etc/pure-ftpd/pureftpd.passwd
pure-pw mkdb /etc/pure-ftpd/pureftpd.pdb -f /etc/pure-ftpd/pureftpd.passwd

# ---------------------------------------------------------------- service user
getent group xpanel >/dev/null || groupadd --system xpanel
id xpanel >/dev/null 2>&1 || useradd --system --gid xpanel --home-dir /var/lib/xpanel \
  --create-home --shell /usr/sbin/nologin xpanel
# Shared state lives here (ACME webroot read by nginx, phpMyAdmin sessions), so it
# must be traversable; Ubuntu 24.04 creates homes as 0750.
chmod 0755 /var/lib/xpanel

# ---------------------------------------------------------------- database
# The API connects over the local socket with peer auth (OS user xpanel =
# DB role xpanel), so no password is needed and pg_hba.conf stays untouched.
pg() { (cd /tmp && runuser -u postgres -- "$@"); }
pg psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='xpanel'" | grep -q 1 || pg createuser xpanel
pg psql -tAc "SELECT 1 FROM pg_database WHERE datname='xpanel'" | grep -q 1 || pg createdb -O xpanel xpanel

# ---------------------------------------------------------------- files
log "Cài đặt vào $PREFIX"
install -d -m 0755 "$PREFIX/bin"
systemctl stop xpanel-api xpanel-agent 2>/dev/null || true
install -m 0755 "$SRC/bin/xpanel-api" "$SRC/bin/xpanel-agent" "$SRC/enable-quota.sh" "$PREFIX/bin/"
rm -rf "$PREFIX/web.new" && cp -r "$SRC/web" "$PREFIX/web.new" && rm -rf "$PREFIX/web" && mv "$PREFIX/web.new" "$PREFIX/web"
chmod -R a+rX "$PREFIX/web"

install -d -m 0750 -o root -g xpanel "$ETC"
FIRST_INSTALL=0
if [ ! -f "$ENV_FILE" ]; then
  FIRST_INSTALL=1
  cat >"$ENV_FILE" <<EOF
XPANEL_LISTEN=unix:$API_SOCK
XPANEL_DATABASE_URL=postgres:///xpanel?host=/var/run/postgresql
XPANEL_JWT_SECRET=$(openssl rand -hex 32)
XPANEL_AGENT_SOCKET=/run/xpanel/agent.sock
XPANEL_WEB_DIR=$PREFIX/web
XPANEL_SECURE_COOKIES=true
EOF
  chown root:xpanel "$ENV_FILE"
  chmod 0640 "$ENV_FILE"
fi
# Older versions listened on 127.0.0.1:8889, reachable by every customer's
# processes: move to the socket only nginx can open.
sed -i "s#^XPANEL_LISTEN=127.0.0.1:8889\$#XPANEL_LISTEN=unix:$API_SOCK#" "$ENV_FILE"
# The API gives its socket to group www-data (nginx).
usermod -aG www-data xpanel

# Self-signed certificate for the panel port; can be replaced with a real one later.
install -d -m 0750 -o root -g root "$ETC/ssl"
if [ ! -f "$ETC/ssl/panel.crt" ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj "/CN=$(hostname -f 2>/dev/null || hostname)" \
    -keyout "$ETC/ssl/panel.key" -out "$ETC/ssl/panel.crt" 2>/dev/null
  chmod 0600 "$ETC/ssl/panel.key"
fi
# Pure-FTPd expects key + certificate in one file.
if [ ! -f /etc/ssl/private/pure-ftpd.pem ]; then
  cat "$ETC/ssl/panel.key" "$ETC/ssl/panel.crt" >/etc/ssl/private/pure-ftpd.pem
  chmod 0600 /etc/ssl/private/pure-ftpd.pem
fi
systemctl restart pure-ftpd

# ---------------------------------------------------------------- nginx
install -d -m 0755 /etc/nginx/conf.d/xpanel /var/lib/xpanel/acme /var/log/nginx/xpanel
install -m 0644 "$SRC/deploy/nginx/xpanel-panel.conf" /etc/nginx/conf.d/xpanel-panel.conf
# Website logs live outside /var/log/nginx/*.log, which the distribution
# rotates; without this they grow until the disk is full.
install -m 0644 /dev/stdin /etc/logrotate.d/xpanel-nginx <<'EOF'
/var/log/nginx/xpanel/*.log {
    daily
    rotate 14
    missingok
    notifempty
    compress
    delaycompress
    sharedscripts
    postrotate
        [ -s /run/nginx.pid ] && kill -USR1 "$(cat /run/nginx.pid)"
    endscript
}
EOF
# Performance/security defaults, only where nginx.conf does not set them itself.
{
  echo "# Managed by X-Panel"
  for d in "server_tokens off" "gzip_vary on" "gzip_proxied any" "gzip_comp_level 5" "gzip_min_length 1024" \
    "gzip_types text/plain text/css text/xml application/json application/javascript application/xml application/rss+xml image/svg+xml" \
    "client_body_timeout 60s" "keepalive_timeout 30s"; do
    grep -qE "^[[:space:]]*${d%% *}[[:space:]]" /etc/nginx/nginx.conf || echo "$d;"
  done
} >/etc/nginx/conf.d/00-xpanel-tuning.conf

nginx -t
systemctl reload nginx

# ---------------------------------------------------------------- firewall
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  for p in 21 22 80 443 "$PANEL_PORT" 30000:30100; do ufw allow "$p/tcp"; done
fi
# Brute-force protection for SSH/SFTP and FTP (journal backend: Debian 12+
# has no /var/log/auth.log by default). Mail jails are added when the mail
# server is installed. Failure here never blocks the panel install.
if ! systemd-detect-virt --container -q 2>/dev/null; then
  if apt-get install -y -q fail2ban python3-systemd >/dev/null 2>&1; then
    install -m 0644 /dev/stdin /etc/fail2ban/jail.d/xpanel.conf <<'EOF'
# Managed by X-Panel
[DEFAULT]
backend = systemd
bantime = 1h
findtime = 10m
maxretry = 6
ignoreip = 127.0.0.1/8 ::1

[sshd]
enabled = true

[pure-ftpd]
enabled = true
EOF
    systemctl enable fail2ban >/dev/null 2>&1 || true
    if ! systemctl restart fail2ban 2>/dev/null; then
      # Older filters lack journal support for pure-ftpd: keep SSH protection.
      sed -i '/^\[pure-ftpd\]/,$d' /etc/fail2ban/jail.d/xpanel.conf
      systemctl restart fail2ban 2>/dev/null || warn "fail2ban chưa khởi động được (journalctl -u fail2ban)"
    fi
  else
    warn "Không cài được fail2ban"
  fi
fi

# ---------------------------------------------------------------- services
install -m 0644 "$SRC/deploy/systemd/xpanel-agent.service" "$SRC/deploy/systemd/xpanel-api.service" /etc/systemd/system/
systemctl daemon-reload

run_api() { runuser -u xpanel -- env $(grep -v '^#' "$ENV_FILE" | xargs) "$PREFIX/bin/xpanel-api" "$@"; }
run_api migrate

# ADMIN_PASSWORD=... ./install.sh sets the admin password (on first install
# or to reset it on an upgrade); otherwise a random one is generated once.
ADMIN_PASS="${ADMIN_PASSWORD:-}"
if [ -n "$ADMIN_PASS" ] && [ "${#ADMIN_PASS}" -lt 10 ]; then
  die "ADMIN_PASSWORD phải có ít nhất 10 ký tự."
fi
if [ "$FIRST_INSTALL" = 1 ]; then
  [ -n "$ADMIN_PASS" ] || ADMIN_PASS="$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | head -c 16)"
  run_api create-admin -username admin -password "$ADMIN_PASS"
elif [ -n "$ADMIN_PASS" ]; then
  run_api reset-password -username admin -password "$ADMIN_PASS"
fi
# The password is only shown on screen, never stored in a file (older
# versions kept /root/xpanel-admin.txt).
rm -f /root/xpanel-admin.txt

systemctl enable xpanel-agent xpanel-api
systemctl restart xpanel-agent xpanel-api

for _ in $(seq 1 30); do
  curl -sf -o /dev/null --unix-socket "$API_SOCK" http://xpanel/api/health && break
  sleep 0.5
done
curl -sf -o /dev/null --unix-socket "$API_SOCK" http://xpanel/api/health \
  || die "xpanel-api không khởi động được, xem: journalctl -u xpanel-api"

# The one-line installer turns on disk quotas here, so the login details
# below are the last thing on screen.
if [ "${XPANEL_ENABLE_QUOTA:-0}" = 1 ]; then
  "$SRC/enable-quota.sh" || warn "Chưa bật được quota tự động (/home cần ext4 hoặc xfs)."
fi
QUOTA_ON=1
findmnt -n -o OPTIONS --target /home | grep -qE '(^|,)(usrquota|quota|uquota|usrjquota=[^,]*)(,|$)' || QUOTA_ON=0

LOCAL_IP="$(hostname -I | awk '{print $1}')"
PUBLIC_IP="$(curl -4 -fsS -m 4 https://api.ipify.org 2>/dev/null | grep -E '^[0-9.]+$' || true)"
echo
printf '\033[1;32m%s\033[0m\n' "=================================================================="
printf '\033[1;32m  X-Panel đã cài xong\033[0m\n'
printf '\033[1;32m%s\033[0m\n' "=================================================================="
if [ -n "$PUBLIC_IP" ] && [ "$PUBLIC_IP" != "$LOCAL_IP" ]; then
  echo "  Địa chỉ   : https://$PUBLIC_IP:$PANEL_PORT"
  echo "              https://$LOCAL_IP:$PANEL_PORT   (mạng nội bộ)"
else
  echo "  Địa chỉ   : https://$LOCAL_IP:$PANEL_PORT"
fi
echo "  Tài khoản : admin"
if [ -n "$ADMIN_PASS" ]; then
  printf '  Mật khẩu  : \033[1m%s\033[0m\n' "$ADMIN_PASS"
  echo
  warn "Hãy lưu mật khẩu này ngay: nó chỉ hiển thị một lần và không được lưu trên máy chủ."
  echo "   Quên mật khẩu: chạy lại lệnh cài kèm ADMIN_PASSWORD='mật-khẩu-mới'."
else
  echo "  Mật khẩu  : giữ nguyên như trước (đã nâng cấp, dữ liệu và cấu hình được giữ nguyên)"
fi
printf '\033[1;32m%s\033[0m\n' "=================================================================="
warn "Chứng chỉ panel là self-signed, trình duyệt sẽ cảnh báo lần đầu (chọn Advanced → Proceed)."
if [ "$QUOTA_ON" = 0 ]; then
  warn "Quota ổ đĩa chưa bật trên /home: giới hạn dung lượng của gói chưa có hiệu lực."
  warn "Bật bằng: $PREFIX/bin/enable-quota.sh"
fi
