#!/usr/bin/env bash
# Certificate management: upload, self-signed, panel certificate (Pebble only).
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 300 /tmp/out; echo; }; }
served_issuer() { echo | openssl s_client -connect 127.0.0.1:443 -servername "$1" 2>/dev/null | openssl x509 -noout -issuer 2>/dev/null; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
c admin POST /users '{"username":"sslcust","password":"Client12345","role":"user"}' >/dev/null; U=$(jq .id /tmp/out)
c admin POST /sites "{\"user_id\":$U,\"domain\":\"ssl.example.com\",\"aliases\":[\"www.ssl.example.com\"],\"app_type\":\"html\",\"doc_root\":\"\"}" >/dev/null
S=$(jq .id /tmp/out)

echo "### upload a certificate bought elsewhere"
cd /tmp && openssl req -x509 -newkey rsa:2048 -nodes -days 90 -subj "/O=Acme Test CA/CN=ssl.example.com" \
  -addext "subjectAltName=DNS:ssl.example.com" -keyout up.key -out up.crt 2>/dev/null
openssl req -x509 -newkey rsa:2048 -nodes -days 90 -subj "/CN=other.example.com" -keyout other.key -out other.crt 2>/dev/null
body() { jq -n --rawfile c "$1" --rawfile k "$2" '{certificate:$c, private_key:$k}'; }
expect "$(c admin POST /sites/$S/ssl/custom "$(body up.crt other.key)")" 400 "mismatched key rejected"
expect "$(c admin POST /sites/$S/ssl/custom "$(body other.crt other.key)")" 400 "certificate for another domain rejected"
expect "$(c admin POST /sites/$S/ssl/custom '{"certificate":"junk","private_key":"junk"}')" 400 "garbage rejected"
expect "$(c admin POST /sites/$S/ssl/custom "$(body up.crt up.key)")" 200 "valid certificate installed"
expect "$(jq -r '.ssl_type + "|" + .ssl_issuer' /tmp/out)" "custom|Acme Test CA" "type and issuer recorded"
jq -r '.warnings[]?' /tmp/out | grep -q "www.ssl.example.com" && ok "warns that www alias is not covered" || bad "missing uncovered-alias warning"
served_issuer ssl.example.com | grep -q "Acme Test CA" && ok "nginx serves the uploaded certificate" || bad "uploaded certificate not served"
expect "$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: ssl.example.com' http://127.0.0.1/)" 301 "HTTP redirects to HTTPS"

echo "### self-signed"
expect "$(c admin POST /sites/$S/ssl/selfsigned)" 200 "self-signed certificate created"
expect "$(jq -r .ssl_type /tmp/out)" selfsigned "type selfsigned"
served_issuer www.ssl.example.com | grep -q "X-Panel self-signed" && ok "self-signed covers the www alias" || bad "self-signed not served"

echo "### another account cannot touch it"
c admin POST /users '{"username":"sslother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c sslother POST /auth/login '{"username":"sslother","password":"Client12345"}' >/dev/null
expect "$(c sslother POST /sites/$S/ssl/selfsigned)" 404 "other account cannot replace the certificate"
expect "$(c sslother DELETE /sites/$S/ssl)" 404 "other account cannot disable HTTPS"

echo "### disable"
expect "$(c admin DELETE /sites/$S/ssl)" 200 "HTTPS disabled"
expect "$(c admin GET /sites)" 200 "list sites"
expect "$(jq -r ".[]|select(.id==$S)|.ssl_status + \"|\" + .ssl_type" /tmp/out)" "none|" "status and type cleared"

if grep -q pebble /etc/xpanel/agent.env 2>/dev/null; then
  echo "### panel certificate (Pebble)"
  expect "$(c admin POST /system/panel-ssl '{"domain":"panel.example.com"}')" 202 "panel certificate queued"
  T=$(jq .id /tmp/out)
  for _ in $(seq 1 60); do st=$(curl -sk -b /tmp/admin "${H[@]}" $B/api/tasks/$T | jq -r .status); [ "$st" = done ] || [ "$st" = failed ] && break; sleep 2; done
  expect "$st" done "panel certificate issued"
  c admin GET /system/panel-ssl >/dev/null
  expect "$(jq -r '.domain + "|" + (.info.self_signed|tostring)' /tmp/out)" "panel.example.com|false" "panel now uses the issued certificate"
  echo | openssl s_client -connect 127.0.0.1:8888 -servername panel.example.com 2>/dev/null | openssl x509 -noout -issuer | grep -qi pebble \
    && ok "port 8888 serves it" || bad "port 8888 still self-signed"
  expect "$(c admin POST /sites "{\"user_id\":$U,\"domain\":\"panel.example.com\",\"aliases\":[],\"app_type\":\"html\",\"doc_root\":\"\"}")" 409 "a website cannot claim the panel hostname"
fi

for id in $U $O; do c admin DELETE /users/$id >/dev/null; done
[ "$FAIL" = 0 ] && echo "PASS ssl" || { echo "FAIL ssl"; exit 1; }
