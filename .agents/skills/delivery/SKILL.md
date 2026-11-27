---
name: delivery
description: How a feature goes from the user's idea to a merged commit when AI agents do the building. Covers triaging raw notes, the plan template and lifecycle, the plans index as a build queue, and the build loop (builder, orchestrator diff check, blind security review, fixes, one heavy run at a time, one commit per plan). Also covers overnight/auto mode with recorded defaults, the findings file, the read/ hand-off folder, parallel worktrees, screenshot evidence, the agent roles (planner, builder, security reviewer, docs updater, committer), push and PR. Use when the user hands over notes or an idea, asks for a plan, says "build this plan", "build everything overnight", "run these in parallel", "review this" or "report back", when orchestrating sub-agents, or when closing a plan, committing or opening a PR.
---

# Delivery workflow

Agents build fast and report confidently. The failures this workflow prevents come from that:
- **The wrong thing gets built**, because a decision was assumed: hence confirmed Decisions before code.
- **Work is graded by its own author**: hence the orchestrator reading the diff, and a reviewer who never sees the builder's summary.
- **The user comes back to 40 commits and no idea what was decided**: hence findings files and the `read/` folder.
- **Parallel lines collide**: hence worktrees with their own database, migration range and test budget.

Every rule below answers one of these.

Owned elsewhere, and not repeated here:
- when a plan is required, reading docs first, recording decisions and renames: `conventions`;
- shared-machine etiquette, destructive actions and completion reports: [conventions/references/agent-etiquette.md](../conventions/references/agent-etiquette.md);
- test suites and e2e isolation: `testing`;
- the commands, env files and the local database: `tooling`;
- what the security reviewer checks against: `security`.

## When to use

- The user hands over a notes file, an idea, or "make a plan for X".
- Writing, confirming, building against or closing a plan; picking the next plan to build.
- "Build this plan", "build every open plan overnight", "run these in parallel".
- Orchestrating sub-agents: briefing a builder, a reviewer, a docs updater or a committer.
- Writing findings, or updating the `read/` folder.
- Committing a plan, pushing, or opening a PR.

Deep detail:
- [references/plan-template.md](references/plan-template.md): read when writing, confirming or closing a plan. The lifecycle, the full template (Overview, Why, Features, MVP, Optional, Future features, Design, API and data, Security, Tests, Docs, Decisions, Not included), recording decisions, As built.
- [references/plans-index.md](references/plans-index.md): read when adding a plan to the queue, choosing the next one, writing a feature folder, or using `future/`.
- [references/intake-and-triage.md](references/intake-and-triage.md): read when the user hands over raw notes or ideas.
- [references/build-report.md](references/build-report.md): read when writing a plan's findings file (its fixed sections and the severity table).
- [references/read-folder.md](references/read-folder.md): read before an unattended run, and whenever its state changes (templates for `questions.md`, `progress.md`, `discoveries.md`).
- [references/parallel-worktrees.md](references/parallel-worktrees.md): read before running more than one plan at a time.
- [references/roles/](references/roles/): read before briefing a role. One file each for the [planner](references/roles/planner.md), [builder](references/roles/builder.md), [security reviewer](references/roles/security-reviewer.md), [docs updater](references/roles/docs-updater.md) and [committer](references/roles/committer.md), with inputs, outputs, what each must never see or do, and a prompt skeleton.

## The pipeline at a glance

| # | Stage | Who | Output | Gate to the next stage |
| --- | --- | --- | --- | --- |
| 1 | Intake | planner | triage table: every note routed | every note accounted for |
| 2 | Plan | planner | `.agents/plans/features/<name>.md`, index row | every template section present |
| 3 | Questions | planner, with the user | Decisions with recommendations | **confirmed by the user**, or auto mode with every default recorded |
| 4 | Line | orchestrator | worktree, branch, dev DB, migration range | none of this shared with another line |
| 5 | Build | builder | code, tests, docs, screenshots, draft findings | the builder's own checks pass |
| 6 | Diff check | orchestrator | notes back to the builder | the diff matches the plan |
| 7 | Security review | security reviewer (blind) | severity table | none |
| 8 | Fixes | builder | fix commits, replay tests | every High and Medium fixed, or put to the user; re-review if new security logic |
| 9 | Docs | docs updater | docs true, plan closed, links fixed | link check passes |
| 10 | Checks | orchestrator | exact counts | full suite green, one heavy run at a time |
| 11 | Commit | committer | one commit per plan (or phase) | packed securely |
| 12 | Report | orchestrator | findings complete, `read/` updated | none |
| 13 | Push, PR | committer | branch, PR URL | **only with the user's authorisation** |

