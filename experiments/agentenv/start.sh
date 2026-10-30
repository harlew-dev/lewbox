#!/usr/bin/env bash
set -euo pipefail
lab_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$lab_dir/lab.env.sh"
exec flock -n "$LAB_ROOT/run/server.lock" \
    "$AGENTENV_SOURCE/scripts/run-with-capabilities.sh" \
    "$LAB_ROOT/server/server" --config "$AENV_CONFIG_PATH"
