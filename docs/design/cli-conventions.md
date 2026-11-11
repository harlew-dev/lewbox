# Lewbox CLI: conventions and setup

> [!NOTE]
> **Design notes.** This page records how the CLI was designed and what was verified while building it. To *use* Lewbox, start at the [docs home](../README.md).

How every command behaves. The list of commands is in [cli-commands.md](cli-commands.md).

The CLI should feel like the [boat.dev CLI](https://docs.boat.dev/quickstart):
- you install it with one line;
- one guided command sets everything up;
- commands are short and readable;
- output is plain text for people and JSON for scripts.

---

## 1. What we take from boat, and what we don't

| boat.dev | Lewbox |
|---|---|
| `curl -fsSL https://boat.dev/install \| sh`, links into `~/.local/bin` | Same (§2) |
| `boat onboard`: one guided, re-runnable setup | `lewbox onboard` (§3). It also offers to install AerolVM here |
| `boat login` in the browser, `--key-stdin` for CI | `lewbox login`, same shape (§6) |
| `boat status [--check]` | `lewbox status [--check]` |
| Global `--api-url`, `--org`, `--json`, `--no-update` | Same, plus `--context` (§5) |
| Text by default; `--json` for machines; JSON on automatically when piped | Same. JSON output is **byte-identical to the HTTP API body** (§4.2) |
| On failure in JSON mode, the last line is a JSON error with a non-zero exit | Same shape as the API: `{"error":{"code","message"}}` on stderr, exit 1 |
| `delete` asks to confirm; `--yes` for scripts | Same for anything destructive (§8) |
| `boat completions <shell>` | Cobra's `lewbox completion <shell>` |
| Silent self-update every 6 hours | **Not copied.** This CLI installs system software with `sudo`, so it never updates itself silently. `lewbox update` is manual, and an update notice goes to stderr |
| Installer opens Stripe checkout | **Not copied.** Billing isn't part of setup |
| Sandbox commands (`new`, `exec`, `ssh`, `stop`) | **Later, and not through Lewbox.** Lewbox is not in the sandbox path (MVP §1). See README open question 5 |

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

It needs no root. `LEWBOX_VERSION=v0.2.0` pins a release, and `LEWBOX_INSTALL_DIR` changes the target folder. After that, `lewbox update` replaces the binary in the same place.

For development, run `go install ./cmd/lewbox` from the repo.

**CI and Docker.** Download the static binary from the release, then sign in without a browser:

```bash
printf '%s' "$LEWBOX_API_KEY" | lewbox login https://lewbox.acme.dev --key-stdin --json
```

---

## 3. First run: `lewbox onboard`

`onboard` asks only for what isn't set up yet, so it is safe to run again. It is a guided wrapper around `login`, `install`, `node add` and `orchestrator enable`, and it prints each command it runs, so you can see what to script later.

```
$ lewbox onboard
Where is your Lewbox?
  › On this machine (start an orchestrator here)
    A Lewbox server (https://…)

Run sandboxes on this machine too?
  › Yes, install AerolVM here
    No

Checking this machine
  ✓ Ubuntu 24.04, x86_64, 16 vCPU, 62 GiB
  ✓ sudo works
  ✓ /dev/kvm present (Firecracker is possible)
How will customers reach this node?
  › Local only, for development (127.0.0.1:21212, no TLS)
    A domain, with TLS through Cloudflare DNS
    This machine's public IP, no TLS

Cluster
  › Start a new cluster
    Join a cluster (pick one: eu-west, us-north)
…
→ lewbox install --mode local
→ lewbox node add lab-1
→ lewbox orchestrator enable --port 8080
✓ Node "lab-1" is in cluster "lab-1". Next: lewbox node list
```

Every question has a flag, so `onboard` also runs without a terminal:

```bash
lewbox onboard --local --install-aerolvm --mode local --yes
lewbox onboard --api-url https://lewbox.acme.dev --install-aerolvm --domain eu-west.api.acme.dev --cluster eu-west-1 --yes
lewbox onboard --api-url https://lewbox.acme.dev        # remote: signs in, installs nothing
```

---

## 4. Command shape and output

### 4.1 Command shape

The pattern is `lewbox <noun> <verb> [args] [flags]`, the same as `gh` and `docker`.
- Nouns are singular, with the plural as an alias (`node` and `nodes`).
- The verbs are `add`, `create`, `list` (alias `ls`), `get`, `update`, `remove` (aliases `rm`, `delete`).
  - `add` means "bring in something that already exists": a machine (`node add`) or a cluster (`cluster add`).
  - `create` means "make something new in Lewbox": a workspace, a key, an account.
- A command that takes `<id|name>` accepts either. Names can never be valid IDs, so there's no ambiguity.
- Machine-wide commands have no noun: `onboard`, `install`, `update`, `login`, `logout`, `whoami`, `status`, `version`, `completion`, `serve`.
- `lewbox serve` is the one shortcut: it is `lewbox orchestrator serve`. We don't add more shortcuts unless a command is typed many times a day.

### 4.2 Output

| | stdout | stderr | exit |
|---|---|---|---|
| Success, terminal | Text: tables for lists, `key: value` for one item | Progress, hints, update notice | 0 |
| Success, `--json` or piped | Exactly the HTTP response body, as one line | Progress only, never JSON | 0 |
| Expected failure (`invalid_request`, `not_found`, `conflict`, `unauthorized`, `forbidden`, `precondition_failed`) | Nothing | `{"error":{"code","message","details"}}` in JSON mode; `Error: <message>` and a hint otherwise | 1 |
| Usage error (unknown flag, wrong arguments) | Nothing | Cobra's message | 2 |

**Today the CLI always prints JSON**, and usage errors exit 1. Switching the default to text is the next CLI change. JSON stays byte-identical to the API. The existing parity test (`cmd/lewbox/parity_test.go`) becomes the test for `--json`, and it covers the piped case as well.

```
$ lewbox node list
NAME         CLUSTER   ROLE    VERSION   HEALTH
eu-west-1    eu-west   mixed   v0.7.0    healthy
eu-west-2    eu-west   mixed   v0.7.0    healthy
lab-1        lab-1     mixed   v0.7.0    healthy

$ lewbox node list | jq -r '.data[].name'
eu-west-1
eu-west-2
lab-1
```

Commands that do work on the machine (`install`, `node add`, `update`) stream progress to stderr as they go. In JSON mode, the final result on stdout is one object that says what changed.

Secrets that are shown once (`admin-key create`, `key create`) go to stdout only. With text output they come with a warning on stderr that they won't be shown again.

---

## 5. Global flags, environment and modes

Precedence (MVP §14): flags, then `LEWBOX_*`, then the config file, then defaults.

| Flag | Env | Meaning |
|---|---|---|
| `--context <name>` | `LEWBOX_CONTEXT` | Which saved sign-in to use (§6) |
| `--api-url <url>` | `LEWBOX_API_URL` | Talk to this Lewbox server. Overrides the context |
| `--org <id\|slug>` | `LEWBOX_ORG` | Organization. Overrides the context's default (`org use`) |
| `--json` | `LEWBOX_OUTPUT=json` | JSON output (on automatically when stdout isn't a terminal) |
| `--yes`, `-y` | | Skip confirmations. Required for destructive commands in JSON mode |
| `--db <path>` | `LEWBOX_DB` | Local database (local mode only). Built (SQLite today; Postgres is `--database-url` from M1) |
| `--no-update-check` | `LEWBOX_NO_UPDATE_CHECK=1` | No update notice |

Flags for `orchestrator serve` (also accepted by `serve` and `orchestrator enable`):

| Flag | Env | Default | Meaning |
|---|---|---|---|
| `--port <n>` | `LEWBOX_PORT` | `8080` | Listen on all interfaces on this port |
| `--listen <addr>` | `LEWBOX_LISTEN` | `:8080` | The whole listen address. Built. Using it together with `--port` is an error |
| `--public-url <url>` | `LEWBOX_PUBLIC_URL` | | The URL people and clusters reach. It is used for ingest URLs and links in emails |
| `--database-url <url>` | `LEWBOX_DATABASE_URL` | | Postgres (M1) |
| `--config <file>` | `LEWBOX_CONFIG` | `lewbox.yaml`, if present | Config file (MVP §14) |

### 5.1 Local and remote mode

This is how the "same code, two entry points" rule from the first build carries over to a remote CLI.

- **Local mode.** It is on when there is no `--api-url` and no signed-in context, or with `--context local`. Commands call `internal/service` in-process, against the database. This is what's built. It is for the machine that runs the orchestrator. **Anyone who can open the database is an instance admin**, so local mode has no sign-in. This is how the first account is made: `lewbox user create you@acme.dev --instance-admin` on the orchestrator host.
- **Remote mode** (`--api-url`, or after `lewbox login`). Commands call the same routes over HTTP with a small client, using the saved session or admin key.

The request and response types come from `internal/service`, so both modes print the same bytes.

**Implementation:** each command asks for a `Backend` interface. It has two implementations: `*service.Service`, and `httpclient.Client`, which has the same method set. A test runs every command against both, and checks that it matches the API.

Commands that change this machine (`install`, `node add`, `update --aerolvm`) work in either mode. They change the machine locally, and talk to whichever Lewbox the mode points at.

---

## 6. Sign-in and stored settings

- `lewbox login [<url>]` prompts for email and password, and saves a CLI session token. `--password-stdin` reads the password from stdin. `--key-stdin` reads an admin key (`lba_…`, MVP §2) from stdin instead, so it never shows up in `ps`. A browser and device-code sign-in comes when the console has one.
- Each sign-in is saved as a **context**: a name, the API URL, the token and a default organization. `login` names it after the host (`lewbox.acme.dev`) unless you pass `--context-name`. `context use` switches between them. `local` is a built-in context that is always there.
- `lewbox logout` ends the session on the server and deletes the token. `--all` does that for every context.
- `lewbox whoami` shows the account, the organization and the role you'd act as.
- `lewbox status` shows: the mode and context, the API URL and health, the account and organization, this machine's AerolVM and node (if any), the database path, and the CLI version and its path. `--check` exits 1 when not signed in or not healthy.
- Settings go in `~/.config/lewbox/config.json` (contexts, defaults). Tokens go in `~/.config/lewbox/credentials.json`, mode `0600`. If a keyring is available we use it instead, but that's not in the first version.

---

## 7. Who can run what

The server checks every permission. The CLI only shows a clear message on `forbidden`.

| Commands | Who |
|---|---|
| `org create`, `org suspend`, `user …` (except `passwd`) | Instance admins, or local mode |
| `cluster add\|update\|remove\|upgrade`, `cluster join-command`, `node add` (it mints a join token), `node drain\|remove`, `member …`, `admin-key …` | Organization `owner` or `admin` |
| `workspace …`, `key …`, `limit …`, `credits grant`, `webhook …` | Organization `owner` or `admin` (and admin keys) |
| Every `list` and `get` | Any organization member, `viewer` and up |
| `install`, `update`, `orchestrator …` | Whoever is on the machine, with `sudo` where needed. No Lewbox role, because they don't change Lewbox data. `node add` does, so it needs the role above |

---

## 8. Destructive and privileged actions

- `remove`, `revoke`, `delete`, `disable`, `suspend` and `cluster upgrade` show what they will do and ask to confirm. `--yes` skips that.
- The CLI never runs as root, and refuses to when `$SUDO_USER` is set. When a step needs root, like running the AerolVM installer, it runs that one step with `sudo` and prints the exact command first, with secrets masked. Your config and database stay in your home directory, owned by you.
- Secrets are never taken as flag values. Every secret has a `--…-stdin` flag (`--password-stdin`, `--key-stdin`, `--admin-token-stdin`, `--dns-api-token-stdin`).

---

## 9. Errors and exit codes

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed: an API error, a failed check, or `status --check` when not signed in or not healthy |
| 2 | Usage error (matches `aerolvm`) |
| 130 | Interrupted (Ctrl-C) |

Error codes are the API's: `invalid_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `internal`. The commands that change this machine add `precondition_failed` for a failed check, with `details` saying which check and how to fix it.
