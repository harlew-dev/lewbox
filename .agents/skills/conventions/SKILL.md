---
name: conventions
description: How the codebase stays consistent over time — convention checks written as tests, the docs folder and keeping it true, plans before architectural changes, recorded decisions, rename discipline, grouped declaration lists, comment style, the fixed vocabulary, agent etiquette on shared machines, and how to report completion. Use before starting any change (to find and read the docs), when a convention check fails, when adding a check, a doc or a plan, when renaming, when docs and code disagree, before running heavy or destructive commands, and before saying "done".
---

# Conventions and docs

A codebase stays consistent only if its conventions are **written down** (docs), **enforced** (checks) and **changed deliberately** (plans and recorded decisions). Every rule here exists because the alternative — conventions held in someone's head — decays with every change, and a wrong name or stale doc gets copied by the next person (or agent) who reads it.

What the names *are* is owned by `naming`; where code *lives* by `structure`; this skill owns the machinery that keeps both true.

## When to use

- Starting any change: find which docs and plans cover the areas you'll touch.
- A convention check fails, or a name isn't settled by the docs.
- Adding a convention check, a doc, or a plan.
- A doc and the code disagree.
- Renaming anything.
- Writing a list of declarations (limits, constants, routes, permissions) or comments.
- About to run tests/builds/servers on a machine others may use, or anything destructive or outward-facing.
- About to report a task as complete.

Deep detail:
- [references/writing-a-check.md](references/writing-a-check.md) — read when adding or changing a convention check; full shape, two-stack example, failure-message format, exception lists.
- Plan template and lifecycle: moved to `delivery` ([references/plan-template.md](../delivery/references/plan-template.md)).
- [references/docs-folder.md](references/docs-folder.md) — read when setting up `docs/`, adding a doc, or unsure what a doc should contain.
- [references/agent-etiquette.md](references/agent-etiquette.md) — read before heavy runs, starting servers, or anything destructive; and before the completion report.

## 1. Read the docs before changing an area

- **ALWAYS**, before the first edit, list every area the change touches and read each area's doc **in full**. Most changes touch several: a new endpoint touches endpoints, auth, rate limits, database and API testing docs, plus the frontend API-client doc if the UI calls it. A new component touches design and UI-kit docs. This applies to subagents too — brief them to read the docs.
- If the work has a plan (open or implemented), read it. **Its confirmed decisions override your defaults.**
- **Follow the docs over your own habits.** The docs hold what the code alone doesn't show: naming, patterns, security rules, known pitfalls.
- **When a doc and the code disagree, or a doc's rule doesn't fit the task: STOP and tell the user.** Say which doc, which code, and what you'd propose. **NEVER** silently pick one side.
  - Why: either the doc is stale (and the next reader will be misled) or the code is wrong (and your change would copy the bug). Only the user knows which.

## 2. Keep the docs true, in the same change

- **MUST** update every doc whose summary your change makes untrue, **in the same change** (same commit/PR). Not "later", not a follow-up ticket.
- A new doc gets a row in the docs index (`docs/README.md`) and in the agent guide's doc table (`AGENTS.md` or equivalent).
- Docs are **short per-area summaries** (how it works, the rules, the pitfalls), not tutorials or code dumps. See [references/docs-folder.md](references/docs-folder.md).
- A link checker runs in `check`: every relative link and `#anchor` in tracked markdown must resolve. **Fix a dead link; never ignore it.**
- When you rename something, `grep` the old name across code, docs and **open** plans and update every mention. Implemented plans are historical records and keep the old names.

## 3. Convention checks are tests

Every naming and structure rule is enforced by an ordinary test (or lint rule/script) that fails `test` or `check` the moment a name is wrong. Each check has **five parts**:

1. **A pure function** taking names (or file facts) and returning violations, each message naming the rule, the doc section it comes from, and how to fix it.
2. **A table test** feeding it good names and names it must refuse — so a check that can never fail is caught.
3. **A run on the real tree** (reflection, source parsing, or importing the modules), which also fails if it finds **suspiciously few** names, so a moved folder can't make it pass by finding nothing.
4. **An exception list in the test**, each entry with its reason; where entries name real code, a failure for any entry nothing uses any more.
5. **A row in `docs/checks.md`** (check, rule, where it lives, its exception list) and a link from the rule's section in the naming doc.

