# Models and modes

> Status: Draft · Milestone: Alpha (M3) · Core probing and caching; mode mappings from the agent plugins · Updated: 2026-10-29

Each prompt can choose the agent's **model**, how hard it thinks (**effort**), its **mode** (how much it may do without asking), and for Codex **fast** mode. Lewbox reads models and efforts from the agent's own CLI in the sandbox, so what's offered is exactly what's installed, and maps five plain-word modes onto each agent's own. This plan also covers the agents' **slash commands**, which Lewagent wants kept so its prompt box can suggest them before a turn has run.

## Today

- **Contract:** [lewbox.com/docs/agents/models-and-modes](https://lewbox.com/docs/agents/models-and-modes).
- **Probing** (`internal/agentd/options.go`, 403 lines, `a7bd7bd`): `lewbox-agentd options` starts each adapter in a scratch directory, opens a session (nothing prompted, so free), reads its `model`, effort (`effort`, Codex's `reasoning_effort`) and `fast-mode` config options, switches to each model to read that model's own efforts, caches the result on disk keyed by the adapter's and CLI's files, and removes what Claude keeps for the scratch directory. The gateway's `GET /v1/sandboxes/{sbx}/agents` (`listAgents`, `conversations.go:1458`) answers `loading` if an agent isn't read within 10 seconds.
- **Read live on 2026-10-22:** Claude Code had Sonnet 5.5, Fable 5.1, Opus 5.5 and Haiku 5.5, each Low to Max; Codex had 6.1 Sol, 6 Astra, 6 Sol, 6 Luna, 5.6 Sol, 5.6 Terra, 5.6 Luna, Low to Ultra (the Lunas to Max), and a fast mode.
- **Modes** (`internal/agentd/agents.go`): Lewbox's `default`, `accept_edits`, `plan`, `auto`, `bypass`, mapped per agent; the app checks a mode against `App\Enums\Profile\Agent` before anything reaches the sandbox.
- **Slash commands** arrive as a `commands` event when they change (Claude's `available_commands_update` is ~12 KB, twice per session start), without `/login` and `/logout`; nothing keeps them.

**Lewagent's ask:** "an agent's slash commands kept by Lewbox, on the conversation (as `context` is) and in each agent's options from `GET /sandboxes/{sbx}/agents`, so the prompt box can suggest them before a turn has streamed its `commands` event" (`lewagent-legacy/docs/todo.md`).

## Goals

- Keep reading options from the agents themselves; no list to maintain.
- Validate a mode, `fast`, and (when known) a model before anything reaches the sandbox.
- Keep slash commands per agent and per conversation.
- Serve the options endpoint from the control plane's cache when it can, so it's instant.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Probing with a throwaway session; per-model efforts; `loading` after 10 s; `failed` with `error`; the five modes and their mapping; Codex in `bypass` and `plan` only; `fast` refused for Claude |
| **Change** | The control plane caches each sandbox's options in Postgres, keyed by the CLI and adapter versions agentd reports, so the endpoint answers from cache and a prompt's model can be checked |
| **Add** | `commands` in options and on the conversation; `?refresh=true`; a project's allowed modes per profile (proposed) |

## Design

### Probing

```
GET /v0/sandboxes/{sbx}/agents
  └─ cache in agent_options for (sandbox, agent, cli_version, adapter_version)?
       hit  → answer
       miss → command agentd: options {agent?}  (≤ 10 s)
                agentd: per agent, cached on disk (options.json, keyed by the files' size and mtime)?
                  no → start the adapter in /root/.lewbox/agents/.probe/<agent>/
                       initialize, session/new, read configOptions (model, effort|reasoning_effort, fast-mode)
                       for each model: session/set_config_option model → read its efforts
                       read available_commands_update (new)
                       close; remove Claude's ~/.claude/projects entry for the scratch dir
                answered in time → store in agent_options, answer
                not yet          → answer status loading; store when agentd finishes
```

- Nothing is prompted, so it costs nothing on any login.
- An agent whose CLI or adapter isn't installed and can't be is `failed` with why; it's tried again on a later request.
- When a template installs other versions, the keys change and it's read again. agentd reports its agents' CLI and adapter versions in `hello` ([agentd § Talking to agentd](agentd.md#talking-to-agentd)), so the control plane knows when its cache is stale without asking.
- `?refresh=true` forces a probe (e.g. after installing a CLI by hand).
- SDKs ask again every 2 s while an agent is `loading`, for up to 60 s.

### The options endpoint

`GET /v0/sandboxes/{sbx}/agents[?agent=claude-code]`, `sandboxes.agents`. The sandbox must be running unless the cache can answer (new: a stopped sandbox's last-known options are returned with `stale: true`).

| Field | | |
|---|---|---|
| `agent` | string | `claude-code`, `codex` |
| `status` | string | `ready`, `loading`, `failed` |
| `models` | array | `{id, label, efforts: [{id, label}]}`, efforts lowest first |
| `efforts` | array | The default model's efforts |
| `default_model`, `default_effort` | string or null | `null`: the agent picks |
| `fast_mode` | boolean | |
| `modes` | array | Lewbox modes it takes |
| `commands` | array | **New.** `{name, description}`, as the agent offers them, without hidden ones |
| `versions` | object | **New.** `{cli, adapter}` |
| `stale` | boolean | **New.** From cache while the sandbox isn't running |
| `error` | string or null | When `failed` |

While `loading` or `failed`, lists are empty and defaults `null`.

### Models and efforts

- Given by `id` on a prompt (`model`, `effort`); they apply from that turn on and stay for later turns. The conversation shows what was last set; each turn records the `model` it ran on.
- **Checked when known (new):** if the cache for the sandbox has the agent `ready`, a `model` not among its `models`, or an `effort` not among that model's, is `422 validation_failed` before reaching the sandbox, naming the valid ones. Without a cache, they're passed as given, and one the agent refuses ends the turn `failed` with an `error` event (legacy behaviour).
- Passed to the adapter as config options: `session/set_config_option` with `model`, and `effort` (Claude) or `reasoning_effort` (Codex). `session/set_mode` is deprecated and not used.

### Modes

| Mode | Claude Code (`mode`) | Codex | What the agent does |
|---|---|---|---|
| `default` | `default` (its default) | | Asks before editing files or running commands; reads and read-only commands don't ask |
| `accept_edits` | `acceptEdits` | | Edits without asking; asks before commands |
| `plan` | `plan` | `agent-full-access` + `collaboration_mode: plan` | Explores and proposes a plan, then asks to carry it out ([approvals § Plan mode](approvals-and-questions.md#plan-mode)) |
| `auto` | `auto` | | Acts on its own, pausing for anything risky |
| `bypass` | `bypassPermissions` | `agent-full-access` + `collaboration_mode: default` (its default) | Never asks |

- A mode the agent lacks is `422 validation_failed` before anything reaches the sandbox, naming its modes. Leaving `mode` out starts in the agent's default.
- **`bypassPermissions` as root needs `IS_SANDBOX=1`;** without it Claude hides the mode and setting it fails `-32603 Invalid value for config option mode`. agentd always sets it ([agentd § Agents](agentd.md#agents)).
- **The agent can change its mode itself**, as when an approved plan picks the next one. A `mode` event says so and the conversation follows. agentd maps the adapter's config back to a Lewbox mode, or passes the adapter's own name when it's none of them (`modeFrom`).
- In `bypass` the agent still asks questions.

### Codex runs with full access

Codex's ask-first presets (`read-only`, `workspace-write`, `agent` with auto review) rely on its own Linux sandbox, bubblewrap. Legacy found it can't start under gVisor (`bwrap: loopback: Failed RTM_NEWADDR: No child process`). The alpha runs Docker (`container`) only, where bubblewrap needs user namespaces that Docker's default seccomp profile normally refuses to unprivileged processes; whether it starts in our sandboxes is an M3 spike, and we won't loosen the sandbox's own isolation (seccomp, capabilities) to make it. In those modes each command fails and Codex sometimes asks to run it outside the sandbox, sometimes just reports the failure. `codex-acp`'s modes are fixed presets (`_AgentMode`), and none is "no sandbox, but ask first" (`dangerFullAccess` with `on-request`). So Codex runs in `bypass` (and `plan`), with full access to its sandbox; the sandbox itself is still isolated from everything else.

**Upstream:** propose that preset to `codex-acp`, as we contribute AerolVM fixes upstream ([D3](../README.md#decisions)). Once it lands, Codex gets `default` (ask first) with its approvals, which were checked to round-trip in legacy's spike. If the M3 spike finds bubblewrap works at `container`, or later at `microvm` ([clusters/runtimes](../clusters/runtimes.md#isolation-levels)), Codex's own presets could be offered there; until then, and in the alpha unless the spike says otherwise, Codex runs as below. The sandbox's level, not its cluster, decides, since one cluster can offer several.

### Fast mode

Codex's `fast-mode` config option answers faster on the same model. `fast` on a prompt turns it on or off from that turn on; the conversation's `fast` is `null` until set (Codex's default). Claude has none: `fast` with Claude is `422`, whether `true` or `false`.

### Slash commands

- agentd already sends a `commands` event when an agent's commands change. The control plane now keeps the latest on the conversation (`commands`, as `context` is kept) and in the options cache per sandbox and agent, from the probe's session.
- Hidden: `/login`, `/logout` (both agents). Lewbox owns logins.
- A slash command is sent as a prompt (`/usage`, `/compact`). Claude's `/usage` answers with the session's cost, not the plan's limits.

### Policy (proposed)

A project may limit, per profile, which modes conversations may use (`agents.modes` on the profile, e.g. no `bypass` for a profile whose sandboxes hold production credentials). A refused mode is `422 validation_failed` with "This profile doesn't allow bypass." Stored with the profile ([profiles](../profiles.md)); checked on every prompt and when an approved plan would switch to a refused mode (agentd is told the allowed modes and answers such an approval's options other than the allowed ones with a reject, which the docs explain).

## Data

`agent_options`: `sandbox_id`, `agent`, `cli_version`, `adapter_version`, `status`, `options jsonb`, `error`, `probed_at`; primary key `(sandbox_id, agent)`. Deleted with the sandbox.

## Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.agents` |
| `404` | `not_found` | No such sandbox the key reaches |
| `409` | `sandbox_not_running` | Not running and nothing cached |
| `422` | `validation_failed` | On a prompt: an unknown mode, a mode the profile forbids, `fast` for Claude, a model or effort the cache says the agent lacks |
| `422` | `agent_refused` | An `agent` that isn't one Lewbox runs |
| `502` | `agent_unavailable` | agentd couldn't be started or reached |
| `503` | `agents_unavailable` | Agents are off |

## Testing

- Fake adapter with scripted config options: probe, per-model efforts, `loading` at 10 s then `ready`, `failed` for a missing CLI, cache invalidated by a version change in `hello`, commands captured.
- Prompt validation: unknown mode, forbidden mode, `fast` for Claude, a model not in the cache, no cache (passes through).
- Live: both agents' real lists in a provided template, and a version bump in a template rebuild.

## Build plan

1. Options endpoint with the Postgres cache and agentd's probe (2 days).
2. Slash commands on probe and conversation (½ day).
3. Model and effort validation against the cache (½ day).
4. Per-profile modes policy (1 day, with [profiles](../profiles.md)).

## Open questions

1. **Validate models strictly?** The cache can be stale for a moment after an upgrade; a false `422` is worse than a failed turn. Proposed: validate only when the cache is `ready` and its versions match what agentd last reported.
2. **Per-profile mode policy:** wanted for the alpha, or later?
3. **Codex's own presets on Docker** (the alpha's `container` sandboxes): if the M3 spike finds bubblewrap works there, offer them, or keep one behaviour everywhere until upstream adds the preset?
