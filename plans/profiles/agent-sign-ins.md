# Agent sign-ins

> Status: Draft · Milestone: Alpha (M3) · Core machinery; flows from the agent provider plugins (`agents/claude-code`, `agents/codex`) · Updated: 2026-10-29

A Claude or ChatGPT **subscription** can't be pasted in like an API key: its owner has to approve a sign-in on claude.ai or chatgpt.com. An **agent sign-in** runs the agent's own CLI login (`claude setup-token`, `codex login --device-auth`) in a short-lived **helper sandbox**, shows the person the link (and code) the CLI prints, takes the code back where Claude needs it, and stores the login the CLI produces as the profile's [credential](agent-credentials.md). Lewbox then keeps ChatGPT logins fresh, since their refresh tokens work only once. Sign-ins are in the alpha ([D7](../README.md#decisions)).

## Today

Built in legacy (`lewbox-legacy/docs/profiles.md` § *Subscription sign-in*; `app/Actions/SignIns/*`, `app/Sandboxes/SignIns/*`, `app/Models/AgentSignIn.php`; setup page card `components/profile/AgentList.svelte`):

- **Helper sandbox** (`OpenSignInHelper`): a sandbox of the signing-in profile with `purpose = sign_in`, from the global `agents` template's ready build on the tenant's **sign-in cluster** (`tenants.sign_in_cluster_id`), sized by `lewbox.sign_ins.helper` (1 vCPU, 2 GB, 10 GB), not persistent, `lifetime` 20 minutes (so AerolVM destroys it even if Lewbox forgets), invisible to both APIs, unbilled, not counted in limits, without the profile's setup, used once and deleted.
- **Concurrency**: at most `tenants.max_sign_in_sandboxes` (5) helpers per tenant; more wait `queued` (oldest first, `AdvanceSignInQueue` under a tenant lock), expiring after 10 minutes; a new sign-in for an agent cancels the one under way.
- **Driving** (`AdvanceAgentSignIn`, run by `sign-ins:advance` every 5 s via a `WatchAgentSignIn` job per sign-in): the app connects to the helper **through the gateway's public API with a sandbox key it mints for itself** (`ConnectToSandbox`), starts the CLI in a session, reads the session log for the link, types a pasted code through a command, checks status, reads the login file.
  - **Claude** (`ClaudeSignIn`): `rm -f /tmp/lewbox-claude-sign-in && mkfifo … && exec 3<>… && TERM=xterm-256color script -qfec 'stty cols 1000 rows 50; claude setup-token' /dev/null <&3`; the link matches `https://claude.(com|ai)/…oauth/authorize?…`; the code is written into the pipe, then `\r` a second later (Claude treats input arriving at once as a paste); the token matches `sk-ant-oat01-[A-Za-z0-9_-]+`; "OAuth error" fails it.
  - **ChatGPT** (`ChatGptSignIn`): write `cli_auth_credentials_store = "file"` to `/root/.codex/config.toml`, run `codex login --device-auth`; the link matches `https://auth.openai.com/codex/device…` and the code `[A-Z0-9]{4}-[A-Z0-9]{4,}`; poll `codex login status | grep -q ChatGPT`; then read `/root/.codex/auth.json`, email from its ID token. Device-code sign-in must be enabled in the ChatGPT account's security settings, or no code is printed.
- **Statuses**: `queued → starting → awaiting_code | awaiting_approval → submitting_code → completed | failed | expired | cancelled`. Expires 15 minutes after starting; fails if still `starting` 180 s after the helper was asked for, saying where it stalled.
- **ChatGPT refresh** (`RefreshSubscriptionLogins` every 5 minutes, `RefreshChatGptLogin`, `PushChatGptLogin`, `CodexAuthFile`): refresh after 3 days or within a day of the access token's expiry, well before Codex's own 8 days; read every running sandbox's copy first and keep any newer one (a sandbox refreshed it itself), else `POST https://auth.openai.com/oauth/token` with `grant_type=refresh_token`; write the new `auth.json` into each sandbox as a file write (not `apply_setup`); on 400/401 record the login as ended.
- **Only on a page**: the setup page or the dashboard's Setup button; not in the Admin API. Lewagent has asked for it in the Admin API (`docs/todo.md`, *Waiting on Lewbox*).
- **Legal**: `docs/acp.md` notes Anthropic's terms on storing subscription tokens are an open question to ask Anthropic about before building on it.

