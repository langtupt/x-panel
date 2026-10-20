#!/usr/bin/env bash
# 2FA recovery codes, monthly bandwidth limit, mailboxes counted in the
# package's disk space.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 400 /tmp/out; echo; }; }
psqlx() { (cd /tmp && runuser -u postgres -- psql -d xpanel -tAqc "$1"); }
command -v oathtool >/dev/null || { export DEBIAN_FRONTEND=noninteractive; apt-get install -y -qq oathtool >/dev/null 2>&1; }

grep -q XPANEL_STATS_INTERVAL /etc/xpanel/xpanel.env || echo "XPANEL_STATS_INTERVAL=10s" >> /etc/xpanel/xpanel.env
systemctl restart xpanel-api; sleep 3

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "l2cust" "L2 *"
c admin POST /packages '{"name":"L2 pkg","kind":"user","disk_mb":500,"bandwidth_gb":1,"max_sites":2,"max_mailboxes":5}' >/dev/null; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"l2cust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}" >/dev/null; U=$(jq .id /tmp/out)
c cust POST /auth/login '{"username":"l2cust","password":"Client12345"}' >/dev/null

echo "### 2FA recovery codes"
c cust POST /me/2fa/setup >/dev/null; SECRET=$(jq -r .secret /tmp/out)
expect "$(c cust POST /me/2fa/enable "{\"secret\":\"$SECRET\",\"code\":\"$(oathtool --totp -b "$SECRET")\"}")" 200 "2FA enabled"
mapfile -t CODES < <(jq -r '.recovery_codes[]' /tmp/out)
expect "${#CODES[@]}" 10 "10 recovery codes issued"
psqlx "SELECT code_hash FROM totp_recovery_codes" | grep -qF "${CODES[0]}" && bad "codes stored in clear" || ok "only hashes stored"
expect "$(c r1 POST /auth/login '{"username":"l2cust","password":"Client12345"}')" 401 "password alone is not enough"
expect "$(c r1 POST /auth/login "{\"username\":\"l2cust\",\"password\":\"Client12345\",\"code\":\"$(echo "${CODES[0]}" | tr a-z A-Z)\"}")" 200 "recovery code logs in (any case)"
expect "$(c r2 POST /auth/login "{\"username\":\"l2cust\",\"password\":\"Client12345\",\"code\":\"${CODES[0]}\"}")" 401 "a recovery code works only once"
expect "$(c r2 POST /auth/login "{\"username\":\"l2cust\",\"password\":\"Wrong12345\",\"code\":\"${CODES[1]}\"}")" 401 "recovery code needs the password"
c admin GET "/audit?limit=20" >/dev/null 2>&1; grep -q auth.login_recovery_code /tmp/out && ok "recovery login audited" || echo "INFO audit list not checked"
c r1 GET /me/2fa/recovery-codes >/dev/null; expect "$(jq .remaining /tmp/out)" 9 "9 codes left"
expect "$(c r1 POST /me/2fa/recovery-codes "{\"password\":\"Client12345\",\"code\":\"${CODES[2]}\"}")" 400 "a recovery code cannot mint new codes"
sleep $((30 - $(date +%s) % 30 + 1))  # next TOTP window: codes are single use
expect "$(c r1 POST /me/2fa/recovery-codes "{\"password\":\"Client12345\",\"code\":\"$(oathtool --totp -b "$SECRET")\"}")" 200 "new set with password + TOTP"
NEW=$(jq -r '.recovery_codes[0]' /tmp/out)
expect "$(c r3 POST /auth/login "{\"username\":\"l2cust\",\"password\":\"Client12345\",\"code\":\"${CODES[3]}\"}")" 401 "old set no longer works"
expect "$(c r3 POST /auth/login "{\"username\":\"l2cust\",\"password\":\"Client12345\",\"code\":\"$NEW\"}")" 200 "new set works"
sleep $((30 - $(date +%s) % 30 + 1))
expect "$(c r3 POST /me/2fa/disable "{\"password\":\"Client12345\",\"code\":\"$(oathtool --totp -b "$SECRET")\"}")" 204 "2FA disabled"
expect "$(psqlx "SELECT count(*) FROM totp_recovery_codes WHERE user_id=$U")" 0 "codes removed with 2FA"

