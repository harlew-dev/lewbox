#!/usr/bin/env bash
# Restore the recorded host prerequisites; retain downloads and experiment data.
set -euo pipefail
lab_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$lab_dir/lab.env.sh"
bash "$lab_dir/stop.sh"
for pid in $(pgrep -u "$(id -u)" -x silo || true); do
    if [[ "$(readlink "/proc/$pid/exe" || true)" == "$LAB_ROOT/silo/silo" ]]; then
        kill -TERM "$pid"
    fi
done
if [[ ! -d "$LAB_ROOT/host-before" ]]; then exit 0; fi
# Remove only the five global rules installed for this lab's default subnet.
# Startup/shutdown experimentation left a copy after the final server exited.
python3 - <<'PY'
import subprocess
rules = [
    ('filter', 'INPUT', '-i veth-+ -s 10.11.0.0/16 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT'),
    ('filter', 'INPUT', '-i veth-+ -s 10.11.0.0/16 -j REJECT'),
    ('filter', 'FORWARD', '-i veth-+ -s 10.11.0.0/16 -j ACCEPT'),
    ('filter', 'FORWARD', '-o veth-+ -d 10.11.0.0/16 -m state --state RELATED,ESTABLISHED -j ACCEPT'),
    ('nat', 'POSTROUTING', '-s 10.11.0.0/16 -j MASQUERADE'),
]
for table, chain, rule in rules:
    base = ['sudo', '-n', 'iptables', '-w', '5', '-t', table]
    while subprocess.run([*base, '-C', chain, *rule.split()], capture_output=True).returncode == 0:
        subprocess.run([*base, '-D', chain, *rule.split()], check=True)
PY
sudo rm -f /etc/udev/rules.d/99-lewbox-agentenv-lab.rules
sudo udevadm control --reload-rules
if [[ -f "$LAB_ROOT/host-before/created-overlaybd-config" ]] && \
   sudo cmp -s /etc/overlaybd/overlaybd.json "$AENV_DEPS_PATH/overlaybd/etc/overlaybd/overlaybd.json"; then
    sudo rm /etc/overlaybd/overlaybd.json
    sudo rmdir --ignore-fail-on-non-empty /etc/overlaybd
fi
if [[ -f "$LAB_ROOT/host-before/added-kvm-group" ]] && \
   id -nG "$(id -un)" | tr ' ' '\n' | rg -qx kvm; then
    sudo gpasswd -d "$(id -un)" kvm
fi
sudo setfacl --restore="$LAB_ROOT/host-before/kvm.acl"
sudo sysctl -w "net.ipv4.ip_forward=$(cat "$LAB_ROOT/host-before/ip_forward")"
if [[ ! -f "$LAB_ROOT/host-before/ublk-was-loaded" ]] && [[ -d /sys/module/ublk_drv ]]; then
    # The kernel refuses unloading if another process still uses the module.
    sudo modprobe -r ublk_drv
fi
echo "Restored lab host prerequisites; source, binaries and state retained at $LAB_ROOT"
