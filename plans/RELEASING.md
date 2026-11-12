# Releasing and going live

How to cut a Lewbox release, put it on your servers, and prove the customer journey works there. Nothing has been pushed or published yet: the repo exists only on this machine (`origin` points at `github.com/harlew-dev/lewbox`, but nothing was pushed).

You need: Go 1.27+, Node 24+, Docker (for the dev-cluster test), and for production:

- **one orchestrator host**: any Linux box with systemd (it's one process plus a SQLite file; 1 vCPU / 1 GB is plenty), with a DNS name such as `lewbox.example.com`;
- **one or more sandbox servers** (Ubuntu 22.04+, systemd, `sudo`, ports 80/443 open; bare metal with `/dev/kvm` for Firecracker), each with a DNS name such as `eu-west.api.example.com` and a wildcard `*.eu-west.api.example.com` for sandbox previews, on Cloudflare (Lewbox's installer uses a Cloudflare token for TLS).

The orchestrator can run on the first sandbox server too. A separate box is cleaner, but not required.

---

## 1. Check the build

```bash
cd ~/dev/harlew-dev/lewbox
git status                      # clean
make test                       # gofmt, vet, Go tests, web typecheck + tests
cd deploy/dev-cluster
LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" ./e2e.sh   # ~10 min, real AerolVM in Docker, includes customers.sh
```

`e2e.sh` ends with "All N steps passed." and tears the cluster down (`--keep` leaves it running).

## 2. Cut the release

```bash
git tag -a v0.2.0 -m "v0.2.0: customers, region plans, sandbox keys, usage, monitoring"
make release VERSION=v0.2.0
ls release/      # lewbox_linux_amd64  lewbox_linux_arm64  release.json  checksums.txt
```

`release/` is exactly what `lewbox update` reads: the binaries, `release.json` (`{"version","aerolvm_version"}`) and `checksums.txt`. The web console and docs are embedded in the binary.

**Publishing** (when you're ready to make it public):

```bash
git push origin main --tags
gh release create v0.2.0 release/* --title v0.2.0 --notes-file plans/STATUS.md
```

Until then, copy `release/lewbox_linux_amd64` to your servers with `scp` (below). Nodes can update from a folder: `lewbox update --release-from /path/to/release`.

**Docker image** instead of a binary: `make docker VERSION=v0.2.0` builds `lewbox:local` (see `docs/setup/run-with-docker.md`; `compose.yaml` at the repo root runs it with a data volume).

## 3. Install the orchestrator

On the orchestrator host:

```bash
scp release/lewbox_linux_amd64 you@lewbox.example.com:/tmp/lewbox
ssh you@lewbox.example.com
sudo install -m 0755 /tmp/lewbox /usr/local/bin/lewbox
sudo useradd --system --create-home --home-dir /var/lib/lewbox lewbox
```

`/etc/systemd/system/lewbox.service`:

```ini
[Unit]
Description=Lewbox
After=network-online.target
Wants=network-online.target

[Service]
User=lewbox
Environment=LEWBOX_DB=/var/lib/lewbox/lewbox.db
Environment=LEWBOX_KEYRING_FILE=/var/lib/lewbox/keyring.key
Environment=LEWBOX_COOKIE_SECURE=always
Environment=LEWBOX_TRUSTED_PROXIES=127.0.0.1/32
Environment=LEWBOX_BOOTSTRAP_ADMIN_EMAIL=you@example.com
EnvironmentFile=-/etc/lewbox/bootstrap.env
ExecStart=/usr/local/bin/lewbox serve --listen 127.0.0.1:8080
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo install -d -m 0700 /etc/lewbox
echo 'LEWBOX_BOOTSTRAP_ADMIN_PASSWORD=<a long password>' | sudo tee /etc/lewbox/bootstrap.env >/dev/null
sudo chmod 0600 /etc/lewbox/bootstrap.env
sudo systemctl daemon-reload && sudo systemctl enable --now lewbox
journalctl -u lewbox -f          # "bootstrap admin created", "orchestrator listening"
```

The bootstrap admin is only created when there are no accounts, so you can delete `bootstrap.env` after the first start.

TLS with Caddy (`/etc/caddy/Caddyfile`), then `sudo systemctl reload caddy`:

```caddy
lewbox.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

`LEWBOX_TRUSTED_PROXIES=127.0.0.1/32` makes rate limits see real client IPs behind Caddy (DECISIONS D15). Open `https://lewbox.example.com/admin` and sign in.

**Back up** `/var/lib/lewbox/lewbox.db` and `/var/lib/lewbox/keyring.key` together (`sqlite3 lewbox.db ".backup 'backup.db'"` works while it runs). Without the keyring the cluster admin tokens can't be decrypted. Together they give full control of every cluster: store them safely.

## 4. Add sandbox servers

On each sandbox server (not as root):

```bash
scp release/lewbox_linux_amd64 you@node1:/tmp/lewbox && ssh you@node1
sudo install -m 0755 /tmp/lewbox /usr/local/bin/lewbox
lewbox login https://lewbox.example.com

# First server of a region: install AerolVM with wildcard TLS, then start the cluster.
lewbox install --domain eu-west.api.example.com --acme-email you@example.com \
  --dns-api-token-stdin < cloudflare-token          # Zone:Read + DNS:Edit
lewbox node add --new-cluster eu-west
lewbox firewall enable                              # ufw with exactly the ports AerolVM needs

# More servers in the same region:
lewbox install --domain eu-west-2.api.example.com --acme-email you@example.com --dns-api-token-stdin < cloudflare-token
lewbox node add --cluster eu-west
lewbox firewall enable
```

Every step is printed before it runs, and each privileged step is one `sudo` command. The cluster's customer endpoint is the first node's AerolVM URL unless you pass `--endpoint-url https://eu-west.api.example.com` to `node add`. Check it in the console: **Servers** → the cluster → every check green, and after a minute the uptime bars start filling.

A region is any cluster in `ready` or `degraded` state; customers see it by its slug and name.

## 5. Set up the product

In the console (`/admin`):

1. **Plans**: edit `free` (default: 2 sandboxes, 2 vCPU, 4 GB), add paid plans.
2. **Customers → New customer**: email, password, organization name, and a plan for each region. Send them the sign-in URL and password.
3. **Settings** (optional): instance name; self sign-up stays `closed` unless you want it (`approval` lets people sign up and wait for you; `open` lets them straight in with the default plan in the default regions).

The same from the CLI: `lewbox plan create pro --name Pro --max-sandboxes 20 --max-vcpus 40 --max-memory 80GB`, `lewbox user create dev@acme.dev`, `lewbox org create acme --name Acme --owner dev@acme.dev`, `lewbox subscription set eu-west --plan pro --org acme`.

## 6. Smoke test on the live servers

**By hand, as a customer** (a private browser window):

1. Create a test customer in the console (**Customers → New customer**) with a plan in the region, and sign in as it.
2. **API keys → Create API key**, pick the region, copy the env vars.
3. Run the TypeScript or Python snippet from the dialog on your laptop. The sandbox appears under **Sandboxes** within ~15 s, and **Home** turns green.
4. **Usage** shows numbers after a minute; **Sandboxes → Stop/Delete** works.

**Automatically**, the same journey the dev cluster runs (it creates two test customers and an `e2e-<time>` plan, and restores your settings when it ends; suspend or ignore the test customers afterwards):

```bash
cd deploy/dev-cluster
LEWBOX_URL=https://lewbox.example.com LEWBOX_REGION=eu-west \
LEWBOX_ADMIN_EMAIL=you@example.com LEWBOX_ADMIN_PASSWORD='…' ./customers.sh
```

It needs `curl` and `jq`, and your machine must reach the region's endpoint. It opens sign-up briefly while it runs; run it before launch, or on a quiet instance.

## 7. Upgrade

```bash
sqlite3 /var/lib/lewbox/lewbox.db ".backup '/var/lib/lewbox/before-v0.3.0.db'"
sudo install -m 0755 /tmp/lewbox /usr/local/bin/lewbox && sudo systemctl restart lewbox
```

Migrations run on start. Nodes update themselves: `lewbox update` (CLI), `lewbox update --aerolvm` (AerolVM, refuses on clusters it can't confirm are safe; see `docs/operations/update.md`).

**Roll back**: stop the service, restore the backup database, install the old binary, start. Migrations don't roll back on their own.

## 8. What to watch in the first week

- `journalctl -u lewbox` for `cluster sync failed` / `push quota failed` warnings.
- **Servers → cluster**: uptime bars, memory and disk free per node.
- **Customers**: usage per customer and region.
- Known limits and their reasons are in [DECISIONS.md](DECISIONS.md); what was and wasn't verified is in [STATUS.md](STATUS.md).
