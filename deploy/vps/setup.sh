#!/usr/bin/env bash
# Prepare an Ubuntu 22.04/24.04 VPS to run the Lewbox orchestrator, and other
# apps, under Docker Compose behind Caddy. Runs as root from this folder;
# `deploy.sh setup` copies the folder over and runs it. Safe to re-run.
#
#   DOMAIN=lewbox.example.com [ADMIN_EMAIL=you@example.com] setup.sh base
#   setup.sh lock-ssh       key-only SSH and no root login. Run it only after
#                           checking that $ADMIN_USER can log in and sudo.
#
# ADMIN_USER (default ops) is the sudo user that gets root's SSH keys.
set -euo pipefail

ADMIN_USER=${ADMIN_USER:-ops}
WEB_SUBNET=172.30.0.0/24
here=$(cd "$(dirname "$0")" && pwd)

[ "$(id -u)" = 0 ] || { echo "setup.sh: run as root" >&2; exit 1; }
step() { printf '\n== %s\n' "$*"; }

base() {
  : "${DOMAIN:?set DOMAIN, e.g. DOMAIN=lewbox.example.com}"
  [[ $DOMAIN =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || { echo "setup.sh: bad DOMAIN" >&2; exit 2; }
  export DEBIAN_FRONTEND=noninteractive

  step "packages"
  apt-get update -q
  apt-get -q -y -o Dpkg::Options::=--force-confold full-upgrade
  apt-get -q -y install ca-certificates curl gnupg openssl sqlite3 ufw unattended-upgrades

  step "admin user $ADMIN_USER"
  id "$ADMIN_USER" >/dev/null 2>&1 || adduser --disabled-password --gecos "" "$ADMIN_USER"
  usermod -aG sudo "$ADMIN_USER"
  install -d -m 0700 -o "$ADMIN_USER" -g "$ADMIN_USER" "/home/$ADMIN_USER/.ssh"
  if [ ! -s "/home/$ADMIN_USER/.ssh/authorized_keys" ]; then
    install -m 0600 -o "$ADMIN_USER" -g "$ADMIN_USER" /root/.ssh/authorized_keys "/home/$ADMIN_USER/.ssh/authorized_keys"
  fi
  # No password exists for this user: SSH keys are the only way in.
  printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$ADMIN_USER" > "/etc/sudoers.d/90-$ADMIN_USER"
  chmod 0440 "/etc/sudoers.d/90-$ADMIN_USER"

  step "deploy user (for CI; may only run lewbox-deploy)"
  id deploy >/dev/null 2>&1 || adduser --system --group --shell /bin/bash --home /var/lib/deploy deploy
  install -d -m 0700 -o deploy -g deploy /var/lib/deploy/.ssh
  [ -f /var/lib/deploy/.ssh/authorized_keys ] || install -m 0600 -o deploy -g deploy /dev/null /var/lib/deploy/.ssh/authorized_keys
  printf 'deploy ALL=(root) NOPASSWD: /usr/local/sbin/lewbox-deploy\n' > /etc/sudoers.d/91-deploy
  chmod 0440 /etc/sudoers.d/91-deploy
  visudo -cq

  step "lewbox user (UID 10001, the container's user)"
  getent group lewbox >/dev/null || groupadd --system --gid 10001 lewbox
  id lewbox >/dev/null 2>&1 || useradd --system --uid 10001 --gid 10001 --no-create-home --home-dir /srv/lewbox --shell /usr/sbin/nologin lewbox

  step "firewall: SSH, HTTP, HTTPS"
  ufw default deny incoming >/dev/null
  ufw default allow outgoing >/dev/null
  ufw allow OpenSSH >/dev/null
  ufw allow 80/tcp >/dev/null
  ufw allow 443/tcp >/dev/null
  ufw allow 443/udp >/dev/null
  ufw --force enable
  ufw status verbose

  step "automatic security updates (reboot at 04:00 UTC when needed)"
  cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
  cat > /etc/apt/apt.conf.d/52lewbox-unattended <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:00";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
EOF
  systemctl enable --now unattended-upgrades >/dev/null

  step "2 GB swap"
  if ! swapon --show=NAME --noheadings | grep -qx /swapfile; then
    [ -f /swapfile ] || { fallocate -l 2G /swapfile; chmod 0600 /swapfile; mkswap /swapfile >/dev/null; }
    swapon /swapfile
  fi
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  echo 'vm.swappiness=10' > /etc/sysctl.d/90-swappiness.conf
  sysctl -q -p /etc/sysctl.d/90-swappiness.conf

  step "Docker (from Docker's apt repository)"
  if ! command -v docker >/dev/null; then
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    . /etc/os-release
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
      > /etc/apt/sources.list.d/docker.list
    apt-get update -q
    apt-get -q -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  fi
  install -d -m 0755 /etc/docker
  want='{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"},"live-restore":true}'
  if [ "$(cat /etc/docker/daemon.json 2>/dev/null)" != "$want" ]; then
    echo "$want" > /etc/docker/daemon.json
    systemctl restart docker
  fi
  systemctl enable --now docker >/dev/null
  docker network inspect web >/dev/null 2>&1 || docker network create --subnet "$WEB_SUBNET" web >/dev/null
  docker --version; docker compose version

  step "stack in /opt/stack, data in /srv/lewbox"
  install -d -m 0755 /opt/stack /opt/stack/sites /opt/stack/lewbox
  install -m 0644 "$here/stack/compose.yaml" "$here/stack/Caddyfile" /opt/stack/
  install -m 0644 "$here/stack/lewbox/Dockerfile" /opt/stack/lewbox/
  if [ -f /opt/stack/.env ]; then
    sed -i "s/^DOMAIN=.*/DOMAIN=$DOMAIN/" /opt/stack/.env
  else
    printf 'DOMAIN=%s\nLEWBOX_TAG=\n' "$DOMAIN" > /opt/stack/.env
  fi
  chmod 0644 /opt/stack/.env
  install -d -m 0700 -o lewbox -g lewbox /srv/lewbox /var/backups/lewbox
  install -m 0755 "$here/bin/lewbox-deploy" "$here/bin/lewbox-backup" /usr/local/sbin/

  if [ -n "${ADMIN_EMAIL:-}" ] && [ ! -f /srv/lewbox/lewbox.db ] && [ ! -f /opt/stack/bootstrap.env ]; then
    umask 077
    printf 'LEWBOX_BOOTSTRAP_ADMIN_EMAIL=%s\nLEWBOX_BOOTSTRAP_ADMIN_PASSWORD=%s\n' \
      "$ADMIN_EMAIL" "$(openssl rand -base64 18 | tr -d '/+=')" > /opt/stack/bootstrap.env
    umask 022
    echo "first admin $ADMIN_EMAIL: password written to /opt/stack/bootstrap.env (root only)"
  fi

  step "nightly backup timer"
  install -m 0644 "$here/systemd/lewbox-backup.service" "$here/systemd/lewbox-backup.timer" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now lewbox-backup.timer >/dev/null
  systemctl list-timers lewbox-backup.timer --no-pager | head -2

  step "done"
  echo "Next: check 'ssh $ADMIN_USER@<host> sudo true' works, then run: setup.sh lock-ssh"
}

lock_ssh() {
  [ -s "/home/$ADMIN_USER/.ssh/authorized_keys" ] || { echo "setup.sh: $ADMIN_USER has no SSH keys; refusing" >&2; exit 1; }
  # sshd keeps the first value it reads, and Ubuntu's cloud images ship
  # 50-cloud-init.conf with PasswordAuthentication yes, so this file sorts first.
  cat > /etc/ssh/sshd_config.d/00-hardening.conf <<EOF
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
X11Forwarding no
MaxAuthTries 3
AllowUsers $ADMIN_USER deploy
EOF
  sshd -t
  systemctl reload ssh
  sshd -T | grep -E '^(permitrootlogin|passwordauthentication|kbdinteractiveauthentication|allowusers) '
}

case ${1:-} in
  base) base ;;
  lock-ssh) lock_ssh ;;
  *) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
