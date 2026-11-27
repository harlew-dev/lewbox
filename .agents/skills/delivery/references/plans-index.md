# The plans index: the build queue

Read when adding a plan to the queue, picking the next plan to build, writing a feature folder, or closing a plan.

## Layout

```
.agents/plans/
  README.md                  the index: open plans in build order, future plans, how they fit together
  features/
    <plan>.md                open plans (draft or confirmed, not yet shipped)
    <feature>/README.md      a large feature: its own ordered table and shared rules
    <feature>/<part>.md
    <feature>/findings/<plan>.md   one per plan built in this feature (build-report.md)
    <feature>/read/          the hand-off to the user (read-folder.md)
    findings/<plan>.md       findings for standalone plans
    read/                    the hand-off for a run across standalone plans
    screenshots/<plan>/      evidence (SKILL.md §9)
    implemented/             shipped plans, including implemented/<feature>/<part>.md
  future/<plan>.md           bigger directions, not in the build order yet
```

**`features/` holds only open plans.** A plan that has shipped is `git mv`-ed into `implemented/`, so a directory listing is the to-do list.

## The index table

`.agents/plans/README.md` begins with one sentence on the rule (every architectural change gets a plan before code) and then the table:

```markdown
## Open plans, in build order

Build them in this order. Each one assumes the ones above it are done, and later plans are
written with the names and tables earlier ones introduce.

| # | Plan | Needs | Status |
| --- | --- | --- | --- |
| 1 | [event-names](features/implemented/event-names.md): `<resource>.<fact>` names; the `events.name` CHECK | none | implemented 2026-10-31 |
| 4 | [rate-limits](features/implemented/rate-limits.md): budgets on every signed-in request, quotas; `auth_attempts` → `rate_limits` | 1, 2 | implemented 2026-10-31, before plan 2 (its name test is a test of its own) |
| 5 | [client-ip](features/client-ip.md): real client IPs behind a proxy (`CLIENT_IP_HEADER`, `TRUSTED_PROXIES`) | 4 (limit keys) | decisions confirmed 2026-11-01 |
| 6 | [captcha](features/captcha.md): challenge levels, `ChallengeDialog` | 4, and 5 before any proxied deploy | draft, decisions to confirm |
```

Column rules:

| Column | Holds | Why |
| --- | --- | --- |
| `#` | A number that never changes once given. Shipped rows stay in the table | Plans, findings and commits refer to "plan 4". Renumbering breaks every reference |
| Plan | A link, then a **one-line summary naming what it introduces**: new tables, env vars, events, components, limit names | The next planner writes against these names without opening every plan |
| Needs | Plan numbers that must ship first, with the reason in brackets when it's partial ("4 (limit keys)"; "6 optional") | This is the dependency graph. The next buildable plan is the first row whose Needs have all shipped |
| Status | `draft`, `decisions to confirm`, `decisions confirmed <date>`, `building (phase n of m)`, or `implemented <date>`, plus any deviation | The queue's state at a glance. A deviation like "before plan 2" or "the admin page moved to plan 21" explains history without opening the plan |

Rules:
- **Add the row when the plan is drafted,** not when it's built.
- **Later plans use the names earlier plans introduce.** If plan 6 depends on plan 4's limit struct, plan 6 says `threshold` levels on plan 4's `Limit`. It never invents a parallel name.
- **When a plan's scope moves** (a page moves to another plan), both rows say so.
- **Never delete a shipped row.** The table is the project's delivery history.
- **Two plans with the same number** is a merge accident. Fix it when you see it: give the later one the next free number.

## How the open plans fit together

A section after the table. It names whatever is shared across plans, so two builders don't each invent their own:
- **Shared names:** where they come from (`naming`, the naming doc), and which plan introduces which.
- **Shared locks:** "Deletion and invitations take the same tenant row lock, always in `id` order."
- **Shared components:** "Plan 3 adds `ConfirmationDialog`; every later confirmation uses it."
- **Ownership of shared payloads:** "Plan 6 owns the `rate_limit.reached` payload."
- **Pointers to feature folders** and their shared rules.

Update this section when a plan is drafted that touches something shared.

## Implementing a plan (the short version in the index)

The index carries this checklist, so a builder who only opens the index still follows it:
1. Confirm its open decisions with the user first.
2. Read the docs for every area it touches, plus the naming doc and the checks doc. Don't work around a failing check (`conventions`).
3. When it ships, close it ([plan-template.md](plan-template.md#closing-a-plan)).

## Future plans

`.agents/plans/future/` holds bigger directions that aren't in the build order: a framework extraction, a rewrite, a new product area. They have their own table:

```markdown
## Future plans

| Plan | Starts after | Status |
| --- | --- | --- |
| [framework](future/framework.md): extract the core into a reusable framework and starter | the open admin plans (A8, A2, A9, A3), since they change many of the files it moves | decisions to confirm |
```

- **"Starts after"** replaces Needs. A future plan waits on a milestone, not on a single plan.
- A future plan **moves into `features/` and gets a numbered row** only once its decisions are confirmed and it's next.
- A plan's "Future features" section can point at a `future/` plan.

## Feature folders

A feature too big for one plan (an admin dashboard: 14 parts) gets `.agents/plans/features/<feature>/README.md`:

1. **Status line and what it builds on**, linking the plan it follows.
2. **Its own ordered table**, with a prefix (`A1`–`A14`). The columns are `#`, Plan, **Adds** (a richer one-line summary), Needs and Status. A **Next:** line follows the table.
3. **"Rules every plan in this folder keeps"**: numbered rules that would otherwise be repeated in every part. For example: every read is recorded; one endpoint per page; the least data that's needed; bounded queries; actions need a role, a fresh code and a reason; no third parties by default; feature code stays in its own build. A part that needs an exception says so in its Decisions.
4. **What it answers and what it can't**: the limits of the feature, so nobody assumes it covers more. For example, "the dashboard can't tell you the app is down; an external uptime check does".
5. **The end state**, such as the sidebar once every part ships.
6. **"Later, not planned yet"**: ideas with the reason they wait.
7. **"When these ship"**: where they move (`implemented/<feature>/`) and which docs grow.

The feature's parts also get rows in the top-level index (one row per part, or one row that links to the folder), so the global queue stays complete.
