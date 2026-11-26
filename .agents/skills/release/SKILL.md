---
name: release
description: Cut and publish a Lewbox release to GitHub - preflight, the full test suite, the version, release notes in the house structure, the build, the annotated tag, the GitHub release with its four assets, and checks afterwards. Use when the user asks to release, cut, tag or publish a version of Lewbox.
argument-hint: "[vX.Y.Z]"
disable-model-invocation: true
---

# Release Lewbox

A release is what `lewbox update` and the VPS deploy tool (`ship deploy lewbox`) download, so a bad one reaches every server. v0.3.5 went out with a one-line description, a commit subject as its tag message, and none of the sections below. Don't repeat that: **every step here is required, in order**, and nothing is pushed until the user has seen the version and notes.

`$ARGUMENTS` may give the version. If not, propose one in step 4.

## 1. Preflight

Stop and tell the user if any of these fail.

```bash
git switch main && git fetch origin --tags
git status --porcelain                 # must print nothing
git log --oneline main..origin/main    # must print nothing (pull first if it doesn't)
prev=$(git describe --tags --abbrev=0 --match 'v*')   # the last release
git log --oneline "$prev"..HEAD        # must not be empty
gh auth status
```

`$prev` is the last release, including release candidates. For a final release after `-rc.N`, compare with the last *final* release instead: `git describe --tags --abbrev=0 --exclude '*-rc*'`.

## 2. Run every check locally

These are the checks AGENTS.md and CI require, plus the end-to-end runs a production release needs. Run them in this order and keep the output: the **Tested** section quotes real numbers from it.

| Command | What it proves | Record |
|---|---|---|
| `make test` | gofmt, vet, `go test -race ./...`, web typecheck, lint and tests | the web test count (`Tests  N passed`) and any new tests worth naming |
| `make e2e-web` | Playwright smoke tests against the built server | pass/fail |
| `make docker-test` | the Go checks and web tests inside the image | pass/fail |
| `make e2e` | the orchestrator image builds and starts | pass/fail |
| `cd deploy/dev-cluster && LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" ./e2e.sh` | real AerolVM on Docker nodes, including `customers.sh` (~6-10 min, needs privileged containers) | `All N steps passed` and the number of checks |
| `… ./e2e.sh --runtime firecracker` | the same on Firecracker | only if `/dev/kvm` exists **and** the release touches sandboxes, archiving or the node runtime |

