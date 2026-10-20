#!/usr/bin/env bash
# Mail server: install, domains/mailboxes/aliases, delivery with DKIM,
# relay and spoofing protection, isolation, suspension, Roundcube.
set -u
B=https://127.0.0.1:8888
W=https://127.0.0.1:2096
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 400 /tmp/out; echo; }; }
wait_task() { for _ in $(seq 1 600); do st=$(curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$1" | jq -r .status)
  [ "$st" = done ] && return 0; [ "$st" = failed ] && { curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$1" | jq -r .log | tail -15; return 1; }; sleep 2; done; return 1; }
install_app() { c admin GET /apps >/dev/null
  if jq -e --arg id "$1" '.apps[]|select(.id==$id)|.installed|length>0' /tmp/out >/dev/null; then ok "$1 already installed"; return; fi
  local v; v=$(jq -r --arg id "$1" '.apps[]|select(.id==$id)|.available[0]' /tmp/out)
  expect "$(c admin POST /apps/$1/install "{\"version\":\"$v\"}")" 202 "install $1 $v"
  wait_task "$(jq -r .id /tmp/out)" && ok "$1 installed" || bad "$1 install failed"; }
msg() { printf 'From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\nHello from X-Panel e2e.\r\n' "$1" "$2" "$3" > /tmp/msg.eml; }
send() { # login password from rcpt subject [port]
  msg "$3" "$4" "$5"
  curl -sS --max-time 30 --url "smtp://127.0.0.1:${6:-587}" --ssl-reqd -k --mail-from "$3" --mail-rcpt "$4" \
    --user "$1:$2" -T /tmp/msg.eml 2>&1; }
imap_count() { curl -sk --max-time 20 "imaps://127.0.0.1/" -u "$1:$2" -X 'STATUS INBOX (MESSAGES)' 2>/dev/null | grep -o 'MESSAGES [0-9]*' | awk '{print $2}'; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "mailcust mailother" "MailPkg"
install_app php
install_app mailserver
for p in 25 587 465 993 995; do ss -ltn | grep -q ":$p " && ok "port $p listening" || bad "port $p not listening"; done

echo "### accounts"
c admin POST /packages '{"name":"MailPkg","kind":"user","disk_mb":500,"max_sites":1,"max_mailboxes":2,"php_versions":[]}' >/dev/null; PK=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"mailcust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$PK}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"mailother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c cust POST /auth/login '{"username":"mailcust","password":"Client12345"}' >/dev/null
c other POST /auth/login '{"username":"mailother","password":"Client12345"}' >/dev/null
echo "mailcust:Linux12345" | chpasswd

expect "$(c cust POST /mail/domains '{"domain":"corp.example.com"}')" 201 "customer adds mail domain"; D=$(jq .id /tmp/out)
expect "$(c other POST /mail/domains '{"domain":"corp.example.com"}')" 409 "same domain for another account refused"
expect "$(c cust POST /mail/domains/$D/mailboxes '{"local_part":"info","password":"MailPass12345","quota_mb":50}')" 201 "mailbox info@"
expect "$(c cust POST /mail/domains/$D/mailboxes '{"local_part":"Bad..x","password":"MailPass12345"}')" 400 "invalid local part"
expect "$(c cust POST /mail/domains/$D/mailboxes '{"local_part":"weak","password":"short"}')" 400 "weak password"
expect "$(c cust POST /mail/domains/$D/mailboxes '{"local_part":"boss","password":"BossPass12345","quota_mb":50}')" 201 "mailbox boss@"; BOSS=$(jq .id /tmp/out)
expect "$(c cust POST /mail/domains/$D/mailboxes '{"local_part":"third","password":"MailPass12345","quota_mb":50}')" 409 "package mailbox limit"
expect "$(c cust POST /mail/domains/$D/aliases '{"local_part":"sales","destinations":["info@corp.example.com"]}')" 201 "alias sales@ -> info@"
expect "$(c cust POST /mail/domains/$D/aliases '{"local_part":"info","destinations":["boss@corp.example.com"]}')" 409 "alias cannot shadow a mailbox"
expect "$(c cust GET /mail/domains/$D)" 200 "domain detail"
jq -e '.dns[]|select(.type=="TXT" and (.name|startswith("xpanel._domainkey.")) and (.value|contains("p=")))' /tmp/out >/dev/null \
  && ok "DKIM record published" || bad "no DKIM record"
jq -e '.dns[]|select(.type=="MX")' /tmp/out >/dev/null && ok "MX record shown" || bad "no MX record"

echo "### isolation between accounts"
expect "$(c other GET /mail/domains/$D)" 404 "other account cannot read the domain"
expect "$(c other POST /mail/domains/$D/mailboxes '{"local_part":"evil","password":"MailPass12345"}')" 404 "other account cannot add mailboxes"
expect "$(c other PATCH /mail/mailboxes/$BOSS '{"password":"Hijacked12345"}')" 404 "other account cannot reset a password"
expect "$(c other DELETE /mail/domains/$D)" 404 "other account cannot delete the domain"
c other GET /mail/domains >/dev/null; expect "$(jq length /tmp/out)" 0 "other account sees no domains"
for f in /etc/dovecot/xpanel-users /var/mail/vhosts/corp.example.com /etc/opendkim/keys/corp.example.com/xpanel.private; do
  runuser -u mailcust -- sh -c "if [ -d '$f' ]; then ls '$f'; else head -c1 '$f'; fi" >/dev/null 2>&1 && bad "customer can read $f" || ok "customer cannot read $f"
done

echo "### delivery"
out=$(send boss@corp.example.com BossPass12345 boss@corp.example.com sales@corp.example.com "to-alias")
[ -z "$out" ] && ok "authenticated submission (587) accepted" || bad "submission: $out"
out=$(curl -sS --max-time 30 --url "smtps://127.0.0.1:465" -k --mail-from boss@corp.example.com --mail-rcpt info@corp.example.com \
  --user boss@corp.example.com:BossPass12345 -T /tmp/msg.eml 2>&1)
[ -z "$out" ] && ok "implicit TLS submission (465) accepted" || bad "smtps: $out"
sleep 4
n=$(imap_count info@corp.example.com MailPass12345); [ "${n:-0}" -ge 2 ] && ok "info@ received $n messages (IMAPS)" || bad "info@ has ${n:-0} messages"
curl -sk "imaps://127.0.0.1/INBOX;UID=1" -u info@corp.example.com:MailPass12345 > /tmp/m1 2>/dev/null
grep -q "^DKIM-Signature:.*d=corp.example.com" /tmp/m1 && ok "message is DKIM-signed" || bad "no DKIM signature"
curl -sk "pop3s://127.0.0.1/" -u info@corp.example.com:MailPass12345 2>/dev/null | grep -q . && ok "POP3S login works" || bad "POP3S failed"
curl -sk "imaps://127.0.0.1/" -u info@corp.example.com:MailPass12345 > /tmp/folders 2>/dev/null
for f in Sent Drafts Trash Junk; do grep -qw "$f" /tmp/folders && ok "folder $f exists" || bad "folder $f missing"; done

echo "### abuse protection"
IP=$(hostname -I | awk '{print $1}')
msg x@gmail.com someone@gmail.com relay > /dev/null
out=$(curl -sS --max-time 20 --url "smtp://$IP:25" --mail-from x@gmail.com --mail-rcpt someone@gmail.com -T /tmp/msg.eml 2>&1)
echo "$out" | grep -qiE "554|454|relay|denied|reject" && ok "open relay refused" || bad "relay not refused: $out"
out=$(send boss@corp.example.com BossPass12345 info@corp.example.com sales@corp.example.com "spoof")
[ -n "$out" ] && ok "sending as another mailbox refused" || bad "sender spoofing accepted"
out=$(send boss@corp.example.com WrongPass12345 boss@corp.example.com info@corp.example.com "badpw")
[ -n "$out" ] && ok "wrong SMTP password refused" || bad "wrong password accepted"
curl -sk --max-time 40 "imaps://127.0.0.1/" -u mailcust:Linux12345 >/dev/null 2>&1 && bad "Linux account logs in to IMAP" || ok "Linux accounts cannot log in to IMAP"

msg info@corp.example.com boss@corp.example.com local-spoof >/dev/null
out=$(runuser -u mailother -- curl -sS --max-time 20 --url "smtp://127.0.0.1:25" --mail-from info@corp.example.com --mail-rcpt boss@corp.example.com -T /tmp/msg.eml 2>&1)
[ -n "$out" ] && ok "another account's process cannot claim a hosted address via localhost" || bad "local cross-account spoofing accepted"
out=$(runuser -u mailother -- curl -sS --max-time 20 --url "smtp://127.0.0.1:25" --mail-from x@corp.example.com --mail-rcpt someone@gmail.com -T /tmp/msg.eml 2>&1)
[ -n "$out" ] && ok "no relay for local processes" || bad "localhost can relay"
printf 'Subject: t\n\nx\n' | runuser -u mailother -- /usr/sbin/sendmail -f info@corp.example.com boss@corp.example.com >/dev/null 2>&1 \
  && bad "customer can use sendmail" || ok "sendmail restricted to root"

echo "### password, suspension"
expect "$(c cust PATCH /mail/mailboxes/$BOSS '{"password":"NewBoss12345"}')" 200 "change mailbox password"
curl -sk --max-time 40 "imaps://127.0.0.1/" -u boss@corp.example.com:BossPass12345 >/dev/null 2>&1 && bad "old password still works" || ok "old password rejected"
curl -sk --max-time 40 "imaps://127.0.0.1/" -u boss@corp.example.com:NewBoss12345 >/dev/null 2>&1 && ok "new password works" || bad "new password rejected"
c admin PATCH /users/$U/status '{"status":"suspended"}' >/dev/null
curl -sk --max-time 40 "imaps://127.0.0.1/" -u info@corp.example.com:MailPass12345 >/dev/null 2>&1 && bad "suspended account mailbox logs in" || ok "suspended account mailboxes locked"
c admin PATCH /users/$U/status '{"status":"active"}' >/dev/null
curl -sk --max-time 40 "imaps://127.0.0.1/" -u info@corp.example.com:MailPass12345 >/dev/null 2>&1 && ok "reactivated mailbox logs in" || bad "mailbox still locked"

echo "### Roundcube"
install_app roundcube
expect "$(curl -sk -o /dev/null -w '%{http_code} %{redirect_url}' $B/webmail/)" "302 https://127.0.0.1:2096/webmail/" "panel port redirects to the webmail port"
expect "$(curl -sk -o /tmp/rc -w '%{http_code}' $W/webmail/)" 200 "webmail page"
grep -qi roundcube /tmp/rc && ok "Roundcube login form" || bad "not Roundcube"
for asset in $(grep -oE '(href|src)="static.php/[^"]+.(css|js)[^"]*"' /tmp/rc | sed -E 's/^(href|src)="//; s/"$//' | head -3); do
  expect "$(curl -sk -o /dev/null -w '%{http_code}' "$W/webmail/$asset")" 200 "webmail asset ${asset%%\?*}"
done
code=$(curl -sk -o /dev/null -w "%{http_code}" $W/webmail/config/config.inc.php); [ "$code" = 403 ] || [ "$code" = 404 ] && ok "config not reachable ($code)" || bad "config reachable ($code)"
TOK=$(curl -sk -c /tmp/rcj -b /tmp/rcj $W/webmail/ | grep -o 'name="_token" value="[^"]*"' | sed 's/.*value="//;s/"//')
code=$(curl -sk -c /tmp/rcj -b /tmp/rcj -o /dev/null -w '%{http_code} %{redirect_url}' "$W/webmail/?_task=login" \
  --data-urlencode "_token=$TOK" -d "_task=login&_action=login&_timezone=UTC" \
  --data-urlencode "_user=info@corp.example.com" --data-urlencode "_pass=MailPass12345")
echo "$code" | grep -q "^302.*_task=mail" && ok "webmail login" || bad "webmail login: $code"
stat -c '%U %a' /var/lib/xpanel/webmail | grep -q "xpanel-webmail 700" && ok "webmail data private" || bad "webmail data perms $(stat -c '%U %a' /var/lib/xpanel/webmail)"

echo "### delete"
expect "$(c cust DELETE /mail/mailboxes/$BOSS)" 204 "delete mailbox"
[ -d /var/mail/vhosts/corp.example.com/boss ] && bad "maildir kept" || ok "maildir removed"
c admin DELETE /users/$U >/dev/null
[ -d /var/mail/vhosts/corp.example.com ] && bad "domain mail kept after account deletion" || ok "account deletion removes mail"
grep -q corp.example.com /etc/postfix/xpanel/domains && bad "domain still routed" || ok "domain no longer routed"
c admin DELETE /users/$O >/dev/null; c admin DELETE /packages/$PK >/dev/null
[ "$FAIL" = 0 ] && echo "PASS mail" || { echo "FAIL mail"; exit 1; }