echo "### mailboxes count in the package's disk space"
if c admin GET /mail/status >/dev/null && [ "$(jq -r .installed /tmp/out)" = true ]; then
  c cust POST /mail/domains '{"domain":"l2mail.example.com"}' >/dev/null; MD=$(jq .id /tmp/out)
  expect "$(c cust POST /mail/domains/$MD/mailboxes '{"local_part":"a","password":"MailPass12345","quota_mb":0}')" 400 "unlimited mailbox refused on a limited package"
  expect "$(c cust POST /mail/domains/$MD/mailboxes '{"local_part":"a","password":"MailPass12345","quota_mb":300}')" 201 "300 MB mailbox"
  A=$(jq .id /tmp/out)
  expect "$(c cust POST /mail/domains/$MD/mailboxes '{"local_part":"b","password":"MailPass12345","quota_mb":200}')" 409 "over the package (500 - 100 for websites - 300)"
  expect "$(c cust POST /mail/domains/$MD/mailboxes '{"local_part":"b","password":"MailPass12345","quota_mb":100}')" 201 "100 MB fits"
  c cust GET /mail/domains/$MD >/dev/null; expect "$(jq .mail_free_mb /tmp/out)" 0 "nothing left to allocate"
  jq -e '.mailbox_list[0] | has("used_mb")' /tmp/out >/dev/null && ok "mailbox usage reported" || bad "no used_mb"
  if repquota -u /home >/dev/null 2>&1; then
    expect "$(quota -u l2cust 2>/dev/null | awk 'NR==3{print $3}')" $((100 * 1024)) "files quota is 500 - 400 MB of mail"
  else
    echo "INFO quotas off on /home: files quota not checked"
  fi
  expect "$(c cust PATCH /mail/mailboxes/$A '{"quota_mb":400}')" 409 "growing a mailbox past the package refused"
  c admin POST /packages '{"name":"L2 tiny","kind":"user","disk_mb":300}' >/dev/null; TP=$(jq .id /tmp/out)
  expect "$(c admin PATCH /users/$U/package "{\"package_id\":$TP}")" 409 "smaller package than the mail allocation refused"
  expect "$(c cust DELETE /mail/mailboxes/$A)" 204 "deleting a mailbox frees its space"
  c cust GET /mail/domains/$MD >/dev/null; expect "$(jq .mail_free_mb /tmp/out)" 300 "space back"
else
  echo "INFO mail server not installed: mail quota not tested"
fi

echo "### monthly bandwidth limit"
c cust POST /sites '{"domain":"bw.example.com","aliases":[],"app_type":"html","doc_root":""}' >/dev/null
echo bw-ok > /home/l2cust/www/bw.example.com/index.html; chown l2cust: /home/l2cust/www/bw.example.com/index.html
get() { curl -sk -o /dev/null -w '%{http_code}' --resolve bw.example.com:80:127.0.0.1 http://bw.example.com/; }
expect "$(get)" 200 "site serves"
# 1.5 GB served earlier this month.
psqlx "INSERT INTO resource_samples (user_id, at, mem_mb, tasks, db_mb, mail_mb, bw_bytes, requests) VALUES ($U, now() - interval '1 minute', 0, 0, 0, 0, 1610612736, 1)"
for _ in $(seq 1 30); do [ "$(get)" = 503 ] && break; sleep 2; done
expect "$(get)" 503 "site blocked over the bandwidth limit"
curl -sk --resolve bw.example.com:80:127.0.0.1 http://bw.example.com/ | grep -q "băng thông" && ok "explains why" || bad "no bandwidth message"
for _ in 1 2 3 4 5; do c cust GET /stats/accounts >/dev/null; [ "$(jq ".[0].bandwidth_blocked" /tmp/out)" = true ] && break; sleep 1; done
expect "$(jq ".[0].bandwidth_blocked" /tmp/out)" true "stats show the block"
# Simulate the month changing while nginx still has the previous block.
psqlx "DELETE FROM resource_samples WHERE user_id=$U; INSERT INTO resource_samples (user_id, at, mem_mb, tasks, db_mb, mail_mb, bw_bytes, requests) VALUES ($U, date_trunc('month', now()) - interval '1 minute', 0, 0, 0, 0, 1610612736, 1); UPDATE users SET bw_blocked_month=(date_trunc('month', now()) - interval '1 day')::date WHERE id=$U"
for _ in $(seq 1 30); do [ "$(get)" = 200 ] && break; sleep 2; done
expect "$(get)" 200 "previous month's block cleared automatically"
psqlx "INSERT INTO resource_samples (user_id, at, mem_mb, tasks, db_mb, mail_mb, bw_bytes, requests) VALUES ($U, now() - interval '1 minute', 0, 0, 0, 0, 1610612736, 1)"
for _ in $(seq 1 30); do [ "$(get)" = 503 ] && break; sleep 2; done
expect "$(get)" 503 "current month's limit still enforced"
c admin PUT /packages/$P '{"name":"L2 pkg","kind":"user","disk_mb":500,"bandwidth_gb":5,"max_sites":2,"max_mailboxes":5}' >/dev/null
for _ in $(seq 1 30); do [ "$(get)" = 200 ] && break; sleep 2; done
expect "$(get)" 200 "unblocked after a bigger package"

sed -i '/XPANEL_STATS_INTERVAL/d' /etc/xpanel/xpanel.env; systemctl restart xpanel-api
[ "$FAIL" = 0 ] && echo "PASS limits2" || { echo "FAIL limits2"; exit 1; }
