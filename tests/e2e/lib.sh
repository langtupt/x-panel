# shellcheck shell=bash
# Shared by the e2e suites. Sourced after the admin login (cookie /tmp/admin).
#
# xp_cleanup "user1 user2" "Package name" "Prefix*" …
# deletes the suite's accounts (in the given order: customers before their
# reseller) and packages, so a suite can run again on a real server and
# never leaves test data behind — call it before setup and on EXIT.
XP_B=${B:-https://127.0.0.1:8888}

xp_api() { curl -sk -b "${XP_JAR:-/tmp/admin}" -c "${XP_JAR:-/tmp/admin}" -H X-XPanel:1 -H Content-Type:application/json "$@"; }

xp_cleanup() {
  local users=$1 name id pat
  shift
  # The suite may just have restarted the API.
  for _ in $(seq 1 30); do [ "$(curl -sk -o /dev/null -w '%{http_code}' "$XP_B/api/health")" = 200 ] && break; sleep 1; done
  xp_api -o /dev/null -X POST "$XP_B/api/auth/login" --data-binary "{\"username\":\"admin\",\"password\":\"${XP_ADMIN_PW:-Admin@123!}\"}"
  for name in $users; do
    id=$(xp_api "$XP_B/api/users" | jq -r --arg n "$name" '.users[]? | select(.username == $n) | .id')
    [ -n "$id" ] && xp_api -o /dev/null -X DELETE "$XP_B/api/users/$id"
  done
  for pat in "$@"; do
    for id in $(xp_api "$XP_B/api/packages" | jq -r --arg p "$pat" '.packages[]?
        | select(if ($p | endswith("*")) then (.name | startswith($p | rtrimstr("*"))) else .name == $p end) | .id'); do
      xp_api -o /dev/null -X DELETE "$XP_B/api/packages/$id"
    done
  done
  return 0
}

# xp_cleanup_on_exit "users" "packages"… : clean now and again when the suite ends.
xp_cleanup_on_exit() {
  xp_cleanup "$@"
  local args
  printf -v args '%q ' "$@"
  # shellcheck disable=SC2064
  trap "xp_cleanup $args" EXIT
}
