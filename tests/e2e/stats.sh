#!/usr/bin/env bash
# Resource statistics per account: bandwidth from access logs, CPU from the
# account's cgroup, visibility between accounts.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 400 /tmp/out; echo; }; }

grep -q XPANEL_STATS_INTERVAL /etc/xpanel/xpanel.env || echo "XPANEL_STATS_INTERVAL=10s" >> /etc/xpanel/xpanel.env
systemctl restart xpanel-api; sleep 2
c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "stuser stother" "StatsPkg"
c admin POST /packages '{"name":"StatsPkg","kind":"user","disk_mb":500,"max_sites":2,"cpu_pct":100,"mem_mb":512,"bandwidth_gb":5}' >/dev/null; PK=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"stuser\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$PK}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"stother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c st POST /auth/login '{"username":"stuser","password":"Client12345"}' >/dev/null
c so POST /auth/login '{"username":"stother","password":"Client12345"}' >/dev/null
c admin POST /sites "{\"user_id\":$U,\"domain\":\"stats.example.com\",\"aliases\":[],\"app_type\":\"html\",\"doc_root\":\"\"}" >/dev/null
head -c 100000 /dev/urandom > /home/stuser/www/stats.example.com/big.bin; chown stuser: /home/stuser/www/stats.example.com/big.bin
sleep 15 # first sample sees the new log at its end
for _ in $(seq 1 20); do curl -s -o /dev/null -H 'Host: stats.example.com' http://127.0.0.1/big.bin; done
# Burn CPU inside the account's slice for a while.
systemd-run --quiet --slice=xpanel-stuser.slice --uid=stuser -p RuntimeMaxSec=25 sh -c 'while :; do :; done'
sleep 25

expect "$(c admin GET '/stats/accounts?range=24h')" 200 "admin stats"
jq ".[]|select(.username==\"stuser\")" /tmp/out > /tmp/st.json
bw=$(jq .bandwidth_bytes /tmp/st.json); req=$(jq .requests /tmp/st.json)
[ "$bw" -ge 2000000 ] && ok "bandwidth counted ($bw bytes)" || bad "bandwidth $bw"
[ "$req" -ge 20 ] && ok "requests counted ($req)" || bad "requests $req"
cpu=$(jq '[.points[].cpu_pct // 0]|max' /tmp/st.json)
awk "BEGIN{exit !($cpu > 20)}" && ok "CPU measured in the account's slice (peak ${cpu}%)" || bad "cpu $cpu"
expect "$(jq -r '.package.bandwidth_gb' /tmp/st.json)" 5 "package limits included"

echo "### visibility"
expect "$(c so GET '/stats/accounts?range=24h')" 200 "user stats"
expect "$(jq -r '[.[].username]|join(",")' /tmp/out)" stother "a user sees only itself"
expect "$(c so GET "/stats/accounts?range=24h&user_id=$U")" 404 "a user cannot read another account"
expect "$(c st GET '/stats/accounts?range=1y')" 400 "invalid range"
logrotate -d /etc/logrotate.d/xpanel-nginx >/dev/null 2>&1 && ok "website logs are rotated" || bad "logrotate config invalid"

for id in $U $O; do c admin DELETE /users/$id >/dev/null; done; c admin DELETE /packages/$PK >/dev/null
sed -i '/XPANEL_STATS_INTERVAL/d' /etc/xpanel/xpanel.env; systemctl restart xpanel-api
[ "$FAIL" = 0 ] && echo "PASS stats" || { echo "FAIL stats"; exit 1; }