- **A new check MUST be seen failing first.** Before fixing the tree, run it against the current code (or a deliberately bad case) and watch it fail with a useful message. A check you never saw fail proves nothing.
- Full shape and examples: [references/writing-a-check.md](references/writing-a-check.md).

## 4. Fix the name, not the check

When a check fails:

- **ALWAYS** rename your code to follow the naming doc. The failure message names the rule and section.
- **NEVER** loosen a regex, add an exception, edit a word list, skip (`t.Skip`, `it.skip`, `@skip`) or delete a test, or rename something to slip past the check while breaking the rule's intent — **unless the user has agreed**.
- If the naming doc doesn't settle the name (a new resource, a new domain verb, an action that isn't one of the five), **stop and ask**, giving your proposed name and why. Don't invent a convention or copy a nearby file that may itself be wrong.
- If a check seems wrong for the task, **say so** and explain why; don't silently work around it.
- With the user's agreement: add the exception to the check's list **with its reason**, in the same change as the code that needs it; if the rule itself changed, update the naming doc too; record the approval in `docs/checks.md` (which plan or decision approved it, and that no rule was otherwise loosened).
- Exceptions marked "existing; to fix" are pre-check violations; fix them when you next touch that code.

## 5. Plans before architectural changes

- **MUST** write a plan in `.agents/plans/features/<name>.md`, and have the user confirm its decisions, **before writing code**, for: a new feature; a new table, package, event, listener or endpoint family; a new dependency; any change to auth, security, rate limits, or how layers fit together. Small fixes and changes within an existing design don't need a plan.
- The template, the lifecycle (draft → confirmed → built → As built → `implemented/`), the plans index and the build loop are owned by `delivery`: see [its plan template](../delivery/references/plan-template.md).

## 6. Record decisions with dates and overrides

- Every decision is recorded where it applies (the plan's Decisions table), with the **date confirmed**: `## Decisions (confirmed 2026-11-01)`.
- When the user overrides your recommendation, record **both**: the decision, and `(Original recommendation — <what you proposed> — replaced.)` Mark it in the Status line too: `implemented 2026-11-02 (decisions 1 and 3 changed by the user)`.
  - Why: a future reader (or you, next month) needs to know a rule was chosen deliberately against the obvious alternative, so they don't "fix" it back.
- A plan written against a snapshot says so: "As of 2026-11-01 … recheck the tree for anything added since before starting."

## 7. Renames

- **A pure rename is its own commit**: no behaviour, response, SQL or URL changes mixed in. State "rename only" in the message. Reviewers can then skim it, and `git blame`/bisect stay useful.
- **Rename with tooling**, not find-and-replace: the language server's rename (`gopls rename`, the IDE's Rename Symbol, `ts-morph`, Rector, `rope`) so tests and doc comments follow; `git mv` for files so history follows.
- Then `grep -rn` every old name across code, docs and open plans; let the type checker find the rest.
- Do renames **before** other feature work on the same code, so feature diffs aren't drowned in renames.
- Before starting a planned rename, recheck the tree for code added since the plan was written and rename it the same way.
- When a rename hides a property the old name stated, keep it in the doc comment: `FindWorkspace` must still say "scoped by the user's membership; missing and non-member both return not-found".
- Renaming the **product** (brand, module, cookies, databases): read [references/renaming-the-product.md](references/renaming-the-product.md) first. Key-derivation labels and lock names must keep the old name.

## 8. Grouped declaration lists

A run of declarations of the same kind (rate limits, a constant block, an enum, permissions, a route table, event names):

- **Group by purpose**, one blank line between groups, none inside a group.
- **Each declaration's comment starts with its name and what it is**, then any context on the following lines.
- A new declaration **joins its group**; a new kind of thing starts a new group after a blank line.
- A list that repeats the declarations elsewhere (a registry slice/array) follows the **same groups and order**, one line per group.

```
// AuthPerIP: everything that signs in or checks a password, per client IP.
AuthPerIP = Limit{…}
// AuthPerAccount: password guesses against one account, from any IP.
AuthPerAccount = Limit{…}

// RegisterPerEmail: sign-ups for one email, from any IP.
// Each one emails the address, so this also caps mail to a victim's inbox.
RegisterPerEmail = Limit{…}
// RegisterGlobal: accounts created by everyone.
// Caps sign-ups during a distributed flood.
RegisterGlobal = Limit{…}
```

