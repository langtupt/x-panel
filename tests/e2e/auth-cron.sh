#!/usr/bin/env bash
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"})
  echo "$code $(head -c 140 /tmp/out)"; }
step() { echo; echo "### $*"; }
SFTP() { printf "$3" > /tmp/b; sshpass -p "$2" sftp -o BatchMode=no -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -b /tmp/b "$1@127.0.0.1" 2>&1 | grep -v "^sftp>" | tr '\n' ' '; echo; }
export DEBIAN_FRONTEND=noninteractive; apt-get install -y -qq oathtool sshpass openssh-client >/dev/null 2>&1

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "acust ares" "Cron1" "R"
c admin POST /packages '{"name":"Cron1","kind":"user","disk_mb":500,"max_cron":1}' >/dev/null; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"acust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /packages '{"name":"R","kind":"reseller","disk_mb":0}' >/dev/null; RP=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"ares\",\"password\":\"Reseller123\",\"role\":\"reseller\",\"package_id\":$RP}" >/dev/null

step "REGRESSION: fresh account can SFTP-upload into ~/www"
echo x > /tmp/x.txt; SFTP acust Client12345 'ls /\nput /tmp/x.txt www/x.txt\n'; stat -c '%U' /home/acust/www/x.txt

step "own password change"
c acust POST /auth/login '{"username":"acust","password":"Client12345"}' >/dev/null
c acust POST /me/password '{"current":"wrong","new":"Changed12345"}'
c acust POST /me/password '{"current":"Client12345","new":"Changed12345"}'
c acust GET /me | cut -c1-40
c acust2 POST /auth/login '{"username":"acust","password":"Client12345"}'
SFTP acust Changed12345 'ls /\n'

step "2FA enable / login / disable"
c acust POST /me/2fa/setup; SECRET=$(jq -r .secret /tmp/out); jq -r .url /tmp/out | cut -c1-60
c acust POST /me/2fa/enable "{\"secret\":\"$SECRET\",\"code\":\"000000\"}"
c acust POST /me/2fa/enable "{\"secret\":\"$SECRET\",\"code\":\"$(oathtool --totp -b $SECRET)\"}"
c t1 POST /auth/login '{"username":"acust","password":"Changed12345"}'
c t1 POST /auth/login '{"username":"acust","password":"Changed12345","code":"123456"}'
c t1 POST /auth/login "{\"username\":\"acust\",\"password\":\"Changed12345\",\"code\":\"$(oathtool --totp -b $SECRET)\"}" | cut -c1-60
c t1 GET /me | jq -c '{username, totp_enabled}' 2>/dev/null || jq -c '{username, totp_enabled}' /tmp/out
c t1 POST /me/2fa/disable '{"password":"Changed12345","code":"000000"}'
echo "replayed code (expect 401):"; c t2 POST /auth/login "{\"username\":\"acust\",\"password\":\"Changed12345\",\"code\":\"$(oathtool --totp -b $SECRET)\"}" | cut -c1-60
sleep $((31 - $(date +%s) % 30))  # a code works only once: wait for the next one
c t1 POST /me/2fa/disable "{\"password\":\"Changed12345\",\"code\":\"$(oathtool --totp -b $SECRET)\"}"
c t2 POST /auth/login '{"username":"acust","password":"Changed12345"}' | cut -c1-40

step "impersonation"
c admin POST /auth/impersonate "{\"user_id\":$U}" | cut -c1-50
c admin GET /me; jq -c '{username, impersonator}' /tmp/out
c admin GET /sites | cut -c1-20
c admin POST /me/password '{"current":"x","new":"Whatever12345"}'
c admin POST /auth/impersonate "{\"user_id\":$U}"
c admin POST /auth/unimpersonate | cut -c1-40
c admin GET /me; jq -c '{username, impersonator}' /tmp/out
c ares POST /auth/login '{"username":"ares","password":"Reseller123"}' >/dev/null
c ares POST /auth/impersonate "{\"user_id\":$U}"
step "logout while impersonating ends both sessions"
c admin POST /auth/impersonate "{\"user_id\":$U}" >/dev/null; cp /tmp/admin /tmp/admin_copy
c admin POST /auth/logout
c admin_copy POST /auth/unimpersonate

step "cron: create, crontab written, runs, limit"
c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
c admin POST /cron "{\"user_id\":$U,\"schedule\":\"* * * * *\",\"command\":\"date +%H:%M:%S >> /home/acust/logs/cron.txt\",\"enabled\":true}"; J=$(jq .id /tmp/out)
systemctl cat "$(systemctl list-unit-files --no-legend 'xpanel-cron-acust-*.timer' | awk '{print $1}' | head -1)" | grep -E '^(OnCalendar|ProtectProc|Slice)='
c admin POST /cron "{\"user_id\":$U,\"schedule\":\"@daily\",\"command\":\"true\",\"enabled\":true}"
c admin POST /cron "{\"user_id\":$U,\"schedule\":\"* * * *\",\"command\":\"true\",\"enabled\":true}"
for i in $(seq 1 75); do [ -s /home/acust/logs/cron.txt ] && break; sleep 1; done
echo "cron output: $(cat /home/acust/logs/cron.txt 2>/dev/null | head -1) owner=$(stat -c %U /home/acust/logs/cron.txt 2>/dev/null)"
c admin PUT /cron/$J "{\"schedule\":\"* * * * *\",\"command\":\"true\",\"enabled\":false}" | cut -c1-30
systemctl list-unit-files --no-legend --state=enabled "xpanel-cron-acust-*.timer" | wc -l | sed "s/^/enabled timers: /"

step "audit log"
c admin GET /audit; jq -r '[.[0:6][] | .action] | join(", ")' /tmp/out
c acust POST /auth/login '{"username":"acust","password":"Changed12345"}' >/dev/null
c acust GET /audit; jq -r '[.[] | .action] | unique | join(", ")' /tmp/out

step "REGRESSION: delete account succeeds (userdel), crontab + home gone"
c admin DELETE /users/$U
id acust 2>&1 | tail -1; ls /home/acust 2>&1 | tail -1; ls /etc/xpanel-cron/acust 2>/dev/null | wc -l | sed "s/^/cron state files: /"; getent group acust || echo "group gone"
step "REGRESSION: phpMyAdmin login page"
curl -skL $B/phpmyadmin/ | grep -o 'name="pma_username"' | head -1 || echo "NO LOGIN FORM"; ls -ld /var/lib/xpanel | cut -c1-10
