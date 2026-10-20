#!/usr/bin/env bash
B=https://127.0.0.1:8888
H=(-H X-XPanel:1 -H Content-Type:application/json)
c() { local jar=$1 m=$2 p=$3 d=${4:-}
  code=$(curl -sk -b /tmp/$jar -c /tmp/$jar -o /tmp/out -w '%{http_code}' "${H[@]}" -X "$m" "$B/api$p" ${d:+-d "$d"})
  echo "$code $(head -c 150 /tmp/out)"; }
step() { echo; echo "### $*"; }
SFTP() { printf "$3" > /tmp/b; sshpass -p "$2" sftp -o BatchMode=no -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -b /tmp/b "$1@127.0.0.1" 2>&1 | grep -v "^sftp>" | tr '\n' ' '; echo; }
FTP() { lftp -u "$1,$2" -e "set ftp:ssl-allow ${4:-no}; set ssl:verify-certificate no; set net:max-retries 1; set net:timeout 8; $3; bye" ftp://127.0.0.1 2>&1 | tr '\n' ' '; echo; }
export DEBIAN_FRONTEND=noninteractive; apt-get install -y -qq sshpass lftp openssh-client >/dev/null 2>&1

c admin POST /auth/login '{"username":"admin","password":"Admin@123!"}' >/dev/null
. "$(dirname "$0")/lib.sh"; xp_cleanup_on_exit "fcust fother" "FTP2"
c admin POST /packages '{"name":"FTP2","kind":"user","disk_mb":500,"max_ftp":2,"max_sites":1}' >/dev/null; P=$(jq .id /tmp/out)
c admin POST /users "{\"username\":\"fcust\",\"password\":\"Client12345\",\"role\":\"user\",\"package_id\":$P}" >/dev/null; U=$(jq .id /tmp/out)
c admin POST /users '{"username":"fother","password":"Client12345","role":"user"}' >/dev/null; O=$(jq .id /tmp/out)
c fcust POST /auth/login '{"username":"fcust","password":"Client12345"}' >/dev/null
c fother POST /auth/login '{"username":"fother","password":"Client12345"}' >/dev/null
echo hello-sftp > /tmp/s.txt

step "SFTP with panel password: chroot, can write www, cannot see /etc"
stat -c 'home %U:%G %a' /home/fcust
SFTP fcust Client12345 'ls /\nput /tmp/s.txt www/s.txt\nls www\ncd /etc\n'
stat -c 'uploaded owner %U' /home/fcust/www/s.txt
step "SSH shell refused";  sshpass -p Client12345 ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR fcust@127.0.0.1 id 2>&1 | tail -1
step "reset password -> old fails, new works"
c admin PATCH /users/$U/password '{"password":"NewClient12345"}'
SFTP fcust Client12345 'ls /\n'; SFTP fcust NewClient12345 'ls /\n'

step "FTP account fcust_site -> ~/www/site1 (chrooted)"
c admin POST /ftp "{\"user_id\":$U,\"name\":\"site\",\"dir\":\"www/site1\",\"password\":\"Ftp-Pass_2026!\"}"; F1=$(jq .id /tmp/out)
echo ftpdata > /tmp/f.txt
FTP fcust_site 'Ftp-Pass_2026!' 'put /tmp/f.txt; ls; cd ..; ls'
ls -l /home/fcust/www/site1/ | tail -1
step "FTPS (explicit TLS)";  FTP fcust_site 'Ftp-Pass_2026!' 'set ftp:ssl-force true; ls' yes
step "second ok, third 409, bad dir 400"
c admin POST /ftp "{\"user_id\":$U,\"name\":\"all\",\"dir\":\"\",\"password\":\"Ftp-Pass_2026!\"}"
c admin POST /ftp "{\"user_id\":$U,\"name\":\"x3\",\"dir\":\"www\",\"password\":\"Ftp-Pass_2026!\"}"
c admin POST /ftp "{\"user_id\":$O,\"name\":\"bad\",\"dir\":\"../fcust\",\"password\":\"Ftp-Pass_2026!\"}"
step "fother cannot see/delete fcust FTP";  c fother DELETE /ftp/$F1; c fother GET /ftp
step "suspend -> FTP + SFTP refused; activate -> ok"
c admin PATCH /users/$U/status '{"status":"suspended"}' >/dev/null
FTP fcust_site 'Ftp-Pass_2026!' 'ls'; SFTP fcust NewClient12345 'ls /\n'
c admin PATCH /users/$U/status '{"status":"active"}' >/dev/null
FTP fcust_site 'Ftp-Pass_2026!' 'ls'; SFTP fcust NewClient12345 'ls /\n'
step "FTP password change";  c fcust PATCH /ftp/$F1/password '{"password":"Ftp-New_2026!"}'; FTP fcust_site 'Ftp-Pass_2026!' 'ls'; FTP fcust_site 'Ftp-New_2026!' 'ls'

step "FILE MANAGER (as fcust)"
c fcust GET '/files?path=/www'; jq -c '[.entries[].name]' /tmp/out
c fcust POST /files/op '{"request":{"op":"mkdir","path":"/www/app"}}'
c fcust POST /files/op '{"request":{"op":"write","path":"/www/app/index.php","content":"<?php echo \"hi\";"}}'
c fcust POST /files/op '{"request":{"op":"read","path":"/www/app/index.php"}}'
c fcust POST /files/op '{"request":{"op":"chmod","path":"/www/app/index.php","mode":"600"}}'; stat -c '%U %a' /home/fcust/www/app/index.php
c fcust POST /files/op '{"request":{"op":"rename","path":"/www/app/index.php","to":"/www/app/main.php"}}'
c fcust POST /files/op '{"request":{"op":"compress","paths":["/www/app"],"to":"/www/app.zip"}}'
c fcust POST /files/op '{"request":{"op":"extract","path":"/www/app.zip","to":"/www/restored"}}'; find /home/fcust/www/restored -type f
step "upload 5MB + download, compare sha256"
head -c 5000000 /dev/urandom > /tmp/big.bin
curl -sk -b /tmp/fcust -H X-XPanel:1 --data-binary @/tmp/big.bin "$B/api/files/upload?path=/www/big.bin" -w " %{http_code}\n"
curl -sk -b /tmp/fcust "$B/api/files/download?path=/www/big.bin" -o /tmp/back.bin -D /tmp/hdr; grep -i "content-disposition" /tmp/hdr | tr -d '\r'
sha256sum /tmp/big.bin /tmp/back.bin | awk '{print $1}' | uniq | wc -l | sed 's/^1$/sha256 identical/'
step "escape attempts"
c fcust POST /files/op '{"request":{"op":"read","path":"/../../etc/passwd"}}'
c fcust POST /files/op '{"request":{"op":"delete","paths":["/"]}}'
c fcust POST /files/op '{"request":{"op":"write","path":"/root.txt","content":"x"}}'
c fcust GET "/files?user_id=$O&path=/"
c fother GET "/files?user_id=$U&path=/www"
c admin GET "/files?user_id=$U&path=/www/restored/app"; jq -c '[.entries[].name]' /tmp/out
c fcust POST /files/op '{"request":{"op":"delete","paths":["/www/restored","/www/app.zip","/www/big.bin"]}}'

step "phpMyAdmin install (task) + served on panel port"
c admin POST /phpmyadmin; T=$(jq .id /tmp/out)
if ! [[ "$T" =~ ^[1-9][0-9]*$ ]]; then
  echo "FAIL phpMyAdmin installation did not return a task ID"; cat /tmp/out; exit 1
fi
for i in $(seq 1 120); do st=$(curl -sk -b /tmp/admin "${H[@]}" $B/api/tasks/$T | jq -r .status); [ "$st" = done ] || [ "$st" = failed ] && break; sleep 2; done
curl -sk -b /tmp/admin "${H[@]}" $B/api/tasks/$T | jq -r '.status + " | " + (.log | split("\n") | map(select(length>0)) | .[-2:] | join(" / "))'
[ "$st" = done ] || { echo "FAIL phpMyAdmin installation task: $st"; exit 1; }
c admin GET /phpmyadmin
curl -sk -o /dev/null -w "panel port redirects %{http_code} %{redirect_url}\n" $B/phpmyadmin/
curl -sk -o /tmp/pma.html -w "phpmyadmin %{http_code} " https://127.0.0.1:8890/phpmyadmin/; grep -o 'name="pma_username"' /tmp/pma.html | head -1
curl -sk -o /dev/null -w "libraries %{http_code}\n" https://127.0.0.1:8890/phpmyadmin/libraries/classes/Config.php

step "delete account -> FTP logins + Linux user removed"
c admin DELETE /users/$U; pure-pw show fcust_site -f /etc/pure-ftpd/pureftpd.passwd >/dev/null 2>&1 && echo "ftp still exists" || echo "ftp login gone"; id fcust 2>&1 | tail -1
