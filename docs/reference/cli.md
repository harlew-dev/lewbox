---
title: CLI reference
description: Every lewbox command, its flags, and what it does.
---

`lewbox <noun> <verb> [args] [flags]`. Nouns also work in the plural (`nodes`, `clusters`, `users`, `orgs`, `members`, `admin-keys`, `keys`, `plans`, `regions`), and `list` has the alias `ls`; `remove` has `rm` and `delete`. Commands that take an `ID|NAME` accept either.

***

## Global flags

These work on every command.

| Flag | Env | |
|---|---|---|
| `--context NAME` | `LEWBOX_CONTEXT` | Use this saved sign-in |
| `--api-url URL` | `LEWBOX_API_URL` | Talk to this orchestrator (token from `LEWBOX_TOKEN` or a saved context) |
| `--org ID\|SLUG` | `LEWBOX_ORG` | The organization `member`, `admin-key` and `key` commands act on. Default: the context's, from `lewbox org use`; with an API key, its own |
| `--json` | `LEWBOX_OUTPUT=json` | Print JSON, byte-identical to the API. On automatically when output isn't a terminal |
| `-y`, `--yes` | | Skip confirmations. Required outside a terminal for commands that ask: `install`, `update`, and every `remove`/`disable` |
| `--db PATH` | `LEWBOX_DB` | The orchestrator database, in local mode. Default `~/.local/share/lewbox/lewbox.db` |
| `--keyring PATH` | `LEWBOX_KEYRING_FILE` | The keyring, in local mode. Default `keyring.key` next to the database |

