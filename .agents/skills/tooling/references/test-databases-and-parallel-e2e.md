# Test databases and parallel e2e runs

Read this when you set up the test databases, write the e2e preflight, port lock or e2e server script, wire the e2e runner's web-server config, or debug "port in use" / "refusing to rebuild". What the tests themselves do (schema-per-test harness, test data, waiting, mail assertions) is `testing`'s.

## Layout

One database container per machine, shared by everyone; runs are kept apart by **databases**, not containers.

| Database | Used by | Created by | Rebuilt |
| --- | --- | --- | --- |
| `app` | development | the container's `POSTGRES_DB` | by you only (`make migrate-fresh CONFIRM=1`) |
| `app_test` | integration tests, one throwaway schema each | `make db`, idempotently | never; each test drops only its own schema |
| `app_test_<port>` | the e2e run that holds `<port>` | the e2e server script, if missing | at every e2e server start (`migrate fresh`) |

- `make db` creates `app_test` after the container is healthy, with a check-then-create that does nothing the second time. This covers existing volumes; an `initdb.d` script would not.
- In CI, the service container's `POSTGRES_DB` is `app_test`, with the same credentials as `.env.testing`. The e2e server creates `app_test_<port>` there as it does locally.
- Engines without schemas: a database per test (MySQL) or a temp file (SQLite). The same guard applies to anything shared.

## The guard

```
localTestDatabase(url, rebuild=false):
  u = parse(url)                               or refuse("unparseable DATABASE_URL")
  host in {127.0.0.1, ::1, localhost, <compose service name>}
                                               or refuse("not on this machine: " + host)
  name = u.path without "/"
  if rebuild: name matches ^[a-z0-9_]+_test_[0-9]+$   or refuse("must be named *_test_<port>")
  else:       name matches ^[a-z0-9_]+_test(_[0-9]+)?$ or refuse("not a test database")
```

- **Run it everywhere a test touches a database:** the integration harness before connecting, the e2e server script before `migrate fresh`, and the `.env.testing` test.
- **Rebuilders are stricter:** the e2e server may rebuild only `*_test_<port>`, never `app_test` (the integration tests' shared database) and never `app`.
- Because the name matched the pattern, it is safe to interpolate into `CREATE DATABASE`; nothing else is.
- Tests for the guard: accepts each local host and the service name with `_test` and `_test_3101`; refuses another host, a name without `_test`, `_test_` followed by non-digits, and an unparseable URL.
- **Three independent checks** stand between a test and real data: the committed file is reviewed like code, the guard checks host and name, and the destructive command itself refuses production.

## The e2e run, step by step

```
make test-e2e [E2E_PORT=3101|auto] [E2E_REUSE=1]
  └ test-env.sh                        emptied environment + .env.testing
    └ e2e-preflight.sh                 < 1 s, before anything is built
        check tools (flock, psql), each with an install hint
        check the database and mail catcher answer ("start it with make db")
        reserve the port (lock), and a second port for settings specs
        print "E2E run: app http://localhost:<port> · database app_test_<port>"
        exec the e2e runner with E2E_PORT, E2E_LOCK_HELD=1
      └ e2e runner (e.g. Playwright)   loads .env.testing over the env, makes per-run keys
          webServer: make serve-test   (build, then serve-test.sh)
            └ serve-test.sh            guard the derived database, create if missing,
                                       migrate fresh, exec the built server on 127.0.0.1:<port>
```

### Reserving a port

```bash
e2e_lock_file() { echo "/tmp/<app>-e2e-$1.lock"; }   # machine-wide, not $TMPDIR

e2e_reserve() {                 # holds the lock in an open fd for this process's life
  local lock fd
  lock="$(e2e_lock_file "$1")"
  exec {fd}>> "$lock"
  chmod a+rw "$lock" 2>/dev/null || true             # another user can take it later
  if flock -n "$fd"; then E2E_LOCK_FD=$fd; return 0; fi
  exec {fd}>&-; return 1
}
```

- **The lock is held by an open file descriptor**, inherited through `exec` by the runner, so it lasts exactly as long as the run and is released by the kernel on exit or crash. No stale lock files to clean up.
- **Take the lock, then check nothing listens.** A server someone started by hand holds no lock but still owns the port.
- **`auto`** walks a range (3101–3199), reserving each candidate and releasing it if something listens, until one sticks. No free port: fail with the range.
- **A named port that clashes** fails at once, suggesting the first free port (checked without holding it) and `E2E_PORT=auto`, with a link to the doc section.
- **Validate the port** (digits, 1024–65535) before using it in a lock path, URL or database name.
- **The child doesn't re-lock:** the preflight passes `E2E_LOCK_HELD=1`; `serve-test.sh` run by hand reserves the port itself, with the same messages.
- **Second ports:** specs that restart a server with other settings (a feature flag, rate limits on) need another port. The preflight reserves the next free one (`E2E_SETTINGS_PORT`) for the run's lifetime; without one those specs are skipped with a message, not failed. Such a server shares the run's database and must not rebuild it (`E2E_SHARED_DATABASE=1`).
- `flock` comes with util-linux on Linux and CI runners; on macOS, `brew install flock`. The preflight says so.

### The e2e server script

1. Validate `E2E_PORT`; derive `DATABASE_URL` by suffixing the database name with `_<port>` (keep the query string).
2. **Guard** the derived URL with the rebuild rules above. Refuse before touching anything.
3. Reserve the port unless `E2E_LOCK_HELD=1`; check it's not in use.
4. Check the database, mail catcher, the built artefact and `psql` exist.
5. Create the database if missing; `migrate fresh -confirm` it (unless sharing a run's database).
6. Use the run's `E2E_APP_KEY` (and other per-run keys) or generate fresh ones, print the URLs, and `exec` the built server with `API_ADDR=127.0.0.1:<port>` and `APP_ORIGIN=http://localhost:<port>`.

Rebuilding at every start means each run begins empty and a changed create-migration always applies, which matters while migrations are squashed.

### The e2e runner's web-server config

```ts
webServer: {
  command: "make serve-test",
  cwd: root,
  url: `${origin}/<a page that only answers once ready>`,
  reuseExistingServer: process.env.E2E_REUSE === "1",   // never by default
  timeout: 180_000,                                      // the build is included
  stdout: "pipe", stderr: "pipe",                        // server log in the run's output
},
```

- **Never reuse a server already on the port by default:** with parallel runs it may be another worktree's server running other code. `E2E_REUSE=1` (with an explicit port, never `auto`) is the opt-in for iterating against a `make serve-test` you started; it skips the reservation.
- Traces kept on failure, screenshots on failure, optional video (`E2E_VIDEO=1`): CI uploads them (`ci.md`).

## Troubleshooting

| Message | Cause | Fix |
| --- | --- | --- |
| `Port 3100 is in use: another e2e run (or a server) has it` | another run holds the lock, or something listens | use the suggested port or `E2E_PORT=auto`; never kill the other run |
| `The test database isn't running at 127.0.0.1:5432` | services down | `make db` |
| `Refusing to rebuild app_test: the e2e database must be named *_test_<port>` | `DATABASE_URL` not derived, or `.env.testing` edited | fix the file; never loosen the pattern |
| `Refusing to rebuild a database on db.example.com` | non-local host | tests only run locally |
| `flock is missing` | macOS without util-linux | `brew install flock` |
| e2e fails randomly while another run builds | two runs in one checkout share build output | one worktree per agent |
| `:3000` taken by Docker | the `app` container from `docker-dev` | `docker compose stop app` if it's yours; otherwise use another port |
