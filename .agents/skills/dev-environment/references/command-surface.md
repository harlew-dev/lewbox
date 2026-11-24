# The command surface: targets, scripts and the dev runner

Read this when you add or change a target or script, write the one-terminal dev runner, or set up the command surface for a stack that doesn't use Make.

## Choosing the runner

Any runner works if it gives one named, discoverable entry point per action, at the repo root, usable from CI. Make is the illustration here because it exists on every Unix machine and CI image.

| Runner | Fits | Watch out for |
| --- | --- | --- |
| `make` | any stack, polyglot repos | loads `.env` into everything if you `include` it; variables can't contain `:`; tabs |
| `just` | any stack; nicer arguments | an extra install on every machine and CI |
| Task (`Taskfile.yml`) | any stack; YAML | an extra install |
| `npm`/`pnpm` scripts | JS-only repos | a backend in another language ends up behind `npm run` |
| `composer` scripts, `mix` aliases, `bin/` scripts | single-language repos | keep the same target names |

Whatever you choose, the names stay the canonical ones in the SKILL (`dev`, `test`, `check`, `test-e2e`, `build`, `serve`, `db`, `migrate`, `seed`, `app-key`…), so docs and agents can rely on them across projects.

## A Makefile skeleton (illustration)

```make
SHELL := /bin/bash
.DEFAULT_GOAL := help
# Local development settings for every target. Tests ignore them (scripts/test-env.sh).
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help install app-key db migrate migrate-fresh migration seed dev api web build serve test check test-e2e serve-test

help:
	@echo 'Targets: install app-key db migrate migrate-fresh migration seed dev api web build serve test check test-e2e serve-test'
install:
	cd api && <backend dependency install, from the lockfile>
	cd web && npm ci
# Writes a key into a .env without one; never replaces an existing key.
app-key:
	<app CLI> app-key generate -env .env
db:
	docker compose up -d --wait postgres mailpit
	@docker compose exec -T postgres psql -U app -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='app_test'" | grep -q 1 \
	  || docker compose exec -T postgres createdb -U app app_test
migrate:
	<app CLI> migrate
# Development only: previews the tables it would drop; CONFIRM=1 drops and re-migrates.
migrate-fresh:
	<app CLI> migrate fresh $(if $(CONFIRM),-confirm)
migration:
	@test -n "$(name)" || (echo 'Usage: make migration name=add_tasks'; exit 1)
	<app CLI> migrate create $(name)
# make seed [only=demo|volume] [count=n]
seed:
	<app CLI> seed $(if $(only),-only '$(only)') $(if $(count),-count '$(count)')
dev:
	./scripts/dev.sh
build:
	./scripts/build.sh
serve: build
	API_ADDR=127.0.0.1:3000 ./bin/app
test:
	./scripts/test-env.sh sh -c 'cd api && <backend test, with race detector> && cd ../web && npm test'
check:
	./scripts/check.sh
test-e2e:
	E2E_PORT='$(E2E_PORT)' E2E_REUSE='$(E2E_REUSE)' ./scripts/test-env.sh ./scripts/e2e-preflight.sh sh -c 'cd web && npm run test:e2e'
serve-test: build
	E2E_PORT='$(E2E_PORT)' ./scripts/test-env.sh ./scripts/serve-test.sh
```

- **`.PHONY` lists every target**, or a stray file named `test` makes `make test` do nothing.
- **A comment above each non-obvious target** says what it does and its parameters. That comment is the documentation people read first.
- **Quote passed values** (`-only '$(only)'`) and pass optional flags only when set (`$(if $(x),-flag '$(x)')`), so the CLI's own default applies otherwise.
- **Make variable names can't contain `:`**, hence `target=events`, not `target:events`.
- **`make` loads `.env`, and its values beat the shell's.** `DATABASE_URL=… make prune` still prunes `.env`'s database. To act on another one, run the binary directly with the environment you want. Say this in the doc for every data-changing target.
- `CONFIRM=1` is the one spelling for confirmation through the runner; the CLI takes `-confirm`.

