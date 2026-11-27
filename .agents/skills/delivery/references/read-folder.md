# The `read/` folder: the hand-off to the user

Read when starting a run the user won't watch (overnight, auto mode, parallel lines), and whenever its state changes during the run.

The user comes back to dozens of commits, several branches and decisions made without them. **The `read/` folder is the only thing they should need to open.** It has three files, and nothing else goes in it:

| File | Answers | Updated |
| --- | --- | --- |
| `questions.md` | "What do you need from me?" It is the entry point, headed "Start here" | Whenever a question arises or a default is taken |
| `progress.md` | "What shipped, what's in progress, what's next?" | After every commit, merge, pause or change of plan |
| `discoveries.md` | "What did you find that isn't part of any one plan?" | When something is found |

Rules:
- **Keep them current during the run, not at the end.** If the run dies (a usage limit, a crash), the folder must still be true. It is also what a resumed agent reads to pick up the work.
- **Write for someone who hasn't seen the run.** Name the plan, link the details, give the default. Never write "as discussed" or "the issue above".
- **Link, don't repeat.** Each row links to the findings section or plan Decision that explains it.
- **The user's own notes files are never edited** (their `notes.md`, their root `discoveries.md`). Say so at the top of `discoveries.md`, so they know their file is untouched.
- **Mark "Last updated <date>, <during|after> the <run name>"** at the top of `questions.md`.

## `questions.md`

```markdown
# Questions for you

Start here. Each question links to the file that explains it. Also in this folder:
- [progress.md](progress.md): what shipped, its commits and check results;
- [discoveries.md](discoveries.md): things found along the way.

Last updated 2026-11-01, during the overnight run.

## Waiting on you (nothing is blocked; each has a default in place)

| # | Question | Default now | Details |
| --- | --- | --- | --- |
| 1 | **Should a correct step-up code reset the wrong-code count?** Today a session gets 5 wrong codes in its whole life, then it ends. Safer against a stolen cookie, but an admin who mistypes 5 times in one session must sign in again | No reset | [email-blocks findings](../findings/email-blocks.md#security-review) |
| 2 | **Production now refuses to start without `CAPTCHA_PROVIDER`.** Add it to your deployment before releasing | As planned | [captcha findings](../findings/captcha.md#what-you-must-configure) |

## Every open plan: recommendations taken as accepted

On 2026-11-01 you asked for "<the user's instruction, quoted>". So each plan's Decisions table is being
taken **as recommended**: <list of plans with links>.
Any decision I'd change, or that a builder had to change, is listed in that plan's findings file.

## Fixed or decided without you (tell me if you disagree)

- **<The decision, in bold>** (<plan>). One or two sentences on why. Link.
```

Rules:
- **Every question has a "Default now"**, the thing that's built or in place. "Nothing is blocked" must be true.
- **Bold the question itself**, then give the context: the trade-off in one or two sentences, and what the user would notice.
- **A question born from the user's own note quotes the note** (`.agents/plans/features/notes.md`, "admin logout button not danger variant") and states how it was read.
- **Things the user must do** (configure, deploy, rotate) go at the top of the table, even though they aren't really questions. They're the most expensive to miss.
- **Answered questions** are moved to a "Answered" section with the answer and date, or removed once their decision is recorded in the plan. Never leave an answered question in "Waiting on you".
- **Number rows stably** during a run, so "question 7" means the same thing in chat and in the file.

## `progress.md`

```markdown
# Progress

What has shipped on branch `<integration-branch>`, and what's next. Nothing is pushed. Open questions are in [questions.md](questions.md).

## How each piece was built

1. A builder sub-agent reads the agent guide, the plan and every doc it touches, then builds the plan with tests and docs.
2. The orchestrator checks the diff.
3. A separate security reviewer sees only the diff and the plan, not the builder's summary, and probes it.
4. Its findings go back to the builder.
5. Check, test, build and e2e run, one heavy run at a time.
6. One commit per plan.

## Shipped

| Plan | Commit | Checks | Findings |
| --- | --- | --- | --- |
| A8: the admin pages as their own build | `942a846` | check, test, build; e2e 22/22 | [separate-build](../findings/separate-build.md) |

Security reviews found:
- **A8:** one medium (admin code could slip past both build guards) and 7 lower.
All were fixed before committing, apart from the few listed as accepted in each findings file.

## In progress

- **A3, observability,** in three phases, each reviewed and committed on its own:
  1. Logs, request IDs, panic recovery: **committed `10797fd`**.
  2. Grouped errors: _building_.
  3. Metrics, heartbeats: not started.

> **Pause:** at about 10:00 every agent stopped on the account's usage limit. They were resumed at 12:51,
> each from where it stopped. No work was lost.

## Parallel lines

| Line | Where | Branch | Order |
| --- | --- | --- | --- |
| Main | this checkout | `<integration-branch>` | A3 (**done**) → A4 (**building**) |
| ui | `../app-ui` | `run/ui` | notes UI fixes (**done**, reviewed, fixed) → policies (**building**) |
| Not part of this run | | | page-folders: cancelled at your request; the file is left untouched |

Keeping them apart: <databases, migration ranges, test-run budget, merge rule> (parallel-worktrees.md).

## Next

## Not started (needs your decisions first)
```

Rules:
- **Every shipped row has a commit, the checks with counts, and a findings link.** Add a screenshot link where there is one.
- **Summarise each security review in one line** (counts by severity, and the worst finding in a few words).
- **Record pauses and resumptions** with times and whether work was lost. Otherwise the user sees a gap and assumes something broke.
- **State what's pushed.** "Nothing is pushed" is the normal state of an unattended run.
- **Cancelled and deferred work is listed** with the reason and who decided it, so the user doesn't think it was forgotten.

## `discoveries.md`

```markdown
# Discoveries

Things found while building <the run> that aren't part of any one plan's findings, or that are worth
knowing beyond them. Your own notes file (`discoveries.md` at the repo root) is untouched. Questions are in [questions.md](questions.md).

## In the code

- **Project names accept control characters.** Every other free-text input refuses them; a NUL byte
  probably answers 500. Fixed with the notes work. See [project-input-validation](../findings/project-input-validation.md).
- **The admin modules are listed by hand in two places** (the build guard and the lint rule). A comment in each says to keep them in sync.

## On this machine

- **The browser for e2e isn't found by default.** E2E needs `CHROMIUM_PATH=<path>`.
- **Other sessions are working in this checkout.** `.agents/plans/features/page-folders.md` appeared during the run and is kept out of these commits.
- **The local dev database was rebuilt** for migration edits. Run the seed command after pulling if your data looks gone.
```

Rules:
- **"In the code"** is for pre-existing bugs, surprising library behaviour, duplicated lists that must stay in sync, and risks that cut across plans. A real bug found here gets a small findings file of its own (What · Why it matters · Suggested fix), then a fix or a question.
- **"On this machine"** is for the environment: paths, ports, other sessions' files, data that was reset. These tell the user why their local state may look different.
- **Anything you did to shared state** (reset a dev database, used a container you didn't start) goes here. Prefer not doing it at all (`conventions/references/agent-etiquette.md`).