This maps the user's own wishlist: a planning agent → a conversation that prompts with questions → confirm before build (or auto) → a new worktree → reviewer, security and docs-updater agents → secure commit packing and push → a PR. The "reviewer" in that list is stage 6, plus any extra blind reviewer you add.

## 1. Intake: triage raw notes

- **NEVER edit the user's notes file.** Triage output goes into plans, findings and `read/`.
- **Split** each paragraph into single asks, then **route each to exactly one place**: a notes batch (small, within the existing design), a new plan, an existing open plan, a future plan, a question, or (rarely) declined with the reason.
- **"Make a plan for X" is not "build X".** Stop at the Decisions.
- **A note that conflicts with a written rule is adapted to the rule, and you say so.** For example, "tell them their email might be blocked" becomes the same text for everyone, because revealing a block enables enumeration.
- **An ambiguous note gets the reading most consistent with the docs,** plus "tell me if you only wanted X".
- **Names in notes are mapped to real names.** Say so in the plan.
- A notes batch still gets the full build loop, with **one commit per note**.
- Details and the triage table: [references/intake-and-triage.md](references/intake-and-triage.md).

## 2. Plans: template and lifecycle

- **Lifecycle:** draft → questions asked → decisions confirmed (dated) → building (phase n of m) → implemented (dated) with **As built** → `git mv` to `implemented/`. Each state is written in the plan's Status line and its index row.
- **MUST** use the full template: [references/plan-template.md](references/plan-template.md). It keeps the user's own scope sections (**Overview** in their words, **Features**, **MVP**, **Optional**, **Future features**, **Security** with their notes quoted) alongside the engineering sections (**Why**, **Design**, **API and data changes**, **Tests**, **Docs**, **Decisions**, **Not included**).
- **Every section is present.** An empty one says `None.` and gives the reason, so "considered" can be told apart from "forgotten".
- **MVP is the cut line.** The builder builds the MVP plus any Optional items a Decision approved, and nothing else. Future features name where they'll go. Not included says what won't be built, and why.
- **Every Decision has a recommendation, its reason, and what the user would notice.** The user's own asks are marked "Decided (your note)", not asked again.
- **NEVER write code before the decisions are confirmed**, except in auto mode (§5).
- **Re-read the plan, and recheck the tree, before building.** A plan describes the tree on the day it was written.
- **Large plans are built in phases**, each reviewed and committed on its own. The plan moves to `implemented/` only after the last phase.
- **Implemented plans, findings and `read/` notes are history.** Never rewrite them for later renames.

## 3. The plans index is the build queue

- `.agents/plans/README.md` holds one table: **`#` · Plan (link + a one-line summary naming the new tables, env vars, events and components) · Needs (plan numbers) · Status**.
- **The next plan to build** is the first row whose Needs have all shipped and whose decisions are confirmed.
- **Later plans are written with the names earlier plans introduce.** "How the open plans fit together" names the shared locks, components, payload owners and naming sources.
- Numbers never change, and shipped rows stay, with their date and any deviation ("implemented 2026-10-31, before plan 2").
- `.agents/plans/future/` holds bigger directions, with a **Starts after** column. A future plan moves into `features/` (and gets a number) once its decisions are confirmed and it's next.
- Large features get a folder with their own table (`A1`–`A14`) and "Rules every plan in this folder keeps".
- Details: [references/plans-index.md](references/plans-index.md).

## 4. The build loop

The orchestrator is the main session. It owns the user conversation, the diff check, decisions taken without the user, edits to the agent guide, merges and the `read/` folder. It delegates building, reviewing, docs and commits to roles, and it never delegates judging whether the result is right.

1. **The builder implements** one plan (or phase, or notes batch) in its line's worktree. It reads the agent guide, the plan and every doc it touches, in full, and delivers code, tests, docs, screenshots and a draft findings file. Brief: [roles/builder.md](references/roles/builder.md).
2. **The orchestrator reads the diff, not the summary.** It checks the diff against the plan's MVP and Decisions, the naming doc and the docs, and runs focused checks. A builder summary that says "all tests pass" is a claim. The diff and a test run are evidence. Mismatches go back to the builder.
3. **A security reviewer sees only the diff and the plan.** **NEVER** give it the builder's summary, the builder's account in the findings file, or your own opinion of the code. A summary frames the review: the reviewer checks the claims it lists and misses what it left out. The reviewer probes with throwaway out-of-tree tests and reports "confirmed sound", plus a severity table. Brief: [roles/security-reviewer.md](references/roles/security-reviewer.md).
4. **Fixes.** The reviewer's table goes **verbatim** to the same builder (continued, so it keeps its context), with your call on each row: fix, accept with a reason, or ask the user. Every High and Medium is fixed before the commit, unless the user accepts it. Each fix gets a test that replays the attack.
   - **A fix that adds new security logic gets a second blind review** of the fix alone. Fixes are written fast. In one real run, a second review found a High-severity bypass that the fix had introduced.
