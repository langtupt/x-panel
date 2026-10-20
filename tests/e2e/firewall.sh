#!/usr/bin/env bash
# Firewall: nftables table, per-IP HTTP limits, allow/deny lists, panel
# login bans (IP) and account lockout, unban/unlock, no self-lockout.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 400 /tmp/out; echo; }; }
IP=$(hostname -I | awk '{print $1}')   # requests to it come from a non-loopback address
EXT=https://$IP:8888

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "fwcust"
c admin POST /users '{"username":"fwcust","password":"Client12345","role":"user","email":"fwcust@example.com"}' >/dev/null
base() { c admin GET /firewall >/dev/null; jq -c '.config' /tmp/out; }

echo "### enabled by default"
expect "$(c admin GET /firewall)" 200 "firewall state"
expect "$(jq -r '"\(.active)|\(.config.enabled)"' /tmp/out)" "true|true" "on after install"
nft list table inet xpanel_fw | grep -q "ip saddr @ban4 drop" && ok "nftables table loaded" || bad "no nftables table"
grep -q "limit_req zone=xp_req" /etc/nginx/conf.d/xpanel-ratelimit.conf && ok "nginx request limits" || bad "no nginx limits"
expect "$(c cust POST /auth/login '{"username":"fwcust","password":"Client12345"}')" 200 "customer login"
expect "$(c cust GET /firewall)" 403 "customers cannot see the firewall"

echo "### no self-lockout"
CFG=$(base)
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.http_burst = 0')")" 200 "zero burst is valid"
nginx -t >/dev/null 2>&1 && ok "zero burst keeps nginx valid" || bad "zero burst broke nginx"
expect "$(c admin PUT /firewall "$CFG")" 200 "restore burst setting"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.block = ["127.0.0.1"]')")" 400 "blocking your own IP refused"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.block = ["1.2.3.4; flush ruleset"]')")" 400 "malformed address refused"
expect "$(c admin POST /firewall/ban '{"ip":"127.0.0.1","minutes":5}')" 400 "cannot ban your own IP"

echo "### HTTP flood limit"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.http_rate = 5 | .http_burst = 5 | .flood_ban = false | .block = ["198.51.100.0/24"]')")" 200 "tight limits applied"
nft list set inet xpanel_fw deny4 | grep -q "198.51.100.0/24" && ok "deny list in nftables" || bad "deny list missing"
codes=$(for i in $(seq 1 40); do curl -sk -o /dev/null -w '%{http_code}\n' "http://$IP/"; done | sort | uniq -c | tr '\n' ' ')
echo "$codes" | grep -q " 429" && ok "flood answered with 429 ($codes)" || bad "no 429 in flood: $codes"
codes=$(for i in $(seq 1 40); do curl -sk -o /dev/null -w '%{http_code}\n' "http://127.0.0.1/"; done | sort | uniq -c | tr '\n' ' ')
echo "$codes" | grep -q " 429" && bad "localhost limited: $codes" || ok "localhost exempt"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c --arg ip "$IP" '.http_rate = 5 | .http_burst = 5 | .allow = [$ip]')")" 200 "allow-listed"
codes=$(for i in $(seq 1 40); do curl -sk -o /dev/null -w '%{http_code}\n' "http://$IP/"; done | sort | uniq -c | tr '\n' ' ')
echo "$codes" | grep -q " 429" && bad "allow-listed IP still limited: $codes" || ok "allow-listed IP not limited"

echo "### panel login: IP ban"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.panel_ip_fails = 3 | .panel_ban_min = 5 | .account_fails = 0')")" 200 "3 failures per IP"
for i in 1 2 3; do curl -sk -o /dev/null "${H[@]}" -X POST "$EXT/api/auth/login" -d '{"username":"nobody","password":"wrong-password"}'; done
sleep 2
nft list set inet xpanel_fw ban4 | grep -q "$IP" && ok "IP banned in the firewall" || bad "IP not banned"
c admin GET /firewall >/dev/null
expect "$(jq -r --arg ip "$IP" '.bans[] | select(.ip == $ip) | .reason' /tmp/out)" panel-login "ban listed with its reason"
expect "$(c admin POST /firewall/unban "{\"ip\":\"$IP\"}")" 204 "unban"
nft list set inet xpanel_fw ban4 | grep -q "$IP" && bad "still banned" || ok "unbanned"
expect "$(c admin POST /firewall/ban '{"ip":"203.0.113.9","minutes":0,"reason":"test"}')" 204 "manual permanent ban"
c admin GET /firewall >/dev/null
expect "$(jq -r '.bans[] | select(.ip == "203.0.113.9") | "\(.reason)|\(.expires_at)"' /tmp/out)" "admin: test|null" "listed as permanent"
c admin POST /firewall/unban '{"ip":"203.0.113.9"}' >/dev/null

echo "### panel login: account lockout"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.panel_ip_fails = 0 | .account_fails = 3 | .account_lock_min = 5')")" 200 "3 failures per account"
for i in 1 2 3; do c x POST /auth/login '{"username":"fwcust","password":"wrong-password"}' >/dev/null; done
expect "$(c x POST /auth/login '{"username":"fwcust","password":"Client12345"}')" 429 "account locked (even with the right password)"
c admin GET /firewall >/dev/null
expect "$(jq -r '.locked_accounts[0].key' /tmp/out)" fwcust "listed as locked"
sleep 1; c cust GET /notifications >/dev/null
jq -e '.notifications | map(.kind) | index("security") != null' /tmp/out >/dev/null && ok "owner alerted" || bad "no alert"
expect "$(c admin POST /firewall/unlock '{"username":"fwcust"}')" 204 "unlock"
expect "$(c x POST /auth/login '{"username":"fwcust","password":"Client12345"}')" 200 "login works again"

echo "### turn off and on"
expect "$(c admin PUT /firewall "$(echo "$CFG" | jq -c '.enabled = false')")" 200 "disabled"
nft list table inet xpanel_fw >/dev/null 2>&1 && bad "table still loaded" || ok "table removed"
[ -e /etc/nginx/conf.d/xpanel-ratelimit.conf ] && bad "nginx limits still on" || ok "nginx limits removed"
expect "$(c admin PUT /firewall "$CFG")" 200 "re-enabled with defaults"
systemctl restart xpanel-agent; sleep 3
nft list table inet xpanel_fw >/dev/null 2>&1 && ok "restored after agent restart" || bad "not restored"
nginx -t >/dev/null 2>&1 && ok "nginx config valid" || bad "nginx config broken"

[ "$FAIL" = 0 ] && echo "PASS firewall" || { echo "FAIL firewall"; exit 1; }
