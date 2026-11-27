---
name: api
description: How HTTP JSON APIs and their persistence are designed and built in any backend stack - request flow and middleware order, response and error shape, status codes, strict input, public IDs, tenant scoping, permissions in responses, list/pagination/search/sort, SQL safety, transactions and locks, migrations, factories and seeders, audit events and the outbox. Use when adding or changing an endpoint, a list, a table, a migration, a factory or seeder, an event or a listener, or when reviewing API or data-layer code.
---

# API design

The API is the only gate between untrusted input and the data. Every rule below exists because breaking it has caused a data leak, an enumeration oracle, a deadlock, a lost write or a migration outage somewhere. Follow them literally; where a rule says **Prefer**, deviate only with a written reason.

Names of routes, handlers, permissions, events and rate limits belong to `naming`. Auth, CSRF and origin checks belong to `security`; rate limits, quotas and CAPTCHA belong to `ops`. Logging, metrics and timeouts at the server level belong to `ops`. Test layout belongs to `testing`.

## When to use

- Adding or changing an endpoint, a middleware, or a response shape.
- Adding a list, a filter, a search box or a sort option.
- Adding a table, a column, an index or a migration.
- Writing a query, a transaction, a lock, a quota or a cleanup job.
- Adding a factory, a seeder, an audit event or a listener (email, webhook, indexing).
- Reviewing any of the above.

## References (read when the task touches them)

- [references/status-codes.md](references/status-codes.md) - the full status table, error bodies, 404-vs-403, 409-vs-422, when 202. Read before choosing any non-200 answer.
- [references/input-validation.md](references/input-validation.md) - body decoding, query allow-lists, the shared text validator, enums, public ID parsing, tokens. Read when writing a handler that takes input.
- [references/pagination.md](references/pagination.md) - the list contract, numbered and cursor pages, search escaping, sort maps, worked example. Read when adding or changing any list.
- [references/persistence.md](references/persistence.md) - query safety, deadlines, UTC, transactions, lock order, quotas, deletion, batched cleanup, public ID generation. Read before writing data-layer code.
- [references/migrations.md](references/migrations.md) - edit-in-place vs append-only, running migrations, expand/contract, the full new-table checklist. Read before touching the schema.
- [references/factories-and-seeding.md](references/factories-and-seeding.md) - factory API, safe fake data, reproducible seeds, seeder rules. Read when adding a table or test data.
- [references/events-and-listeners.md](references/events-and-listeners.md) - audit rows, payload rules, the outbox, the delivery worker, idempotent listeners, exposing a log to tenants. Read when a change must be recorded or must trigger a side effect.

## 1. Request flow

Every request passes the same pipeline, cheapest and most general rejection first. **MUST** keep this order; each step relies on the one before.

```
1  Request context   client IP (trusted-proxy rules), a server-generated request ID (X-Request-ID on every response)
2  Security          security headers, Cache-Control: no-store; request deadline (e.g. 10 s);
                     on mutations: exact Origin, Content-Type: application/json (else 415)
3  Panic recovery    any panic -> the generic 500 with requestId
4  Router            method + path; unknown path -> JSON 404; known path, wrong method -> 405
5  Authentication    session cookie or bearer credential -> user (else 401); CSRF on cookie mutations (403)
6  Tenant            {tenant} in the path -> the caller's membership row -> tenant + role (else 404)
7  Permission        role grants <resource>.<action>? (else 403)
8  Limits            per-user budget and per-feature limit (429 / 503); anonymous routes: per-IP limit right after routing
9  Handler           allow-list query params / decode body -> validate (400 / 422) -> call the data layer
10 Data layer        query scoped by the tenant's internal ID, inside the request deadline; events in the same transaction
11 Respond           JSON in the fixed shape
```

