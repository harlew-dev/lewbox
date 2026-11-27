# Migrations and new tables

## Files

- Numbered pairs: `<version>_<name>.up.sql` and `.down.sql` (e.g. `005_create_projects.up.sql`). Use your migration tool's naming, but always with a down file and always plain SQL you can read.
- **One create migration per table**, in dependency order (a table after the tables it references). The first migration creates shared functions (`public_id`, see [persistence.md](persistence.md#public-id-generation)).
- Each migration runs in its own transaction. The exception is a statement the database refuses inside one (PostgreSQL's `CREATE INDEX CONCURRENTLY`); give it a migration of its own, marked non-transactional, and make it re-runnable (`IF NOT EXISTS`).
- `CHECK` constraints mirror the API's validation (lengths with `char_length`, enums as `IN (...)`, formats with a regex), so a row written by any path (a seeder, a script, a future endpoint) obeys the same rules.

## Two phases of a project's life

### Before real data: edit in place

While no database holds data that must be kept (only local development and test databases exist), **change a table by editing its create migration**, not by adding an `ALTER` migration. Then rebuild the local database with a guarded "fresh" command and reseed.

Why: twenty `ALTER`s on one table make its shape unreadable; one create file per table is the schema's documentation.

The fresh command:

- lists the tables and row counts it would drop, and does nothing without `CONFIRM=1`;
- drops the schema (`DROP SCHEMA ... CASCADE`), recreates it and migrates;
- **refuses when the environment is production**, and should refuse any database host other than the local one.

Every teammate sharing a database runs it after pulling a schema change: migration tools record which versions ran, so an edited file won't re-run on its own.

### After real data: append-only

The day a database holds data that must be kept (production, a staging database people rely on, a customer's install), migrations become **append-only**:

- **NEVER** edit or delete a migration that has run on such a database. It won't run again there, so environments silently diverge, and the next fresh build differs from production.
- Every change is a new migration.
- Write it down in the project's docs when the switch happens (see `conventions`), so nobody keeps editing in place out of habit.

## Running migrations

- **NEVER migrate on server start.** With several instances starting together, startup migrations race; a slow migration delays readiness and fails health checks; a failed migration takes every instance down at once; and the server's database user then needs DDL rights it otherwise shouldn't have.
- Migrating is an explicit deploy step (`make migrate`, a CI job, a release command) that applies pending files as one group, holding a database lock so two runs can't interleave.
- A status command lists applied and pending versions.
- **Rollback previews unless confirmed.** A down migration usually drops tables; running it by accident is data loss.
- The server may check at startup that the schema version is the one it expects, and refuse to start (or warn) if it's behind. Checking is fine; migrating is not.

## Expand/contract for breaking changes

Once live, old and new code run side by side during a deploy (rolling restarts, several instances, a rollback). Every schema change must work with **both** the code before it and the code after it. Split breaking changes into releases:

| Step | Schema | Code |
| - | - | - |
| 1. Expand | Add the new column/table, **nullable or with a default**; no constraint old code would violate | Unchanged |
| 2. Dual-write | - | Writes both old and new; reads old |
| 3. Backfill | A batched job fills the new column for existing rows (batches by ID, as in [persistence.md](persistence.md#batched-deletes-cleanup-and-retention)), never one giant `UPDATE` | - |
| 4. Switch reads | Add `NOT NULL`/constraints now that every row complies (in PostgreSQL, `ADD CONSTRAINT ... NOT VALID` then `VALIDATE CONSTRAINT` avoids a long lock) | Reads new; still writes both |
| 5. Contract | - | Stops writing old |
| 6. Drop | Drop the old column/table in a **later** release | - |

Examples:

- **Renaming a column** is add new + dual-write + backfill + switch + drop old. Never `ALTER TABLE ... RENAME COLUMN` on a live table: old instances still query the old name.
- **Adding a required column**: add it with a default (or nullable), backfill, then add `NOT NULL`.
- **Adding an index on a large table**: `CREATE INDEX CONCURRENTLY` in its own non-transactional migration; a plain `CREATE INDEX` blocks writes for the whole build.
- **Changing a type** (`int` to `bigint`, `text` to an enum): a new column and the same steps.
- **Dropping a table**: stop using it in one release, drop it in the next.

Before any real data exists, none of this is needed: edit the create migration.

## The new-table checklist

A table is not done until every item holds. Make as many of these as possible fail a test when missing (e.g. a test listing the schema's tables against the factory registry, the data map and the tenant-table registry).

1. **Create migration**
   - `id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY` (internal: foreign keys, joins, tie-breaks, batched deletes).
   - `public_id text NOT NULL UNIQUE DEFAULT public_id('<pfx>') CHECK (public_id ~ '^<pfx>_[0-9A-Za-z]{14}$')` if users open, link or search its records. Pick the prefix with `naming`.
   - For tenant data: `tenant_id bigint NOT NULL REFERENCES tenants(id) ON DELETE CASCADE`.
   - Other foreign keys with a deliberate `ON DELETE` (`CASCADE` for owned rows, `SET NULL` for history and "created by").
   - `CHECK` constraints matching validation (lengths, enums, formats, "both or neither" pairs, durations in hours).
   - `created_at timestamptz NOT NULL DEFAULT now()`, `updated_at` likewise when rows change.
   - An index for the main query, **leading with `tenant_id`** for tenant data (`(tenant_id, updated_at DESC, id DESC)`), plus unique indexes for business uniqueness (`UNIQUE (tenant_id, lower(email))`).
2. **Model** in the data layer: public ID left empty on insert and read back; internal ID hidden from JSON; secrets in a separate unexported model if the table holds any.
3. **Queries** taking the tenant's internal ID (`WHERE x.public_id = ? AND x.tenant_id = ?`); lists through the shared pager with a sort map ending in `id`.
4. **Quota and limit** if users can create rows without bound (a per-user rate limit on the create route, a quota under a lock).
5. **Factory** with its own model, output passing validation; seeders updated if demo data should include it ([factories-and-seeding.md](factories-and-seeding.md)).
6. **Audit events** for each change, recorded in the same transaction ([events-and-listeners.md](events-and-listeners.md)).
7. **Data map entry:** what personal data it holds, why, how long it's kept, who can see it, and its retention rule: a prune target, the periodic cleanup, or deleted with its owner. See `conventions`.
8. **Registries:** add it to any list of known tables (an operator dashboard's table sizes, per-tenant row counts).
9. **Integration tests:** create/read/update/delete, validation, and another tenant's access (404 or an empty list). See `testing`.
10. **Docs:** the table's row in the schema docs and the endpoints table.

When a migration changes a table later, update the model, the factory and any seeder **in the same change**.

## Never

- Edit an applied migration once real data exists.
- Migrate on startup.
- Rename or drop a column in the same release as the code that stops using it.
- A table without a factory, a retention rule, or tenant scoping when it holds tenant data.
- Drop or reset any database other than the local development one (and that one only through the guarded fresh command). Integration tests use their own throwaway database or schema.
