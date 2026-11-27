# Env files and the emptied test environment

Read this when you add a config variable, write or change `.env.example` or `.env.testing`, write the test-env wrapper, implement `app-key`, or touch `.gitignore`/`.dockerignore`. Production config validation (what the server refuses at startup) is `ops`'s (`references/configuration-and-secrets.md`).

## The three files

| File | Committed | Holds | Read by |
| --- | --- | --- | --- |
| `.env.example` | yes | every variable, documented, with local-only example values | humans; copied to `.env` |
| `.env` | **never** | the developer's dev settings | the task runner, the dev runner |
| `.env.testing` | yes | throwaway test settings only | every test, however it's started |

## `.env.example`

Grouped, commented blocks. Each block says what the variables do, the default, what production refuses, and where the docs are:

```sh
# Local development settings: cp .env.example .env (make loads .env).
# Every variable: docs/<backend>/README.md#environment. Production: docs/deployment.md.

# App: name, mode, public origin, and the key that seals two-factor secrets.
# This key is for local use only and production refuses it; make app-key
# writes a new one into a .env without one.
APP_NAME=app
APP_ENV=development
APP_ORIGIN=http://localhost:3000
APP_KEY=<a valid, published, local-only example key>

# API server: where it listens (localhost only), and where the frontend's dev proxy finds it.
API_ADDR=127.0.0.1:8080
API_URL=http://127.0.0.1:8080

# Database: the local container that make db starts.
DATABASE_URL=postgresql://app:password@127.0.0.1:5432/app?sslmode=disable

# Mail: to the local catcher from make db, inbox at http://localhost:8025.
MAIL_DRIVER=smtp
SMTP_HOST=127.0.0.1
SMTP_PORT=1025

# Optional settings stay commented out, with their default stated.
# LOG_LEVEL=info
```

- The example values make `cp .env.example .env && make dev` work with no edits.
- **Example secrets must be valid in shape** (a 32-byte key decodes) so development works, **and listed in the server's production refusals**, so a copied example can never run in production.
- Optional variables appear commented out with their default, so the file is the complete list.
- A variable added to the code but not to `.env.example` is a bug: review checks it, and a test can (parse the config struct's tags or the loader's key list and compare).
- Values a frontend build bakes in (the product name in titles, a release ID) need a **rebuild** after changing, not just a restart. Say so beside the variable.

## `app-key`

```
app-key generate -env .env:
  read .env (fail if missing: "cp .env.example .env first")
  if it has a non-empty APP_KEY: print "APP_KEY already set; not replacing it" and exit 0
  write APP_KEY=<32 random bytes, base64> (append, or fill the empty line)
```

- **NEVER replace an existing key**, not even with `--force` through the task runner. The key seals second-factor secrets and derives other keys; replacing it locks every two-factor account out of that database. Rotation is a separate, deliberate procedure (`ops`).
- Run it once, before the first `make dev`. Generators for other secrets (`route-key generate`) print a value rather than editing files.

## `.env.testing`

```sh
# Test settings, committed on purpose. make test, make test-e2e, the backend
# test runner and the e2e runner use this file and ignore .env, however started.
# Only local, throwaway values: never a real secret (a test checks).
# Plain KEY=value lines only: no quotes, no spaces, no export, no $.
APP_ENV=development
# The e2e server's base port; its origin, listen address and database
# (app_test_<port>) are derived from it. Per run: make test-e2e E2E_PORT=auto.
E2E_PORT=3100
# The integration tests' database: each test makes a throwaway schema in it.
DATABASE_URL=postgresql://app:password@127.0.0.1:5432/app_test?sslmode=disable
# One e2e run signs up more users from one IP than the per-IP limit allows.
RATE_LIMITS=off
CAPTCHA_PROVIDER=off
MAIL_DRIVER=smtp
SMTP_HOST=127.0.0.1
SMTP_PORT=1025
SMTP_PASSWORD=
MAILPIT_URL=http://127.0.0.1:8025
```

- **Only values that are already public:** the same local container credentials committed in `compose.yaml`, on a database bound to `127.0.0.1`.
- **Hold the base, derive the rest.** It holds `E2E_PORT`, not the e2e origin and listen address, which the e2e scripts and config derive per run. Storing derived values invites them to disagree.
- **No per-run secrets.** The e2e config generates the app key (and any admin route key) once per run and passes them to every server it starts, so servers sharing a database can read each other's sealed values.
- `APP_ENV=development`, not a special `testing` mode: config distinguishes production from everything else, and the test server should behave like development.