- **Tenant before permission.** A non-member must get 404 (the tenant "doesn't exist" for them); only a member learns that something exists and they lack the right (403). Checking permission first leaks existence.
- **Handlers never build queries**; the data layer never reads the request. The handler passes typed, validated values plus the tenant's internal ID.
- **ALWAYS** pass the request's context/deadline into every data-layer call so a cancelled or timed-out request stops its queries.
- **Middleware is the only place** that puts the user, the tenant and the role on the request, through keys handlers cannot forge (e.g. unexported context keys).
- **Unknown `/api/*` paths are the API's JSON 404**, never the web app's HTML shell; a client that typos a path must get a parseable error.

## 2. Response and error shape

- Responses are JSON, field names `camelCase`, timestamps RFC 3339 in UTC (`2026-10-30T12:00:00Z`), the record's public ID as `"id"`.
- **Errors are always** `{"error": "<human sentence>"}`, plus `"field": "<bodyKey>"` when exactly one input field is at fault:

  ```json
  {"error": "Name must be 1-100 characters.", "field": "name"}
  ```

  `field` is the exact JSON key the client sent, so the form can attach the message to its input.
- **A route may add documented, machine-readable extras** (`{"error": "...", "suspended": true}`, a list of the blocking records) - never free-form debug data.
- **A 500 answers only** `{"error": "Something went wrong. Please try again.", "requestId": "req_..."}`. The cause goes to the log under that ID. **NEVER** put an exception message, SQL, stack or upstream response in a body: it leaks schema and internals, and attackers mine it.
- The request ID is always the server's own, never echoed from the client or a proxy (a client-chosen ID could collide with or impersonate another request in the logs).
- Success: `store` 201 with the record, `update` 200 with the record, `destroy` 204 with no body, `show` 200 with the record, `index` 200 with the list envelope (section 7).
- **Prefer** a PATCH body carrying the whole editable representation (all editable fields): one validator for create and update, no ambiguity between "absent" and "null".
- Lists inside a record are `[]` when empty, **NEVER** `null`. Clients then never need a null check before `.length` or a loop.

## 3. Status codes: one fixed meaning each

| Code | Means only |
| - | - |
| 200 / 201 / 204 | Read or update / created (with the record) / done, nothing to return |
| 202 | Accepted: the work happens later (an email from a listener), or the answer must be identical whatever exists (a reset request for any valid email) |
| 400 | The request can't be read: malformed JSON, unknown or mistyped field, oversized body, unknown/repeated/malformed query parameter, a cursor that isn't one of this list's rows |
| 401 | Not authenticated: no or expired session, wrong sign-in credentials |
| 403 | Authenticated and the target is visible, but not allowed: missing permission, bad Origin/CSRF, suspended account |
| 404 | Not found, not yours, or a malformed ID - one identical answer |
| 405 | Known path, wrong method |
| 409 | Input is fine but the current state forbids it (last owner leaving, already enabled, already taken) |
| 415 | A mutation without `Content-Type: application/json` (a body-less DELETE is exempt) |
| 422 | Parsed but invalid: a rule on a value (`field` set), or a quota reached (no `field`) |
| 429 | This caller is rate limited; `Retry-After` in seconds |
| 500 | Our fault; generic message + `requestId` |
| 503 | A dependency is down, a limit shared by everyone is reached, a feature is switched off, or the server is draining; `Retry-After` |

- **NEVER** reuse a code for a different meaning (e.g. 401 for "wrong current password" while signed in - the client would treat the session as dead and sign the user out; that is 422 with `field: "currentPassword"`).
- Full decision rules, edge cases and body examples: [references/status-codes.md](references/status-codes.md).

## 4. The same-answer 404

**MUST** answer a missing record, another tenant's record and a malformed ID with the **same status, body and headers**: `404 {"error": "Not found."}`.

- Different answers ("forbidden" vs "not found") let anyone probe which IDs exist in other tenants.
- Scope the lookup itself (`WHERE public_id = ? AND tenant_id = ?`), so "not yours" is literally "no row". **NEVER** fetch by ID and then compare the owner in code - one forgotten comparison is a cross-tenant leak.
- A record of another tenant **you also belong to** is still 404 under this tenant's path.

## 5. Strict input

