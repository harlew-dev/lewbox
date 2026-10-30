#!/usr/bin/env bash
set -euo pipefail
lab_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$lab_dir/lab.env.sh"
for pid in $(pgrep -u "$(id -u)" -x server || true); do
    if [[ "$(sudo -n readlink "/proc/$pid/exe" || true)" == "$LAB_ROOT/server/server" ]]; then
        kill -TERM "$pid"
        for ((attempt=0; attempt<60; attempt++)); do
            if ! kill -0 "$pid" 2>/dev/null; then break; fi
            sleep 1
        done
        if kill -0 "$pid" 2>/dev/null; then
            echo "Lab server $pid did not stop gracefully" >&2
            exit 1
        fi
    fi
done
