# Lewbox CLI: AerolVM on this machine

> [!NOTE]
> **Design notes.** This page records how the CLI was designed and what was verified while building it. To *use* Lewbox, start at the [docs home](../README.md).

This covers the commands that change the machine you're on: `lewbox install`, `lewbox node add`, `lewbox update` and `lewbox node remove`. The full command list is in [cli-commands.md](cli-commands.md).

They reuse AerolVM's own installer (`scripts/install.sh` in `aerol-ai/microvm`) instead of copying it. They show every privileged command before running it.

Everything marked **verified** below was tested on 2026-11-11 against AerolVM v0.7.0 (and v0.6.13 for the upgrade), on nodes running in Docker ([deploy/dev-cluster](../../deploy/dev-cluster/README.md)).

---

## 1. Why install and node add are separate

AerolVM sets up cluster nodes in two ways (`install.sh --help`, "Starting or joining a cluster"):

```
install.sh [options] init [sandboxd init options]
install.sh [options] join <url> --token <id.secret> [--ca-cert-hash sha256:<hex>] [--node-id <id>]
```

Both leave `sandboxd` stopped until `sandboxd init` or `sandboxd join` has the cluster's config and keys. If it booted first, as a lone node, "it would make a server key of its own", and then it couldn't join a cluster.

So the work is split in two:

| Step | Command | What it does | Can be redone |
|---|---|---|---|
| 1 | `lewbox install` | Checks the machine, installs AerolVM's binaries, services, Caddy and config. `sandboxd` is **left stopped** (`--no-start`) | Yes. Running it again repairs the install |
| 2 | `lewbox node add` | Either **starts a new cluster** here (`init`) or **joins an existing one** (`join`, with `--cluster`). Then it applies Lewbox's settings and registers the node | Yes. It does nothing if the node is already in that cluster |