## Script conventions

- Every script starts with a header comment: what it does, what it's used by (`make dev`, CI), and how to run it by hand. A one-off utility (e.g. rendering a logo PNG for emails, because mail clients don't show SVG) also says **when** to run it.
- `set -euo pipefail` (bash) or `set -eu` (POSIX `sh`, for scripts that also run in a minimal container stage).
- Resolve the repo root from the script's own path (`root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"`) so it works from any directory.
- Shared helpers go in a sourced library (`scripts/e2e-lib.sh`) so two scripts can't disagree on, say, how a database name is derived.
- Usage errors exit 2, failed preconditions exit 1, each with a fix-it message on stderr.
- **Guard scripts get a self-test script** that `make check` runs: build a clean fixture (expect pass), plant each forbidden thing in each form (expect fail), and remove the input entirely (expect fail).

## The one-terminal dev runner

Illustration in bash; the behaviour is what matters.

```bash
#!/usr/bin/env bash
# make dev: builds what the API serves, then runs the API and the web dev server
# in one terminal with prefixed output. Ctrl-C stops all; if one exits, all stop.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"; cd "$root"

[[ -f .env ]] || { echo "No .env. Create it with: cp .env.example .env" >&2; exit 1; }
[[ -d web/node_modules ]] || { echo "Dependencies are missing. Run: make install" >&2; exit 1; }
set -a; source .env; set +a

# Every port must be free BEFORE anything starts.
in_use() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
for port in "${API_ADDR##*:}" 3000; do
  if in_use "$port"; then
    echo "Port $port is already in use. Stop the other server (make api, make web, make serve) first." >&2
    exit 1
  fi
done

label() { while IFS= read -r line; do printf '%s %s\n' "$1" "$line"; done; }

# A failed build stops here: nothing starts.
./scripts/build-web.sh 2>&1 | label "[build]"

# One signal to the process group stops every child and every prefixer.
trap 'trap - INT TERM EXIT; kill 0 2>/dev/null || true' INT TERM EXIT

(cd api && exec <run the API>) > >(label "[api]") 2>&1 & api=$!
(cd web && exec npm run dev)   > >(label "[web]") 2>&1 & web=$!

echo "dev: http://localhost:3000 (API on $API_ADDR). Ctrl-C stops both."
while kill -0 "$api" 2>/dev/null && kill -0 "$web" 2>/dev/null; do sleep 1; done
echo "A server stopped; stopping the others." >&2
```

- `pipefail` makes the `build | label` pipeline fail when the build fails, not when the labeller succeeds.
- The script loads `.env` itself, so it works without `make`.
- `/dev/tcp` probing is bash-only; in other languages, try to `listen` on the port and close it, or connect to it.
- **Off-the-shelf alternatives** give the same behaviour if configured to: `concurrently --kill-others --names api,web`, or a Procfile runner (foreman, overmind, honcho), which stops all when one exits. Still add the port and precondition checks in front.
- **Common mistakes:** starting processes with `&` and no trap (Ctrl-C leaves an orphaned API holding the port); checking ports *after* starting the first process; a dev server whose port silently increments.

## Production-like and containerised targets

- `make serve` runs the built artefact on the app's public port, bound to `127.0.0.1`, exactly as production would serve it (one process for API, pages and assets if that is the design). Stop `make dev` first: they share the port.
- `make docker-dev`: `db`, build the image, run migrate and seed as one-off containers (`docker compose run --rm app migrate`), then `up -d --wait app`. `make docker-app`: `docker compose up -d --build --no-deps --wait app`, so the database keeps running and nothing re-migrates.
- If the image installs every CLI as its own command, `docker compose run --rm app migrate` works; say in the docs if the plain binary does not take subcommands, so nobody runs `./bin/app migrate` and starts a full server by accident.
