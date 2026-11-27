# Persistence

Rules for the data layer: the one module that talks to the database. SQL examples use PostgreSQL syntax because it's the usual choice; the rules apply to any relational database (translate `FOR UPDATE SKIP LOCKED`, advisory locks, `RETURNING` and `timestamptz` to your engine's equivalents, and if one is missing, write down what replaces it).

## Where SQL lives

- **All queries live in the data layer** (a `store`/`repository` module). Handlers call typed functions; they never build or run a query. One place to audit for scoping and injection.
- **Use the query builder, or raw SQL with placeholders.** Values always go through bound parameters (`?`, `$1`, `:name`). SQL text is never built by concatenating or formatting input, not even "safe" input like an enum (today's enum is tomorrow's free-text field).
- **Dynamic SQL comes from fixed maps** written in code: sort keys to `ORDER BY` fragments, filter groups to `name IN (...)` lists, enum order to a bound array. The request picks a key; the code picks the SQL.
- Identifiers (table, column names) never come from input. If you think you need that, you need a map.
- Raw SQL outside the builder is limited to migrations and a few named infrastructure helpers (advisory locks, schema setup). Tests use the same builder.

```
// wrong: injection, and a crash on a quote
db.query("SELECT * FROM projects WHERE name LIKE '%" + q + "%' ORDER BY " + sort)

// right
db.select(projects).where("tenant_id = ?", tenantID)
  .where("name ILIKE ? ESCAPE '\\'", containsPattern(q))
  .orderExpr(projectSorts[sortKey])          // sortKey already validated against the map
```

## Public ID generation

The database generates public IDs so that every insert path (API, factories, seeders, a `psql` session) produces them identically and application code can't get the format wrong.

```sql
-- migration 001: before any table uses it
CREATE FUNCTION public_id(prefix text) RETURNS text LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  alphabet constant text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
  result text := '';
  bytes bytea;
  b int;
BEGIN
  WHILE length(result) < 14 LOOP
    bytes := uuid_send(gen_random_uuid());          -- 16 bytes from the built-in CSPRNG
    FOR i IN 0..15 LOOP
      CONTINUE WHEN i = 6 OR i = 8;                 -- UUID version/variant bytes are not fully random
      b := get_byte(bytes, i);
      CONTINUE WHEN b >= 248;                       -- 248 = 4 * 62: reject so every character is equally likely
      result := result || substr(alphabet, b % 62 + 1, 1);
      EXIT WHEN length(result) = 14;
    END LOOP;
  END LOOP;
  RETURN prefix || '_' || result;
END $$;

-- in each table
public_id text NOT NULL UNIQUE DEFAULT public_id('prj')
  CHECK (public_id ~ '^prj_[0-9A-Za-z]{14}$'),
```

- 14 base62 characters is about 83 random bits: unguessable and collision-free in practice; the `UNIQUE` constraint is the backstop.
- Models leave the public ID empty on insert so the database default applies (the ORM must send `DEFAULT`, not an empty string), and read it back with `RETURNING`.
- The internal `bigint` ID is the primary key and the target of every foreign key, join and sort tie-break. It is never serialised (mark it hidden in the JSON mapping).
- If your database can't run such a function, use a column default with its own random source, or as a last resort one shared data-layer function using a CSPRNG; never per-call-site generation.

## Deadlines

- Every query runs under the request's context, which carries the request deadline (e.g. 10 s, set by middleware). A slow query is cancelled instead of holding a connection after the client gave up.
- Background work (workers, cleanups, rollups) sets its own deadline per unit of work (e.g. 30 s per listener run, 30 minutes per prune run, stopping between batches).
- Log queries slower than a threshold (e.g. 500 ms) at warning level, with the route but **without** the SQL text or values (they may hold personal data). See `ops`.
- Keep the connection pool small and fixed (e.g. 10). A pool larger than the database can serve moves the queue into the database, where it's harder to see.

## Time: UTC everywhere

- Store instants as `timestamptz` (or your engine's UTC instant type), never local times and never strings.
- **Pin every connection's session time zone to UTC** at connect time, whatever the server default, the environment (`PGTZ`) or the URL say. Then `date_trunc('day', ...)`, `now() - interval '1 day'` and printed times are the same on every server.
- **Write fixed durations in hours** in `CHECK` constraints and migration SQL: `interval '720 hours'`, not `'30 days'`. In a session that isn't UTC (someone's `psql`, a migration run by hand), a "day" is 23 or 25 hours across a DST change, so a 30-day cap silently becomes 29 days 23 hours.
- APIs speak RFC 3339 with `Z`. Converting to a user's zone is the client's job.

## Transactions

- **A change and everything that must agree with it commit together:** the row, its audit event, its outbox deliveries, counters it maintains. A failure anywhere rolls back everything.
- **Read-check-write happens under a lock, in one transaction.** Checking "is this the last owner?" before the transaction and deleting inside it lets two concurrent requests both pass. Lock, re-check, write:

  ```sql
  BEGIN;
  SELECT id FROM tenants WHERE id = $1 FOR UPDATE;                         -- serialise membership changes
  SELECT count(*) FROM memberships WHERE tenant_id = $1 AND role = 'owner';
  -- refuse with 409 if this would remove the last owner
  DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2;
  INSERT INTO events (...) VALUES (...);
  COMMIT;
  ```

