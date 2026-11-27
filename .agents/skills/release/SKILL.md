---
name: release
description: Cut and publish a release of any package or app - preflight, every check the project requires, CI on the release commit, the version, release notes in one fixed structure (opening paragraph, Highlights, Fixed, Upgrade notes, Tested, What's Changed), the user's go-ahead, the build, the annotated tag, the GitHub release and registry publish, and checks afterwards. Reads the project's own facts (checks, assets, version rules, upgrade notes) from its RELEASING.md, and writes one with the user if it is missing. Use when the user asks to release, cut, tag or publish a version.
argument-hint: "[vX.Y.Z]"
disable-model-invocation: true
---

# Release

A release is what users and servers download, so a bad one reaches all of them, and a tag can't be cleanly taken back. The failure this skill prevents is a release that goes out with a one-line description, a commit subject as its tag message, untested assets or no upgrade notes. **Every step here is required, in order**, and nothing is pushed until the user has seen the version and notes.

`$ARGUMENTS` may give the version. If not, propose one in step 4.

## 0. Read the project's release guide

The steps below are the same for every project. What differs (which checks to run, what gets built, what counts as a minor version, what the upgrade notes must say, where it is published, how it is deployed) lives in the project's **release guide**: `RELEASING.md` at the repository root, unless `AGENTS.md` or `CLAUDE.md` points somewhere else. Read it in full before step 1. Where it is more specific than this skill, it wins.

If there is no guide, **stop**. Offer to write one from [references/release-guide-template.md](references/release-guide-template.md), filled in from the repository (the Makefile or task runner, the CI workflows, the build scripts, previous tags and release notes) rather than from memory. Show it to the user and have them confirm it before the first release that uses it. Commit it as its own change, with their go-ahead.

## 1. Preflight

Stop and tell the user if any of these fail. Use the guide's release branch if it isn't `main`.

```bash
git switch main && git fetch origin --tags
git status --porcelain                 # must print nothing
git log --oneline main..origin/main    # must print nothing (pull first if it doesn't)
prev=$(git describe --tags --abbrev=0 --match 'v*')   # the last release
git log --oneline "$prev"..HEAD        # must not be empty
gh auth status
```

`$prev` is the last release, including release candidates. For a final release after `-rc.N`, compare with the last *final* release instead: `git describe --tags --abbrev=0 --match 'v*' --exclude '*-rc*'`. For the first release ever there is no `$prev`; use the root commit and say so in the notes.

## 2. Run every check locally

Run every check in the guide's **Checks** table, in its order, and keep the output: the **Tested** section quotes real numbers from it (test counts, steps passed).

- **Any failure stops the release.** Fix it in a normal commit (with the user's go-ahead) and start again at step 1. Never weaken or skip a test to ship.
- A check the guide marks as conditional runs when its condition holds. Say in **Tested** which conditional checks didn't run and why.
- If a required check can't run on this machine (no Docker, no device, no credentials), ask the user before going on. If they say go on, **Tested** says it wasn't run and why.
- Clean up the way the guide says. Never run host-wide cleanup (`docker system prune`, `docker volume prune`) on a shared machine.

## 3. Push the branch and wait for CI

The tag must point at a commit CI passed on.

```bash
git push origin main                   # if ahead
run=$(gh run list --branch main --workflow "<CI workflow from the guide>" --commit "$(git rev-parse HEAD)" --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$run" --exit-status
```

Pushing the release branch is part of releasing, so the user's release request covers it. A red CI run stops the release just like a local failure. If the project has no CI, say so in **Tested**.

## 4. Pick the version and gather the changes

Use semantic versioning, with the guide's rules for what bumps what. Without them:

- **Pre-1.0:** patch (`v0.3.4` → `v0.3.5`) for fixes and small docs or UI changes only; minor (`v0.3.x` → `v0.4.0`) for any new feature, command, flag, endpoint, schema migration, pinned-dependency change, or anything a user must do on upgrade. Breaking changes are minor too, and the upgrade notes say so plainly.
- **1.0 and later:** patch for fixes, minor for backwards-compatible features, major for anything that breaks a user.
- **Release candidate** (`v0.4.0-rc.1`): only when the user asks for one. It is published as a pre-release.

Collect the facts the notes need. Don't write from memory.

```bash
git log --reverse --format='%h %s' "$prev"..HEAD     # every commit, oldest first
git diff --stat "$prev"..HEAD
```

Then run the guide's extra **Facts** commands (migrations in this release, pinned-version changes, the paths that hold user-visible behavior).

Read the diffs of anything user-visible until you can say, for each change, what a user saw before and what they see now. If a behavior, flag, option or message changed and its documentation didn't, stop and tell the user: the docs must ship in the same release.

## 5. Write the release notes

Write them to a file outside the build output folder (the build may delete it), for example `notes=$(mktemp -d)/notes.md`. [references/example-notes.md](references/example-notes.md) is the model: match its structure and voice. If the guide names an example of its own, that one wins.

**Title:** `vX.Y.Z: <what changed, in a few lowercase words>`, for example `v0.3.4: domain-mode clusters get their DNS records and register`. The annotated tag uses the **same string** as its message.

**Body, in this order and with these exact headings:**

1. **An opening paragraph, with no heading.** One to three sentences about what a user gets from this release. For a fix to an earlier fix, say why the earlier one didn't work.
2. `### Highlights`: **only if the release adds features.** One bullet per feature, starting with a bold sentence saying what's new. Nested bullets for details.
3. `### Fixed`: **only if the release fixes bugs.** One bullet per bug:
   - **Start with a bold sentence describing the symptom as the user saw it**, with the exact command and error text in backticks.
   - Then the cause, plainly, then what happens now. Use nested bullets when there are several outcomes.
   - Then any remaining limits.
   - Docs-only corrections go in their own bullet, linking to the page.
4. `### Upgrade notes`: **always.** The guide lists what goes here and in what order. Without one:
   - The migrations or breaking-changes line, always first: `**No database migrations.**` / `**No breaking changes.**`, or name each one and say what it changes and how to roll back.
   - What to update, and in what order, when there's more than one piece.
   - What to do if the bug already happened, and anything transient the user may still see.
   - Pinned dependencies, always last: say they stay, or what moves and what to run.
5. `### Tested`: **always, and only with what step 2 and step 3 actually ran.**
   - Each check with its real numbers, followed by the new tests that cover this release (what they check, and "this failed before the fix" where true). Mention new fixtures or test data.
   - Say plainly what wasn't covered and how it was verified instead. Don't claim a manual or real-environment check the user didn't tell you about.
6. `## What's Changed`: every commit from `$prev..HEAD`, **oldest first**, one line each: `- <commit subject> in <short sha>`.

**Writing rules:**

- Plain words, short sentences, no marketing ("seamless", "robust", "improved experience").
- Commands, flags, paths, field names, HTTP statuses and error text go in backticks, quoted exactly.
- Links to docs are pinned to the tag (`https://github.com/<owner>/<repo>/blob/vX.Y.Z/docs/….md#anchor`), not to `main`. Check that each page and anchor exists at that commit.
- No other sections: no "Full Changelog" line, no assets table, no contributor list unless the guide asks for one.
- Never include secrets, real customer data, internal hostnames or IPs. Use `203.0.113.x` and `example.com`.

## 6. Get the user's go-ahead

Show the user the version, the title and the full notes, and list the checks that ran with their results. **Wait for their go-ahead.** Tagging and publishing can't be cleanly undone. Apply their edits and show the notes again if they change much.

## 7. Build and check the assets

Run the guide's **Build** command with the version, then its checks on the output. At least:

- the artifact folder holds **exactly** the files the guide lists, no more and no fewer;
- checksums verify (`(cd <dir> && sha256sum -c checksums.txt)`) when the project publishes them;
- each built artifact reports the new version (`<binary> version`, the packed `package.json`, the wheel's metadata).

Any mismatch stops the release.

## 8. Tag and publish

```bash
title="vX.Y.Z: …"                                  # the same title as step 5
git tag -a vX.Y.Z -m "$title"                       # annotated, on the HEAD that CI passed
test "$(git rev-parse vX.Y.Z^{commit})" = "$(git rev-parse HEAD)"
git push origin vX.Y.Z
gh release create vX.Y.Z <the guide's assets, if any> \
  --verify-tag --title "$title" --notes-file "$notes"      # add --prerelease for -rc.N
```

Then run the guide's registry publish (`npm publish`, `cargo publish`, `twine upload`, an image push), if it has one, after the tag and GitHub release succeed.

Never use `git push --tags`, which pushes every local tag. Never use `--generate-notes`. If the tag is pushed by CI or a release workflow instead, follow the guide, but the notes, the go-ahead and step 9 still apply.

## 9. Check what was published

```bash
git cat-file -t vX.Y.Z                                   # tag (annotated, not commit)
git tag -l vX.Y.Z --format='%(contents:subject)'         # the title
gh release view vX.Y.Z --json name,isDraft,isPrerelease,assets \
  -q '.name, .isDraft, .isPrerelease, (.assets[].name)'
gh release list --limit 5 --json tagName,isLatest -q '.[] | select(.isLatest) | .tagName'   # vX.Y.Z (not for an rc)
d=$(mktemp -d) && gh release download vX.Y.Z -D "$d" && (cd "$d" && sha256sum -c checksums.txt)
```

Then run the guide's **After publishing** checks (for example, the app's own update check, or installing the package from the registry into a temporary directory). Point any config or cache directories at temp dirs so nothing on this machine changes.

The release must be named with the title, not a draft, marked latest (or a pre-release for an rc), and have exactly the guide's assets with passing checksums. Then give the user the release URL and the upgrade notes they need to act on.

## Fixing a release that's already out

- **Wrong notes or title:** `gh release edit vX.Y.Z --title "…" --notes-file …`. This is safe at any time.
- **A broken artifact, or a wrong tag:** never move or delete a published tag, never replace its assets, and never unpublish from a registry that forbids reusing a version. Users may already have them, and the checksums would change under them. Fix forward with the next patch release, and have its opening paragraph say what was wrong with the last one.

## Deploying

Publishing doesn't deploy. Only deploy when the user asks, as a separate step, the way the guide's **Deploying** section says.
