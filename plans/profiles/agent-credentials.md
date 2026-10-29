# Agent credentials

> Status: Draft · Milestone: Alpha (M3) · Plugins: `agents/claude-code`, `agents/codex`, `agents/opencode` (planned), on the core credentials store · Updated: 2026-10-29

Coding agents in a profile's sandboxes need to be signed in: Claude Code with a Claude subscription or an Anthropic API key, Codex with a ChatGPT subscription or an OpenAI API key. **Agent credentials** are those logins, held on the profile, encrypted, never shown again, and written into every sandbox of the profile by [profile setup](../sandboxes/profile-setup.md) so that both Lewbox's agent runner ([agents/agentd](../agents/agentd.md)) and anything the customer runs in a terminal find `claude` and `codex` already signed in. Usage is billed by the provider to that login, as if the customer ran the agent themselves.

## Today

Built in legacy (`lewbox-legacy/docs/profiles.md` § *Agent authentication*):

- `profile_credentials` (`ProfileCredential`): `profile_id`, `agent` (`claude-code`, `codex`, or null for Git), `provider` (`anthropic_subscription`, `chatgpt_subscription`, `anthropic`, `openai`, `github`, `github_app`), `secret` (`encrypted:array`: `key`, `token`, or `auth_json`), `metadata` (account email, last four, refresh errors), `verified_at`. Unique on `(profile_id, agent, provider)`.
- Saved through `SaveProfileCredential` (`app/Actions/Profiles`), from the setup page (`SetupLink\Agent\CredentialController`) or the Admin API (`PUT /api/v1/profiles/{prf}/agents/{agent}/credential`, `GET /profiles/{prf}/agents`).
- **Checked before stored** (`VerifyCredential`): Anthropic keys with `GET https://api.anthropic.com/v1/models` (`x-api-key`, `anthropic-version: 2023-06-01`), OpenAI keys with `GET https://api.openai.com/v1/models`. Pasted Claude subscription tokens aren't checked.
- Written into sandboxes (`app/Sandboxes/Setup/ClaudeCodeSetup.php`, `CodexSetup.php`):
  - Claude Code: `/root/.claude.json` merged (onboarding done, `/root` and `/root/workspace` trusted, the API key's last 20 characters approved); `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` in the profile env file `/root/.config/lewbox/env` (`0600`, loaded by `/etc/profile.d/lewbox.sh`). Check: `claude auth status`.
  - Codex: `/root/.codex/config.toml` only when missing (`cli_auth_credentials_store = "file"`, trusted directories); an API key logged in with `printenv OPENAI_API_KEY | codex login --with-api-key`; a ChatGPT login as `/root/.codex/auth.json` (secret). Cleanup removes `auth.json`. Check: `codex login status`.
- An agent uses one method at a time. Values are write-only. A profile key can't read or change them: the consumer sees only each agent's state in a sandbox's `setup` report.
- Planned and listed in legacy: Cursor, Grok, OpenCode, pi, Antigravity, and more providers (OpenRouter, DeepSeek, Amazon Bedrock, xAI, Gemini).

## Goals

- Keep the model and the in-sandbox layout; it works and the agents depend on it.
- One registry of agents and providers in Go, from which the API's validation, the setup steps, the setup page's forms and the docs all come.
- Verify every credential we can before storing it; record when it last worked from inside a sandbox.
- Re-apply to running sandboxes on every change, and remove from them on removal.

### Non-goals

- Storing anything for agents Lewbox doesn't run. The alpha supports Claude Code and Codex; others are listed as Later.
- Proxying the agents' API traffic through Lewbox (a Later idea that would keep keys out of sandboxes; see [Open questions](#open-questions)).

## Design

### Agents and providers

| Agent (`agent`) | Provider (`provider`) | Kind | Secret | Verified with | Alpha |
|---|---|---|---|---|---|
| `claude-code` | `anthropic_subscription` | subscription | `token` (`sk-ant-oat01-…`, from `claude setup-token`) | A sign-in produces it ([agent-sign-ins](agent-sign-ins.md)); a pasted one: `claude auth status` in the next sandbox | ✓ |
| `claude-code` | `anthropic` | API key | `key` | `GET api.anthropic.com/v1/models` | ✓ |
| `claude-code` | `bedrock` | cloud | `access_key_id`, `secret_access_key`, `region`, `model` | `bedrock:ListFoundationModels` | Later |
| `claude-code` | `openrouter` | API key | `key` | `GET openrouter.ai/api/v1/key` | Later |
| `codex` | `chatgpt_subscription` | subscription | `auth_json` (Codex's whole `auth.json`) | A sign-in produces it; refreshed by Lewbox ([agent-sign-ins](agent-sign-ins.md#refreshing-chatgpt-logins)) | ✓ |
| `codex` | `openai` | API key | `key` | `GET api.openai.com/v1/models` | ✓ |
| `codex` | `openrouter`, `azure_openai` | API key | | | Later |

The registry is the set of **agent provider plugins** ([plugins/agent-providers](../plugins/agent-providers.md)): each of `plugins/agents/claude-code`, `plugins/agents/codex` (and later `plugins/agents/opencode`) registers its agent id, display name, credential methods (the rows above: provider, kind, fields with labels, help text and a "create a key" URL), its verify functions, and the files and env its credentials become in a sandbox. Core keeps the registry (`internal/agents/registry`), the encrypted store and the endpoints; adding an agent never touches them. The registry is served to the dashboard and setup page as `GET /v0/agents/providers` so forms are built from it rather than hard-coded.

### Data model

```sql
create table profile_credentials (
  id           bigint generated always as identity primary key,
  public_id    text not null unique,                -- crd_…
  profile_id   bigint not null references profiles(id) on delete cascade,
  agent        text not null,                       -- claude-code | codex
  provider     text not null,
  secret       bytea not null,                      -- encrypted JSON, keyring ([foundation/secrets-and-encryption])
  hint         text,                                -- last four of a key; never the secret
  account      text,                                -- subscription's account email, when known
  metadata     jsonb not null default '{}',         -- e.g. refresh error, plan type
  verified_at  timestamptz,                         -- last verified by Lewbox
  last_ready_at timestamptz,                        -- last time a sandbox's check passed
  last_error   text,                                -- last failure (verification, refresh, or sandbox check)
  created_by   jsonb not null,                      -- { type, id, name } of who set it
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  unique (profile_id, agent)                        -- one method per agent
);
```

GitHub connections move to their own tables ([git/github-app](../git/github-app.md), [git/tokens](../git/tokens.md)), and pasted GitHub tokens are dropped, rather than sharing this one with `agent` null, as legacy did; agents and Git have different lifecycles and fields.

**One method per agent** (`unique (profile_id, agent)`): setting a different provider for an agent replaces the old credential in the same transaction. Legacy's multi-method agents (OpenCode, pi) can lift this later with a `priority` column.

### Setting a credential

`PUT /v0/projects/{prj}/profiles/{prf}/agents/{agent}/credential { provider, ...fields }`:

1. Validate fields from the registry (e.g. an Anthropic key starts `sk-ant-api`, a subscription token `sk-ant-oat01-`).
2. **Verify** with the provider when the registry can (an HTTP call with a 10 s timeout from the control plane, to the provider's fixed host only, never a URL from the request). A refusal is `422 credential_invalid` with the provider's message (`provider_message`) ("invalid x-api-key"); a provider that's down is `502 provider_unavailable`, and `?verify=false` stores it unverified (owner/admin only).
3. Encrypt and store; set `hint`, `account`, `verified_at`.
4. In the same transaction, enqueue `apply_setup` for each of the profile's running sandboxes ([sandboxes/profile-setup](../sandboxes/profile-setup.md)), and write the audit event and `profile.setup_changed` webhook.

Subscriptions can be **pasted** (a Claude `setup-token` someone ran themselves, or a Codex `auth.json`) or obtained through a [sign-in](agent-sign-ins.md); either way they're stored the same.

`DELETE …/agents/{agent}/credential` removes it and re-applies setup, whose cleanup removes what the last setup wrote (the env entry, Codex's `auth.json`).

### What reaches the sandbox

[Profile setup](../sandboxes/profile-setup.md) owns the manifest format, ordering, cleanup and reports. Each agent contributes one step; their content (kept from legacy):

| Step | Requires | Writes | Env file entries | Commands | Check | Cleanup |
|---|---|---|---|---|---|---|
| `claude-code` | `claude` | `/root/.claude.json` merged: `hasCompletedOnboarding`, trusted `/root`, `/root/workspace`, `customApiKeyResponses.approved` += last 20 chars of a key | `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`, in `/run/lewbox/env` | | `claude auth status` | env entries (file rewritten), the approved key suffix |
| `codex` | `codex` | `/root/.codex/config.toml` if missing (`cli_auth_credentials_store = "file"`, trusted dirs); `/run/lewbox/codex/auth.json` (`0600`), for ChatGPT *and* for an API key (written directly in Codex's own shape, not by `codex login`, which would put the key on disk), with `/root/.codex/auth.json` a symlink to it | | | `codex login status` | Remove both, after reading `auth.json` back ([harvest](../sandboxes/profile-setup.md#harvest-first)) |

Rules kept from legacy, worth restating because they're why it works:

- **Files and a login-shell env file, not create-time env.** CLIs read their logins from their own files and need state beside them (onboarding, trust). Env goes in `/run/lewbox/env`, loaded by every login shell, so commands, sessions, terminals, SSH and agentd's adapters all get it ([lessons](../lessons.md): agentd must start in a login shell or Claude says "Authentication required").
- **Only for tools the sandbox has.** A step whose command isn't found is `not_applicable`.
- **Never in a snapshot.** Secrets are written to `/run/lewbox`, the sandbox's memory-only mount; they're cleaned up before every stop that's asked for, and scrubbed from every snapshot on the node whatever stopped the sandbox, after reading back a login the agent may have refreshed ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)). Setup writes them again on every start. Legacy left them in snapshots after AerolVM's timer stops; that's no longer accepted.
- **A failed check doesn't fail the sandbox**; it's reported (`failed`, with redacted output).
- **Re-applied on change** as an `apply_setup` operation; a busy sandbox gets it after its current operation.

New: when a sandbox's `claude-code` or `codex` check passes, Lewbox sets the credential's `last_ready_at`; when it fails with an auth error, `last_error`, and after three sandboxes in a row fail, a project notification and the `profile.credential_failing` webhook. So an expired subscription is noticed without the customer reporting it.

### Reading state

`GET /v0/projects/{prj}/profiles/{prf}/agents`:

```json
{ "data": [
  { "agent": "claude-code", "name": "Claude Code",
    "credential": { "id": "crd_…", "provider": "anthropic_subscription", "kind": "subscription",
                    "account": "sam@acme.dev", "hint": null, "verified_at": "…", "last_ready_at": "…",
                    "last_error": null, "created_by": { "type": "setup_link", "id": "psl_…" }, "updated_at": "…" },
    "sign_in": null },
  { "agent": "codex", "name": "Codex", "credential": null,
    "sign_in": { "id": "asi_…", "status": "awaiting_approval", "verification_url": "https://auth.openai.com/codex/device", "user_code": "ABCD-EFGH" } }
] }
```

The consumer's `GET /v0/profile` shows `agents: [{ agent, configured, method }]` only.

## API

Under `/v0/projects/{prj}/profiles/{prf}`; owner, admin or member session, setup session with `agents`, or admin key with `profiles.credentials`:

| Method | Path | What |
|---|---|---|
| `GET` | `/agents` | Each agent with its credential summary and any sign-in under way |
| `PUT` | `/agents/{agent}/credential` | Set or replace; verified first |
| `DELETE` | `/agents/{agent}/credential` | Remove |
| | `/agents/{agent}/sign-ins…` | [agent-sign-ins](agent-sign-ins.md) |
| `GET` | `/v0/agents/providers` | The registry: agents, providers, fields (any signed-in principal) |

### Errors

| Status | Code | When |
|---|---|---|
| `404` | `not_found` | Unknown agent |
| `422` | `validation_failed` | Unknown provider for the agent; missing or malformed fields |
| `422` | `credential_invalid` | The provider refused it; its message is in `provider_message` |
| `502` | `provider_unavailable` | The provider couldn't be reached to verify |

## Security

- Encrypted at rest with the keyring; decrypted only when building a sandbox's setup manifest or refreshing a login.
- Never returned; `hint` and `account` only. Never logged; the setup runner redacts every secret value from command output and reports ([sandboxes/profile-setup](../sandboxes/profile-setup.md)).
- **Readable inside the sandbox, by design.** Anything the agent runs (a hijacked dependency, a malicious page it opens) can read the env file and `auth.json`. The setup page says so plainly and recommends an API key with its own spend limit at the provider, or a dedicated subscription, for untrusted workloads.
- Verification calls go to fixed provider hosts only.
- The project's members can set credentials but can't read them back; nor can instance admins through the API (they have the database and the keyring, which is the trust boundary).

## Observability

- Audit: `profile.credential_set`, `profile.credential_removed` (agent, provider, by whom).
- Webhooks: `profile.setup_changed`, `profile.credential_failing`.
- Metrics: `lewbox_credential_verifications_total{provider,result}`.

## Testing

- Registry-driven validation for each provider; verification against fake provider servers (accept, refuse, timeout).
- Replacing one method with another; removal triggers cleanup in running sandboxes (fake toolbox).
- Secrets never appear in any API response, log line or setup report (a test greps captured output for the secret).
- `last_ready_at` / `last_error` updated from sandbox setup reports.

## Build plan

Core: `internal/agents/registry` (the provider interface and registry), `internal/profiles/credentials` (storage, endpoints). Plugins: `plugins/agents/claude-code` and `plugins/agents/codex`, each with its credential methods, verifiers and setup contributor (ported from `ClaudeCodeSetup` and `CodexSetup`). A credential whose agent plugin is turned off is kept but not written into sandboxes, and setting one is `422 plugin_disabled`.

1. Registry and provider interface in core; the Claude Code and Codex plugins with their providers and verifiers.
2. Table, encryption, endpoints; `apply_setup` enqueue on change.
3. Setup contributors in each plugin, ported from `ClaudeCodeSetup` and `CodexSetup`, tested against the fake toolbox.
4. `last_ready_at` / `last_error` from setup reports; failing notification.
5. `GET /v0/agents/providers` and the shared setup-page component.

## Open questions

1. **An API proxy for keys.** Deferred (Later): a per-sandbox token for a Lewbox proxy to Anthropic and OpenAI, keeping real keys out of sandboxes.
2. **More agents.** Deferred (Beta): OpenCode first (its plugin is planned, through the generic ACP driver), then by demand; each is an agent plugin ([plugins/agent-providers](../plugins/agent-providers.md)).
3. **Project-wide credentials.** Deferred (Beta): one key a project sets for all its profiles, with a profile's own taking precedence.
4. **Bedrock and Vertex.** Deferred (Later): as credential methods of the `agents/claude-code` plugin, when an enterprise needs them.
