# Migration: Lewagent

> Status: Draft · Milestone: Beta · Updated: 2026-10-29

Lewagent is Lewbox's largest consumer: every Lewagent "computer" is a Lewbox sandbox. It calls Lewbox from its PHP backend (Admin and Consumer APIs through the PHP SDK), from the browser (sandbox keys straight to the gateway), and receives Lewbox's webhooks. The new API isn't wire-compatible ([D4](../README.md#decisions)), so Lewagent has to move. This plan lists every call Lewagent makes, where it goes in the new API, what Lewagent can stop working around, and the order of the move.

Paths for the new API below follow the shape [api](../api.md) proposes (profile- and sandbox-key routes at `/v0/…`, admin routes at `/v0/projects/{prj}/…`); the final paths come from there.

## Today

All paths are under `lewagent-legacy/`.

- **SDK:** `packages/lewbox-php` (namespace `Lewbox\`), an identical copy of `lewbox-legacy/packages/lewbox-php-sdk`, synced by `app/Console/Commands/SyncLewboxSdkCommand.php`. Used in **46 PHP files**.
- **Config:** `config/lewbox.php`, `.env`: `LEWBOX_ADMIN_URL` (`https://box.harlew.com/api/v1`), `LEWBOX_ADMIN_KEY` (`lbt_`), `LEWBOX_API_URL` (`https://lewbox-gateway.laravel.cloud/v1`), `LEWBOX_TIMEOUT`, `LEWBOX_MAX_RETRIES`, `LEWBOX_WEBHOOK_SECRET`. `config/lewagent.php`: the computer template (`lewagent-computer`, 4 vCPU, 12 GB, 30 GB), locations to cluster slugs (`brighton` → `eu-west`, `new-york` → `new-york`), agent models and modes, the 20,000-character system prompt limit, 20 repositories per create.
- **Auth:** one Lewbox profile **per user** (`app/Jobs/Users/ProvisionLewboxProfile.php`), its `lbp_` key in `users.lewbox_api_key`, a `Lewbox` client per user (`User::lewbox()`, `app/Models/User.php:143`); the tenant key for admin calls; sandbox keys minted for the browser by `app/Http/Controllers/Computer/AgentKeyController.php`.
- **Webhooks in:** `POST /webhooks/lewbox` (`routes/hooks.php` → `app/Http/Controllers/Webhooks/LewboxWebhookController.php` → `app/Jobs/Computers/HandleLewboxWebhook.php`), Standard Webhooks verified, deduplicated by event id for 3 days.
- **Polling on top:** `lewagent:refresh-computers` every minute, `check-builds` every 5 s (1 min with a webhook secret), `settle-checkout-tasks` every 15 s (1 min), and `resources/js/components/computer-polling.tsx` in the browser.
- **Waiting on Lewbox:** `docs/todo.md` lines 498–538 list what Lewagent needs from Lewbox ([Wanted](#what-lewagent-is-waiting-for)).

## Goals

- Lewagent works on the new Lewbox with no change its users notice.
- Lewagent's own code gets simpler: workarounds for legacy bugs and gaps go away.
- One SDK on each side (PHP on the server, `@lewbox/sdk` in the browser), installed from package registries.
- The move is tested end to end before the switch, and can be undone.

## Design

### Server calls: Consumer API (per-user profile key)

| Lewagent file (`app/…`) | Legacy call | New API | Notes |
|---|---|---|---|
| `Actions/Computers/CreateComputer.php` | `POST /sandboxes` (`template`, `cluster`, `cpu`, `memoryMb`, `diskGb`, `idleTimeout`, `env: {SELKIES_FRAME_ANCESTORS}`, `metadata: {computer}`, `persist: true`, `repositories`, `wait: false`, idempotency key = the computer's public id) | `POST /v0/sandboxes` | Same fields. `SELKIES_FRAME_ANCESTORS` stays the app's business |
| `Jobs/Computers/RefreshComputerStatus.php`, `Jobs/Computers/SetUpComputer.php`, `Http/Controllers/Computer/SetupController.php` | `GET /sandboxes/{id}` | `GET /v0/sandboxes/{sbx}` | |
| `Actions/Computers/StopComputer.php`, `StartComputer.php` | `PATCH /sandboxes/{id}` `status` | `PATCH /v0/sandboxes/{sbx}` | |
| `Actions/Computers/RestartComputer.php` | `POST …/operations {kind: restart}` | `POST /v0/sandboxes/{sbx}/operations` | |
| `Actions/Computers/DeleteComputer.php` | `DELETE` via `kill(wait: false)` | `DELETE /v0/sandboxes/{sbx}` | Lewagent's `small-todo.md` wants a "deleting" status until it's gone: follow the delete operation, or the `sandbox.deleted` webhook |
| (all lifecycle) | `GET …/operations/{op}` | `GET /v0/sandboxes/{sbx}/operations/{op}` | |
| `Http/Controllers/Computer/PortController.php`, `Actions/Setup/RunSetupStep.php` | `PUT`/`DELETE …/ports/{port}` | `PUT`/`DELETE /v0/sandboxes/{sbx}/ports/{port}` | Exposing an already-private port again now keeps its proxy ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)) |
| `Http/Controllers/Computer/ScreenController.php`, `PreviewController.php` | `POST …/ports/{port}/links` (`path`) | `POST /v0/sandboxes/{sbx}/ports/{port}/links` | |
| `Actions/Computers/CheckPort.php`, `Http/Controllers/Computer/ServiceController.php`, `Actions/Computers/PrepareCheckoutTask.php`, `Actions/Setup/RunSetupStep.php` | `POST …/commands` (`env`, `timeout`, `label`) | `POST /v0/sandboxes/{sbx}/commands` | Exit codes are right under gVisor once the fork's fix ships, so `RunSetupStep`'s printed sentinels can go |
| `Actions/Setup/RunSetupStep.php`, `StopComputerSetup.php` | `POST …/sessions` (`watch: true`), `GET …/sessions/{id}`, `…/log?tail=`, kill | `/v0/sandboxes/{sbx}/sessions…` | `watch` keeps working; `session.ended` arrives as a webhook |
| `Actions/Setup/RunSetupStep.php`, `Actions/Artifacts/SyncComputerArtifacts.php`, `Jobs/Artifacts/ImportArtifact.php` | `GET`/`PUT`/`PATCH`/`DELETE …/files`, `entries`, `directories` | `/v0/sandboxes/{sbx}/files…` | Downloads up to 1 GB are streamed, not read whole (an AerolVM fix in the fork) |
| `Http/Controllers/Computer/ActivityController.php` | `GET /activity` | `GET /v0/activity` | |
| `Actions/Activity/RecordLewagentEvent.php` | `POST …/activity` (`app.*`) | `POST /v0/sandboxes/{sbx}/activity` | Event kinds in `app/Enums/Activity/LewagentEvent.php` unchanged |
| `Actions/Computers/PrepareCheckoutTask.php` | `GET /conversations/{id}`, its messages | `GET /v0/conversations/{cnv}`, `…/messages` | |
| `Http/Controllers/Computer/Repository/RepositoryController.php`, `BranchController.php` | `GET /profile/repositories`, `…/{owner}/{repo}/branches` | `GET /v0/profile/git/repositories…` | |
| `Http/Controllers/Computer/AgentKeyController.php` | `POST …/keys` (`['sandboxes.agents', 'sandboxes.exec']`, ~15 min), retrying with no scopes on a validation error | `POST /v0/sandboxes/{sbx}/keys` | The scope bug is fixed in the new server, so the no-scope fallback (which handed browsers an all-permission key) is removed |

### Server calls: Admin API (tenant key → admin key)

| Lewagent file (`app/…`) | Legacy call | New API | Milestone |
|---|---|---|---|
| `Jobs/Users/ProvisionLewboxProfile.php` | `profiles()->findByExternalId()`, `create(name, externalId, defaultCluster)`, `limits(id)->replace([...])`, `apiKeys(id)->create('Lewagent', clusterAccess: 'all')` | `/v0/projects/{prj}/profiles` (`?external_id=`), `…/limits`, `…/keys` | Alpha |
| `Models/Plan.php` (`lewboxLimits()`) | Limit kinds `running_sandboxes`, `vcpus`, `memory_mb`, `sandbox_vcpus`, `sandbox_memory_mb`, `sandbox_disk_gb`, `storage_gb` | Same kinds ([limits](../limits.md)) | Alpha |
| `Console/Commands/ComputerTemplateCommand.php` (`lewagent:template push|build --wait`) | `POST`/`GET`/`PATCH /templates`, `POST …/builds`, `waitUntilBuilt` | `/v0/projects/{prj}/templates…` | Alpha |
| `Http/Controllers/Profile/SetupController.php` | `POST /profiles/{prf}/setup-links {returnUrl, sections}` | `POST /v0/projects/{prj}/profiles/{prf}/setup-links` | Alpha |
| `Http/Controllers/Profile/SetupReturnController.php` | `GET /profiles/{prf}/agents`, `…/git` | `GET …/profiles/{prf}/agents`, `…/git` | Alpha |
| `Actions/Computers/GrantGitRepositories.php`, `StartComputer.php` | `PUT /sandboxes/{id}/git/repositories` before every start | `PUT /v0/sandboxes/{sbx}/git` `{repositories}` ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)); or name them once, as `git.repositories` on create, and drop the call before every start. **Lewagent must create computers with `git: {access: "read"}` and set its project's `sandbox_access_max` to `read`**: Lewbox's default is now `full`, which would break its "nothing on the computer can push" promise | Alpha |
| `Http/Controllers/Computer/OwnerController.php` | `GET /profiles/{prf}/repository-owners` | In Lewagent: `GET /user/installations` with the person's user token from `POST …/profiles/{prf}/git/tokens {type: user}`, plus the connection's linked installations ([git/integrating](../git/integrating.md#creating-a-repository)) | Alpha |
| `Actions/Repositories/CreateGitHubRepository.php` | `POST /profiles/{prf}/repositories` | In Lewagent: `POST /user/repos` or `/orgs/{org}/repos` on GitHub with the user token; legacy's `CreateGitHubRepository.php` moves into Lewagent | Alpha |
| `Http/Controllers/Computer/PushController.php` | `GET`/`POST /sandboxes/{id}/pushes`, `GET …/{push}`, `POST …/{push}/confirmation`, `DELETE` | In Lewagent: legacy's `app/Actions/Pushes/*`, `PushGitHub.php`, `RunPush.php` and the `pushes` table move into Lewagent, using `…/commands`, `…/files` and an installation token with `contents: write` from `…/git/tokens` ([git/integrating](../git/integrating.md#lewagent)) | Alpha |
| `Http/Controllers/Computer/Repository/PullRequestController.php` | Everything under `/profiles/{prf}/repositories/{repo}/pulls…` (create, ready, edit, close, merge, files, viewed, threads, comments, replies, conversation, resolve, review) | In Lewagent: legacy's `GitHubPullRequests.php` moves into Lewagent and calls GitHub directly with the user token (installation token for `as: app`) ([git/integrating](../git/integrating.md#pull-requests-and-reviews)) | Alpha |

**Pushes, pull requests and repository creation move into Lewagent**, ported from legacy Lewbox (also PHP and Laravel), and call GitHub with tokens the new Lewbox issues. None of them waits for a Lewbox Beta, so they don't hold up the move. Lewagent's project keeps its sandbox Git cap read-only, so "nothing on the computer can push" still holds ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)). The work is in Lewagent: about 2,000 lines of PHP moved and pointed at the new API.

