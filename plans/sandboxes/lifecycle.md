# Sandbox lifecycle

> Status: Draft · Milestone: Alpha (M1) · Core; some create fields owned by plugins · Updated: 2026-10-29

A sandbox is created, runs, may be stopped and started again, and is finally deleted. This plan covers each status and transition, the create request and how each field is decided, waiting for a create, start, stop, restart and delete as requests, changing settings, reading and listing sandboxes, and who may do each. The work behind each transition is an [operation](operations.md); what a stop keeps is [persistence](persistence.md).

## Today

- **Reserving** is the app's `ReserveSandbox` (`lewbox-legacy/app/Actions/Sandboxes/ReserveSandbox.php`). Under a lock on the profile row it handles the steps below, then inserts the sandbox (`creating`) and its `create` operation:
  - replays an idempotency key, comparing a canonical fingerprint;
  - chooses the cluster and refuses a suspended one;
  - refuses `persist` without cloud storage;
  - resolves the template to its latest ready build on that cluster;
  - fills the size from defaults within limits (`ResolveSandboxSize`);
  - checks totals and credits (`EnsureProfileCanRun`), and storage when the sandbox persists;
  - merges the template's env with the create's;
  - records the template's ports, persisted, with proxy ports for private ones (`ChooseProxyPort`).
- **The gateway answers.** `POST /v1/sandboxes` calls the app's `POST sandboxes`, hands the operation to its local runner, and waits up to `GATEWAY_CREATE_WAIT` (60 s). It answers `201` with the sandbox, or `202` with the operation (`lewbox-gateway-legacy/internal/server/sandboxes.go`).
- **Requests for start, stop, restart, delete and apply_setup** go through `RequestSandboxOperation` (`lewbox-legacy/app/Actions/Sandboxes/RequestSandboxOperation.php`). It:
  - refuses a start or restart on a suspended cluster or profile, or one over its credits;
  - checks limits for a start;
  - ends billing at once for a stop, restart or delete;
  - turns a stop of a sandbox that doesn't persist into a delete;
  - returns nothing when the sandbox is already as asked.
- **Reports** from the gateway (status, AerolVM id, node, snapshot, command) go through `RecordSandboxProgress` (`…/RecordSandboxProgress.php`). It locks the profile, applies the `expected_status` and `expected_aerolvm_id` preconditions, refuses changes to a deleted sandbox, starts or ends billing runs, clears sessions and port URLs, and finishes AerolVM's own stops.
- **`status_sequence`** is bumped by model hooks, and webhooks are raised from them (`app/Models/Sandbox.php`).
- **Settings** go through `UpdateSandboxSettings`, which refuses timer changes while an operation runs. The gateway then applies the timers to AerolVM itself (`Runner.ApplyLifecycle`).

**What's wrong with it.**
- The state machine lives in four places (the reserve, the request, the progress report and the model hooks), plus the gateway's handlers, with HTTP between them.
- Start and stop exist twice in the API (`PATCH status` and `POST …/operations`).
- The Admin API couldn't create sandboxes, so the dashboard had a separate reserve path.
- A delete is fire-and-forget for Lewagent (`kill(wait: false)`), and it reloads the page to find out.

## Goals

- One state machine, in one Go function, enforced by the database too.
- A create that does every check in one transaction and is safe to retry.
- Every transition visible in order (`status_sequence`), in the activity log and as a webhook, written atomically with the change.
- Clients can wait for any operation without polling (`?wait=`), and can follow status live ([activity-and-webhooks](../activity-and-webhooks.md)).
- Admin keys and the dashboard use the same create and the same endpoints as profile keys.

