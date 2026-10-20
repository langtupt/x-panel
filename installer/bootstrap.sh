#!/usr/bin/env bash
# Compatibility entry point: the bootstrap is now at /install.sh in the repository.
set -euo pipefail
command -v curl >/dev/null || { apt-get update -q && apt-get install -y -q curl ca-certificates; }
curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | bash