## 9. Comment style

- **Line comments only** (`//`, `#`, `--`), never block comments (`/* */`) for prose. Doc-comment syntax required by a tool (JSDoc `/** */`, PHPDoc, docstrings) is the exception where the tool needs it.
- **Name first:** a declaration's comment starts with the name (`// AuthPerIP: everything that signs in or checks a password, per client IP.`).
- Comments say **what it is, why, and any non-obvious contract** (scoping, ordering, what errors mean, security reasons). **NEVER** restate the code (`// increment i`).
- Keep comments true: a change that makes a comment false updates it, like a doc.
- Mark deliberate oddities with the reason and a link (`// PATCH, not PUT: update is always PATCH — docs/naming.md#controllers`).

## 10. A fixed vocabulary

Keep a **Say / Not** table in the naming doc and use only the left column, in code, docs, UI copy where relevant, commit messages and conversation. The canonical list is owned by `naming`; the common ones:

| Say | Not | Where |
| --- | --- | --- |
| `index` | `list`, `all` | API, client and page action |
| `store` | `create`, `add` | API and client create action (the data layer's write is `Create…`) |
| `destroy` | `delete`, `remove` | API and client delete action (data layer: `Delete…` or a domain verb) |
| `update` (PATCH) | `edit`, `save`, PUT | API change action; `edit` is only the page action |
| `<resource>.<fact>` | `<resource>.<command>` | events are past-tense facts (`project.created`) |
| member | user | someone in a tenant is a member; the account is a user |

- When a new term enters the domain, add it to the table (with what it's not) **before** it spreads.
- Why: two words for one thing means two code paths, two searches, and a reader wondering whether they differ.

## 11. Agent etiquette on shared machines

Other people and agents may be using the same machine, checkout or worktrees. Summary (full list: [references/agent-etiquette.md](references/agent-etiquette.md)):

- **NEVER** stop, restart, reuse or kill a process, server, container or test run you didn't start.
- **Reuse** the existing database/service containers; never install a second database server. Restart your own process after config changes rather than starting a duplicate.
- Before a heavy run (full test suite, end-to-end, build): check what's running and which ports are taken, consider CPU/memory, wait if the machine is busy, run long jobs in the background, never start a second copy of your own run. A single focused test is fine any time.
- Use isolated ports and databases for end-to-end runs (auto-picked, reported back).
- Untracked files may be another session's work in progress: don't delete or "clean up" files you didn't create.
- **Keep scope to the request.** No unrequested refactors, dependency bumps or drive-by fixes; mention them instead.
- **NEVER** push, merge, force-push, delete branches, drop/reset databases, run down migrations, send emails/messages/webhooks, or make any other destructive or outward action **without explicit authorisation**.
- **Destructive scripts preview by default** and act only with an explicit confirmation (`CONFIRM=1`), refuse in production where inappropriate, and say what they would do.

## 12. Completion reporting

- **Before claiming done, run all relevant checks**: the full test suite (including integration tests against the real database), lint/type/format/convention/doc-link checks, end-to-end for user flows, and the production build when build or composition changed.
- **Report failures** with what failed and why; **NEVER** skip, silence or leave a warning for the user to find.
- **NEVER claim a capability that isn't implemented and tested** ("supports passkeys", "works offline", "handles 10k users"). If you didn't run it, say "not verified".
- Finish with a concise summary: what changed, which checks ran (and their results), what's left or limited, and which docs/plans you updated.

## Pre-merge checklist

- [ ] Read every doc (and plan) for each area touched before the first edit.
- [ ] No unresolved doc/code disagreement — or the user was told and decided.
- [ ] Every doc the change made untrue updated in this change; new docs indexed; doc links pass.
- [ ] Architectural change? A plan exists with confirmed, dated decisions; on completion: Status, As built, moved to `implemented/`.
- [ ] Convention checks pass **by renaming code**; any new exception was agreed by the user, has a reason, and is recorded.
- [ ] New check: five parts present, and it was seen failing first.
- [ ] Renames: own commit, done with tooling/`git mv`, old names grepped out of code, docs and open plans.
- [ ] Declaration lists grouped, name-first comments, line comments only.
- [ ] Only vocabulary from the Say column.
- [ ] Didn't touch processes, files or services you didn't start/create; nothing destructive or outward without authorisation.
- [ ] All checks run; failures reported; no untested claims; summary written.
