# Plan template and lifecycle

Read when writing, confirming, building against or closing a plan. When a plan is required at all is set in `conventions` §5. How plans queue up is in [plans-index.md](plans-index.md).

## When a plan is required

Before any code, for:
- a new feature;
- a new table, package, event, listener or endpoint family;
- a new dependency;
- a change to auth, security, rate limits, or how layers fit together;
- a convention change (renaming across layers, or moving files by a new rule).

Not required: bug fixes and changes within an existing design. Small items from the user's notes can be built as one batch without a plan ([intake-and-triage.md](intake-and-triage.md)).

## Lifecycle

| State | Status line | What happens next |
| --- | --- | --- |
| Draft | `Status: **draft 2026-11-01**` | The planner writes every section. Each open question goes in **Decisions**, with a recommendation |
| Questions asked | `Status: **draft 2026-11-01**, decisions to confirm` | The user answers in conversation ([roles/planner.md](roles/planner.md)) |
| Confirmed | `Status: **confirmed 2026-11-01**` (or `confirmed 2026-11-01, as recommended (overnight run)`) | Decisions retitled and dated (below). Now, and only now, code may be written |
| Building | `Status: **building**, phase 2 of 3 (phase 1: \`10797fd\`)` | The build loop in SKILL.md §4 |
| Implemented | `Status: **implemented 2026-11-02** (decisions 1 and 3 changed by the user; see As built)` | **As built** written, docs linked, index row updated, `git mv` to `implemented/` |

Rules:
1. **No code before the decisions are confirmed.** The only exception is auto mode, where every decision already has a recorded default (SKILL.md §5).
2. **Re-read the plan before building.** A plan describes the tree on the day it was written. Recheck the tree for code added since then, and rename or adapt that code the same way.
3. **A decision that changes during the build** is changed in place, with its new date, and noted in As built and the findings file.
4. **Implemented plans are history.** Later renames never rewrite them, and their old names stay. The same goes for findings files and `read/` notes.
5. **Large features use a folder**: `.agents/plans/features/<feature>/README.md` with its own ordered table and shared rules, plus one file per part. Each part keeps every section below ([plans-index.md](plans-index.md)).
6. **Large plans are built in phases.** Each phase is reviewed and committed on its own. The plan moves to `implemented/` only after the last phase.

## Template

Every section is present in every plan. For a section that doesn't apply, write `None.` and the reason (for example `Optional: none; the feature is a single toggle`). That way a reader can tell "considered and empty" from "forgotten".

The scope sections come from the user's own way of thinking about features: Overview, Features, MVP, Optional, Future features and Security. The engineering sections make the plan buildable and reviewable: Why, Design, API and data, Tests, Docs, Decisions and Not included. Keep both sets.

```markdown
# <Feature name>

Status: **draft 2026-11-01**
Needs: <plan numbers or names that must ship first, or "none">. Follows: [<related plan>](…). Absorbs: [<older plan>](…).

## Overview

The user's idea, in their own words where possible (quote their note), then one paragraph restating it
precisely. This is what the user reads to check you understood them.

## Why

The problem in concrete terms: what's wrong or missing today, with real examples
(`POST /projects` is `createProject` here and `projects.add()` there).
What it costs (bugs, confusion, risk). What "done" looks like, in one paragraph.

## Features

Numbered list of every capability the user wants, each one line and testable
("An owner can revoke a pending invitation; the link then answers 404").

## MVP

Which of the Features (by number) ship in this plan's build, and the one-sentence test of "done"
for the MVP. Anything not listed here is not built, even when it's easy.

## Optional

Nice-to-haves. Each one is built only if a Decision row says so. Otherwise it moves to Future features.

## Future features

Wanted, but not now. Each becomes a later plan, a `.agents/plans/future/` plan, or a "Later, not planned yet"
line in the feature folder's README. Name which.

## Design

The rule or flow being introduced, stated so someone could apply it without you.
Tables for vocabularies, numbered steps for flows, short code for shapes.
Name the files and modules involved and where new code goes (`structure`).
Names come from `naming`.

## API and data changes

- Endpoints added or changed: method, path, action, permission, rate limit, status codes (`api`).
- Tables and columns added, each with its data-map row (personal data, purpose, retention, who sees it).
- Events added. Config added: name, default, and the production rule
  (for example, "production refuses to start without it").
- Migrations: the numbers this plan will use (its range, when parallel lines run; see parallel-worktrees.md).

### Changes to existing code (refactors and renames)

As of <date>. Recheck <files> for anything added since before starting.

| Where | Now | After |
| --- | --- | --- |
| `lib/api/projects.<ext>` | `create` | `store` |

If it's true, say "This is a rename only. No behaviour, response, SQL or URL changes."

## Security

### The user's notes

Anything the user said about security, quoted. "None given" if none.

### Risks and how they're avoided

| Risk | Control |
| --- | --- |
| Revealing whether an email has an account | The same answer, status and timing for both; a test compares them (`security`) |

Write "None new" only with the reason (no URL, header, loader or API change), and name the existing
tests that guard the boundary.

## Tests

- New tests: behaviour, security boundaries, real persistence (`testing`). For a new check, "see it fail on today's tree first".
- Existing suites that must pass unchanged.
- Commands to run: test, check, build, e2e.
- Screenshots to capture (screens, dialogs; light and dark; desktop and phone).

## Docs

Every doc that changes, and what changes in it. New docs and their index rows.

## Decisions

| # | Question | Recommendation |
| --- | --- | --- |
| 1 | Where do the tests go? | Beside the page they test; cross-area tests at the area root |
| 2 | Build Optional item 1 (CSV export)? | No: a Future feature. Nobody has asked for exports yet |

## Not included

What this plan deliberately leaves out and does not intend to build, each with a one-line reason
or a link to the plan that covers it. (Future features are things that will be built later. Not included is everything else.)
```

