# CI

Read this when you write or change the CI workflow, add a service, change caching, or debug "passes locally, fails in CI" (or the reverse). The examples are GitHub Actions; GitLab CI, Buildkite and others have direct equivalents (`services:`, `cache:`, `artifacts:`).

## The principle

**CI is a clean laptop running the documented commands.** It installs dependencies from the lockfiles, starts the same services, and runs `make install`, `make test check build` and `make test-e2e`. Test settings come from the committed `.env.testing`, as locally. So:

- a green CI means `make test check build test-e2e` passes on a clean checkout with only committed files;
- a CI failure is reproducible locally with the same target;
- **no `env:` block of test settings in the workflow.** A setting CI needs that a laptop doesn't is either missing from `.env.testing` or a sign that the two have drifted.

## Skeleton

```yaml
name: CI
on: [push, pull_request]
permissions:
  contents: read
jobs:
  check:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    services:
      postgres:
        image: postgres:17                    # the tag compose.yaml pins
        env:                                  # the credentials and database .env.testing names
          POSTGRES_USER: app
          POSTGRES_PASSWORD: password
          POSTGRES_DB: app_test
        ports: ['5432:5432']
        options: >-
          --health-cmd pg_isready --health-interval 5s --health-timeout 5s --health-retries 10
      mailpit:
        image: axllent/mailpit:v1.31.0        # the tag compose.yaml pins
        ports: ['1025:1025', '8025:8025']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5             # or setup-node/python/php…: version from the repo
        with:
          go-version-file: api/go.mod
          cache-dependency-path: api/go.sum
      - uses: actions/setup-node@v4
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: make install
      - run: make test check build
      - run: npm --prefix web exec -- playwright install --with-deps chromium
      - name: Browser tests
        run: make test-e2e                    # the server log appears prefixed [WebServer]
      - if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: e2e-results
          path: |
            web/test-results/
            web/playwright-report/
          retention-days: 14
```

## Rules

- **Services mirror `compose.yaml` and `.env.testing`:** the same image and tag, the credentials and database name the test file uses, a healthcheck on each, so steps start only once they're ready. A different image in CI is how "works locally" happens.
- **Toolchain versions come from the repo** (`go-version-file`, `node-version-file`/`.nvmrc`, `.tool-versions`, `composer.json` platform). Never a second, hard-coded version in the YAML that drifts from the one developers use.
- **Cache dependency downloads keyed on the lockfile** (the setup actions' built-in caches do this). Install with the lockfile-strict command (`npm ci`, `go mod download`, `composer install`, `pip install -r` with hashes). Never cache build outputs or `node_modules` across lockfile changes.
- **Install only the browser the suite uses, with its OS dependencies** (`playwright install --with-deps chromium`). All browsers triple the time for no extra coverage.
- **Everything fails the build:** format check, lint with zero warnings, type check, docs link check, convention checks, guard self-tests, unit, integration, build, e2e. NEVER `continue-on-error`, `|| true`, an allowed-to-fail job, automatic retries of failed tests, or a skipped suite. A check that can be red without blocking is a check nobody reads.
- **Integration tests MUST run in CI**, against the service database. A harness that skips without a database is green and tests nothing (`testing`).
- **Upload artifacts on failure:** traces, screenshots, videos and the HTML report, with a retention limit. Screenshots and traces may show test data only (the e2e database is throwaway), never secrets. A red e2e run without them costs a re-run to diagnose.
- **The e2e server's output goes to the job log**, piped through the runner's `webServer` with a prefix, so a startup failure is visible next to the test that waited for it.
- **Least privilege:** `permissions: contents: read` by default; grant more per job only when a step needs it. Workflows from forks get no secrets, and the test suite needs none.
- **Pin third-party actions** to a major version at least (or a commit SHA for anything outside the platform's own actions).
- **Require the CI check on the default branch** (branch protection), so it gates merges, not just informs.

## Job layout

- **Start with one job** that runs exactly the laptop sequence. It's the easiest to reproduce and keeps one set of services.
- **Split when wall-clock time matters**, along the targets: `check` (no services), `test` (database), `e2e` (database, mail catcher, browser). Each job still calls make targets, and each starts only the services it needs. Run them in parallel; don't chain `needs:` unless a later job consumes an earlier artifact.
- **Prefer** cancelling superseded runs on the same branch (`concurrency: { group: ci-${{ github.ref }}, cancel-in-progress: true }`) for pull requests, never for the default branch.
- Set a job `timeout-minutes`, so a hung server or browser fails in minutes instead of hours.

## Debugging CI-only failures

| Symptom | Usual cause |
| --- | --- |
| Integration tests fail to connect | service credentials or database name differ from `.env.testing`; no healthcheck, so tests raced startup |
| Passes locally, fails in CI | a value leaking from the developer's `.env` locally (the test target isn't using the emptied environment), or an unpinned tool version |
| e2e can't launch the browser | missing `--with-deps`, or a `CHROMIUM_PATH` override pointing at a binary that isn't there |
| e2e times out waiting for the server | the build is slow on the runner (raise the web-server timeout to cover the build) or the server failed to start: read the prefixed server log |
| Flaky only in CI | a fixed sleep or a race the slower runner exposes: fix the test (`testing`), never add retries |
