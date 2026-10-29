# Hooks and scrub paths

> Status: Draft · Milestone: Alpha (M2) · Core · Updated: 2026-10-29

A sandbox can be given commands to run at points in its life, and paths to delete before it's ever saved. `hooks.on_create` runs once, after the sandbox is first set up; `hooks.on_start` runs every time it starts; `hooks.on_stop` runs before every stop, whatever asked for it, while the sandbox and its credentials are still there. `scrub` lists files and directories that are removed from the sandbox before any snapshot is taken, on the node, even when nothing in the sandbox can run any more. This plan covers the fields, when each hook runs and with what, limits, results, permissions, templates, and how hooks still run when AerolVM's own timers stop a sandbox.

```json
POST /v0/sandboxes
{
  "template": "claude-code",
  "persist": true,
  "idle_timeout": 1800,
  "hooks": {
    "on_create": [{ "label": "install", "command": "npm ci", "directory": "~/workspace/web", "timeout": 600 }],
    "on_start":  [{ "label": "dev-server", "command": "npm run dev", "directory": "~/workspace/web", "background": true }],
    "on_stop":   ["git -C ~/workspace/web stash push -u -m lewbox-stop || true"]
  },
  "scrub": ["~/workspace/web/.env", "~/.npmrc"]
}
```

The user's first sketch called this `execute: {onStart, onStop}`. We use `hooks` with `on_create`, `on_start` and `on_stop`, as the words other platforms use for the same idea (lifecycle hooks), and snake_case like every other field; the TypeScript SDK takes `hooks: {onCreate, onStart, onStop}`. The name was agreed on 2026-10-29.

## Today

Nothing like it exists in legacy Lewbox:

- **Start-up commands** are the image's own `CMD`/`ENTRYPOINT`, re-run on every start (after the nested-toolbox fix, [persistence](persistence.md#container-command)). There's no per-sandbox command and no way to run something after setup.
- **Nothing runs before a stop.** The gateway's stop runs Lewbox's own setup cleanup, only for stops it was asked for; AerolVM's timer stops run nothing ([timers-and-activity](timers-and-activity.md#today)).
- **Nothing is removed before a snapshot** but Lewbox's own credentials, and those only for stops that were asked for (fixed in the rebuild by [profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).
- **Lewagent builds it all itself.** Its Build panel runs each repository's setup as commands and watched sessions after the create, through the gateway (`lewagent-legacy/app/Actions/Setup/RunSetupStep.php`), with "commands run once (`create`), on every start (`start`), or by hand (`manual`)" (`lewagent-legacy/docs/todo.md` l.348). It asked Lewbox for **setup steps on create**, `setup: [{name, command, timeout}]`, run once after the clones and reported per step as `repositories` is, so installs "survive anything going wrong in Lewagent, and other apps get the same" (`lewagent-legacy/docs/services.md` § *What Lewbox needs*; `docs/todo.md` l.529). Its `small-todo.md` also wants a computer to clean up after itself.

## Goals

- Run a sandbox's own commands at creation, at every start and before every stop, including stops Lewbox didn't ask for: idle timeouts, lifetimes, credits, suspensions, node drains.
- Let the user delete paths before a snapshot, reliably, even after a crash or a timer stop.
- Report every run: exit code, duration, a redacted stderr tail, in the sandbox object, the activity log and webhooks.
- Cover Lewagent's setup steps with `on_create` and `on_start`, so it can drop most of its own runner.
- Keep stops bounded: no hook can hold a sandbox (and its metering) running indefinitely.

### Non-goals

- A build system. Hooks are commands; ordering is the list's, there are no dependencies, conditions or retries.
- Hooks on other events (a port opened, a conversation finished). Webhooks and the API cover reacting to those from outside.
- Running hooks on a stop of a sandbox that isn't running (already stopped, crashed, lost with its node). `scrub` still applies wherever there's a filesystem to scrub.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The image's `CMD` as the way to start a template's long-running service, and Lewbox's own setup and cleanup, which run around the hooks |
| **Change** | — (new) |
| **Add** | `hooks` (`on_create`, `on_start`, `on_stop`) and `scrub` on sandboxes and templates; a `hooks` report; `hook.ran` activity events; the Lewbox-owned stop decision that lets `on_stop` run before timer stops ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)) |

## Design

### Fields