## Writing good Decisions

- **One question per row.** State it so the user can answer "yes" or pick an option without opening the code.
- **Every row has a recommendation and its reason.** "Up to you" isn't a recommendation. In auto mode the recommendation *is* what gets built.
- **Name the consequence the user would notice**, for example "**Today" and "Yesterday" are UTC days**, or "production refuses to start without `CAPTCHA_PROVIDER`". Bold the few rows worth a look, so the user can skim a 22-row table.
- **The user's own asks are already decided.** If the user said "a Filters dialog with badges", that row reads "Decided (your note)". It isn't re-asked.
- **Don't ask what the docs answer.** A question whose answer is a written rule is a design statement, not a decision.
- **Prefer the reversible, safer option** as the recommendation when the choice is close.

## Recording decisions

After the user answers, retitle the section and fill it in. The general rule (date, keep overrides visible) is `conventions` §6. In a plan, it looks like this:

```markdown
## Decisions (confirmed 2026-11-01)

| # | Question | Decision |
| --- | --- | --- |
| 1 | Account settings page | **Moves like every page** (`settings/index.<ext>`). (Original recommendation — stay a single file until it gets tabs — replaced.) |
| 2 | Where tests go | **Beside the page they test.** |
```

In auto mode, the heading says how the decisions were confirmed: `## Decisions (confirmed 2026-11-01, as recommended)`. Every row is then a default the user can still overturn. `read/questions.md` lists the ones worth a look.

- The date is when the user confirmed, not when you wrote the plan.
- An override keeps the original recommendation in its row, marked replaced, so nobody "fixes" it back.

## As built

Added when the plan is closed. It is never left out: a missing As built means nobody checked.

```markdown
## As built

Built as planned. Small differences:

- <What differs, and why> (for example, a local name collided, so the list became `MemberList`).
- <Anything found and fixed along the way.>
- Phases: 1 `10797fd`, 2 `9ae4822`, 3 `ec5ed7d`.

Findings: [findings/<plan>.md](…). Docs updated: [naming.md](…), [endpoints.md](…), AGENTS.md rule 4.
```

"Built as planned" is fine when it's true. Every difference listed here is also listed in the findings file's "Where the build differs from the plan" section.

## Closing a plan

1. Set `Status: implemented <date>`, noting any user-changed decisions.
2. Write As built and link the docs that were updated.
3. Update the plan's row in the index ([plans-index.md](plans-index.md)): Status with the date, plus any deviation ("implemented 2026-10-31, before plan 2").
4. `git mv .agents/plans/features/<name>.md .agents/plans/features/implemented/<name>.md` and fix the relative links to and from it. The docs link check catches any you miss.
5. Do this in the plan's final commit (or its last phase's commit), not as a separate tidy-up later.
