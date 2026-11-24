# Backend integration tests: harness, configuration, factories

Read this when you set up the backend test harness for a new stack, write or debug an integration test, add a table or column, or add a factory or seeder.

## The database harness

### One throwaway schema per test

```
openTestDB(t):
  url   = readTestConfig()["DATABASE_URL"]       # from the committed file, never os.env
  refuseUnlessLocal(url)                          # host in {127.0.0.1, ::1, localhost, <compose service>}
  refuseUnlessTestName(url)                       # db name matches *_test or *_test_<port>
  db    = connect(url)  or fail("database unreachable: start it with `make db`")
  name  = "app_test_" + timestamp + random
  exec("CREATE SCHEMA " + name)
  pin search_path (or the driver's equivalent) to name, on every pooled connection
  migrate(db)
  assert migrate(db) is a no-op                   # migrations are idempotent
  t.cleanup(() => exec("DROP SCHEMA " + name + " CASCADE"))   # only its own schema
  return db
```

- **Why a schema per test, not a shared DB that you truncate:** tests run in parallel without seeing each other's rows, a crashed test leaves nothing that breaks the next one, and existing data (someone's local test DB) is never touched.
- **Why not wrap each test in a rolled-back transaction:** it hides what only happens across connections and at commit: row locks between two requests, deferred constraints, unique violations under concurrency, deadlocks. Race tests need several real connections.
- Engines without schemas: use a database per test (MySQL) or a temp file (SQLite). Same rules: unique name, created and dropped by the test, local only.
- Pin the schema on **every** connection in the pool (e.g. in the DSN's `search_path` option or a connect hook), not with one `SET` on whichever connection you happen to get.
- **Common mistake:** running integration tests against the dev database "because it's already migrated". One `DELETE` without a tenant filter in a test wipes real work.

### The test configuration file

- The committed `.env.testing`, its strict `KEY=value` format, the emptied environment test targets run in, and the database guard's exact rules: `dev-environment` (`references/env-files.md`, `references/test-databases-and-parallel-e2e.md`).
- The harness's part: direct runs (`<test-runner> ./pkg -run TestX`) find the file by walking up from the working directory, and the DB URL is read from it, never from the process environment, with no fallback.
- Tests that guard the guards:
  - `TestLocalTestDatabase`: non-local hosts and non-`_test` names are refused.
  - `TestEnvTestingHoldsNoSecrets`: the file parses, points at a local test DB, and nothing whose name looks like a secret (`*_KEY`, `*_SECRET`, `*_TOKEN`, `*_PASSWORD`) has a value.
  - The parser test: quotes, `export`, `$` expansion and repeated keys are refused, naming the line.

### Never skip

```
# Wrong: silently green when Postgres is down
if !dbAvailable() { t.Skip("no database") }

# Right: red, with the fix
db = connect(url) or t.Fatal("database unreachable at 127.0.0.1:5432: start it with `make db`")
```

The same applies to any required tool (browser binary, mail catcher, C compiler for the race detector): fail and name the install or start command.

## Building the server in a test

- Each test package has a short `server(t)` that returns the **real** server (real router, real middleware, real store) on a fresh schema. Tests that need other configuration (a trusted proxy, rate limits on, a feature flag) build their own from the same constructor.
- Send requests through **one helper** that does what a real browser would: trusted `Origin`, `Content-Type: application/json`, the session cookie, the CSRF token, a fixed client address (`127.0.0.1`). It **fails the test on an unexpected status** and returns the decoded body:

  ```
  body = call(t, handler, "PATCH", "/api/projects/prj_123", {name: "New"},
              csrf=session.csrf, cookie=session.cookie, want=200)
  ```

  Have a non-failing variant (`send`) for use inside goroutines, where failing the test from another thread is unsafe in some languages.
- Background work (mail queue, event deliveries) runs through the **real** worker, invoked once synchronously by the test (`deliver()`), not by sleeping until a timer fires.
- Lookups the tests need often (`idOf(table, publicId)`, `emailOf(userId)`, `eventsNamed(name)`, `onlyEvent(name)`) live in the shared helpers so nobody writes one-off SQL in a test.
- **Tests that need internals stay beside them:** the permission matrix reads the router's registry, so it lives in the router's test package. Everything else goes through HTTP.

## The shared test-helpers module

- One module (e.g. `apitest/`, `testutil/`) holds: `server`, `call`/`send`, `signedIn(t, db, faker)` (a fake user plus a live session: user ID, cookie, CSRF token), `ownedTenant`, `tenantPath(tenantPublicId, "/projects")`, `withMFA` and code generation, common request bodies, event assertions, `retryWithin` (checks `Retry-After`), the mailbox driver.
- **Only tests import it.** An architecture test fails if production code does. It must not import the composition root (router or server wiring), so tests inside those packages can use it without an import cycle.
- A helper used by one package's tests stays in that package. Promote it only when a second package needs it.

## Factories

### Shape

```
f       = testFaker(t)                                   # seeded; prints the seed on failure
user    = factories.users.createOne(ctx, db, f)
session = sessionFor(ctx, db, f, user.id)                # session.cookie(), session.csrfToken
tenant  = tenantFor(ctx, db, f, user.id)                 # owned by user
memberOf(ctx, db, f, other.id, tenant.id, role="viewer")

projectsIn(tenant.id)
  .state(p => p.status = "completed")                    # every record
  .sequence((i, p) => p.name = "P" + i)                  # by position
  .create(ctx, db, f, 45)                                # one batched INSERT … RETURNING
```

- Methods: `make` (build, don't insert), `create`, `createOne`, `state`, `sequence`. **Factories are immutable:** each state returns a copy, so a shared base factory can't be mutated by one test and break another.
- **Named states** for meaningful variants: `expired`, `idle`, `unverified`, `sent`, `revoked`, `exhausted(limit)`, `forIP(ip)`, `forAccount(id)`, `invitedAs(email, role)`. A test reads like the scenario: `invitationsTo(t.id).expired().createOne(...)`.
- **Relationship helpers** (`sessionFor`, `tenantFor`, `memberOf`, `projectsIn`) set the foreign keys, so a test can't forget the membership row and get a 404 for the wrong reason.
- **Public IDs** come from the database default and are read back by `create`, exactly as in production.
- **Secrets:** the factory stores only the digest (as the app does) and keeps the raw token, code or key on the returned object for the test to send: `reset.token`, `totp.code()`, `registration.code`.
- Factory models are their own types, separate from the application's models, so a factory can write columns the app never writes directly (timestamps in the past, revoked states).

### Values

- Random but valid: unique lower-case emails at `example.test`, names within length limits, real-looking company names, dates in a plausible recent window.
- IP addresses from documentation ranges: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`. They can never be real users.
- Safe defaults: email verified unless `unverified`, API keys revoked unless the test supplies a live digest, nothing sent, nobody suspended. A test that needs the dangerous state asks for it by name.

### Set only what you assert

```
# Wrong: which of these matter?
project = createProject({name: "Alpha", status: "active", color: "blue", description: "x", createdAt: …})
assert list()[0].name == "Alpha"

# Right
projectsIn(t.id).sequence((i, p) => p.name = ["b", "a", "c"][i]).create(…, 3)
assert names(list(sort="name")) == ["a", "b", "c"]
```

### Reproducible randomness

- Every run takes a fresh seed. On failure the test logs `fake data seed 8172631`. `FAKER_SEED=8172631 <run that test>` replays the same data.
- Seeders print their seed too.
- **Common mistake:** a global unseeded random source somewhere in a factory (e.g. `Math.random()` for a suffix). The replay then differs and the bug can't be reproduced. All randomness goes through the seeded faker. Uniqueness suffixes come from a counter.

### Enforced rules

| Test | Fails when |
| --- | --- |
| `TestEveryTableHasAFactory` | a migrated table has no factory, or a factory's output doesn't insert into a freshly migrated schema (a new required column with no factory value) |
| `TestFactoriesMatchValidation` | a factory produces a value the API would refuse (a name too long, a forbidden character) |
| `TestSeedingIsRefusedInProduction` | a seeder can run with a production config (refused before connecting) |
| architecture test | the production build includes the factory or seeder packages |

A migration that adds a table or required column updates the factory, the seeders and the app's model **in the same change**.

## Seeders (development data)

- Seeders reuse the factories. Each runs in one transaction, prints its seed, **never deletes or truncates**, and refuses production.
- A demo seeder is idempotent (skips if its demo account exists). Volume seeders are insert-only, with bounded counts (e.g. 0–100 000, events up to 2 000 000).
- Fake history (events, metrics, errors) is only for volume checks and is marked (`"seeded": true` in the payload), so it can't be mistaken for real activity. Seeders never create admins, live credentials or blocks.

## Common mistakes

- Hand-writing an `INSERT` because "the factory doesn't support that field yet". Add the state to the factory.
- Creating test data through other endpoints (`POST /register` then `POST /tenants`) in a test about projects. It's slow, and when registration breaks every test fails, which hides the real failure. Use factories.
- Asserting only on the response body. A handler can return the right JSON and never commit. Read it back.
- Sharing one tenant across tests through a package variable. Tests become order-dependent and fail only in parallel.