**Non-goals:** moving a sandbox between clusters; resizing a sandbox after create (both later; see [open questions](#open-questions)).

## Keep, change, drop

| | |
|---|---|
| **Keep** | Statuses and their meaning. Fields of a create. Cluster choice order. Asking for more than a limit is refused, not lowered. `persist` fixed at create. The `201` / `202` wait. One operation at a time. Metering ends when a stop is *asked for*. Deleted sandboxes stay readable. Metadata rules. `env` write-only |
| **Change** | One `Transition` function. One way to start and stop (`POST …/operations`). Admin keys and the dashboard create through `POST /v0/sandboxes` with `profile`. Long-poll on operations. `cpu` stored as millicores, shown as a number. Template resolved by the latest ready build, recorded on the sandbox *and* the operation |
| **Drop** | `PATCH /v0/sandboxes/{sbx}` with `status` (open question). The dashboard's separate reserve path. Model-hook side effects |

## Design

### Statuses

| Status | Means | Metered | Operation | Reachable inside |
|---|---|---|---|---|
| `creating` | Its `create` is under way | No | `create` | No |
| `running` | Running on a node | Yes | None, `apply_setup`, or the first half of none | Yes |
| `stopping` | A stop (or the first half of a restart) is under way | No | `stop`, `restart` | No |
| `stopped` | Stopped. A sandbox that persists only | No | None, or `restart` mid-way | No |
| `starting` | A start is under way | No (until `running`) | `start` | No |
| `deleting` | A delete is under way | No | `delete` | No |
| `deleted` | Gone for good. Row kept, listed only with `status=deleted` | No | None | No |
| `failed` | A create failed, a stop failed before it stopped, a delete failed, or it was lost with its node. Can only be deleted | No | None | No |

`status` is a Postgres enum (`sandbox_status`). The row also has `status_reason` (`{code, message}` or null), e.g. `{code: "idle_timeout"}` on a stop AerolVM made, `{code: "credits_exhausted"}` on one Lewbox asked for.

### Transitions

Every change of status goes through `sandboxes.Transition(ctx, tx, sandbox, to, opts)`, which:

1. Locks the sandbox row (`SELECT … FOR UPDATE`).
2. Checks `opts.ExpectStatus` and `opts.ExpectRuntimeID` when given. If the sandbox moved on, it returns `ErrStale` and changes nothing (the reconciler's reports; see [reconciler](reconciler.md#preconditions)).
3. Checks the transition is allowed (table below). A disallowed one is a bug, logged with the sandbox and both statuses, and refused.
4. Sets `status`, `status_reason` and `status_changed_at`, and increments `status_sequence`.
5. On entering `running`, opens a metering run (`sandbox_runs`). On leaving it, closes the run with the time the stop was *asked for* (`opts.MeteringEndsAt`), not now ([credits/metering](../credits/metering.md)).
6. On entering `stopped`, `deleted` or `failed`, clears runtime state: sessions become `lost`, port URLs are cleared, and non-persisted ports are forgotten ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md)).
7. Writes an activity event and a webhook event (`sandbox.<status>`, with `previous_status`, `status_sequence`, `reason` and `operation`) in the same transaction ([activity-and-webhooks](../activity-and-webhooks.md)).
8. `NOTIFY sandbox_changed, '<sbx id>'`, so long-polls and live streams on any replica wake.

Allowed transitions:

| From → to | By |
|---|---|
| (new) → `creating` | Create |
| `creating` → `running` | `create` op, once AerolVM reports it started |
| `creating` → `failed` | `create` op failed |
| `running` → `stopping` | Stop or restart asked for |
| `running` → `deleting` | Delete asked for; a stop of a sandbox that doesn't persist |
| `running` → `stopped` | Reconciler: AerolVM stopped it on its own (then a `stop` op offloads it, which goes back to `stopping` and on to `stopped`; see [reconciler](reconciler.md)) |
| `running` → `failed` | Reconciler: lost with its node, or AerolVM reports `error` |
| `stopping` → `stopped` | `stop` op (after offload), `restart` op (midway) |
| `stopping` → `running` | `restart` op failed before AerolVM stopped it, and it's still running |
| `stopping` → `failed` | `stop` op failed before AerolVM stopped it |
| `stopped` → `stopping` | AerolVM's own stop being offloaded |
| `stopped` → `starting` | Start asked for |
| `stopped` → `running` | `restart` op, second half |
| `stopped` → `deleting` | Delete asked for |
| `starting` → `running` | `start` op |
| `starting` → `stopped` | `start` op failed; left as it was |
| `starting` → `failed` | `start` op: nothing to start from (no snapshot and gone from its node) |
| `deleting` → `deleted` | `delete` op |
| `deleting` → `failed` | `delete` op failed |
| `failed` → `deleting` | Delete asked for |

A sandbox's status changes a moment *before* its operation finishes, as now: the operation's last step records the status, then the operation finishes. Clients wait for the operation, not the status ([operations](operations.md#waiting)).

### `status_sequence`

A `bigint`, incremented on every status change, starting at 1 on create. It's on the sandbox, its webhook events and its activity events. Consumers (Lewagent stores it as `computers.status_sequence`) ignore a webhook whose sequence is lower than the last they saw, since deliveries can arrive out of order.

### Create

`POST /v0/sandboxes` (profile key with `sandboxes.create`, or admin key, or the dashboard).

#### Fields

| Field | Type | Rules | Default |
|---|---|---|---|
| `profile` | string | `prf_` id or `external_id`. **Admin keys and the dashboard only**, and required for them. A profile key's request with it is `validation_failed` | The key's profile |
| `image` | string | A public image reference (e.g. `ubuntu:24.04`, `ghcr.io/acme/tool:1.2`). Exactly one of `image` and `template` | |
| `template` | string | A template id the profile can use ([templates](../templates.md#which-templates-a-profile-can-use)) | |
| `cluster` | string | Name (`eu-west`) or `clu_` id. The key must reach it | [Cluster choice](#choosing-the-cluster) |
| `isolation` | string | **New.** `container`, `gvisor` or `microvm`: offered by the cluster and allowed for the profile (only `container` in the alpha). Fixed for the sandbox's life | [Isolation choice](#choosing-the-isolation-level) |
| `cpu` | number | `0.1` to `256`, at most 3 decimal places (stored as millicores) | The template's, else `lewbox.sandboxes.defaults.cpu` (2), capped at the profile's `sandbox_vcpus` |
| `memory_mb` | integer | ≥ 128 | Template's, else 4096, capped at `sandbox_memory_mb` |
| `disk_gb` | integer | ≥ 1 | Template's, else 20, capped at `sandbox_disk_gb` |
| `idle_timeout` | integer or null | Seconds, 60 to 2 592 000 | The profile's `sandbox_idle_seconds` limit, else none |
| `lifetime` | integer or null | Seconds, 60 to 2 592 000 | The profile's `sandbox_lifetime_seconds` limit, else none |
| `persist` | boolean | Fixed for the sandbox's life | `false` |
| `env` | object | Up to 100; names `[A-Za-z_][A-Za-z0-9_]*`, at most 128 characters; values strings up to 32 KiB, 256 KiB total. Write-only | `{}` |
| `metadata` | object | Up to 50; keys `[A-Za-z0-9_.:/-]{1,64}`; values strings ≤ 255 | `{}` |
| `repositories` | array | Up to 20 ([repositories](repositories.md)) | `[]` |
| `git` | object | **New.** `{access, repositories}`: the access the sandbox's GitHub tokens have (`read`, `write`, `full`, or a permission set), and the repositories they reach, each optionally with its own `access`. Up to the project's cap ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)) | `access: "full"`; the cloned GitHub repositories |
| `ports` | array | **New.** Up to 20 `{port, protocol, access}`, exposed and persisted as the template's are, after them. Replaces the expose-after-create round trip | `[]` |
| `hooks` | object | **New.** `{on_create, on_start, on_stop}`: commands run once after setup, on every start, and before every stop, whatever asked for it. Needs `sandboxes.exec` too ([hooks](hooks.md#fields)) | none (the template's, if any) |
| `stop_grace` | integer | **New.** Seconds the `on_stop` hooks have in total, 0 to 600 ([hooks](hooks.md#stops-are-bounded)) | The template's, else 60 |
| `scrub` | array | **New.** Up to 50 paths removed from the sandbox, on the node, before any snapshot. Needs `sandboxes.exec` too ([hooks](hooks.md#scrub)) | `[]` (plus the template's) |
| `warn_before` | integer | **New.** Seconds before an idle stop to send `sandbox.idle_warning`; `0` for none ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)) | 300 |
| `wait` | boolean | | `true` |

**Fields owned by plugins.** Most fields are core. `git` belongs to the core Git framework and needs a Git provider plugin (`git/github`) to mint tokens; the size defaults and caps come from the `limits` plugin; the credits check from the `credits` plugin ([plugins/extension-points](../plugins/extension-points.md)). A plugin registers the fields it owns with their validation; a field whose plugin is off for the project is refused with `422 plugin_disabled` on that field, and a field nobody owns is `validation_failed`.

`Idempotency-Key` header: see [api/idempotency](../api/idempotency.md). The fingerprint covers every field above and the key's id. A replay answers as the first request's sandbox *is now*, with `Idempotent-Replayed: true`.

A size you ask for that's over a per-sandbox limit is refused (`validation_failed`, naming the limit), not lowered. A size you leave out is the default, or the limit where that's lower. The same applies to timers.

#### Choosing the cluster

The first of:

1. The request's `cluster`. The key must reach it, or it's `validation_failed` on `cluster`, as for one that doesn't exist.
2. The profile's `default_cluster`, if the key reaches it.
3. The only cluster the key reaches.

Otherwise: `422 cluster_required`. A suspended cluster that was chosen is refused with `409 cluster_suspended`, never passed over. Admin keys and the dashboard reach every cluster of the project, then filtered by the profile's own cluster access, if profiles get one ([profiles](../profiles.md); open question there).

#### Choosing the isolation level

After the cluster, the sandbox's [isolation level](../clusters/runtimes.md#isolation-levels) (`container` on Docker; `gvisor` from the beta; `microvm` on Firecracker later). **In the alpha every cluster offers only `container`**, so a create asking for another level is `422 isolation_unavailable` (`reason: not_offered`). The level is the first of:

1. The request's `isolation`. It must be offered by the cluster and allowed for the profile ([profiles § Isolation](../profiles.md#isolation)), or it's `422 isolation_unavailable` (`reason: not_offered` or `not_allowed`).
2. The profile's `default_isolation`, if the cluster offers it.
3. The cluster's `default_isolation`, if the profile is allowed it.
4. The strongest level both the cluster offers and the profile is allowed.

Otherwise: `422 isolation_unavailable` (`reason: none_allowed`). The error carries `available`, the levels this profile may use on this cluster. The full rules, and what each level can do, are in [clusters/runtimes](../clusters/runtimes.md#choosing-a-sandboxs-level).

The sandbox keeps its level, and its runtime, through every stop and start: a start resumes it on a node that runs that level, from a snapshot that runtime took.

#### Choosing the image

- `image` is used as given. AerolVM pulls it on the node.
- `template` resolves to the template's **latest ready build on the chosen cluster** ([templates/builds](../templates/builds.md#which-build-a-sandbox-uses)). The sandbox records `template_id` and `template_build_id`, and keeps that build for life. If there's no ready build, it's `422 template_unavailable` with `template.reason`: `no_cloud_storage`, `not_on_cluster`, `building`, `failed` or `not_built`.
- The template's default size, env and ports apply. The create's `env` goes on top of the template's, and its `ports` after the template's.

#### The transaction

All in one transaction (`sandboxes.Create`):

1. **Idempotency.** Look up `(profile_id, idempotency_key)` within 24 h. On a match, compare fingerprints: the same request returns the existing sandbox; a different one is `422 idempotency_key_reused`. ([api/idempotency](../api/idempotency.md) owns the general mechanism; sandbox create uses it with the sandbox as the stored result.)
2. **Lock the profile row** (`FOR UPDATE`), so two creates can't both take the last slot.
3. Refuse a suspended profile (`403 profile_suspended`, though auth catches it first).
4. Choose the cluster, the isolation level and the build (a build for the level's image kind: an OCI image, or a Firecracker template at `microvm`; [templates/builds](../templates/builds.md#firecracker-clusters)).
5. Refuse `persist` on a cluster without cloud storage, or at a level that can't snapshot there (`422 persistence_unsupported`, [persistence](persistence.md#firecracker)). In the alpha the only level is `container`, which can; anything else is refused earlier as `isolation_unavailable`. Refuse `gpu` at a level other than `container`.
6. Resolve the size and timers.
7. Check limits ([limits](../limits.md)): `running_sandboxes`, `vcpus`, `memory_mb`; `storage_gb` when it persists; budgets and credits when [credits](../credits.md) is on (`402 credits_exhausted`, `429 limit_exceeded` with `limit`).
8. Insert the sandbox (`creating`, `status_sequence = 1`) with `env` encrypted ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)).
9. Insert its ports: the template's, then the request's, all persisted. Private ones get a proxy port ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)).
10. Insert the `create` operation, and enqueue its job ([operations](operations.md)).
11. Write the `sandbox.creating` events, through `Transition`.

#### Waiting

- `wait: true` (the default): the handler waits for the operation to finish, up to `LEWBOX_CREATE_WAIT` (60 s). It listens on `operation_finished` and re-reads the row, so it doesn't matter which replica runs the job.
  - Succeeded: `201` with the sandbox, `running`.
  - Failed: `502 create_failed` or `503 cluster_capacity_exceeded`, naming the sandbox, which is left `failed`.
  - Still going: `202` with the sandbox, `creating`, and its `operation`.
- `wait: false`: `202` at once.
- A replayed create waits the same way, for the first request's operation.

60 s stays, as it's what most proxies tolerate. Legacy's Laravel Cloud edge cut requests at ~20 s, which broke waiting creates for Lewagent; that constraint goes with Laravel Cloud, but the docs should still recommend `wait: false` behind short proxies.

### Start, stop, restart

`POST /v0/sandboxes/{sbx}/operations` with `{"kind": "start" | "stop" | "restart"}`.

| Kind | Needs | From | Answers | Then |
|---|---|---|---|---|
| `start` | `sandboxes.start` | `stopped` | `202` with the operation; `200` with no operation if already `running` | `starting` → `running` |
| `stop` | `sandboxes.stop` | `running` | `202`; `200` if already `stopped` | Persists: `stopping` → `stopped`. Doesn't: becomes a `delete`, `deleting` → `deleted` |
| `restart` | both | `running` | Always `202` | `stopping` → `stopped` → `running`, on the same node, never deleted |

In one transaction: lock the sandbox, refuse with `409 operation_in_progress` (carrying `operation: {id, kind}`) if one is in flight, refuse with `409 invalid_state` from the wrong status, and then:

- **Start or restart:**
  - refused on a suspended cluster (`409 cluster_suspended`) or with no credits (`402 credits_exhausted`);
  - a start (not a restart) is also checked against `running_sandboxes`, `vcpus` and `memory_mb` under the profile lock;
  - `storage_gb` isn't checked again, as the sandbox already counts.
- **Stop, restart or delete:** the metering run ends now (`MeteringEndsAt = now`).
- Then insert the operation, `Transition` the sandbox, and enqueue.

The response body is the **operation**, with the sandbox's id and new status. (Legacy answered with the sandbox, holding its operation; the SDKs then unwrapped it. Answering with the operation is what the client follows next.)

### Delete

`DELETE /v0/sandboxes/{sbx}` (`sandboxes.delete`).

- From `running`, `stopped` or `failed`: `202` with the `delete` operation.
- From `deleted`: `200` with the sandbox and no operation, so a retried delete is safe.
- With an operation in flight: `409 operation_in_progress`. Exception: a `create` stuck waiting on capacity can be cancelled by a delete ([operations](operations.md#cancelling)).
- Metering ends at once. The row stays, readable, with its id never reused.
- Snapshots go too ([persistence](persistence.md#deleting)). Conversations keep their history ([agents/conversations](../agents/conversations.md)).

### Settings and metadata

`PATCH /v0/sandboxes/{sbx}` with any of `metadata`, `idle_timeout`, `lifetime`, `warn_before` (`sandboxes.update`), and `hooks`, `stop_grace`, `scrub` (`sandboxes.update` **and** `sandboxes.exec`, never a sandbox key: [hooks](hooks.md#changing-hooks)). Answers `200` with the sandbox.

- `metadata` replaces the map; `{}` or `null` clears it. It can change during an operation.
- Timers:
  - `null` means the profile's limit, or none;
  - over the limit is `422`;
  - refused with `409 operation_in_progress` while an operation runs, as it may be applying the old ones;
  - on a running sandbox they're applied to AerolVM at once ([timers-and-activity](timers-and-activity.md#changing-timers)); if the node can't be reached, saved anyway and `503 cluster_unavailable`;
  - a changed `lifetime` counts from now.
- `hooks`, `stop_grace` and `scrub`: refused with `409 operation_in_progress` while an operation runs, so a stop in progress keeps the hooks it started with; on a running sandbox, the backstop script is rewritten at once ([hooks](hooks.md#changing-hooks)).
- From `deleted` or `failed`: `409 invalid_state`.

### Reading and listing

- `GET /v0/sandboxes/{sbx}`: any key that can see it, with no permission needed. A sandbox on a cluster the key doesn't reach, or another profile's, is `404 not_found`. A deleted one is readable.
- `GET /v0/sandboxes`, oldest first, cursor-paged (50, at most 100), with these filters:
  - `status`, one or several, comma-separated;
  - `cluster`;
  - `template`;
  - `isolation` (new);
  - `metadata[key]=value` (up to 10, all must match);
  - `created_after` and `created_before` (new);
  - `profile`, for admin keys only.

  Deleted sandboxes appear only with `status=deleted`.
- Metadata filters use a GIN index on `metadata jsonb_path_ops` (`metadata @> '{"k":"v"}'`).

### The sandbox object

| Field | Type | Notes |
|---|---|---|
| `id` | string | `sbx_…` |
| `profile` | object | `{id, name, external_id}`. **New**; useful to admin keys |
| `status` | string | |
| `status_sequence` | integer | **New** in the object (was webhook-only) |
| `status_reason` | object or null | `{code, message}`: `idle_timeout`, `lifetime_reached`, `credits_exhausted`, `budget_reached`, `profile_suspended`, `node_lost`, … |
| `cluster` | object | `{id, name}` |
| `isolation` | string | `container`, `gvisor` or `microvm`. **New** |
| `runtime` | string | The runtime behind it: `docker`, `gvisor` or `firecracker`. **New**, for people who need to know |
| `template` | string or null | Template id |
| `template_build` | string or null | `bld_…`. **New** |
| `image` | string or null | |
| `cpu`, `memory_mb`, `disk_gb` | | |
| `idle_timeout`, `lifetime` | integer or null | Seconds |
| `persist` | boolean | |
| `metadata` | object | |
| `operation` | object or null | The one in flight |
| `ports` | array | [inside-a-sandbox/ports](../inside-a-sandbox/ports.md) |
| `setup` | object or null | [profile-setup](profile-setup.md#the-report) |
| `repositories` | array | [repositories](repositories.md#the-report) |
| `git` | object | The sandbox's Git access, each repository's effective permissions, and its tokens' status, never the tokens ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)). **New** |
| `hooks` | object | Its hooks (the template's and its own, marked by `source`, without `env`), `stop_grace`, and each hook's last run: status, exit code, duration, redacted stderr tail ([hooks](hooks.md#the-report)). **New** |
| `scrub` | array | The paths removed before any snapshot, the template's and its own. **New** |
| `warn_before`, `last_active_at`, `idle_stops_at`, `lifetime_ends_at` | | [timers-and-activity](timers-and-activity.md#api). **New** |
| `created_at`, `started_at`, `stopped_at` | string or null | `started_at`: when it last became `running` (lifetime counts from it). **New** |
| `created_by` | object | `{type, id, name}`: `profile_key`, `admin_key`, `user`, `app`. **New** |

`env` is never returned.

## Data model

```sql
CREATE TYPE sandbox_status AS ENUM
  ('creating','running','stopping','stopped','starting','deleting','deleted','failed');

CREATE TABLE sandboxes (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id          text NOT NULL UNIQUE,                 -- sbx_…
  project_id         bigint NOT NULL REFERENCES projects,
  profile_id         bigint NOT NULL REFERENCES profiles,
  cluster_id         bigint NOT NULL REFERENCES clusters,
  node_id            bigint REFERENCES nodes ON DELETE SET NULL,
  isolation          text NOT NULL,                         -- container | gvisor | microvm; fixed for life
  template_id        bigint REFERENCES templates,
  template_build_id  bigint REFERENCES template_builds,
  image              text,                                  -- the public image, or the build's image ref
  purpose            text NOT NULL DEFAULT 'workload',      -- workload | sign_in
  cpu_millis         integer NOT NULL,
  memory_mb          integer NOT NULL,
  disk_gb            integer NOT NULL,
  idle_timeout_s     integer,
  lifetime_s         integer,
  persist            boolean NOT NULL DEFAULT false,
  env                bytea,                                 -- encrypted JSON
  metadata           jsonb NOT NULL DEFAULT '{}',
  status             sandbox_status NOT NULL,
  status_sequence    bigint NOT NULL DEFAULT 1,
  status_reason      jsonb,
  status_changed_at  timestamptz NOT NULL,
  runtime_id         text,                                  -- AerolVM's sandbox id; changes on each resume
  latest_snapshot_id bigint,                                -- REFERENCES sandbox_snapshots
  setup              jsonb,                                 -- the setup report
  repositories       jsonb,                                 -- the clone report
  hooks              jsonb NOT NULL DEFAULT '{}',           -- {on_create, on_start, on_stop}: template's (copied at create) and own; hook env encrypted inside
  hook_runs          jsonb NOT NULL DEFAULT '[]',           -- the last run of each hook (see hooks)
  stop_grace_s       integer NOT NULL DEFAULT 60,
  scrub_paths        text[] NOT NULL DEFAULT '{}',          -- template's and own
  hooks_updated_by   jsonb,                                 -- actor who last set hooks or scrub
  started_at         timestamptz,
  stopped_at         timestamptz,
  created_by_type    text NOT NULL,
  created_by_id      bigint,
  idempotency_key    text,
  idempotency_hash   bytea,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON sandboxes (project_id, profile_id, status);
CREATE INDEX ON sandboxes (cluster_id, status);
CREATE INDEX ON sandboxes USING gin (metadata jsonb_path_ops);
CREATE UNIQUE INDEX ON sandboxes (profile_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
```

Changes from legacy:
- `aerolvm_id` becomes `runtime_id`.
- `isolation` is new: legacy had one runtime per cluster, so a sandbox had none of its own.
- `command` is dropped: the fork keeps a resumed sandbox's command right (F10), so Lewbox records none ([persistence](persistence.md#the-images-command)).
- `snapshot` becomes a reference to `sandbox_snapshots`.
- `tenant_id` becomes `project_id`.
- `env` is encrypted with the keyring rather than `APP_KEY`.
- `git_repositories` and `github_token_expires_at` move to the Git plan's own table ([git/tokens](../git/tokens.md)).

## API

| Endpoint | Does | Permission |
|---|---|---|
| `POST /v0/sandboxes` | Create | `sandboxes.create`; with `hooks` or `scrub`, also `sandboxes.exec` |
| `GET /v0/sandboxes` | List | None |
| `GET /v0/sandboxes/{sbx}` | Get | None |
| `PATCH /v0/sandboxes/{sbx}` | Settings, metadata, hooks and scrub paths | `sandboxes.update`; hooks and scrub also `sandboxes.exec` |
| `DELETE /v0/sandboxes/{sbx}` | Delete; runs `on_stop` first on a running sandbox, unless `?hooks=false` | `sandboxes.delete` |
| `POST /v0/sandboxes/{sbx}/touch` | Count as activity, re-arming the idle timer. **New** ([timers-and-activity](timers-and-activity.md#api)) | Any of `sandboxes.exec`, `sandboxes.ports.view`, `sandboxes.agents` |
| `POST /v0/sandboxes/{sbx}/operations` | `start`, `stop`, `restart` | `sandboxes.start`, `sandboxes.stop` |
| `GET /v0/sandboxes/{sbx}/operations` | List its operations, newest first. **New** | None |
| `GET /v0/sandboxes/{sbx}/operations/{op}` | Get one; `?wait=<seconds>` long-polls up to 60 s until it finishes | None |

Admin keys use the same endpoints, reaching every sandbox in their project. There's no separate `/v0/projects/{prj}/sandboxes`; the dashboard calls these with its session ([api](../api.md)).

### Errors

| Status | Code | When |
|---|---|---|
| `402` | `credits_exhausted` | Create, start or restart with no credits (when the `credits` plugin is on) |
| `403` | `permission_denied` | Missing permission, named in the message |
| `403` | `wrong_key_kind` | A sandbox key setting `hooks` or `scrub` |
| `404` | `not_found` | No sandbox this key can see |
| `409` | `operation_in_progress` | Another operation is in flight; carries `operation` |
| `409` | `invalid_state` | Wrong status for the request |
| `409` | `cluster_suspended` | Create or start on a suspended cluster; carries `cluster` |
| `422` | `validation_failed` | Field errors in `errors` |
| `422` | `cluster_required` | No cluster could be chosen |
| `422` | `template_unavailable` | Carries `template: {id, cluster, reason}` |
| `422` | `persistence_unsupported` | Carries `cluster`, and `isolation` when it's the level that can't snapshot |
| `422` | `isolation_unavailable` | Carries `isolation`, `cluster`, `reason` and `available` ([clusters/runtimes](../clusters/runtimes.md#api)) |
| `422` | `idempotency_key_reused` | |
| `429` | `limit_exceeded` | Carries `limit: {kind, value, period}` |
| `502` | `create_failed` | A waited-for create failed; message names the sandbox |
| `502` | `hook_failed` | A waited-for create whose `required` hook failed; the sandbox is `running` ([hooks](hooks.md#failures)) |
| `503` | `cluster_capacity_exceeded` | A waited-for create found no room in 2 minutes |
| `503` | `cluster_unavailable` | A timer change saved but not applied |

## Security

- `env` is encrypted at rest and never returned, logged or put in activity. It reaches AerolVM in the create call only, over TLS to the node (or through the tunnel; [clusters/connectivity](../clusters/connectivity.md)).
- `metadata` isn't secret, and is documented as such.
- A profile key can never name `profile`; an admin key can only name its own project's profiles.
- Image references are passed to AerolVM as given. Private registries aren't supported (later: registry credentials on a profile or project).

## Observability

- Activity:
  - `operation` events for each operation, with `requested_by` and `reason`;
  - `sandbox.stopped` for stops Lewbox didn't ask for;
  - `request.refused` for refusals, with code.
- Webhooks: `sandbox.<status>` and `operation.{started,succeeded,failed}` ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).
- Metrics:
  - `lewbox_sandboxes{status,cluster}` (a gauge, from the leader);
  - `lewbox_sandbox_transitions_total{from,to}`;
  - `lewbox_sandbox_create_wait_seconds` (histogram), with outcome `201` or `202`.
- Logs: every transition at `info`, with `sandbox`, `profile`, `project`, `from`, `to` and `operation`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_CREATE_WAIT` | `60s` | How long a waiting create waits |
| `LEWBOX_SANDBOX_DEFAULT_CPU` | `2` | |
| `LEWBOX_SANDBOX_DEFAULT_MEMORY_MB` | `4096` | |
| `LEWBOX_SANDBOX_DEFAULT_DISK_GB` | `20` | |
| `LEWBOX_SANDBOX_MAX_REPOSITORIES` | `20` | |
| `LEWBOX_OPERATION_WAIT_MAX` | `60s` | Longest `?wait=` |

## Testing

- **Unit:** the transition table, run in both directions over every pair of statuses.
- **Property test:** random sequences of requests and reconciler reports never leave a disallowed transition or a gap in `status_sequence`.
- **Create:**
  - each refusal, in its order of precedence: idempotency before limits, suspended cluster before limits, and so on;
  - fingerprint canonicalisation (field order, nulls, `false`);
  - two concurrent creates at `running_sandboxes - 1`: exactly one succeeds.
- **Waiting:** `201` within the wait; `202` after it (with the operation held by the fake AerolVM); `502` and `503` for a failure while waiting; a replay waiting for the original.
- **End to end** against the fake AerolVM ([foundation/testing](../foundation/testing.md)) and in a nightly live-cluster run: create → stop → start → restart → delete, for `persist` both ways.

## Build plan

1. Migration: `sandbox_status`, `sandboxes`, indexes.
2. `internal/sandboxes`:
   - `Transition` and its table;
   - `Create` (with idempotency, cluster, build, size, limits);
   - `RequestOperation`, `Delete`, `UpdateSettings`;
   - queries with `sqlc`.
3. Handlers and OpenAPI for every endpoint above; long-poll on operations through the notifier ([api/streaming](../api/streaming.md)).
4. Events and webhooks from `Transition` (stubs until M4 delivers them).
5. Fake-AerolVM end-to-end tests; the live-cluster smoke test.

## Open questions

1. **Drop `PATCH status`?** Proposed: yes. Start, stop and restart only through `POST …/operations`, which answers with the operation. If kept, it's an alias.
2. **Admin keys creating sandboxes.** Proposed: yes, naming `profile`. That lets Lewagent's backend use one key. But Lewagent's browser still needs sandbox keys, and Lewagent today keeps a profile key per user; should it keep doing so? ([migration/lewagent](../migration/lewagent.md))
3. **`ports` on create.** New, to save Lewagent a call per port after create. Worth it, or leave ports to templates and the ports API?
4. **Resizing** a stopped persisted sandbox (`cpu`, `memory_mb`; `disk_gb` only up): the next start would use the new size. Cheap to add once offload exists. Later?
5. **Image registries with credentials** (private `image`s). Later, as a project or profile setting.
