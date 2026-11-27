# Releasing Lewbox

Every release follows the generic `release` skill (from [harrisonclewis/skills](https://github.com/harrisonclewis/skills): `/skills:release` as the Claude Code plugin, `/release` through `npx skills add`), with the facts below. Never publish by hand.

## What a release is

A release is what `lewbox update` and the VPS deploy tool (`ship deploy lewbox`) download, so a bad one reaches every server. v0.3.5 went out with a one-line description, a commit subject as its tag message, and none of the notes' sections. Don't repeat that.

## Checks

These are the checks AGENTS.md and CI require, plus the end-to-end runs a production release needs. Run them in this order.

| Command | What it proves | Record |
|---|---|---|
| `make test` | gofmt, vet, `go test -race ./...`, web typecheck, lint and tests | the web test count (`Tests  N passed`) and any new tests worth naming |
| `make e2e-web` | Playwright smoke tests against the built server | pass/fail |
| `make docker-test` | the Go checks and web tests inside the image | pass/fail |
| `make e2e` | the orchestrator image builds and starts | pass/fail |
| `cd deploy/dev-cluster && LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" ./e2e.sh` | real AerolVM on Docker nodes, including `customers.sh` (~6-10 min, needs privileged containers) | `All N steps passed` and the number of checks |
| `… ./e2e.sh --runtime firecracker` | the same on Firecracker | conditional: only if `/dev/kvm` exists **and** the release touches sandboxes, archiving or the node runtime |

`e2e.sh` cleans up after itself. If it doesn't, use `./dev.sh down`. **Never** use `docker volume prune` or other host-wide cleanup.

## CI

Workflow name: `CI` (`.github/workflows/ci.yml`). The `test`, `e2e-web` and `docker` jobs must all pass on the release commit. CI doesn't run the dev-cluster e2e (it needs privileged containers and KVM), so that comes from the local run.

## Versioning

Lewbox is pre-1.0:

- **Patch** (`v0.3.4` → `v0.3.5`): fixes and small UI or docs changes only, with no migration and no new commands or flags.
- **Minor** (`v0.3.x` → `v0.4.0`): any new feature, command, flag, endpoint or migration, an AerolVM pin change, or anything an operator must do on upgrade.
- **Release candidate** (`v0.4.0-rc.1`): when the user asks for one.

## Facts

```bash
git diff --name-only "$prev"..HEAD -- internal/db/migrations    # migrations in this release
git diff "$prev"..HEAD -- internal/machine/source.go | grep PinnedVersion   # AerolVM pin change
git diff --name-only "$prev"..HEAD -- docs/ cmd/ internal/machine/ deploy/
```

User-visible behavior lives in the CLI, the API, the console and node-side work. When a behavior, flag or message changes, its page under `docs/` and `docs/platform/reference/cli.md` must change in the same release.

Doc links are pinned to the tag: `https://github.com/harlew-dev/lewbox/blob/vX.Y.Z/docs/platform/….md#anchor`.

The model for the notes is the v0.3.4 release (`gh release view v0.3.4`), which the skill also carries as its example.

## Upgrade notes

In this order:

1. The migrations line, always first: `**No database migrations.**`, or name each new migration and say what it changes. The VPS deploy backs up first, migrations don't roll back, and rolling back means restoring the `pre-<tag>` backup.
2. What to update: the orchestrator (`lewbox update` or `ship deploy lewbox`), the CLI on nodes, and `lewbox node configure` when node-side files changed.
3. What to do if the bug already happened, and anything transient the operator may still see (caches, the next check).
4. The AerolVM pin, always last: `AerolVM stays pinned at v0.7.0.` or `AerolVM moves from vA to vB: run lewbox update --aerolvm on each node, one at a time.`

In **Tested**, the dev cluster runs in IP mode, so domain mode is covered only by the unit tests unless the user says they checked a real node.

## Build

```bash
make release VERSION=vX.Y.Z      # builds web/dist, then release/: both binaries, release.json, checksums.txt
cat release/release.json         # {"version":"vX.Y.Z","aerolvm_version":"<PinnedVersion>"}
(cd release && sha256sum -c checksums.txt)
./release/lewbox_linux_amd64 version    # must print vX.Y.Z
ls release                       # exactly: checksums.txt lewbox_linux_amd64 lewbox_linux_arm64 release.json
```

`make release` deletes `release/`, so keep the notes file elsewhere.

## Publish

GitHub release assets, exactly these four:

```bash
release/lewbox_linux_amd64 release/lewbox_linux_arm64 release/release.json release/checksums.txt
```

No registry: the GitHub release is the only place Lewbox is published.

## After publishing

```bash
d=$(mktemp -d) && gh release download vX.Y.Z -D "$d" && (cd "$d" && sha256sum -c checksums.txt && cat release.json)
XDG_CONFIG_HOME="$d/cfg" XDG_CACHE_HOME="$d/cache" ./release/lewbox_linux_amd64 update --check   # "latest":"vX.Y.Z"
```

## Deploying

Only when the user asks: `ship deploy lewbox vX.Y.Z` from `~/dev/deploy` for the orchestrator (see that repo's README), then `lewbox update` (and `lewbox node configure` or `lewbox update --aerolvm` if the upgrade notes say so) on each node.
