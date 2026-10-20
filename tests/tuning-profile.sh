#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
. "$ROOT/installer/tuning.sh"
for ram in 768 1024 2048 4096 8192 16384 65536; do
  for cpu in 1 2 8 64; do
    xpanel_profile "$ram" "$cpu"
    # Fixed database caches + estimated connection overhead leave >= 50% RAM.
    # Two PG clusters, conservative 8MB per Maria connection / 4MB per PG backend.
    db=$((T_MY_POOL + 2*T_PG_SHARED + T_MY_CONNECTIONS*8 + 2*T_PG_CONNECTIONS*4))
    ((T_MY_CONNECTIONS <= 200 && T_MY_POOL <= 4096 && T_PG_SHARED <= 256))
    ((db < ram / 2)) || { echo "Unsafe baseline: $ram MB / $cpu CPU ($db MB)"; exit 1; }
    ((T_NGINX_CONNECTIONS >= 2048 && T_OPCACHE <= 64))
  done
done
if xpanel_profile garbage 1; then echo 'Invalid profile accepted'; exit 1; fi
echo 'PASS: 28 RAM/CPU profiles; conservative baseline and bounded caches/connections'
