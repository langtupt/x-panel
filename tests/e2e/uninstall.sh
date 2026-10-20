#!/usr/bin/env bash
# Uninstall: removes the panel and every customer's data, leaves a working
# system, and a fresh install works again afterwards. Run last.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }

# The previous suite may just have restarted the API.
for _ in $(seq 1 30); do [ "$(curl -sk -o /dev/null -w '%{http_code}' $B/api/health)" = 200 ] && break; sleep 1; done
c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
c admin POST /users '{"username":"ucust","password":"Client12345","role":"user"}' >/dev/null; U=$(jq .id /tmp/out)
c admin POST /sites "{\"user_id\":$U,\"domain\":\"gone.example.com\",\"aliases\":[],\"app_type\":\"html\",\"doc_root\":\"\"}" >/dev/null
c admin POST /databases "{\"user_id\":$U,\"name\":\"shop\",\"password\":\"Db-Pass_12345!\",\"engine\":\"mariadb\"}" >/dev/null
[ -d /home/ucust ] && mariadb -e "USE ucust_shop" && ok "test data created" || bad "setup"

echo "### refuses without confirmation"
/usr/local/sbin/xpanel-uninstall </dev/null >/dev/null 2>&1 && bad "ran without confirmation" || ok "asks for confirmation"
systemctl is-active -q xpanel-api && ok "nothing removed when not confirmed" || bad "removed without confirmation"

echo "### uninstall"
XPANEL_CONFIRM=XOA-SACH /usr/local/sbin/xpanel-uninstall >/tmp/uninstall.log 2>&1 && ok "uninstall finished" || { bad "uninstall failed"; tail -20 /tmp/uninstall.log; }
for s in xpanel-api xpanel-agent; do systemctl cat "$s" >/dev/null 2>&1 && bad "$s still installed" || ok "$s removed"; done
[ -z "$(systemctl list-units --all --plain --no-legend 'xpanel-*' | grep -v not-found)" ] && ok "no xpanel units left" || bad "units left: $(systemctl list-units --all --plain --no-legend 'xpanel-*' | head -3)"
for p in /opt/xpanel /etc/xpanel /var/lib/xpanel /etc/nginx/conf.d/xpanel /etc/nginx/conf.d/xpanel-panel.conf /home/ucust /usr/local/sbin/xpanel-uninstall; do
  [ -e "$p" ] && bad "$p still exists" || ok "$p removed"
done
id ucust >/dev/null 2>&1 && bad "hosting user still exists" || ok "hosting user removed"
getent group ucust >/dev/null && bad "hosting group left behind" || ok "hosting group removed"
id xpanel >/dev/null 2>&1 && bad "xpanel user still exists" || ok "service user removed"
mariadb -e "USE ucust_shop" 2>/dev/null && bad "customer database still exists" || ok "customer database dropped"
(cd /tmp && runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='xpanel'") | grep -q 1 && bad "panel database still exists" || ok "panel database dropped"
nft list table inet xpanel_apps >/dev/null 2>&1 && bad "firewall table left" || ok "firewall table removed"
nft list table inet xpanel_fw >/dev/null 2>&1 && bad "X-Panel firewall left" || ok "X-Panel firewall removed"
[ -e /etc/nginx/conf.d/xpanel-ratelimit.conf ] && bad "nginx limits left" || ok "nginx limits removed"
grep -q "BEGIN X-Panel" /etc/ssh/sshd_config && bad "sshd block left" || ok "sshd config restored"
sshd -t && ok "sshd config valid" || bad "sshd config broken"
nginx -t >/dev/null 2>&1 && systemctl is-active -q nginx && ok "nginx still healthy" || bad "nginx broken"
systemctl is-active -q mariadb && ok "MariaDB still running" || bad "MariaDB stopped"
ss -ltn | grep -q ":8888 " && bad "panel port still open" || ok "panel port closed"

echo "### reinstall on the same machine"
cd /root/rel && ADMIN_PASSWORD='Admin@123!' ./install.sh >/tmp/reinstall.log 2>&1 && ok "reinstall finished" || { bad "reinstall failed"; tail -20 /tmp/reinstall.log; }
rm -f /tmp/admin
c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
[ "$(c admin GET /users)" = 200 ] && [ "$(jq '.users|length' /tmp/out)" = 1 ] && ok "fresh panel works (only admin)" || bad "panel after reinstall"
# A name used before the uninstall can be created again (orphan groups once broke this).
groupadd ghost 2>/dev/null; gpasswd -a www-data ghost >/dev/null 2>&1
[ "$(c admin POST /users '{"username":"ucust","password":"Client12345","role":"user"}')" = 201 ] && ok "same account name works after reinstall" || bad "cannot recreate ucust: $(cat /tmp/out)"
[ "$(c admin POST /users '{"username":"ghost","password":"Client12345","role":"user"}')" = 201 ] && [ "$(id -gn ghost)" = ghost ] && ok "orphan group reused" || bad "orphan group blocks account: $(cat /tmp/out)"

[ "$FAIL" = 0 ] && echo "PASS uninstall" || { echo "FAIL uninstall"; exit 1; }