- **Re-check user-typed confirmations under the lock.** Deleting a tenant requires typing its name; compare the name after locking the row, so a tab opened before a rename can't delete it under the old name.
- Keep transactions short: no network calls (email, webhooks, HTTP APIs), no waiting on users, no large loops. Side effects go to the outbox ([events-and-listeners.md](events-and-listeners.md)).
- Write paths that act on behalf of a credential (an API key, a session) re-read the live authority inside the transaction (the membership and permission still exist, the account isn't suspended), because it may have changed since the middleware checked.

## Consistent lock order

Deadlocks happen when transaction A locks X then Y and transaction B locks Y then X. The fix is a single global order that every code path follows:

1. The acting user's row (or the account being changed).
2. Tenant rows, **by ascending internal ID** when more than one.
3. Child rows (credentials, projects, memberships), by ascending ID.

Example: deleting an account locks the user, then every tenant the user owns in `id` order, then checks "is this user the only owner of any of them?". Creating an invitation locks the inviter's user row and then the tenant's: the same order, so the two can't deadlock.

- `SELECT ... FOR UPDATE` on a list must have `ORDER BY id` to take locks in order.
- When a new code path needs locks, write its order in a comment and check it against the others.
- When the thing to serialise has no row (an email address that may exist in two tables, a global counter), use a **transaction-scoped advisory lock** on a hash of a name + the value (`pg_advisory_xact_lock(hashtext('email:' || $1))`). Example: blocking an address and creating an account with it both take the email lock before checking the other's table, so both can't pass. Include the schema name in the hash if tests run in parallel schemas.
- Singleton jobs (an hourly rollup, an alert evaluator) use `pg_try_advisory_xact_lock`: one server does the work, the others skip instead of waiting.

## Quotas under a lock

Anything a user can create without bound gets a hard cap (tenants per user, projects per tenant, invitations live per tenant). The check must be atomic:

```sql
BEGIN;
SELECT id FROM tenants WHERE id = $1 FOR UPDATE;        -- the parent the quota belongs to
SELECT count(*) FROM projects WHERE tenant_id = $1;     -- refuse past the cap -> 422 without field
INSERT INTO projects (...) VALUES (...) RETURNING *;
INSERT INTO events (...) VALUES (...);
COMMIT;
```

Without the lock, 50 parallel requests at 999/1000 all see 999 and all insert. The quota error is a typed error the handler maps to `422 {"error": "A workspace can have up to 1,000 projects."}`. Quotas are separate from rate limits (which bound requests per time window, see `ops`) and stay on even when rate limits are switched off for tests.

## Deleting

- **Permanent and immediate** unless a product requirement says otherwise: one transaction, with `ON DELETE CASCADE` on the foreign keys of rows the parent owns (a tenant's projects and memberships), and `ON DELETE SET NULL` on references that must outlive it (an event's actor, an invitation's inviter).
- Write the cascade map down per resource: what goes with it, what stays and how what stays is anonymised. Audit history usually stays, readable by the public IDs in its payload.
- Deleting an account also erases its personal data where it survives elsewhere (e.g. its email in other rows' event payloads replaced by `"[deleted]"`). See `ops` (privacy and compliance) for erasure and the data map.
- Large cascades take the parent's lock first, in the global lock order.

## Batched deletes (cleanup and retention)

Log-like tables (events, errors, metrics, expired sessions and links) grow forever unless something deletes old rows. A single `DELETE FROM events WHERE created_at < $cutoff` on millions of rows holds locks for minutes, blocks writers and bloats the write-ahead log. Delete in batches:

```sql
-- repeat until it deletes 0 rows
DELETE FROM events
WHERE id IN (
  SELECT id FROM events
  WHERE created_at < $cutoff                 -- fixed when the run started
    AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.event_id = events.id AND d.delivered_at IS NULL)
  ORDER BY id
  LIMIT 5000
  FOR UPDATE SKIP LOCKED                     -- two runs don't wait on each other
);
```

| Rule | Why |
| - | - |
| Fixed cutoff, computed once at start | Rows created during the run are never in scope |
| Batches of a few thousand, each its own short statement/transaction | No long transaction; cancellation (Ctrl-C, timeout) stops between batches with a report of what was done |
| `FOR UPDATE SKIP LOCKED` | Concurrent runs and live writers don't block each other |
| A `Keep` condition per target | E.g. keep events with a pending delivery: a listener may still need them |
| Preview unless confirmed (`CONFIRM=1`, `-confirm`) | Prints "would delete 12 408 older than 2026-08-01 (90d)"; destructive only on purpose |
| Per-target default age; an override has a floor (e.g. at least 1 hour) | A typo like `0d` can't empty a table |
| Record a completion event (`target`, `deleted`, `kept`, `cutoff`) | Evidence that retention runs |
| The table needs a bigint `id` primary key | Batches delete by ID |

Short-lived rows (sessions, reset links, challenges, rate-limit counters) are removed by a periodic in-server cleanup (e.g. every 15 minutes); long-lived logs by an explicit prune command run from a scheduler. Both are allowed in production, unlike seeding.

## Never

- SQL in a handler, or a string-built query.
- A lookup by ID followed by an ownership check in code.
- A network call inside a transaction.
- A check-then-write without a lock.
- Locks taken in an order that differs from the global order.
- Local times, or day-based intervals in SQL that must be exact.
- An unbounded `DELETE` on a large table.