- **Bodies:** cap the size (**Prefer** 16 KiB; raise per route only with a reason), require exactly one JSON object (no trailing data, no array), **reject unknown fields** (400). Unknown fields are how mass-assignment bugs start (`"role": "owner"`, `"tenantId": ...`), and rejecting them catches client typos instead of silently ignoring them.
- **Query parameters:** each handler names the parameters it accepts; anything else, a repeat, or a malformed value is 400.
- **Nothing is coerced.** `"2"` is not `2`, `+2`, ` 2` and `1e3` are not page numbers, `"true"` is not `true`. Coercion hides client bugs and creates parser differentials.
- **One shared text validator** for every human-typed string: trim, length in characters (not bytes), valid UTF-8, no control characters, no invisible formatting characters (bidi overrides, zero-width), line breaks and tabs only in multi-line fields. A new field can't forget a rule it gets for free.
- **Enums** are closed sets checked server-side (`status` in `Planned | In progress | Completed`); unknown is 422 with `field`.
- **The tenant comes from the path**, never the body; the server never accepts an ID it can derive.
- **Tokens (invitation, reset, verification links) go in the JSON body, NEVER the URL path or query.** URLs end up in access logs, proxy logs, browser history, analytics and `Referer` headers. A link token is checked by a named action: `POST /api/invitations/check {"token": "..."}`.
- Details and the validator's exact rules: [references/input-validation.md](references/input-validation.md).

## 6. Public IDs, tenancy and capabilities

### Public IDs

- Every record a user can open, link or search has a **public ID**: a type prefix + `_` + 14 base62 characters (`prj_j292s2S2j7Qx4B`, about 83 random bits). Internal numeric IDs stay inside the server (joins, foreign keys, sort tie-breaks) and **NEVER** appear in a request or response: sequential IDs reveal volume and invite guessing.
- **The database generates it** (a column default calling a `public_id('prj')` function), with a `CHECK` on the format. Application code never generates IDs, so every insert path (API, factories, seeders, manual SQL) gets one the same way. Inserts read it back (`RETURNING`).
- **Shape is checked before any query**: wrong prefix or shape is the 404 from section 4 without touching the database.
- Prefixes are 3 lowercase letters, unique per type; the catalogue lives with `naming`.

### Tenant scoping through membership

- A tenant (workspace, organisation, team) is read **only** through the caller's membership row: `JOIN memberships m ON m.tenant_id = t.id AND m.user_id = ?`. No membership, no row, 404.
- Child data is scoped by the tenant's **internal** ID handed down by the tenant middleware: `WHERE p.public_id = ? AND p.tenant_id = ?`.
- Cross-tenant reads (an operator dashboard) are a separate, explicitly named set of queries reachable only behind operator authentication. **NEVER** add an "unscoped" flag to a normal query.

### Permissions and capabilities in responses

- Permissions are `<resource>.<action>` (see `naming`), granted by role in one server-side table.
- Every tenant response (the list included) carries what the caller may do, computed from the role at no extra query cost:

  ```json
  {"id": "wsp_2yEHYLwiwPVhLW", "name": "Acme", "role": "admin",
   "permissions": ["project.destroy", "project.store", "project.update"],
   "assignableRoles": ["member", "viewer"], "canLeave": true}
  ```

- `permissions` sorted, lists `[]` for none. A derived capability that needs data (`canLeave`: false for the only owner) is read **in the same query** as the role.
- The UI shows, hides and disables from these fields and never re-implements role logic. The server still enforces every permission on every request; capabilities are a hint, not a gate.

## 7. Lists: pagination, search, sort

Lists are paged, filtered and sorted **in the API, never in the browser**.

```json
{"data": [{"id": "prj_j292s2S2j7Qx4B", "name": "..."}],
 "pages": {"current": 2, "total": 7, "perPage": 20, "items": 131}}
```

