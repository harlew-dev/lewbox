# Release guide template

Copy this to `RELEASING.md` at the project root and fill in every section from the repository: the Makefile or task runner, the CI workflows, the build scripts, previous tags and release notes. Delete a section only when it truly doesn't apply, and say so in one line instead ("No registry: the GitHub release is the only place it is published."). The `release` skill reads this file before every release, so write facts and commands, not process: the process is in the skill.

```markdown
# Releasing <project>

Every release follows the `release` skill, with the facts below. Never publish by hand.

## What a release is

Who downloads it and how (an update command, a deploy tool, a package registry), so a bad one reaches
<whom>. Anything that has gone wrong in a past release and must not happen again.

## Checks

Run in this order. **Record** is what the Tested section quotes.

| Command | What it proves | Record |
|---|---|---|
| `make test` | … | the test counts |
| … | … | pass/fail |

Conditional checks, and when they run: …
How to clean up after a check that leaves things behind: …

## CI

Workflow name: `CI`. Jobs that must pass on the release commit: …

## Versioning

What makes a patch, a minor and a major here (schema migrations, new commands or flags, pinned
dependency changes, anything users must do on upgrade). Whether release candidates are used.

## Facts

Extra commands to run when gathering changes:

    git diff --name-only "$prev"..HEAD -- <migrations dir>     # migrations in this release
    git diff "$prev"..HEAD -- <file holding a pinned version>   # pin changes
    git diff --name-only "$prev"..HEAD -- <paths with user-visible behavior>

Docs that must change with behavior, and where they live: …
Base URL for doc links pinned to the tag: `https://github.com/<owner>/<repo>/blob/vX.Y.Z/…`

## Upgrade notes

What this section must say, in order (first and last items are fixed):
1. Migrations / breaking changes: …
2. What to update, and in what order: …
3. …
4. Pinned dependencies: `<dependency> stays pinned at vA.` or what to run when it moves.

## Build

    make release VERSION=vX.Y.Z

Output folder: `release/` (deleted by the build, so keep notes elsewhere). It must hold exactly:
<file list>. Checks on the output: …

## Publish

GitHub release assets, in this order: …
Registry publish, if any: …

## After publishing

Project-specific checks on what was published (an update check, an install from the registry
into a temp dir): …

## Deploying

Only when the user asks: …
```
