# Audit events, the outbox and listeners

Every meaningful thing that happens is recorded as a row in an `events` table: an **audit trail** of who did what, when, from where. The same rows drive side effects: **listeners** (send an email, post a webhook, update a search index) react to events without the handler knowing they exist. Event and listener naming (`<resource>.<past-tense fact>`, at most 3 snake_case segments) belongs to `naming`.

## Recording

### Same transaction as the change

The data-layer function that makes a change records its event **inside the same transaction**:

```
function updateProject(ctx, tenantID, actor, id, input):
  tx = begin(ctx)
  before = tx.select(projects).where("public_id = ? AND tenant_id = ?", id, tenantID).forUpdate()
  if !before: return NotFound
  after = tx.update(projects).set(input).where("id = ?", before.id).returning("*")
  changes = diff(before, after, ["name", "description", "status", "color"])
  if changes not empty:
    record(tx, ProjectUpdated{project: after.publicID, tenant: tenant.publicID, changes}, scope{actor, tenantID})
  commit(tx)
```

- A change that fails leaves no event. An event that can't be written rolls the change back. There is never a change without its record, or a record of a change that didn't happen.
- `record` also inserts the outbox rows for that event's listeners in the same transaction (below).
- Recording is an ordinary insert: no table locks, no extra round trips beyond the insert.
- **Events with no data change** (a login attempt, a password check, a rate limit crossed) are written on their own, outside any change transaction. If that write fails, the request fails with 500: an attempt that can't be audited doesn't proceed.
- An integration test asserts each endpoint writes **exactly one** event with the right name, scope and payload, and **none** when the request is refused.

### Row columns

| Column | Holds |
| - | - |
| `id`, `public_id` | Internal ID; `evt_` + 14 base62 |
| `name` | `<resource>.<fact>`; a `CHECK` enforces the format (below) |
| `actor_id` | Who acted (the signed-in user, or the user who just signed in/registered). Null for anonymous attempts, system jobs, and once the actor's account is deleted (`ON DELETE SET NULL`) |
| `user_id` | The account the event is **about** (auth events, the member in member events). `ON DELETE CASCADE`: deleting the account deletes events about it |
| `tenant_id` | The tenant for tenant-scoped events. `ON DELETE SET NULL`: history outlives the tenant, readable through the public ID in the payload, until pruned |
| `payload` | The event's typed struct as JSON (`jsonb`) |
| `ip` | The client address, for events recorded during a request; null for CLIs and listeners. The **only** place an IP is kept |
| `request_id` | The server's request ID (`req_...`), linking the event to its log lines and to the `requestId` a user quotes from a 500; empty outside a request |
| `user_agent` | Valid UTF-8, cut to e.g. 256 characters |
| `created_at` | When |

Internal IDs in columns are for scoping and joins; they never appear in a payload. Actor, IP, request ID and user agent come from request middleware (attached to the context), not from each handler.

**The name `CHECK`** mirrors the naming rule in `naming` (two or three snake_case segments, each starting with a letter), so a bad name can't reach the audit trail even through a path that skips the code's own check: `CHECK (name ~ '^[a-z][a-z_]*(\.[a-z][a-z_]*){1,2}$')`. Test the same regex in code: it accepts `project.created` and `workspace.member.removed`, and refuses `Workspace.created`, `workspace..created`, `workspace.created.`, `workspace.1created` and any four-segment name.

**Indexes** follow the queries the log actually serves, each ending in `created_at DESC` because every read is "newest first within a scope": `(user_id, created_at DESC)` for events about an account, `(tenant_id, created_at DESC)` for a tenant's log, `(name, created_at DESC)` for "login attempts for this email in the last day", and a plain `(created_at)` so the retention prune deletes by age without a full scan.

## Payload rules