- **Any failure stops the release.** Fix it in a normal commit (with the user's go-ahead) and start again at step 1. Never weaken or skip a test to ship.
- If `e2e.sh` can't run here (no Docker, no privileged containers), ask the user before going on. If they say go on, the **Tested** section says it wasn't run and why.
- `e2e.sh` cleans up after itself. If it doesn't, use `./dev.sh down`. **Never** use `docker volume prune` or other host-wide cleanup.

## 3. Push `main` and wait for CI

The tag must point at a commit CI passed on.

```bash
git push origin main                   # if ahead
run=$(gh run list --branch main --workflow CI --commit "$(git rev-parse HEAD)" --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$run" --exit-status      # test, e2e-web and docker must all pass
```

Pushing `main` is part of releasing, so the user's `/release` covers it. A red CI run stops the release just like a local failure.

## 4. Pick the version and gather the changes

Lewbox is pre-1.0:

- **Patch** (`v0.3.4` → `v0.3.5`): fixes and small UI or docs changes only, with no migration and no new commands or flags.
- **Minor** (`v0.3.x` → `v0.4.0`): any new feature, command, flag, endpoint or migration, an AerolVM pin change, or anything an operator must do on upgrade.
- **Release candidate** (`v0.4.0-rc.1`): when the user asks for one. It is published with `--prerelease`.

Collect the facts the notes need. Don't write from memory.

```bash
git log --reverse --format='%h %s' "$prev"..HEAD               # every commit, oldest first
git diff --stat "$prev"..HEAD
git diff --name-only "$prev"..HEAD -- internal/db/migrations    # migrations in this release
git diff "$prev"..HEAD -- internal/machine/source.go | grep PinnedVersion   # AerolVM pin change
git diff --name-only "$prev"..HEAD -- docs/ cmd/ internal/machine/ deploy/
```

Read the diffs of anything user-visible (the CLI, API, console, node-side work) until you can say for each change what an operator saw before and what they see now. If a behavior, flag or message changed and its page under `docs/` or `docs/platform/reference/cli.md` didn't, stop and tell the user: the docs must ship in the same release.

## 5. Write the release notes

Write them to a file outside `release/` (`make release` deletes that folder), for example `notes=$(mktemp -d)/notes.md`. [references/example-v0.3.4.md](references/example-v0.3.4.md) is the model: match its structure and voice exactly.

**Title:** `vX.Y.Z: <what changed, in a few lowercase words>`, for example `v0.3.4: domain-mode clusters get their DNS records and register`. The annotated tag uses the **same string** as its message.

**Body, in this order and with these exact headings:**

1. **An opening paragraph, with no heading.** One to three sentences about what an operator gets from this release. For a fix to an earlier fix, say why the earlier one didn't work.
2. `### Highlights`: **only if the release adds features.** One bullet per feature, starting with a bold sentence saying what's new. Nested bullets for details.
3. `### Fixed`: **only if the release fixes bugs.** One bullet per bug:
   - **Start with a bold sentence describing the symptom as the operator saw it**, with the exact command and error text in backticks (`` **`lewbox node add … --new-cluster` in domain mode still failed with `no such host`.** ``).
   - Then the cause, plainly, then what happens now. Use nested bullets when there are several outcomes.
   - Then any remaining limits ("A private `data_plane_host` … still gets no record").
   - Docs-only corrections go in their own bullet, linking to the page.
4. `### Upgrade notes`: **always.** In this order:
   - The migrations line, always first: `**No database migrations.**`, or name each new migration and say what it changes. The VPS deploy backs up first, migrations don't roll back, and rolling back means restoring the `pre-<tag>` backup.
   - What to update: the orchestrator (`lewbox update` or `ship deploy lewbox`), the CLI on nodes, and `lewbox node configure` when node-side files changed.
   - What to do if the bug already happened, and anything transient the operator may still see (caches, the next check).
   - The AerolVM pin, always last: `AerolVM stays pinned at v0.7.0.` or `AerolVM moves from vA to vB: run lewbox update --aerolvm on each node, one at a time.`
5. `### Tested`: **always, and only with what step 2 actually ran.**
   - `make test`: gofmt, vet, Go tests, and N web tests, followed by the new tests that cover this release (what they check, and "this failed before the fix" where true). Mention new `testdata`.
   - `deploy/dev-cluster/e2e.sh` on Docker nodes: all N steps (M checks).
   - Say plainly what wasn't covered and how it was verified instead (for example "The dev cluster runs in IP mode, so domain mode is verified by the tests above and on a real node"). Don't claim a real-node check the user didn't tell you about.
6. `## What's Changed`: every commit from `$prev..HEAD`, **oldest first**, one line each: `- <commit subject> in <short sha>`.

**Writing rules:**

- Plain words, short sentences, no marketing ("seamless", "robust", "improved experience").
- Commands, flags, paths, field names, HTTP statuses and error text go in backticks, quoted exactly.
- Links to docs are pinned to the tag: `https://github.com/harlew-dev/lewbox/blob/vX.Y.Z/docs/platform/….md#anchor`. Check that each page and anchor exists.
- No other sections: no "Full Changelog" line, no assets table.
- Never include secrets, real customer data, internal hostnames or IPs. Use `203.0.113.x` and `example.com`.

## 6. Get the user's go-ahead

Show the user the version, the title and the full notes, and list the checks that ran with their results. **Wait for their go-ahead.** Tagging and publishing can't be cleanly undone. Apply their edits and show the notes again if they change much.

## 7. Build and check the assets

```bash
make release VERSION=vX.Y.Z      # builds web/dist, then release/: both binaries, release.json, checksums.txt
cat release/release.json         # {"version":"vX.Y.Z","aerolvm_version":"<PinnedVersion>"}
(cd release && sha256sum -c checksums.txt)
./release/lewbox_linux_amd64 version    # must print vX.Y.Z
ls release                       # exactly: checksums.txt lewbox_linux_amd64 lewbox_linux_arm64 release.json
```

Any mismatch stops the release.

## 8. Tag and publish

```bash
title="vX.Y.Z: …"                                  # the same title as step 5
git tag -a vX.Y.Z -m "$title"                       # annotated, on the HEAD that CI passed
test "$(git rev-parse vX.Y.Z^{commit})" = "$(git rev-parse HEAD)"
git push origin vX.Y.Z
gh release create vX.Y.Z \
  release/lewbox_linux_amd64 release/lewbox_linux_arm64 release/release.json release/checksums.txt \
  --verify-tag --title "$title" --notes-file "$notes"      # add --prerelease for -rc.N
```

Never use `git push --tags`, which pushes every local tag. Never use `--generate-notes`.

## 9. Check what was published

```bash
git cat-file -t vX.Y.Z                                   # tag (annotated, not commit)
git tag -l vX.Y.Z --format='%(contents:subject)'         # the title
gh release view vX.Y.Z --json name,isDraft,isPrerelease,assets \
  -q '.name, .isDraft, .isPrerelease, (.assets[].name)'
gh release list --limit 5 --json tagName,isLatest -q '.[] | select(.isLatest) | .tagName'   # vX.Y.Z (not for an rc)
d=$(mktemp -d) && gh release download vX.Y.Z -D "$d" && (cd "$d" && sha256sum -c checksums.txt && cat release.json)
XDG_CONFIG_HOME="$d/cfg" XDG_CACHE_HOME="$d/cache" ./release/lewbox_linux_amd64 update --check   # "latest":"vX.Y.Z"
```

The release must be named with the title, not a draft, marked latest (or a pre-release for an rc), and have exactly four assets whose checksums pass. Then give the user the release URL and the upgrade notes they need to act on.

## Fixing a release that's already out

- **Wrong notes or title:** `gh release edit vX.Y.Z --title "…" --notes-file …`. This is safe at any time.
- **A broken binary, or a wrong tag:** never move or delete a published tag, and never replace its assets. Servers may already have them, and `checksums.txt` would change under them. Fix forward with the next patch release, and have its opening paragraph say what was wrong with the last one.

## Deploying

Publishing doesn't deploy. Only deploy when the user asks, as a separate step: `ship deploy lewbox vX.Y.Z` from `~/dev/deploy` for the orchestrator (see that repo's README), then `lewbox update` (and `lewbox node configure` or `lewbox update --aerolvm` if the upgrade notes say so) on each node.