5. **The docs updater** makes every affected doc true and closes the plan. Brief: [roles/docs-updater.md](references/roles/docs-updater.md).
6. **Full checks, one heavy run at a time:** check, test, build and e2e. Never two heavy runs on one line. Across parallel lines, stay within the machine's budget. Record exact counts, and name flakes ("341, after one flaky run of an untouched test that passed alone 3 of 3 times"). Etiquette for heavy runs: [agent-etiquette.md](../conventions/references/agent-etiquette.md).
7. **One commit per plan** (per phase for phased plans, per note for a notes batch), packed securely. Brief: [roles/committer.md](references/roles/committer.md).
8. **Report.** Complete the findings file, then update `read/progress.md` and `read/questions.md`.

- **MUST** brief every sub-agent with the files to read, the line's facts (worktree, database, migration range, ports) and what to return. A sub-agent knows only its brief.
- **NEVER** let one agent both build and security-review the same change.
- **Prefer** continuing the same builder for fixes, and a fresh reviewer for each review.

## 5. Confirm before build, or auto mode ("build everything overnight")

- **Confirm before build is the default.** Switch to auto mode only when the user explicitly says so ("build every open plan while I sleep, recommendations accepted"). **Record their words, with the date,** at the top of `read/questions.md`.
- **MUST, before any building in auto mode:**
  - every open question in every plan in scope has a **recorded default**, which is the plan's recommendation;
  - each Decisions heading reads `(confirmed <date>, as recommended)`;
  - each plan's Status and index row say so.
  
  A question with no default would block the run, or get answered by whichever agent hits it first.
- **A question that comes up during the build** gets the safest reversible default. It is built that way, and recorded at once in the findings ("For you to decide") and in `questions.md` (Question · Default now · Details). **Nothing blocks.**
- **Auto mode does not authorise:** pushing, PRs, merging into the default branch, deploying, sending anything outward, deleting branches or data, loosening a check, or adding an exception. Each needs its own explicit instruction (`agent-etiquette.md`). A new dependency is allowed only if the plan's confirmed recommendation includes it, and it is flagged in `questions.md`.
- **Pauses** (usage limits, crashes) are recorded in `progress.md` with times. Resumed agents continue from `read/` and the findings, so keep those current, not written at the end.
- **When the user returns,** point them to `read/questions.md` ("Start here").

## 6. The findings file per plan

- Every plan, phase or notes batch built by agents gets `findings/<plan>.md`: [references/build-report.md](references/build-report.md).
- **Fixed sections, in order** (empty ones left out, apart from Checks and Security review):
  - What shipped (with a commit table);
  - Where the build differs from the plan (recorded in the plan too);
  - Decisions made while building;
  - Stronger than the plan asked;
  - For you to decide;
  - Fixed without asking;
  - What you must configure;
  - Risks that remain;
  - For later plans;
  - Environment notes;
  - Checks (exact counts);
  - Security review.
- **The Security review is the reviewer's output:**
  - "confirmed sound" (what was probed, and how);
  - the table **`#` · Severity (High / Medium / Low / Nit, with qualifiers like "(likely)" or "(older)") · Finding (attack plus evidence) · Resolution**;
  - Fix results, with commits and replayed attacks.
- The builder's own security claims stay in a section labelled as the builder's account. They are never mixed with the review.

## 7. The `read/` folder: the hand-off to the user

Three files, kept current **during** the run: [references/read-folder.md](references/read-folder.md).
- **`questions.md`, "Start here":** a numbered table (Question · Default now · Details link), "recommendations taken as accepted" with the user's instruction quoted, and "Fixed or decided without you (tell me if you disagree)". Things the user must configure go first.
- **`progress.md`:**
  - how each piece was built;
  - shipped plans (commit · checks · findings);
  - in progress, by phase;
  - pauses;
  - the parallel lines;
  - what's next, and what's not started.
  
  Also what's pushed (normally "nothing").
- **`discoveries.md`:** findings that belong to no single plan, split into "In the code" and "On this machine".

## 8. Parallel worktrees

Details: [references/parallel-worktrees.md](references/parallel-worktrees.md).
- **Parallelise by what plans touch, not by their size.** Plans that share files, tables or shared components go on one line, in order. A plan that would collide is deferred or cancelled explicitly, and the reason is recorded.
- **Each line gets its own:**
  - a sibling worktree and branch (`../app-ui`, `run/ui`);
  - **its own dev database** (`app_ui`);
  - **its own reserved migration-number range** (main 020–049, ui 050–059, events 060–069);
  - its own ports, dependencies and build output.
