#!/usr/bin/env bash
# Minimal lab prerequisites; avoids the upstream persistent service installer.
set -euo pipefail
lab_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$lab_dir/lab.env.sh"
mkdir -p "$LAB_ROOT/host-before"
if [[ ! -f "$LAB_ROOT/host-before/ip_forward" ]]; then
    cat /proc/sys/net/ipv4/ip_forward > "$LAB_ROOT/host-before/ip_forward"
    getfacl -p /dev/kvm > "$LAB_ROOT/host-before/kvm.acl"
    if [[ -d /sys/module/ublk_drv ]]; then
        touch "$LAB_ROOT/host-before/ublk-was-loaded"
    fi
fi
sudo modprobe ublk_drv ublks_max=64
if ! id -nG "$(id -un)" | tr ' ' '\n' | rg -qx kvm; then
    sudo usermod -aG kvm "$(id -un)"
    touch "$LAB_ROOT/host-before/added-kvm-group"
fi
sudo setfacl -m "u:$(id -un):rw" /dev/kvm /dev/ublk-control
rule="$LAB_ROOT/ublk-lab.rules"
cat > "$rule" <<EOF
# Lewbox AgentENV experiment: remove with cleanup-host.sh after stopping the lab.
KERNEL=="ublk-control", MODE="0660", GROUP="$(id -gn)"
KERNEL=="ublkc*", MODE="0660", GROUP="$(id -gn)"
KERNEL=="ublkb*", MODE="0660", GROUP="$(id -gn)"
ACTION=="add", SUBSYSTEM=="block", KERNEL=="ublkb*", ATTR{queue/scheduler}="none"
EOF
sudo install -m 0644 "$rule" /etc/udev/rules.d/99-lewbox-agentenv-lab.rules
sudo udevadm control --reload-rules
sudo udevadm trigger --sysname-match='ublk*'
sudo udevadm settle
if [[ ! -e /etc/overlaybd/overlaybd.json ]]; then
    sudo install -d -m 0750 -g "$(id -gn)" /etc/overlaybd
    sudo install -m 0640 -g "$(id -gn)" \
        "$AENV_DEPS_PATH/overlaybd/etc/overlaybd/overlaybd.json" /etc/overlaybd/overlaybd.json
    touch "$LAB_ROOT/host-before/created-overlaybd-config"
fi
sudo sysctl -w net.ipv4.ip_forward=1
