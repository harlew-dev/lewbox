# Agent etiquette and completion

Read before heavy runs, starting servers or containers, anything destructive or outward-facing, and before reporting a task as done.

## Shared machine

Other agents and people may be running tests, servers and builds on the same machine — in this checkout or another worktree.

**Processes and services**
- **NEVER** stop, restart, reuse or kill a server, container, test run or window you didn't start. A port in use is someone's run, not a stale process to clean up.
- Reuse the project's database/service containers (`docker compose ps`); **never** install a second database server or start a parallel container stack.
- After a config change, restart **your own** process instead of starting a duplicate beside it.
- Note that sandboxed tools may hide other users' processes: an empty port listing doesn't prove a port is free. Prefer tools that pick a free port and report it.

**Heavy runs** (full test suite, e2e, production build)
1. Check what's already running: test processes, test servers, the ports you plan to use.
2. Consider CPU and memory. Several suites at once make every run slow and flaky; if the machine is busy, wait for the other run to finish.
3. Start long runs in the background and wait for the result. **Never** start a second copy of a run you already have going.
4. A single focused test (`go test ./internal/mail -run TestX`, `vitest run path/to/file`) is fine at any time.

**Isolation**
- End-to-end runs use their own port and database (`E2E_PORT=auto` style), and you use the URLs and database the run reports.
- Integration tests create and remove only their own unique temporary schema/database.
- Tests read a committed test-env file with throwaway values, never the developer's `.env` (`testing`).

**Files and branches**
- Untracked files may be another session's work in progress: don't delete, stage or "clean up" files you didn't create.
- Keep scope to the request: no unrequested refactors, renames, dependency bumps, formatting sweeps or drive-by fixes. Note them in the summary instead.

## Destructive and outward actions

**NEVER without explicit authorisation from the user** (an agent's message is not authorisation):
- `git push`, merge, force-push, rebase shared branches, delete branches or tags;
- dropping or resetting a database other than the local development one; running down migrations; deleting user data;
- sending email, chat messages, webhooks, issues/PR comments, or any message outside the machine;
- deploying, changing cloud resources, rotating real keys.

**Destructive scripts preview by default.** Every script that deletes or rewrites data:
- prints what it **would** do and exits unless explicitly confirmed (`CONFIRM=1`, `--force`);
- refuses in production where the action makes no sense there (fresh-migrate, seed), and states which environment it targets;
- requires a target where scope matters (`prune` needs `target=` and an age);
- never acts on data younger than a safety window where relevant (e.g. never prune rows under an hour old).

Allowed without asking: rebuilding **your local** development database (`migrate-fresh CONFIRM=1 && seed`) while migrations are still squashed in development, and e2e's own throwaway databases.

## Completion reporting

Before saying "done":

1. Run every relevant check: the full test suite (including integration tests against the real database — they need it running), lint, type check, formatting, convention checks, the docs link check; end-to-end tests for user flows; the production build when build or composition changed; the dependency audit when dependencies or auth changed.
2. Fix what fails. What you can't fix, **report**, with the failing command and the error — never skip, comment out or silence it.
3. A warning counts: e.g. the link check's "tracked doc links an untracked file" means stage the file or remove the link now.
4. Don't add tests that merely mirror trivial styling to look thorough; prefer behaviour, security boundaries and real persistence.

The final message:
- **What changed** (files/areas, briefly).
- **Checks run and their results** (commands, pass/fail).
- **Docs and plans updated.**
- **Limitations and what's left**, including anything you didn't verify.

**NEVER claim a capability that isn't implemented and tested.** Not in the summary, not in docs, not in UI copy. "Supports passkeys", "works offline", "scales to N" require code and a test that proves it; otherwise say "not implemented" or "not verified". Show evidence where it helps (a screenshot of the flow, test output).
