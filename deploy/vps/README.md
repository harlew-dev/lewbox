# Lewbox on a VPS

Run the orchestrator (API, console, docs) on its own small VPS, apart from the AerolVM nodes, so it stays up when a node goes down. Docker Compose runs Caddy (TLS) and Lewbox. Other apps can share the box.

Tested on a Hetzner VPS with Ubuntu 22.04 (2 vCPU, 4 GB). Any Ubuntu 22.04 or 24.04 VPS should work.

## What's here

| Path | On the server | What |
|---|---|---|
| `setup.sh` | Run from a temp copy | Prepares the server. `base` does packages, users, firewall, swap, Docker, the stack and backups; `lock-ssh` hardens SSH |
| `deploy.sh` | Runs on your machine | Runs everything over SSH |
| `stack/compose.yaml`, `stack/Caddyfile` | `/opt/stack/` | Caddy and Lewbox |
| `stack/lewbox/Dockerfile` | `/opt/stack/lewbox/` | Builds the image from a published release binary |
| `bin/lewbox-deploy` | `/usr/local/sbin/` | Downloads a release, verifies it, backs up, switches, checks health, rolls back on failure |
| `bin/lewbox-backup` | `/usr/local/sbin/` | Backs up the database and keyring together |
| `systemd/lewbox-backup.*` | `/etc/systemd/system/` | Nightly backup at 03:30 UTC |

On the server you'll also find:

- `/opt/stack/.env`: holds `DOMAIN` and `LEWBOX_TAG`.
- `/srv/lewbox`: Lewbox's data, owned by UID 10001.
- `/var/backups/lewbox`: the backups.

## Set up a new server

```bash
# First run as root, with the key the provider installed.
DEPLOY_HOST=root@203.0.113.10 deploy/vps/deploy.sh setup lewbox.example.com you@example.com

# Check that the admin user works, then turn off root and password logins.
ssh ops@203.0.113.10 sudo true
DEPLOY_HOST=ops@203.0.113.10 deploy/vps/deploy.sh lock-ssh

# Reboot if a kernel update arrived, then deploy a release.
ssh ops@203.0.113.10 'test -f /var/run/reboot-required && sudo systemctl reboot'
DEPLOY_HOST=ops@203.0.113.10 deploy/vps/deploy.sh v0.2.0
```

Point the domain's A (and AAAA) record at the server. Caddy gets a certificate on its own once DNS resolves. It retries in the background; `sudo docker compose --project-directory /opt/stack restart caddy` makes it try at once.

### The first admin

If you pass an email to `setup`, it writes `/opt/stack/bootstrap.env` (root only) with that email and a random password. Lewbox creates the account on its first start. To use it:

1. Read the password: `ssh ops@HOST sudo cat /opt/stack/bootstrap.env`.
2. Sign in at `https://DOMAIN/admin` and change the password.
3. Delete the file and recreate the container so the password leaves its environment:

```bash
ssh ops@HOST 'sudo rm /opt/stack/bootstrap.env && sudo lewbox-deploy restart'
```

## Deploy, roll back, back up

```bash
export DEPLOY_HOST=ops@lewbox.example.com
deploy/vps/deploy.sh v0.2.1        # or: latest
deploy/vps/deploy.sh status
deploy/vps/deploy.sh rollback      # previous image; the database is NOT restored
deploy/vps/deploy.sh backup
deploy/vps/deploy.sh logs
```

A deploy goes through these steps, and stops at the first one that fails:

1. Downloads `lewbox_linux_<arch>` and `checksums.txt` from the GitHub release.
2. Checks the SHA-256 against `checksums.txt`.
3. Builds `lewbox:<tag>` and checks that the binary reports `<tag>`.
4. Backs up the database and keyring to `pre-<tag>-…`.
5. Switches `LEWBOX_TAG` and waits up to 90 s for the container to be healthy.

If the new version never becomes healthy, it switches back to the previous image.

> [!WARNING]
> Migrations run when Lewbox starts and don't roll back. If a new version migrated the database and you go back to an older one, restore the `pre-<tag>` backup too.

### Restore a backup

```bash
cd /opt/stack
b=/var/backups/lewbox/pre-v0.2.1-20261112T120000Z      # the backup to restore
sudo docker compose stop lewbox
sudo install -m 0600 -o lewbox -g lewbox $b/lewbox.db /srv/lewbox/lewbox.db
sudo install -m 0600 -o lewbox -g lewbox $b/keyring.key /srv/lewbox/keyring.key
sudo rm -f /srv/lewbox/lewbox.db-wal /srv/lewbox/lewbox.db-shm
sudo lewbox-deploy v0.2.0                               # the version that wrote the backup
```

Backups are kept on the server only: daily ones for 14 days, the last 5 pre-deploy ones. **Copy them off the server too** (for example `restic` to object storage), and keep a separate copy of `keyring.key`. Without it, the cluster tokens in any database backup can't be decrypted.

## Deploy from CI

The `deploy` user may only run `sudo lewbox-deploy`. Give CI its own key and pin it to that command in `/var/lib/deploy/.ssh/authorized_keys`:

```
command="sudo /usr/local/sbin/lewbox-deploy \"$SSH_ORIGINAL_COMMAND\"",restrict ssh-ed25519 AAAA… ci
```

CI then deploys with `ssh deploy@HOST v0.2.1`. Store the server's host key in CI (`ssh-keyscan HOST`) rather than turning off host key checking.

## Other apps on the same VPS

- **Network:** put each app on the external `web` network in its own Compose project, and never publish its ports. Docker's published ports bypass ufw.
- **Routing:** add `/opt/stack/sites/<app>.caddy`, for example `app.example.com { reverse_proxy myapp:8080 }`, then run `sudo docker compose --project-directory /opt/stack restart caddy`.
- **Limits:** give each app a `mem_limit`.

## What setup.sh changes

- **Packages:** a full upgrade, and installs `sqlite3`, `ufw` and Docker from Docker's own repository.
- **Automatic updates:** unattended security upgrades, with a reboot at 04:00 UTC when one is needed.
- **Users:**
  - `ops`: the admin, with root's SSH keys and passwordless sudo.
  - `deploy`: may only run `lewbox-deploy`.
  - `lewbox`: UID 10001, the container's user, which owns the data.
- **Firewall:** ufw denies all incoming traffic except 22/tcp, 80/tcp, 443/tcp and 443/udp.
- **Memory:** a 2 GB `/swapfile`, with `vm.swappiness=10`.
- **Docker:**
  - Logs are capped at 3 × 10 MB per container.
  - `live-restore` is on, so restarting the Docker daemon leaves containers running.
  - The `web` network uses 172.30.0.0/24. Lewbox trusts `X-Forwarded-For` only from that subnet.
- **SSH (`lock-ssh`):** `/etc/ssh/sshd_config.d/00-hardening.conf` sets no root login, no passwords, keys only, and `AllowUsers ops deploy`.

Things to do outside the server:

- Turn on the provider's firewall (Hetzner Cloud Firewall: 22 from your IP, 80, 443).
- Set up off-site backups.
- Watch `https://DOMAIN/v0/health` with an uptime monitor.
