# Reconciler

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

Lewbox's database says what each sandbox should be; AerolVM, on each node, says what it is. They drift: AerolVM stops sandboxes on their timers without telling anyone, nodes die, a crash can leave a container Lewbox doesn't know about. The reconciler compares the two, records what AerolVM did, finishes the stops it started, marks sandboxes lost with their nodes, corrects where a sandbox runs, and cleans up orphans. This plan covers what it checks, how it reports, how it stays safe when it's wrong, and how lifecycle events from the node make most of its work immediate.

## Today

`lewbox-gateway-legacy/internal/reconcile/reconcile.go`, every `GATEWAY_RECONCILE_INTERVAL` (60 s), **on every gateway replica**:

1. List every sandbox the app records `running` (`GET sandboxes?status=running`, without a key).
2. For each of their clusters, list AerolVM's sandboxes (for status) and read the sandbox index (for nodes).
3. For each sandbox:
   - AerolVM says `stopped`: report it `stopped`. The app then offloads one that persists (a `stop` operation) or deletes one that doesn't (`RecordSandboxProgress::finishStop`).
   - AerolVM says `error` or `destroyed`, or the listing misses it and a `GET` answers `404`/`410`: report it `failed`, or `deleted` if it doesn't persist (its destroy timer removed it).
   - It's on another node than recorded, or no node is recorded: correct the node.
4. Every report carries `expected_status: running` and the AerolVM id seen, so a stale report is refused (`RecordSandboxProgress::ensureExpected`).
5. A stop or destroy gets a `reason` inferred from timers (`stopReason`).
6. A node that doesn't answer leaves the sandboxes it should have alone until the next pass. Duplicate reports from other replicas are refused harmlessly.

Separately, in the app:
- `nodes:mark-offline` marks nodes offline after 3 minutes without a heartbeat, and ends billing for their sandboxes (`EndOfflineSandboxRuns`).
- `ResumeNodeSandboxes` bills them again and re-applies setup when the node returns.

The **session watcher** (same package) is described in [inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md).

**What's wrong with it.**
- Every replica does the whole job, relying on refusals for correctness.
- Stops are noticed up to a minute late, with a guessed reason.
- Sandboxes in other statuses (a `stopping` whose operation died) aren't looked at.
- AerolVM sandboxes Lewbox doesn't know about are never found.
- Node health and sandbox health live in two different processes.

## Goals

- The database converges to what AerolVM says, within seconds when events flow and within a minute when they don't.
- Every correction is safe to repeat and safe when the reconciler's view is stale.
- Nothing is destroyed on a guess: orphans only after a grace, and only ones tagged for this Lewbox instance and project.
- One reconciler at a time.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The 60 s pass. The checks above. Preconditions on every report. Inferring the reason as a fallback. Leaving unreachable nodes' sandboxes alone. Finishing AerolVM's own stops as operations |
| **Change** | Runs on the **leader** only ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md#leadership)). Driven first by **lifecycle events** from the node agent (fork change T4), with the pass as a backstop. Also checks sandboxes in transitional statuses with no live operation, and running sandboxes on offline nodes. Finds **orphans**. Reports through `sandboxes.Transition` with preconditions, in-process |
| **Drop** | The internal `GET sandboxes` and `PATCH sandboxes/{sbx}` HTTP calls. Per-replica duplication |

## Design

### Events

