#!/usr/bin/env bash
# X-Panel uninstaller. Run as root:
# curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | bash
# Default: remove everything X-Panel installed - panel, customer data and the
# hosting service packages (nginx, PHP, databases, mail, FTP, ClamAV...) with
# their data and configuration.
# Options (environment variables before bash):
# XPANEL_KEEP_PACKAGES=1   Remove panel and customer data; retain service packages.
# XPANEL_KEEP_DATA=1       Remove panel while retaining customer data (and packages).
# XPANEL_PURGE_PACKAGES=1  Accepted for compatibility (purging is the default).
# XPANEL_CONFIRM=DELETE-ALL  Skip interactive confirmation for automation.
# Legacy XOA-SACH / XOA SACH confirmations remain accepted for compatibility.
set -euo pipefail

main() {

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx %s\033[0m\n' "$*" >&2; exit 1; }
q()    { "$@" >/dev/null 2>&1 || true; }

[ "$(id -u)" -eq 0 ] || die "Root access is required. Log in as root (or run sudo -i), then run the command again."
KEEP="${XPANEL_KEEP_DATA:-0}"
KEEP_PKGS="${XPANEL_KEEP_PACKAGES:-0}"
FORCE_PURGE="${XPANEL_PURGE_PACKAGES:-}"
[ "$FORCE_PURGE" = 0 ] && KEEP_PKGS=1
[ "$FORCE_PURGE" != 1 ] || { [ "$KEEP" != 1 ] && [ "$KEEP_PKGS" != 1 ]; } \
  || die "Retaining data or packages cannot be combined with XPANEL_PURGE_PACKAGES=1. Select one mode only."
# Purging deletes the database directories, so retaining data retains packages.
PURGE=1
if [ "$KEEP" = 1 ] || [ "$KEEP_PKGS" = 1 ]; then PURGE=0; fi

# Only an explicit XPANEL_PURGE_PACKAGES=1 cleans up a server without X-Panel
# (leftovers of an interrupted uninstall): never purge a foreign server by default.
if [ "$FORCE_PURGE" != 1 ] && [ ! -e /etc/xpanel ] && [ ! -e /opt/xpanel ] && ! systemctl cat xpanel-api >/dev/null 2>&1; then
  die "No X-Panel installation was found on this server."
fi

# ---------------------------------------------------------------- inventory
pgq() { (cd /tmp && runuser -u postgres -- psql -d xpanel -tAc "$1" 2>/dev/null) || true; }
HOSTING_USERS="$(pgq "SELECT username FROM users WHERE linux_uid IS NOT NULL ORDER BY username")"
if [ -z "$HOSTING_USERS" ]; then # panel database already gone: use the SFTP group
  HOSTING_USERS="$(getent group xpanel-sftp | cut -d: -f4 | tr ',' '\n' | grep -v '^$' || true)"
fi
MARIA_DBS="$(pgq "SELECT name || ' ' || db_user FROM databases WHERE engine='mariadb'")"
# Host changes made by the installer (swap file, quota mount options).
HOST_CHANGES="$(cat /etc/xpanel/host-changes /etc/xpanel-host-changes 2>/dev/null || true)"
N_USERS=$(printf '%s\n' "$HOSTING_USERS" | grep -c . || true)
N_SITES=$(pgq "SELECT count(*) FROM sites"); N_SITES=${N_SITES:-?}
N_DBS=$(pgq "SELECT count(*) FROM databases"); N_DBS=${N_DBS:-?}
N_BOXES=$(pgq "SELECT count(*) FROM mailboxes"); N_BOXES=${N_BOXES:-?}

echo
printf '\033[1;31m%s\033[0m\n' "================= UNINSTALL X-PANEL ================="
echo "  The panel, its services, configuration and panel database will be removed."
if [ "$KEEP" = 1 ]; then
  echo "  Customer data will be RETAINED (XPANEL_KEEP_DATA=1)."
else
  echo "  The following customer data will be PERMANENTLY DELETED:"
  echo "    - $N_USERS hosting accounts (Linux users and /home/<user> directories)"
  echo "    - $N_SITES websites, $N_DBS databases, $N_BOXES mailboxes (/var/mail/vhosts)"
  echo "  This action cannot be undone. Download any required backups before continuing."
fi
if [ "$PURGE" = 1 ]; then
  echo "  PURGE nginx, PHP, MariaDB, PostgreSQL, Pure-FTPd, Postfix, Dovecot, OpenDKIM, fail2ban, ClamAV, Redis,"
  echo "  Memcached, Node.js and quota packages. Their data and configuration will also be deleted, including"
  echo "  services installed outside X-Panel. The swap file and quota options added by the installer are removed."
else
  echo "  nginx, PHP, MariaDB, PostgreSQL and FTP/mail packages will be RETAINED."
  echo "  To purge them as well, cancel and run again without XPANEL_KEEP_PACKAGES / XPANEL_KEEP_DATA."
fi
printf '\033[1;31m%s\033[0m\n' "======================================================"

if [ "${XPANEL_CONFIRM:-}" != "DELETE-ALL" ] && [ "${XPANEL_CONFIRM:-}" != "XOA-SACH" ]; then
  [ -r /dev/tty ] || die "Confirmation is required: run again with XPANEL_CONFIRM=DELETE-ALL before \"bash\"."
  printf 'Type DELETE ALL to continue (press Enter to cancel): '
  { read -r answer </dev/tty; } 2>/dev/null || answer=""
  [ "$answer" = "DELETE ALL" ] || [ "$answer" = "XOA SACH" ] || die "Cancelled. No changes were made."
fi

# ---------------------------------------------------------------- panel services
say "Stopping X-Panel services"
q systemctl disable --now xpanel-api xpanel-agent
units=$(systemctl list-units --all --plain --no-legend 'xpanel-*' 2>/dev/null | awk '{print $1}')
for u in $units; do q systemctl stop "$u"; q systemctl disable "$u"; done
for u in $(systemctl list-unit-files --plain --no-legend 'xpanel-*' 2>/dev/null | awk '{print $1}'); do q systemctl disable "$u"; done
rm -f /etc/systemd/system/xpanel-*.service /etc/systemd/system/xpanel-*.timer /etc/systemd/system/xpanel-*.slice
rm -rf /etc/systemd/system/xpanel-*.d /etc/systemd/system.control/xpanel-* /run/systemd/system.control/xpanel-*
q systemctl daemon-reload
q systemctl reset-failed
q nft delete table inet xpanel_apps
q nft delete table inet xpanel_fw
# Parent of the per-account slices; transient, but stop it now.
q systemctl stop xpanel.slice

# ---------------------------------------------------------------- customer data
if [ "$KEEP" != 1 ]; then
  say "Removing customer databases"
  printf '%s\n' "$MARIA_DBS" | while read -r db user; do
    [ -n "$db" ] || continue
    q mariadb -e "DROP DATABASE IF EXISTS \`$db\`; DROP USER IF EXISTS '$user'@'localhost';"
  done
  if command -v pg_lsclusters >/dev/null; then
    for ver in $(pg_lsclusters -h 2>/dev/null | awk '$2=="customers" {print $1}'); do
      q pg_dropcluster --stop "$ver" customers
    done
  fi

  say "Removing hosting accounts and home directories"
  for u in $HOSTING_USERS; do
    case "$u" in root|xpanel|postgres|mysql|www-data|"") continue ;; esac
    id "$u" >/dev/null 2>&1 || continue
    q pkill -KILL -u "$u"

    q userdel -r "$u"
    # nginx is a member of each account's group, so userdel leaves it behind.
    q groupdel "$u"
    rm -rf "/home/${u:?}"
  done
  # Groups of accounts removed by older versions: userdel keeps a group that
  # nginx is a member of. Only groups nobody uses as primary group.
  for g in $(awk -F: '$3>=1000 && $3<65534 && (","$4",") ~ /,www-data,/ {print $1":"$3}' /etc/group); do
    awk -F: -v gid="${g#*:}" '$4==gid {found=1} END {exit !found}' /etc/passwd && continue
    q groupdel "${g%%:*}"
  done

  say "Removing customer email data"
  rm -rf /var/mail/vhosts /etc/dovecot/xpanel-users /etc/postfix/xpanel /etc/opendkim/keys
  q userdel vmail
  rm -f /etc/pure-ftpd/pureftpd.passwd /etc/pure-ftpd/pureftpd.pdb
fi

# ---------------------------------------------------------------- panel files
say "Removing panel files, configuration and panel database"
(cd /tmp && runuser -u postgres -- psql -c "DROP DATABASE IF EXISTS xpanel" >/dev/null 2>&1) || true
(cd /tmp && runuser -u postgres -- psql -c "DROP ROLE IF EXISTS xpanel" >/dev/null 2>&1) || true

rm -f /etc/nginx/conf.d/xpanel-panel.conf /etc/nginx/conf.d/xpanel-webmail.conf /etc/nginx/conf.d/xpanel-dbtools.conf \
      /etc/nginx/conf.d/00-xpanel-tuning.conf /etc/nginx/conf.d/xpanel-ratelimit.conf /etc/nginx/conf.d/xpanel-default.conf
# Give the package its default site back when nginx stays installed.
if [ -f /etc/nginx/sites-available/default ] && [ ! -e /etc/nginx/sites-enabled/default ]; then
  ln -s /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default
fi
rm -rf /etc/nginx/conf.d/xpanel /var/log/nginx/xpanel
for pool in /etc/php/*/fpm/pool.d/xpanel-*.conf; do [ -e "$pool" ] && rm -f "$pool"; done
rm -f /etc/php/*/fpm/conf.d/99-xpanel.ini
rm -f /etc/php/*/fpm/conf.d/98-xpanel-performance.ini
rm -f /etc/postgresql/*/*/conf.d/90-xpanel-performance.conf
for v in $(ls /etc/php 2>/dev/null); do q systemctl reload-or-restart "php$v-fpm"; done
# Remember the host changes for a later XPANEL_PURGE_PACKAGES=1 run.
if [ "$PURGE" != 1 ] && [ -n "$HOST_CHANGES" ]; then
  echo "$HOST_CHANGES" >/etc/xpanel-host-changes
fi
if [ "$KEEP" != 1 ]; then
  rm -rf /opt/xpanel /etc/xpanel /var/lib/xpanel /etc/xpanel-cron /etc/xpanel-site-apps /run/xpanel*
else
  # Keep site certificates and runtimes customers' apps may still use.
  rm -rf /opt/xpanel/bin /opt/xpanel/web /opt/xpanel/phpmyadmin /opt/xpanel/adminer /opt/xpanel/roundcube \
         /var/lib/xpanel/acme* /var/lib/xpanel/pma-tmp /var/lib/xpanel/webmail /var/lib/xpanel/backup-tmp \
         /etc/xpanel/xpanel.env /etc/xpanel/agent.env /etc/xpanel/nginx /run/xpanel /run/xpanel-api \
         /etc/xpanel/firewall.json /var/lib/xpanel/quarantine /var/lib/xpanel/bans
fi
rm -f /usr/local/sbin/xpanel-uninstall /etc/logrotate.d/xpanel-nginx /etc/sysctl.d/90-xpanel.conf /etc/systemd/journald.conf.d/90-xpanel.conf \
      /etc/mysql/mariadb.conf.d/90-xpanel.cnf /etc/fail2ban/jail.d/xpanel.conf /etc/fail2ban/jail.d/xpanel-mail.conf \
      /etc/fail2ban/jail.d/xpanel-firewall.local /etc/fail2ban/action.d/xpanel.conf \
      /etc/dovecot/conf.d/99-xpanel.conf /etc/systemd/system/opendkim.service.d/xpanel.conf /etc/apt/sources.list.d/xpanel-php.list \
      /var/log/xpanel-install.log /var/log/xpanel-update.log
rm -f /etc/logrotate.d/xpanel-php
rm -f /etc/logrotate.d/xpanel-mariadb
rm -rf /var/log/mysql/xpanel
rm -f /etc/nginx/modules-enabled/99-xpanel-performance.conf
rm -f /etc/systemd/system/nginx.service.d/xpanel-performance.conf
# Restore only the worker_connections value we manage; preserve later nginx edits.
if [ -f /etc/nginx/nginx.conf.xpanel-before-tuning ] && [ -f /etc/nginx/nginx.conf ]; then
  old_connections=$(awk '/^[[:space:]]*worker_connections[[:space:]]/ {gsub(";", "", $2); print $2; exit}' /etc/nginx/nginx.conf.xpanel-before-tuning)
  if [[ "$old_connections" =~ ^[0-9]+$ ]]; then
    sed -i -E "s/^([[:space:]]*worker_connections[[:space:]]+)[0-9]+/\1$old_connections/" /etc/nginx/nginx.conf
  fi
  rm -f /etc/nginx/nginx.conf.xpanel-before-tuning
fi
rm -rf /var/log/xpanel
[ -f /etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel ] && mv -f /etc/dovecot/conf.d/10-auth.conf.dpkg-xpanel /etc/dovecot/conf.d/10-auth.conf

# SFTP chroot block in sshd_config.
if grep -q "BEGIN X-Panel" /etc/ssh/sshd_config 2>/dev/null; then
  sed -i '/^# BEGIN X-Panel/,/^# END X-Panel/d' /etc/ssh/sshd_config
  # Ubuntu 24.04 starts sshd by socket activation: ssh.service may be
  # inactive and a reload then fails, which must not abort the uninstall.
  if sshd -t; then
    systemctl try-reload-or-restart ssh 2>/dev/null || true
  else
    warn "Invalid sshd_config after removing the X-Panel block. Run: sshd -t"
  fi
fi
q userdel xpanel-pma
q userdel xpanel-webmail
# Do not delete a pre-existing interactive account with the same name.
XPANEL_HOME=$(getent passwd xpanel | cut -d: -f6 || true)
if [ -z "$XPANEL_HOME" ] || [ "$XPANEL_HOME" = /var/lib/xpanel ]; then
  q userdel xpanel
  q groupdel xpanel
else
  warn "Preserving the existing xpanel account with home $XPANEL_HOME; it was not created as an installer service account."
fi
[ "$KEEP" = 1 ] || q groupdel xpanel-sftp

if [ "$PURGE" = 1 ]; then
  say "Reverting host changes made by the installer"
  while read -r kind a b; do
    case "$kind" in
      quota)
        q quotaoff -u "$a"
        awk -v mp="$a" -v opt="$b" 'BEGIN { OFS = "\t" }
          $1 !~ /^#/ && $2 == mp { n = split($4, o, ","); s = ""
            for (i = 1; i <= n; i++) if (o[i] != opt) s = s (s == "" ? "" : ",") o[i]
            $4 = (s == "" ? "defaults" : s) }
          { print }' /etc/fstab >/etc/fstab.xpanel
        if findmnt --verify --tab-file /etc/fstab.xpanel >/dev/null 2>&1; then
          mv /etc/fstab.xpanel /etc/fstab
          rm -f "${a%/}/aquota.user"
        else
          rm -f /etc/fstab.xpanel
          warn "Kept the $b option in /etc/fstab: the edited file did not verify."
        fi ;;
      grub)
        if [ "$a" = rootflags=uquota ] && [ -f /etc/default/grub ]; then
          sed -i 's/rootflags=uquota //' /etc/default/grub && q update-grub
        fi ;;
      swap)
        [ "$a" = /swapfile ] || continue
        q swapoff /swapfile
        awk '!($1 == "/swapfile" && $3 == "swap")' /etc/fstab >/etc/fstab.xpanel && mv /etc/fstab.xpanel /etc/fstab
        rm -f /swapfile ;;
    esac
  done <<<"$HOST_CHANGES"
  rm -f /etc/xpanel-host-changes

  say "Purging hosting service packages"
  export DEBIAN_FRONTEND=noninteractive
  # Include rc packages: a previous remove can leave configuration behind.
  pkgs=$( { dpkg-query -W -f='${binary:Package} ${db:Status-Abbrev}\n' 'nginx*' 'libnginx-mod-*' 'php*' 'mariadb-*' 'postgresql*' 'pure-ftpd*' 'postfix*' \
         'dovecot-*' 'opendkim*' 'fail2ban' 'clamav*' 'libclamav*' 'redis-server' 'redis-tools' 'memcached' \
         'python3-systemd' 'quota' 'nodejs' 'debsuryorg-archive-keyring' 2>/dev/null || true; } | awk '$2 ~ /^(ii|rc)/ {print $1}')
  if [ -n "$pkgs" ]; then
    apt-get purge -y -q $pkgs || die "APT purge failed. Resolve the errors above and rerun with XPANEL_PURGE_PACKAGES=1. Database directories have not been deleted."
  fi
  apt-get autoremove --purge -y -q || die "APT autoremove failed. Resolve the issue and rerun with XPANEL_PURGE_PACKAGES=1."
  rm -rf /etc/php /etc/nginx /etc/mysql /var/lib/mysql /etc/postgresql /var/lib/postgresql /etc/dovecot /etc/postfix \
         /etc/opendkim /etc/pure-ftpd /etc/clamav /var/lib/clamav /etc/redis /var/lib/redis /opt/xpanel
  for f in /etc/apt/sources.list.d/*ondrej* /etc/apt/sources.list.d/*pgdg* /etc/apt/sources.list.d/nodesource* \
           /etc/apt/trusted.gpg.d/*ondrej* /etc/apt/keyrings/nodesource* /usr/share/keyrings/nodesource*; do
    [ -e "$f" ] && rm -f "$f"
  done
  rm -f /usr/local/bin/composer
  rm -f /etc/ssl/private/pure-ftpd.pem
  rm -rf /run/clamav /run/redis /run/mysqld /run/postgresql /run/php /var/log/clamav /var/log/redis /var/log/mysql /var/log/postgresql
  # nginx's default page; anything else in /var/www is kept.
  rm -f /var/www/html/index.nginx-debian.html
  rmdir /var/www/html /var/www 2>/dev/null || true
  # Package purges keep their system users; nothing owns files of theirs any more.
  for u in mysql postgres opendkim clamav redis memcache postfix dovecot dovenull; do q userdel "$u"; done
  for g in mysql postgres opendkim clamav redis memcache postfix postdrop dovecot dovenull; do q groupdel "$g"; done
  dpkg -s ssl-cert >/dev/null 2>&1 || q groupdel ssl-cert
  q systemctl stop system-postfix.slice system-postgresql.slice system-mariadb.slice
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
printf '\033[1;32m%s\033[0m\n' "X-Panel has been uninstalled."
[ "$KEEP" = 1 ] && echo "Customer data remains in /home, MariaDB/PostgreSQL and /var/mail/vhosts."
if [ "$PURGE" = 1 ]; then
  echo "Hosting service packages and their data/configuration directories have been removed."
else
  echo "nginx, PHP and database packages remain installed. To purge them, run:"
  echo "curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | XPANEL_PURGE_PACKAGES=1 bash"
fi
if [ "$PURGE" = 1 ]; then
  echo "The swap file and quota options added by the installer have been removed; SSH and network settings are unchanged."
else
  echo "Swap (/swapfile, if created by the installer) and disk quota settings have been retained."
fi
}

main "$@"
