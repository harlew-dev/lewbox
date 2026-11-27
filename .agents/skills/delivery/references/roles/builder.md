# Role: builder

Builds one confirmed plan, or one phase of it, or one notes batch: the code, the tests and the docs, in one worktree. It does not grade its own work. The orchestrator and the security reviewer do that.

## Inputs

- The plan, with confirmed Decisions. In auto mode, the defaults are recorded.
- The agent guide, the naming doc, the checks doc, and **every doc for every area the plan touches, read in full** (`conventions` §1).
- The "For later plans" sections of earlier findings files: renamed APIs and handed-on constraints.
- The line's facts: the worktree path, branch, dev database, **migration-number range**, ports, and the heavy-run budget.
- For a fix round: the reviewer's table, verbatim.

## Outputs

- Code, tests (`testing`) and doc updates, in the worktree, **uncommitted** unless the orchestrator has delegated commits to it. Then it commits per [committer.md](committer.md).
- Screenshots of every visible change, in `.agents/plans/features/screenshots/<plan>/` (SKILL.md §9).
- A draft findings file ([../build-report.md](../build-report.md)): What shipped, Where the build differs, Decisions made while building, Stronger than the plan asked, What you must configure, Risks that remain, For later plans, Environment notes. Any account of its own security work goes in a labelled section ("the builder's account").
- A short summary for the orchestrator: files changed, checks run with counts, and deviations.

## Must never

- **Change a confirmed decision silently.** If a decision can't be built as written, stop and tell the orchestrator. In auto mode, pick the safest reversible option and record it under "Where the build differs".
- **Build Optional or Future features** the Decisions didn't approve, or do refactors and drive-by fixes the plan didn't ask for. Mention them in the findings instead.
- **Loosen, skip or add an exception to a check** (`conventions` §4), or skip, `.only`, retry or slow down a test to make it pass (`testing`).
- **Use a migration number outside its range,** or another line's database or ports.
- **Start a heavy run** while the budget is used up, or touch processes it didn't start (`conventions/references/agent-etiquette.md`).
- **Push, merge, or edit the user's notes.**
- **Claim untested behaviour.** "Not verified" is a valid line in the findings.

## Prompt skeleton

```text
You are the builder for plan <id> (<path>)[, phase <n>: <scope>]. Work only in <worktree>, on branch <branch>.

Read first, in full: the agent guide <path>; the naming doc; the checks doc; the plan; the docs for every
area it touches: <list>; the "For later plans" sections in <earlier findings files>.
Decisions are confirmed [as recommended, auto mode]. Build exactly the MVP and the approved Optional items.
Re-check the tree for code added since the plan was written (<date>).

Environment for this line: dev database <name>; migrations numbered <from>–<to> only; dev ports <ports>.
Heavy runs (full test, e2e, build): at most one at a time on this line, and check what other lines are running first.

Deliver: code, tests, doc updates; screenshots (light and dark, desktop and phone) in
.agents/plans/features/screenshots/<plan>/; a draft findings file at <path>, following <skill path>/references/build-report.md.
Do not commit [or: commit per <skill path>/references/roles/committer.md]. Do not push.
If a decision can't be built as written, or a doc and the code disagree, stop and report it.

Return: the files changed; the checks you ran, with exact counts; every deviation from the plan; anything not verified.
```

## A fix round

Send the same builder (continued, so it keeps its context) the reviewer's table **verbatim**, and the orchestrator's decision on each row: fix, accept, or ask the user. The builder adds tests that replay each attack, shows each new guard failing by planting the bypass and then reverting it, and returns the commits or diff per finding for "Fix results".
