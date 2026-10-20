#!/usr/bin/env bash
# Sourced by install.sh. Conservative shared-hosting defaults, not capacity guarantees.
xpanel_profile() {
  local ram=$1 cpu=$2
  [[ "$ram" =~ ^[0-9]+$ && "$cpu" =~ ^[0-9]+$ ]] && (( ram >= 512 && cpu >= 1 )) || return 1
  T_RAM=$ram T_CPU=$cpu
  T_MY_POOL=$((ram / 8)); ((T_MY_POOL >= 64)) || T_MY_POOL=64
  ((T_MY_POOL <= 4096)) || T_MY_POOL=4096
  T_MY_CONNECTIONS=$((ram / 64)); ((T_MY_CONNECTIONS >= 16)) || T_MY_CONNECTIONS=16
  ((T_MY_CONNECTIONS <= cpu * 32)) || T_MY_CONNECTIONS=$((cpu * 32))
  ((T_MY_CONNECTIONS <= 200)) || T_MY_CONNECTIONS=200
  T_PG_SHARED=$((ram / 32)); ((T_PG_SHARED >= 16)) || T_PG_SHARED=16
  ((T_PG_SHARED <= 256)) || T_PG_SHARED=256
  T_PG_CONNECTIONS=$((24 + cpu * 4)); ((T_PG_CONNECTIONS <= 96)) || T_PG_CONNECTIONS=96
  local pg_ram_cap=$((ram / 64)); ((pg_ram_cap >= 12)) || pg_ram_cap=12
  ((T_PG_CONNECTIONS <= pg_ram_cap)) || T_PG_CONNECTIONS=$pg_ram_cap
  T_NGINX_CONNECTIONS=2048
  ((ram < 2048)) || T_NGINX_CONNECTIONS=4096
  ((ram < 8192)) || T_NGINX_CONNECTIONS=8192
  T_OPCACHE=32; ((ram < 8192)) || T_OPCACHE=64
  # All calculations reserve most RAM for the OS, applications and DB overhead.
}

xpanel_tune_kernel() {
  # Never lower a vendor's higher network queue limits.
  local accept syn
  accept=$(sysctl -n net.core.somaxconn); syn=$(sysctl -n net.ipv4.tcp_max_syn_backlog)
  ((accept >= 4096)) || accept=4096
  ((syn >= 8192)) || syn=8192
  install -m 0644 /dev/stdin /etc/sysctl.d/90-xpanel.conf <<EOF
# Managed by X-Panel
vm.swappiness = 10
net.core.somaxconn = $accept
net.ipv4.tcp_max_syn_backlog = $syn
fs.inotify.max_user_watches = 524288
EOF
  sysctl -q -p /etc/sysctl.d/90-xpanel.conf || warn "Some sysctl settings could not be applied. Check the VPS restrictions."
}

xpanel_tune_mariadb() {
  local conf=/etc/mysql/mariadb.conf.d/90-xpanel.cnf backup
  backup=$(mktemp)
  [ ! -f "$conf" ] || cp -p "$conf" "$backup"
  local existed=0; [ ! -f "$conf" ] || existed=1
  install -d -m 0755 /var/log/mysql
  install -d -m 0750 -o mysql -g adm /var/log/mysql/xpanel
  touch /var/log/mysql/xpanel/slow.log
  chown mysql:adm /var/log/mysql/xpanel/slow.log
  chmod 0640 /var/log/mysql/xpanel/slow.log
  install -m 0644 /dev/stdin "$conf" <<EOF
# Managed by X-Panel (shared hosting, ${T_RAM}MB / ${T_CPU} CPU)
[mysqld]
bind-address = 127.0.0.1
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci
innodb_buffer_pool_size = ${T_MY_POOL}M
max_connections = $T_MY_CONNECTIONS
thread_cache_size = 16
tmp_table_size = 8M
max_heap_table_size = 8M
slow_query_log = ON
slow_query_log_file = /var/log/mysql/xpanel/slow.log
long_query_time = 2
log_queries_not_using_indexes = OFF
EOF
  # MariaDB's default slow log stays in its root/mysql-owned data directory.
  if ! mariadbd --verbose --help >/dev/null 2>&1 || ! systemctl restart mariadb; then
    if [ "$existed" = 1 ]; then cp -p "$backup" "$conf"; else rm -f "$conf"; fi
    rm -f "$backup"
    systemctl restart mariadb || true
    die "Invalid MariaDB configuration. The previous configuration has been restored."
  fi
  rm -f "$backup"
  cat >/etc/logrotate.d/xpanel-mariadb <<'EOF'
/var/log/mysql/xpanel/slow.log {
    daily
    maxsize 20M
    rotate 7
    missingok
    notifempty
    compress
    delaycompress
    create 0640 mysql adm
    sharedscripts
    postrotate
        /usr/bin/mariadb -e 'FLUSH SLOW LOGS' >/dev/null 2>&1 || true
    endscript
}
EOF
}

