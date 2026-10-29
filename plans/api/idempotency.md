# Idempotency

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

A create that times out may still have worked, and retrying it blindly could make a second sandbox, a second conversation turn, or log an event twice. Requests that create things take an `Idempotency-Key` header; a retry with the same key answers with what the first request made, instead of making something new. The SDKs send one with every create, which is what makes it safe for them to retry.

## Today

- The gateway accepts `Idempotency-Key` on `POST /v1/sandboxes`, conversations and their messages, and `POST /v1/sandboxes/{sbx}/activity`; the app stores keys (`sandboxes.idempotency_key` and a fingerprint column, the same on messages and events) and checks them while reserving under the profile's row lock (`app/Actions/Sandboxes/ReserveSandbox.php`).
- Replays answer the resource as it is now, with `Idempotent-Replayed: true`; a different request with the same key is `422 idempotency_key_reused`. Keys last 24 hours. Documented at [lewbox.com/docs/basics/idempotency](https://lewbox.com/docs/basics/idempotency).
- Each resource kind stores its own key columns, so each create re-implements the check.
- The Admin API has none: creating a profile from a customer sign-up webhook that's retried makes two profiles unless the tenant checks `external_id` first (Lewagent does: `findByExternalId` then `create`).
- Lewagent uses the computer's own id as the sandbox's idempotency key, a good pattern the docs should show.

## Goals

- One mechanism, one table, for every endpoint that takes a key.
- Exactly one resource per key, even with concurrent retries.
- Replays are cheap and answer the resource's current state, as documented.
- Admin API creates are idempotent too.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The header name, 1–255 printable ASCII; 24 hours; `Idempotent-Replayed: true`; `422 idempotency_key_reused`; replays answer current state; refused requests make nothing and aren't remembered |
| **Change** | One `idempotency_keys` table instead of columns on each resource |
| **Change** | Keys belong to the profile *and* credential kind's scope (below), not "the profile", since admin keys and sessions create too |
| **Add** | Idempotency on profile, template, webhook endpoint, cluster and installation creates, and on prompts |
| **Add** | `409 idempotency_key_in_use` when the first request is still running after 30 s |

---

## Design

### Which endpoints

| Endpoint | M |
|---|---|
| `POST /v0/sandboxes` | M1 |
| `POST /v0/sandboxes/{sbx}/conversations` | M3 |
| `POST /v0/conversations/{cnv}/messages` | M3 |
| `POST /v0/sandboxes/{sbx}/activity` | M4 |
| `POST /v0/templates`, `POST /v0/templates/{tpl}/builds` | M1 |
| `POST /v0/projects/{prj}/profiles` | M1 |
| `POST /v0/projects/{prj}/clusters`, `POST …/clusters/{clu}/installations` | M1 |
| `POST /v0/projects/{prj}/webhook-endpoints` | M4 |

Not idempotent, deliberately:

- **Endpoints whose answer is a secret shown once** (profile and admin keys, sandbox keys, setup links): a replay would have to store the secret to show it again. Retrying one makes another key; the client revokes the extra. (Webhook endpoints are the exception: their secret is stored encrypted anyway, since we sign with it, so a replay can show it.)
- **Commands**: a command is an action, not a resource, and a retried `git push` must be the client's decision. Clients that need it run commands as sessions with a known name.
- `PUT` and `DELETE`, which are idempotent by nature, and `PATCH`, which converges.
- **Credit transactions** use their own `reference` field, unique per profile, which the project's own billing system already has ([credits](../credits.md)).

Route metadata marks an endpoint `idempotent: true`, which wires the middleware and documents the header in the OpenAPI document.

### Scope

A key is unique within its **scope**, so two customers' keys never collide and one credential can't replay another's request:

| Principal | Scope |
|---|---|
| Profile key, sandbox key | The profile |
| Admin key, session | The project |
| Setup session | Not applicable (its endpoints aren't idempotent) |

Within the scope, the key is bound to the **credential** that first sent it (the `key_` id, or the user for sessions). Another credential of the same profile or project sending it: `422 idempotency_key_reused`, as documented ("by another of the profile's keys").

### The fingerprint

`SHA-256(method + "\n" + route pattern + "\n" + path parameters + "\n" + canonical JSON body)`.

- **Canonical JSON**: parsed and re-serialised with sorted keys and no insignificant whitespace, so a client that reorders fields still matches. Defaults are not filled in: `{}` and `{"wait": true}` are different requests (a client changing its body is suspicious enough to refuse).
- **Headers that change meaning** are included: none today; `X-Lewbox-Label` isn't (a label doesn't change what's made).
- Bodies over 1 MiB (prompts with images) are hashed streaming; the fingerprint is all that's kept.

### Storage

```sql
idempotency_keys (
  id bigint primary key,
  scope_kind text not null,          -- profile | project
  scope_id bigint not null,
  key text not null,                 -- the client's key
  credential_id text not null,       -- key_… or usr_…
  fingerprint bytea not null,
  state text not null,               -- in_progress | completed
  resource_type text,                -- sandbox | message | event | profile | …
  resource_id bigint,
  response_status int,               -- the first response's status, for replays that answer it
  created_at timestamptz not null,
  expires_at timestamptz not null,   -- created_at + 24h
  unique (scope_kind, scope_id, key)
)
```

Pruned hourly (`DELETE … WHERE expires_at < now()`, in batches) by a periodic job ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)).

