# agentd: the runner in the sandbox

> Status: Draft · Milestone: Alpha (M3) · Core, with per-agent drivers from the agent plugins · Updated: 2026-10-29

`lewbox agentd` is the small daemon Lewbox runs inside a sandbox to drive coding agents. It's the [Agent Client Protocol](https://agentclientprotocol.com) (ACP) client: it starts each conversation's adapter (`claude-agent-acp`, `codex-acp`), sends it prompts, turns what the agent does into Lewbox's [events](turns-and-events.md), holds the queue, waits for replies to approvals and questions, and sends everything up to the control plane. One daemon serves every conversation in its sandbox.

It's a port of legacy `lewbox-agentd`. The ACP side is kept almost as it is; what changes is how the control plane reaches it.

## Today

- **Code:** `lewbox-gateway-legacy/cmd/lewbox-agentd/main.go`, `internal/agentd/*.go` (`daemon.go` 719 lines, `conversation.go` 2,207, `chunks.go` 659, `tasks.go` 553, `agents.go` 443, `worktree.go` 434, `options.go` 403, `events.go` 363, `rpc.go` 262, `upload.go` 237, `codexproxy.go` 173, `config.go` 193, `install.go` 88), with ~2,900 lines of tests including a scripted fake adapter (`fake_adapter_test.go`, the test binary re-exec'd with `AGENTD_FAKE_ADAPTER=1`).
- **Gateway side:** `internal/agents/agents.go` (380 lines). It uploads the build to `/usr/local/lib/lewbox/agentd-<version>` (part file, chmod checked by `stat` for gVisor, move), starts it as toolbox session `lewbox-agentd-<rand>` running `exec <path> serve` in a login shell, and calls it by writing the request to `/usr/local/lib/lewbox/call-<rand>.json` and running `agentd call --file` through Execute (120 s timeout).
- **Subcommands:** `serve`, `call [JSON | --base64 | --file]` (exit `3` when no daemon answers), `events --conversation --after --turn|--message --follow --idle-exit --open-idle-exit --wait-uploaded`, `options`; and, with `LEWBOX_AGENTD_CODEX` set, a stand-in for `codex` (`codexproxy.go`).
- **Built and checked live** with both agents on 2026-10-21 (`0ebe406`), then extended almost daily: queue and steer (`053107e`), per-turn cost (`4c370f2`), Codex command output (`b89e993`), finished held until uploaded (`56a4a76`), file-based calls (`4776361`), open tool calls ended `unreported` (`84a9a50`), worktrees (`b84bbb0`, `5b4392d`), background tasks (`f5adfcc`), tool call timings (`ae373a4`), telemetry (`5b56a01`), instructions (`6d95391`), output schemas (`5bb00fe`).

**Problems:**

1. **A process per command.** Each reply, cancel or status check is a file write plus a toolbox Execute: 100–300 ms of overhead, exposed to gVisor's lost exit statuses, and 128 KiB was the first limit hit (`4776361`: Linux caps one argument at 128 KiB, so requests moved from `--base64` to `--file`).
2. **A process per follower.** Each SSE follower runs `agentd events --follow` over an exec-stream WebSocket ([turns-and-events](turns-and-events.md#where-events-come-from)).
3. **No way to reach the control plane but HTTP uploads.** agentd already calls home (`POST /v1/agent-uploads`), but commands can't flow the other way on the same path.
4. **Memory grows with chats.** Each conversation keeps its adapter and its MCP servers loaded for 10 minutes after its last turn (`LEWBOX_AGENTD_ADAPTER_IDLE`), 2 hours with background work. Lewagent saw 17 chats hold ~2.5 GB of a 12 GB computer (`lewagent-legacy/docs/todo.md`, 2026-10-23).
5. **Codex's first start races.** Two Codex chats started at once on a new computer: one failed in 9 s with "failed to initialize sqlite state runtime under /root/.codex", as both set up `~/.codex` together (2026-10-23).

## Goals

- Port agentd and its tests with the smallest possible change to its ACP logic.
- Commands, events and uploads over one long-lived WebSocket that agentd dials home over: the [agent channel](#the-agent-channel). Decided for the alpha (2026-10-29). The toolbox is used only to install and start agentd.
- Bounded memory: unload idle adapters sooner when memory is short.
- No first-start races between conversations.
- One binary to build and ship to sandboxes, for both architectures.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The ACP client, event mapping, numbering, JSONL log, chunker, queue, steer, replies, tasks, worktrees, options probe, Codex stand-in, upgrade handshake, login-shell environment, telemetry, installing adapters from pinned npm packages, every `LEWBOX_AGENTD_*` setting |
| **Change** | Talks to the control plane over the [agent channel](#talking-to-agentd). Shipped inside the [sandbox binary](#the-binary), not as its own. Started with a sandbox-scoped token. Unloads by memory as well as idle time. Serializes adapter starts |
| **Add** | `add_worktree` / `remove_worktree` by worktree id ([worktrees](worktrees.md)); per-conversation `env`, `skills`, `mcp_servers`, `allowed_tools` ([conversations](conversations.md#environment-skills-mcp-servers-and-tools)); slash commands in `options`; `parent_call_id` on sub-agents' events where the adapter reports it |
| **Drop** | `call --base64`; `call --file` and the per-call JSON files in `/usr/local/lib/lewbox/`; `events --follow` over exec-stream; `POST /v1/agent-uploads`. The control plane no longer drives agentd through the toolbox: it installs and starts it that way, then everything goes over the channel ([no second path](#no-second-path)) |

## Design

### The binary

The control plane's binary can't embed itself, and it's large (dashboard, both architectures' helpers). So the module builds a second, small binary for sandboxes, from the same source with a build tag:

```
go build -tags sandbox -o lewbox-sandbox-linux-amd64 ./cmd/lewbox   # CGO_ENABLED=0
go build -tags sandbox -o lewbox-sandbox-linux-arm64 ./cmd/lewbox
```

- With the `sandbox` tag, `cmd/lewbox` registers only `agentd` and `portd` (and `version`), so it's a few MB with no dashboard, database or HTTP server code.
- Both builds are embedded in `lewbox serve` with `go:embed` and uploaded on demand, by the sandbox's architecture, as legacy uploads its helpers (`internal/privateports`, `internal/agents`). [foundation/binary-and-commands](../foundation/binary-and-commands.md) owns the build; [foundation/build-and-release](../foundation/build-and-release.md) the release.
- It's invoked as `lewbox agentd …` and `lewbox portd …`, so commands read the same everywhere. In the sandbox the file is `/usr/local/lib/lewbox/lewbox-<version>`, with `lewbox` symlinked to the current one.
- A template may preinstall it (the provided `claude-code` and `codex` templates do); agentd's version check makes that safe.

### Agents

Each agent is a **spec**, the only place agents differ (legacy `internal/agentd/agents.go`). In the rebuild each spec comes from its **agent provider plugin** ([plugins/agent-providers](../plugins/agent-providers.md)): the declarative part (adapter package and version, command, binary env, modes, fast mode, hidden slash commands, stop signal) is the plugin's manifest, and the parts that need code (instructions, output schema, background tasks) are an optional **driver** the plugin compiles into the sandbox build (`plugins/agents/<name>/agentd`, build tag `sandbox`). An agent without a driver runs on agentd's generic ACP driver. Today's two specs:

| | `claude-code` | `codex` |
|---|---|---|
| Adapter | `claude-agent-acp`, from `@agentclientprotocol/claude-agent-acp@0.94.0` | `codex-acp`, from `@agentclientprotocol/codex-acp@2.2.2` |
| Agent binary | `CLAUDE_CODE_EXECUTABLE=$(command -v claude)` | `CODEX_PATH` = agentd itself as the [stand-in](#the-codex-stand-in), with `LEWBOX_AGENTD_CODEX=$(command -v codex)` |
| Default mode | `default` | `bypass` (`agent-full-access`) |
| Modes | `default`, `accept_edits`, `plan`, `auto`, `bypass` → `default`, `acceptEdits`, `plan`, `auto`, `bypassPermissions` | `bypass`, `plan` → `agent-full-access` with `collaboration_mode` `default` or `plan` ([models-and-modes](models-and-modes.md#modes)) |
| Fast mode | none | config option `fast-mode` |
| Instructions | ACP `systemPrompt` (`append`) on `session/new`, `load`, `resume` | `developer_instructions` in `CODEX_CONFIG` |
| Output schema | `_meta.claudeCode.options.outputFormat`, session-wide (a change reopens the session) | `outputSchema` on `turn/start`, via the stand-in |
| Background tasks | `_meta.claudeCode.emitRawSDKMessages` task messages | not reported ([background-tasks](background-tasks.md)) |
| Required env | `IS_SANDBOX=1` (else `bypassPermissions` is hidden for root) | |
| Removed from env | `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` when `CLAUDE_CODE_OAUTH_TOKEN` is set | |
| Stop | close stdin, then `SIGTERM` to the process group | `SIGTERM` (it doesn't exit when stdin closes) |
| Hidden slash commands | `/login`, `/logout` | `/logout` |

**Client capabilities** offered on `initialize`: `{fs: {readTextFile: false, writeTextFile: false}, terminal: false, elicitation: {form: {}}}`. Both adapters work without `fs` and `terminal`, as agents use their own tools in the sandbox; without `elicitation.form`, Claude can't ask questions and Codex's are answered with nothing (legacy `docs/acp.md` § Spike findings).

**Never** call `authenticate` or `logout`: Codex's `authenticate` forces a token refresh or rewrites `auth.json`, and `logout` deletes the login. Never pass `--hide-claude-auth` or `--bare`. ACP v1 only; the SDK ships a v2 schema to watch.

Adding an agent (OpenCode, Gemini, pi) is a new **agent provider plugin**: a manifest (the table's rows), credential methods and sign-in flows if it has them, what it adds to the provided templates, and a driver only if plain ACP isn't enough. Core, agentd and the control plane's API don't change; the agent appears in `GET …/agents` once its plugin is enabled ([plugins/agent-providers](../plugins/agent-providers.md)).

### Installing adapters

- The first time an agent is used in a sandbox whose image lacks its adapter, agentd installs the pinned package with `npm install --omit=optional --prefix /usr/local/lib/lewbox/acp` (57 MB rather than 756 MB, ~26 s; the optional dependencies are each adapter's bundled agent binary, which `CLAUDE_CODE_EXECUTABLE` and `CODEX_PATH` make unused).
- The pinned versions come from the control plane with each `prompt` (`agents.adapters.*`), so upgrading an adapter is a Lewbox setting, not an agentd build. agentd installs a version beside the last and switches when no conversation runs on the old one.
- Needs Node 22+ and `npm` in the image. Without them the turn fails `agent_unavailable` with "Node.js 22 or later is needed in the sandbox to run Claude Code's adapter", and `options` reports the agent `failed`.
- An install in progress blocks only conversations of that agent; it holds a lock so two first prompts install once.

### Starting and upgrading

```
control plane                                    sandbox
─────────────                                    ───────
prompt arrives; no agentd connected for sbx
pg_advisory_xact_lock(hash('agentd', sbx))
toolbox: stat /usr/local/lib/lewbox/lewbox-<v> ─▶ missing?
toolbox: upload part file, chmod, verify mode, move
toolbox: write /run/lewbox/agentd/channel.token (0600)
toolbox: session "lewbox-agentd" ───────────────▶ bash -lc 'exec /usr/local/lib/lewbox/lewbox agentd serve'
                                                  ├─ takes the socket lock (a second daemon exits 0)
                                                  ├─ replays each conversation's events.jsonl
                                                  ├─ ends turns left open: error + finished(interrupted)
wait ≤ agents.start_wait (10s) for hello ◀──────── dials GET /agentd/v0/channel, sends hello
```

- **Login shell.** agentd starts in a login shell, and starts each adapter with a login shell's environment read afresh, so the profile's logins from setup (`/run/lewbox/env`, loaded by `/etc/profile.d/lewbox.sh`) are current. Without it, Claude reports "Authentication required" (`37a5808`).
- **One daemon per sandbox.** It holds a lock on `/root/.lewbox/agents/agentd.lock`; a second exits at once and leaves the first alone. The control plane also starts it under an advisory lock, and checks before starting that no session named `lewbox-agentd` is running, because toolboxd makes a new session on every create, whatever its name (lesson from private ports, `5f3d19e`).
- **Upgrade.** `hello` names agentd's build. When it isn't the control plane's, and agentd is idle (no turn running or waiting, nothing queued, no background tasks), the control plane sends `upgrade`, agentd exits, and the next command starts the new build. A busy daemon serves on and is upgraded when next idle. This is legacy's handshake (`upgrade` error code), moved onto the channel.
- **After a sandbox start**, processes are gone. agentd isn't started with the sandbox; the first command that needs it starts it, and it ends any turn the stop interrupted (`interrupted`, legacy `docs/acp.md` § lewbox-agentd).

### Talking to agentd

#### The agent channel

agentd dials home over a WebSocket, `GET /agentd/v0/channel`, and keeps it open. Commands go down; events, uploads and command results come up.

- **Auth:** `Authorization: Bearer <agent token>`, a sandbox-scoped token (Ed25519, audience `agentd`, claims `{sbx, exp}`, signed with the agent-token key in the keyring, [storage-and-uploads § Tokens](storage-and-uploads.md#tokens)). It's written to `/run/lewbox/agentd/channel.token` (0600) when agentd starts, and replaced over the channel before it expires. Anything in the sandbox can read it, which is fine: it reaches only this sandbox's conversations, which anything in the sandbox can already reach.
- **URL:** the control plane's public URL ([foundation/configuration](../foundation/configuration.md)), or a cluster's override where sandboxes reach the control plane by another address.
- **Frames:** JSON text messages, one object each:

| Type | Direction | Fields |
|---|---|---|
| `hello` | up | `build`, `sandbox`, `conversations: [{id, events_through, uploaded_through, state}]` |
| `welcome` | down | `acked: {cnv: event_id}` (what's stored), `settings` (timeouts, chunk cap, adapter versions) |
| `command` | down | `id`, `name`, `args` |
| `result` | up | `id`, `ok`, `error: {code, message}`, `response` |
| `events` | up | `conversation`, `events: [...]` (numbered; text deltas coalesced over ~50 ms) |
| `upload` | up | `conversation`, `through`, `updates`, `turns`, `chunks` ([storage-and-uploads § Uploads](storage-and-uploads.md#uploads)) |
| `ack` | down | `conversation`, `through` |
| `token` | down | a fresh token |
| `upgrade` | down | |

  Ping and pong are WebSocket frames, every 30 s.
- **Reconnecting:** agentd retries with backoff (1 s to 30 s, jittered). On `welcome`, it resends events and uploads after each conversation's `acked`. Its JSONL log is the source of truth, so nothing is lost while disconnected, and the control plane's stores are idempotent on (conversation, event id) and (message, sequence).
- **Routing across replicas:** agentd's channel lands on one replica. That replica records `(sandbox, replica, connected_at)` in `agentd_connections` and `LISTEN`s on its own channel. A command from another replica is inserted into `agentd_commands` and `NOTIFY`d to the owning replica, which forwards it and writes the result back with a `NOTIFY` to the caller. A replica that dies loses its connections; agentd reconnects elsewhere within seconds, and callers waiting on it time out with `agent_unavailable` (`502`), which clients retry.
- **Commands are idempotent** by their arguments (a `prompt` for a message already queued or running answers as it is), so a retried command after a dropped channel can't run twice.

**Why:** a command becomes a frame (milliseconds, no process, no exit-status trap), events reach the control plane once however many follow them, stored events outlive the sandbox, and the app learns a turn ended the moment it does. agentd already needs to reach the control plane for uploads, so nothing new is asked of the network.

#### No second path

Legacy's toolbox path (commands as `lewbox agentd call --file` through Execute, events from `lewbox agentd events --follow` over exec-stream, uploads by HTTP) is **not** kept as a fallback. The toolbox is used to install agentd, write its token and start it ([starting and upgrading](#starting-and-upgrading)), and nothing more.

Why not keep it:

- **It doesn't help when it would be needed.** A sandbox whose agentd can't reach the control plane can't upload what its agent did either, so the conversation couldn't be stored or read back. A fallback would run turns nobody can see after the fact.
- **It's the path with the problems:** a process per command, exit codes lost under gVisor, the 128 KiB argument limit, a process per follower. Keeping it means keeping all of that, tested.
- **Two paths double the work** for every command and event type, and the rarely used one rots.

Instead:

- **A clear failure.** No `hello` within `agents.start_wait` (10 s) fails the prompt with `503 agent_unavailable`, and the message says the sandbox couldn't reach the control plane at the channel URL, with agentd's last connection error read from its log through the toolbox. The sandbox's diagnostics in the dashboard show the same.
- **Documented egress.** Sandboxes must reach the control plane's public URL over HTTPS, as they must today for uploads (the public docs already say so).
- **For networks where sandboxes can't reach it** (a cluster behind NAT on the [tunnel](../clusters/connectivity.md), or egress locked down): a cluster setting `agent_channel_url` points agentd at another address, and Later the node agent can relay the channel over its own tunnel to the control plane (`agent_channel: via_node`), so sandboxes only need to reach their node. That's the answer for restricted networks, not a second protocol.

The local socket stays, for people: `lewbox agentd status` and `lewbox agentd call` in a sandbox's terminal talk to the daemon over `/root/.lewbox/agents/agentd.sock`, for debugging.

### Commands

Sent as `command` frames on the channel. Each answers `ok`, `error` (`code`, `message`) and the conversation's status. From legacy `docs/acp.md` § lewbox-agentd, with additions marked **new**:

| Command | Args | Does |
|---|---|---|
| `prompt` | `conversation`, `message`, `prompt`, `output_schema`, settings (`model`, `mode`, `effort`, `fast`, `on_unanswered`, `reply_timeout` **new**), `queue`, `steer`; on the first prompt `agent`, `directory`, `instructions`, `agent_session_id`, `worktree`; on every prompt `env`, `skills`, `mcp_servers`, `allowed_tools` (**new**), `upload_token`, `chunk_max_bytes` | Starts the adapter if needed (`session/new` or `session/resume`), applies settings, starts the turn, or queues or steers it. Answers once the turn has started (`turn`), been queued (`queued`) or steered (`steered`, with the new turn's number). What fails later fails the turn |
| `steer` | `conversation`, `message` | Steers a queued prompt into the running turn |
| `cancel` | `conversation`, `message`? | `session/cancel`, or takes a queued prompt out of the queue |
| `reply` | `conversation`, `request`, `option` \| `answers` \| `decline` | Answers an approval or question |
| `configure` | `conversation`, settings | Settings for the next turn |
| `status` | `conversation`? | `state` (`idle`, `running`, `waiting`, `background`), `turn`, `message`, `pending`, `queued`, `tasks`, `last_event`, `agent_session_id`, `adapter_running` |
| `stop_task` | `conversation`, `task` | [background-tasks](background-tasks.md#stopping-one) |
| `add_worktree` **new** | `id`, `directory`, `branch`, `from`, `copy`, `name` | [worktrees](worktrees.md#in-agentd) |
| `remove_worktree` | `id`, `force` | [worktrees](worktrees.md#in-agentd) |
| `options` | `agent`? | [models-and-modes](models-and-modes.md#probing) |
| `close` | `conversation` | Stops the adapter |
| `delete` | `conversation` | Stops the adapter and removes the conversation's directory |
| `unload` **new** | `conversation` | Stops an idle adapter now (the control plane's memory policy can ask) |

### Files on disk

```
/usr/local/lib/lewbox/lewbox-<version>          the sandbox binary (lewbox → current)
/usr/local/lib/lewbox/acp/                      adapters installed by agentd
/root/.lewbox/agents/agentd.lock                one daemon per sandbox
/root/.lewbox/agents/agentd.sock                local socket, for `lewbox agentd status` and `call` in a terminal
/root/.lewbox/agents/agentd.log                 agentd's own log, rotated at 10 MB
/run/lewbox/agentd/channel.token               the agent token (0600; memory-only, never in a snapshot)
/root/.lewbox/agents/options.json               probed models and modes, keyed by CLI and adapter file
/root/.lewbox/agents/<cnv>/conversation.json    agent, session id, directory, settings, queue, open turn,
                                                uploaded_through, worktree id; never secrets
/root/.lewbox/agents/<cnv>/events.jsonl         numbered events; a line cut short by a crash is dropped
/root/.lewbox/agents/<cnv>/adapter.log          the adapter's stderr; its end goes into error events
/root/.lewbox/agents/<cnv>/plugin/              skills for this conversation (new)
/root/.lewbox/worktrees/<wtr>.json              worktree meta (new)
```

Agents keep their own transcripts too (Claude's `~/.claude/projects/<dir>/<id>.jsonl`, 150–240 KB for a short session; Codex's `~/.codex/sessions/…/rollout-…-<id>.jsonl`, ~60 KB). They're how a conversation resumes, so they persist with the sandbox's files.

### The adapter's environment

Built fresh for each adapter start, in this order (later wins):

1. A login shell's environment (`bash -lc env`), which brings the profile's setup ([sandboxes/profile-setup](../sandboxes/profile-setup.md)).
2. The agent spec's variables (`IS_SANDBOX=1`, `CLAUDE_CODE_EXECUTABLE`, `CODEX_PATH`, `CODEX_CONFIG` merged with instructions).
3. Telemetry: the profile's OpenTelemetry settings from `/run/lewbox/telemetry.env` (`LEWBOX_AGENTD_TELEMETRY_FILE`, or `none`), with `lewbox.conversation` and `lewbox.agent` added to `OTEL_RESOURCE_ATTRIBUTES` (`5b56a01`; [profiles/telemetry](../profiles/telemetry.md)). For Codex, the exporter settings go into `CODEX_CONFIG`'s `otel` table too, so nothing secret is written to `config.toml` (**new**).
4. The conversation's own `env` (**new**), held in memory from the last prompt. Names Lewbox sets itself (`IS_SANDBOX`, `CLAUDE_CODE_EXECUTABLE`, `CODEX_PATH`, `CODEX_HOME`, `OTEL_*`, `LEWBOX_*`) are refused at the API ([conversations](conversations.md#environment-skills-mcp-servers-and-tools)).
5. Removals: `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` when `CLAUDE_CODE_OAUTH_TOKEN` is set.

Each adapter runs in its own process group, so stopping it stops what it started, MCP servers included.

### Memory and unloading

Each conversation costs about 1 GB at its peak (Claude: adapter ~210 MB plus `claude` 450–490 MB; Codex: ~150 + 130 MB of Node, `codex` ~410 MB, `codex-code-mode` ~80 MB), plus its MCP servers (Playwright's ~70 MB). An idle one keeps most of that.

Proposed policy, in agentd:

- **Idle:** unload an adapter after `LEWBOX_AGENTD_ADAPTER_IDLE` (default 10 min) with nothing running, waiting, queued or in the background; `LEWBOX_AGENTD_BACKGROUND_IDLE` (2 h) while it has background tasks that aren't `ambient`.
- **Pressure (new):** every 15 s, read the sandbox's memory (cgroup v2 `memory.current` and `memory.max` where they exist, else `/proc/meminfo`). Below 15% available, or with more than `LEWBOX_AGENTD_MAX_LOADED` adapters loaded (default: sandbox memory in GB minus one, at least 2), unload idle adapters, least recently used first, never one with a turn, a request, a queue or background work.
- **Reload** is transparent: the next prompt resumes the session (`session/resume`, with instructions, schema and MCP servers again). It costs a second or two, and on Claude a cold prompt cache.

### First starts

A per-agent lock is held from spawning an adapter until its first `session/new` or `session/resume` answers, so two conversations never initialize one agent's home (`~/.codex`, `~/.claude.json`) together. Starts of the same agent queue behind it; others don't. As a second guard, a Codex start that fails with "failed to initialize sqlite state runtime" is retried once after 2 s.

### The Codex stand-in

Part of the `agents/codex` plugin's driver (`plugins/agents/codex/agentd/codexproxy.go`, legacy `internal/agentd/codexproxy.go`). `codex-acp` 2.2.2 can't pass an output schema to Codex's `turn/start`, so agentd stands in for `codex`: with `LEWBOX_AGENTD_CODEX` naming the real binary, `lewbox agentd` run as `codex` runs the real one with the same arguments, and for `app-server` copies the adapter's JSON-RPC to it line by line, adding the schema from `LEWBOX_AGENTD_CODEX_OUTPUT_SCHEMA`'s file as `outputSchema` to each `turn/start` of the conversation's thread that has none. Kept as it is; drop it once `codex-acp` takes a schema on `session/prompt`, which we propose upstream ([conversations § Structured output](conversations.md#structured-output)).

## Security

- agentd runs as root, as everything in a sandbox does. It holds an agent token for its sandbox, the upload tokens of its conversations, and the conversations' `env` and MCP secrets in memory.
- It writes no secret to disk. `conversation.json` holds no `env` values or MCP headers. The channel token is on `/run/lewbox`, the sandbox's memory-only mount, so it's never in a snapshot, and its path is in every snapshot's scrub list anyway ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)). A stop empties it; a start writes a new one.
- The control plane trusts nothing agentd says about other sandboxes or conversations: every frame is checked against the token's sandbox and the conversation's row.
- Adapter output is untrusted data, stored and streamed as data, never interpreted by the control plane.

## Configuration

agentd reads these from its environment (legacy `internal/agentd/config.go`); the control plane sets the ones it needs on the session, and sends timeouts in `welcome`:

| Variable | Default | |
|---|---|---|
| `LEWBOX_AGENTD_DIR` | `/root/.lewbox/agents` | |
| `LEWBOX_AGENTD_SOCKET` | `<dir>/agentd.sock` | |
| `LEWBOX_AGENTD_CHANNEL_URL` **new** | | `wss://…/agentd/v0/channel` |
| `LEWBOX_AGENTD_WORKTREES_DIR` | `/root/workspace/.worktrees` | |
| `LEWBOX_AGENTD_REPLY_TIMEOUT` | `10m` | Overridden per prompt |
| `LEWBOX_AGENTD_ADAPTER_IDLE` | `10m` | |
| `LEWBOX_AGENTD_BACKGROUND_IDLE` | `2h` | |
| `LEWBOX_AGENTD_AGENT_TURN_QUIET` | `2m` | Ends a turn the agent started itself |
| `LEWBOX_AGENTD_MAX_LOADED` **new** | memory GB − 1 | |
| `LEWBOX_AGENTD_CLAUDE_CODE_ADAPTER`, `LEWBOX_AGENTD_CODEX_ADAPTER` | on the `PATH` | Adapter commands |
| `LEWBOX_AGENTD_CLAUDE_CODE_PACKAGE`, `LEWBOX_AGENTD_CODEX_PACKAGE` | pinned | Overridden by the control plane |
| `LEWBOX_AGENTD_INSTALL_DIR` | `/usr/local/lib/lewbox/acp` | |
| `LEWBOX_AGENTD_UPLOAD_INTERVAL` | `1s` | How often `upload` frames are batched |
| `LEWBOX_AGENTD_HEARTBEAT` | `1m` | While not idle |
| `LEWBOX_AGENTD_TELEMETRY_FILE` | `/run/lewbox/telemetry.env` | `none` turns it off |

## Testing

- **Unit:** the ported tests (`agentd_test.go`, `upload_test.go`, `worktree_test.go`, `output_test.go`, `codexproxy_test.go`) against `internal/testing/fakeacp`, a scripted adapter (the test binary re-exec'd), extended with: steered turns across a reconnect; two first prompts of one agent; memory-pressure unloads (fake meminfo); env precedence; the channel's resend after `welcome`.
- **Channel:** an in-process control plane (httptest) with a fake toolbox ([testing](../foundation/testing.md)): agentd started, `hello`, commands, a dropped connection mid-turn, a replica handover, a token refresh, an upgrade while busy then idle.
- **Live, before the alpha,** in a gVisor sandbox from each provided template, with subscription and API-key logins: a turn per agent, an approval, a question, plan mode, a cancel, a steer, a background `sleep` stopped, a stop and start mid-turn (`interrupted`, then resumed), 10 conversations at once with unloading, and the first-start race (two Codex chats at once on a fresh sandbox).

## Build plan

1. Move `internal/agentd` and `cmd/lewbox-agentd` into the module under the `sandbox` build tag; `internal/acp` split out of `rpc.go`; tests green (2 days).
2. Embed both architectures' sandbox builds; install and start through the toolbox; version handshake (1 day).
3. The channel client in agentd, `/agentd/v0/channel` and the hub in the control plane, cross-replica routing in Postgres (4 days).
4. Per-conversation env, skills, MCP servers, allowed tools in `prompt` (2 days, with [conversations](conversations.md)).
5. Memory policy and first-start lock (1 day).
6. Adapter versions from the control plane, side-by-side installs (1 day).

## Open questions

1. ~~**Channel or toolbox for the alpha?**~~ Decided (2026-10-29): the channel, in the alpha, with [no second path](#no-second-path).
2. **Cross-replica routing:** Postgres `NOTIFY` with a commands table (proposed), or replica-to-replica HTTP? The table is simplest with one dependency; latency is a few ms.
3. **Memory thresholds** (15%, memory GB − 1): measure on a 12 GB Lewagent computer with Playwright MCP per chat.
4. **Preinstalled adapters vs installs:** do we host the adapter tarballs (in the cluster's registry or storage) so sandboxes without npm access can install them?
5. **ACP v2:** the SDK ships it (`replayFrom`, `requires_action` session states). When both adapters move, does agentd follow at once?
