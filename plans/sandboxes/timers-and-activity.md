# Timers and activity

> Status: Draft · Milestone: Alpha (M1; notify mode and the backstop M2) · Updated: 2026-10-29

Two timers stop a sandbox on their own, so a forgotten one doesn't keep running: `idle_timeout` stops it after a while without activity, and `lifetime` stops it after it's run for a while, busy or not. A sandbox that doesn't persist is deleted instead. This plan covers the timers' semantics, **who decides to stop** (Lewbox, told by the AerolVM fork's timers, with a backstop on the node), warnings before an idle stop, what counts as activity and how Lewbox reports activity AerolVM can't see, how a stop is explained, and changing timers.

## Today

- **Fields.** `idle_timeout` and `lifetime` are seconds, 60 to 2 592 000 (30 days), off by default. The profile limits `sandbox_idle_seconds` and `sandbox_lifetime_seconds` fill one that's left out and refuse one over the limit (`lewbox-legacy/app/Actions/Sandboxes/ResolveSandboxSize.php`; public docs *Idle timeouts and lifetimes*).
- **Enforced by AerolVM, on the node.** The gateway sets the lifecycle on create and start (`lewbox-gateway-legacy/internal/aerolvm/types.go` `Lifecycle`):
  - a sandbox that persists gets `stop_if_idle_for` and `stop_at_age`;
  - one that doesn't gets `destroy_if_idle_for` and `destroy_at_age`.

  Idle counts from the last toolbox, exec or SSH activity. **Age counts from AerolVM's create and doesn't reset on stop and start**, so an in-place start moves `stop_at_age` forward (`PUT …/lifecycle`).
- **Activity is counted per toolbox request, when it starts,** not while a stream is open (`lewbox-gateway-legacy/docs/aerolvm.md`). So the gateway "touches" the sandbox (`GET …/toolbox/health`) at most once per `GATEWAY_ACTIVITY_INTERVAL` (60 s) while data moves on a stream (`internal/server/inside.go`).

  Agent uploads touch it too, and agentd heartbeats at least once a minute while a conversation isn't idle (`internal/server/conversations.go`, `docs/consumer-api.md`).
- **Watched sessions** are polled every 5 s, and each poll counts as activity, so a watched session keeps its sandbox awake (`docs/operations.md`).
- **Nobody is told when AerolVM stops a sandbox.** The reconciler notices within about a minute and infers the reason from the timers (`stopReason` in `internal/reconcile/reconcile.go`): `lifetime_reached` if it's as old as its age timer, else `idle_timeout` if it's been idle as long as its idle timer.
- **Unknown.** Whether traffic to an *exposed port* counts as activity was never checked (`docs/roadmap.md` item 2). A user watching a desktop through a private port may be stopped as idle. Lewagent lists it as an open item.

## Goals

- Timers behave exactly as the docs say: idle counts from the last *activity*, lifetime from each start or change, and both stop (or delete) the sandbox.
- Activity is anything a person or agent is doing in the sandbox, including a desktop open through a port, an attached terminal with traffic, and an agent turn, but not idle connections or reading the sandbox from outside.
- **Every timer stop is a normal stop operation**, so the sandbox's `on_stop` hooks, the harvest and the in-sandbox cleanup run before it, as for a stop that's asked for ([hooks](hooks.md)).
- A warning before an idle stop, so an app can tell its user or keep the sandbox.
- Stops still happen when the control plane is down: a node never keeps a sandbox running forever because it can't reach Lewbox.
- Stops are noticed in seconds, not a minute, and always carry their reason.
- Lewbox shows `last_active_at` on the sandbox.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Fields, ranges, limits and defaults. AerolVM keeps time on the node (it sees every toolbox request), and still stops a sandbox itself as a backstop if Lewbox doesn't. `stop_*` vs `destroy_*` by `persist`. Lifetime counts from each start and from a change. Touches during streams. Agent heartbeats |
| **Change** | **Lewbox decides when to stop.** The fork's timers run in *notify* mode: they emit `idle_warning`, `idle` and `lifetime_reached`, the node agent forwards them, and Lewbox re-checks and runs a normal `stop` operation, hooks first ([Lewbox decides when to stop](#lewbox-decides-when-to-stop)). The fork stops the sandbox itself only after a grace period, running a script Lewbox registered. Port traffic counts as activity (in the fork's proxy). The fork exposes `last_active_at`, shown on the sandbox. Touches go through a dedicated `POST …/activity` on the fork rather than `toolbox/health`, and the API gets `POST /v0/sandboxes/{sbx}/touch` |
| **Drop** | Inferring the reason from timers: the fork's events carry it (F35, F50). Touching the toolbox while streams move: the fork counts stream bytes (F5). No fallback for fork releases without notify mode ([D15](../README.md#decisions)). Watched-session polling as a side-effect keep-awake (sessions get an explicit `keep_awake`; [inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md)) |

