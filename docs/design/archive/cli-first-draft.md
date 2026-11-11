# Lewbox CLI

> [!WARNING]
> **Superseded.** This is the first draft of the CLI design (2026-11-10), kept for history. Several commands here were renamed or redesigned; see [../cli-commands.md](../cli-commands.md) and the [docs home](../../README.md).

Status: draft, 2026-11-10. Built so far: `orchestrator`, `health`, `hello`, `version`, and `node add|list|get|remove`. Everything else here is planned.

The CLI should feel like the [boat.dev CLI](https://docs.boat.dev/quickstart):
- you install it with one line;
- one guided command sets everything up;
- commands are short and readable;
- output is plain text for people and JSON for scripts.

On top of that, Lewbox can **install AerolVM on the machine you're on** and add it as a node.

---

## 1. What we take from boat, and what we don't

| boat.dev | Lewbox |
|---|---|
| `curl -fsSL https://boat.dev/install \| sh`, links into `~/.local/bin` | Same: `curl -fsSL https://github.com/harlew-dev/lewbox/releases/latest/download/install.sh \| sh` (§2) |
| `boat onboard`: one guided, re-runnable setup | `lewbox onboard` (§3). It also offers to install AerolVM here |
| `boat login` in the browser, `--key-stdin` for CI | `lewbox login`, same shape (§4.4) |
| `boat status [--check]` | `lewbox status [--check]` |
| Global `--api-url`, `--org`, `--json`, `--no-update` | Same, as `LEWBOX_API_URL` and `LEWBOX_ORG` |
| Text by default; `--json` for machines; JSON on automatically when piped | Same. JSON output is **byte-identical to the HTTP API body** (§4.2) |
| On failure in JSON mode, the last line is a JSON error with a non-zero exit | Same shape as the API: `{"error":{"code","message"}}` on stderr, exit 1 |
| `delete` asks to confirm; `--yes` for scripts | Same for anything destructive |
| `boat completions <shell>` | Cobra's `lewbox completion <shell>` |
| Silent self-update every 6 hours | **Not copied.** This CLI installs system software with `sudo`, so it never updates itself silently. `lewbox self-update` is manual, and an update notice goes to stderr |
| Installer opens Stripe checkout | **Not copied.** Billing isn't part of setup |
| Sandbox commands (`new`, `exec`, `ssh`, `stop`) | **Later, and not through Lewbox.** Lewbox is not in the sandbox path (MVP §1). If we add them, they call the cluster directly with a cluster key, the way the SDKs do (§8) |

---

## 2. Install the CLI

```bash
# macOS and Linux
curl -fsSL https://github.com/harlew-dev/lewbox/releases/latest/download/install.sh | sh
```

The script:
1. Picks `linux|darwin` and `amd64|arm64`, and downloads the matching goreleaser archive.
2. Checks it against the release's `checksums.txt`.
3. Puts `lewbox` in `~/.local/bin`. It adds that folder to `PATH` in your shell's rc file if it isn't already there.
4. Adds tab completion: `source <(lewbox completion bash)` (zsh and fish work the same way).
5. Tells you to open a new terminal, or to run `lewbox onboard`.

It needs no root. `LEWBOX_VERSION=v0.2.0` pins a release, and `LEWBOX_INSTALL_DIR` changes the target folder.

For development, run `go install ./cmd/lewbox` from the repo.

**CI and Docker.** Download the static binary from the release, then sign in without a browser:

```bash
printf '%s' "$LEWBOX_API_KEY" | lewbox login --key-stdin --json
```

---

## 3. First run: `lewbox onboard`

`onboard` asks only for what isn't set up yet, so it is safe to run again.

```
$ lewbox onboard
Where is your Lewbox?
  › On this machine (local database at ~/.local/share/lewbox/lewbox.db)
    A Lewbox server (https://…)

Run sandboxes on this machine too?
  › Yes, install AerolVM here and add it as a node
    No

Checking this machine
  ✓ Ubuntu 24.04, x86_64, 16 vCPU, 62 GiB
  ✓ sudo works
  ✓ /dev/kvm present (Firecracker is possible)
How will customers reach this node?
  › Local only, for development (127.0.0.1:21212, no TLS)
    A domain, with TLS through Cloudflare DNS
    This machine's public IP, no TLS
…
✓ Node "lab-1" added. Next: lewbox node list
```

Every question has a flag, so `onboard` also runs without a terminal:

```bash
lewbox onboard --local --install-aerolvm --mode local --yes
lewbox onboard --api-url https://lewbox.acme.dev        # remote: signs in, installs nothing
```

---

## 4. Conventions

### 4.1 Command shape

The pattern is `lewbox <noun> <verb> [args] [flags]`, the same as `gh` and `docker`.
- Nouns are singular, with the plural as an alias (`node` and `nodes`).
- The verbs are `add`, `list` (alias `ls`), `get`, `remove` (aliases `rm`, `delete`) and `install`.
- `add` means "register something that already exists". `create`/`new` mean "make something new". `create` is also accepted as an alias of `node add`.
- A command that takes `<id|name>` accepts either. Names can never be valid UUIDs, so there's no ambiguity.

Machine-wide commands have no noun: `onboard`, `login`, `logout`, `status`, `version`, `self-update`, `completion`, `orchestrator`, `migrate`.

### 4.2 Output

| | stdout | stderr | exit |
|---|---|---|---|
| Success, terminal | Text: tables for lists, `key: value` for one item | Progress, hints, update notice | 0 |
| Success, `--json` or piped | Exactly the HTTP response body, as one line | Progress only, never JSON | 0 |
| Expected failure (`invalid_request`, `not_found`, `conflict`) | Nothing | `{"error":{"code","message"}}` in JSON mode; `Error: <message>` otherwise | 1 |
| Usage error (unknown flag, wrong arguments) | Nothing | Cobra's message | 1 |

**Today the CLI always prints JSON.** Switching the default to text is the next CLI change. JSON stays byte-identical to the API. The existing parity test (`cmd/lewbox/parity_test.go`) becomes the test for `--json`, and it covers the piped case as well.

```
$ lewbox node list
NAME         URL                              ADDED
ap-south-1   http://10.0.0.7:8080             2m ago
eu-west-1    https://eu-west-1.internal:8443  5m ago

$ lewbox node list | jq -r '.data[].name'
ap-south-1
eu-west-1
```

### 4.3 Global flags and environment

Precedence (MVP §14): flags, then `LEWBOX_*`, then the config file, then defaults.

| Flag | Env | Meaning |
|---|---|---|
| `--api-url <url>` | `LEWBOX_API_URL` | Talk to a Lewbox server. Unset means local mode (§4.5) |
| `--org <id\|slug>` | `LEWBOX_ORG` | Organization, once there are organizations |
| `--json` | | JSON output (on automatically when stdout isn't a terminal) |
| `--yes`, `-y` | | Skip confirmations. Required for destructive commands in JSON mode |
| `--db <path>` | `LEWBOX_DB` | Local database (local mode only). Built |
| `--no-update-check` | `LEWBOX_NO_UPDATE_CHECK=1` | No update notice |

### 4.4 Sign-in and stored settings

- `lewbox login` opens the browser and saves a token. Without a browser it prints a URL and a code. `--key-stdin` reads an admin key (`lba_…`, MVP §2) from stdin, so it never shows up in `ps`.
- `lewbox logout` deletes the saved token.
- `lewbox status` shows: the mode (local or remote), the API URL and health, the signed-in account and organization, the database path, the CLI version and its path. `--check` exits 1 when not signed in or not healthy.
- Settings go in `~/.config/lewbox/config.json`. The token goes in `~/.config/lewbox/credentials.json`, mode `0600`. If a keyring is available we use it instead, but that's not in the first version.

### 4.5 Local and remote mode

This is how the "same code, two entry points" rule from the first build carries over to a remote CLI.

- **Local mode** (no `--api-url`, the default). Commands call `internal/service` in-process, against the local SQLite database. This is what's built. It is for the machine that runs `lewbox orchestrator`.
- **Remote mode** (`--api-url`, or after `lewbox login`). Commands call the same routes over HTTP with a small client.

The request and response types come from `internal/service`, so both modes print the same bytes.

**Implementation:** each command asks for a `Backend` interface. It has two implementations: `*service.Service`, and `httpclient.Client`, which has the same method set. A test runs every command against both, and checks that it matches the API.

### 4.6 Destructive and privileged actions

- `remove` and `uninstall`-style commands show what they will do and ask to confirm. `--yes` skips that.
- The CLI never runs as root. When a step needs root, like installing AerolVM, it runs that one step with `sudo` and prints the exact command first. Your config and database stay in your home directory, owned by you.

---

## 5. Commands

✅ built · 🔜 next · 📋 planned (milestone from MVP §18)

### Setup and account
| Command | Status |
|---|---|
| `lewbox onboard [--local\|--api-url <url>] [--install-aerolvm] [--yes]` | 🔜 |
| `lewbox login [--key-stdin] [--json]`, `lewbox logout` | 📋 M1 |
| `lewbox status [--check]` | 🔜 |
| `lewbox version` | ✅ |
| `lewbox self-update [--version <tag>]` | 📋 |
| `lewbox completion bash\|zsh\|fish\|powershell` | ✅ (from cobra) |

### Nodes
| Command | API | Status |
|---|---|---|
| `lewbox node add <name> --url <url>` (alias `create`) | `POST /v0/nodes` | ✅ |
| `lewbox node list` (alias `ls`) | `GET /v0/nodes` | ✅ |
| `lewbox node get <id\|name>` | `GET /v0/nodes/{ref}` | ✅ |
| `lewbox node remove <id\|name>` (aliases `rm`, `delete`) | `DELETE /v0/nodes/{ref}` | ✅ (no confirmation yet) |
| `lewbox node install [flags]`: install AerolVM here and add it as a node | local only | 🔜 (§6) |
| `lewbox node check <id\|name>`: reach `/health` and report the AerolVM version | `POST /v0/nodes/{ref}/check` | 🔜 |

### Clusters, workspaces and keys (MVP §7)
| Command | Status |
|---|---|
| `lewbox cluster add <slug> --admin-url … --endpoint-url … --runtime docker\|firecracker --admin-token-stdin` | 📋 M2 |
| `lewbox cluster list\|get\|check\|remove` | 📋 M2 |
| `lewbox workspace create\|list\|get\|suspend\|restore\|delete` | 📋 M3 |
| `lewbox key create <workspace> --cluster <slug>`, `lewbox key list\|revoke` | 📋 M3 |

### Server
| Command | Status |
|---|---|
| `lewbox orchestrator [--listen :8080]` | ✅ |
| `lewbox migrate [up\|status]` | 📋 M1 (migrations already run on open) |
| `lewbox health`, `lewbox hello [name]` | ✅ (examples, may be dropped) |

---

## 6. Installing AerolVM on this machine: `lewbox node install`

One command does all of this:
1. checks the machine;
2. runs AerolVM's own installer at a version we pin;
3. turns on the settings Lewbox needs (MVP §5);
4. adds the machine as a node.

It reuses AerolVM's installer instead of copying it, and it shows every privileged command before running it.

```
$ lewbox node install --domain eu-west.api.acme.dev --dns-api-token-stdin < cf-token
Checking this machine
  ✓ Ubuntu 24.04 (tested path: Ubuntu 22.04+), x86_64
  ✓ sudo works
  ✓ ports 443, 21212 free
AerolVM v0.7.0
  → downloaded install.sh (sha256 checked against the pinned value)
  → sudo bash install.sh --version v0.7.0 --domain eu-west.api.acme.dev \
      --dns-provider cloudflare --dns-api-token ****** --pat-scope host --node-name eu-west-1
  ✓ sandboxd healthy: https://eu-west.api.acme.dev/health
Lewbox settings
  ✓ /etc/sandboxd/sandboxd.env: SB_IDENTITY_ENABLED=true, SB_TOKEN_MAX_TTL=876000h
  ✓ sandboxd restarted, healthy
  ✓ node "eu-west-1" added (01a1c797-d94f-742c-a33a-18d5822e8c9f)
```

### 6.1 Flags

| `lewbox node install` | Passed to AerolVM `install.sh` | Notes |
|---|---|---|
| `--name <name>` (default: hostname, lowercased) | `--node-name <name>` | Also the Lewbox node name |
| `--mode local` (default without `--domain`) | `--local` | Development only: `127.0.0.1:21212`, no TLS, and public URLs don't work (REVIEW §5.4). Also works on macOS (installs as you, no sudo; OrbStack or Docker Desktop must be running) |
| `--domain <domain>` | `--domain <domain> --dns-provider cloudflare` | Production. Wildcard TLS by DNS-01 |
| `--dns-api-token-stdin` | `--dns-api-token <token>` | Cloudflare token with Zone:Read and DNS:Edit. Read from stdin, never as a flag value (see §6.4) |
| `--acme-email <email>` | `--acme-email <email>` | Recommended with `--domain` |
| `--public-host <host-or-ip>` | `--public-host <host-or-ip>` | IP mode, no TLS. For labs |
| `--runtime docker` (default) \| `firecracker` | nothing, or `--with-firecracker` | One runtime per cluster (MVP §11). Firecracker needs `/dev/kvm` on x86_64 or aarch64 and isn't allowed with `--mode local` |
| `--aerolvm-version <tag>` | `--version <tag>` | Default: the version this Lewbox release was tested with. Never lower than v0.6.13 (MVP §5) |
| `--no-register` | | Install AerolVM only; don't add a node |
| `--dry-run` | | Run the checks and print every command, but change nothing |
| `--yes` | | No prompts |
| always | `--pat-scope host` | The PAT only works from this host and cluster peers (MVP §5) |

AerolVM's other installer flags (gVisor, GPU, containerd, Caddy on S3, joining a cluster) aren't wrapped. Pass them through after `--`:

```bash
lewbox node install --domain … -- --lb-health-port 8081
```

### 6.2 What happens, in order

1. **Checks.** Linux, or macOS with `--mode local`. Ubuntu or Debian (other distros get a warning, not a stop). `sudo -v` succeeds. The ports are free. `/dev/kvm` exists for Firecracker. The domain resolves to this machine. Every failed check says how to fix it.
2. **Get the installer.** Download `install.sh` from `https://github.com/aerol-ai/microvm/releases/download/<tag>/install.sh`. **Not** the `curl https://aerolvm.aerol.ai | sudo bash` one-liner, because that always serves whatever is newest. Check its SHA-256 against a value built into this Lewbox release.
3. **Run it** with `sudo bash install.sh <mapped flags>`, streaming its output. The installer is safe to re-run: it keeps the existing PAT and settings in `/etc/sandboxd/node.json`. So running `node install` again repairs or upgrades the node.
4. **Lewbox settings.** The installer has no flag for identity mode. Lewbox sets `SB_IDENTITY_ENABLED=true` and `SB_TOKEN_MAX_TTL=876000h` in `/etc/sandboxd/sandboxd.env`, runs `systemctl restart sandboxd`, then waits for `/health`. Audit export (`SB_AUDIT_EXPORT_*`) comes later, with cluster registration (M2).
5. **Get an admin token.** Read the PAT from `/etc/sandboxd/sandboxd.env` (`SB_PAT_TOKEN`) with sudo, never from the installer's output. Use it over `127.0.0.1:21212` to mint an admin `avm_` token. After that, Lewbox only uses that token (MVP §4 trust boundaries). Storing it encrypted needs the M1 keyring. Until then, step 5 is skipped and the node is added without a token.
6. **Add the node.** Call `AddNode` with the name and the URL: `https://<domain>`, `http://<public-host>:21212`, or `http://127.0.0.1:21212`.

### 6.3 What it leaves on the machine

The AerolVM installer owns these files:
- binaries: `sandboxd` and `toolboxd` in `/usr/local/bin`;
- services: `sandboxd.service`, `sandboxd-healthcheck.timer` and Caddy;
- settings: `/etc/sandboxd/sandboxd.env` (secrets) and `/etc/sandboxd/node.json` (other settings);
- logs: `journalctl -u sandboxd`.

`lewbox status` shows the node's health. `lewbox node remove` **only unregisters** the node. AerolVM's installer has no uninstall, so we document the manual steps rather than guess them.

### 6.4 Security notes

- **The DNS token is visible to other users.** `install.sh` takes it as an argument (`--dns-api-token <token>`), so other local users can see it in `ps` while the installer runs. Lewbox reads it from stdin, but it can't avoid this hand-off. Ask upstream for a `--dns-api-token-file` (add it to MVP §17).
- **No pipe-to-bash.** Lewbox downloads a pinned installer and checks its hash. It never runs `curl | sudo bash`.
- **The PAT stays on the host.** It is read from the root-only env file, used once over loopback and never stored by Lewbox. `--pat-scope host` keeps it off the network.

---

## 7. Errors and exit codes

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed: an API error, a usage error, a failed check, or `status --check` when not signed in or not healthy |
| 130 | Interrupted (Ctrl-C) |

Error codes are the API's: `invalid_request`, `not_found`, `conflict`, `internal`. `node install` adds `precondition_failed` for a failed check, with `details` saying which check and how to fix it.

---

## 8. Open questions

1. **Sandbox commands in this CLI?** For `lewbox new`, `exec` and `ssh` in the boat style, the CLI would call the cluster directly with a cluster key, which keeps Lewbox out of the path. Or we leave this to AerolVM's own `aerolvm` CLI and document that.
2. **How nodes relate to clusters.** MVP treats a single node as a one-node cluster (§2). Do we keep `node` as its own thing that gets grouped into clusters, or does `node install` register a cluster?
3. **Where the installer lives.** `install.sh` from GitHub releases, or our own domain once there is one? `lewbox self-update` reads from the same place.
4. **Which AerolVM version to pin.** GitHub's "latest" redirect points at v0.7.0, but pkg.go.dev lists v0.7.23. Pick one and test it in the M0 spike.
5. **Checksum for `install.sh`.** We pin its hash per Lewbox release. Check whether AerolVM's `checksums.txt` also covers `install.sh`, so we can verify against upstream.
6. **Are installer edits kept?** Does a re-run of AerolVM's installer (or `--config-only`) keep the extra lines Lewbox writes to `sandboxd.env`? Test in the spike. If it doesn't, ask upstream for an `--identity` flag.