| Rule | Example |
| - | - |
| Payload keys are `camelCase`, like API responses | `{"otherSessionsRevoked": 2}`; one key style across the API and the log, so a tenant-log projection copies fields without renaming them |
| Records identified by **public ID only** | `{"project": "prj_...", "workspace": "wsp_..."}`, never `projectId: 42` |
| **NEVER** secrets or anything that unlocks something | No passwords (old, new or attempted), hashes, session or CSRF tokens, link tokens, MFA secrets, codes, recovery codes, API keys, or digests of any of these |
| No IP or user agent in the payload | They have columns, which retention can minimise in one place |
| Updates record `changes` per field, and only when something changed | `{"changes": {"name": {"from": "Acme", "to": "Acme Ltd"}}}`; a PATCH that changes nothing writes no event |
| Deletes keep what identifies the deleted thing for humans | `project.deleted`: `project`, `workspace`, `name` |
| One event type per fact; variable detail in the payload | `rate_limit.reached` with `{"limit": "auth:per-ip:block"}`, not one event name per limit |
| Outcome fields are closed enums | `auth.login_attempted` with `result: succeeded / wrong_password / unknown_account / suspended` |
| Personal data only on purpose, and listed in the data map | The email typed on a login attempt (only once it parses as an address), an invited address, an operator's free-text reason |
| Payload structs are typed and checked by a test | A test walks every event struct's field names and types and fails on `password`, `token`, `secret`, `ip`, internal IDs |

Erasure and retention (see `ops`, privacy and compliance, for the policy):