What's wrong:

- The app calls itself through the public API (sandbox keys that show up in the helper's activity log, HTTP hops, a PHP SDK in the loop).
- A 5-second sweep plus a job per sign-in plus cache locks, with lock-lapse bugs (a day-long lock once stalled every sign-in in production).
- Polling the session log over HTTP for a link.
- No API for projects that want their own UI.

## Goals

- Keep the flows and their hard-won details (the pipe and delayed Enter for Claude, `script` for a terminal, file-based Codex auth, single-refresher rule for ChatGPT).
- Drive helpers directly through the toolbox client, in one job per sign-in that waits on events instead of a sweep.
- Stream the CLI's output instead of polling the log.
- Offer sign-ins in the management API, so a project can show the link in its own product.
- Make it obvious to the person what to do at each step, and what went wrong.

### Non-goals

- Signing in subscriptions without the person (it's their account; they approve).
- Sharing one subscription across profiles.

## Design

### Where it runs

- The project's **sign-in cluster** ([projects/settings](../projects/settings.md#sign-in-settings)), with the provided `agents` template ready on it ([templates/provided-templates](../templates/provided-templates.md)): Ubuntu with `claude`, `codex`, `script`, `curl`, kept current by rebuilding on a schedule (weekly) and whenever its pinned CLI versions change.
- Without a sign-in cluster: the project's only cluster, if it has one; otherwise `409 sign_ins_unavailable` with `reason: no_cluster`, saying a sign-in cluster needs choosing. If `agents` isn't ready there: `409 sign_ins_unavailable` with `reason: template_building | template_failed`, and the settings page shows the build.
- **Helpers** are sandboxes with `purpose = sign_in`: created and deleted by ordinary operations (so placement, capacity and failures work as for any sandbox), never listed by the consumer API or counted in limits or credits, listed in the dashboard and management API with a *Sign-in* badge and filterable by purpose. No profile setup. `persist: false`, `lifetime` 20 minutes as a backstop. One per sign-in, deleted when it ends.

### Data model

```sql
create table agent_sign_ins (
  id                bigint generated always as identity primary key,
  public_id         text not null unique,       -- asi_…
  project_id        bigint not null references projects(id) on delete cascade,
  profile_id        bigint not null references profiles(id) on delete cascade,
  agent             text not null,              -- claude-code | codex
  provider          text not null,              -- anthropic_subscription | chatgpt_subscription
  status            text not null,              -- see below
  stage             text,                       -- finer progress for the UI: helper_creating, cli_starting, …
  sandbox_id        bigint references sandboxes(id) on delete set null,   -- the helper
  session_id        text,                       -- toolbox session running the CLI
  verification_url  text,
  user_code         text,                       -- ChatGPT's code to type; null for Claude
  error             text,
  error_code        text,                       -- helper_failed, cli_no_link, code_refused, cli_exited, timed_out, …
  return_url        text,                       -- for API-started sign-ins with a hosted page
  created_by        jsonb not null,             -- { type, id, name }
  queued_at         timestamptz not null default now(),
  started_at        timestamptz,
  expires_at        timestamptz not null,
  finished_at       timestamptz
);
create index on agent_sign_ins (project_id, status) where finished_at is null;
create unique index on agent_sign_ins (profile_id, agent) where finished_at is null;  -- one open per agent
```

### States

```
           ┌──────────── cancelled (person, or a newer sign-in for the agent)
           │
 queued ──▶ starting ──▶ awaiting_code ──▶ submitting_code ──▶ completed
   │          │      └─▶ awaiting_approval ─────────────────▶ completed
   │          │                 │                 │
   └──────────┴─────────────────┴─────────────────┴──▶ failed | expired
```

| Status | Means | Shown to the person |
|---|---|---|
| `queued` | The project has `max_concurrent` helpers already | "Waiting for a free slot: 2 ahead of you" |
| `starting` | Helper being created, or CLI starting (`stage` says which) | "Starting a secure sandbox…" / "Starting Claude Code…" |
| `awaiting_code` | Claude: the person opens the link, approves, and pastes back the code Claude shows | The link (opens in a new tab) and a box for the code |
| `submitting_code` | The code is being typed into the CLI | "Signing in…" |
| `awaiting_approval` | ChatGPT: the person opens the link and types the code shown | The link, the code with a copy button, and a reminder about device-code sign-in in ChatGPT's security settings |
| `completed` | The login is stored as the profile's credential | "Signed in as sam@acme.dev" |
| `failed` | `error` says why, with the CLI's last lines where useful | The message, and *Try again* |
| `expired` | Out of time (15 min from start; 10 min queued) | "Took too long", *Try again* |
| `cancelled` | Cancelled | |

### The job

One job per sign-in, `signin.run` ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)), unique per sign-in, which owns it from start to finish and holds a lease while it runs. It doesn't poll on a timer; it waits on things:

1. **Admit.** In a transaction with the project's sign-in settings row locked: if fewer than `max_concurrent` sign-ins of the project are past `queued` and unfinished, move to `starting`; else stay `queued` and end the job. When any sign-in of the project finishes, it enqueues `signin.run` for the oldest queued one (in the finishing transaction), so the queue moves at once, not on a sweep.
2. **Helper.** Reserve a helper sandbox (`purpose = sign_in`, the `agents` template, the sign-in cluster) and its `create` operation, and wait for the operation (the operation runner notifies on finish via `LISTEN/NOTIFY`). Capacity or create failure → `failed`, `helper_failed`, with the operation's error.
3. **CLI.** Prepare (ChatGPT: write Codex's config to keep the login in a file), start the CLI as a toolbox session in a PTY in `/root`, and **stream** its output through the toolbox attach (not log polls). Match the link (and code) as output arrives; on a match, set `awaiting_code` or `awaiting_approval` and `NOTIFY` the page. If the CLI exits, or 60 s pass without a link, → `failed`, `cli_no_link`, with the last 20 lines (cleaned of ANSI) and the flow's hint ("Check device code sign-in is on in your ChatGPT account's security settings").
4. **Wait for the person.**
   - Claude: wait for a code (the code endpoint `NOTIFY`s the job). Write it into the pipe, sleep 1 s, write `\r`; `submitting_code`. Keep reading output: `sk-ant-oat01-…` → step 5; "OAuth error" → `failed`, `code_refused` (the CLI exits after a refused code, so it can't go back to `awaiting_code`), "Claude didn't accept that code. Start again and paste the newest code Claude shows."
   - ChatGPT: every 3 s, run `codex login status`; when it reports ChatGPT, read `/root/.codex/auth.json`, parse it (`CodexAuthFile`), take the email from the ID token → step 5.
   - The CLI exiting without a login → `failed`, `cli_exited`. Reaching `expires_at` → `expired`.
5. **Store.** Save the credential through the same code path as a pasted one ([agent-credentials](agent-credentials.md#setting-a-credential)), which re-applies setup to the profile's running sandboxes, in a transaction that also sets `completed`.
6. **Clean up.** Always, on every exit path: kill the session, enqueue the helper's `delete` operation, and admit the next queued sign-in.

If a replica dies mid-job, the lease lapses and another picks it up; every step is idempotent (an existing helper is reused, an existing session re-attached, a stored credential not stored twice). The helper's 20-minute lifetime guarantees cleanup even if Lewbox loses it.

Following progress: the page subscribes to `GET …/sign-ins/{asi}/events` (SSE), which emits the sign-in object on each change (fed by `NOTIFY`), with polling of `GET …/sign-ins/{asi}` as the fallback.

### From the dashboard and setup page

The *Sign in with Claude* / *Sign in with ChatGPT* buttons on each agent's card ([setup-links](setup-links.md)) start one, follow it, and show each state as in the table. Starting a new one for an agent cancels the open one.

### From the management API

New, for projects building their own UI (Lewagent):

```
POST /v0/projects/{prj}/profiles/{prf}/agents/{agent}/sign-ins     { provider? }   → 201 sign-in
GET  …/sign-ins/{asi}                                                               → sign-in
GET  …/sign-ins/{asi}/events                                                        → SSE of the sign-in
PUT  …/sign-ins/{asi}/code   { code }                                               → 202
DELETE …/sign-ins/{asi}                                                             → cancel
```

The project shows `verification_url` (and `user_code`) in its own product and posts back Claude's code. Webhooks `sign_in.awaiting_person`, `sign_in.completed`, `sign_in.failed` let its backend react without polling.

The person must still approve on the provider's site; the API only spares the project from sending them to Lewbox's page.

### Refreshing ChatGPT logins

A ChatGPT login's refresh token works **once**: if two copies refresh, the second signs every copy out. So Lewbox must be the only refresher, and must hand the new tokens to every copy:

- A periodic job (`signin.refresh_chatgpt`, every 5 minutes, leader only) finds `chatgpt_subscription` credentials due: last refreshed over 3 days ago, or the access token expiring within a day (from the JWT's `exp`). Codex refreshes on its own after 8 days, so Lewbox always gets there first.
- For each, under a row lock on the credential:
  1. Read `auth.json` from each running sandbox whose last setup wrote it (directly through the toolbox; no sandbox keys). If any copy has a different refresh token and a later `last_refresh`, a sandbox refreshed it itself: adopt it as the stored login.
  2. If the stored login is still due, `POST https://auth.openai.com/oauth/token` (`grant_type=refresh_token`, Codex's public `client_id`, the refresh token), merge the new tokens, set `last_refresh`.
  3. Store it, then write it to every running sandbox that has a copy, as one file write each (not an `apply_setup`, so busy sandboxes get it too), in parallel with a 10 s timeout each; failures retried next pass.
  4. On 400/401 from OpenAI: mark the credential ended (`last_error`, "ChatGPT signed this login out. Sign in again."), raise a notification and the `profile.credential_failing` webhook. The setup page shows Sign in again.
- **Harvest before every scrub.** Every stop reads the sandbox's `auth.json` back before anything is removed, and adopts it by the same rule as step 1: through the toolbox for a stop that's asked for, and from the node by the snapshot's own scrub for a sandbox AerolVM stopped on a timer ([sandboxes/profile-setup](../sandboxes/profile-setup.md#harvest-first)). Adopting it writes it to the profile's other running sandboxes, as step 3 does. So a sandbox that refreshed its own copy never takes the only live login with it when it stops, which legacy accepted as a known window.
- An unreadable copy (a broken file; an older fork release on a timer stop) keeps the stored login and is logged; the next pass of this job finds out whether the stored one still works.
- Claude's `setup-token` lasts a year and needs no refresh; Lewbox records its creation date and notifies 14 days before a year is up.

### Legal

Legacy flagged that storing a person's Claude subscription token, and using it in sandboxes, may be restricted by Anthropic's terms (`docs/acp.md`), and the same goes for ChatGPT logins.

**Settled for now (2026-10-29): sign-ins ship in the alpha, as designed.** Lewbox is self-hosted: the logins are stored in the instance's own database and written into sandboxes on the same people's own machines. The person runs the sign-in themselves, and the tokens never leave infrastructure they or their project run. That's the case the alpha is for.

- **Revisit for the hosted instance** (lewbox.com), where Harlew would hold other people's subscription tokens. Confirm with Anthropic and OpenAI before sign-ins are offered there (open question 1).
- **The switch stays:** an instance can turn either provider off (`LEWBOX_SIGN_IN_PROVIDERS`), refusing new sign-ins with `409 sign_ins_unavailable` and `reason: provider_disabled`; API keys always remain. So if an answer changes, nothing needs building.

## API

As above, plus `GET /v0/projects/{prj}/profiles/{prf}/sign-ins` (open and recent sign-ins). Who: owner, admin, member, setup session with `agents`, admin key with `profiles.credentials`.

The sign-in object:

```json
{
  "id": "asi_7Gm2kQ…", "agent": "codex", "provider": "chatgpt_subscription",
  "status": "awaiting_approval", "stage": null,
  "verification_url": "https://auth.openai.com/codex/device", "user_code": "ABCD-EFGH",
  "queue_position": null, "error": null, "error_code": null,
  "created_by": { "type": "setup_link", "id": "psl_…" },
  "started_at": "…", "expires_at": "…", "finished_at": null
}
```

### Errors

| Status | Code | When |
|---|---|---|
| `409` | `sign_ins_unavailable` | No sign-in cluster, `agents` not ready there, or the instance turned the provider off; carries `reason`: `no_cluster`, `template_building`, `template_failed` or `provider_disabled` ([Legal](#legal)) |
| `409` | `sign_in_not_awaiting_code` | A code for a sign-in not in `awaiting_code` |
| `409` | `sign_in_finished` | Cancelling or coding a finished one |
| `422` | `validation_failed` | A provider that isn't a subscription for the agent; an empty code |

## Security

- The helper is a fresh, single-use sandbox: no profile setup, no other logins, deleted afterwards; its lifetime is a backstop.
- The code the person pastes reaches the CLI without ever being on a command line: written into the pipe through the toolbox's file API (legacy passed it in a command's environment, which also kept it out of `ps`).
- The login is stored encrypted and never shown; the helper's activity is logged with actor `lewbox` and no output.
- Only Lewbox refreshes ChatGPT logins; sandboxes are given logins that Codex won't try to refresh for days.

## Observability

- Audit: `sign_in.started`, `sign_in.completed`, `sign_in.failed`, `sign_in.cancelled`.
- Webhooks: `sign_in.awaiting_person`, `sign_in.completed`, `sign_in.failed`, `profile.credential_failing`.
- Metrics: `lewbox_sign_ins_total{provider,result,error_code}`, `lewbox_sign_in_duration_seconds{provider,stage}`, `lewbox_sign_ins_queued`, `lewbox_chatgpt_refreshes_total{result}`.
- Logs: each stage transition with the sign-in id; CLI output only at debug level, redacted.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SIGN_IN_HELPER_CPU` / `_MEMORY_MB` / `_DISK_GB` | `1` / `2048` / `10` | Helper size |
| `LEWBOX_SIGN_IN_HELPER_LIFETIME` | `20m` | Backstop |
| `LEWBOX_SIGN_IN_TTL` | `15m` | From start to expiry |
| `LEWBOX_SIGN_IN_QUEUE_TTL` | `10m` | Queued expiry |
| `LEWBOX_SIGN_IN_LINK_WAIT` | `60s` | CLI must print a link within |
| `LEWBOX_SIGN_IN_CHATGPT_REFRESH_AFTER` | `72h` | |
| `LEWBOX_SIGN_IN_PROVIDERS` | `anthropic_subscription,chatgpt_subscription` | Instance-wide enable list |

## Testing

- Flows against the fake toolbox with a scripted CLI: Claude link, code, token; Claude refused code; ChatGPT link and code, status flip, `auth.json` read; CLI exiting early; no link in time.
- Queue: `max_concurrent` respected under parallel starts; next admitted on finish; queued expiry.
- Replica failure mid-job: lease lapses, another resumes without a second helper.
- ChatGPT refresh: due detection; adopting a newer sandbox copy; refresh call against a fake OpenAI; distribution to sandboxes; 401 handling.
- One live test per provider before the alpha, on a real sign-in cluster (manual, documented in the release checklist).

## Build plan

Core: `internal/signins` (state machine, the `Flow` interface, the job, refresh scheduling), using `internal/sandboxes` for helpers and `internal/aerolvm` for sessions. Each **flow is contributed by its agent's plugin** ([plugins/agent-providers](../plugins/agent-providers.md)): `plugins/agents/claude-code` the Claude setup-token flow, `plugins/agents/codex` the ChatGPT device-auth flow and its refresher. A new agent with a subscription sign-in implements `Flow` in its plugin; nothing in core changes. The CLIs the helper needs come from the plugin's contribution to the `agents` template ([templates/provided-templates](../templates/provided-templates.md)).

1. Table, endpoints (management and setup session), SSE.
2. The `Flow` interface (`Prepare`, `Command`, `MatchPrompt`, `AwaitsCode`, `SubmitCode`, `Credential`, `NoPromptHint`) in core, and the Claude and ChatGPT flows in their plugins, ported with their regexes.
3. The `signin.run` job: admission, helper, streaming CLI, waits, store, cleanup.
4. ChatGPT refresh job; Claude expiry reminder.
5. Setup-page card in React; webhooks.
6. Live checks on a real cluster.

## Open questions

1. **Hosted instance:** confirm with Anthropic and OpenAI before lewbox.com offers subscription sign-ins. Self-hosted instances ship with them in the alpha ([Legal](#legal)).
2. **A shared "agents" helper per project** instead of one sandbox per sign-in? Faster (seconds instead of a create), but a helper would hold several customers' logins in turn. Proposed: no, keep one per sign-in.
3. **Version drift.** If `claude` or `codex` change their sign-in output, every sign-in breaks. Pin CLI versions in `agents` and run a nightly canary sign-in up to the link?
4. **Other subscriptions** (SuperGrok, Cursor) after the alpha?
