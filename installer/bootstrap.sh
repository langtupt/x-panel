#!/usr/bin/env bash
# Giữ cho các hướng dẫn cũ: trình cài đặt nay nằm ở /install.sh của repo.
set -euo pipefail
command -v curl >/dev/null || { apt-get update -q && apt-get install -y -q curl ca-certificates; }
curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | bash