xpanel_tune_php() {
  local ini ver backup existed
  for ini in /etc/php/*/fpm/conf.d; do
    [ -d "$ini" ] || continue
    ver=${ini#/etc/php/}; ver=${ver%%/*}
    backup=$(mktemp); existed=0
    if [ -f "$ini/98-xpanel-performance.ini" ]; then cp -p "$ini/98-xpanel-performance.ini" "$backup"; existed=1; fi
    cat >"$ini/98-xpanel-performance.ini" <<EOF
; Managed by X-Panel; 99-xpanel.ini admin settings take precedence.
opcache.enable=1
opcache.memory_consumption=$T_OPCACHE
opcache.interned_strings_buffer=4
opcache.max_accelerated_files=10000
opcache.validate_timestamps=1
opcache.revalidate_freq=2
EOF
    if ! "/usr/sbin/php-fpm$ver" -t >/dev/null 2>&1; then
      if [ "$existed" = 1 ]; then cp -p "$backup" "$ini/98-xpanel-performance.ini"; else rm -f "$ini/98-xpanel-performance.ini"; fi
      rm -f "$backup"; die "PHP $ver configuration validation failed. The previous INI has been restored."
    fi
    rm -f "$backup"
    # Includes masters generated for individual accounts and sites.
    while read -r unit rest; do
      case "$unit" in
        "php$ver-fpm.service"|"xpanel-php$ver-"*|xpanel-app-*)
          if systemctl show "$unit" -p ExecStart --value | grep -q "php-fpm$ver"; then
            systemctl kill --kill-whom=main -s USR2 "$unit"
          fi ;;
      esac
    done < <(systemctl list-units --type=service --state=running --no-legend --plain)
  done
}

xpanel_tune_postgres() {
  local ver cluster port status owner rest dir conf backup existed target=${1:-}
  while read -r ver cluster port status owner rest; do
    # Panel on 5432; the customer's separate cluster on 5433 also shares the host.
    [[ "$ver" =~ ^[0-9]+$ ]] || continue
    [[ "$cluster:$port" = main:5432 || "$cluster:$port" = customers:5433 ]] || continue
    [ -z "$target" ] || [ "$cluster" = "$target" ] || continue
    dir="/etc/postgresql/$ver/$cluster/conf.d"
    install -d -m 0755 "$dir"
    conf="$dir/90-xpanel-performance.conf"; backup=$(mktemp); existed=0
    if [ -f "$conf" ]; then cp -p "$conf" "$backup"; existed=1; fi
    cat >"$conf" <<EOF
# Managed by X-Panel: per-cluster budget, leaves RAM for MariaDB and apps.
shared_buffers = '${T_PG_SHARED}MB'
work_mem = '2MB'
maintenance_work_mem = '32MB'
max_connections = $T_PG_CONNECTIONS
max_parallel_workers_per_gather = 0
log_min_duration_statement = 2000
EOF
    chmod 0644 "$conf"
    if ! (cd /tmp && runuser -u postgres -- "/usr/lib/postgresql/$ver/bin/postgres" -D "/var/lib/postgresql/$ver/$cluster" -c "config_file=/etc/postgresql/$ver/$cluster/postgresql.conf" -C shared_buffers >/dev/null) \
        || ! systemctl restart "postgresql@$ver-$cluster"; then
      if [ "$existed" = 1 ]; then cp -p "$backup" "$conf"; else rm -f "$conf"; fi
      rm -f "$backup"
      systemctl restart "postgresql@$ver-$cluster" || true
      die "Unable to apply PostgreSQL settings. The previous configuration has been restored."
    fi
    rm -f "$backup"
  done < <(pg_lsclusters -h)
}

xpanel_tune_nginx() {
  local original=/etc/nginx/nginx.conf.xpanel-before-tuning tmp previous
  [ -f "$original" ] || cp -p /etc/nginx/nginx.conf "$original"
  previous=$(mktemp); cp -p /etc/nginx/nginx.conf "$previous"
  tmp=$(mktemp)
  # Debian/Ubuntu's events block is in nginx.conf. Do not touch other directives.
  awk -v connections="$T_NGINX_CONNECTIONS" '
    /^[[:space:]]*events[[:space:]]*\{/ { events=1 }
    events && /^[[:space:]]*worker_connections[[:space:]]/ { current=$2+0; if (current < connections) sub(/worker_connections[[:space:]]+[0-9]+/, "worker_connections " connections); found=1 }
    events && /}/ { events=0 }
    { print }
    END { if (!found) exit 1 }
  ' /etc/nginx/nginx.conf >"$tmp" || { rm -f "$tmp" "$previous"; die "events.worker_connections was not found. nginx.conf has not been changed."; }
  cat "$tmp" >/etc/nginx/nginx.conf; rm -f "$tmp"
  # Main context include on supported distro packages.
  if ! grep -qE '^[[:space:]]*worker_rlimit_nofile[[:space:]]' /etc/nginx/nginx.conf; then
    printf 'worker_rlimit_nofile 65536;\n' >/etc/nginx/modules-enabled/99-xpanel-performance.conf
  else
    rm -f /etc/nginx/modules-enabled/99-xpanel-performance.conf
  fi
  install -d /etc/systemd/system/nginx.service.d
  printf '[Service]\nLimitNOFILE=65536\n' >/etc/systemd/system/nginx.service.d/xpanel-performance.conf
  if ! nginx -t; then
    cp -p "$previous" /etc/nginx/nginx.conf
    rm -f /etc/nginx/modules-enabled/99-xpanel-performance.conf
    rm -f "$previous"
    die "nginx configuration validation failed. nginx.conf has been restored."
  fi
  systemctl daemon-reload
  # Apply the service descriptor limit as well as the nginx worker configuration.
  if ! systemctl restart nginx; then
    cp -p "$previous" /etc/nginx/nginx.conf
    rm -f /etc/nginx/modules-enabled/99-xpanel-performance.conf
    rm -f "$previous"
    systemctl restart nginx || true
    die "Unable to start nginx. nginx.conf has been restored."
  fi
  rm -f "$previous"
}

xpanel_tuning_summary() {
  install -d -m 0755 /var/lib/xpanel
  cat >/var/lib/xpanel/performance-profile.txt <<EOF
RAM_MB=$T_RAM
CPU=$T_CPU
MARIADB_BUFFER_MB=$T_MY_POOL
MARIADB_MAX_CONNECTIONS=$T_MY_CONNECTIONS
POSTGRES_SHARED_MB_PER_CLUSTER=$T_PG_SHARED
POSTGRES_MAX_CONNECTIONS_PER_CLUSTER=$T_PG_CONNECTIONS
NGINX_WORKER_CONNECTIONS=$T_NGINX_CONNECTIONS
PHP_OPCACHE_DEFAULT_MB=$T_OPCACHE
EOF
  log "Auto-tune: ${T_CPU} CPU / ${T_RAM}MB; nginx $T_NGINX_CONNECTIONS connections/worker; MariaDB ${T_MY_POOL}MB, $T_MY_CONNECTIONS connections; PostgreSQL ${T_PG_SHARED}MB/cluster."
}

# The agent uses this after installing a customer's PostgreSQL cluster later.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  [ "${1:-}" = --postgres ] || { echo 'Usage: tuning.sh --postgres' >&2; exit 2; }
  set -euo pipefail
  warn() { printf '%s\n' "$*" >&2; }
  die() { warn "$*"; exit 1; }
  xpanel_profile "$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)" "$(nproc)"
  xpanel_tune_postgres customers
fi
