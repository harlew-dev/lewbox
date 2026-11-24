# Factories and seeders

**Factories** build fake records for every table. **Seeders** use factories to fill a development database. Tests use the same factories, so the data developers click through and the data tests assert on come from one source.

Use your stack's fake-data library (e.g. Faker for PHP/JS/Python, gofakeit for Go) behind your own factory functions; never call it directly from tests or seeders.

**Prefer** writing the factory type yourself (a small generic `Factory<T>` with `state`, `make`, `create`, typically ~50 lines) over a factory or fixture library where the stack has no well-maintained standard one (Laravel and factory_bot are). Ports of such libraries tend to be reflection-based, untyped and thinly maintained, and YAML fixtures pin you to a library version; your own is type-checked and inserts through your own models.

## Factories

### Shape of the API

```
f = testFaker(t)                                    // or newFaker() outside tests; logs its seed
user      = factories.users.createOne(ctx, db, f)
session   = factories.sessionFor(ctx, db, f, user.id)            // session.cookie(), session.csrfToken
tenant    = factories.tenantFor(ctx, db, f, user.id)             // owned by user
factories.memberOf(ctx, db, f, other.id, tenant.id, "member")

factories.projectsIn(tenant.id)
  .state(p => p.status = "Completed")               // applies to every record
  .sequence((i, p) => p.name = "P" + i)             // by position
  .create(ctx, db, f, 45)                           // one batched INSERT ... RETURNING
```

| Method | Does |
| - | - |
| `make` | Builds records in memory, no database |
| `create` / `createOne` | Inserts and returns them with DB-generated values (public ID, timestamps) read back |
| `state(fn)` | Returns a **copy** with an override applied to every record |
| `sequence(fn)` | Returns a copy with a per-position override |
| Named states | `expired`, `unverified`, `sent`, `revoked`, `named("Acme")`: one per meaningful variant, so tests read as intent |
| Helpers `<thing>For(parentID)` | Fill the required foreign key and any related setup (a session for a user, a membership for a tenant) |

Factories are **immutable**: every `state`/`sequence` returns a new factory, so a test can't leak a state into another test sharing the base factory.

### Rules

| Rule | Why | Enforced by |
| - | - | - |
| **Every table has a factory** | A table without one gets no realistic test data, and the next test author hand-writes SQL | A test listing the migrated schema's tables against the factory registry |
| Each factory's output is inserted into a freshly migrated schema | A new `NOT NULL` column or `CHECK` breaks the factory test, not someone's afternoon | A test creating one record per factory |
| **Factory output passes API validation** | Fake data the API would refuse hides UI and test bugs (a 300-character name that no form can produce) | A test running each factory's records through the API's validators |
| Factories have their own models, separate from the app's | They set fields the app never writes and must keep working when the app's model changes shape | Code review |
| Public IDs and timestamps are left to the database | Same path as production inserts | Read back with `RETURNING` |
| Secrets are real but contained: a factory session holds a real random token and stores only its digest; encrypted fields use a per-process random test key | Tests exercise the real crypto path without a shared secret in the repo | - |
| Expensive values are computed once per process (one password hash shared by every factory user) | Password hashing is deliberately slow; 500 users would take minutes | - |
| **Timestamps are spread, not all "now"** (e.g. `created_at` within the last 90 days, `updated_at` between it and now) | Records created in one batch share one timestamp, so sorting by date shows nothing and a tiebreak bug stays hidden | Review; a seeded list sorted by date visibly changes order |

### Reproducible randomness

- Values are random on every run: that's how factories find the bug your hand-picked "Test Project" never would (an apostrophe, a 100-character name, a non-Latin script).
- **Every run logs its seed.** A failing test prints `fake data seed 8231947`; every seeder prints its seed.
- **An environment variable replays it:** `FAKER_SEED=8231947` makes the same run produce the same data, so a flaky failure becomes a deterministic one.
- Uniqueness comes from the faker plus a per-process counter where collisions matter (emails), never from hoping.

### Safe defaults

A factory's default record is the **least dangerous** valid one. A test or seeder opts into danger explicitly.

| Data | Default |
| - | - |
| Email addresses | Unique, lower-case, at `example.test` (a reserved TLD, so no real inbox ever receives mail) |
| IP addresses | Documentation ranges: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32` |
| Credentials, API keys | Revoked or unusable by default; a test asks for a live one explicitly |
| Passwords | One known test password (constant in the factory), never a real one |
| Admin/operator rights | None. Seeders make no admins; only the real grant command does |
| Account state | Ordinary: verified, not suspended, MFA off; named states for the rest |
| Events, errors, metrics | Only when asked for by name; fake failures and traffic would mislead dashboards |
| Payload marker | Seeded events carry `"seeded": true`, so fake history can always be told apart and removed |

## Seeders

```sh
make seed                                  # the demo account, its tenant, named records (skipped if present)
make seed only=volume count=500            # bulk rows for checking lists at volume
make seed only=users count=25              # many accounts, each with a tenant
make seed only=events count=1000000        # a large log table, for EXPLAIN at volume
```

| Rule | Why |
| - | - |
| **Refused in production, before connecting** | A seeder near real data creates fake accounts with a known password. Check the environment first, not after opening a connection (a test asserts it) |
| One transaction per seeder | A failure leaves nothing half-written |
| **Idempotent where it creates named data**: the demo seeder skips if the demo account exists | `make seed` is safe to run twice, after every fresh rebuild |
| Insert-only bulk seeders say so (in `-h` and docs) and refuse bad targets (an unknown account, an account with no tenant) | Running them twice is expected to add more |
| **Never delete or truncate** | Resetting is the fresh-migration command's job, with its own guard |
| Counts bounded (e.g. 0-100 000; 0 means the default; a higher cap for log tables) | A typo can't fill the disk |
| Seeded accounts sign in with a password from the environment (`DEMO_PASSWORD`) | No real password in the repo |
| Prints its fake-data seed | Reproducible like tests |
| Lists its seeders with `-h` | Discoverable |
| The server build excludes factories and seeders | Fake-data code and a known password can't ship to production |
| Targets the configured local database; another database needs an explicit override | No accidental seeding of a shared database |

Give bulk seeders a useful default count so a bare run is meaningful (e.g. `volume`: 120 records, enough for several pages at 20 per page; `users`: 10 accounts with a random 0-60 records each, so some lists are empty and some paginate).

Demo data should exercise the UI: several records in each state, members in every role, enough rows to show a second page, a short history for log views. Volume seeders exist to test queries (`EXPLAIN` at a million rows), not for demos.

## When the schema changes

Update the model, the factory and every seeder that touches the table **in the same change** as the migration, then rebuild locally (`fresh` + `seed`). The factory tests catch what you missed.
