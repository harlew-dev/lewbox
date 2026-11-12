#!/usr/bin/env bash
# Manage the Lewbox VPS from your machine, over SSH.
#
#   DEPLOY_HOST=ops@lewbox.example.com deploy/vps/deploy.sh COMMAND
#
#   setup DOMAIN [ADMIN_EMAIL]   prepare the server (setup.sh base); re-runnable
#   lock-ssh                     key-only SSH, no root login (setup.sh lock-ssh)
#   vX.Y.Z | latest              deploy that published release
#   status                       running version and health
#   rollback                     back to the previous version's image
#   backup                       back up the database and keyring now
#   logs                         follow Lewbox's logs
#
# For the very first `setup`, DEPLOY_HOST is root@<ip>; afterwards use the
# admin user it creates (ops by default).
set -euo pipefail

: "${DEPLOY_HOST:?set DEPLOY_HOST, e.g. DEPLOY_HOST=ops@lewbox.example.com}"
here=$(cd "$(dirname "$0")" && pwd)
ssh_() { ssh -o BatchMode=yes "$DEPLOY_HOST" "$@"; }

push_files() {
  local remote=/tmp/lewbox-vps
  ssh_ "rm -rf $remote && mkdir -p $remote"
  tar -C "$here" -cf - setup.sh stack bin systemd | ssh_ "tar -C $remote -xf -"
  echo "$remote"
}

case ${1:-} in
  setup)
    domain=${2:?usage: deploy.sh setup DOMAIN [ADMIN_EMAIL]}
    email=${3:-}
    [[ $domain =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || { echo "deploy.sh: bad domain" >&2; exit 2; }
    [[ -z $email || $email =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+$ ]] || { echo "deploy.sh: bad email" >&2; exit 2; }
    remote=$(push_files)
    ssh_ "sudo env DOMAIN='$domain' ADMIN_EMAIL='$email' bash $remote/setup.sh base; rm -rf $remote"
    ;;
  lock-ssh)
    remote=$(push_files)
    ssh_ "sudo bash $remote/setup.sh lock-ssh; rm -rf $remote"
    ;;
  v*)
    [[ $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "deploy.sh: not a release tag: $1" >&2; exit 2; }
    ssh_ sudo lewbox-deploy "$1"
    ;;
  status|rollback|latest)
    ssh_ sudo lewbox-deploy "$1"
    ;;
  backup)
    ssh_ sudo lewbox-backup manual
    ;;
  logs)
    ssh -t "$DEPLOY_HOST" sudo docker compose --project-directory /opt/stack logs -f --tail 100 lewbox
    ;;
  *)
    sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
    ;;
esac