### The flow

In the same transaction that creates the resource:

1. `INSERT INTO idempotency_keys (…, state = 'in_progress') ON CONFLICT DO NOTHING RETURNING id`.
2. **Inserted**: carry on. Create the resource; set `state = 'completed'`, `resource_type`, `resource_id`, `response_status`; commit. The resource and its key are committed together, so there is never a resource without its key or a completed key without its resource.
3. **Not inserted**: read the existing row `FOR SHARE`, which waits if the first transaction is still open.
   - Different credential or fingerprint: `422 idempotency_key_reused`.
   - `completed`: **replay**: load the resource as it is now and answer it, with `Idempotent-Replayed: true` and the status the endpoint's replay rule gives (below).
4. **Refusals aren't kept.** If the first request is refused (`422`, `429`, `402`, `409`), its transaction rolls back, taking the key row with it, so a retry is checked afresh, as documented.

**Long creates.** A sandbox create's transaction only *reserves* the sandbox and its `create` operation; the slow work runs as a job ([sandboxes](../sandboxes.md)). So the key's transaction is short, and the request then *waits on the operation*, not on the transaction. A retry during that wait finds a `completed` key pointing at a sandbox still `creating`, and waits on the same operation, as documented.

### Races

Two requests with the same key at the same moment: the unique index serialises them. The second blocks on the first's row until it commits (milliseconds, see above), then replays. If the first request is still in a transaction after **30 s** (it shouldn't be), the second gives up with `409 idempotency_key_in_use` and `Retry-After: 1`. A crashed request's transaction is rolled back by Postgres, freeing the key.

### Replay answers

| Endpoint | First answered | Replay answers |
|---|---|---|
| `POST /v0/sandboxes` | `201`/`202`/`502`/`503` | As [documented](https://lewbox.com/docs/basics/idempotency): `202` if its create hasn't finished (after waiting as a create does), `201` if it's running, `502 create_failed` if it failed, `200` if it has moved on (stopped, deleted) |
| Conversations, messages | `201` | `201` with the conversation or message as it is now |
| Activity events | `201` | `200` with the event as logged the first time |
| Everything else | `201` | `200` with the resource as it is now; `404` if it has since been deleted |

### In the SDKs and CLI

- Every create sends an `Idempotency-Key`, a new UUIDv7 per call, reused for that call's own retries ([sdks-and-cli](../sdks-and-cli.md)).
- Callers can pass their own, which is the only way to make a create idempotent across process restarts (a queued job retried after a crash). The docs recommend an id from the caller's own system (`job-8812-sandbox`, or as Lewagent does, its own record's id).
- The CLI takes `--idempotency-key`.

---

## Observability

- Metrics: `lewbox_idempotency_total{endpoint,result=new|replayed|reused|in_use}`.
- Replays are logged with the original request's resource id.
- Activity: a replayed sandbox create doesn't log a second `operation` event.

## Testing

- Same key, same body, sequential: one resource, second answer replayed.
- Same key, concurrent (50 goroutines): one resource; all answers agree.
- Same key, different body; same key, other credential: `422`.
- First refused, retry succeeds.
- Reordered JSON keys replay; changed values don't.
- Expiry: after 24 h (clock injected) the key is forgotten.
- Replay statuses for each sandbox state.

## Build plan

`internal/httpapi/idempotency`: middleware that reads and validates the header, computes the fingerprint, and puts a `Claim` in the context; a `Claim.Begin(tx)` / `Claim.Complete(tx, resource)` API the services call inside their creating transaction; replay loaders registered per resource type; the pruning job.

## Open questions

1. Should `GET`-after-`POST` races be documented (a client that replays immediately may see a resource still `creating`)? (It's the documented behaviour; just make sure the examples show it.)
2. Do we keep refusals for a short time (say 60 s) so a client hammering a `429` with the same key gets the same answer cheaply? (Proposed: no; rate limits handle hammering.)
3. 24 hours is short for queued jobs that retry over days. Configurable per project, up to 7 days? (Proposed: Later, if asked.)
