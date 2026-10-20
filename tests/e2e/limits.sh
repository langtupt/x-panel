#!/usr/bin/env bash
# Package CPU/RAM limits: every PHP site runs its own PHP-FPM service
# (xpanel-app-<user>-<hash>) inside the account's slice xpanel-<user>.slice.
# Needs PHP 8.3 (installed by web.sh / applications.sh).
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"})
  echo "$code $(head -c 140 /tmp/out)"; }
step() { echo; echo "### $*"; }
web() { curl -s -o /tmp/body -w '%{http_code}' -H "Host: $1" "http://127.0.0.1${2:-/}"; echo " $(head -c 100 /tmp/body | tr '\n' ' ')"; }
unit() { systemctl list-units --type=service --all --no-legend 'xpanel-app-lcust-*' | awk '{print $1}' | head -1; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "lcust" "Small CPU/RAM *" "Big *"
c admin POST /packages '{"name":"Small CPU/RAM '"$(date +%s)"'","kind":"user","disk_mb":500,"max_sites":2,"cpu_pct":50,"mem_mb":128}' >/dev/null; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"lcust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /sites "{\"user_id\":$U,\"domain\":\"limits.example.com\",\"aliases\":[],\"app_type\":\"php\",\"php_version\":\"8.3\",\"doc_root\":\"\"}" | cut -c1-60
UNIT=$(unit)

step "own PHP-FPM service inside xpanel-lcust.slice"
systemctl is-active "$UNIT"
systemctl show "$UNIT" -p ControlGroup --value | grep -o "xpanel-lcust.slice/.*"
systemctl show xpanel-lcust.slice -p CPUQuotaPerSecUSec -p MemoryMax | tr '\n' ' '; echo
grep -h "^pm.max_children" /etc/xpanel-site-apps/lcust/*.fpm
systemctl show "$UNIT" -p OOMPolicy --value | sed 's/^/OOMPolicy=/'
ls /etc/php/8.3/fpm/pool.d/ | tr '\n' ' '; echo "(no customer pools in the shared service)"

step "PHP works; a request needing more than 128 MB RAM is killed, the site survives"
cat > /home/lcust/www/limits.example.com/ok.php <<'PHP'
<?php echo "ok as ", posix_getpwuid(posix_geteuid())["name"];
PHP
cat > /home/lcust/www/limits.example.com/hog.php <<'PHP'
<?php ini_set("memory_limit", "-1"); $a = str_repeat("x", 300 * 1024 * 1024); echo strlen($a);
PHP
chown lcust:lcust /home/lcust/www/limits.example.com/*.php
web limits.example.com /ok.php
web limits.example.com /hog.php
web limits.example.com /ok.php

step "raise limits in the package -> slice updated live"
c admin PUT /packages/$P "{\"name\":\"Big $(date +%s)\",\"disk_mb\":500,\"max_sites\":2,\"cpu_pct\":0,\"mem_mb\":0}" | cut -c1-40
systemctl show xpanel-lcust.slice -p CPUQuotaPerSecUSec -p MemoryMax | tr '\n' ' '; echo
web limits.example.com /hog.php

step "suspend stops the service; activate starts it again"
c admin PATCH /users/$U/status '{"status":"suspended"}' >/dev/null; systemctl is-active "$UNIT"
c admin PATCH /users/$U/status '{"status":"active"}' >/dev/null;    systemctl is-active "$UNIT"

step "delete account -> service, config and slice limits removed"
c admin DELETE /users/$U
systemctl is-active "$UNIT"; ls /etc/systemd/system/"$UNIT" 2>&1 | tail -1
ls -d /etc/systemd/system.control/xpanel-lcust.slice.d 2>&1 | tail -1
