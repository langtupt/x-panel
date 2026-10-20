#!/usr/bin/env bash
# Assertions for Applications, site processes, PostgreSQL and tenant isolation.
set -euo pipefail
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
req() {
 local method=$1 path=$2 body=${3:-} expect=${4:-200}
 local args=(); [ -z "$body" ] || args=(-d "$body")
 local status
 status=$(curl -sk -b /tmp/apps-cookie -c /tmp/apps-cookie "${H[@]}" -X "$method" "$B/api$path" "${args[@]}" -o /tmp/apps-response -w '%{http_code}')
 if [ "$status" != "$expect" ]; then echo "FAIL $method $path expected $expect got $status"; cat /tmp/apps-response; exit 1; fi
}
wait_task() {
 local id=$1
 for i in $(seq 1 900); do
  req GET "/tasks/$id"
  local status; status=$(jq -r .status /tmp/apps-response)
  if [ "$status" = done ]; then return; fi
  if [ "$status" = failed ]; then jq -r .log /tmp/apps-response; exit 1; fi
  sleep 2
 done
 echo 'Task timeout'; exit 1
}
install_app() {
 req GET /apps
 if jq -e --arg id "$1" --arg v "$2" '.apps[]|select(.id==$id)|.installed|index($v) != null' /tmp/apps-response >/dev/null; then echo "PASS already installed $1 $2"; return; fi
 req POST "/apps/$1/install" "{\"version\":\"$2\"}" 202
 wait_task "$(jq -r .id /tmp/apps-response)"
 echo "PASS install $1 $2"
}
web() {
 local domain=$1 want=$2
 for i in $(seq 1 30); do
  if curl -s -H "Host: $domain" http://127.0.0.1 | grep -q "$want"; then return; fi
  sleep 1
 done
 echo "FAIL web $domain"; journalctl -u 'xpanel-app-*' -n 20 --no-pager; exit 1
}
apt-get install -y -qq jq >/dev/null 2>&1
ADMIN_PASSWORD="${ADMIN_PASSWORD:-Admin@123!}"
req POST /auth/login "$(jq -nc --arg pw "$ADMIN_PASSWORD" '{username:"admin",password:$pw}')"
XP_JAR=/tmp/apps-cookie; . "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "appcust" "Apps acceptance *"
req GET /users
OLD=$(jq -r '.users[] | select(.username=="appcust") | .id' /tmp/apps-response)
if [ -n "$OLD" ]; then req DELETE "/users/$OLD" '' 204; fi
req GET /apps
jq -e '.apps | length == 10' /tmp/apps-response >/dev/null
req POST /apps/unknown/install '{"version":"x"}' 400
install_app php 8.3
install_app nodejs 24
req GET /apps
GV=$(jq -r '.apps[]|select(.id=="go")|.available[0]' /tmp/apps-response)
install_app go "$GV"
install_app postgresql 18
req GET /apps
install_app composer "$(jq -r '.apps[]|select(.id=="composer")|.available[0]' /tmp/apps-response)"
req POST /packages '{"name":"Apps acceptance '"$(date +%s)"'","kind":"user","disk_mb":2500,"max_sites":10,"max_dbs":4,"cpu_pct":100,"mem_mb":2048}' 201
PK=$(jq .id /tmp/apps-response)
req POST /users "{\"username\":\"appcust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$PK}" 201
UID_APP=$(jq .id /tmp/apps-response)
for kind in html php nodejs go nextjs laravel; do
 req POST /sites "{\"user_id\":$UID_APP,\"domain\":\"$kind.apps.example.com\",\"aliases\":[],\"app_type\":\"$kind\",\"php_version\":\"8.3\",\"doc_root\":\"\",\"sample\":true,\"env\":{\"GREETING\":\"world\"}}" 201
 SITE=$(jq .id /tmp/apps-response); TASK=$(jq .task.id /tmp/apps-response)
 wait_task "$TASK"
 if [ "$kind" = laravel ]; then web "$kind.apps.example.com" 'Laravel'; else web "$kind.apps.example.com" 'Hello from X-Panel'; fi
 req GET "/sites/$SITE/detect"
 jq -e --arg type "$kind" '.app_type==$type' /tmp/apps-response >/dev/null
 req GET "/sites/$SITE/logs"
 echo "PASS site $kind"
done
# Every running PHP/process service belongs to the account's limited slice.
for unit in $(systemctl list-units --type=service --no-legend 'xpanel-app-appcust-*' | awk '{print $1}'); do
 systemctl show "$unit" -p Slice --value | grep -qx xpanel-appcust.slice
done
for engine in mariadb postgresql; do
 req POST /databases "{\"user_id\":$UID_APP,\"name\":\"$engine\",\"password\":\"Db-Pass_12345!\",\"engine\":\"$engine\"}" 201
 DB_ID=$(jq .id /tmp/apps-response)
 req PATCH "/databases/$DB_ID/password" '{"password":"New-Pass_12345!"}' 204
 if [ "$engine" = postgresql ]; then
  PGPASSWORD='New-Pass_12345!' psql -h 127.0.0.1 -p 5433 -U appcust_postgresql -d appcust_postgresql -Atc 'SELECT current_database()' | grep -qx appcust_postgresql
 fi
 echo "PASS database $engine"
done
req PATCH "/users/$UID_APP/status" '{"status":"suspended"}'
[ "$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: nodejs.apps.example.com' http://127.0.0.1)" = 503 ]
if PGPASSWORD='New-Pass_12345!' psql -h 127.0.0.1 -p 5433 -U appcust_postgresql -d appcust_postgresql -Atc 'SELECT 1' >/dev/null 2>&1; then echo 'FAIL suspended PostgreSQL login'; exit 1; fi
req PATCH "/users/$UID_APP/status" '{"status":"active"}'
web nodejs.apps.example.com 'Hello from X-Panel'
req DELETE "/users/$UID_APP" '' 204
[ -z "$(systemctl list-units --type=service --no-legend 'xpanel-app-appcust-*')" ]
echo 'PASS applications acceptance'

