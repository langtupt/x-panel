#!/usr/bin/env bash
# X-Panel — gỡ cài đặt (đăng nhập root rồi dán):
#
#   curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | bash
#
# Mặc định XÓA SẠCH: panel và toàn bộ dữ liệu khách hàng (website, thư mục
# home, database, email, FTP, cron, chứng chỉ). Các gói phần mềm (nginx, PHP,
# MariaDB, PostgreSQL, Postfix, Dovecot…) được giữ lại.
#
# Tuỳ chọn (biến môi trường, đặt trước "bash"):
#   XPANEL_KEEP_DATA=1        giữ lại dữ liệu khách hàng (home, database, email);
#                             chỉ gỡ panel
#   XPANEL_PURGE_PACKAGES=1   gỡ luôn các gói phần mềm X-Panel đã cài
#   XPANEL_CONFIRM=XOA-SACH   không hỏi xác nhận (dùng cho script tự động)
set -euo pipefail

main() {

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx %s\033[0m\n' "$*" >&2; exit 1; }
q()    { "$@" >/dev/null 2>&1 || true; }

[ "$(id -u)" -eq 0 ] || die "Cần quyền root. Đăng nhập root (hoặc: sudo -i) rồi dán lại lệnh."
KEEP="${XPANEL_KEEP_DATA:-0}"
PURGE="${XPANEL_PURGE_PACKAGES:-0}"
[ "$KEEP" != 1 ] || [ "$PURGE" != 1 ] \
  || die "Không thể vừa giữ dữ liệu vừa purge các gói/database. Chỉ chọn một chế độ."

if [ "$PURGE" != 1 ] && [ ! -e /etc/xpanel ] && [ ! -e /opt/xpanel ] && ! systemctl cat xpanel-api >/dev/null 2>&1; then
  die "Không thấy X-Panel trên máy này."
fi

# ---------------------------------------------------------------- inventory
pgq() { (cd /tmp && runuser -u postgres -- psql -d xpanel -tAc "$1" 2>/dev/null) || true; }
HOSTING_USERS="$(pgq "SELECT username FROM users WHERE linux_uid IS NOT NULL ORDER BY username")"
if [ -z "$HOSTING_USERS" ]; then # panel database already gone: use the SFTP group
  HOSTING_USERS="$(getent group xpanel-sftp | cut -d: -f4 | tr ',' '\n' | grep -v '^$' || true)"
fi
MARIA_DBS="$(pgq "SELECT name || ' ' || db_user FROM databases WHERE engine='mariadb'")"
N_USERS=$(printf '%s\n' "$HOSTING_USERS" | grep -c . || true)
N_SITES=$(pgq "SELECT count(*) FROM sites"); N_SITES=${N_SITES:-?}
N_DBS=$(pgq "SELECT count(*) FROM databases"); N_DBS=${N_DBS:-?}
N_BOXES=$(pgq "SELECT count(*) FROM mailboxes"); N_BOXES=${N_BOXES:-?}

echo
printf '\033[1;31m%s\033[0m\n' "================= GỠ CÀI ĐẶT X-PANEL ================="
echo "  Panel, dịch vụ, cấu hình và database của panel sẽ bị xóa."
if [ "$KEEP" = 1 ]; then
  echo "  Dữ liệu khách hàng được GIỮ LẠI (XPANEL_KEEP_DATA=1)."
else
  echo "  XÓA VĨNH VIỄN dữ liệu khách hàng:"
  echo "    - $N_USERS tài khoản hosting (user Linux + thư mục /home/<user>)"
  echo "    - $N_SITES website, $N_DBS database, $N_BOXES hộp thư email (/var/mail/vhosts)"
  echo "  Không thể khôi phục. Hãy tải backup về trước nếu cần."
fi
if [ "$PURGE" = 1 ]; then
  echo "  Gỡ TOÀN BỘ các gói nginx, PHP, MariaDB, PostgreSQL, Pure-FTPd, Postfix, Dovecot, OpenDKIM, fail2ban."
  echo "  Xóa cả dữ liệu/cấu hình của các dịch vụ này, kể cả được cài ngoài X-Panel."
else
  echo "  Các gói nginx, PHP, MariaDB, PostgreSQL, FTP/mail vẫn được GIỮ LẠI."
  echo "  Muốn gỡ cả các gói: hủy rồi chạy lại với XPANEL_PURGE_PACKAGES=1."
fi
printf '\033[1;31m%s\033[0m\n' "======================================================"

if [ "${XPANEL_CONFIRM:-}" != "XOA-SACH" ]; then
  [ -r /dev/tty ] || die "Cần xác nhận: chạy lại với XPANEL_CONFIRM=XOA-SACH trước \"bash\"."
  printf 'Gõ  XOA SACH  để tiếp tục (Enter để hủy): '
  { read -r answer </dev/tty; } 2>/dev/null || answer=""
  [ "$answer" = "XOA SACH" ] || die "Đã hủy, không thay đổi gì."
fi

# ---------------------------------------------------------------- panel services
say "Dừng dịch vụ X-Panel"
q systemctl disable --now xpanel-api xpanel-agent
units=$(systemctl list-units --all --plain --no-legend 'xpanel-*' 2>/dev/null | awk '{print $1}')
for u in $units; do q systemctl stop "$u"; q systemctl disable "$u"; done
for u in $(systemctl list-unit-files --plain --no-legend 'xpanel-*' 2>/dev/null | awk '{print $1}'); do q systemctl disable "$u"; done
rm -f /etc/systemd/system/xpanel-*.service /etc/systemd/system/xpanel-*.timer /etc/systemd/system/xpanel-*.slice
rm -rf /etc/systemd/system/xpanel-*.d /etc/systemd/system.control/xpanel-* /run/systemd/system.control/xpanel-*
q systemctl daemon-reload
q systemctl reset-failed
q nft delete table inet xpanel_apps

# ---------------------------------------------------------------- customer data
if [ "$KEEP" != 1 ]; then
  say "Xóa database của khách hàng"
  printf '%s\n' "$MARIA_DBS" | while read -r db user; do
    [ -n "$db" ] || continue
    q mariadb -e "DROP DATABASE IF EXISTS \`$db\`; DROP USER IF EXISTS '$user'@'localhost';"
  done
  if command -v pg_lsclusters >/dev/null; then
    for ver in $(pg_lsclusters -h 2>/dev/null | awk '$2=="customers" {print $1}'); do
      q pg_dropcluster --stop "$ver" customers
    done
  fi

  say "Xóa tài khoản hosting và thư mục home"
  for u in $HOSTING_USERS; do
    case "$u" in root|xpanel|postgres|mysql|www-data|"") continue ;; esac
    id "$u" >/dev/null 2>&1 || continue
    q pkill -KILL -u "$u"

    q userdel -r "$u"
    rm -rf "/home/${u:?}"
  done

  say "Xóa email"
  rm -rf /var/mail/vhosts /etc/dovecot/xpanel-users /etc/postfix/xpanel /etc/opendkim/keys
  q userdel vmail
  rm -f /etc/pure-ftpd/pureftpd.passwd /etc/pure-ftpd/pureftpd.pdb
fi

# ---------------------------------------------------------------- panel files
say "Xóa panel, cấu hình và database của panel"
(cd /tmp && runuser -u postgres -- psql -c "DROP DATABASE IF EXISTS xpanel" >/dev/null 2>&1) || true
(cd /tmp && runuser -u postgres -- psql -c "DROP ROLE IF EXISTS xpanel" >/dev/null 2>&1) || true

rm -f /etc/nginx/conf.d/xpanel-panel.conf /etc/nginx/conf.d/xpanel-webmail.conf /etc/nginx/conf.d/xpanel-dbtools.conf \
      /etc/nginx/conf.d/00-xpanel-tuning.conf
rm -rf /etc/nginx/conf.d/xpanel /var/log/nginx/xpanel
for pool in /etc/php/*/fpm/pool.d/xpanel-*.conf; do [ -e "$pool" ] && rm -f "$pool"; done
rm -f /etc/php/*/fpm/conf.d/99-xpanel.ini
for v in $(ls /etc/php 2>/dev/null); do q systemctl reload-or-restart "php$v-fpm"; done
if [ "$KEEP" != 1 ]; then
  rm -rf /opt/xpanel /etc/xpanel /var/lib/xpanel /etc/xpanel-cron /etc/xpanel-site-apps /run/xpanel*
else
  # Keep site certificates and runtimes customers' apps may still use.
  rm -rf /opt/xpanel/bin /opt/xpanel/web /opt/xpanel/phpmyadmin /opt/xpanel/adminer /opt/xpanel/roundcube \
         /var/lib/xpanel/acme* /var/lib/xpanel/pma-tmp /var/lib/xpanel/webmail /var/lib/xpanel/backup-tmp \
         /etc/xpanel/xpanel.env /etc/xpanel/agent.env /etc/xpanel/nginx /run/xpanel /run/xpanel-api
fi
rm -f /usr/local/sbin/xpanel-uninstall /etc/logrotate.d/xpanel-nginx /etc/sysctl.d/90-xpanel.conf /etc/systemd/journald.conf.d/90-xpanel.conf \
      /etc/mysql/mariadb.conf.d/90-xpanel.cnf /etc/fail2ban/jail.d/xpanel.conf /etc/fail2ban/jail.d/xpanel-mail.conf \
      /etc/dovecot/conf.d/99-xpanel.conf /etc/systemd/system/opendkim.service.d/xpanel.conf /etc/apt/sources.list.d/xpanel-php.list \
      /var/log/xpanel-install.log
rm -f /etc/logrotate.d/xpanel-php
rm -rf /var/log/xpanel
[ -f /etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel ] && mv -f /etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel /etc/dovecot/conf.d/10-auth.conf

# SFTP chroot block in sshd_config.
if grep -q "BEGIN X-Panel" /etc/ssh/sshd_config 2>/dev/null; then
  sed -i '/^# BEGIN X-Panel/,/^# END X-Panel/d' /etc/ssh/sshd_config
  sshd -t && systemctl reload ssh
fi
q userdel xpanel-pma
q userdel xpanel-webmail
# Do not delete a pre-existing interactive account with the same name.
XPANEL_HOME=$(getent passwd xpanel | cut -d: -f6 || true)
if [ -z "$XPANEL_HOME" ] || [ "$XPANEL_HOME" = /var/lib/xpanel ]; then
  q userdel xpanel
  q groupdel xpanel
else
  warn "Giữ tài khoản xpanel có home $XPANEL_HOME (không phải tài khoản dịch vụ do installer tạo)."
fi
[ "$KEEP" = 1 ] || q groupdel xpanel-sftp

if [ "$PURGE" = 1 ]; then
  say "Gỡ các gói phần mềm"
  export DEBIAN_FRONTEND=noninteractive
  # Include rc packages: a previous remove can leave configuration behind.
  pkgs=$( { dpkg-query -W -f='${binary:Package} ${db:Status-Abbrev}\n' 'nginx*' 'libnginx-mod-*' 'php*' 'mariadb-*' 'postgresql*' 'pure-ftpd*' 'postfix*' \
         'dovecot-*' 'opendkim*' 'fail2ban' 'debsuryorg-archive-keyring' 2>/dev/null || true; } | awk '$2 ~ /^(ii|rc)/ {print $1}')
  if [ -n "$pkgs" ]; then
    apt-get purge -y -q $pkgs || die "APT purge thất bại. Khắc phục lỗi ở trên rồi chạy lại cùng XPANEL_PURGE_PACKAGES=1; chưa xóa thư mục database."
  fi
  apt-get autoremove --purge -y -q || die "APT autoremove thất bại. Có thể chạy lại cùng XPANEL_PURGE_PACKAGES=1."
  rm -rf /etc/php /etc/nginx /etc/mysql /var/lib/mysql /etc/postgresql /var/lib/postgresql /etc/dovecot /etc/postfix \
         /etc/opendkim /etc/pure-ftpd /opt/xpanel
  for f in /etc/apt/sources.list.d/*ondrej* /etc/apt/sources.list.d/*pgdg*; do [ -e "$f" ] && rm -f "$f"; done
  rm -f /usr/local/bin/composer
  rm -f /etc/ssl/private/pure-ftpd.pem
else
  q nginx -t && q systemctl reload nginx
  q systemctl restart mariadb
  [ -e /etc/dovecot ] && q systemctl restart dovecot
  [ -e /etc/postfix/main.cf ] && q postconf -e "virtual_mailbox_domains=" "virtual_mailbox_maps=" "virtual_alias_maps=" \
    "smtpd_sender_login_maps=" "smtpd_milters=" "non_smtpd_milters=" "smtpd_sender_restrictions=" && q systemctl restart postfix
  q systemctl restart fail2ban
fi
q systemctl daemon-reload
q sysctl --system

echo
printf '\033[1;32m%s\033[0m\n' "Đã gỡ X-Panel."
[ "$KEEP" = 1 ] && echo "Dữ liệu khách hàng vẫn còn trong /home, MariaDB/PostgreSQL và /var/mail/vhosts."
if [ "$PURGE" = 1 ]; then
  echo "Đã purge các gói dịch vụ hosting và xóa thư mục dữ liệu/cấu hình của chúng."
else
  echo "Các gói nginx, PHP, MariaDB… vẫn còn. Chạy dọn tiếp bằng:"
  echo "curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | XPANEL_PURGE_PACKAGES=1 bash"
fi
echo "Swap /swapfile (nếu installer đã tạo) và quota ổ đĩa được giữ nguyên."
}

main "$@"