`hooks` is an object with three lists. Each entry is a string (the command) or an object:

| Field | Type | Rules | Default |
|---|---|---|---|
| `command` | string | Required. Run with `bash -lc` (else `sh -lc`), as [commands](../inside-a-sandbox/commands.md) are. Up to 16 KiB | |
| `label` | string | `^[a-z0-9][a-z0-9._-]{0,63}$`, unique within its list; shown in reports and activity | `on_start.0`, `on_start.1`, … |
| `directory` | string | Absolute or `~/…`. A directory that doesn't exist fails the hook with `directory_missing` | `/root` |
| `env` | object | Up to 50 string pairs, added to the sandbox's. **Write-only**, like the sandbox's `env`: never returned | `{}` |
| `timeout` | integer | Seconds. `on_create`/`on_start`: 1 to 3600. `on_stop`: 1 to the sandbox's `stop_grace` | 300 (`on_stop`: `stop_grace`) |
| `background` | boolean | `on_create`/`on_start` only. Start it as a [session](../inside-a-sandbox/sessions.md) named `hook:<label>` and go on without waiting; for dev servers and watchers. Its exit is reported when the session ends | `false` |
| `required` | boolean | `on_create`/`on_start` only. If it fails, the hooks after it are skipped and the operation ends `failed` with `hook_failed` (the sandbox stays `running`). Set `false` for a hook whose failure should only be reported | `true` |

Beside `hooks`, two more fields:

| Field | Type | Rules | Default |
|---|---|---|---|
| `stop_grace` | integer | Seconds the `on_stop` hooks have in total, from 0 to 600. `0` runs no `on_stop` hooks | 60 |
| `scrub` | array of strings | Paths removed before any snapshot ([scrub](#scrub)). Up to 50 | `[]` |

**Limits:** up to 20 hooks in each list (the template's and the sandbox's together), 64 KiB of commands in all. Over is `422 validation_failed` naming the field.

### When hooks run

| Hook | Runs | Operation and step |
|---|---|---|
| `on_create` | **Once**, at create, after setup, clones and ports, so it finds the repositories cloned and the credentials in place. Not again on a start, not on a restart | `create`, step `hooks_ran` |
| `on_start` | **Every start:** create (after `on_create`), start (in place or a resume on another node), restart | `create`, `start`, `restart`: step `hooks_ran` |
| `on_stop` | **Every stop of a running sandbox**, whatever asked for it: a person or key, an idle timeout, a lifetime, credits, a budget, a suspension, a node drain moving it, a restart, and the stop of a sandbox that doesn't persist (which is a delete). And a `DELETE` of a running sandbox, unless `?hooks=false` | `stop`, `restart`, `delete`: step `stop_hooks_ran`, **first**, before Lewbox's harvest and cleanup |

They don't run on `apply_setup`, on operations Lewbox runs for itself (a scrub by restart, [profile-setup](profile-setup.md#older-fork-releases)), or on a sandbox that isn't running when its stop begins (stopped by its node crashing, already stopped by AerolVM's backstop: then they ran there, [below](#when-lewbox-cant-act)).

**In order:** a list runs top to bottom, the template's hooks before the sandbox's ([templates](#templates)). Foreground hooks run one at a time; a `background` hook starts its session and the next begins at once.

**The operation waits for foreground hooks.** The sandbox is `running` before its hooks run (its status changes at step `running`), so commands, files and terminals work meanwhile, but the operation finishes only after `hooks_ran`. A client that needs "set up and ready" waits for the operation; one that needs "reachable" waits for the status. A create with `wait: true` that's still running hooks after 60 s answers `202` with the operation, as any slow create does ([lifecycle](lifecycle.md#waiting)).

### What a hook runs with

- **A login shell, as root,** as every command: `/etc/profile.d` and the setup's env file are loaded, so the agents' keys, Git's helper and the telemetry settings are there.
- **The profile's credentials are present** for `on_stop` too: it runs before Lewbox's harvest and cleanup, so `git push` with a write token, or a final upload, works ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).
- **Environment variables** Lewbox adds: `LEWBOX_SANDBOX_ID`, `LEWBOX_HOOK` (`on_create`, `on_start`, `on_stop`), `LEWBOX_HOOK_LABEL`, `LEWBOX_OPERATION_ID`, and for `on_stop`, `LEWBOX_STOP_REASON` (`requested`, `idle_timeout`, `lifetime_reached`, `credits_exhausted`, `budget_reached`, `profile_suspended`, `node_drain`, `restart`, `delete`) and `LEWBOX_STOP_DEADLINE` (Unix seconds).
- **Not counted as activity** ([timers-and-activity](timers-and-activity.md#what-counts-as-activity)): hooks are Lewbox's maintenance calls (`X-Lewbox-Maintenance`), so an `on_start` hook doesn't reset the idle timer and an `on_stop` hook can't cancel the stop it's part of. A `background` hook's session counts as any session does once it moves data.

### Stops are bounded

`on_stop` hooks share `stop_grace` (default 60 s, at most 600 s): each runs with `min(timeout, time left)`, and when it runs out, the running hook is killed (`TERM`, then `KILL` after 5 s) and the rest are reported `skipped` with `stop_grace_exhausted`. Then the stop carries on: harvest, cleanup, stop, scrub, snapshot ([operations](operations.md#stop)).

- **Metering.** A stop ends metering when it's asked for, as now; the grace is never metered. That's why it's capped, and why the cap is ten minutes, not an hour.
- **Credits and suspensions** use `min(stop_grace, LEWBOX_HOOKS_FORCED_STOP_GRACE)` (default 30 s), so a profile out of credits can't keep sandboxes running for long.
- **A node drain** uses the full grace: the sandbox is moving, not being cut off.

### Failures

- **`on_stop`** never fails the stop. A failure, a timeout or a skip is reported, and the stop goes on.
- **`on_create` and `on_start`** fail their operation when a hook fails, as hooks are `required` by default (decided 2026-10-29); a hook marked `required: false` is only reported, and the hooks after it still run. A failed required hook skips the hooks after it, and the operation ends `failed` with `error_code: hook_failed` and the hook's label; the sandbox stays `running`, so the user can open a terminal and look. A create that was still waiting answers `502 hook_failed`, naming the sandbox (which is running, unlike after `create_failed`). There's no automatic retry: run the hook again by hand, or restart.
- **The toolbox not answering** (a sandbox that just started, under heavy load) is retried for `LEWBOX_TOOLBOX_WAIT` (30 s), as any command ([inside-a-sandbox/toolbox](../inside-a-sandbox/toolbox.md)); then the hook fails with `toolbox_unavailable`.

### The report

On the sandbox object, `hooks` holds the definitions (without `env`) and the last run of each hook:

```json
"hooks": {
  "on_create": [{ "label": "install", "command": "npm ci", "directory": "/root/workspace/web", "timeout": 600, "source": "sandbox" }],
  "on_start":  [{ "label": "dev-server", "command": "npm run dev", "directory": "/root/workspace/web", "background": true, "source": "sandbox" }],
  "on_stop":   [{ "label": "on_stop.0", "command": "git -C ~/workspace/web stash push -u -m lewbox-stop || true", "timeout": 60, "source": "sandbox" }],
  "stop_grace": 60,
  "runs": [
    { "hook": "on_create", "label": "install", "source": "sandbox", "operation": "op_…", "status": "succeeded",
      "exit_code": 0, "started_at": "…", "duration_ms": 48211, "error": null, "stderr": null },
    { "hook": "on_stop", "label": "on_stop.0", "source": "sandbox", "operation": "op_…", "status": "failed",
      "exit_code": 1, "started_at": "…", "duration_ms": 312, "error": "exit_code", "stderr": "fatal: not a git repository … [redacted]" }
  ]
}
```

- `status`: `succeeded`, `failed` (non-zero exit), `timed_out`, `killed`, `skipped` (with `error` saying why: `stop_grace_exhausted`, `required_failed`, `sandbox_not_running`, `fork_unsupported`), `running` (a background hook's session), or `started` (a background hook whose session is still up).
- `stderr`: the last 2 KB of a hook that didn't succeed, redacted as command stderr is ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md#whats-never-kept)): secrets Lewbox knows (the profile's credentials, the hook's own `env`) are removed by value, and token-shaped strings by pattern. Never stdout.
- `runs` keeps the latest run of each hook; history is in the activity log.

### Activity and webhooks

- Each run writes `hook.ran` to the sandbox's activity log (group `hooks`, new), with `outcome` (`succeeded`, `failed`, `timed_out`, `killed`), `label`, `details: {hook, source, exit_code, duration_ms, background, stop_reason}`, the redacted stderr tail, and the operation. Skipped hooks write nothing; the report shows them.
- Webhooks get `activity.hook.ran` like every activity kind ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)), so a project can subscribe to `activity.hook.*` and filter on `outcome`.

### Changing hooks

`PATCH /v0/sandboxes/{sbx}` with `hooks`, `stop_grace` and/or `scrub` ([lifecycle](lifecycle.md#settings-and-metadata)):

- `hooks` replaces the sandbox's own lists (the template's aren't touched); a list left out is kept, `[]` empties it. `scrub` replaces the list.
- Allowed while the sandbox is in any status but `deleted` and `failed`, and **refused while an operation runs** (`409 operation_in_progress`), so a stop in progress uses the hooks it started with.
- On a running sandbox, the change is written to its backstop script at once ([below](#when-lewbox-cant-act)); if that write fails, the change is saved and `503 toolbox_unavailable` says the backstop has the old ones until the next start.
- `on_create` can be changed but never runs again; changing it is allowed so a sandbox's definition can be copied to another.

### Permissions

Hooks run code later, unattended, as root, with the profile's credentials. So:

| Action | Needs |
|---|---|
| Create with `hooks` or `scrub` | `sandboxes.create` **and** `sandboxes.exec` |
| Change `hooks`, `stop_grace` or `scrub` | `sandboxes.update` **and** `sandboxes.exec` |
| Read the definitions and the report | Nothing beyond reading the sandbox |

**Sandbox keys can't set hooks** (`403 wrong_key_kind`). A sandbox key is a 15-minute key handed to a browser; a hook planted with one would run long after the key expired, at every start and stop, with the profile's credentials, beyond what the person holding the key was trusted with. Sandbox keys don't hold `sandboxes.update` anyway ([api/permissions](../api/permissions.md#sandbox-permissions)); the rule is stated so a future permission doesn't open it by accident. Admin keys and profile keys with both permissions can, and so can dashboard members.

### Templates

A template can carry `hooks`, `stop_grace` and `scrub` too ([templates](../templates.md)), for what every sandbox from it needs: a desktop template's `on_stop` that saves the browser profile, a dev-stack template's `on_start` that starts its services.

- The template's hooks run **before** the sandbox's in each list, and its `scrub` paths are added to the sandbox's. A sandbox's `stop_grace` overrides the template's.
- The sandbox **keeps a copy** of the template's hooks and scrub paths as they were at create (`source: "template"` in the report), so a template change never alters a running sandbox's stop, as with a template's ports. Changing a template's hooks needs no build.
- Labels must be unique across the template's and the sandbox's in each list; a clash is `422 validation_failed` on `hooks`.

### Scrub

`scrub` lists paths the user wants gone **before any snapshot**: a `.env`, a browser profile, a cache of downloaded secrets, a `~/.npmrc` with a token in it. It's the same mechanism that keeps Lewbox's own credentials out of snapshots ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)), with the user's paths added:

- **Where:** at every stop that snapshots or leaves a sandbox stopped on its node, the user's paths are merged into the scrub manifest and removed **on the node, from the stopped container's filesystem** (fork change F46, [clusters/aerolvm-fork](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)), after the `on_stop` hooks and after AerolVM has stopped it. So it works when the sandbox crashed, when its toolbox is gone, and when AerolVM's backstop stopped it.
- **Verified like Lewbox's own:** the fork's scrub report lists every path (`absent`, `removed`, `whited_out`); a path not accounted for fails the scrub, and the snapshot is never uploaded (`scrub_failed`, [profile-setup](profile-setup.md#when-a-scrub-fails)). A path that doesn't exist is fine.
- **What it means for the user:** the path is gone when the sandbox starts again. It's deleted, not hidden: there's no restoring it from the snapshot.
- **Not on a restart**, which takes no snapshot; not on a sandbox that doesn't persist, whose stop deletes everything.

**Paths:**

| Rule | |
|---|---|
| Absolute, or starting `~/` (the root user's home, `/root`) | `projects/.env` without `~/` or `/` is `422` |
| No `..`, no glob characters (`*`, `?`, `[`), no NUL; up to 1,024 bytes each | Globs need a listing of every layer; Later |
| A directory is removed with everything under it | |
| Symbolic links are removed, never followed | |
| Refused: `/`, and anything under `/proc`, `/sys`, `/dev`, `/run/lewbox` (Lewbox's own, scrubbed anyway), `/usr/local/lib/lewbox` (Lewbox's helpers), `/root/.lewbox` (agentd's conversations and Lewbox's state) | `422 validation_failed` on `scrub`, naming the path |

The in-sandbox cleanup doesn't remove the user's scrub paths for stops it can run before: deleting them while `on_stop` hooks and processes may still use them would surprise; the node-side removal, after the stop, is the one that counts.

### When Lewbox can't act

Hooks are run by the stop operation, through the toolbox, when Lewbox decides to stop a sandbox, which it now does for idle timeouts and lifetimes too: the fork's timers tell Lewbox instead of stopping ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)). If Lewbox doesn't act in time (the control plane is down, the node can't reach it), the fork's **backstop** stops the sandbox itself, after running a script Lewbox registered with it (fork change F49):

1. **Setup writes the script** on every apply and on every hook change: `/run/lewbox/hooks/on-stop.sh`, on the memory-only mount (it may hold hook `env` values, so it's never on disk or in a snapshot), runs the template's and sandbox's `on_stop` hooks in order with their timeouts within `stop_grace`, then Lewbox's own in-sandbox cleanup, and appends each hook's result as a JSON line to `/root/.lewbox/hooks/backstop.jsonl`.
2. **Lewbox registers it** with AerolVM when the sandbox is created or started (`pre_stop: {command: "sh /run/lewbox/hooks/on-stop.sh", timeout: stop_grace + 15}`).
3. **When the backstop fires**, AerolVM runs it, then stops the sandbox and emits `stopped` with `backstop: true`. When Lewbox next hears from the node, it offloads the sandbox as for any timer stop; the scrub reads `backstop.jsonl` (`read`) and then removes it (`remove`), and Lewbox turns it into the hooks report and `hook.ran` events, redacting stderr then. Harvest happens in the same scrub ([profile-setup](profile-setup.md#harvest-first)).

On a fork release without F49, the backstop stops without running anything: `on_stop` hooks are reported `skipped` with `fork_unsupported`, and the scrub paths are still removed (F46). On one without F50 (no notify mode, so AerolVM stops on its timers as legacy does), timer stops take the same path as a backstop stop.

## API

Fields on `POST /v0/sandboxes` and `PATCH /v0/sandboxes/{sbx}`, and on the sandbox object ([lifecycle](lifecycle.md#fields)); on templates ([templates](../templates.md)). One new query parameter, `DELETE /v0/sandboxes/{sbx}?hooks=false`, to delete without running `on_stop` (needs `sandboxes.delete`).

Run a hook again by hand? Not an endpoint: it's a [command](../inside-a-sandbox/commands.md). The dashboard's *Run again* sends the hook's command as one, labelled `hook.<label>`.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | Hooks or scrub without `sandboxes.exec` beside `sandboxes.create`/`sandboxes.update` |
| `403` | `wrong_key_kind` | A sandbox key setting hooks or scrub |
| `409` | `operation_in_progress` | Changing hooks while an operation runs |
| `422` | `validation_failed` | Too many hooks, a bad label, a timeout out of range, `background` or `required` on `on_stop`, a refused scrub path |
| `502` | `hook_failed` | A create that waited, whose `required` hook failed. Also an operation's `error_code` |

## Security

- **Hooks are code with the profile's credentials, run unattended.** Hence `sandboxes.exec` on top of create and update, no sandbox keys, and every run in the activity log with its actor (whoever last set the hooks is recorded on the sandbox as `hooks_updated_by`).
- **Hook `env` is write-only**, encrypted at rest with the keyring like the sandbox's `env`, and only ever written into the sandbox on the memory-only mount, so it never reaches a snapshot.
- **The backstop script** lives on `/run/lewbox` for the same reason; its results file on disk holds only exit codes, durations and raw stderr tails, which the scrub removes before upload and Lewbox redacts before keeping.
- **`on_stop` can't extend a stop past `stop_grace`**, and forced stops (credits, suspension) shorten it, so hooks can't be used to keep a sandbox running unmetered.
- **Scrub is best understood as deletion,** not as a secret store: a process may have copied the file elsewhere. The docs say so, as profile setup's do for Lewbox's own credentials.

## Observability

- **Metrics:** `lewbox_hook_runs_total{hook,status}`, `lewbox_hook_duration_seconds{hook}`, `lewbox_hook_stop_grace_exhausted_total`, `lewbox_backstop_stops_total`, `lewbox_scrub_user_paths_total{result}`.
- **Logs:** one line per run at `info`, failures at `warn`, with sandbox, hook, label and operation; never the command's `env`.
- **Activity and webhooks:** `hook.ran` / `activity.hook.ran`, above.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_HOOKS_DEFAULT_TIMEOUT` | `300s` | A start or create hook's timeout when none is given |
| `LEWBOX_HOOKS_MAX_TIMEOUT` | `1h` | The most an `on_create`/`on_start` hook may ask for |
| `LEWBOX_HOOKS_DEFAULT_STOP_GRACE` | `60s` | `stop_grace` when none is given |
| `LEWBOX_HOOKS_MAX_STOP_GRACE` | `10m` | The most `stop_grace` may be |
| `LEWBOX_HOOKS_FORCED_STOP_GRACE` | `30s` | The most `on_stop` hooks get on a stop for credits, a budget or a suspension |
| `LEWBOX_HOOKS_MAX_PER_LIST` | `20` | Template's and sandbox's together |
| `LEWBOX_SCRUB_MAX_PATHS` | `50` | Per sandbox, the template's included |

## Testing

- **Unit:** field validation (labels, timeouts, refused paths, `~` expansion), merging template and sandbox lists, the budget arithmetic for `stop_grace` and forced stops, the backstop script's rendering (golden files, `shellcheck` in CI).
- **Fake toolbox:** hooks run in order with their env; a `required` failure skips the rest and fails the operation with the sandbox still running; a background hook becomes a session; `on_stop` killed at the grace, the rest `skipped`; stderr redacted by value.
- **Fake AerolVM:** a notify-mode idle event leads to a stop operation whose first step runs `on_stop`; a backstop stop (no Lewbox) leads to the offload reading `backstop.jsonl` into the report; a fork without F49 reports `fork_unsupported`; user scrub paths appear in the scrub request and a missing one in the report fails the snapshot.
- **Live, each isolation level:** a sandbox with `on_start` writing a file, `on_stop` committing in a repository and `scrub: ["~/secret.txt"]`; stop by request, by idle timeout and by the backstop (control plane stopped); start again: the commit is there, `~/secret.txt` isn't, and the snapshot's layers don't contain it.

## Build plan

1. **M2:** fields, validation and storage (`sandbox_hooks` columns, template copy), the report and `hook.ran` (2 days).
2. **M2:** the `hooks_ran` step in create, start and restart; `stop_hooks_ran` in stop, restart and delete, with the grace budget (2 days).
3. **M2:** user scrub paths merged into the scrub manifest and the report check (with [profile-setup](profile-setup.md)'s scrub) (1 day).
4. **M2–M3:** the backstop script, its registration (F49) and reading `backstop.jsonl` on offload (2 days, after the fork change).
5. **M3:** dashboard (create dialog, sandbox page), SDK and CLI (with [dashboard/sandbox-pages](../dashboard/sandbox-pages.md), [sdks-and-cli](../sdks-and-cli.md)).

**Why M2:** hooks need the toolbox client and commands (M2) and the stop operation with its scrub (M1). The alpha needs them: Lewagent's setup steps move onto `on_create`/`on_start` ([migration/lewagent](../migration/lewagent.md)), and timer stops need somewhere for the user's cleanup to run.

## Open questions

1. ~~**`on_create` as a third hook.**~~ Decided (2026-10-29): kept, beside `on_start` and `on_stop`.
2. ~~**`required` on start hooks.**~~ Decided (2026-10-29): kept, and **`true` by default** for `on_create` and `on_start`: a failing hook fails its operation unless it's marked `required: false`. `on_stop` hooks never block a stop; their failures are reported.
3. **Glob scrub paths** (`~/workspace/*/.env`). Useful; needs the fork to match against every layer, not just the top one. Later?
4. **Hooks on profile level.** Should a profile (or project) have default hooks for all its sandboxes, as it has setup? (Proposed: Later; templates cover the common case.)
5. ~~**Name.**~~ Decided (2026-10-29): `hooks`, with `on_create`/`on_start`/`on_stop` (`onCreate`/`onStart`/`onStop` in the TypeScript SDK).