## Design

### Semantics

| | `idle_timeout` | `lifetime` |
|---|---|---|
| Counts from | The last activity | The last start (create, start, restart), or the last change to it |
| Off when | `null` and no profile limit | Same |
| On a sandbox that persists | Stop (snapshot, offload) | Stop |
| On one that doesn't | Delete | Delete |
| Range | 60 s to 30 days | 60 s to 30 days |
| Changing it on a running sandbox | Takes effect at once; doesn't reset the lifetime | Counts again from now |

### Mapping to AerolVM

`timers.Lifecycle(sandbox) aerolvm.Lifecycle`:

| Sandbox | AerolVM lifecycle |
|---|---|
| Persists | `stop_if_idle_for = idle_timeout`, `stop_at_age = (age now) + lifetime` |
| Doesn't persist | `destroy_if_idle_for = idle_timeout`, `destroy_at_age = (age now) + lifetime` |
| Either, on a fork release with notify mode (F50) | The same timers, plus `on_expiry: "notify"`, `warn_before` (the sandbox's), `backstop_after` (the [grace](#the-backstop)) and `pre_stop` (the registered script, F49): the timer emits events, and only the backstop stops |

AerolVM's age counts from *its* create of the container. So for an in-place start, restart or lifetime change, Lewbox sets the age timer to the container's current age plus the lifetime: `PUT /v1/sandboxes/{id}/lifecycle`. After a resume (a new AerolVM sandbox), the age is ~0, so it's just `lifetime`.

**Fork change T1** adds `stop_after` and `destroy_after`: durations from *now*, set on each call. This removes the age arithmetic, and the risk of a clock or age mismatch.

The lifecycle is set:
- on create and resume (in the create request);
- on an in-place start and restart (`PUT …/lifecycle`);
- on a settings change to a running sandbox ([changing timers](#changing-timers)).

### What counts as activity

| Counts | How AerolVM learns of it |
|---|---|
| Any request inside the sandbox: a command, a session start or read, a file read or write, a directory listing | It's a toolbox request; AerolVM counts it when it starts |
| Data moving on a stream: streamed command output, an interactive command, an attached session, a file transfer | The fork counts bytes moving on its streams, at most once a minute (F5) |
| An agent turn running, waiting on an approval or question, or with background work | agentd's heartbeat upload, at least once a minute while not idle; each upload touches the sandbox |
| A session started with `keep_awake: true` (new; was a side-effect of `watch`) | The fork counts it as activity while it runs (part of F36) |
| **Traffic through an exposed port** (new) | Fork change T2: AerolVM's proxy (Caddy route or TCP proxy) counts a request or a connection with bytes moving, at most once per interval |
| SSH sessions | AerolVM already counts SSH activity |

| Doesn't count | Why |
|---|---|
| A stream or connection with nothing moving | An abandoned terminal shouldn't keep a sandbox alive |
| Reading the sandbox from outside: get, list, operations, activity log | It's not work in the sandbox |
| Lewbox's own maintenance: the reconciler, setup refreshes, token refreshes | Fork change T3: Lewbox's maintenance calls carry `X-Lewbox-Maintenance: 1`, which AerolVM doesn't count. Today they count, so a token refresh every 5 minutes could keep a sandbox awake for good; legacy avoided it only because refreshes were rare |
| A command running silently for longer than the idle timeout | Nothing moves. The docs advise a longer timeout, or streaming output |

`Touch` is `POST /v1/sandboxes/{id}/activity` on the fork (T3, in the minimum release), for activity only Lewbox can see: agent turns, and the consumer's own `POST /v0/sandboxes/{sbx}/touch`. It's rate-limited per sandbox per replica in memory; with several replicas, at most one touch per replica per interval, which is fine.

### Lewbox decides when to stop

On a fork release with notify-mode timers (F50, [clusters/aerolvm-fork](../clusters/aerolvm-fork.md#api-clustering-and-lifecycle)), AerolVM keeps counting idle time and age on the node, as it sees every toolbox request, but when a timer runs out it **tells** Lewbox instead of stopping:

```
AerolVM (node)                    node agent                 control plane
idle for idle_timeout − warn ──▶ idle_warning ────────────▶ webhook sandbox.idle_warning; dashboard banner
idle for idle_timeout ─────────▶ idle ────────────────────▶ re-check ─┬─ still busy → touch (timer re-armed)
age reaches lifetime ──────────▶ lifetime_reached ────────▶           └─ stop operation, reason idle_timeout
                                                                          or lifetime_reached:
                                                                          disarm timers → on_stop hooks → harvest →
                                                                          cleanup → stop → node scrub → verify → snapshot
idle + backstop_after, no stop ─▶ (fork runs pre_stop script, stops) ─▶ stopped {backstop: true} → offload
```

1. **Warning.** `warn_before` (the sandbox's, default 300 s; `0` turns it off) before an idle stop, AerolVM emits `idle_warning` with `stops_at`. Lewbox records it on the sandbox (`idle_stops_at`), sends the webhook `sandbox.idle_warning` and an activity event, and the dashboard shows a banner with *Keep running* (a touch). Nothing else happens: activity before `stops_at` re-arms the timer as usual. There's no lifetime warning event; `lifetime_ends_at` is on the sandbox from the start, and a project that wants one schedules it.
2. **Expiry.** At `idle_timeout` without activity AerolVM emits `idle`; at the lifetime, `lifetime_reached`. Each carries `at`, `last_active_at` and the AerolVM id; the node agent forwards it at once ([clusters/node-agent](../clusters/node-agent.md#lifecycle-events)).
3. **Re-check** (idle only; a lifetime stops a sandbox busy or not). Lewbox knows of activity AerolVM can't see, so before stopping it checks, in this order, and if any is true it **touches** the sandbox (re-arming the idle timer) and records `idle_deferred` in the activity log with the reason:
   - an agent turn running, waiting on an approval or question, or with background tasks ([agents](../agents.md));
   - a stream through Lewbox with bytes moving in the last `LEWBOX_ACTIVITY_TOUCH_INTERVAL`: an attached terminal, a streamed command, a file transfer;
   - a session with `keep_awake`;
   - a touch received through the API since AerolVM's `last_active_at` (a race between a touch and the event).

   It doesn't stop a sandbox with an operation already in progress (that operation decides), or one that's no longer `running`.
4. **Stop.** Otherwise Lewbox enqueues a `stop` operation (a `delete` for a sandbox that doesn't persist) with reason `idle_timeout` or `lifetime_reached`, in the same transaction that records the event. It's the same operation as any other stop ([operations](operations.md#stop)): its first step **disarms the timers** on the node (so the backstop can't fire mid-stop), then the `on_stop` hooks run with their grace ([hooks](hooks.md#stops-are-bounded)), then harvest, cleanup, stop, scrub (Lewbox's paths and the user's), verify, snapshot.
5. **Metering** ends at the event's `at`, not when the operation finishes, as for any stop that's asked for. The grace for hooks is never metered.

Events are at-least-once and may repeat; the stop is enqueued under the sandbox's row lock with "one operation at a time", so a duplicate `idle` finds the stop already there and does nothing.

#### The backstop

If Lewbox doesn't stop the sandbox, AerolVM does, so a sandbox never runs forever because its node can't reach the control plane (a control plane outage, a tunnel down, a bug in the event path):

- `backstop_after` after the expiry (default `LEWBOX_TIMER_BACKSTOP_GRACE`, 10 minutes, and always at least the sandbox's `stop_grace` + 5 minutes), if the timer hasn't been disarmed or re-armed, AerolVM runs the sandbox's registered `pre_stop` script (F49), which runs the `on_stop` hooks and Lewbox's in-sandbox cleanup ([hooks](hooks.md#when-lewbox-cant-act)), then stops or destroys it as its lifecycle says, and emits `stopped` with `reason`, `at` and `backstop: true`.
- The node agent buffers events while it can't reach the control plane (on disk, up to 10,000 or 7 days) and sends them when it can. Lewbox then records the stop, ends metering at `at`, and offloads the sandbox as for any timer stop ([reconciler](reconciler.md#aerolvms-own-stops)): the node scrub removes credentials and the user's scrub paths, reads the hooks' results and the harvest, and checks before upload.
- `lewbox_timer_backstop_stops_total` counts these; any at all while the control plane is healthy is a bug, and alerts.

#### No fallback without notify mode

Notify-mode timers (F50), the pre-stop script (F49) and lifecycle events (F35) are P0 and in Lewbox's minimum fork release, so every node has them. There's no second way to handle a timer stop: no reconciler inferring a reason from timers, and no timer stop that skips hooks ([D15](../README.md#decisions)).

### Stop reasons

On `sandboxes.status_reason`, in activity, and in webhook events:

| `code` | Message |
|---|---|
| `idle_timeout` | The sandbox was stopped, as it was idle for its idle timeout. |
| `lifetime_reached` | The sandbox was stopped, as it reached its lifetime. |
| `credits_exhausted` | The sandbox was stopped, as its profile's credits ran out. |
| `budget_reached` | The sandbox was stopped, as its profile reached a budget. |
| `profile_suspended` | The sandbox was stopped, as its profile was suspended. |
| `node_lost` | The sandbox was lost with its node. |
| `requested` | (implicit; no reason shown) |

### `last_active_at`

A new read-only field on the sandbox: when AerolVM last counted activity, from the fork's sandbox object (T5; legacy's `last_active_at` exists in AerolVM's list response already). It's refreshed by the reconciler (once a minute) and on each `GET` of a running sandbox, at most every 10 s per sandbox. It lets the dashboard and Lewagent show "idle for 12 min, stops in 18 min" (`idle_stops_at`, derived).

### Changing timers

`PATCH /v0/sandboxes/{sbx}` with `idle_timeout` and/or `lifetime` ([lifecycle](lifecycle.md#settings-and-metadata)):

1. In a transaction: refuse while an operation runs (`409`); validate against limits; save.
2. If the sandbox is running, call `PUT …/lifecycle` on its node with the new timers (a changed lifetime counts from now; an unchanged one keeps its deadline). Recording `lifetime_started_at` on change gives the deadline to recompute.
3. If the node can't be reached: answer `503 cluster_unavailable`. The new values are saved and applied on the next start, or on the client's retry.

## Data model

On `sandboxes`:
- `idle_timeout_s`, `lifetime_s`;
- `lifetime_started_at timestamptz`: set on each start and on a lifetime change;
- `last_active_at timestamptz`, cached from AerolVM;
- `warn_before_s integer NOT NULL DEFAULT 300`;
- `idle_stops_at timestamptz`: set by an `idle_warning`, cleared by activity or a stop;
- `timers_armed boolean`: false while a stop operation has disarmed them;
- `status_reason jsonb`.

## API

On the sandbox object:
- `idle_timeout`, `lifetime`, and **new** `warn_before` (seconds before an idle stop to warn, `0` for none; default 300);
- **new:** `last_active_at`, `idle_stops_at` and `lifetime_ends_at`, each null when it doesn't apply.

Changed through `PATCH /v0/sandboxes/{sbx}`.

**`POST /v0/sandboxes/{sbx}/touch`** (new) counts as activity: it re-arms the idle timer, through the fork's touch (T3). For a page that shows a desktop or a dev server through a port, until port traffic counts on its own (T2), and for *Keep running* on an idle warning. It needs any of `sandboxes.exec`, `sandboxes.ports.view` or `sandboxes.agents` (so a sandbox key for a desktop can keep it awake), answers `200` with `{last_active_at, idle_stops_at}`, and counts at most once every 30 s per sandbox (later calls answer the same without calling the node). On a sandbox that isn't running: `409 sandbox_not_running`. It's separate from `POST …/activity`, which adds an app event to the log and doesn't keep anything awake.

## Fork changes

| # | Change | Why |
|---|---|---|
| T1 | `stop_after` / `destroy_after` relative to now | No age arithmetic across stops and starts |
| T2 | Exposed-port traffic counts as activity (HTTP routes and TCP proxy), at most once a minute per sandbox | A user on a desktop or dev server through a port isn't idle |
| T3 | `POST /v1/sandboxes/{id}/activity` (touch), and `X-Lewbox-Maintenance` requests not counted | An explicit touch; maintenance doesn't keep sandboxes awake |
| T4 | Lifecycle events with reason and time, consumable by the node agent (SSE or a local Unix socket) | Stops noticed in seconds, with a real reason |
| T5 | `last_active_at` on the sandbox object (if not already reliable) | Show idleness |
| T6 | A registered pre-stop script: a command AerolVM runs in the sandbox, with the sandbox's timeout, before any stop it makes itself ([aerolvm-fork F49](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)) | `on_stop` hooks and the in-sandbox cleanup run before backstop stops too ([hooks](hooks.md#when-lewbox-cant-act)). The node-side scrub is still what keeps credentials out of snapshots |
| T7 | **Notify-mode timers** (F50): `on_expiry: notify` emits `idle_warning` (at `warn_before`), `idle` and `lifetime_reached` instead of stopping, and stops only after `backstop_after`; `PUT …/lifecycle` disarms and re-arms | Lewbox decides when to stop, so every timer stop runs hooks and the re-check ([Lewbox decides when to stop](#lewbox-decides-when-to-stop)) |

## Security

Not security-sensitive, except that timers bound cost when credentials leak. A lifetime is the docs' advice for a ceiling on runaway cost. Timer stops don't weaken the rule that snapshots hold no imported credentials ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).

## Observability

- **Metrics:**
  - `lewbox_sandbox_timer_stops_total{reason}`;
  - `lewbox_sandbox_stop_notice_delay_seconds` (from AerolVM's `at` to recorded);
  - `lewbox_touches_total{source=stream|agent|session|port}`.
- **Activity:** `sandbox.stopped` with the reason; `sandbox.idle_warning`; `sandbox.idle_deferred` (a re-check kept it, with why).
- **Webhooks:** `sandbox.idle_warning` (new); the stop itself is `sandbox.stopping` / `sandbox.stopped` with `reason.code` `idle_timeout` or `lifetime_reached`, so there's no separate `sandbox.idle` event to keep in step.
- **Metrics:** also `lewbox_timer_events_total{type}`, `lewbox_idle_deferred_total{reason}`, `lewbox_timer_backstop_stops_total`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_ACTIVITY_TOUCH_INTERVAL` | `60s` | Most frequent touch per sandbox per replica |
| `LEWBOX_SANDBOX_MAX_TIMER` | `720h` | Upper bound on both timers (AerolVM's `MaxLifecycle`) |
| `LEWBOX_IDLE_WARN_BEFORE` | `5m` | Default `warn_before` for new sandboxes |
| `LEWBOX_TIMER_BACKSTOP_GRACE` | `10m` | How long after a timer expires the fork waits for Lewbox before stopping the sandbox itself; at least `stop_grace` + 5 minutes per sandbox |

## Testing

- **Unit:** the lifecycle mapping for persist and non-persist, in-place starts (age arithmetic) and T1.
- **Fake AerolVM:** timers fire on a fake clock; touches reset idle; maintenance calls don't (T3); lifecycle events (T4) arrive and are recorded.
- **Notify mode:** `idle_warning` becomes a webhook; `idle` with a running agent turn is deferred (touch, `idle_deferred`); `idle` on a quiet sandbox enqueues one stop even when the event arrives twice; the stop disarms the timers before its hooks; with the control plane stopped, the backstop fires after the grace, runs the script, and the later offload reads its results; `lifetime_reached` stops a busy sandbox.
- **Streams:** a long download with data moving keeps the sandbox; a silent one doesn't.
- **Live:** a sandbox with `idle_timeout: 120` and a desktop open through a private port stays up while the desktop is used (needs T2), and stops after it's closed.

## Build plan

1. `internal/sandboxes/timers.go`: lifecycle mapping; settings changes applying it.
2. Touch helper (`internal/aerolvm.Touch` with rate limiting) used by agent uploads and the touch endpoint.
3. `last_active_at` refresh; derived fields.
4. Event intake from the node agent (with [reconciler](reconciler.md)): `idle_warning`, `idle`, `lifetime_reached`, `stopped`.
5. The re-check, the stop enqueue and the timer disarm step; `POST /v0/sandboxes/{sbx}/touch`.
6. Fork work, all P0 for the alpha (in the minimum release): T3 (touch and maintenance), T4 (events, F35), T5, T6 (pre-stop, F49), T7 (notify mode, F50), and T2 (port traffic as activity, which Lewagent's desktops need); T1 with them.

## Open questions

1. ~~**Port traffic and idleness**, until T2 lands.~~ Decided (2026-10-29): `POST /v0/sandboxes/{sbx}/touch`, which a desktop page calls with its sandbox key while it's visible ([API](#api)).
2. **Idle while an agent waits on a person.** An approval left unanswered keeps the sandbox awake forever (agentd heartbeats). Should waiting on a person count only up to a limit (e.g. an hour)?
3. **Maintenance counted today.** Until T3, refresh jobs reset idle timers. Skip refreshing sandboxes that are close to their idle stop?
