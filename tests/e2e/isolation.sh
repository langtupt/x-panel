#!/usr/bin/env bash
# Cross-account isolation: account "isoa" actively tries to reach account
# "isob" (PHP-FPM sockets, app ports, files via nginx symlinks, processes,
# API objects). Every attempt must fail. Needs PHP 8.3 + Node (applications.sh).
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"}); echo "$code"; }
ok()   { echo "PASS $*"; }
bad()  { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || bad "$3: got $1, want $2"; }
wait_task() { for _ in $(seq 1 600); do st=$(curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$1" | jq -r .status); [ "$st" = done ] && return 0; [ "$st" = failed ] && { curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$1" | jq -r .log | tail -5; return 1; }; sleep 2; done; return 1; }
web() { curl -s -o /tmp/body -w '%{http_code}' -H "Host: $1" "http://127.0.0.1$2"; }
asphp() { runuser -u "$1" -- php8.3 -r "$2" 2>&1; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "isoa isob" "Iso *"
c admin POST /packages '{"name":"Iso '"$(date +%s)"'","kind":"user","disk_mb":2000,"max_sites":5,"cpu_pct":100,"mem_mb":1024}' >/dev/null; PK=$(jq .id /tmp/out)
for u in isoa isob; do
  c admin POST /users "{\"username\":\"$u\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$PK}" >/dev/null
  c "$u" POST /auth/login "{\"username\":\"$u\",\"password\":\"Client12345\"}" >/dev/null
done
A_ID=$(curl -sk -b /tmp/admin "${H[@]}" $B/api/users | jq '.users[]|select(.username=="isoa").id')
B_ID=$(curl -sk -b /tmp/admin "${H[@]}" $B/api/users | jq '.users[]|select(.username=="isob").id')

site() { # site <jar> <domain> <type>
  c "$1" POST /sites "{\"domain\":\"$2\",\"aliases\":[],\"app_type\":\"$3\",\"php_version\":\"8.3\",\"doc_root\":\"\",\"sample\":true,\"env\":{}}" >/dev/null
  local t; t=$(jq -r '.task.id // empty' /tmp/out); [ -n "$t" ] && wait_task "$t"; jq -r .id /tmp/out; }
A_PHP=$(site isoa a.iso.example.com php)
B_PHP=$(site isob b.iso.example.com php)
B_NODE=$(site isob bnode.iso.example.com nodejs)
expect "$(web b.iso.example.com /)" 200 "B's PHP site serves"
expect "$(web bnode.iso.example.com /)" 200 "B's Node site serves through nginx"

echo "### 1. PHP-FPM sockets"
B_SOCK=$(ls /run/xpanel-app-isob-*/fpm.sock | head -1)
stat -c 'B socket: %U:%G %a' "$B_SOCK"
r=$(asphp isoa "echo @stream_socket_client('unix://$B_SOCK') ? 'CONNECTED' : 'DENIED';"); expect "$r" DENIED "A cannot open B's PHP-FPM socket"
r=$(asphp isob "echo @stream_socket_client('unix://$B_SOCK') ? 'CONNECTED' : 'DENIED';"); expect "$r" CONNECTED "B can open its own socket"
[ -S /run/php/xpanel-pma.sock ] && { r=$(asphp isoa "echo @stream_socket_client('unix:///run/php/xpanel-pma.sock') ? 'CONNECTED' : 'DENIED';"); expect "$r" DENIED "A cannot open the phpMyAdmin socket"; }
id -nG isoa | grep -qw www-data && bad "isoa is in www-data" || ok "customers are not in www-data"
for unit in $(systemctl list-units --type=service --no-legend 'xpanel-app-iso*' | awk '{print $1}'); do
  [ -z "$(systemctl show "$unit" -p SupplementaryGroups --value)" ] || bad "$unit has SupplementaryGroups"
done

echo "### 2. Application ports"
B_PORT=$(curl -sk -b /tmp/isob "${H[@]}" $B/api/sites | jq ".[]|select(.id==$B_NODE).port")
r=$(asphp isoa "echo @fsockopen('127.0.0.1', $B_PORT, \$e, \$s, 3) ? 'CONNECTED' : 'DENIED';"); expect "$r" DENIED "A cannot connect to B's app port $B_PORT"
r=$(asphp isob "echo @fsockopen('127.0.0.1', $B_PORT, \$e, \$s, 3) ? 'CONNECTED' : 'DENIED';"); expect "$r" CONNECTED "B can reach its own app"
expect "$(web bnode.iso.example.com /)" 200 "nginx still reaches B's app"

echo "### 3. Files through nginx symlinks"
echo "B-SECRET" > /home/isob/www/b.iso.example.com/secret.txt; chown isob:isob /home/isob/www/b.iso.example.com/secret.txt
runuser -u isoa -- ln -s /home/isob/www/b.iso.example.com/secret.txt /home/isoa/www/a.iso.example.com/leak.txt
code=$(web a.iso.example.com /leak.txt); grep -q B-SECRET /tmp/body && bad "A read B's file via symlink ($code)" || ok "symlink to B's file refused ($code)"
runuser -u isoa -- cat /home/isob/www/b.iso.example.com/secret.txt >/dev/null 2>&1 && bad "A can read B's home" || ok "A cannot read B's home directly"

echo "### 4. Processes"
cat > /home/isoa/www/a.iso.example.com/ps.php <<'PHP'
<?php $me = posix_geteuid(); $o = 0;
foreach (glob('/proc/[0-9]*/status') as $f) { if (preg_match('/^Uid:\s+(\d+)/m', @file_get_contents($f), $m) && (int)$m[1] !== $me) $o++; }
echo "others=$o";
PHP
chown isoa:isoa /home/isoa/www/a.iso.example.com/ps.php
web a.iso.example.com /ps.php >/dev/null; expect "$(cat /tmp/body)" "others=0" "A's PHP sees no other account's processes"
# Cron jobs run in the same sandbox (systemd timer in A's slice).
c isoa POST /cron '{"schedule":"* * * * *","command":"find /proc -maxdepth 1 -regex \"/proc/[0-9]+\" ! -user isoa | wc -l > /home/isoa/logs/cron-ps.txt; ls /home/isob >/dev/null 2>&1 && echo LEAK >> /home/isoa/logs/cron-ps.txt","enabled":true}' >/dev/null
for _ in $(seq 1 75); do [ -s /home/isoa/logs/cron-ps.txt ] && break; sleep 1; done
expect "$(tr -d ' \n' < /home/isoa/logs/cron-ps.txt 2>/dev/null)" 0 "A's cron job sees no other process and cannot list B's home"
expect "$(systemctl show "$(systemctl list-unit-files --no-legend 'xpanel-cron-isoa-*.service' | awk '{print $1}' | head -1)" -p Slice --value)" xpanel-isoa.slice "cron job runs in A's slice"

echo "### 5. API"
expect "$(c isoa GET /sites/$B_PHP/logs)" 404 "A cannot read B's site logs"
expect "$(c isoa POST /sites/$B_NODE/build)" 404 "A cannot build B's site"
expect "$(c isoa DELETE /sites/$B_PHP)" 404 "A cannot delete B's site"
expect "$(c isoa GET "/files?user_id=$B_ID&path=/www")" 404 "A cannot browse B's files"
c isoa GET /sites >/dev/null; expect "$(jq '[.[]|select(.username=="isob")]|length' /tmp/out)" 0 "A's site list has none of B's"

echo "### 6. Suspend stops everything the account runs"
c admin PATCH /users/$B_ID/status '{"status":"suspended"}' >/dev/null
expect "$(systemctl list-units --state=active --no-legend 'xpanel-app-isob-*' | wc -l)" 0 "B's processes stopped"
c admin PATCH /users/$B_ID/status '{"status":"active"}' >/dev/null
expect "$(web b.iso.example.com /)" 200 "B's PHP back after reactivation"

for id in $A_ID $B_ID; do c admin DELETE /users/$id >/dev/null; done
nft list set inet xpanel_apps ports 2>/dev/null | grep -q "$B_PORT" && bad "port rule left after delete" || ok "port rules cleaned up"
[ "$FAIL" = 0 ] && echo "PASS isolation" || { echo "FAIL isolation"; exit 1; }
