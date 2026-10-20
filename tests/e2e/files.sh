#!/usr/bin/env bash
# File manager: edit, copy, move, compress (zip, tar.gz), extract, isolation.
set -u
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
FAIL=0
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+--data-binary "$d"}; }
f() { c fm POST /files/op "{\"request\":$1}"; }
ok()  { echo "PASS $*"; }
bad() { echo "FAIL $*"; FAIL=1; }
expect() { [ "$1" = "$2" ] && ok "$3 ($1)" || { bad "$3: got $1, want $2"; head -c 300 /tmp/out; echo; }; }

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
c admin POST /users '{"username":"fmuser","password":"Client12345","role":"user"}' >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"fmother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c fm POST /auth/login '{"username":"fmuser","password":"Client12345"}' >/dev/null
c fo POST /auth/login '{"username":"fmother","password":"Client12345"}' >/dev/null
H0=/home/fmuser

expect "$(f '{"op":"mkdir","path":"/www/app"}')" 200 "mkdir"
expect "$(f '{"op":"write","path":"/www/app/index.php","content":"<?php echo 1;"}')" 200 "create file"
expect "$(f '{"op":"read","path":"/www/app/index.php"}')" 200 "read file"
expect "$(jq -r .content /tmp/out)" "<?php echo 1;" "editor content"
expect "$(f '{"op":"write","path":"/www/app/index.php","content":"<?php echo 2;"}')" 200 "save edit"
expect "$(cat $H0/www/app/index.php)" "<?php echo 2;" "edit on disk"
expect "$(stat -c %U $H0/www/app/index.php)" fmuser "owned by the account"

echo "### copy, move"
expect "$(f '{"op":"mkdir","path":"/www/dest"}')" 200 "mkdir dest"
expect "$(f '{"op":"copy","paths":["/www/app"],"to":"/www/dest"}')" 200 "copy directory"
[ -f $H0/www/dest/app/index.php ] && [ -f $H0/www/app/index.php ] && ok "copied, original kept" || bad "copy"
expect "$(f '{"op":"copy","paths":["/www/app/index.php"],"to":"/www/app"}')" 200 "copy into same folder"
[ -f "$H0/www/app/index (copy).php" ] && ok "copy gets a free name" || bad "no '(copy)' file"
expect "$(f '{"op":"copy","paths":["/www/app"],"to":"/www/app"}')" 400 "copy into itself refused"
expect "$(f '{"op":"move","paths":["/www/app/index (copy).php"],"to":"/www/dest"}')" 200 "move"
[ -f "$H0/www/dest/index (copy).php" ] && [ ! -e "$H0/www/app/index (copy).php" ] && ok "moved" || bad "move"
expect "$(f '{"op":"move","paths":["/www/dest"],"to":"/www/dest/app"}')" 400 "move into itself refused"
expect "$(f '{"op":"move","paths":["/www/app/index.php"],"to":"/nope"}')" 400 "missing destination refused"

echo "### compress, extract"
expect "$(f '{"op":"compress","paths":["/www/app"],"to":"/www/app.zip"}')" 200 "zip"
expect "$(f '{"op":"compress","paths":["/www/app"],"to":"/www/app.tar.gz"}')" 200 "tar.gz"
tar -tzf $H0/www/app.tar.gz | grep -q "app/index.php" && ok "tar.gz content" || bad "tar.gz content"
expect "$(f '{"op":"extract","path":"/www/app.zip","to":"/www/unzipped"}')" 200 "extract zip into new folder"
[ -f $H0/www/unzipped/app/index.php ] && ok "zip extracted" || bad "zip not extracted"
expect "$(f '{"op":"extract","path":"/www/app.tar.gz","to":"/www/untarred"}')" 200 "extract tar.gz"
[ "$(cat $H0/www/untarred/app/index.php 2>/dev/null)" = "<?php echo 2;" ] && ok "tar.gz extracted" || bad "tar.gz not extracted"
# An uploaded archive with "../" entries stays inside the target folder.
cd /tmp && rm -rf evil && mkdir -p evil && echo pwned > evil/x && tar -czf evil.tgz --absolute-names --transform 's#^evil/x#../../../../tmp/escaped#' evil/x
curl -sk -b /tmp/fm -H X-XPanel:1 --data-binary @/tmp/evil.tgz "$B/api/files/upload?path=/www/evil.tgz" -o /dev/null
rm -f /tmp/escaped
expect "$(f '{"op":"extract","path":"/www/evil.tgz","to":"/www/ev"}')" 200 "extract crafted archive"
[ ! -e /tmp/escaped ] && ok "path traversal contained" || bad "archive escaped the home"
expect "$(f '{"op":"copy","paths":["/../../etc/passwd"],"to":"/www"}')" 400 "no access outside the home"

echo "### other accounts"
expect "$(c fo POST /files/op "{\"user_id\":$U,\"request\":{\"op\":\"list\",\"path\":\"/www\"}}")" 404 "another user cannot browse"
expect "$(c fo POST /files/op '{"request":{"op":"copy","paths":["/../fmuser/www/app"],"to":"/"}}')" 400 "cannot reach another home by path"

for id in $U $O; do c admin DELETE /users/$id >/dev/null; done
[ "$FAIL" = 0 ] && echo "PASS files" || { echo "FAIL files"; exit 1; }
