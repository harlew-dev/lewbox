# The findings file (a plan's build report)

Read when writing or updating the findings for a plan, a phase, or a batch of the user's notes.

Every plan built by agents gets `findings/<plan>.md`, beside the `read/` folder it reports into ([plans-index.md](plans-index.md#layout)). It is the plan's build report: what shipped, where the build differs, what the user must decide or configure, and the independent security review. The plan says what was intended. The findings file says what happened.

**Why a separate file and not just the plan:** the plan stays readable as a design. The findings file can be long, with commit tables, severity tables and exact check counts, and it keeps the reviewer's words apart from the builder's.

## Who writes what

| Section | Written by |
| --- | --- |
| Header, What shipped, Where the build differs, Decisions made while building, Stronger than the plan asked, Fixed without asking, What you must configure, Risks that remain, For later plans, Environment notes | Builder drafts, orchestrator checks against the diff |
| For you to decide | Orchestrator (also copied to `read/questions.md`) |
| Checks | Orchestrator, from runs it watched itself |
| Security review | Pasted from the security reviewer's output, unedited apart from the Resolution column and Fix results |

A section with nothing in it is **left out**, unlike the plan, where every section is kept. "Security review" and "Checks" are never left out.

## Sections, in order

```markdown
# Findings: <plan id>, <plan title>

Plan: [<plan>](../../implemented/<plan>.md). Built on the <line> worktree (`../app-<line>`, branch `run/<line>`)
[, with every recommendation in its Decisions table taken as accepted]. Screenshots are in [<plan>/](../../screenshots/<plan>/).

## What shipped

A summary list or table of the user-visible result, then the commits:

| Commit | What |
| --- | --- |
| `ae94bda` | Migrations 060 and 061, factories, store, events |
| `6186fa3` | The API and its enforcement |
| `ba95925` | Web: dialogs and the new page |
| `0ddd6f5` | Docs; the plan implemented and moved |

Screenshot links, each described: [the suspend dialog](…), [on a phone](…).

## Where the build differs from the plan (recorded in the plan)

Every deviation, each with its reason. Each one is also in the plan's As built.

## Decisions made while building (worth your review)

Choices the plan didn't settle, which the orchestrator or builder made. Each with the alternative.

## Stronger than the plan asked

Extra protections added, so the user knows they exist and why. For example, "suspension also blocks new sessions, under the same row lock".

## For you to decide

Numbered. Each has the question, the default in place now, and the recommendation with its reason.

## Fixed without asking

Small defects found and fixed along the way. Each with what it was and the commit.

## What you must configure

Anything that changes deployment, in bold. For example, "**Production refuses to start unless `CAPTCHA_PROVIDER` is set.** Add it before deploying."

## Risks that remain (accepted by the plan, or noted)

Known weaknesses that weren't fixed, each with why that's acceptable.

## For later plans

What the next builder must know: renamed APIs ("`paths.admin` is now `adminPaths`"), open questions handed on, and constraints.
Also correct the later plans' text if it uses the old names.

## Environment notes

Anything about this machine that affected the build (a missing browser path, a port held by another process).
Copy anything that isn't specific to this plan into `read/discoveries.md`.

## Checks

Exact commands and exact results, never "all green":
- `check`;
- `test`: 21 backend packages, 353 frontend tests;
- `build`;
- e2e: 28 of 28.
Flakes are named: "Vitest 341, after one flaky run of an untouched test that passed alone 3 of 3 times".

## Security review

(the reviewer's output: see below)
```

**Phased plans** repeat the per-build sections under `## Phase 1`, `## Phase 2` and so on (`### What shipped`, `### Where the build differs`, `### Security review`). A phase's review fixes get their own heading: `## Phase 1 fixes (\`f017cfe\`..\`d82d331\`)`.

**Builds from the user's notes** (a batch of small items, not a plan) use a per-note table instead of What shipped:

```markdown
## Per note

| Note | Commit | What changed | Screenshot |
| --- | --- | --- | --- |
| "Forgot password" under the password input | `88621aa` | It's now the field's description, 12px below the input | [login](…) |
```

They add two sections ([intake-and-triage.md](intake-and-triage.md)):
- **Where a note was adapted to a written rule**: the note, the rule, and what was built instead.
- **Corrected after your notes were misread**: what the builder first understood, what was done, and "tell me if you only wanted X".

**The builder's own account of security** may appear as its own section, but it must be labelled: `## How the security points are met (the builder's account; the independent review follows)`. The two must never be mixed.

## The Security review section

The reviewer's output has three parts, in this order.

**1. Confirmed sound.** A short list of what the reviewer probed and found correct, with how it checked:

> It probed `/admin-assets/` with an out-of-tree router test and found the refusals byte-identical to `GET /nowhere`: no pass, a bad pass, an unknown file, a directory, HEAD, and Range without a pass.

This is just as important as the findings. It tells the user (and a later reviewer) which guarantees were actually tested, not assumed.

**2. The severity table.**

```markdown
| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | **Medium** | **Bots could lock everyone out without solving anything.** A challenged request still counted against the shared limits. The probe needed about 21 addresses to block every real password reset for the hour | Above a challenge level, a request counts against a shared limit only once it has passed |
| 2 | Low | A redirect from the verification endpoint would have re-sent the secret | Redirects aren't followed |
| 3–5 | Nits | Wording; a plain-object lookup; docs on the shared budget | Fixed |
```

| Severity | Meaning |
| --- | --- |
| **High** | Exploitable now: an auth or authorisation bypass, data exposure across tenants, an account takeover, or one attacker denying a feature to everyone. Blocks the commit |
| **Medium** | Exploitable under realistic conditions, or the feature's security goal isn't met. Fixed before the commit unless the user accepts it |
| Low | Defence in depth, a configuration trap, a narrow timing or race window, misleading logs |
| Nit | Wording, style, harmless edge cases |

Rules for the table:
- **The Finding states the attack and the evidence**: "confirmed by a probe", "confirmed by SQL", "with 0 calls to the provider". A finding without evidence says "(likely)".
- **Qualifiers in the Severity column** are allowed and useful: `Medium (likely)`, `Medium (older)` (it predates this change, so it isn't a regression), `Low (dev only)`, `Low–medium`.
- **Bold the High and Medium rows**, and the first sentence of their Finding.
- **The Resolution column** is filled in by the orchestrator after the fixes: what changed, or "Left as is" or "Accepted" with the reason, or "**Your call**" (which is then also in `read/questions.md`).
- **Group nits** into one row (`7–10 | Nits`).

**3. Fix results.** A commit table for the fixes, then what the new tests replay (the reviewer's attack, now refused), then the full checks again with counts:

```markdown
**Fix results:**

| Commit | Fix |
| --- | --- |
| `8143cff` | Shared limits keep a separate count of requests that passed the challenge |

What the new tests replay: the reviewer's attack (101 unsolved requests from 21 addresses) now blocks nobody.
Each guard was shown to fail by planting the bypass, which was then reverted.
All checks passed: `check`, `test` (355 frontend tests), `build`, e2e 28 of 28.
```

**A second review** goes under its own heading, `### Second review of <the fix>`, with its own table. It is required whenever a fix adds new security logic (new counting, a new guard, a new token check), because fixes are written fast and under pressure. In one real run, the second review of a rate-limit fix found a **High** bypass that the fix itself had introduced.