Every Lewbox node is started with `init`, never as a lone node. That way any of them can later become the first node of a bigger cluster. (Upstream does let a lone node become a cluster's *seed* later with `sudo sandboxd init`, but not *join* another cluster, so starting with `init` keeps both doors open.)

**Verified:** `install.sh … --no-start` then, on the same machine, `install.sh … init` (first node) or `install.sh … join` (others) gives a working cluster; the join took 12 seconds and every preflight check passed.

The exception is **`--mode local`**: AerolVM's `--local` never joins a cluster. `lewbox install --mode local` starts `sandboxd` straight away, and `lewbox node add` only registers it as a one-node cluster. `--cluster` is refused in local mode.

---

## 2. `lewbox install`

```
$ lewbox install --domain eu-west.api.acme.dev --dns-api-token-stdin < cf-token
Checking this machine
  ✓ Ubuntu 24.04 (tested path: Ubuntu 22.04+), x86_64
  ✓ sudo works
  ✓ ports 443, 21212, 7000-7003 free
  ✓ eu-west.api.acme.dev resolves to this machine
AerolVM v0.7.0
  → downloaded install.sh (sha256 checked against the pinned value)
  → sudo bash install.sh --version v0.7.0 --domain eu-west.api.acme.dev \
      --dns-provider cloudflare --dns-api-token ****** --pat-scope host \
      --node-name eu-west-1 --no-start
  ✓ installed; sandboxd is stopped until this node starts or joins a cluster
Next:
  lewbox node add --new-cluster eu-west      # start a cluster here
  lewbox node add --cluster <node|cluster>   # or join one
```

### 2.1 Flags

| `lewbox install` | Passed to AerolVM `install.sh` | Notes |
|---|---|---|
| `--name <name>` (default: hostname, lowercased) | `--node-name <name>` | Also the Lewbox node name. `node add <name>` can change it before the node starts |
| `--mode local` (default without `--domain` or `--public-host`) | `--local` | Development only: `127.0.0.1:21212`, no TLS, and public URLs don't work (REVIEW §5.4). Starts at once. Linux only for now: AerolVM's installer supports macOS with `--local`, but Lewbox's settings step assumes systemd, so `install` refuses macOS up front |
| `--domain <domain>` | `--domain <domain> --dns-provider cloudflare` | Production. Wildcard TLS by DNS-01 |
| `--dns-api-token-stdin` | `--dns-api-token <token>` | Cloudflare token with Zone:Read and DNS:Edit. Read from stdin, never as a flag value (§6) |
| `--acme-email <email>` | `--acme-email <email>` | Recommended with `--domain` |
| `--public-host <host-or-ip>` | `--public-host <host-or-ip>` | IP mode, no TLS. For labs |
| `--runtime docker` (default) \| `firecracker` | nothing, or `--with-firecracker` | One runtime per cluster (MVP §11). Firecracker needs `/dev/kvm` on x86_64 or aarch64 and isn't allowed with `--mode local`. `node add --cluster` checks that this matches the cluster |
| `--aerolvm-version <tag>` | `--version <tag>` | Default: the version this Lewbox release was tested with. Never lower than v0.6.13 (MVP §5). When joining, the cluster's version wins (§3.2) |
| `--from <dir>` (env `LEWBOX_AEROLVM_FROM`) | `--sandboxd-url file://…` and the other download URLs | Take the release files from `<dir>/<tag>/` instead of GitHub. The installer accepts `file://` URLs (verified). The dev cluster uses this so nodes don't each download about 200 MB |
| `--dry-run` | | Run the checks and print every command, but change nothing |
| `--yes` | | No prompts |
| always, except `--mode local` | `--no-start` | §1 |
| always | `--pat-scope host` | The PAT only works from this host and cluster peers (MVP §5) |

AerolVM's other installer flags (gVisor, GPU, containerd, Caddy on S3) aren't wrapped. Pass them through after `--`:

```bash
lewbox install --domain … -- --lb-health-port 8081
```

`install.sh` saves every option except secrets and download choices. So later runs of it (from `node add`, or `update --aerolvm`) keep them without Lewbox having to remember them.

### 2.2 What happens, in order

1. **Checks.** Linux. `sudo` works without a password prompt (or with one, on a terminal). The ports are free: 21212, plus 443 in domain mode and 7000–7003 for cluster traffic. `/dev/kvm` exists for Firecracker, which also needs a non-local mode. Domain mode has `--dns-api-token-stdin`. Every failed check says how to fix it, and nothing changes until all pass. (Planned: a distro check and that the domain resolves to this machine.)
2. **Get the installer.** Download `install.sh` from `https://github.com/aerol-ai/microvm/releases/download/<tag>/install.sh`. **Not** the `curl https://aerolvm.aerol.ai | sudo bash` one-liner, because that always serves whatever is newest. Check its SHA-256 against a value built into this Lewbox release (v0.7.0: `0305d18c…54da7a`). For any other version, check it against that release's `checksums.txt`, which lists `install.sh` too (verified).
3. **Run it** with `sudo bash install.sh <mapped flags> --no-start`, streaming its output. **The installer prints the PAT** (`PAT token: …`, verified), so every line goes through a filter that masks it and any other secret before it reaches the terminal.
4. **Write down the install** in `~/.config/lewbox/node.json`: the version, the mode, the runtime and the node name. `node add` and `update` read it. Nothing secret goes there.

### 2.3 What it leaves on the machine

The AerolVM installer owns these files:
- binaries: `sandboxd` and `toolboxd` in `/usr/local/bin`;
- services: `sandboxd.service`, `sandboxd-healthcheck.timer` and Caddy;
- settings: `/etc/sandboxd/sandboxd.env` (secrets) and `/etc/sandboxd/node.json` (other settings);
- logs: `journalctl -u sandboxd`.

---

## 3. `lewbox node add`

### 3.1 Start a new cluster here

```
$ lewbox node add eu-west-1 --new-cluster eu-west
  → sudo bash install.sh init
  ✓ sandboxd healthy: https://eu-west.api.acme.dev/health
Lewbox settings
  ✓ /etc/sandboxd/lewbox.env: SB_IDENTITY_ENABLED=true, SB_TOKEN_MAX_TTL=876000h
  ✓ sandboxd restarted, healthy
  ✓ admin token minted over 127.0.0.1:21212
  ✓ cluster "eu-west" registered (clu_3k9QhV2mXp7LrT), node "eu-west-1" added
```

1. **Start it.** `sudo bash install.sh init` (the installer's saved options are reused).
2. **Lewbox settings.** The installer has no flag for identity mode. Lewbox writes **its own files**, which the installer never touches, so re-running the installer can't undo them:
   - `/etc/sandboxd/lewbox.env` (0600): `SB_IDENTITY_ENABLED=true`, `SB_TOKEN_MAX_TTL=876000h`;
   - `/etc/systemd/system/sandboxd.service.d/lewbox.conf`: `EnvironmentFile=/etc/sandboxd/lewbox.env`.

   Then `systemctl daemon-reload`, `systemctl restart sandboxd`, and wait for `/health`. **Verified:** identity must be on *every* node; a node without it answers 401 to every token, including ones minted elsewhere in the cluster. Still to do: a create policy that allows only this cluster's runtime (MVP §5).
3. **Get an admin token.** Read the PAT from `/etc/sandboxd/sandboxd.env` (`SB_PAT_TOKEN`) with sudo, never from the installer's output. Use it over `127.0.0.1:21212` to mint an admin `avm_` token (`--role admin --team '*' --expires-in 36500d`). After that, Lewbox only uses that token (MVP §4 trust boundaries). **Verified:** right after a start, `/health` answers before raft has a leader, and token writes return 503 for 1–3 seconds; Lewbox retries them. With `--pat-scope host` the PAT is refused from any address but loopback (401).
4. **Register the cluster.** Call `POST /v0/clusters` with the slug, the token, the runtime and the endpoint: `https://<domain>`, `http://<public-host>:21212`, or `http://127.0.0.1:21212`. That runs the normal validation (MVP §7.1). The token goes to the orchestrator once, and is stored there encrypted with the keyring (`keyring.key` next to the database).
5. Later (planned): print the `cluster setup` snippet for the audit export.

### 3.2 Join an existing cluster

```
$ lewbox node add eu-west-2 --cluster eu-west-1
  ✓ eu-west-1 is in cluster "eu-west" (docker, v0.7.0, 1 node)
  ✓ runtime docker matches this install
  ✓ join token from the orchestrator (single use, expires in 15m)
  ✓ reached 10.0.0.7:7000-7003
  → sudo bash install.sh join https://10.0.0.7:21212 --token ****** \
      --ca-cert-hash sha256:4f1c… --node-id eu-west-2
  ✓ eu-west-2 is a member of eu-west (mixed), healthy
Lewbox settings
  ✓ /etc/sandboxd/lewbox.env: SB_IDENTITY_ENABLED=true, SB_TOKEN_MAX_TTL=876000h
  ✓ sandboxd restarted, healthy
  ✓ node "eu-west-2" added to cluster "eu-west"
```

1. **Find the cluster.** `--cluster` is either a cluster slug, or the name of any node already in a cluster. That node's cluster is used. `not_found` if neither matches. (README open question 2 covers a name that matches both.)
2. **Check it fits.** The cluster's runtime must match this install's `--runtime`; a Firecracker cluster needs `/dev/kvm` here. A local-mode install can't join. A failure here is `precondition_failed`, and nothing has changed yet.
3. **Get a join token.** `POST /v0/clusters/{id}/join-tokens {role, ttl: "15m", reusable: false}`. The orchestrator calls AerolVM's `POST /v1/cluster/join-tokens` with the cluster's admin token, and returns the join URL, token and CA hash. Lewbox stores none of them. **Verified:** an `--role admin --team '*'` token can mint join tokens from outside the cluster. A `mixed` node needs a `server` token, so Lewbox asks AerolVM for that and passes `--role mixed` to the join. This step is why `node add` needs an organization `owner` or `admin`: a join token lets a machine into the fleet. `--role worker|ingress|server|mixed` is passed through (default: `mixed`).
4. **Check the network.** This machine must reach the join URL (port 7003). The cluster's peers must reach it back on 7000–7002: AerolVM's own preflight dials back and refuses the join otherwise (a private full mesh is needed; REVIEW §3).
5. **Join.** `sudo bash install.sh join <url> --token <token> --ca-cert-hash <hash> --node-id <name>`. The installer asks the cluster which version to install, and installs that, even if it differs from the version `lewbox install` put here.
6. **Wait** until the node is in `GET /v1/cluster/members` and healthy.
7. **Lewbox settings**, the same as §3.1 step 2. These are set on each node, not once for the whole cluster.
8. **Register.** Lewbox gets its node list from each cluster's members. `node add` asks the orchestrator to sync that cluster now, and prints the new node. No admin token is minted: the cluster already has one.

**Joining a machine that doesn't use the Lewbox CLI.** `lewbox cluster join-command eu-west` prints the line for it. The line downloads the installer of the cluster's own version, checks it against that release's `checksums.txt`, and only then runs it with sudo; every value in it is shell-quoted, and a version that isn't a plain release tag produces no line at all. You then have to apply the Lewbox settings to that node yourself (`/etc/sandboxd/lewbox.env` and the drop-in from §3.1), or that node answers 401 to Lewbox's token.

---

## 4. `lewbox update`

```bash
lewbox update                         # the CLI only
lewbox update --aerolvm               # the CLI, then AerolVM on this machine
lewbox update --check                 # show what would change, change nothing
lewbox update --version v0.4.0        # a specific Lewbox release
lewbox update --aerolvm --aerolvm-version v0.7.3   # an AerolVM version other than the tested one (warns)
```

"Latest" means the latest Lewbox release. AerolVM is moved to **the version that release was tested with**, not to whatever AerolVM released last. `--aerolvm-version latest` asks for AerolVM's newest version (read from its latest `release.json`), and prints a warning that it is untested. Any version must be a plain release tag such as `v0.7.0`.

### 4.1 The CLI

1. Look up the release (`--version`, or latest) from the same place as the install script.
2. Download the archive for this OS and architecture, and check it against `checksums.txt`.
3. Replace the binary where it is now, atomically (write next to it, then rename). If that folder isn't writable by you, stop and print the `sudo` command rather than running it.
4. If the orchestrator runs here as a `lewbox-orchestrator` service (`orchestrator enable`), restart it. Migrations run on start.

Inside the Docker image, `update` refuses and tells you to pull a new image tag.

### 4.2 AerolVM (`--aerolvm`)

After replacing itself, the CLI **runs the new binary** for this part. That way the pinned AerolVM version and installer hash come from the new release.

1. Read `~/.config/lewbox/node.json`. No AerolVM here → nothing to do.
2. Ask **this machine's AerolVM** (over loopback, with the PAT) how many members its cluster has. **More than one → stop**, and say to run `lewbox cluster upgrade <slug>`. If it can't tell, it also stops: an unanswered question must not let one member be upgraded alone. Upgrading one Raft member on its own leaves the cluster on mixed versions; a rolling upgrade does it a batch at a time, and can be rolled back.
3. One node (or local mode): download and check the pinned `install.sh` for the new version, and run `sudo bash install.sh --version <tag>`. The installer reuses its saved options. When the installed `sandboxd` has `sandboxd upgrade node`, the installer hands the new binaries to it. That keeps the old files as `*.prev`, backs up the database, and keeps Firecracker VMs running.
4. Re-apply the Lewbox settings. They are in Lewbox's own files, so the installer leaves them alone (verified on a v0.6.13 → v0.7.0 upgrade); this only repairs a hand-edited node.
5. Wait for `/health`, and check that it reports the new version.

### 4.3 A whole cluster: `lewbox cluster upgrade`

Planned. This is run from anywhere, not on a node. The orchestrator starts AerolVM's own rolling upgrade, which is what `aerolvm upgrade plan|apply|status|stop|resume|rollback` drives, using the cluster's admin token. The routes are `GET/POST /v1/cluster/upgrade` plus `…/stop`, `…/resume`, `…/finalize` and `…/rollback`; `GET` answers 404 `this cluster never had a rollout` before the first one. `--batch` and `--pause-after` are passed through. `cluster upgrade status` follows it.

---

## 5. `lewbox node remove`

1. Confirm (`--yes` to skip).
2. Refuse if it is the cluster's last node (use `cluster remove` for that), or if it is the node behind the cluster's `admin_url`: Lewbox would lose its way into the cluster. This guard compares URLs, so it only works when `admin_url` is a node's own address (IP mode, or the dev cluster). With a domain or load balancer in front, check yourself which machine it points at.
3. Check it is still a member. **Verified:** AerolVM's drain answers 204 even for a node it doesn't know.
4. Drain it (`POST /v1/cluster/nodes/{id}/drain`), then take it out of the cluster (`DELETE /v1/cluster/nodes/{id}`, `?force=true` with `--force` when it still owns sandboxes). A 404 means AerolVM already forgot it.
5. Unregister it in Lewbox.

There is no "unregister but keep it in the cluster" option: the next sync would find it among the members and add it back.

AerolVM stays installed on the machine. To use it again, run `sudo sandboxd reset --yes` there (AerolVM denies the old node ID and key), then `lewbox node add` again. AerolVM releases now ship an `uninstall.sh`, which a future `lewbox uninstall` can wrap.

---

## 6. Security notes

- **Secrets on the command line can be seen.** `install.sh` takes the DNS token (`--dns-api-token`) and the join token (`--token`) as arguments, so other local users can see them in `ps` while the installer runs. Lewbox reads both from stdin or the API, but it can't avoid this hand-off. The join token is single use and lasts 15 minutes, which limits the damage. Ask upstream for `--dns-api-token-file` and `--token-file` (add them to MVP §17).
- **No pipe-to-bash.** Lewbox downloads a pinned installer and checks its hash. It never runs `curl | sudo bash`.
- **The PAT stays on the host.** It is read from the root-only env file, used once over loopback and never stored by Lewbox. `--pat-scope host` keeps it off the network.
- **Join tokens are fleet power.** Only owners and admins can get one. It is minted for one join, never written to disk by Lewbox, and never logged.

---

## 7. Questions answered by the dev cluster

1. **Which AerolVM version to pin:** v0.7.0, released 2026-11-10 and the latest on GitHub. Its `install.sh` SHA-256 is `0305d18cb86ef06b11dd6b3cc40f91f8caa6abc8b5975895231ff894b9848d33`.
2. **Checksum for `install.sh`:** the release's `checksums.txt` lists it, so other versions are verified against upstream.
3. **Routes behind `aerolvm upgrade`:** found (§4.3); not used yet.
4. **Are installer edits kept?** Lewbox no longer edits the installer's files; it uses its own env file and systemd drop-in (§3.1).
5. **Can a node that booted alone join later?** It can become a seed (`sandboxd init`) but not join. Starting every node with `init` covers both.

Still open: a create policy per runtime (§3.1), and the dev cluster only tests IP mode, not domain mode with TLS.