### Tests that guard the file

```
TestEnvTestingHoldsNoSecrets:
  env = parse(".env.testing")                # the strict parser below; fails on bad lines
  assert localTestDatabase(env["DATABASE_URL"]) is ok
  for key, value in env:
    if (key contains PASSWORD|SECRET|TOKEN or key ends with KEY) and value != "":
      fail(key + " looks like a secret and has a value; .env.testing is committed")

TestEnvParser:
  accepts: KEY=value, # comments, blank lines
  refuses (naming the line): quotes, export, spaces around =, $, backslashes, repeated keys
```

The strict format is what lets bash `source`, Make `include`, your backend and Node's `util.parseEnv` (or `dotenv`) agree on every value. A value that means one thing to bash and another to the app is a test that passes for the wrong reason.

## The emptied environment: `scripts/test-env.sh`

```bash
#!/usr/bin/env bash
# Runs a command with .env.testing and nothing else from the environment, so values
# from .env (which make exports to every target) never reach a test.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ $# -gt 0 ]] || { echo "Usage: scripts/test-env.sh <command> [args…]" >&2; exit 2; }

settings=()
while IFS= read -r line || [[ -n $line ]]; do
  [[ -z $line || $line == \#* ]] && continue
  settings+=("$line")
done < "$root/.env.testing"

kept=()
for name in $(compgen -e); do
  case "$name" in
    PATH | HOME | USER | TMPDIR | TERM | CI | CC | CHROMIUM_PATH | FAKER_SEED | GO* | PLAYWRIGHT_*) ;;
    E2E_PORT | E2E_REUSE | E2E_LOCK_HELD | E2E_SETTINGS_PORT | E2E_APP_KEY | E2E_VIDEO) [[ -n ${!name} ]] || continue ;;
    *) continue ;;
  esac
  kept+=("$name=${!name}")
done

# Later assignments win: a caller's E2E_PORT replaces the file's.
exec env -i "${settings[@]}" "${kept[@]}" "$@"
```

- **The allow-list:** what finds the tools (`PATH`, `HOME`, `USER`, `TMPDIR`, `TERM`, the toolchain's own variables such as `GO*`, `CC`, `NODE_*` if needed), `CI`, the browser path, `FAKER_SEED` (to replay a failing seed), the test runner's own settings (`PLAYWRIGHT_*`) and the run's own `E2E_*` values.
- **Empty `E2E_*` values count as unset:** the task runner passes `E2E_PORT=''` when none is given, which would otherwise replace the file's default with nothing.
- **Widen the list only with a reason** written beside it. Never add a pattern like `APP_*` or `DATABASE_*`: those are exactly what must not leak.
- **Direct runs:** the backend harness finds `.env.testing` by walking up from the working directory and reads the database URL **from the file, never the process environment**. The e2e config reads the file and **assigns every key over `process.env`**, keeping only the caller's `E2E_PORT`:

  ```ts
  const callerPort = process.env.E2E_PORT;
  Object.assign(process.env, parseEnv(readFileSync(`${root}/.env.testing`, "utf8")));
  const port = callerPort || process.env.E2E_PORT || "3100";
  ```

## Ignore files

```gitignore
.env
.env.*
!.env.example
!.env.testing
tmp/
```

```dockerignore
.git
.env
.env.*
**/node_modules
<build output>
plans
docs
# A dev mailbox file in tmp/ holds working password-reset links.
**/tmp
```

- `.env.*` with explicit exceptions, never a list of known names: a new `.env.staging` is then ignored by default.
- The Docker build context excludes every env file, including `.env.testing`. Configuration reaches containers through the environment at run time, never through a baked file.

## Common mistakes

- Putting a "test" API key or real SMTP password in `.env.testing` "because the test needs it". Fake the boundary instead (`testing`), or generate the value per run.
- Adding `TEST_DATABASE_URL` alongside `DATABASE_URL`. Two names for one thing means a test can read the wrong one; use the server's own name from the test file.
- Reading the test database URL from the process environment "as a fallback". The fallback is exactly how the dev URL gets in.
- Telling developers to `export` variables in their shell profile. The next project, or the test suite, inherits them.