- Deleting an account replaces its email (and the name beside it) with `"[deleted]"` in every event that outlives it.
- **Audit-class events** (auth, admin, security) are kept longer (e.g. a year) than ordinary ones (e.g. 90 days). After a short period (e.g. 30 days) a minimisation job truncates IPs (IPv4 /24, IPv6 /48) and replaces payload emails with keyed pseudonyms.
- Old events are removed by the batched prune ([persistence.md](persistence.md#batched-deletes-cleanup-and-retention)), keeping any with a pending delivery.

### Enumeration-safe recording

Record attempts whether or not the target exists (`auth.login_attempted` for unknown accounts too, `auth.password_reset.requested` for every valid email). If the event write only happened for real accounts, response timing would reveal which emails are registered. The listener, after the response, is the first thing that looks the account up.

## The outbox

Side effects (email, webhooks, search indexing, notifying another service) **never** run inside the request's transaction:

- the commit may still fail after the email went out (an email about a change that never happened);
- a slow SMTP or webhook call holds the transaction and its locks open;
- if the server dies after commit but before the call, the effect is lost.

Instead, `record` inserts one `deliveries` row per listener of that event in the same transaction. The commit makes the change, the event and the "this must happen" promise durable together.

```sql
CREATE TABLE event_deliveries (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id        bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  listener        text   NOT NULL CHECK (char_length(listener) BETWEEN 1 AND 100),
  attempts        int    NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),   -- due time, and the claim lease
  delivered_at    timestamptz,
  failed_at       timestamptz,
  last_error      text                                   -- scrubbed, cut to ~500 characters
);
CREATE INDEX ON event_deliveries (next_attempt_at) WHERE delivered_at IS NULL AND failed_at IS NULL;
```

## The delivery worker

Every server process runs one worker; they share the queue.

1. **Claim** in one statement: take up to N (e.g. 10) due rows with `FOR UPDATE SKIP LOCKED` (other workers skip them instead of waiting), increment `attempts`, and push `next_attempt_at` ahead (e.g. 5 minutes) as a **lease**. The row locks end with that statement.

   ```sql
   UPDATE event_deliveries d SET attempts = attempts + 1, next_attempt_at = now() + interval '5 minutes'
   WHERE d.id IN (
     SELECT id FROM event_deliveries
     WHERE delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()
     ORDER BY next_attempt_at, id LIMIT 10
     FOR UPDATE SKIP LOCKED)
   RETURNING d.*;
   ```

2. **Run** the batch's listeners concurrently, **with no lock or transaction held**, each with a timeout (e.g. 30 s). A panic/exception counts as a failure and is reported.
3. **Record:** success sets `delivered_at`. Failure stores a scrubbed `last_error` and reschedules with backoff (e.g. 1 s, 10 s, 1 min, 10 min, 1 h); after the last attempt it sets `failed_at` and reports an error once. One listener failing never affects another listener of the same event.
4. **Poll:** an empty claim waits (e.g. 1 s); a full batch is followed immediately by the next. Polling alone is correct; a push wake-up (e.g. Postgres `NOTIFY` after commit) only cuts latency, so add it when a second of delay matters, never as the only trigger (a notification sent while no worker is listening is simply lost).
5. **Shutdown:** stop claiming, finish the current batch within the grace period. A delivery cut off by a crash is claimed again when its lease expires.

Measure deliveries by listener and outcome, and the lag of first attempts; alert on overdue and failed deliveries (see `ops`). Operators can requeue failed deliveries.

## Listeners

```
listen(bus, "mail.password_reset", (ctx, e: PasswordResetRequested, d: Delivery) => {
  if data.eventExists(ctx, "auth.password_reset.sent", request = d.eventID): return ok   // already done: idempotent
  user = data.userByEmail(ctx, e.email)
  if !user: return ok                                              // nothing to do; the 202 was already sent
  token = data.issuePasswordReset(ctx, user.id)                   // stores the digest
  mailer.send(ctx, resetEmail(user, token))
  data.record(ctx, PasswordResetSent{user: user.publicID, request: d.eventID})
})
```

| Rule | Why |
| - | - |
| **Idempotent, keyed on the event ID** | Delivery is **at least once**: a server can stop after the listener ran but before it was marked delivered. "Email for `evt_...` already sent" must be checkable (a unique `(event_id, listener)` record, or an idempotency key passed to the provider) |
| **No ordering assumptions** | Retries and concurrent workers mean a later event can arrive before an earlier one. Re-read current state instead of trusting the payload when it matters |
| **Respect the context/timeout** | A listener that ignores cancellation can't be stopped and blocks shutdown |
| **Typed** | The listener receives its own event type, decoded from the payload; registering for one event can't receive another |
| **Named, and the name is stable** | The name is stored on each delivery. Renaming a listener makes it a new one; deliveries queued for the old name fail as "no listener". Listener names are independent of event names: **renaming an event never renames its listeners** (e.g. `mail.password_reset` keeps its name when the event becomes `auth.password_reset.requested`) |
| **Registered in one place** (server startup) | One search shows everything that reacts to an event |
| **Re-check permanent refusals** | A blocked address or a deleted account makes the listener succeed with nothing sent (no retry), and record no "sent" event |
| **A listener's own events** are recorded without request metadata (no IP, no request ID) and link back with `request: <evt_ id of the triggering event>` | The audit trail shows cause and effect |
| Errors are scrubbed before storage or logging | SMTP refusals echo recipient addresses; webhook errors echo URLs |

## Exposing the log to tenants

If tenant admins can read their own audit log, it is a narrow window onto the events table, never the table itself:

- **Scope by the column**, `WHERE tenant_id = ?` from the tenant middleware, never by a payload field (anyone could name another tenant in a payload).
- **An allow-list with a projection:** each event shown lists the payload fields it copies to the response. No IP, user agent, internal ID, operator flag or raw payload is ever returned.
- **A new event is hidden until classified:** a test fails for any catalogue event that is neither in the allow-list nor explicitly hidden with a reason.
- **Cursor pages** with the cursor validated against this scope ([pagination.md](pagination.md#cursor-pages-log-tables-only)); one event of another tenant, a hidden one and a missing one are the same 404.
- Filters by type expand to `name IN (...)` from a fixed group map, never `LIKE` on input. Search escapes wildcards.
- Names of people come from `actor_id`/`user_id` joins, shown only for people who are or were in this tenant, so a filter can't be used to look up any account.
- Reading the log records no event and uses the ordinary read budget.

## Adding an event

1. A typed struct with JSON tags and its name, added to the one event catalogue. Public IDs only; no secrets.
2. Record it in the data-layer function that makes the change, inside its transaction, with its scope (actor, user, tenant). Use the standalone record only when no data changes.
3. Classify it for any tenant-visible log (shown with fields, or hidden with a reason) and its retention class.
4. An integration test: exactly one event with the right name, scope and payload on success; none on refusal.
5. Add it to the documented catalogue.

## Adding a listener

1. Register it at startup under a stable, unique name.
2. Make it idempotent on the event ID; give it no ordering assumptions; honour the timeout.
3. Treat permanent refusals as success with nothing done.
4. Test: it runs once, runs again harmlessly, and records its own outcome event if it has one.