With the fork's lifecycle events ([timers-and-activity](timers-and-activity.md#fork-changes), T4 and T7), the node agent subscribes to AerolVM's events locally and forwards them over its connection, or `POST /node/v0/events` ([clusters/node-agent](../clusters/node-agent.md#lifecycle-events)). Each event has the AerolVM id, its tags (`lewbox_sandbox`, `lewbox_project`), a per-node sequence number, `type` and `at`.

- **`idle_warning`, `idle` and `lifetime_reached`** (notify-mode timers) go to the timers' handler, which warns, re-checks and enqueues the stop ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)). They're not the reconciler's: nothing has stopped yet.
- **`stopped`, `destroyed`, `error`, `started`** get the same rules as the pass (below), for that one sandbox, at once. A `stopped` with `backstop: true` is an [AerolVM stop](#aerolvms-own-stops) like any timer stop, and its offload reads the pre-stop script's results ([hooks](hooks.md#when-lewbox-cant-act)).

Events are at-least-once and may arrive out of order; they're deduplicated by `(node, seq)`, and the preconditions make stale ones harmless. **The pass stays** as the safety net for events lost before the node agent stored them and for fork releases without events; with events it runs every 5 minutes instead of every minute (`LEWBOX_RECONCILE_INTERVAL`).

### The pass

A periodic job, `reconcile.sandboxes`, every `LEWBOX_RECONCILE_INTERVAL` (60 s), on the leader:

1. **Load** the sandboxes that should exist in AerolVM: those `running`, plus those `creating`, `starting`, `stopping` or `deleting` with a `runtime_id`, grouped by cluster.
2. **For each cluster** (in parallel, up to 8), with a 30 s budget:
   - list AerolVM's sandboxes tagged with this instance (`lewbox_instance=<id>`; new tag), across the cluster, with paging;
   - read the sandbox index (node of each).

   A listing that isn't complete (AerolVM says so when a node didn't answer) is used only for what it *has*; absence proves nothing.
3. **For each sandbox Lewbox has running**, decide by AerolVM's answer:

   | AerolVM says | Persists | Action |
   |---|---|---|
   | `started`, on the recorded node | | Nothing. Refresh `last_active_at` |
   | `started`, on another node, or none recorded | | Correct `node_id` |
   | `stopped` | Yes | `Transition` → `stopped` (reason from the event, else inferred), then a `stop` operation to offload it, reason the same ([AerolVM's own stops](#aerolvms-own-stops)) |
   | `stopped` | No | Its destroy timer should have destroyed it, so this is a stop without one: `Transition` → `deleting`, and a `delete` operation |
   | `destroyed`, or missing and `GET` says `404`/`410` | No | `Transition` → `deleted` (the destroy timer did it), reason inferred |
   | `destroyed`, or missing and `GET` says `404`/`410` | Yes | `Transition` → `failed` with `node_lost`, or with `vanished` when its node is fine. See [open questions](#open-questions) |
   | `error` | | `Transition` → `failed`, with AerolVM's message in `status_reason.message` |
   | Missing, and its node is unreachable | | Nothing; the node's health decides ([nodes offline](#nodes-offline)) |

4. **Transitional sandboxes** (`creating`, `starting`, `stopping`, `deleting`) whose operation is finished or missing: this shouldn't happen, as transitions and operations commit together. If it does (a bug), log at `error`, raise an alert, and don't touch it.
5. **Operations whose job is gone** (status `pending`/`running`, no job row): re-enqueue. Again a should-never-happen backstop, cheap to check.
6. **Orphans:** see [below](#orphans).

### Preconditions

Every correction is made through `sandboxes.Transition` with `ExpectStatus` (the status read at the start of the pass) and `ExpectRuntimeID` (the AerolVM id seen). If the sandbox has moved on, the correction is skipped. A stop or start may have begun, or it may have been resumed as a new AerolVM sandbox. Skipped corrections are counted, not logged as errors.

This is what makes a pass that read the database 20 s ago safe, and why events and passes can overlap.

### AerolVM's own stops

When AerolVM stops a sandbox on its timer, Lewbox finishes it as any stop would. In one transaction:

1. `Transition` the sandbox from `running` → `stopped`, with `status_reason` and the metering run ending at AerolVM's `at`. This writes the `sandbox.stopped` activity event (actor `app`) and the `sandbox.stopped` webhook.
2. For a sandbox that persists, on a cluster with cloud storage: insert a `stop` operation (reason `idle_timeout` or `lifetime_reached`), and `Transition` → `stopping`. The operation offloads it ([operations](operations.md#stop)); its step `stopped` is already true, so it starts at the scrub. The `on_stop` hooks and setup cleanup are skipped, as the sandbox can't run anything: the pre-stop script ran them before AerolVM stopped it, where the fork has F49, and the scrub reads their results ([hooks](hooks.md#when-lewbox-cant-act), [persistence](persistence.md#credentials-in-snapshots)).

With notify-mode timers (F50), AerolVM stops a sandbox itself only as a backstop, when Lewbox didn't act on `idle` or `lifetime_reached` in time; most timer stops are Lewbox's own stop operations and never reach this path.
3. For one that doesn't persist: insert a `delete` operation and `Transition` → `deleting`.

The operation comes first, so the status webhook carries it, as legacy does.

### Nodes offline

Node health is the [clusters](../clusters.md) plan's ([clusters/health-and-metrics](../clusters/health-and-metrics.md)). A node with no heartbeat for `LEWBOX_NODE_OFFLINE_AFTER` (3 min) is `offline`. For sandboxes, the reconciler then:

- **Offline:** metering for its `running` sandboxes pauses (the run ends at the last heartbeat), and each sandbox gets `reachable: false` (new) in the API. Status stays `running`, since nothing says it stopped. Requests inside it fail with `503 cluster_unavailable`.
- **Back online:** on the next pass, sandboxes AerolVM still has `started` are `reachable` again, metered again (a new run), and get an `apply_setup` (reason `node_back_online`), since profile changes made while it was away skipped them. Ones AerolVM lost are handled by the table above.
- **Lost:** a node offline for `LEWBOX_NODE_LOST_AFTER` (24 h), or removed by a project admin, is `lost`. Its `running` sandboxes become `failed` with `node_lost`.

### Orphans

AerolVM sandboxes tagged `lewbox_instance=<this instance>` that Lewbox doesn't expect:
- no sandbox with that `lewbox_sandbox` id;
- a sandbox `deleted` or `failed`;
- a `runtime_id` that isn't the sandbox's current one (e.g. a resume that raced).

These come from crashes between AerolVM's create and Lewbox's record, which adopt-by-tag mostly prevents, and from deletes that failed halfway.

1. First seen: record in `reconcile_orphans` with `first_seen_at`.
2. Still orphaned after `LEWBOX_ORPHAN_GRACE` (15 min) and not adopted by a running operation: destroy it in AerolVM, and log at `warn` with its tags.
3. Sandboxes without the instance tag (another Lewbox, or made by hand) are **never** touched.

Orphan destruction is off by default for the first release (`LEWBOX_ORPHAN_DESTROY=false`: report only, in metrics and the dashboard's cluster page), until we trust it.

## Data model

```sql
CREATE TABLE reconcile_orphans (
  cluster_id     bigint NOT NULL REFERENCES clusters,
  runtime_id     text NOT NULL,
  tags           jsonb NOT NULL,
  first_seen_at  timestamptz NOT NULL,
  last_seen_at   timestamptz NOT NULL,
  destroyed_at   timestamptz,
  PRIMARY KEY (cluster_id, runtime_id)
);
```

`sandboxes.unreachable_since timestamptz` (null when reachable) backs `reachable` in the API.

## Security

- The reconciler acts as `app`. Every action it takes is in the activity log with that actor and a reason.
- It only ever destroys AerolVM sandboxes tagged for this instance; tags are set by Lewbox on create and can't be set by consumers.

## Observability

- **Metrics:**
  - `lewbox_reconcile_pass_duration_seconds{cluster}`;
  - `lewbox_reconcile_corrections_total{kind}`, with kind one of `stopped`, `deleted`, `failed`, `node_corrected`, `skipped_stale`;
  - `lewbox_reconcile_events_total{type}`;
  - `lewbox_reconcile_orphans{cluster}`;
  - `lewbox_reconcile_incomplete_listings_total`.
- **Alerts:** a pass failing for a cluster three times; transitional sandboxes without operations; orphans present.
- **Logs:** each correction at `info` with the before and after, and the AerolVM state seen.

## Configuration

| Variable | Default |
|---|---|
| `LEWBOX_RECONCILE_INTERVAL` | `60s`; clusters whose fork release has lifecycle events are passed every `LEWBOX_RECONCILE_INTERVAL_WITH_EVENTS` (`5m`) |
| `LEWBOX_NODE_OFFLINE_AFTER` | `3m` (owned by [clusters](../clusters.md)) |
| `LEWBOX_NODE_LOST_AFTER` | `24h` |
| `LEWBOX_ORPHAN_GRACE` | `15m` |
| `LEWBOX_ORPHAN_DESTROY` | `false` |

## Testing

Ported from `lewbox-gateway-legacy/internal/reconcile/reconcile_test.go`, plus:
- every row of the decision table, for `persist` both ways;
- stale passes: a sandbox stopped, started or resumed between the read and the report is left alone;
- events and passes racing: the outcome is the same, once;
- incomplete listings: absence is ignored;
- a node going offline and coming back: metering pauses and resumes, and `apply_setup` is enqueued;
- orphans: found, graced, destroyed only with the instance tag and only when enabled.

## Build plan

1. `internal/reconcile`: the pass as a leader-only periodic job; the decision table as a pure function (`decide(sandbox, aerolvmState) action`), tested exhaustively.
2. Event intake from the node API, calling the same `decide`.
3. Node offline, online and lost handling, with [clusters](../clusters.md).
4. Orphan tracking (report only), then destruction behind the flag.
5. The `lewbox_instance` tag on every create ([operations](operations.md#create)).

## Open questions

1. **A persistent sandbox that vanished.** If AerolVM lost a running persistent sandbox (node died, disk wiped), its latest snapshot (from its previous stop) still exists. Mark it `failed`, or `stopped` with `status_reason: node_lost`, so a start resumes from that snapshot and loses only what changed since? The second is kinder but surprising. Proposed: `stopped` with a clear reason and message, when a snapshot exists; `failed` otherwise.
2. **Node lost after 24 h:** too long or too short? Projects may run homelab nodes that sleep. Make it a per-cluster setting?
3. **Instance tag.** Do we need `lewbox_instance` as well as `lewbox_project`, given a cluster belongs to one project? Yes if two instances might ever share a cluster (e.g. staging and production pointing at one homelab). Cheap to add.
