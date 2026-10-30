#!/usr/bin/env bash
# Source this from a lab command. Runtime credentials stay in LAB_ROOT.
export LAB_ROOT="${LAB_ROOT:-/home/homelab/.local/share/lewbox-agentenv-lab}"
export AGENTENV_SOURCE="${AGENTENV_SOURCE:-/home/homelab/dev/AgentENV}"
export AENV_HOME_PATH="${AENV_HOME_PATH:-$LAB_ROOT/state}"
export AENV_RUNTIME_PATH="${AENV_RUNTIME_PATH:-$LAB_ROOT/run}"
export AENV_DEPS_PATH="$LAB_ROOT/server/deps"
export AENV_CONFIG_PATH="${AENV_CONFIG_PATH:-$LAB_ROOT/config.toml}"
export AENV_UBLK_DAEMON_BINARY_PATH="$LAB_ROOT/server/ublk/uvm-ublk-daemon"
export AENV_UBLK_DAEMON_METRICS_LISTEN_ADDR="127.0.0.1:19103"
export API_ADDR="127.0.0.1:18080"
export XDG_CONFIG_HOME="$LAB_ROOT/client-config"
export RUST_LOG="info"
export PATH="$LAB_ROOT/cli:$PATH"