- **A shared heavy-run budget across all lines.** One orchestrator owns every line and the single `read/` folder.
- **Merge the integration branch into the line, run the full checks there, then merge back.** Re-run the link and convention checks after every merge. Leave worktrees and branches until the user says to remove them.

## 9. Screenshot evidence

- **Every visible change gets screenshots,** saved to `.agents/plans/features/screenshots/<plan>/` and linked from the findings ("What shipped", or the per-note table) and from `progress.md`.
- **Descriptive file names** that say the screen, state, theme and device: `suspend-dialog.png`, `events-charts-badges-dark.png`, `ips-phone.png`. Prefix them with the note's number in a notes batch: `3-didnt-receive-dialog-phone.png`.
- **Capture light and dark, desktop and phone** for new screens and dialogs, plus the important states (empty, error, cancelled, the "before" when the note was about a defect).
- Let the e2e specs copy their review screenshots to a folder given by an env var (for example, `E2E_SCREENSHOTS`). How to capture them is in `testing`.
- **NEVER capture a real secret or personal data.** Use seeded data, and keep one-time values masked (`frontend`). The committer checks screenshots before staging them.

## 10. Agent roles

| Role | Gets | Produces | Never |
| --- | --- | --- | --- |
| [Planner](references/roles/planner.md) | the user's words, the index, the docs | plan, index row, questions | writes production code; marks decisions confirmed; edits the notes |
| [Builder](references/roles/builder.md) | the confirmed plan, the docs, the line's facts | code, tests, docs, screenshots, draft findings | changes a decision silently; builds unapproved scope; loosens checks; pushes |
| Orchestrator (main session) | everything | diff check, decisions without the user, merges, `read/` | delegates judging the result; lets a builder review itself |
| [Security reviewer](references/roles/security-reviewer.md) | **the diff and the plan only** | confirmed-sound list, severity table | sees the builder's summary; edits code; leaves probes; probes production |
| [Docs updater](references/roles/docs-updater.md) | the final diff, plan, findings | true docs, closed plan, fixed links | changes code; documents unbuilt behaviour; rewrites history |
| [Committer](references/roles/committer.md) | the plan's files, check results, push authorisation | one commit per plan; PR when authorised | `git add -A`; commits secrets or others' files; pushes without authorisation; `--no-verify` |

The role files can also be saved as sub-agent definitions (for example `.claude/agents/<role>.md`, or your tool's equivalent), with the "Must never" list as the agent's standing instructions.

## 11. Commit, push and PR

- **One commit per plan** (or phase, or note). It never mixes two plans. Review fixes may be follow-up commits, listed in Fix results.
- **Secure packing:** stage explicit paths only, never `git add -A`, `git add .` or `git commit -a`. Check every staged file belongs to the plan. Scan the staged diff for secrets. Never stage env files, probes, build output or other sessions' untracked files. Never use `--no-verify`.
- **Push and PR only with the user's explicit authorisation for this run.** The PR body links the plan, the findings and the screenshots, and lists the checks, the review summary, "What you must configure" and the open questions.

## Pre-merge checklist

- [ ] Every note from the user is routed (triage table), and their notes file is untouched.
- [ ] The plan has every template section (`None.` with a reason where empty), and its index row has Needs and Status.
- [ ] Decisions were confirmed by the user and dated, or, in auto mode, each has a recorded default and the user's instruction is quoted in `questions.md`.
- [ ] The builder built the MVP and the approved Optional items only. Every deviation is in As built and the findings.
- [ ] The orchestrator read the diff and ran the checks itself.
- [ ] A security reviewer that never saw the builder's summary reviewed the diff and the plan. Every High and Medium is fixed or accepted by the user, and new security logic in a fix was reviewed again.
- [ ] Full checks passed with exact counts, one heavy run at a time.
- [ ] Docs are true, and the link check passes. The plan is closed (Status, As built, index row, `git mv`, links).
- [ ] The findings file is complete: Checks, the Security review table, What you must configure, For later plans.
- [ ] Screenshots are saved and linked, with no secrets in them.
- [ ] One commit per plan, explicit paths only, no secrets, no other session's files, hooks run.
- [ ] `read/questions.md` and `read/progress.md` are current, and progress says what's pushed.
- [ ] Parallel lines: own database, own migration range, the integration branch merged in and checked before merging back.
- [ ] Nothing pushed, merged into the default branch or deleted without the user's explicit authorisation.
