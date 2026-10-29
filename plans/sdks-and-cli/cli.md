# CLI

> Status: Draft · Milestone: Alpha (M2 core, M4 complete) · Updated: 2026-10-29

`lewbox` is the command line for Lewbox: create and manage sandboxes, run commands, move files, open a shell, expose ports, prompt coding agents, build templates, and, with an admin key, manage a project. It's the same Go binary as the server ([D1](../README.md#decisions)): `lewbox serve` runs a control plane, `lewbox node` a node, and every other command is the CLI. It's built on the public [Go client](go.md).

## Today

No CLI exists. The public docs already document one for every example (`/tmp/lewbox-docs/*.md`, the `cli` code blocks): installed with `npm install --global @lewbox/cli`, signed in with `lewbox login`, and about 60 subcommands, listed in [Command tree](#command-tree). The docs also fix its behaviour in places:

- `lewbox login` saves the key in `~/.config/lewbox/config.json`, after checking it; `--api-key` and `LEWBOX_API_KEY` win over the saved key ([authentication](https://lewbox.com/docs/getting-started/authentication)).
- `lewbox config set api-url …`, `config set cluster …` ([configuration](https://lewbox.com/docs/getting-started/configuration)).
- Errors print their message, code and request id to stderr, and exit `1`. `lewbox commands run` exits with the command's own exit code instead; an `Error:` line on stderr tells them apart ([errors](https://lewbox.com/docs/basics/errors)).
- Lists fetch every page unless `--limit`; `--json` prints the API's JSON without the `data` wrapper, as one array for lists ([pagination](https://lewbox.com/docs/basics/pagination)).
- Operations are waited for unless `--no-wait`; `--quiet` prints only an id ([operations](https://lewbox.com/docs/basics/operations)).
- Timeouts: `--timeout` (each request, 30 s) and `--wait-timeout` (waiting, 10 minutes), in seconds.
- `lewbox shell "$SANDBOX"`: an interactive login shell; `lewbox commands stream "$SANDBOX" --pty -- python3 -i` ([interactive commands](https://lewbox.com/docs/inside-a-sandbox/interactive-commands)).
- `lewbox commands run` always streams, stdout to stdout and stderr to stderr ([streaming output](https://lewbox.com/docs/inside-a-sandbox/streaming-output)).

AerolVM ships a CLI and an MCP server in one binary; legacy's `docs/sdks.md` (open decision 5) wanted the same for Lewbox.

## Goals

- Everything in the API is reachable from the CLI, for scripts, CI and agents.
- Scriptable: `--quiet` for ids, `--json` for machine output, stable exit codes, no prompts when stdin isn't a terminal.
- Pleasant: tables, colours, progress, and interactive prompts where they help, all off in pipes.
- One download for everything: the CLI, the control plane and the node agent.

## Design

### Command tree

`lewbox <group> <verb> [args] [flags]`. A sandbox id is the first positional argument wherever one is needed. Server-side commands sit beside the client ones.

#### Client commands (profile or sandbox key)

| Command | Does | API |
|---|---|---|
| `login [--api-url URL]` | Asks for a key (hidden input), checks it with `GET /profile`, saves it | |
| `logout` | Forgets the saved key | |
| `config set|get|unset|list <key> [value]` | `api-key`, `api-url`, `cluster`, `admin-key`, `output` (`table`/`json`), `color` | |
| `profile` | Your profile: clusters, default cluster, limits, usage | `GET /v0/profile` |
| `profile update --default-cluster NAME` | | `PATCH /v0/profile` |
| `sandboxes create --image IMG | --template ID [--cluster] [--cpu] [--memory] [--disk] [--idle-timeout] [--lifetime] [--persist] [--env K=V]… [--metadata K=V]… [--repository SPEC]… [--git-access read\|write\|full] [--on-create CMD]… [--on-start CMD]… [--on-stop CMD]… [--hooks FILE] [--stop-grace SECONDS] [--scrub PATH]… [--no-wait] [--idempotency-key]` | Create; waits until running. `--git-access` sets the sandbox's Git access (default `full`); a `--repository` spec may end `:read`, `:write` or `:full` for one repository. `--on-*` add hooks as plain commands; `--hooks` reads the full `hooks` object (labels, timeouts, background) from JSON or YAML ([sandboxes/hooks](../sandboxes/hooks.md)) | `POST /v0/sandboxes` |
| `sandboxes hooks SBX [--set FILE] [--scrub PATH]… [--stop-grace SECONDS]` | Show the hooks and their last runs; with `--set`, replace them | `GET`/`PATCH /v0/sandboxes/{sbx}` |
| `sandboxes touch SBX` | Count as activity, putting off an idle stop | `POST /v0/sandboxes/{sbx}/touch` |
| `sandboxes list [--status] [--cluster] [--metadata K=V]… [--limit N]` | | `GET /v0/sandboxes` |
| `sandboxes get SBX` | | `GET /v0/sandboxes/{sbx}` |
| `sandboxes update SBX [--metadata K=V]… [--unset-metadata K]… [--idle-timeout S|none] [--lifetime S|none]` | | `PATCH` |
| `sandboxes start|stop|restart SBX [--no-wait]` | Waits for the operation | `POST …/operations` |
| `sandboxes delete SBX [--no-wait] [--yes]` | Asks to confirm on a terminal | `DELETE` |
| `sandboxes keys create SBX [--permission P]…` | Makes a sandbox key; prints the token | `POST …/keys` |
| `operations get SBX OP` | | `GET …/operations/{op}` |
| `operations wait SBX OP` | Waits until it's finished; exits `1` if it failed | polls |
| `commands run SBX [--directory] [--env K=V]… [--timeout S] [--label] -- CMD…` | Streams output; exits with the command's code | `POST …/commands` (SSE) |
| `commands stream SBX [--pty] [--cols] [--rows] -- CMD…` | Interactive: your terminal's stdin and stdout connected | `…/commands/stream` (WebSocket) |
| `shell SBX [--directory]` | An interactive login shell (a PTY session, attached) | sessions + attach |
| `sessions create SBX [--pty] [--directory] [--env]… [--watch] [-- CMD…]` | | `POST …/sessions` |
| `sessions list SBX [--status]` / `get SBX SES` | | |
| `sessions log SBX SES [--tail N] [--follow]` | | `…/log` |
| `sessions attach SBX SES` | Replays, then follows; `Ctrl-] q` detaches | `…/attach` |
| `sessions signal SBX SES INT|TERM|KILL|HUP|QUIT` / `resize SBX SES --cols --rows` / `kill SBX SES` | | `PATCH`, `DELETE` |
| `files read SBX PATH [--output FILE]` | To stdout, or a file | `GET …/files` |
| `files write SBX PATH [--from FILE]` | From a file or stdin; makes directories | `PUT …/files` |
| `files list SBX PATH [--depth N]` / `stat SBX PATH` | | `…/directories`, `…/entries` |
| `files mkdir SBX PATH` / `rename SBX FROM TO` / `remove SBX PATH` | | |
| `files cp SRC DST` | `SBX:PATH` on either side, like `scp`; directories with `-r` (tar over a command) | |
| `ports list SBX` | | `GET …/ports` |
| `ports expose SBX PORT [--tcp] [--private] [--persist]` | Prints the URL | `PUT …/ports/{port}` |
| `ports close SBX PORT` | | `DELETE` |
| `ports link SBX PORT [--path P] [--open]` | A one-minute link to a private port; `--open` opens it in your browser | `POST …/links` |
| `ssh list SBX` / `ssh add SBX NAME [--key FILE]` / `ssh remove SBX NAME` | `add` defaults to `~/.ssh/id_ed25519.pub` | `…/ssh-keys` |
| `ssh connect SBX [--port 22]` | Exposes 22 over TCP if needed, adds your key, runs `ssh` ([SSH guide](https://lewbox.com/docs/guides/ssh-access)) | |
| `activity [SBX] [--kind] [--group] [--outcome] [--label] [--since] [--until] [--follow]` | | `…/activity` |
| `activity record SBX --kind app.… [--label] [--outcome] [--message] [--detail K=V]…` | | `POST …/activity` |
| `agents list SBX` | Models, modes, thinking levels | `GET …/agents` |
| `conversations start SBX --agent claude-code|codex [--directory] [--worktree [--branch] [--from] [--name]] [--model] [--mode] [--effort] [--fast] [--on-unanswered allow|deny] [--instructions T | --instructions-file F] [--output-schema F] [--no-follow] -- PROMPT` | Starts and follows the turn: prints text as it arrives, asks about approvals and questions on a terminal | `POST …/conversations` + events |
| `conversations prompt CNV [--queue|--no-queue] [--steer] [--no-follow] -- PROMPT` | | `POST …/messages` |
| `conversations list [--sandbox] [--agent] [--status]` / `get CNV` | | |
| `conversations messages CNV [--after] [--json]` | The transcript as text; `--json` as JSON lines | |
| `conversations events CNV --turn MSG [--after N] [--json]` | Follows a turn | SSE |
| `conversations reply CNV REQUEST [--option ID | --answers JSON | --decline]` | | `POST …/replies` |
| `conversations cancel CNV [--turn MSG]` / `steer CNV MSG` / `queued CNV` | | |
| `conversations tasks CNV` / `stop-task CNV TASK` | | |
| `conversations delete CNV [--force] [--keep-worktree]` | | |
| `templates list [--cluster]` / `get ID` | | |
| `templates create ID --name N --dockerfile FILE [--cluster C]… [--port P/proto/access]… [--file template.json] [--wait]` | | `POST /v0/templates` |
| `templates rebuild ID [--dockerfile FILE] [--wait]` / `delete ID` | | |
| `templates builds ID [BLD] [--follow]` | A build's status and log | |
| `repositories list [--search] [--provider]` / `branches OWNER/REPO` | The profile's repositories on its Git providers | `GET /v0/profile/git/repositories` |
| `git token REPO… [--permission contents=write …] [--provider github]` | Issue an installation token for the profile and print it (for scripts: `git push https://x-access-token:$(lewbox git token acme/web --permission contents=write)@github.com/acme/web.git`) | `POST /v0/profile/git/tokens` |
| `git token --user LOGIN --profile PRF [--provider github]` | A person's user token; with an admin key only (profile keys can't get user tokens) | `POST /v0/projects/{prj}/profiles/{prf}/git/tokens` |
| `git people` / `git accounts` | The profile's Git people and accounts on every provider, and the limit | `GET …/git` |

#### Admin commands (admin key)

`lewbox admin …` uses `LEWBOX_ADMIN_KEY` or `--admin-key`, against the Admin API ([api](../api.md)):

| Command | Does |
|---|---|
| `admin profiles list|get|create|update|delete|suspend|resume` | |
| `admin profiles keys PRF list|create|revoke` | Prints a new key once |
| `admin profiles limits PRF get|set KIND=VALUE[/PERIOD]…` | |
| `admin profiles setup-link PRF [--section]… [--return-url]` | Prints the link |
| `admin profiles agents PRF list|set-key AGENT --provider P --secret-file F|remove AGENT` | |
| `admin profiles git PRF get|set|remove`, `admin profiles telemetry PRF get|set|remove`, `admin profiles ssh-keys PRF …` | |
| `admin clusters list|get|create|update|suspend|resume` | |
| `admin clusters install CLU [--name] [--subdomain]` | Makes an installation; prints the install command |
| `admin nodes list [--cluster]|get|drain|update|remove` | |
| `admin sandboxes list|get|start|stop|delete [--profile]` | Any profile's sandboxes |
| `admin templates …` | Provided templates |
| `admin webhooks list|create|update|delete|roll-secret|test|deliveries|redeliver` | |
| `admin activity [--profile] [--sandbox] …` | |
| `admin keys list|create|revoke` | Admin keys, with a session only ([open question](#open-questions)) |

#### Plugins

Each plugin compiled in may add commands under its name ([plugins/extension-points](../plugins/extension-points.md#cli-commands)), from the plugin's `cli` package, so the slim `lewbox-cli` build has them too:

| Command | Plugin |
|---|---|
| `admin credits buckets PRF list|create|update|renew`, `admin credits transactions PRF list|create --amount --reference`, `admin credits usage PRF [--period]` | `credits` (Beta) |
| `admin limits PRF get|set KIND=VALUE[/PERIOD]…` | `limits` (also as `admin profiles limits`) |
| `git tokens create --repository R --permission contents=write [--provider github]` | `git/github` through the Git framework |
| `admin plugins list`, `admin plugins enable|disable NAME` | Core: the project's plugins ([projects/settings](../projects/settings.md#plugins)) |
| `plugins list`, `plugins info NAME`, `build --with MODULE --without NAME` | Core, local: the plugins in this binary, and custom builds ([plugins/building-and-enabling](../plugins/building-and-enabling.md#custom-builds)) |

A command calling a plugin that's off for the project prints the API's message ("Credits are turned off for this project") and exits `1`.

#### Server and node commands

Not API clients; documented in their own plans:

| Command | Plan |
|---|---|
| `serve` | [foundation/binary-and-commands](../foundation/binary-and-commands.md) |
| `migrate up|down|status` | [foundation/database](../foundation/database.md) |
| `node install|run|status|update|uninstall|check` | [clusters/node-agent](../clusters/node-agent.md), [clusters/installation](../clusters/installation.md) |
| `agentd …`, `portd` (hidden) | [agents/agentd](../agents/agentd.md), [inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md) |
| `instance admins add|remove EMAIL`, `instance projects create NAME --owner EMAIL` | [projects/instance-administration](../projects/instance-administration.md): talks to the database directly, for bootstrapping a self-hosted instance |
| `webhooks verify --secret S < delivery.json` | Checks a saved delivery, for debugging a receiver |
| `version` | Versions of the CLI, and of the server it's pointed at |

### Config

- **Order:** a flag, then the environment (`LEWBOX_API_KEY`, `LEWBOX_ADMIN_KEY`, `LEWBOX_API_URL`, `LEWBOX_CLUSTER`), then the config file, then defaults.
- **File:** `~/.config/lewbox/config.json` (`$XDG_CONFIG_HOME`, or `%AppData%\lewbox` on Windows), mode `0600`, as the docs say. Keys go in the OS keychain instead when one's available (`zalando/go-keyring`), with the file holding only a marker; `--insecure-storage` keeps them in the file.
- **Contexts** for several instances or profiles: `lewbox config use acme-prod`, each context its own `api-url`, keys and cluster. `--context` on any command. The docs' single-file shape is the default context.
- `lewbox login` refuses an admin key (it asks for a profile key) and `lewbox login --admin` the reverse, by prefix.

### Output

- **Default** is human: tables for lists (width-aware, ids never truncated), key-value blocks for one resource (as the docs' `files stat` example), colours for statuses, relative times. Off when stdout isn't a terminal, or with `NO_COLOR`, or `--color never`.
- `--json` prints the API's JSON without the `data` wrapper: one object, or one array for lists. `--jsonl` prints one object per line, for lists and streams (`conversations messages --json` prints JSON lines, as the docs show).
- `--quiet` prints only ids, one per line (or the operation id with `--no-wait`), as the docs use: `SANDBOX=$(lewbox sandboxes create --image ubuntu:24.04 --quiet)`.
- **Progress** for waits goes to stderr ("Creating sbx_… · setup_applied · 4.2 s"), so stdout stays clean.
- `--limit N` stops lists after N; otherwise every page is fetched.

### Exit codes

| Code | Means |
|---|---|
| `0` | Success |
| `1` | An API error (message, code and request id printed to stderr as `Error: …`), a failed operation, or a usage error |
| `2` | Bad flags or arguments |
| `124` | A wait or `--timeout` ran out (like `timeout(1)`) |
| `130` | Interrupted by `Ctrl-C` |
| *n* | `commands run`, `commands stream`, `shell`: the command's own exit code |

### Waiting and streaming

- Lifecycle commands wait for their operation by default, polling from 1 s backing off to 5 s, with `--wait-timeout` (10 minutes). `Ctrl-C` stops waiting but not the operation, and says how to follow it (`lewbox operations wait SBX OP`).
- `commands run` uses SSE and writes bytes as they arrive. `--timeout` is the command's own timeout in the sandbox, as the API takes it.
- `shell`, `commands stream --pty` and `sessions attach` put the local terminal in raw mode, send resizes on `SIGWINCH`, and restore the terminal on exit, even on a panic. A dropped connection reattaches once, with the replay, before giving up.
- `conversations start|prompt` follow the turn: text to stdout, tool calls as one-line summaries to stderr, approvals and questions asked interactively on a terminal (or answered by `--on-unanswered` when not), `--json` for the raw events.

### Idempotency and retries

From the [Go client](go.md#transport): creates send an `Idempotency-Key` (`--idempotency-key` to choose one, for a CI job retried by its runner); network errors, `429`, `502`, `503` and `504` are retried with backoff, honouring `Retry-After`, except `429` `limit_exceeded`.

### Shell completion and help

- `lewbox completion bash|zsh|fish|powershell`. Completion of sandbox ids, conversation ids, template ids and cluster names asks the API, with a short cache.
- `--help` on every command, with examples taken from the docs, so they stay the same.

### Distribution

The docs promise `npm install --global @lewbox/cli`. The CLI is a Go binary, so:

1. **GitHub Releases:** `lewbox_<version>_<os>_<arch>.tar.gz` for linux, darwin and windows on amd64 and arm64, with checksums and signatures (cosign or minisign), from the release pipeline ([foundation/build-and-release](../foundation/build-and-release.md)).
2. **npm:** `@lewbox/cli` with per-platform packages as optional dependencies (`@lewbox/cli-linux-x64`, `@lewbox/cli-darwin-arm64`, …), each holding the binary, as esbuild and Biome do. The main package's `bin` is a tiny Node launcher that runs the right binary. No postinstall download.
3. **Install script:** `curl -fsSL https://lewbox.com/install | sh` detects the platform, verifies the checksum, and installs to `~/.local/bin` or `/usr/local/bin`. The node installer is a different script ([clusters/installation](../clusters/installation.md)).
4. **Homebrew:** a tap, `harlew-dev/tap/lewbox`.
5. **Container image:** `ghcr.io/harlew-dev/lewbox`, which is also how `lewbox serve` is usually run.

The same binary is ~40 MB with the dashboard embedded. If that's too heavy for a CLI download, a build tag (`-tags cli`) leaves out the server, the dashboard and the in-sandbox helpers, for a ~12 MB CLI-only binary that the npm package and Homebrew ship ([open question](#open-questions)).

### Updates

`lewbox version` shows the CLI's version and, if configured, the server's (`GET /up` returns it). Once a day at most, on a terminal, the CLI checks the latest release and prints one line on stderr if it's newer. `LEWBOX_NO_UPDATE_CHECK=1` turns that off, and it's off in CI (`CI` set).

## Security

- Hidden input for keys at `login`; keys never printed except when made (`keys create`), and then only once.
- Keychain storage by default; config file `0600`.
- `--debug` logs requests and responses to stderr with `Authorization`, `Sec-WebSocket-Protocol`, `env` values and secret fields redacted.
- `ssh connect` adds only a public key; it never sends a private one.

## Testing

- **Golden output tests:** each command's table, block, `--json` and `--quiet` output against the fake server, compared with files in `testdata/`.
- **Docs examples:** every `cli` block in `docs/` runs in CI against `lewbox serve --fake-cluster`, with a script that sets `$SANDBOX` and friends; output is checked where the docs show it.
- **TTY tests:** `shell` and `commands stream --pty` through a pseudo-terminal (`creack/pty`): raw mode, resize, detach, restore on exit.
- **Exit codes:** a table test over the codes above.
- **npm package:** installs on each platform in CI and runs `lewbox --version`.

## Build plan

1. `internal/cli`: command framework (Cobra, or `ff`/`kong`), config and contexts, output (tables, JSON, quiet), errors and exit codes. (M2)
2. `login`, `profile`, `sandboxes`, `operations`. (M2)
3. `commands run|stream`, `shell`, `sessions`, `files` (with `cp`), `ports`, `ssh`. (M2)
4. `conversations`, `agents`. (M3)
5. `templates`, `repositories`, `activity`. (M4)
6. `admin …`. (M4)
7. Completion, update check, keychain. (M4)
8. Distribution: releases, npm packages, install script, Homebrew. (M4)

## Open questions

1. **One binary or two?** Ship the full binary everywhere (simple), or a `-tags cli` build for npm and Homebrew (smaller)? (Proposed: CLI-only build for npm and Homebrew; full binary in releases and the container image.)
2. **Command framework:** Cobra (familiar, big) or Kong (struct-based, smaller)?
3. **Admin keys from the CLI:** may an admin key create other admin keys? Legacy said no (tenant keys were dashboard-only).
4. **`lewbox mcp`**: an MCP server exposing sandboxes as tools for agents, after the alpha?
5. **Windows:** in the first release, or later? `shell` and PTYs need ConPTY handling.
