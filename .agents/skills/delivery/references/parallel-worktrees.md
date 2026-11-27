# Parallel lines in git worktrees

Read before running more than one plan at a time, or when a builder needs its own checkout.

Unrelated plans can be built at the same time, each in its own **line**: a sibling git worktree on its own branch, with its own builder, reviews and commits, merged into the integration branch when it's done. Two agents in one checkout trample each other's files, build output, dev database and ports. A worktree separates the files. The rules below separate everything else.

## Setting up a line

1. **Choose what runs in parallel by what it touches, not by size.** Two plans that edit the same files, the same tables, or shared components (the auth layout, the router, the users table) go on the **same** line, in order. Plans that would collide and can't wait are **deferred or cancelled explicitly**, with the reason in `read/progress.md`. Example: "page-folders (moves every page file) cancelled from this run; it would collide with the new admin pages."
2. **Create the worktree beside the main checkout**, named after the line, on a branch named after the run:
   ```sh
   git worktree add ../app-ui -b run/ui <integration-branch>
   git worktree add ../app-events -b run/events <integration-branch>
   ```
3. **Give each line its own development database**: `app_ui`, `app_events`. Set it in that worktree's local env file (untracked). Never point two lines at one dev database: migrations from one line break the other's running app, and resetting one wipes the other's data. Test and e2e databases are already per run (`testing`). How the env file and dev database are created is `tooling`'s.
4. **Give each line a disjoint migration-number range**, reserved before anyone writes a migration:

   | Line | Migrations |
   | --- | --- |
   | main | 020–049 |
   | ui | 050–059 |
   | events | 060–069 |

   Why: two branches that each add "migration 021" merge cleanly in git and then fail, or silently skip one, at migrate time. Gaps in the numbering are harmless. If the project names migrations by timestamp, numbering can't collide, but the order can still invert across a merge: check that a line's migrations don't depend on another line's.
   Record the ranges in `read/progress.md` and in each builder's brief.
5. **Give each line its own dev-server ports** if anyone will run the app there. E2E runs pick their own ports (`E2E_PORT=auto` style; see `testing`).
6. **Install dependencies and build in each worktree.** Build output and dependency folders are per checkout. Never symlink them between lines.

## Running lines

- **A shared budget for heavy runs across all lines,** set by the machine (for example, about 2 full suites or e2e runs at once). Within a line, one heavy run at a time. Before starting one, check what other lines are running (`conventions/references/agent-etiquette.md`).
- **Each line runs the full build loop** (SKILL.md §4): its own builder, blind security review, fixes, checks, commits on its own branch.
- **Each line's findings and `read/progress.md` rows name the line**: "Built on the ui worktree (`../app-ui`, branch `run/ui`)".
- **One orchestrator owns every line.** It writes `read/` (in the main checkout only, so there's one copy) and decides the merge order.

## Merging a line back

1. Only after its review fixes and full checks pass.
2. **Merge the integration branch into the line first**, then run the full checks on the line. Conflicts and broken assumptions show up there, not on the integration branch. A merge that changes behaviour gets its own entry in the findings ("The merge (`0cf723a`)": what broke, what was fixed).
3. Then merge the line into the integration branch. A fast-forward is best.
4. **Re-run the docs link check and the convention checks after every merge.** Merges break links (a plan moved on one line, linked on another) and duplicate numbers (two plans numbered 26).
5. Note the merge commit in `read/progress.md`.
6. **Leave the worktree and branch in place** until the user says to remove them. Deleting branches is a destructive action (`conventions/references/agent-etiquette.md`). List the lines still open in `progress.md`.

## Other sessions in the same checkout

Other people or agents may be working in the main checkout during your run. Untracked files that appear (a new plan, a draft doc) are theirs:
- don't stage, move or delete them;
- keep them out of your commits (stage explicit paths only; see [roles/committer.md](roles/committer.md));
- note them in `read/discoveries.md` under "On this machine", along with any collision they could cause with your work.
