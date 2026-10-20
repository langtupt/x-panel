#!/usr/bin/env bash
# Backups to (a fake) Google Drive: connect, encrypted backup of files,
# databases and mail, restore, retention, isolation between accounts.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FD=http://127.0.0.1:9999
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 400 /tmp/out; echo; }; }
task() { # wait for the task in /tmp/out, print its final status
  local id; id=$(jq -r .id /tmp/out)
  for _ in $(seq 1 300); do
    st=$(curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$id" | jq -r .status)
    [ "$st" = done ] || [ "$st" = failed ] && break; sleep 2
  done
  [ "$st" = failed ] && curl -sk -b /tmp/admin "${H[@]}" "$B/api/tasks/$id" | jq -r .log | tail -8 >&2
  echo "$st"; }

# Point X-Panel at the fake Google endpoints.
pkill -f fakedrive 2>/dev/null; (/e2e/fakedrive/fakedrive -listen 127.0.0.1:9999 >/tmp/fakedrive.log 2>&1 &)
for f in /etc/xpanel/agent.env /etc/xpanel/xpanel.env; do
  grep -q XPANEL_GDRIVE_ENDPOINT "$f" 2>/dev/null || echo "XPANEL_GDRIVE_ENDPOINT=$FD" >> "$f"
done
systemctl restart xpanel-agent xpanel-api; sleep 3

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "bkcust bkother"

c admin POST /users '{"username":"bkcust","password":"Client12345","role":"user"}' >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"bkother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c cust POST /auth/login '{"username":"bkcust","password":"Client12345"}' >/dev/null
c other POST /auth/login '{"username":"bkother","password":"Client12345"}' >/dev/null
echo "### connect Google Drive (device flow)"
expect "$(c admin GET /backups/settings)" 403 "administrators have no shared Google account"
expect "$(c cust POST /backups/drive '{"client_id":"123-test.apps.googleusercontent.com","client_secret":"secret"}')" 200 "device flow started"
expect "$(jq -r .user_code /tmp/out)" "ABCD-EFGH" "user code shown"
for _ in $(seq 1 20); do c cust GET /backups/settings >/dev/null; [ "$(jq -r .connected /tmp/out)" = true ] && break; sleep 1; done
expect "$(jq -r '"\(.connected)|\(.email)"' /tmp/out)" "true|owner@example.com" "drive connected"
expect "$(c cust PUT /backups/settings '{"schedule":"daily","hour":3,"keep":2}')" 400 "schedule needs a passphrase first"
expect "$(c cust PUT /backups/settings '{"schedule":"off","hour":3,"keep":2,"passphrase":"short"}')" 400 "short passphrase refused"
expect "$(c cust PUT /backups/settings '{"schedule":"daily","hour":3,"keep":2,"passphrase":"correct horse battery staple"}')" 200 "settings saved"
c cust GET /backups/settings >/dev/null
jq -e '.passphrase_set and (tostring|contains("correct horse")|not)' /tmp/out >/dev/null && ok "passphrase never returned" || bad "passphrase leaked in settings"
c other GET /backups/settings >/dev/null; expect "$(jq -r .connected /tmp/out)" false "another account has its own, unconnected setup"
echo "### secrets are sealed in the database"
cd /tmp && runuser -u postgres -- psql -d xpanel -tAc "SELECT refresh_token, passphrase, client_secret FROM backup_configs" > /tmp/sealed
grep -qE 'refresh-1|correct horse|(^|[|])secret([|]|$)' /tmp/sealed && bad "secrets stored in clear" || ok "secrets stored encrypted"

echo "### data to protect"
c admin POST /sites "{\"user_id\":$U,\"domain\":\"bk.example.com\",\"aliases\":[],\"app_type\":\"html\",\"doc_root\":\"\"}" >/dev/null
W=/home/bkcust/www/bk.example.com
runuser -u bkcust -- sh -c "echo 'precious-content-42' > $W/precious.txt; mkdir -p $W/sub; echo nested > $W/sub/n.txt; ln -s /etc/shadow $W/leak"
expect "$(c admin POST /databases "{\"user_id\":$U,\"name\":\"shop\",\"password\":\"Db-Pass_12345!\",\"engine\":\"mariadb\"}")" 201 "database bkcust_shop"
mariadb bkcust_shop -e "CREATE TABLE t (id INT PRIMARY KEY, v TEXT); INSERT INTO t VALUES (1,'row-one'),(2,'row-two');"
expect "$(c cust POST /cron '{"schedule":"17 3 * * *","command":"echo nightly","enabled":true}')" 201 "cron job"
MAIL=0
if c admin GET /mail/status >/dev/null && [ "$(jq -r .installed /tmp/out)" = true ]; then
  MAIL=1
  c admin POST /mail/domains "{\"user_id\":$U,\"domain\":\"bkmail.example.com\"}" >/dev/null; MD=$(jq .id /tmp/out)
  c admin POST /mail/domains/$MD/mailboxes '{"local_part":"info","password":"MailPass12345"}' >/dev/null
  printf 'From: info@bkmail.example.com\r\nTo: info@bkmail.example.com\r\nSubject: keep-me\r\n\r\nbody\r\n' > /tmp/m.eml
  curl -s --url smtp://127.0.0.1:587 --ssl-reqd -k --mail-from info@bkmail.example.com --mail-rcpt info@bkmail.example.com \
    --user info@bkmail.example.com:MailPass12345 -T /tmp/m.eml && sleep 3
fi

echo "### backup"
expect "$(c other POST /backups "{\"user_id\":$U}")" 404 "another account cannot back up bkcust"
expect "$(c cust POST /backups "{\"user_id\":$U}")" 202 "customer backs up own account"
expect "$(task)" done "backup task"
c cust GET /backups >/dev/null; expect "$(jq length /tmp/out)" 1 "one backup listed"
BK=$(jq '.[0].id' /tmp/out)
c other GET /backups >/dev/null; expect "$(jq length /tmp/out)" 0 "other account sees no backups"
FID=$(curl -s $FD/debug/files | jq -r '.[0].id')
curl -s $FD/debug/raw/$FID -o /tmp/raw.xpb
[ "$(head -c 8 /tmp/raw.xpb)" = XPANELB1 ] && ok "file on Drive is in the encrypted format" || bad "unexpected file format"
grep -aq "precious-content-42\|row-one" /tmp/raw.xpb && bad "plaintext visible on Drive" || ok "no plaintext on Drive"

echo "### damage, then restore"
rm -f $W/precious.txt; rm -rf $W/sub
mariadb bkcust_shop -e "DELETE FROM t; INSERT INTO t VALUES (9,'garbage');"
[ $MAIL = 1 ] && find /var/mail/vhosts/bkmail.example.com -type f -path '*/new/*' -delete
expect "$(c other POST /backups/$BK/restore '{"files":true,"databases":true,"mail":true}')" 404 "another account cannot restore it"
expect "$(c cust POST /backups/$BK/restore '{"files":true,"databases":true,"mail":true}')" 202 "restore started"
expect "$(task)" done "restore task"
[ "$(cat $W/precious.txt 2>/dev/null)" = precious-content-42 ] && ok "files restored" || bad "file not restored"
[ "$(cat $W/sub/n.txt 2>/dev/null)" = nested ] && ok "nested files restored" || bad "nested file missing"
[ "$(stat -c %U $W/precious.txt 2>/dev/null)" = bkcust ] && ok "restored files belong to the account" || bad "wrong owner"
[ -L $W/leak ] && [ "$(readlink $W/leak)" = /etc/shadow ] && ok "planted symlink stays a symlink (no /etc/shadow copy)" || bad "symlink attack: leak is not the original symlink"
expect "$(mariadb -N bkcust_shop -e 'SELECT group_concat(v ORDER BY id) FROM t')" "row-one,row-two" "database restored"
if [ $MAIL = 1 ]; then
  n=$(curl -sk "imaps://127.0.0.1/" -u info@bkmail.example.com:MailPass12345 -X 'STATUS INBOX (MESSAGES)' | grep -o 'MESSAGES [0-9]*' | awk '{print $2}')
  [ "${n:-0}" -ge 1 ] && ok "mail restored ($n)" || bad "mail not restored"
  [ "$(stat -c %U /var/mail/vhosts/bkmail.example.com/info)" = vmail ] && ok "maildir owned by vmail" || bad "maildir owner"
fi

echo "### wrong passphrase, retention, delete"
expect "$(c admin POST /backups/$BK/restore '{"files":true,"databases":false,"mail":false,"passphrase":"definitely wrong pass"}')" 202 "restore with another passphrase queued"
expect "$(task)" failed "wrong passphrase is rejected"
for _ in 1 2; do c admin POST /backups "{\"user_id\":$U}" >/dev/null; task >/dev/null; done
c admin GET /backups >/dev/null; expect "$(jq "[.[]|select(.username==\"bkcust\")]|length" /tmp/out)" 2 "only the newest 2 kept"
expect "$(curl -s $FD/debug/files | jq length)" 2 "old file removed from Drive"
LAST=$(jq '[.[]|select(.username=="bkcust")][0].id' /tmp/out)
expect "$(c other DELETE /backups/$LAST)" 404 "another account cannot delete it"
expect "$(c cust DELETE /backups/$LAST)" 204 "owner deletes a backup"
expect "$(curl -s $FD/debug/files | jq length)" 1 "deleted from Drive"

echo "### move to a new server: same username, import from Drive, recreate"
# Simulate the new server: the account and every record of it are gone.
expect "$(c admin DELETE /users/$U)" 204 "old account deleted"
(cd /tmp && runuser -u postgres -- psql -d xpanel -qc "DELETE FROM backups WHERE username='bkcust'")
{ id bkcust >/dev/null 2>&1 || mariadb -N -e "SHOW DATABASES LIKE 'bkcust%'" | grep -q .; } && bad "old account still there" || ok "old account fully removed"
c admin POST /users '{"username":"bkcust","password":"Client12345","role":"user"}' >/dev/null; U=$(jq .id /tmp/out)
c cust POST /auth/login '{"username":"bkcust","password":"Client12345"}' >/dev/null
c cust POST /backups/drive '{"client_id":"123-test.apps.googleusercontent.com","client_secret":"secret"}' >/dev/null
for _ in $(seq 1 20); do c cust GET /backups/settings >/dev/null; [ "$(jq -r .connected /tmp/out)" = true ] && break; sleep 1; done
c cust PUT /backups/settings '{"schedule":"off","hour":3,"keep":5,"passphrase":"correct horse battery staple"}' >/dev/null
expect "$(c other POST /backups/import)" 409 "an account without its own Drive connection imports nothing"
expect "$(c cust POST /backups/import)" 200 "import from Drive"
expect "$(jq .added /tmp/out)" 1 "backup made on the old server found"
c cust POST /backups/import >/dev/null; expect "$(jq .added /tmp/out)" 0 "importing again adds nothing"
c cust GET /backups >/dev/null; IB=$(jq '.[0].id' /tmp/out); expect "$(jq -r '.[0].kind' /tmp/out)" imported "listed as imported"
expect "$(c cust POST /backups/$IB/restore '{"files":true,"databases":true,"mail":true,"recreate":true}')" 202 "restore with recreate"
expect "$(task)" done "restore task"
c cust GET /sites >/dev/null; expect "$(jq -r '[.[] | select(.domain == "bk.example.com")] | length' /tmp/out)" 1 "website recreated"
[ "$(cat $W/precious.txt 2>/dev/null)" = precious-content-42 ] && ok "files back" || bad "files missing after move"
expect "$(mariadb -N bkcust_shop -e 'SELECT group_concat(v ORDER BY id) FROM t' 2>&1)" "row-one,row-two" "database recreated with its data"
expect "$(MYSQL_PWD='Db-Pass_12345!' mariadb -N --protocol=TCP -h 127.0.0.1 -u bkcust_shop bkcust_shop -e 'SELECT count(*) FROM t' 2>&1)" 2 "database user keeps its old password"
c cust GET /cron >/dev/null; jq -e '[.[] | select(.command == "echo nightly")] | length == 1' /tmp/out >/dev/null && ok "cron job recreated" || bad "cron job missing"
if [ $MAIL = 1 ]; then
  n=$(curl -sk "imaps://127.0.0.1/" -u info@bkmail.example.com:MailPass12345 -X 'STATUS INBOX (MESSAGES)' | grep -o 'MESSAGES [0-9]*' | awk '{print $2}')
  [ "${n:-0}" -ge 1 ] && ok "mailbox recreated, old password works, mail back ($n)" || bad "mailbox not usable after move"
fi

expect "$(c cust DELETE /backups/drive)" 204 "disconnect drive"
for id in $U $O; do c admin DELETE /users/$id >/dev/null; done
pkill -f fakedrive
[ "$FAIL" = 0 ] && echo "PASS backup" || { echo "FAIL backup"; exit 1; }