### Browser calls

`resources/js/lib/agents.ts` (`ComputerAgents`, 1282 lines) and `resources/js/lib/git.ts` (2330 lines) call the gateway with sandbox keys:

| Area | Legacy | New |
|---|---|---|
| Commands (the whole Git panel runs `git` and `gh` this way, with labels) | `POST /sandboxes/{sbx}/commands` | `@lewbox/sdk` `sandbox.commands.run` |
| Files (uploads, directory picker, editor) | `PUT`/`GET …/files?path=`, `entries`, `directories` | `sandbox.files.*` |
| Conversations | `GET /conversations?sandbox=&per_page=100`, `GET`/`DELETE /conversations/{id}` (`force`), `POST /sandboxes/{sbx}/conversations` (agent, prompt, directory, `worktree`, model, mode, effort, `output_schema`, idempotency key), `POST …/messages` (`steer`, `output_schema`), `PATCH …/messages/{turn}`, `GET …/messages?status=queued` and `?after=`, `POST …/replies`, `GET`/`PATCH …/tasks/{task}`, `GET /sandboxes/{sbx}/agents` | `sandbox.prompt`, `conversation.*`, `lewbox.conversations.list` |
| Turn events | SSE `…/messages/{turn}/events?after=N`; events `started`, `text`, `reasoning`, `tool_call`, `tool_output`, `plan`, `approval_request`, `question`, `answered`, `usage`, `finished`, `error`, `task`, `commands`, `title`, `mode`; control events `stream_interrupted`, `gateway_restarting` | `conversation.follow` ([sdks-and-cli/typescript](../sdks-and-cli/typescript.md#conversations)); `gateway_restarting` becomes `server_restarting`; `?after=` still accepted for cross-origin pages |
| Terminal | `GET`/`POST`/`DELETE …/sessions` (`pty`, `directory`, `cols`, `rows`), WebSocket `…/sessions/{id}/attach` with `['lewbox.bearer', token]` | `sandbox.sessions.*`, `attach` |
| Keys | `getKey(renew)` from Lewagent's own endpoint | `new Lewbox({ getKey })` |

Plan: replace `ComputerAgents`' transport, `followTurn` and `mergeEvent` with `@lewbox/sdk`, keeping Lewagent's own UI and its Lewagent-specific helpers (`openOnDesktop`, `stillOfDesktop`, `repository`, `switchBranch`, attachments). That removes several hundred lines and the second copy of the event model. The chat components can later move to `@lewbox/react` if we publish it ([dashboard/stack-and-structure](../dashboard/stack-and-structure.md#shared-components)).

### Webhooks

| Legacy event | Lewagent uses it for | New event |
|---|---|---|
| `sandbox.<status>` with `data.sandbox`, `previous_status`, `status_sequence`, `reason.code` (`idle_timeout`, `lifetime_reached`, `credits_exhausted`, `limit_exceeded`, `profile_suspended`) | Computer status, ordered by `status_sequence` | Same names and fields ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)) |
| `operation.failed` (`kind`, `error`) | Failing a create, start, stop or restart | Same |
| `activity.request.refused` | Surfacing refusals | Same |
| `activity.session.ended` (`details.session`) | Continuing setup and builds | Same |
| `activity.agent.turn_ended` (`details.message`, `details.conversation`) | Settling checkout tasks | Same |

The aim is that Lewagent's `HandleLewboxWebhook` needs no change. The endpoint is re-created on the new project by the migration ([data](data.md)), keeping its secret, so `LEWBOX_WEBHOOK_SECRET` stays.

### What Lewagent can drop

| Workaround | Why it can go |
|---|---|
| `SyncLewboxSdkCommand` and `packages/lewbox-php` | The SDK is on Packagist ([sdks-and-cli/php](../sdks-and-cli/php.md)) |
| The no-scope sandbox key fallback in `AgentKeyController` | Scopes work |
| `CheckPort` curling a port before making a desktop link | A link to a port that isn't answering yet waits for it, or the port reports `reachable` ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)) |
| Exit-code sentinels in `RunSetupStep` | Exit codes are right under gVisor (fork fix, [lessons](../lessons.md)) |
| Most of `RunSetupStep` and its watched sessions for create-time and every-start commands | The computer's setup commands are sent as `hooks.on_create` and `hooks.on_start` on create, so they run (and survive Lewagent being down) in Lewbox; the Build panel shows the sandbox's `hooks` report. Lewagent keeps running *manual* commands and adding services to a running computer through commands |
| Avoiding re-exposing a port with the same access | Fixed |
| `check-builds` every 5 s and `settle-checkout-tasks` every 15 s | Webhooks are reliable; keep a 5-minute backstop |
| Separate admin and consumer URLs | One `LEWBOX_API_URL` |
| Inlining template files as base64 placeholders | Still needed unless templates get a build context ([templates](../templates.md), open question) |

### What Lewagent is waiting for

From `docs/todo.md` § "Waiting on Lewbox" and `small-todo.md`, and where each lands:

| Need | Plan | When |
|---|---|---|
| Private ports checked live; Safari cookies in iframes | [inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md) | Alpha |
| Exit status lost under gVisor | [clusters/aerolvm-fork](../clusters/aerolvm-fork.md), [lessons](../lessons.md) | Alpha |
| Requests cut at ~20 s (Laravel Cloud edge) | Gone: `lewbox serve` has no 20 s edge ([foundation](../foundation.md)) | Alpha |
| Standalone worktrees API (`POST /v0/sandboxes/{sbx}/worktrees`, `worktree: {id}`, `worktree.*` webhooks) | [agents/worktrees](../agents/worktrees.md) | Later (or Beta) |
| Per-conversation `env`, skills, MCP servers, allowed tools | [agents/conversations](../agents/conversations.md) | Beta |
| Codex `~/.codex` sqlite race; unloading idle chats; slash commands kept; `on_unanswered: wait` | [agents/agentd](../agents/agentd.md) | Alpha |
| Conditional file writes (`If-Match`, `412`); file-change events | [inside-a-sandbox/files](../inside-a-sandbox/files.md) | Beta |
| Session WebSocket closing with 1011 instead of 1000 | [api/streaming](../api/streaming.md) | Alpha |
| `setup: [{name, command, timeout}]` steps on create | **`hooks.on_create`** (run once after setup and the clones, each with a label, directory and timeout, reported per hook) and `hooks.on_start` for its "run on every start" commands; `required` for a step that must succeed ([sandboxes/hooks](../sandboxes/hooks.md)) | Alpha |
| A computer cleaning up after itself before a stop (`small-todo.md`) | `hooks.on_stop` and `scrub`, run before every stop, idle and lifetime stops included ([sandboxes/hooks](../sandboxes/hooks.md)) | Alpha |
| Profile templates in the Consumer API | [templates](../templates.md) | Alpha |
| A turn's status and final text without the stream | [agents/turns-and-events](../agents/turns-and-events.md) | Alpha |
| Usage endpoints | [credits](../credits.md) | Beta |
| Subscription sign-in through the Admin API | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) | Alpha |
| Sub-agent output kept apart from the main reply | [agents/turns-and-events](../agents/turns-and-events.md) | Beta |
| GitHub App webhooks forwarded (`pull_request.closed`, `push`, `gollum`) | Every event the App receives is forwarded whole, as `github.<event>` ([activity-and-webhooks/github-webhooks](../activity-and-webhooks/github-webhooks.md#forwarding)) | Alpha |
| Comment as the App, edit that comment; a pull request by number on profile keys | Done by Lewagent on GitHub with an installation token ([git/integrating](../git/integrating.md#pull-requests-and-reviews)) | Alpha |
| Memory overprovisioning per cluster; limits per cluster | [clusters/placement-and-capacity](../clusters/placement-and-capacity.md), [limits](../limits.md) | Beta |
| Moving a persistent sandbox to another cluster | [sandboxes/persistence](../sandboxes/persistence.md) | Later |
| A `conversation_search` across runtimes | [agents/conversations](../agents/conversations.md) | Later |

### Order of the move

1. **New Lewbox** has everything in the tables above that Lewagent needs from Lewbox (the alpha's token API included); Lewagent has ported pushes, pull requests and repository creation.
2. **PHP SDK** on the new API, published ([sdks-and-cli/php](../sdks-and-cli/php.md)); `@lewbox/sdk` published.
3. **Lewagent branch `lewbox-v1`:**
   1. Require `lewbox/lewbox-php` from Packagist; delete `packages/lewbox-php` and the sync command.
   2. `config/lewbox.php`: one `LEWBOX_API_URL`, `LEWBOX_ADMIN_KEY` as an `lba_` key (an `lbt_` key works for 90 days after migration).
   3. Update the 46 files: mostly method renames and the admin resources' new nesting; types of returned objects keep their names.
   4. Replace `resources/js/lib/agents.ts`' transport, `followTurn` and `mergeEvent` with `@lewbox/sdk`.
   5. Drop the workarounds listed above.
   6. Port legacy Lewbox's pushes (`app/Actions/Pushes/*`, `PushGitHub.php`, `RunPush.php`, `CleanUpPushes.php`, the `pushes` table), pull requests (`GitHubPullRequests.php`) and repository creation (`CreateGitHubRepository.php`, `ListGitHubRepositoryOwners.php`) into Lewagent, on issued tokens ([git/integrating](../git/integrating.md#lewagent)). Subscribe a webhook endpoint to `github.pull_request`.
   7. Run Lewagent's tests against `lewbox serve --fake-cluster` ([Testing](#testing)).
4. **Staging:** Lewagent staging against a new Lewbox staging instance with a real cluster; a week of daily use by the team.
5. **Cutover:** Lewbox data migrated first; Lewagent's branch deployed in the same window, pointed at the new Lewbox ([cutover](cutover.md#lewagent)). Lewagent's own database needs no migration: sandbox, profile and key ids are kept.
6. **After:** remove the remaining polling; adopt worktrees and per-conversation env as they ship.

### Auth model, revisited

Lewagent uses two auth paths for one feature today: a profile key for commands and files, and the tenant key with a `profile_id` for pushes and pull requests. In the new API, GitHub tokens come from `…/git/tokens`, with the admin key (or a per-user profile key made with `git.tokens` and `git.tokens.user`). In the new API an admin key can reach any of its project's sandboxes, so Lewagent could use the admin key for everything server-side and keep per-user profile keys only for what should be attributed to the user (activity actors, limits). Whether to simplify that way is Lewagent's call ([open question](#open-questions)); the migration works either way, since both key kinds keep working.

## Testing

- **Fake server:** Lewagent's Pest suite runs in CI against `lewbox serve --fake-cluster` (started by a GitHub Actions service container), with the real PHP SDK, instead of mocked HTTP. Tests that mock the SDK stay for unit coverage.
- **Contract:** a list of every call in the tables above, as a test in Lewbox's own CI using the PHP SDK, so a change to the new API that would break Lewagent fails in Lewbox first.
- **Browser:** Lewagent's Playwright flows (new computer, chat with approvals, terminal, Git panel commit and push, screen, artifacts) against staging.
- **Webhooks:** replay recorded legacy webhook bodies into the new `HandleLewboxWebhook` code and the new server's payloads into the old code, both ways, to prove the shapes match.

## Build plan

1. Contract test listing every Lewagent call, in Lewbox's CI. (Beta)
2. PHP SDK and `@lewbox/sdk` releases with what Lewagent needs. (Beta)
3. Lewagent `lewbox-v1` branch, steps 3.1 to 3.7, including the Git port. (Beta)
4. Staging week. (Beta)
5. Cutover with Lewbox. (Beta)
6. Clean-up of polling and workarounds. (Beta)

## Open questions

1. **One key or two** for Lewagent's backend? Admin key for everything, or keep per-user profile keys? (Affects activity attribution and per-user limits.)
2. **Profile per user or per workspace?** The config says workspace, the code does per user. The migration keeps per user; changing it is a Lewagent decision that's easier before cutover than after.
3. **Bundle or keep `receivePack`?** Lewagent can move legacy's push code as it is (a PHP `git-receive-pack` client) or switch to the bundle recipe, which needs `git` on its servers ([git/integrating](../git/integrating.md#pushing-from-your-servers)). Proposed: move it as it is, switch later if it's a burden.
4. **Template build context:** do we add one (a tarball upload) so Lewagent can stop inlining files? ([templates](../templates.md))
