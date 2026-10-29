# Worktrees

> Status: Draft · Milestone: Alpha (M3) for worktrees made with a conversation · standalone worktrees Beta, aiming for Alpha · Updated: 2026-10-29

Conversations in one sandbox share its files, so two working in one repository edit the same checkout. A **worktree** keeps a conversation's work apart: a [git worktree](https://git-scm.com/docs/git-worktree) of the repository, on a branch of its own, under `/root/workspace/.worktrees/`. Lewbox makes and removes them, because Lewbox starts and ends conversations, so removing a conversation's worktree with it is reliable, and any app running several agents in a sandbox needs the same.

Today a worktree can only be made with a conversation's first prompt. This plan keeps that and adds **standalone worktrees** (`wtr_`), made before any conversation, so an app can prepare one (write `.env`, install dependencies, make a database, run migrations) before the agent starts in it.

## Today

- **Built** 2026-10-24 (`b84bbb0`), `keep_worktree` on delete (`92beca5`), names 2026-10-29 (`5b4392d`), in `internal/agentd/worktree.go` (434 lines) and `worktree_test.go`; the gateway records the worktree on the app as the conversation's `worktree` JSON (`recordWorktree`, `conversations.go:489`).
- **Contract:** [lewbox.com/docs/agents/conversations § Worktrees](https://lewbox.com/docs/agents/conversations#worktrees).
- **Behaviour:** `worktree: true` or `{branch, from, copy, name}`. Made at `/root/workspace/.worktrees/<cnv_id>` (or `<name>`) on a new branch (default `lewbox/<cnv_id>`) from `from` (default what the repository has checked out), with up to 20 untracked paths copied in (`.env`). The agent starts at the same place inside it as `directory` is in the repository. A `directory` outside a repository is `422 not_a_repository`; a worktree git can't make (branch exists, unknown `from`) is `409 worktree_failed` with git's message. Deleting the conversation removes the worktree and keeps the branch; uncommitted changes refuse it (`409 worktree_dirty`) unless `?force=true`; `?keep_worktree=true` leaves it. In a stopped sandbox it's left.
- **Names** (`5b4392d`): `^[A-Za-z0-9._-]{1,100}$`, not starting with `-`, not `.` or `..` (else `invalid_request`); a name another worktree has is `worktree_failed`, unless that worktree is on the branch this request would make, as when a first prompt is sent again.
- **Designed, not built** (2026-10-28, legacy `docs/acp.md` § Worktrees on their own; gateway `35a4f27`): the standalone API below.

**Lewagent's ask** (`lewagent-legacy/docs/todo.md`; `docs/sessions.md` § Working directory; `docs/services.md` § Worktrees): worktrees on their own, `worktree: {id}` on starting a conversation, removal with the last conversation in it, and `worktree.created`/`worktree.removed` webhooks, so a chat's worktree can be set up behind a "Creating environment" screen before it starts. Its Git panel also makes worktrees with `git worktree add` through commands, which Lewbox can't list, check or remove. "Archive" a chat (remove its worktree, keep the conversation) needs Lewbox to remove a worktree on its own.

## Goals

- One kind of worktree: every worktree Lewbox makes is a `wtr_` resource, whether made with a conversation or before one.
- Make one before a conversation, start conversations in it, remove it when its last conversation goes.
- Remove a worktree without deleting a conversation (Lewagent's Archive).
- Hear about worktrees made and removed by other clients.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Paths, branch defaults, `from`, `copy`, names and their rule, the agent starting at the same relative place, `not_a_repository`, `worktree_failed` with git's message, `worktree_dirty` and `?force=true`, branches kept on removal, worktrees left in stopped sandboxes |
| **Change** | A worktree made with a conversation is a `wtr_` too; the conversation's `worktree` gains `id`. Deleting a conversation removes its worktree only if no other conversation works in it, so callers no longer have to work out `keep_worktree` (they still can pass it) |
| **Add** | `POST /v0/sandboxes/{sbx}/worktrees`, `GET` list and one, `DELETE /v0/worktrees/{wtr}`; `worktree: {id}` on starting a conversation; `label`; activity and webhooks; `add_worktree` and `remove_worktree` by id in agentd |

## Design

### With a conversation

`worktree` on `POST /v0/sandboxes/{sbx}/conversations` ([conversations](conversations.md#start-a-conversation)):

| Form | Means |
|---|---|
| `true` | A new worktree with the defaults |
| `{branch, from, copy, name}` | A new worktree with these |
| `{id}` | **New.** Work in an existing worktree. `branch`, `from` or `copy` beside `id` is `422`. `404 not_found` if there's no such worktree in this sandbox, `409 worktree_removed` if it has been removed |

| Field | Default | |
|---|---|---|
| `branch` | `lewbox/<wtr_id>` | The new branch, as git takes a branch's name. (Legacy defaulted to `lewbox/<cnv_id>`; now the worktree's id) |
| `from` | What the repository has checked out | A branch, tag or commit |
| `copy` | none | Up to 20 paths relative to the repository's root, copied in: `.env` and other untracked files |
| `name` | the worktree's id | Its folder in `/root/workspace/.worktrees/` |

A new worktree is made by agentd's `add_worktree` before the prompt goes on, then the conversation starts at `directory`'s place inside it. The conversation's `directory` is then that place in the worktree, and its `worktree` is `{id, path, branch, from, repository}`.

### Standalone worktrees

`POST /v0/sandboxes/{sbx}/worktrees`, `sandboxes.agents`. The sandbox must be running.

| Field | | |
|---|---|---|
| `directory` | string | Required. A directory in the repository, as a conversation's `directory` is |
| `branch`, `from`, `copy`, `name` | | As above |
| `label` | string | For the activity log (`worktree.created`), as a command's; or `X-Lewbox-Label` |

Takes an `Idempotency-Key`: a retry answers the worktree made the first time. `201` with the [worktree](#the-worktree-object).

- `GET /v0/sandboxes/{sbx}/worktrees`: the sandbox's worktrees that haven't been removed, newest first; `?removed=true` includes removed ones. Cursor pages. Any key that sees the sandbox.
- `GET /v0/worktrees/{wtr}`: one, removed or not.
- `DELETE /v0/worktrees/{wtr}`: [Removing](#removing).

Lewagent's flow:

```
POST /v0/sandboxes/sbx_…/worktrees   {directory: "/root/workspace/acme-web", from: "main", copy: [".env"], name: "fix-login"}
POST /v0/sandboxes/sbx_…/commands    {command: "composer install && php artisan migrate", directory: "/root/workspace/.worktrees/fix-login"}
POST /v0/sandboxes/sbx_…/conversations {agent: "claude-code", prompt: "…", directory: "/root/workspace/acme-web", worktree: {id: "wtr_…"}}
```

### The worktree object

| Field | | |
|---|---|---|
| `id` | string | `wtr_…` |
| `sandbox` | string | |
| `path` | string | `/root/workspace/.worktrees/<name>` |
| `name` | string | |
| `branch` | string | |
| `from` | string | What it was made from: a branch name, or a commit |
| `repository` | string | The repository's root in the sandbox |
| `copied` | array | The paths copied in |
| `conversations` | array | Ids of conversations working in it |
| `label` | string or null | |
| `created_at` | string | |
| `removed_at` | string or null | |

### Removing

`DELETE /v0/worktrees/{wtr}`, `sandboxes.agents`, `204`:

1. A conversation that works in it and isn't deleted: `409 worktree_in_use`, naming them. (For Lewagent's Archive, `?detach=true` (**new**) removes the worktree anyway; those conversations keep their history but can't be prompted again: a prompt is `409 worktree_removed`.)
2. Uncommitted changes, other than the paths it copied in: `409 worktree_dirty` unless `?force=true`.
3. A stopped sandbox: `409 sandbox_not_running`.
4. agentd's `remove_worktree` runs `git worktree remove [--force]`, keeping the branch. One already gone from the sandbox is removed without complaint.
5. `removed_at` is set; `worktree.removed` is logged and sent.

**With conversations:** deleting a conversation removes its worktree when no other undeleted conversation works in it, and not with `?keep_worktree=true`. A worktree no conversation ever worked in stays until it's deleted. In a stopped sandbox the conversation is deleted and the worktree's record stays, so `DELETE` can remove it once the sandbox runs.

### In agentd

- **`add_worktree`** `{id, directory, branch, from, copy, name}`: legacy's `addWorktree`, keyed by the worktree's id and name rather than a conversation's. It finds the repository (`git rev-parse --show-toplevel` from `directory`), checks the name, runs `git worktree add -b <branch> <path> <from>`, copies the paths (files and directories, keeping modes), and writes its meta to `/root/.lewbox/worktrees/<wtr>.json` (`{id, path, branch, from, repository, copied}`). Sent again with the same request, it answers the worktree already made, so a retry is safe. A per-sandbox lock makes two adds with one name race safely.
- **`remove_worktree`** `{id, force}`: the dirty check runs `git status --porcelain` in the worktree and ignores the copied paths; then `git worktree remove`, and the meta file goes.
- A first prompt with `worktree.id` reads the meta to find the path and starts the agent in the right place.
- The sandbox binary is uploaded by version, so running sandboxes get these commands without a template rebuild.

### Data

`agent_worktrees`: `id`, `public_id` (`wtr_`), `sandbox_id`, `profile_id`, `name`, `path`, `branch`, `from_ref`, `repository`, `copied jsonb`, `label`, `created_by` (actor), `created_at`, `removed_at`. Unique `(sandbox_id, name) WHERE removed_at IS NULL`. `agent_conversations.worktree_id` references it. Migrating legacy data: each conversation's `worktree` JSON becomes a row ([migration](../migration.md)).

## API

| Route | Permission | |
|---|---|---|
| `POST /v0/sandboxes/{sbx}/worktrees` | `sandboxes.agents` | Make one |
| `GET /v0/sandboxes/{sbx}/worktrees` | any | List |
| `GET /v0/worktrees/{wtr}` | any | One |
| `DELETE /v0/worktrees/{wtr}` | `sandboxes.agents` | `?force`, `?detach` |

### Errors

| Status | Code | When |
|---|---|---|
| `404` | `not_found` | No such worktree or sandbox the key sees |
| `409` | `sandbox_not_running` | |
| `409` | `worktree_failed` | git couldn't make it (branch exists, `from` unknown); message is git's |
| `409` | `already_exists` | Another worktree has the name and isn't on the branch this request would make |
| `409` | `worktree_in_use` | Removing one conversations work in |
| `409` | `worktree_dirty` | Uncommitted changes |
| `409` | `worktree_removed` | Starting or prompting a conversation in a removed worktree |
| `422` | `not_a_repository` | `directory` isn't in a git repository |
| `422` | `validation_failed` | A bad name, more than 20 `copy` paths, `branch`/`from`/`copy` beside `id` |

(`already_exists` replaces legacy's `worktree_failed` for a taken name, so clients can tell them apart; it's the one code for a taken name across the API, [api/errors](../api/errors.md).)

## Security

- Worktrees are directories in the sandbox; anything there can change them. Lewbox's record is what Lewbox made, not a guarantee of what's on disk.
- Copied paths may hold secrets (`.env`); they're copied inside the sandbox, never through the control plane.

## Observability

- Activity: `worktree.created` and `worktree.removed`, with `label`, the actor, and the conversation when made or removed with one. Webhooks `activity.worktree.created`, `activity.worktree.removed` ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).
- Metrics: worktrees made, removed, refused dirty, failed by reason.

## Testing

- agentd unit tests (ported `worktree_test.go`): defaults, names (valid, invalid, taken, retried on the same branch), copy, dirty ignoring copied paths, remove forced, gone already.
- Service tests: standalone create, conversation in it, second conversation in it, deleting the first (kept), deleting the second (removed), `keep_worktree`, stopped sandbox, `detach`, idempotent create.

## Build plan

1. `wtr_` rows for worktrees made with conversations; `id` on the conversation (1 day).
2. `add_worktree`/`remove_worktree` by id in agentd (1 day).
3. The standalone routes, removal rules, activity and webhooks (2 days).

## Open questions

1. **Worktrees removed by hand** (legacy open decision 9): leave the record until something reads it (proposed); check paths on every list while the sandbox runs (a command per list); or have agentd watch `/root/workspace/.worktrees` and report removals?
2. **Worktrees Lewbox didn't make** (legacy open decision 10): list every `git worktree list` entry of the sandbox's repositories for a picker, or only Lewbox's own (proposed)? An "adopt" call (`POST …/worktrees` with `path` of an existing one) could bring the Git panel's under Lewbox.
3. **Default branch name** changes from `lewbox/<cnv_id>` to `lewbox/<wtr_id>`. Any consumer depending on the old one? (Lewagent names its branches.)
4. **`detach`** for Archive: or should Archive be a conversation state (`archived`, worktree removed, history kept, can't be prompted)?
