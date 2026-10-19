#!/usr/bin/env bash
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"})
  echo "$code $(head -c 160 /tmp/out)"; }
step() { echo; echo "### $*"; }
wait_task() { # wait_task <jar> <id>
  case "$2" in ""|null) echo "task: no id (request failed)"; return 1;; esac
  for i in $(seq 1 400); do
    st=$(curl -sk -b /tmp/$1 "${H[@]}" $B/api/tasks/$2 | jq -r .status)
    [ "$st" = done ] || [ "$st" = failed ] && break; sleep 2; done
  curl -sk -b /tmp/$1 "${H[@]}" $B/api/tasks/$2 | jq -r '"task " + .status + "\n" + .log' | tail -6; }
web() { curl -s -o /tmp/body -w '%{http_code}' -H "Host: $1" "http://127.0.0.1${2:-/}"; echo " $(head -c 120 /tmp/body | tr '\n' ' ')"; }
webs() { curl -sk -o /tmp/body -w '%{http_code}' --resolve "$1:443:127.0.0.1" "https://$1${2:-/}"; echo " $(head -c 120 /tmp/body | tr '\n' ' ')"; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
step "admin: PHP list before";             c admin GET /php
step "admin: install PHP 8.3 (task)";      c admin POST /php/8.3; T=$(jq .id /tmp/out); wait_task admin $T
step "admin: install again (expect 409)";  c admin POST /php/8.3
step "admin: PHP list after";              c admin GET /php

step "admin: package (2 sites, PHP 8.3 only) + cust1"
c admin POST /packages '{"name":"Web","kind":"user","disk_mb":500,"max_sites":2,"php_versions":["8.3"]}'; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"cust1\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}"; U1=$(jq .id /tmp/out)
c admin POST /users '{"username":"cust2","password":"Client12345","role":"user"}'
c cust1 POST /auth/login '{"username":"cust1","password":"Client12345"}' >/dev/null

step "cust1: site1.example.com + www, PHP 8.3"
c cust1 POST /sites '{"domain":"Site1.Example.com","aliases":["www.site1.example.com"],"php_version":"8.3","doc_root":""}'; S1=$(jq .id /tmp/out)
ls /etc/nginx/conf.d/xpanel/cust1/; ls /etc/php/8.3/fpm/pool.d/; ls -l /run/php/ | grep xpanel
web site1.example.com
cat > /home/cust1/www/site1.example.com/info.php <<'PHP'
<?php echo "uid=" . posix_getpwuid(posix_geteuid())["name"] . " php=" . PHP_VERSION . " basedir=" . ini_get("open_basedir") . " passwd=" . (@file_get_contents("/etc/passwd") === false ? "blocked" : "READABLE");
PHP
chown cust1:cust1 /home/cust1/www/site1.example.com/info.php
step "PHP runs as cust1 with open_basedir";  web www.site1.example.com /info.php
step "cust1: static site2";                  c cust1 POST /sites '{"domain":"site2.example.com","aliases":[],"php_version":"","doc_root":""}'; S2=$(jq .id /tmp/out); web site2.example.com
step "cust1: third site (expect 409)";       c cust1 POST /sites '{"domain":"site3.example.com","aliases":[],"php_version":"","doc_root":""}'
step "cust1: PHP 7.4 (expect 400)";          c cust1 PUT /sites/$S2 '{"aliases":[],"php_version":"7.4","doc_root":""}'
step "admin: cust2 site reusing alias (409)"; c admin POST /sites '{"user_id":'$(curl -sk -b /tmp/admin "${H[@]}" $B/api/users | jq '.users[]|select(.username=="cust2").id')',"domain":"other.example.com","aliases":["www.site1.example.com"],"php_version":"","doc_root":""}'
step "bad domain / doc root (400)";          c cust1 POST /sites '{"domain":"evil.com;rm","aliases":[],"php_version":"","doc_root":""}'; c cust1 PUT /sites/$S1 '{"aliases":[],"php_version":"8.3","doc_root":"../../etc"}'

step "symlink attack: ~/www/evil.example.com -> /etc (expect agent refusal, /etc untouched)"
c admin DELETE /sites/$S2 >/dev/null
runuser -u cust1 -- ln -s /etc /home/cust1/www/evil.example.com
c cust1 POST /sites '{"domain":"evil.example.com","aliases":[],"php_version":"","doc_root":"x"}'
stat -c '/etc owner=%U' /etc; ls /etc/x 2>/dev/null || echo "/etc/x not created"
c cust1 GET /sites; jq -c '[.[].domain]' /tmp/out

step "SSL via Pebble ACME"
c cust1 POST /sites/$S1/ssl; T=$(jq .id /tmp/out); wait_task cust1 $T
c cust1 GET /sites; jq -c '.[] | {domain, ssl_status, ssl_expires_at}' /tmp/out
web site1.example.com /info.php
webs www.site1.example.com /info.php
openssl s_client -connect 127.0.0.1:443 -servername site1.example.com </dev/null 2>/dev/null | openssl x509 -noout -issuer -ext subjectAltName 2>/dev/null | tr '\n' ' '; echo

step "change doc root to public (keeps HTTPS)"
c cust1 PUT /sites/$S1 '{"aliases":["www.site1.example.com"],"php_version":"8.3","doc_root":"public"}'; webs site1.example.com

step "suspend cust1 -> 503, pool removed"; c admin PATCH /users/$U1/status '{"status":"suspended"}' >/dev/null
webs site1.example.com; ls /etc/php/8.3/fpm/pool.d/ | tr '\n' ' '; echo
step "reactivate -> 200";                    c admin PATCH /users/$U1/status '{"status":"active"}' >/dev/null; webs site1.example.com

step "delete account cust1 -> vhosts, pools, certs removed"
c admin DELETE /users/$U1
ls /etc/nginx/conf.d/xpanel/cust1/ | wc -l; ls /etc/php/8.3/fpm/pool.d/ | tr '\n' ' '; ls /etc/xpanel/ssl/sites/ | wc -l; web site1.example.com | cut -c1-3
nginx -t 2>&1 | tail -1
