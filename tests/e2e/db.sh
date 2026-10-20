#!/usr/bin/env bash
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"})
  echo "$code $(head -c 150 /tmp/out)"; }
step() { echo; echo "### $*"; }
my() { local u=$1 p=$2 h=$3; shift 3; mariadb -u"$u" -p"$p" -h"$h" --batch --skip-column-names "$@" 2>&1 | head -3 | tr '\n' ' '; echo; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "dbcust other" "DB2"
c admin POST /packages '{"name":"DB2","kind":"user","disk_mb":500,"max_dbs":2,"max_sites":1}' >/dev/null; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"dbcust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"other","password":"Client12345","role":"user"}' >/dev/null
c dbcust POST /auth/login '{"username":"dbcust","password":"Client12345"}' >/dev/null
c other POST /auth/login '{"username":"other","password":"Client12345"}' >/dev/null
PW='Sh0p-Pass_2026!'

step "create dbcust_shop";       c dbcust POST /databases "{\"name\":\"shop\",\"password\":\"$PW\"}"; D1=$(jq .id /tmp/out)
step "use it via socket + TCP";  my dbcust_shop "$PW" localhost dbcust_shop -e "CREATE TABLE t(x INT); INSERT INTO t VALUES (1),(2); SELECT COUNT(*) FROM t;"
                                 my dbcust_shop "$PW" 127.0.0.1 dbcust_shop -e "SELECT SUM(x) FROM t;"
step "no access to mysql.* or other DBs"; my dbcust_shop "$PW" localhost mysql -e "SELECT User FROM user LIMIT 1;"
step "grants";                   mariadb -e "SHOW GRANTS FOR 'dbcust_shop'@'localhost'" --batch --skip-column-names | tail -1
step "list with size";           c dbcust GET /databases; jq -c '[.[] | {name, size_bytes}]' /tmp/out
step "second db ok, third 409";  c dbcust POST /databases "{\"name\":\"blog\",\"password\":\"$PW\"}"; D2=$(jq .id /tmp/out); c dbcust POST /databases "{\"name\":\"x3\",\"password\":\"$PW\"}"
step "bad name / bad password (400)"; c dbcust POST /databases "{\"name\":\"a-b\",\"password\":\"$PW\"}"; c dbcust POST /databases "{\"name\":\"ok\",\"password\":\"bad'quote12345\"}"
step "other user cannot see/delete (404, empty list)"; c other DELETE /databases/$D1; c other GET /databases
step "change password: old fails, new works"
c dbcust PATCH /databases/$D1/password '{"password":"N3w-Pass_2026!"}'
my dbcust_shop "$PW" localhost dbcust_shop -e "SELECT 1;"; my dbcust_shop 'N3w-Pass_2026!' localhost dbcust_shop -e "SELECT 'new ok';"
step "suspend -> DB login locked; activate -> works"
c admin PATCH /users/$U/status '{"status":"suspended"}' >/dev/null; my dbcust_shop 'N3w-Pass_2026!' localhost dbcust_shop -e "SELECT 1;"
c admin PATCH /users/$U/status '{"status":"active"}' >/dev/null;    my dbcust_shop 'N3w-Pass_2026!' localhost dbcust_shop -e "SELECT 'unlocked';"
step "delete blog";              c dbcust DELETE /databases/$D2; mariadb -e "SHOW DATABASES LIKE 'dbcust_%'" --batch --skip-column-names | tr '\n' ' '; echo
step "delete account -> DBs and DB users dropped"
c admin DELETE /users/$U; mariadb -e "SHOW DATABASES LIKE 'dbcust_%'; SELECT COUNT(*) FROM mysql.global_priv WHERE User LIKE 'dbcust%';" --batch --skip-column-names | tr '\n' ' '; echo
step "MariaDB listens on localhost only"; ss -ltn | grep 3306