- `data` is always an array, **NEVER** `null`. `total` is at least 1 (an empty list is "page 1 of 1").
- A page past the end is `200` with `data: []` and the real totals (the client can jump back), not 404.
- Params: `page` (1-10 000, default 1), `perPage` (1-100, default 20), `q` (trimmed, at most 100 characters, case-insensitive), filters as enums, `sort` from an allowlist with `-` for descending (`-updated`). Plain digits only.
- **Sort keys map to fixed SQL**; no column name ever comes from input. **Every order ends with the internal ID** as a tie-break, or rows with equal values repeat or vanish between pages.
- **Search escapes `%`, `_` and `\`** before binding the `LIKE`/`ILIKE` pattern, so `50%` matches literally and `%` can't force a full scan of everything.
- **Scope before count:** one base query with the tenant `WHERE`, used for both the count and the rows. The pager adds only `LIMIT`/`OFFSET`, never a `WHERE`.
- **Log tables use cursor pages** (`{data, next}`, `before=<last id>`, no totals): counting millions of rows is the slow part and nobody pages to the middle of an audit log.
- Full contract, cursor rules and a worked example: [references/pagination.md](references/pagination.md).

## 8. Persistence

- **Query builder or placeholders only.** Values are always bound parameters; SQL text is never concatenated from input. Dynamic parts (sort, enum order) come from fixed maps in code. All SQL lives in the data layer.
- **Request deadlines:** every query runs under the request's context (e.g. 10 s). Background jobs set their own deadline.
- **UTC everywhere:** pin each database session's time zone to UTC; store `timestamptz`; write fixed durations in hours (`interval '720 hours'`), never days, in `CHECK`s and migrations (a "day" is 23 or 25 hours across DST in a non-UTC session).
- **Transactions:** a change, its checks and its audit event commit together or not at all. Re-check business rules (last owner, typed confirmation name) **under the lock**, not before it.
- **Consistent lock order:** always lock in one global order (e.g. user row, then tenants by ascending ID, then children). Two transactions locking the same rows in different orders deadlock.
- **Quotas under a lock:** lock the parent row, count, insert, in one transaction. Count-then-insert without the lock lets parallel requests all pass the check.
- **Batched deletes** for cleanup: a fixed cutoff, batches of a few thousand by ID, `FOR UPDATE SKIP LOCKED`, preview unless confirmed. One giant `DELETE` holds locks and bloats the log.
- Details and SQL: [references/persistence.md](references/persistence.md).

## 9. Migrations

- **Before any real data exists:** change a table by **editing its create migration** and rebuilding the local database (a guarded "fresh" command, refused in production). The schema stays readable, one file per table.
- **Once a database holds data that must be kept:** migrations are **append-only**. **NEVER** edit a migration that has run anywhere that matters; it won't re-run there and environments silently diverge.
- **NEVER auto-migrate on server start.** Migrating is an explicit, locked deploy step; with several instances starting at once, startup migrations race and a failed one takes every instance down.
- **Expand/contract** for every breaking change once live: add, dual-write, backfill, switch reads, stop writing, drop in a later release. Never rename or drop a column in the same release as the code that stops using it.
- Up and down pairs, each in a transaction (except statements the database forbids in one, e.g. concurrent index builds).
- Details: [references/migrations.md](references/migrations.md).

## 10. The new-table checklist

A new table is not done until every line holds (full version in [references/migrations.md](references/migrations.md#the-new-table-checklist)):

1. Create migration: bigint internal `id`; `public_id` with DB default + format `CHECK` if users see its records; `tenant_id ... NOT NULL REFERENCES tenants(id) ON DELETE CASCADE` for tenant data; `CHECK`s mirroring validation; `created_at`/`updated_at timestamptz NOT NULL DEFAULT now()`; an index for the main query, leading with `tenant_id`.
2. Model: public ID read back from the DB, internal ID never serialised.
3. Queries taking the tenant's internal ID; lists through the shared pager.
4. A factory (a test fails without one) and seeder updates.
5. Audit events for its changes.
6. A data-map entry: what personal data it holds, why, how long, who sees it, and its retention rule (cleanup job, prune target, or deleted with its owner). See `ops` (privacy and compliance).
7. Integration tests including another tenant's access (404) - see `testing`.

## 11. Factories and seeders

- **Every table has a factory**, enforced by a test that lists the schema's tables. Each factory's output is inserted into a freshly migrated schema in tests, so a new required column breaks the build, not a teammate's afternoon.
- **Factory output passes API validation** (a test runs the API's validators over it). Fake data the API would reject hides bugs in screens and tests.
- **Reproducible randomness:** values are random per run; the seed is logged (by a failing test, by every seeder) and an environment variable (e.g. `FAKER_SEED=N`) replays it.
- **Safe defaults:** emails at `example.test`, IPs from documentation ranges, credentials revoked/unusable by default, no admins, no fake errors or metrics. A seeded database must never send real mail or grant real access.
- **Seeders:** one transaction each, idempotent where they create named demo data (skip if present), never delete or truncate, bounded counts, and **refused in production before connecting**. The server binary does not include factories or seeders.
- Details: [references/factories-and-seeding.md](references/factories-and-seeding.md).

## 12. Audit events and side effects

- **Every state change records an event in the same transaction** as the change: a failed change leaves no event; an event that can't be written rolls the change back. Events with no data change (a login attempt, a rate limit crossed) are written on their own; if that write fails, the request fails.
- **Payload rules:** public IDs only (never internal IDs); **NEVER** passwords, hashes, tokens, codes, secrets or their digests; the client IP lives in a column, never the payload; updates record `changes: {field: {from, to}}` and only when something actually changed; one event type per fact, variable detail in the payload. Any personal data is there on purpose and listed in the data map.
- **Side effects go through an outbox:** the same transaction inserts one delivery row per listener; a worker claims due rows (`FOR UPDATE SKIP LOCKED`, a lease), runs listeners **outside** any transaction with a timeout, and retries with backoff. **NEVER** send email or call a webhook inside the request's transaction (the commit may still fail, or the call may hang the transaction).
- **Listeners are idempotent:** delivery is at least once, unordered. Key every side effect on the event's ID.
- Naming of events and listeners: `naming`. Details: [references/events-and-listeners.md](references/events-and-listeners.md).

## Pre-merge checklist

Endpoint
- [ ] Route, handler, permission and event names follow `naming`; the route is in the one route registry.
- [ ] Middleware order: auth -> tenant (404) -> permission (403) -> limits -> handler. Creating rows has a per-user limit; anything unbounded has a quota.
- [ ] Body capped, unknown fields 400, query params allow-listed, nothing coerced; text through the shared validator; enums closed.
- [ ] No token, secret or personal data in a path or query string.
- [ ] Path IDs are public IDs, shape-checked before any query; missing / not yours / malformed give one identical 404.
- [ ] Errors are `{error, field?}`; 500 carries only the generic message and `requestId`; every status code matches its single meaning.
- [ ] Tenant responses carry `permissions` and capabilities; every list field is `[]`, never `null`.

Lists
- [ ] `{data, pages}` envelope (or `{data, next}` for a log table); page past the end is 200 with `[]`.
- [ ] Sort from a fixed map, ending in the internal ID; search wildcards escaped; scope applied before count.

Data
- [ ] All SQL in the data layer, bound parameters only, under the request deadline.
- [ ] Read-check-write runs in one transaction under a lock taken in the global order; quotas counted under the lock.
- [ ] Times in UTC; fixed durations in hours.
- [ ] Migration edited in place (pre-data) or appended (post-data), with expand/contract for breaking changes; no auto-migrate.
- [ ] New table: every line of the new-table checklist, factory included; model, factory and seeders updated in the same change as the migration.

Events
- [ ] Each change writes exactly one event in its transaction, none when refused; payload has public IDs only and no secrets.
- [ ] Side effects go through the outbox; listeners are idempotent on the event ID and respect their timeout.

Tests (see `testing`)
- [ ] Success, validation (422 with `field`), 401, non-member (404), another tenant's record (404), each role allowed/denied, and the event written.