How a command picks its orchestrator: [Sign in](../accounts/sign-in.md#which-orchestrator-a-command-uses).

***

## Getting started

### `lewbox version`

Prints the CLI version.

### `lewbox status [--check]`

Shows the context, mode (local or remote), API URL, health, signed-in account and version. `--check` exits 1 when the orchestrator isn't healthy or, in remote mode, you aren't signed in. Use it in scripts.

### `lewbox health`

Calls `GET /v0/health`.

### `lewbox completion bash|zsh|fish|powershell`

Prints a shell completion script.

***

## Sign-in

### `lewbox login [URL]`

Signs in and saves a context, then makes it current. Prompts for email and password on a terminal.

| Flag | |
|---|---|
| `--email EMAIL` | The account's email |
| `--password-stdin` | Read the password from stdin |
| `--key-stdin` | Save a token read from stdin instead of signing in. With an API key (`lba_…`), its organization becomes the context's default |
| `--context-name NAME` | Save it under this name. Default: the URL's host |

The URL defaults to `--api-url`. The output never contains the token.

### `lewbox logout [--all]`

Ends the session on the server and deletes the saved token, for the current context or (with `--all`) every context.

### `lewbox whoami`

The signed-in account, how you're signed in (`session` or `local`), and the context.

### `lewbox context list|use NAME|remove NAME`

Lists saved contexts (the current one is marked `*`), switches to one, or forgets one and its token. The built-in `local` context can't be removed.

***

## Orchestrator

### `lewbox serve`

Runs the orchestrator in the foreground. The same as `lewbox orchestrator serve`.

| Flag | Env | Default | |
|---|---|---|---|
| `--port N` | `LEWBOX_PORT` | `8080` | Listen on every interface on this port |
| `--listen ADDR` | `LEWBOX_LISTEN` | `:8080` | The whole listen address, such as `127.0.0.1:8080`. Not with `--port` |
| `--check-interval D` | `LEWBOX_CHECK_INTERVAL` | `1m` | How often to check every cluster. `0` turns it off |
| `--cookie-secure MODE` | `LEWBOX_COOKIE_SECURE` | `auto` | `auto` or `always`. `always` marks browser session cookies `Secure`, for a TLS proxy that does not set `X-Forwarded-Proto` |
| `--trusted-origin ORIGIN` | `LEWBOX_TRUSTED_ORIGINS` | | Extra origin the CSRF check accepts, repeatable (env: comma-separated) |

`LEWBOX_BOOTSTRAP_ADMIN_EMAIL` with `LEWBOX_BOOTSTRAP_ADMIN_PASSWORD` (or `…_PASSWORD_FILE`) creates the first instance admin when there are no accounts. See [Run the orchestrator](../setup/run-the-orchestrator.md).

### `lewbox orchestrator status`

Checks the orchestrator on this machine (same `--port`/`--listen` as `serve`) and prints whether it's listening, healthy, and its version.

***

## On a machine

These change the machine they run on. They refuse to run as root and use `sudo` for each privileged step, printing it first. [What Lewbox changes on a machine](what-lewbox-changes.md) lists every step.

### `lewbox install`

Checks the machine and installs AerolVM with its own installer, verified by checksum. Asks to confirm before installing (`--yes` skips).

```bash
lewbox install                                   # local mode
lewbox install --public-host 10.0.0.5            # IP mode, no TLS
lewbox install --domain eu.api.example.com --dns-api-token-stdin < token
lewbox install --public-host 10.0.0.5 --dry-run  # print, change nothing
lewbox install --public-host 10.0.0.5 -- --lb-health-port 8081   # pass flags to install.sh
```

| Flag | Default | |
|---|---|---|
| `--mode local\|public-host\|domain` | `local`, unless `--domain` or `--public-host` is set | How the node is reached |
| `--public-host HOST` | | IP mode: customers reach `http://HOST:21212` |
| `--domain DOMAIN` | | Domain mode with wildcard TLS through Cloudflare DNS |
| `--dns-api-token-stdin` | | Read the Cloudflare token from stdin. Required with `--domain` |
| `--acme-email EMAIL` | | Let's Encrypt contact for domain mode |
| `--runtime docker\|firecracker` | `docker` | Firecracker needs `/dev/kvm` and a non-local mode |
| `--name NAME` | short hostname | The node name |
| `--aerolvm-version TAG` | `v0.7.0` | Another AerolVM version (warns: untested). `latest` for the newest |
| `--from DIR` | `LEWBOX_AEROLVM_FROM` | Use AerolVM release files from `DIR/<tag>/` instead of GitHub |
| `--dry-run` | | Run the checks and print every command, changing nothing |

Outside local mode, AerolVM is installed but **not started**: `lewbox node add` starts or joins a cluster.

### `lewbox node add [NAME]`

Run on a machine after `lewbox install`.

```bash
lewbox node add node-1 --new-cluster eu-west   # start a new cluster here
lewbox node add node-2 --cluster eu-west       # join a cluster (slug or any node's name)
lewbox node add                                # local mode: register this machine as a one-node cluster
lewbox node add eu-1 --url https://eu-1:8443   # only record a node by URL; changes no machine
```

| Flag | Default | |
|---|---|---|
| `--new-cluster SLUG` | the node name | Start a new cluster with this slug |
| `--cluster REF` | | Join this cluster: its slug, or the name of one of its nodes. `cluster/x` or `node/x` when a name is ambiguous |
| `--role mixed\|server\|worker\|ingress` | `mixed` | The node's role in the cluster |
| `--advertise IP` | the public host's IP | The address other nodes use for this one |
| `--admin-url URL` | this machine's AerolVM URL | Where the orchestrator reaches the new cluster |
| `--endpoint-url URL` | `--admin-url` | What customers use |
| `--url URL` | | Record a node by URL only |

`NAME` defaults to the name given to `install`. Both forms register things with the orchestrator, so when signed in they need an instance admin. Running it again is safe. See [Start a cluster](../clusters/start-a-cluster.md) and [Add nodes](../clusters/add-nodes.md).

### `lewbox update`

```bash
lewbox update --check
lewbox update
lewbox update --aerolvm
```

Asks to confirm before replacing anything (`--yes` skips).

| Flag | |
|---|---|
| `--check` | Show what would change, change nothing |
| `--version TAG` | A specific Lewbox release. Default: the latest |
| `--aerolvm` | Also update AerolVM on this machine (refused on a multi-node cluster) |
| `--aerolvm-version TAG` | The AerolVM version to move to. Default: the one the release was tested with |
| `--release-from DIR` | `LEWBOX_RELEASE_FROM`: read the Lewbox release from a folder |
| `--from DIR` | `LEWBOX_AEROLVM_FROM`: read AerolVM's files from a mirror |

See [Update](../operations/update.md).

### `lewbox firewall enable`

Run on a node after `lewbox node add`. Turns on ufw with the ports this node needs: SSH, its install mode's ports, the cluster ports from the private network only, and AerolVM's sandbox rules. If ufw has other rules, it asks `Do you want to reset your firewall? [Y/n]`: yes resets ufw to Lewbox's rules only, no keeps them.

```bash
lewbox firewall enable
lewbox firewall enable --peers 10.0.0.0/16 --allow 8080/tcp
lewbox firewall enable --reset=false --dry-run
```

| Flag | Default | |
|---|---|---|
| `--peers CIDR` | the private network of this node's cluster address | Where cluster ports are open from. Repeatable. Refused if a current node falls outside it |
| `--allow PORT[/PROTO]` | | Also open this port, or `START:END` range, to anyone. `tcp` unless `/udp`. Repeatable |
| `--reset` | ask | `--reset` resets without asking, `--reset=false` keeps other rules. Needed outside a terminal when there are other rules, unless `--yes` (which resets) |
| `--dry-run` | | Read everything, print every command, change nothing |

See [Turn on the firewall](../operations/firewall.md).

***

## Clusters

### `lewbox cluster add SLUG`

Registers a cluster that already exists, after checking it. See [Register an existing cluster](../clusters/register-an-existing-cluster.md).

| Flag | |
|---|---|
| `--admin-url URL` | Required. Where the orchestrator reaches it |
| `--admin-token-stdin` | Required. Read the `avm_…` admin token from stdin |
| `--endpoint-url URL` | What customers use. Default: `--admin-url` |
| `--runtime docker\|firecracker` | Default `docker` |
| `--name NAME` | Display name. Default: the slug |

### `lewbox cluster list`

Every cluster with its runtime, status, node count, version and endpoint.

### `lewbox cluster get ID|SLUG`

One cluster: its fields, the last check results, and its nodes. Doesn't contact the cluster.

### `lewbox cluster check ID|SLUG`

Runs every check now and syncs the node list. See [Health and checks](../clusters/health-and-checks.md).

### `lewbox cluster join-command ID|SLUG`

Prints a command that joins a machine without the Lewbox CLI, with a fresh single-use join token.

| Flag | Default | |
|---|---|---|
| `--role ROLE` | `mixed` | The new node's role |
| `--ttl DURATION` | `15m` | How long the token is valid |

### `lewbox cluster remove ID|SLUG`

Stops managing the cluster: revokes Lewbox's admin token on it and forgets it and its nodes. The cluster keeps running. Asks to confirm.

***

## Nodes

### `lewbox node list [--cluster REF]`

Every node, or one cluster's, with its cluster, role, version, whether it's alive, and its URL.

### `lewbox node get ID|NAME`

One node.

### `lewbox node remove ID|NAME`

Drains the node, removes it from its cluster in AerolVM, and forgets it. Asks to confirm. `--force` removes a node that still owns sandboxes. Refuses a cluster's last node and the node behind its admin URL. See [Remove nodes and clusters](../clusters/remove-nodes-and-clusters.md).

***

## Accounts

### `lewbox user create EMAIL [--instance-admin] [--password-stdin]`

Creates an account. Prompts for the password twice on a terminal.

### `lewbox user list` · `lewbox user get ID|EMAIL`

Lists accounts (admins only), or shows one (admins, or the account itself).

### `lewbox user set-password ID|EMAIL [--password-stdin]`

Sets a password: admins for anyone, anyone for themselves. Ends that account's other sessions.

### `lewbox user disable ID|EMAIL` · `lewbox user enable ID|EMAIL`

Blocks sign-in and ends every session, or allows sign-in again. `disable` asks to confirm.

### `lewbox user remove ID|EMAIL`

Deletes the account, its sessions and its memberships. Asks to confirm. The last enabled instance admin can't be disabled or removed.

***

## Organizations

See [Organizations and API keys](../accounts/organizations-and-api-keys.md).

### `lewbox org create SLUG [--name NAME] [--owner EMAIL] [--plan SLUG]`

Creates an organization. Instance admins only. The owner defaults to you (none in local mode); the plan defaults to the instance's default plan.

### `lewbox org list [--user ID|EMAIL] [--status STATUS]` · `lewbox org get [ID|SLUG]`

Lists the organizations you can see (instance admins: all of them), or one. `--user` lists that account's organizations and its role in each; `--status` keeps `pending`, `active` or `suspended` ones. `get` with no argument shows `--org`, the context's default, or an API key's own.

### `lewbox org update ID|SLUG [--name NAME] [--plan SLUG] [--max-sandboxes N] [--max-vcpus N] [--max-memory SIZE]`

Renames an organization (owners, admins and instance admins) or, for instance admins, re-plans it or sets its own limits. `--max-memory` takes `4096`, `4096MB` or `4GB`; `0` or `unlimited` falls back to the plan's limit.

### `lewbox org suspend ID|SLUG` · `lewbox org restore ID|SLUG` · `lewbox org approve ID|SLUG`

Instance admins only. Suspending stops the organization's API keys and leaves its members read-only; restoring and approving make it `active` again. `suspend` asks to confirm.

### `lewbox org use ID|SLUG`

Makes it the current context's default organization for `member`, `admin-key` and `key`. Not available in local mode: pass `--org` there.

### `lewbox member list` · `add EMAIL [--role ROLE]` · `set-role EMAIL|ID ROLE` · `remove EMAIL|ID`

The organization's members. Roles are `owner`, `admin`, `member` (default) and `viewer`. Owners and admins manage members; only owners manage owners; anyone can remove themselves. The last owner stays. `remove` asks to confirm.

### `lewbox admin-key create NAME [--expires-in DUR | --never-expires]`

Creates an API key and prints its `lba_…` token, once. `--expires-in` takes `30d`, `720h` and so on, from 1 hour to 3650 days; the default is `90d`. Owners and admins.

### `lewbox admin-key list` · `lewbox admin-key revoke ID|NAME`

Lists the keys, revoked ones included, with their status and last use; or revokes one at once. Revoke asks to confirm.

### `lewbox key create NAME [--region SLUG] [--expires-in DUR | --never-expires]`

Creates a sandbox key for the organization's team and prints its `avm_…` token, once, with `SB_API_URL`/`SB_API_TOKEN` and, in a `docker` region, the E2B settings. `--region` defaults to the only ready region. `--expires-in` defaults to `90d` and takes the same durations as `admin-key`. Owners, admins and members.

### `lewbox key list` · `lewbox key revoke ID|NAME`

Lists the keys, live ones first and revoked ones included; or revokes one at once. A member can revoke only the keys they created; owners and admins can revoke any. Revoke asks to confirm.

***

## Customers

See [Customers, plans and limits](../accounts/customers-and-plans.md).

### `lewbox region list`

The clusters customers can use: ready or degraded ones, with their endpoint, runtime and status. Any signed-in account or API key.

### `lewbox settings` · `lewbox settings set [--name NAME] [--signup MODE] [--default-plan SLUG]`

Shows the instance settings, or changes them. Instance admins only. `--signup` is `open`, `approval` or `closed`; `--default-plan` is the plan new sign-ups get.

### `lewbox plan list` · `lewbox plan show ID|SLUG`

Every plan with its limits, organization count and whether it is the default, or one plan.

### `lewbox plan create SLUG [--name NAME] [--max-sandboxes N] [--max-vcpus N] [--max-memory SIZE]`

Creates a plan. A missing or cleared limit is unlimited.

### `lewbox plan update ID|SLUG [--name NAME] [--max-sandboxes N] [--max-vcpus N] [--max-memory SIZE]`

Changes a plan. `--max-memory` takes `4096`, `4096MB` or `4GB`; `0` and `unlimited` clear a limit.

### `lewbox plan delete ID|SLUG`

Deletes a plan no organization uses and that is not the default. Asks to confirm.
