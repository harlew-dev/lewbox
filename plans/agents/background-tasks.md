# Background tasks

> Status: Draft · Milestone: Alpha (M3) · Core, reported by agent drivers that support it (`agents/claude-code`) · Updated: 2026-10-29

Claude Code moves long work into the background and carries on: a dev server, a long test run, a subagent reviewing a diff. That work outlives the turn that started it, and when it ends the agent reports on it in a [turn of its own](turns-and-events.md#turns-the-agent-starts-itself). Lewbox lists what's running so a client can show it, stops what the user doesn't want, and keeps the sandbox and the agent alive while it runs.

## Today

- **Built** 2026-10-25 (`f5adfcc`): `internal/agentd/tasks.go` (553 lines); `GET /v1/conversations/{cnv}/tasks` and `PATCH …/tasks/{task}` in `internal/server/tasks.go` (157 lines). Not in the gateway's `openapi.yaml` (a drift this rebuild fixes by generating from one spec, [api/openapi-and-codegen](../api/openapi-and-codegen.md)).
- **Source of truth:** Claude's own task messages, which `claude-agent-acp` passes on as `_claude/sdkMessage` notifications to a session opened with `_meta.claudeCode.emitRawSDKMessages`. agentd asks for `task_started`, `task_updated`, `task_progress`, `task_notification` and `background_tasks_changed` (the whole set of live tasks each time it changes) (legacy `docs/acp.md` § Background tasks).
- **Not used:** the adapters' own `async_task_*` extension, offered only to a client declaring JetBrains AIR (`_meta.jetbrains.air`); declaring it makes both adapters drop tool calls' `rawInput` and `rawOutput` (Codex its `title`, `kind`, `status` and `content` too), which Lewbox's events need. So Codex's background terminals aren't reported.
- **Stopping:** Claude points a background command's output at `<tasks dir>/<id>.output`; agentd finds the adapter's descendants writing to it (and what they started) through `/proc`, signals them and their process groups (never the adapter's), SIGTERM then SIGKILL after 3 s. The task ends `stopped`, whatever Claude says of the exit (`failed`, 143).
- **Lewagent** lists tasks below the prompt box, read as a chat opens, as each turn ends, and every 5 s while any run; a row scrolls to the tool call that started it, opens a command's output on the desktop, and stops it (`lewagent-legacy/docs/sessions.md` § What Lewbox gives).

## Goals

- Keep listing and stopping as built.
- Serve lists from the control plane, without a round trip into the sandbox, and across a sandbox's conversations.
- Keep the agent and sandbox alive exactly as long as real work runs.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The task object, `task` events on the open turn, stopping commands and monitors, `task_not_stoppable` for subagents and workflows, `ambient` tasks not keeping the sandbox awake, the 2-hour adapter hold |
| **Change** | The control plane keeps the current set per conversation, from `task` events, so `GET …/tasks` answers from Postgres; agentd's `status` stays the check after a reconnect |
| **Add** | `GET /v0/sandboxes/{sbx}/tasks` (every conversation's), a recently-ended list (`?status=ended`), `conversation` and `ended_at` on each task |

## Design

### The task object

| Field | Type | |
|---|---|---|
| `id` | string | The agent's id for it |
| `conversation` | string | **New.** `cnv_…` |
| `type` | string | `local_bash` (command), `local_monitor`, `local_agent` (subagent), `local_workflow`; others may appear |
| `description` | string | The command, or what the agent said the work is |
| `call_id` | string or null | The tool call that started it |
| `status` | string | `running`, `paused`; once ended `completed`, `failed` or `stopped` |
| `summary` | string or null | A subagent's latest progress, or how it ended |
| `output_file` | string or null | Where its output is in the sandbox, once known |
| `ambient` | boolean | Housekeeping (a watcher) rather than work the agent waits on; doesn't keep the sandbox awake |
| `stoppable` | boolean | Commands and monitors, until stopped |
| `started_at` | string | When Lewbox first heard of it |
| `ended_at` | string or null | **New** |

A command the agent waits on in the foreground is a task too, once it's moved to the background.

### Events and the current set

- Each change is a `task` event with the task, whole, on the turn that's open (or after the last turn, between turns) ([turns-and-events](turns-and-events.md#event-model)). Only events in an open turn reach that turn's stream; the [conversation stream](turns-and-events.md#following) carries all of them.
- The control plane applies each `task` event to `agent_tasks` (`conversation_id`, `task_id`, `data jsonb`, `status`, `started_at`, `ended_at`), so lists answer from Postgres. Ended tasks are kept 24 hours for `?status=ended`, then pruned.
- When an adapter stops (unloaded, exited, its session reopened for an output-schema change, the sandbox stopped), its running tasks end with it: agentd reports them `stopped` with `summary` "The agent stopped." and the control plane closes any it didn't hear about when the conversation's adapter or the sandbox stops.
- After a channel reconnect, agentd's `hello` carries each conversation's state, and the control plane asks `status` for conversations with tasks to resync.

### Keeping things alive

- **The adapter:** agentd keeps a conversation's adapter loaded `LEWBOX_AGENTD_BACKGROUND_IDLE` (2 h) while it has tasks that aren't `ambient`, since stopping the adapter kills them. For an adapter that reports none (Codex), a tool call started with `run_in_background` that no turn of the agent's own has reported on counts. Memory-pressure unloading never picks a conversation with background work ([agentd § Memory and unloading](agentd.md#memory-and-unloading)).
- **The sandbox:** a conversation with non-ambient tasks is `background` in agentd's state, which counts as activity, so the sandbox's idle timeout doesn't stop it under the agent ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md)). Its lifetime still applies.
- **Output-schema switches** on Claude reopen its process and stop its background tasks ([conversations § Structured output](conversations.md#structured-output)); the docs warn of it.

### Stopping one

`PATCH /v0/conversations/{cnv}/tasks/{task}` with `{"status": "stopped"}`, `sandboxes.agents`:

1. A subagent or workflow: `409 task_not_stoppable` (they run inside the agent, which offers no way to stop them without the AIR extension). Ask the agent to stop it instead.
2. A task that isn't running: `404`.
3. Otherwise agentd's `stop_task` finds the processes writing to the task's output file and what they started, sends SIGTERM to them and their process groups, and SIGKILL after 3 s. `202` with the task.
4. The task ends `stopped`; Claude then reports on it in a turn of its own.

### API

| Route | Permission | |
|---|---|---|
| `GET /v0/conversations/{cnv}/tasks` | `sandboxes.agents` | Running tasks, oldest first; `?status=ended` for the last 24 h |
| `GET /v0/sandboxes/{sbx}/tasks` | `sandboxes.agents` | **New.** Every conversation's in the sandbox, for a computer-wide view |
| `PATCH /v0/conversations/{cnv}/tasks/{task}` | `sandboxes.agents` | `status: stopped` |

A sandbox that isn't running has none running.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | |
| `404` | `not_found` | No such conversation, or the task isn't running |
| `409` | `task_not_stoppable` | A subagent or workflow |
| `409` | `sandbox_not_running` | Stopping one in a stopped sandbox |
| `502` | `agent_unavailable` | agentd couldn't be reached |

## Security

- Stopping signals processes as root inside the sandbox; agentd only ever signals the adapter's descendants that write to the task's output file, and never the adapter's own group.

## Observability

- Metrics: tasks started by type, running per sandbox, stopped by client, time adapters were held for background work.
- Activity: `agent.task_stopped` (actor, conversation, task type, description truncated to 200 characters).

## Testing

- Fake adapter emitting task messages: start, progress, notification, `background_tasks_changed`; list, resync after reconnect, ended pruning.
- Stopping: a real background `sleep` under the fake adapter's process tree, SIGTERM honoured, SIGTERM ignored then SIGKILL, subagent refused.
- The adapter held 2 h with work, unloaded after, never by memory pressure while tasks run.
- Live: Claude moving `sleep 60` to the background, its own turn after, a stop mid-run.

## Build plan

1. `agent_tasks` from events; list endpoints (1 day).
2. Stop through the channel; resync on reconnect (1 day).

## Open questions

1. **Codex's tasks:** ask `codex-acp` (and `claude-agent-acp`) upstream to offer the task extension without AIR's other changes, so Codex's background terminals and Claude's subagents can be listed and stopped the same way.
2. **Stopping subagents:** the same upstream ask would let Lewbox stop them.
3. **Should ended tasks be stored with the conversation** (as chunks) rather than pruned after a day, so a transcript shows what ran in the background?
