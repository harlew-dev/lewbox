# Lewbox CLI

> [!NOTE]
> **Design notes.** This page records how the CLI was designed and what was verified while building it. To *use* Lewbox, start at the [docs home](../README.md).

Status: 2026-11-11. This proposes the whole command set. What is built is marked ✅ and is tested end to end against real AerolVM nodes ([deploy/dev-cluster](../../deploy/dev-cluster/README.md)); everything else is planned.

The files in this folder:

| File | What's in it |
|---|---|
| `cli-commands.md` (this file) | Every command in one table, and the common flows |
| [conventions.md](cli-conventions.md) | Command shape, output, global flags, sign-in and stored settings, local vs remote mode, exit codes |
| [aerolvm.md](aerolvm-integration.md) | What happens on the machine: `lewbox install`, `lewbox node add` (start or join a cluster), `lewbox update`, removing a node |

This replaced the first draft ([archive/cli-first-draft.md](archive/cli-first-draft.md)). Its content moved into these three files, and some commands were renamed (see [Changes from what's built](#what-changed-from-the-first-build)).

---

## The common flows

**1. One machine, for development.** Run the orchestrator and AerolVM on your laptop.

```bash
curl -fsSL https://github.com/harlew-dev/lewbox/releases/latest/download/install.sh | sh
lewbox install --mode local          # AerolVM on 127.0.0.1:21212, no TLS
lewbox node add                       # starts a one-node cluster named after this host and registers it
lewbox serve                          # orchestrator on :8080
```

`lewbox onboard` asks these questions and does the same thing.

**2. A production cluster on three machines.** The orchestrator runs somewhere else, already set up.

```bash
# On the first machine
lewbox login https://lewbox.acme.dev
lewbox install --domain eu-west.api.acme.dev --dns-api-token-stdin < cf-token
lewbox node add eu-west-1 --new-cluster eu-west     # starts cluster "eu-west" with this node in it

# On each of the other machines
lewbox login https://lewbox.acme.dev
lewbox install --domain eu-west.api.acme.dev --dns-api-token-stdin < cf-token
lewbox node add eu-west-2 --cluster eu-west-1       # joins whichever cluster eu-west-1 is in
```

`--cluster` takes the cluster's slug, or the name of any node already in it.

**3. Register a cluster that already exists.** It was set up with AerolVM's own tools, not with Lewbox.

```bash
aerolvm token create --name lewbox --role admin --team '*' --expires-in 36500d \
  | lewbox cluster add us-north --admin-url https://10.0.1.5:21212 \
      --endpoint-url https://us-north.api.acme.dev --runtime docker --admin-token-stdin
```

**4. Run the orchestrator as a service.**

```bash
lewbox orchestrator enable --port 8000 --public-url https://lewbox.acme.dev   # systemd unit, started at boot
lewbox orchestrator status
lewbox orchestrator logs -f
```

**5. Keep everything up to date.**

```bash
lewbox update                 # just the CLI
lewbox update --aerolvm       # the CLI, then AerolVM on this machine
lewbox cluster upgrade eu-west  # rolling AerolVM upgrade of a whole cluster
```

---

## Commands

✅ built · 🔁 built, renamed or changed by this doc · 🔜 next · 📋 planned (milestone from [MVP.md](../../MVP.md) §18) · ❓ open question

**Runs on** says where the command must run:
- **this machine**: it changes the machine you're on, and runs one step at a time with `sudo`;
- **orchestrator host**: it needs the database directly (local mode);
- **anywhere**: it calls the Lewbox API, either in-process in local mode or over HTTP once you're signed in ([conventions.md §5](cli-conventions.md#51-local-and-remote-mode)).

### Getting started

| Command | What it does | Runs on | Status |
|---|---|---|---|
| `lewbox onboard [--local\|--api-url <url>] [--install-aerolvm] [--yes]` | Guided setup. It asks only for what isn't done yet, so it is safe to run again | anywhere | 📋 |
| `lewbox status [--check]` | Shows the mode, API URL and health, account, organization, this machine's AerolVM and node, and the CLI version. `--check` exits 1 if anything is wrong | anywhere | ✅ |
| `lewbox version [--json]` | Prints the CLI version and the AerolVM version this release is tested with | anywhere | ✅ (prints the CLI version only) |
| `lewbox completion bash\|zsh\|fish\|powershell` | Prints a shell completion script | anywhere | ✅ (from cobra) |

### Install and update (this machine)

Details in [aerolvm.md](aerolvm-integration.md).

| Command | What it does | Runs on | Status |
|---|---|---|---|
| `lewbox install [--mode local] [--domain <d> --dns-api-token-stdin] [--public-host <h>] [--runtime docker\|firecracker] [--aerolvm-version <tag>] [--dry-run] [-- <install.sh flags>]` | Checks this machine and installs AerolVM with a pinned, hash-checked installer. Server installs are left **stopped** until `node add` starts or joins a cluster. `--mode local` starts straight away | this machine | ✅ |
| `lewbox install --dry-run` | Runs every check and prints every command, but changes nothing. Use it as a "can this machine run AerolVM?" check | this machine | ✅ |
| `lewbox update [--version <tag>] [--check]` | Updates the CLI itself. Never automatic | this machine | ✅ |
| `lewbox update --aerolvm [--aerolvm-version <tag>]` | Updates the CLI, then AerolVM on this machine, to the version the new CLI is tested with. On a cluster with more than one node it refuses and points you to `cluster upgrade` | this machine | ✅ (tested v0.6.13 → v0.7.0) |
| `lewbox firewall enable [--peers <cidr>] [--allow <port[/proto]>] [--reset[=false]] [--dry-run]` | Turns on ufw with the ports this node needs (mode ports, cluster ports from the private network only, AerolVM's sandbox node-guard rules). Offers to reset ufw when it has rules Lewbox doesn't set (`[Y/n]`) | this machine | ✅ |
| `lewbox uninstall` | Prints the manual steps to remove AerolVM. AerolVM's installer has no uninstall, so Lewbox doesn't guess | this machine | ❓ (AerolVM releases now ship `uninstall.sh`) |

### Orchestrator (the Lewbox server)

| Command | What it does | Runs on | Status |
|---|---|---|---|
| `lewbox orchestrator serve [--port <n>\|--listen <addr>] [--public-url <url>] [--database-url <url>] [--config <file>]` | Runs the orchestrator in the foreground: API, console, portal and background jobs. `--port 8000` means `:8000`; `--listen 127.0.0.1:8000` sets the whole address. Giving both is an error | orchestrator host | ✅ |
| `lewbox serve [flags]` | Shorter name for `orchestrator serve`, with the same flags | orchestrator host | ✅ |
| `lewbox orchestrator enable [serve flags] [--user <u>]` | Installs a `lewbox-orchestrator` systemd unit that runs `orchestrator serve` with these flags, enables it at boot and starts it | orchestrator host | 📋 |
| `lewbox orchestrator disable` | Stops the service and removes the unit. The database and keyring are kept | orchestrator host | 📋 |
| `lewbox orchestrator status` | Is it running, on which address, which version, database reachable, migrations current, keyring loaded | orchestrator host | ✅ (listening, health, version) |
| `lewbox orchestrator logs [-f] [--since <dur>]` | `journalctl -u lewbox-orchestrator`, as JSON lines | orchestrator host | 📋 |
| `lewbox orchestrator migrate [up\|status]` | Runs or shows database migrations. They also run on start (`migrate_on_start`) | orchestrator host | 📋 (migrations run on start) |
| `lewbox orchestrator config` | Prints the effective config, with each value's source (flag, env, file, default). Secrets are masked | orchestrator host | 📋 |
| `lewbox orchestrator keyring init\|rotate` | Creates or rotates the envelope-encryption keyring (MVP §14). Back it up: without it, cluster admin tokens can't be decrypted | orchestrator host | 📋 (a keyring file is created on first use) |
| `lewbox health` | Calls `GET /v0/health` | anywhere | ✅ |
| `lewbox hello [name]` | Example command | anywhere | removed |

### Sign-in and contexts

| Command | What it does | API | Status |
|---|---|---|---|
| `lewbox login [<url>] [--email <e> --password-stdin] [--token[=<t>]]` | Signs in to a Lewbox server and saves a context. It prompts for email and password. `--token` saves an access token (`lbp_…`) or admin key (`lba_…`) instead, read from stdin or a no-echo prompt (`--key-stdin` is the old name). A browser and device-code flow comes later | `POST /v0/auth/login` | ✅ |
| `lewbox token create\|list\|revoke [--user <email>]` | An account's access tokens (D38) | `/v0/access-tokens` | ✅ |
| `lewbox logout [--all]` | Ends the session and deletes the saved token, for the current context (or all of them) | `POST /v0/auth/logout` | ✅ |
| `lewbox whoami` | Account, whether it is an instance admin, how you're signed in (session or local) and the context. Organizations and roles come later | `GET /v0/me` | ✅ |
| `lewbox context list\|use <name>\|remove <name>` | One context per Lewbox server you use (for example `local`, `prod`). `--context` and `LEWBOX_CONTEXT` choose one for a single command | local only | ✅ |

### Accounts and organizations

| Command | What it does | API | Status |
|---|---|---|---|
| `lewbox user create <email> [--instance-admin] [--password-stdin]` | Creates an account; prompts for the password on a terminal. On the orchestrator host, in local mode, this is also how you create the first instance admin (or set `LEWBOX_BOOTSTRAP_ADMIN_EMAIL` and `…_PASSWORD` for `serve`). An email invite comes with SMTP | `POST /v0/users` (add to MVP §12) | ✅ |
| `lewbox user list\|get <email\|id>` | Lists or shows accounts (instance admins; anyone can get their own) | `GET /v0/users`, `GET /v0/users/{ref}` | ✅ |
| `lewbox user disable\|enable <email\|id>` | Blocks or allows sign-in; disabling ends the account's sessions. The last enabled instance admin can't be disabled | `POST /v0/users/{ref}/disable\|enable` | ✅ |
| `lewbox user remove <email\|id>` | Deletes an account and its sessions. Asks to confirm; refuses the last enabled instance admin | `DELETE /v0/users/{ref}` | ✅ |
| `lewbox user set-password <id\|email> [--password-stdin]` | Sets a password (instance admins, or yourself). Ends that account's other sessions. A reset-by-email flow comes with SMTP | `PUT /v0/users/{ref}/password` | ✅ |
| `lewbox passwd` | Changes your own password | ❓ | 📋 (use `user set-password <you>`) |
| `lewbox org create <slug> --name <name> [--owner <email>]` | Creates an organization (instance admins). The owner defaults to you | `POST /v0/organizations` | ✅ |
| `lewbox org list\|get\|update\|suspend\|restore <slug>` | `list --user <email>` lists an account's organizations; `update --name` renames | `GET/PATCH /v0/organizations` | ✅ |
| `lewbox org use <slug>` | Sets the default organization for this context. Global `--org`/`LEWBOX_ORG` overrides it; over HTTP it is the `Lewbox-Organization` header | local only | ✅ |
| `lewbox member add <email> --role owner\|admin\|member\|viewer` | Gives an existing account a role | `POST /v0/members` | ✅ (until invitations: add needs an existing account) |
| `lewbox member invite <email> --role owner\|admin\|member\|viewer` | Invites someone to the current organization | `POST /v0/invitations` | 📋 M1 (needs email) |
| `lewbox member list\|set-role <email> <role>\|remove <email>` | The last owner always stays | `/v0/members` | ✅ |
| `lewbox invitation list\|revoke <id>` | | `/v0/invitations` | 📋 M1 |
| `lewbox admin-key create <name> [--expires-in <dur>\|--never-expires]` | Creates an organization API key. The `lba_…` token is shown once. Default `--expires-in 90d` | `POST /v0/admin-keys` | ✅ |
| `lewbox admin-key list\|revoke <id\|name>` | A key reads its own organization only; it can't change members or keys | `/v0/admin-keys` | ✅ |

### Clusters

A Lewbox cluster is one AerolVM cluster. A single machine is a one-node cluster (MVP §2).

| Command | What it does | API | Status |
|---|---|---|---|
| `lewbox cluster add <slug> --admin-url <url> --endpoint-url <url> --preview-domain <d> --runtime docker\|firecracker --admin-token-stdin` | Registers a cluster that already exists, and validates it (MVP §7.1) | `POST /v0/clusters` | ✅ |
| `lewbox cluster list` | Slug, runtime, endpoint, nodes, health | `GET /v0/clusters` | ✅ |
| `lewbox cluster get <slug>` | Everything about one cluster, including its nodes and what the last check found | `GET /v0/clusters/{id}` | ✅ |
| `lewbox cluster check <slug>` | Runs validation now and prints each requirement from MVP §5 as ✓ or ✗, with how to fix it | `POST /v0/clusters/{id}/check` | ✅ |
| `lewbox cluster setup <slug>` | Prints the settings each node needs: the `SB_AUDIT_EXPORT_*` snippet with this cluster's ingest URL and HMAC key, identity and TTL settings, and the create policy for its runtime | `GET /v0/clusters/{id}` | 📋 |
| `lewbox cluster update <slug> [--endpoint-url …] [--admin-url …] [--admin-token-stdin] [--name …]` | Changes endpoints, or rotates the admin token | `PATCH /v0/clusters/{id}` | 📋 |
| `lewbox cluster remove <slug>` | Unregisters the cluster. It refuses while workspaces are entitled to it, unless you pass `--force`. Nothing on the cluster is changed | `DELETE /v0/clusters/{id}` | ✅ (no `--force` check for workspaces yet: there are none) |
| `lewbox cluster join-command <slug> [--role worker\|ingress\|server\|mixed] [--ttl 2h] [--reusable]` | Prints the `install.sh … join` line for a machine that won't run the Lewbox CLI. It wraps `aerolvm join-command` | `POST /v0/clusters/{id}/join-tokens` | ✅ |
| `lewbox cluster upgrade <slug> [--version <tag>] [--batch <n\|n%>] [--pause-after <n>]` | Rolling AerolVM upgrade of every node. The default is the version this Lewbox release is tested with | `POST /v0/clusters/{id}/upgrade` | 📋 |
| `lewbox cluster upgrade status\|pause\|resume\|rollback <slug>` | | `/v0/clusters/{id}/upgrade` | 📋 |

### Nodes

| Command | What it does | Runs on | Status |
|---|---|---|---|
| `lewbox node add [<name>] --cluster <cluster\|node>` | **Joins this machine to an existing cluster.** `--cluster` takes the cluster's slug, or the name of any node in it. It gets a single-use join token through the orchestrator, then runs `install.sh join`. Needs `lewbox install` first | this machine | ✅ |
| `lewbox node add [<name>] [--new-cluster <slug>]` | **Starts a new one-node cluster on this machine** (`install.sh init`), and registers it. The slug defaults to the node name, and the node name defaults to the hostname | this machine | ✅ |
| `lewbox node add <name> --url <url>` | Registers a node by URL, without touching any machine. Replaced by `cluster add` in M2 | anywhere | ✅ (kept for registering a node by hand) |
| `lewbox node list [--cluster <slug>]` | Every node, with its cluster, role, version, health and whether it's cordoned. Nodes come from each cluster's member list | anywhere | ✅ |
| `lewbox node get <id\|name>` | | anywhere | ✅ |
| `lewbox node check <id\|name>` | Reaches the node's `/health`, and reports its AerolVM version and runtimes | anywhere | 📋 (`cluster check` covers it) |
| `lewbox node cordon\|uncordon <name>` | Stops or resumes new sandboxes on the node | anywhere | 📋 |
| `lewbox node drain <name> [--timeout 10m] [--migrate]` | Cordons it and moves or waits out its sandboxes | anywhere | 📋 |
| `lewbox node remove <id\|name> [--force] [--yes]` | Drains the node, takes it out of its cluster, and unregisters it. AerolVM stays installed on the machine. It refuses the cluster's last node (use `cluster remove`) and the node behind the cluster's admin URL. `--force` deletes a node that still owns sandboxes | anywhere | ✅ |

### Workspaces, keys and limits (MVP §7.2–7.6)

| Command | API | Status |
|---|---|---|
| `lewbox workspace create <name> [--external-id <id>] [--cluster <slug>…\|--all-clusters]` | `POST /v0/workspaces` | 📋 M3 |
| `lewbox workspace list\|get <id\|name>` | `GET /v0/workspaces` | 📋 M3 |
| `lewbox workspace suspend\|restore <id\|name>` | `PATCH /v0/workspaces/{id}` | 📋 M3 |
| `lewbox workspace delete <id\|name>` | `DELETE /v0/workspaces/{id}` | 📋 M3 |
| `lewbox workspace entitle\|unentitle <workspace> <cluster>` | `PUT/DELETE /v0/workspaces/{id}/clusters/{cluster}` | 📋 M3 |
| `lewbox workspace member invite\|list\|remove <workspace> …` | `/v0/workspaces/{id}/members`, `/invitations` | 📋 M7 |
| `lewbox key create <workspace> --cluster <slug> [--name <n>] [--expires-in <dur>\|--never-expires]` | `POST /v0/workspaces/{id}/keys`. The `avm_…` token is shown once | 📋 M3 |
| `lewbox key list <workspace>`, `lewbox key revoke <id>` | `/v0/workspaces/{id}/keys`, `DELETE /v0/keys/{id}` | 📋 M3 |
| `lewbox limit get <workspace>`, `lewbox limit set <workspace> [--cluster <slug>] --max-sandboxes <n> --max-cpu <n> --max-memory <size> [--mode strict\|shared]` | `GET/PUT /v0/workspaces/{id}/limits` | 📋 M3 |
| `lewbox sandbox list <workspace> [--cluster <slug>]`, `lewbox snapshot list <workspace>` | Read from the mirror. Read only: Lewbox stays out of the sandbox path | 📋 M4 |

### Billing, webhooks and audit (MVP §9, §12)

| Command | API | Status |
|---|---|---|
| `lewbox rate-card create\|list\|get` | `/v0/rate-cards` | 📋 M5 |
| `lewbox credits list <workspace>`, `lewbox credits grant <workspace> <units> --reference <ref>` | `/v0/workspaces/{id}/credits/*` | 📋 M5 |
| `lewbox budget get\|set <workspace>` | `/v0/workspaces/{id}/budgets` | 📋 M5 |
| `lewbox usage [--group-by workspace\|cluster\|runtime\|sandbox] [--from] [--to] [--csv]` | `GET /v0/usage` | 📋 M4 |
| `lewbox webhook create\|list\|get\|update\|remove\|test` | `/v0/webhooks` | 📋 M5 |
| `lewbox webhook redeliver <delivery-id>` | `/v0/webhooks/{id}/deliveries/{id}/redeliver` | 📋 M5 |
| `lewbox audit list [--since] [--actor] [--action]` | `GET /v0/audit-events` | 📋 M1 |
| `lewbox portal get\|set [--enabled] [--signup open\|invite] …` | `/v0/portal-settings` | 📋 M7 |

---

## What changed from the first build

| First build | Now | Why |
|---|---|---|
| `lewbox orchestrator [--listen :8080]` | `lewbox orchestrator serve [--port\|--listen]`, short form `lewbox serve` | `orchestrator` is now a group (`serve`, `status`; later `enable`, `logs`, `migrate`…). The bare `orchestrator` still serves, with a warning on stderr |
| `lewbox node add <name> --url <url>` only | `lewbox node add [--new-cluster\|--cluster]` on the machine itself; `--url` still works; `cluster add` for clusters that already exist | Lewbox registers clusters and gets their nodes from AerolVM (MVP §2, §7.1) |
| `lewbox node remove` unregistered only | drains, takes the node out of the cluster, unregisters, asks to confirm | A node gone from Lewbox but still in the cluster would keep taking sandboxes, and the next sync would add it back |
| `lewbox node install` (old doc) | `lewbox install` + `lewbox node add` | A node that has booted alone can't then join a cluster ([aerolvm.md §1](aerolvm-integration.md#1-why-install-and-node-add-are-separate)) |
| `lewbox self-update` (old doc) | `lewbox update [--aerolvm]` | One command for the CLI and the AerolVM on this machine |
| `lewbox hello` | removed | It was an example |
| Every route open | Every route except `/v0/health` and `/v0/auth/login` needs a session token; local mode (direct database access) needs none | Accounts (M1) |
| JSON only | Text on a terminal, JSON with `--json` or when piped (still byte-identical to the API); exit 2 for usage errors | [conventions.md §4.2](cli-conventions.md#42-output) |
| `key create` for cluster keys (old doc) | unchanged; admin keys get their own `admin-key` noun | `key` alone was ambiguous between `lba_` and `avm_` |

---

## Open questions

1. **Accounts API.** The `/v0/users` routes (create, list, get, remove, disable, enable, password) are built but not yet in MVP §12. Add them there.
2. **`--cluster` takes a node or a cluster.** Built as described: if a cluster slug and a node name match different clusters, the command fails with `conflict` and asks for `cluster/<slug>` or `node/<name>`. We could instead make slugs and node names share one namespace per organization, so this can't happen.
3. **Sandbox commands** (`lewbox new`, `exec`, `ssh`). Either they call the cluster directly with a cluster key, or we point people to `aerolvm`. Lewbox itself stays out of the sandbox path either way.
4. **Where releases live** (GitHub, or our own domain). `update` reads `release.json`, `checksums.txt` and `lewbox_<os>_<arch>` from `LEWBOX_RELEASE_URL` (default: GitHub releases of harlew-dev/lewbox, which has none yet).
5. **Docker installs.** `lewbox update` inside the orchestrator image should refuse and tell you to pull a new image. `orchestrator enable` makes no sense there either.
6. **Organizations.** Everything is instance-wide today: a user is an instance admin or read-only. Organizations, members and admin keys (MVP M1) change every permission check.

Answered by the dev cluster (2026-11-11): an `--role admin --team '*'` token can mint join tokens; AerolVM removes members with `DELETE /v1/cluster/nodes/{id}`; see also [aerolvm.md §7](aerolvm-integration.md#7-questions-answered-by-the-dev-cluster).
