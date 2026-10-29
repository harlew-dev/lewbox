# Errors

> Status: Draft · Milestone: Alpha (M0 for the framework; codes arrive with their features) · Updated: 2026-10-29

When a request fails, Lewbox answers with an HTTP status and one JSON body saying why, with a stable `code` for programs. This plan is the **one catalogue** of every code, merged from the public docs, the gateway and the app, with what's kept, renamed, added and removed. Codes live in one Go registry that the handlers, the OpenAPI document, the SDKs' error classes and the docs are all built from.

## Today

Two catalogues that mostly agree:

- The gateway's, in `lewbox-gateway-legacy/docs/api-conventions.md` and the public [errors page](https://lewbox.com/docs/basics/errors), with codes as string literals across `internal/server/*.go`.
- The app's, in `lewbox-legacy/docs/admin/basics/errors.md`, with codes as string literals in controllers and actions (`'code' => 'github_unavailable'` in six places).
- App refusals were passed through the gateway unchanged (`*lewbox.RefusalError`), so a code could originate in either service, and nothing checked the two lists matched. Some codes exist only in code (`push_in_progress`, `operation_claimed`), some only in docs (`wrong_domain`, planned).
- Field error messages came from Laravel's validator in the Admin API and from hand-written Go in the gateway, in different voices.

## Goals

- One registry of codes in Go: each with its status, a default message, its extra fields, and its docs. Handlers can only answer codes from it.
- The OpenAPI document's error schema, the SDKs' error classes, and the docs' tables are generated from the registry; CI fails if they differ ([openapi-and-codegen](openapi-and-codegen.md)).
- Messages are for people: full sentences saying what to do. Codes are for programs: never changed, never reused.

## The error body

```json
{
  "message": "This sandbox has an operation in progress. Wait for it to finish, then try again.",
  "code": "operation_in_progress",
  "operation": {"id": "op_Vb7nM2xQ9kLp4T", "kind": "stop"}
}
```

| Field | Type | |
|---|---|---|
| `message` | string | For people. A full sentence, saying what to do where it can. May be reworded: never match on it |
| `code` | string | For programs. Stable: a code never changes meaning and is never reused |
| `errors` | object | Only on `422 validation_failed`: field errors, keyed by field path (`repositories.2.ref`, `env.MY_VAR`, `body`), each a list of messages |
| *extra* | varies | Some codes carry one more field that says more; see [Extra fields](#extra-fields) |

Every error response also carries `Lewbox-Request-Id`. `401` responses carry `WWW-Authenticate: Bearer realm="lewbox"` (with `error="invalid_token"` for `invalid_key`, per RFC 6750). `405` carries `Allow`. `429` and some `503`s carry `Retry-After`.

**New codes may be added.** Clients must handle an unknown code by its status: a `4xx` means the request needs changing, a `5xx` means it may work later.

### Field errors

- Keyed by the request field's path, dotted, with array indexes: `repositories.0.name`. Query parameters by name. A malformed body is `body`. A missing required header is `header.Idempotency-Key`.
- Messages are sentences about the field, written by us (not a validator library's defaults), e.g. `"Choose a cluster this key reaches."`, `"Use 60 to 2592000 seconds."`.
- Every invalid field is reported at once, not just the first.

---

## Catalogue

**State:** *kept* (in legacy, same meaning), *renamed* (in legacy as another code), *new*, or *changed* (same code, wider or narrower meaning). **M:** the milestone it arrives with.

### 400

| Code | When | State | M |
|---|---|---|---|
| `bad_request` | The request can't be parsed at the HTTP level: a malformed query string, a bad `Range` header, a WebSocket handshake missing required headers | new | M0 |

### 401: who's calling

| Code | When | State | M |
|---|---|---|---|
| `unauthenticated` | No credentials: no `Authorization: Bearer`, no session cookie, no `lewbox.bearer` subprotocol | kept | M0 |
| `invalid_key` | A key that's malformed, unknown, revoked or expired, or whose profile or project was deleted | kept | M0 |
| `session_expired` | A session cookie that has expired, been signed out, or whose user was disabled | new | M1 |
| `invalid_token` | On `/agentd/v0` and `/node/v0`: an agentd token, node token or install token that's invalid, expired, or for something else | kept (agentd), widened | M1 |
| `wrong_domain` | A key used on a custom domain that belongs to another project | kept (planned) | Later |
| `setup_session_ended` | A setup session that expired, or whose link was revoked or ended ([profiles/setup-links](../profiles/setup-links.md)) | new | M3 |

### 403: what they may do

| Code | When | Extra | State | M |
|---|---|---|---|---|
| `wrong_key_kind` | A principal on a route it can never use: a sandbox key outside its sandbox, an admin key on [what stays human](../api.md#what-stays-human), a profile key on project routes, a setup session outside its profile. The message says which kind of credential the route takes | | changed: was `401` for a tenant key on the Consumer API; now always `403`, since the key *is* valid | M0 |
| `permission_denied` | The principal lacks the permission the route needs. The message names it | `permission` | kept | M0 |
| `csrf_failed` | A session request that changes something, without a valid CSRF header or from another origin | | new | M1 |
| `reauthentication_required` | A dangerous action (creating an admin key, deleting a project) from a session that signed in more than 15 minutes ago. Sign in again with a fresh code | | new | M1 |
| `project_suspended` | The principal's project is suspended by the instance's admins | | new | M1 |
| `profile_suspended` | The key's profile is suspended | | kept | M1 |
| `user_disabled` | Signing in as a user an instance admin disabled | | new | M1 |
| `section_forbidden` | A setup session on a setup section its link wasn't made for | `section` | new | M3 |
| `path_forbidden` | The sandbox refused a file operation on a path it may not touch | | kept | M2 |
| `instance_admin_required` | A user who isn't an instance admin, on `/v0/instance` ([projects/instance-administration](../projects/instance-administration.md)) | | new | M1 |
| `project_creation_disabled` | Creating a project when the instance's policy doesn't let this user ([projects/instance-administration](../projects/instance-administration.md)) | | new | M1 |
| `invitation_email_mismatch` | Accepting a project invitation made for another email address ([projects/members-and-roles](../projects/members-and-roles.md)) | | new | M1 |
| `node_failed` | Node API: a node token whose installation failed, until it's retried ([clusters/installation](../clusters/installation.md)) | | new | M1 |
| `sign_up_disabled` | Reserved for OAuth sign-up refusals; email-code sign-up answers like any code request ([projects/instance-administration](../projects/instance-administration.md)) | | new (reserved) | Later |

### 404

| Code | When | State | M |
|---|---|---|---|
| `not_found` | Nothing at this path, or nothing this principal can see. A sandbox on a cluster the key doesn't reach, another project's anything, another profile's sandbox: all `404`, never `403`, so ids can't be probed | kept | M0 |
| `repository_not_found` | A repository GitHub doesn't have, or the profile's connection doesn't reach | kept | M3 |

### 405, 406, 409, 410

| Status | Code | When | Extra | State | M |
|---|---|---|---|---|---|
| 405 | `method_not_allowed` | The path exists, but not for this method. `Allow` lists those it takes | | kept | M0 |
| 406 | `not_acceptable` | `Accept` names nothing the endpoint can answer with | | new | M0 |
| 409 | `operation_in_progress` | The sandbox (or build) already has an operation in progress | `operation` | kept | M1 |
| 409 | `invalid_state` | The resource can't do that now: starting a sandbox that isn't stopped, stopping one that isn't running, anything on a deleted one, retrying a finished delivery | `status` | kept, widened (`status` added) | M1 |
| 409 | `sandbox_not_running` | Commands, sessions, files or exposing a port, in a sandbox that isn't running | `status` | kept | M2 |
| 409 | `session_not_running` | Signalling, resizing, attaching to or reading the log of a session that's ended or lost | `status` | kept | M2 |
| 409 | `already_exists` | Making a directory where something is; a project slug, cluster name or SSH key name that's taken | | kept, widened | M1 |
| 409 | `port_protocol_conflict` | Exposing a port already exposed with the other protocol | | kept | M2 |
| 409 | `port_not_private` | A link to a public port, which needs none | | kept | M2 |
| 409 | `port_unreachable` | A link to a private port that isn't reachable yet, as while its sandbox starts | | kept | M2 |
| 409 | `template_in_use` | Deleting a template a sandbox was created from | | kept (planned) | M1 |
| 409 | `cluster_suspended` | Creating or starting a sandbox on a suspended cluster | `cluster` | kept | M1 |
| 409 | `cluster_not_empty` | Deleting a cluster that still has nodes or sandboxes | | new | Beta |
| 409 | `profile_has_sandboxes` | Deleting a profile that still has sandboxes | | kept | M1 |
| 409 | `project_not_empty` | Deleting a project that still has sandboxes or nodes | | new | M1 |
| 409 | `installation_finished` | Cancelling a cluster installation that's already online or failed; on the node API, using its token after it has finished | | kept | M1 |
| 409 | `last_owner` | Removing or demoting a project's last owner, or deleting the account of someone who is the only owner of a project ([projects/users-and-login](../projects/users-and-login.md)) | | new | M1 |
| 409 | `already_member` | Inviting someone who's already a member ([projects/members-and-roles](../projects/members-and-roles.md)) | | new | M1 |
| 409 | `external_id_taken` | Creating a profile, or changing one, to an `external_id` a live profile has; carries that `profile` so a retry can recover it ([profiles](../profiles.md)) | `profile` | new | M1 |
| 409 | `isolation_in_use` | Removing an isolation level from a cluster while sandboxes at that level exist on it ([clusters/runtimes](../clusters/runtimes.md)) | `count` | new | M1 |
| 409 | `cluster_locked` | Changing a cluster's architecture or GPU vendor after a node has enrolled ([clusters/runtimes](../clusters/runtimes.md)) | | new | M1 |
| 409 | `ssh_key_on_profile` | Adding to a sandbox an SSH key its profile already has, which removing from the sandbox couldn't revoke ([profiles/ssh-keys](../profiles/ssh-keys.md)) | | new | M2 |
| 409 | `setup_link_used` | Using a setup link that's already been used ([profiles/setup-links](../profiles/setup-links.md)) | | new | M3 |
| 409 | `sign_ins_unavailable` | Starting an agent sign-in when it can't run: no sign-in cluster, the `agents` template not ready there, or the instance turned the provider off ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)) | `reason` | new | M3 |
| 409 | `sign_in_not_awaiting_code` | Sending a code to a sign-in that isn't waiting for one | | new | M3 |
| 409 | `sign_in_finished` | Cancelling, or sending a code to, a sign-in that has finished | | new | M3 |
| 409 | `worktree_removed` | Prompting a conversation, or starting one, in a worktree that's been removed ([agents/worktrees](../agents/worktrees.md)) | | new | Later |
| 409 | `reference_used` | A credit transaction whose `reference` the profile has already used; carries the original `transaction` ([credits/balances](../credits/balances.md)) | `transaction` | new | Beta |
| 409 | `subscription_exists` | A second subscription bucket when the project doesn't allow several ([credits/balances](../credits/balances.md)) | | new | Beta |
| 409 | `installation_started`, `already_enrolled`, `seed_not_ready`, `cluster_has_no_bundles` | Node API: an installation token used again after it started; a node enrolling twice; a joining node before the cluster's seed is online, or before the seed has uploaded the CA ([clusters/installation](../clusters/installation.md), [clusters/pki-and-dns](../clusters/pki-and-dns.md)) | | new | M1 |
| 409 | `turn_running` | Prompting with `queue: false`, or deleting a conversation, while a turn runs; cancelling a turn that has ended | | kept | M3 |
| 409 | `queue_full` | Prompting a conversation with 20 prompts queued | | kept | M3 |
| 409 | `not_steerable` | Steering into a turn that can't take it now, as one waiting on an approval | | kept | M3 |
| 409 | `not_queued` | Steering or cancelling a prompt that isn't queued | | kept | M3 |
| 409 | `request_answered` | Replying to an approval or question that's already been answered or has expired | | new | M3 |
| 409 | `task_not_stoppable` | Stopping a background task that's the agent's own work (a subagent), not a command or monitor | | kept | M3 |
| 409 | `worktree_failed` | git couldn't make a conversation's worktree; the message is git's | | kept | M3 |
| 409 | `worktree_dirty` | Deleting a conversation, or a worktree, with uncommitted changes, without `force` | | kept | M3 |
| 409 | `worktree_in_use` | Removing a worktree a conversation still uses | | kept (planned) | Later |
| 409 | `name_taken` | A worktree name another worktree has | | kept (planned) | Later |
| 409 | `sign_in_in_progress` | Starting an agent sign-in while another runs for the same profile and agent | | new | M3 |
| 409 | `git_not_connected` | Something that needs the profile's connection to a Git provider, with none, or (GitHub) with no linked installation on a repository's owner | `provider`, `accounts` | renamed from `github_not_connected` | M3 |
| 409 | `git_provider_not_configured` | An install link, or connecting a profile, before the project has configured the provider (GitHub: registered or brought its App) | `provider` | new | M3 |
| 409 | `git_person_not_authorized` | Asking for a user token when the person hasn't authorized the provider (GitHub: the App), or their authorization lapsed | `provider`, `person` | new | M3 |
| 409 | `plugin_disabled` | The plugin that owns this route, field, provider or agent is compiled in but off for the instance or for this project ([plugins](../plugins.md#turning-plugins-on-and-off)). A plugin not in the build has no routes (`404`) and its fields are unknown (`422 validation_failed`) | `plugin` | new | M0 |
| 409 | `plugin_in_use` | Turning off a driver plugin something still uses, e.g. `dns/libdns` while clusters use one of its providers | `plugin`, `used_by` | new | M0 |
| 409 | `cluster_has_nodes` | Changing a cluster's DNS provider or domain while it has nodes ([clusters/pki-and-dns](../clusters/pki-and-dns.md)) | | new | M1 |
| 409 | `cluster_has_snapshots` | Changing a cluster's storage while snapshots exist ([clusters/storage-and-registry](../clusters/storage-and-registry.md#the-bucket)) | | new | M1 |
| 409 | `aerolvm_version_unsupported` | Enrolling a node, or upgrading a cluster, to an AerolVM release below Lewbox's minimum fork release; carries `minimum` ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)) | `minimum` | new | M1 |
| 410 | `setup_link_expired` | Using a setup link past its expiry ([profiles/setup-links](../profiles/setup-links.md)) | | new | M3 |
| 410 | `installation_expired` | Node API: an installation token past its expiry ([clusters/installation](../clusters/installation.md)) | | new | M1 |
| 409 | `idempotency_key_in_use` | A request with an `Idempotency-Key` whose first request is still being handled, after waiting 30 s for it ([idempotency](idempotency.md#races)) | | new | M0 |

### 412, 413, 415, 422, 426

| Status | Code | When | Extra | State | M |
|---|---|---|---|---|---|
| 412 | `precondition_failed` | A conditional file write (`If-Match`, `If-None-Match`) whose file has changed. Offered once the fork's conditional writes (F51) are in the minimum release | `etag` | new | Alpha with F51 (P1), else Beta |
| 413 | `payload_too_large` | A body over its endpoint's limit | `limit_bytes` | new | M0 |
| 415 | `unsupported_media_type` | A body that isn't JSON on a JSON endpoint | | new | M0 |
| 422 | `validation_failed` | The request's fields are invalid; see `errors` | `errors` | kept | M0 |
| 422 | `project_required` | A session listing top-level resources without `project` | | new | M1 |
| 422 | `profile_required` | A session or admin key creating a sandbox, template or conversation scope without `profile` | | new | M1 |
| 422 | `cluster_required` | A create that names no cluster, when several could be chosen and none is the default | | kept | M1 |
| 422 | `template_unavailable` | The template can't be used on that cluster | `template` | kept | M1 |
| 422 | `persistence_unsupported` | `persist` on a cluster without cloud storage, or at an isolation level that can't snapshot there | `cluster`, `isolation` | kept | M1 |
| 422 | `isolation_unavailable` | The isolation level asked for isn't offered by the cluster or allowed for the profile, or none is both ([clusters/runtimes](../clusters/runtimes.md#choosing-a-sandboxs-level)) | `isolation`, `cluster`, `reason`, `available` | **new** | M1 |
| 422 | `private_ports_unavailable` | A private port on a cluster without a domain, or whose proxy couldn't start (with why) | | kept, narrowed (no longer "server has no signing key", which is now required config) | M2 |
| 422 | `sandbox_refused` | The sandbox refused the request, e.g. a directory that doesn't exist. The message is the sandbox's | | kept | M2 |
| 422 | `not_a_repository` | A conversation `worktree` whose `directory` isn't in a git repository | | kept | M3 |
| 422 | `agent_refused` | agentd refused the request, e.g. a setting the agent doesn't have. The message is its | | kept | M3 |
| 422 | `credential_invalid` | An agent credential, Git token or telemetry endpoint that failed its check with the provider | `provider_message` | new | M3 |
| 422 | `plugin_dependency` | Turning on a plugin whose required plugin is off for the project, or turning off one that another plugin on for the project requires, e.g. `limits` while `credits` is on ([projects/settings](../projects/settings.md#plugins)) | `plugin`, `requires` or `required_by` | new | M0 |
| 422 | `dns_connection` | A cluster's DNS connection test failed ([plugins/dns-providers](../plugins/dns-providers.md)) | `step`, `reason` | new | M1 |
| 422 | `storage_connection` | A cluster's storage connection test failed ([plugins/storage-providers](../plugins/storage-providers.md)) | `step`, `reason` | new | M1 |
| 422 | `invalid_code` | A sign-in code that's wrong, expired or used: one code for all three, so it reveals nothing ([projects/users-and-login](../projects/users-and-login.md)) | | new | M1 |
| 422 | `dns_required` | A `direct` cluster without DNS settings, outside development ([clusters/connectivity](../clusters/connectivity.md)) | | new | M1 |
| 422 | `relay_unavailable` | A `tunnel` cluster on an instance with no relay domain ([clusters/connectivity](../clusters/connectivity.md)) | | new | Beta |
| 422 | `credits_disabled` | Credit buckets or transactions on a profile that doesn't use credits ([credits/balances](../credits/balances.md)) | | new | Beta |
| 422 | `architecture_mismatch`, `ip_in_use`, `ip_not_public` | Node API, enrolling: the host's architecture isn't the cluster's; its IP is another node's; a `direct` node without a public IP ([clusters/installation](../clusters/installation.md)) | | new | M1 |
| 422 | `invalid_certificate_request` | Node API: a certificate request that isn't a PEM CSR, has a bad signature or a weak key ([clusters/pki-and-dns](../clusters/pki-and-dns.md)) | | new | M1 |
| 422 | `idempotency_key_reused` | An `Idempotency-Key` already used within 24 hours for a different request, or by another credential | | kept | M0 |
| 426 | `upgrade_required` | A WebSocket endpoint opened without `Upgrade: websocket` | | kept | M2 |

### 402, 429: limits and credits

| Status | Code | When | Extra | State | M |
|---|---|---|---|---|---|
| 402 | `credits_exhausted` | The profile's credits are used up: creates and starts are refused, and its running sandboxes are being stopped | | kept | Beta |
| 429 | `limit_exceeded` | A profile limit refuses this (running sandboxes, vCPUs, storage, a budget, or `git_connections`: Git people and accounts linked to the profile, across providers, [git/github-app](../git/github-app.md#the-limit)). Retrying won't help until something stops, the period ends, or something is unlinked | `limit` | kept | M1 |
| 429 | `rate_limited` | Too many requests from this credential. Wait for `Retry-After` | | kept | M0 |
| 429 | `quota_exceeded` | A project quota the instance set refuses a create: clusters, nodes, profiles, admin keys, webhook endpoints or members ([projects/instance-administration](../projects/instance-administration.md#quotas)). Distinct from a profile's `limit_exceeded` | `quota` | new | M1 |

### 5xx: failures

| Status | Code | When | Extra | State | M |
|---|---|---|---|---|---|
| 500 | `internal_error` | A bug in Lewbox. Quote the request id | | kept | M0 |
| 502 | `sandbox_error` | The sandbox failed to do it, e.g. reading a directory as a file. The message is the sandbox's | | kept | M2 |
| 502 | `create_failed` | A create failed while the request waited. The sandbox is left `failed` | `sandbox` | kept | M1 |
| 502 | `hook_failed` | A create that waited, whose `required` `on_create` or `on_start` hook failed. The sandbox is `running` ([sandboxes/hooks](../sandboxes/hooks.md#failures)) | `sandbox`, `hook {name, label}` | new | M2 |
| 502 | `agent_unavailable` | agentd couldn't be started or reached in the sandbox | | kept | M3 |
| 502 | `git_provider_unavailable` | The Git provider couldn't be reached, answered with a server error, or rate-limited us (GitHub: the App) | `provider` | renamed from `github_unavailable` | M3 |
| 502 | `git_provider_refused` | The Git provider refused the profile's connection (GitHub: the installation suspended or uninstalled, the App's key refused, an authorization revoked) | `provider` | renamed from `github_refused` (was a push `error_code`) | M3 |
| 502 | `dns_provider_refused` | The DNS provider refused a call (creating a node's records, a challenge), with its reason | `provider`, `provider_message` | new | M1 |
| 502 | `provider_unavailable` | An agent provider (Anthropic, OpenAI), a DNS provider or a storage provider couldn't be reached while checking a credential | `provider` | new | M1 |
| 503 | `service_unavailable` | Lewbox is briefly unavailable (its database, or it's starting or stopping). Retry with backoff | | renamed from `app_unavailable` | M0 |
| 503 | `cluster_capacity_exceeded` | The cluster had no room for the sandbox after 2 minutes of trying | | kept | M1 |
| 503 | `cluster_unavailable` | The cluster, or the sandbox's node, can't be reached. It's the project's infrastructure, and the message says so | `cluster` | kept | M1 |
| 503 | `toolbox_unavailable` | The sandbox didn't answer within 30 s, as briefly after a start | | kept | M2 |

### Removed

| Code | Why |
|---|---|
| `app_unavailable` | No app to be unavailable. `service_unavailable` covers the database |
| `agents_unavailable` | A replica without agentd builds or a signing key can't exist: they're embedded, and the key is required config |
| `operation_claimed`, `operation_not_claimed` | Internal to the gateway↔app lease protocol, which is gone |
| `invalid_request` | The gateway's code for a bad worktree name; it's `validation_failed` on `worktree.name` like any other field |
| `wrong_key_kind` as `401` | Always `403` now (above) |

Codes earlier drafts of these plans used for something the catalogue already had, merged so each failure has one code: `credential_refused` → `credential_invalid`; `role_forbidden` → `permission_denied` (the message names the role); `sole_owner` → `last_owner`; `insufficient_credits` → `validation_failed` on `amount`; `limit_reached` (webhook endpoints) → `quota_exceeded`; `provider_disabled` (sign-ins) → `sign_ins_unavailable` with `reason: provider_disabled`.

---

## Extra fields

| Code | Field | Type | Holds |
|---|---|---|---|
| `permission_denied` | `permission` | string | The permission needed, e.g. `sandboxes.exec` |
| `operation_in_progress` | `operation` | object | `id`, `kind`, `status` |
| `invalid_state`, `sandbox_not_running`, `session_not_running` | `status` | string | The resource's status now |
| `limit_exceeded` | `limit` | object | `kind`, `value`, `period`, and `current` (new: what's in use) |
| `template_unavailable` | `template` | object | `id`, `cluster` (`id`, `name`), `reason` |
| `cluster_suspended`, `persistence_unsupported`, `cluster_unavailable` | `cluster` | object | `id`, `name` |
| `isolation_unavailable` | `isolation`, `cluster`, `reason`, `available` | string or null, object, string, array | The level asked for (or `null`); the cluster's `id` and `name`; `not_offered`, `not_allowed` or `none_allowed`; the levels this profile may use on this cluster |
| `create_failed` | `sandbox` | object | `id`, `status` |
| `external_id_taken` | `profile` | object | The live profile with that `external_id`: `id`, `name` |
| `isolation_in_use` | `count` | integer | Sandboxes at that level on the cluster, stopped persistent ones included |
| `sign_ins_unavailable` | `reason` | string | `no_cluster`, `template_building`, `template_failed` or `provider_disabled` |
| `quota_exceeded` | `quota` | object | `kind`, `value`, and `current` |
| `reference_used` | `transaction` | object | The transaction first made with that reference |
| `section_forbidden` | `section` | string | `agents`, `git`, `ssh_keys` or `telemetry` |
| `precondition_failed` | `etag` | string | The file's current ETag |
| `payload_too_large` | `limit_bytes` | integer | The endpoint's limit |
| `credential_invalid` | `provider_message` | string or null | What the provider said, redacted |
| `provider_unavailable` | `provider` | string | e.g. `anthropic`, `cloudflare`, `s3` |
| `git_not_connected`, `git_provider_not_configured`, `git_person_not_authorized`, `git_provider_refused`, `git_provider_unavailable` | `provider` | string | The Git provider, e.g. `github`. Git errors are provider-neutral (decided 2026-10-29), so a second provider reuses them |
| `plugin_disabled`, `plugin_in_use`, `plugin_dependency` | `plugin` | object | `name` (e.g. `credits`, `git/github`) and `scope` (`instance` or `project`) |
| `plugin_in_use` | `used_by` | array | What uses it: `{type, id}`, e.g. `{type: "cluster", id: "clu_…"}` |
| `plugin_dependency` | `requires`, `required_by` | array | Plugin names: what must be on first, or what's on and needs it |
| `dns_connection`, `storage_connection` | `step`, `reason` | string | Which step of the test failed (`zone`, `set`, `delete`; `write`, `read`, `list`, `multipart`, `presign`) and why |

`template.reason` is one of `no_cloud_storage`, `not_on_cluster`, `building`, `failed`, `not_built` ([templates](../templates.md)).

## Codes outside response bodies

### Operations' `error_code`

A failed operation ([sandboxes](../sandboxes.md)) carries one of these, and new ones may appear (treat unknown as `operation_failed`):

| Code | Means |
|---|---|
| `cluster_capacity_exceeded` | No room after 2 minutes of trying |
| `cluster_unavailable` | The cluster couldn't be reached in time |
| `image_pull_failed` | New: the image or template build couldn't be pulled (was `operation_failed` with a message) |
| `build_failed` | A template build failed; its `error` is Docker's last words |
| `snapshot_failed` | New: a stop couldn't save the sandbox's files in time; it's left stopped with its files where they were |
| `hook_failed` | New: a `required` start or create hook failed; the sandbox is left `running` ([sandboxes/hooks](../sandboxes/hooks.md#failures)) |
| `scrub_failed` | New: a stop couldn't prove the snapshot was free of credentials Lewbox imported, so nothing was uploaded; the sandbox is left stopped on its node, and the stop can be retried ([sandboxes/profile-setup](../sandboxes/profile-setup.md#when-a-scrub-fails)) |
| `operation_failed` | Anything else; `error` says what |

### Stream errors

The last event of an SSE stream, or the last text message of a WebSocket, when it ends without an exit ([streaming](streaming.md)):

| Code | Means | State |
|---|---|---|
| `command_timeout` | The command ran past its `timeout` and was killed | kept |
| `command_failed` | The command couldn't start | kept |
| `stream_interrupted` | The sandbox's stream ended before the command or turn did | kept |
| `server_restarting` | The server carrying the stream is shutting down; reconnect (turns resume, commands were killed) | renamed from `gateway_restarting` |
| `invalid_start_message` | A WebSocket's first message was late or invalid (then close `1008`) | kept |
| `key_revoked` | The credential the stream was opened with was revoked, or its profile or project suspended (then close `4403`) | new |
| `events_expired` | A resume from an event id older than the stream keeps; re-read the state instead | new |
| `slow_consumer` | The client fell too far behind and was cut; resume by event id | new |

WebSocket close codes are in [streaming](streaming.md#websocket-conventions): `1000`, `1001`, `1008`, `1011`, and the new `4403` and `4429`.

### Removed with pushes

`push_in_progress`, `push_not_waiting`, `push_not_cancellable`, and pushes' `error_code`s (`head_moved`, `lfs_not_supported`, `not_fast_forward`, `pack_too_large`, `stalled`) are gone: Lewbox no longer pushes ([git](../git.md#keep-change-drop)). [git/integrating](../git/integrating.md#pushing-from-your-servers) uses the same names for the failures a project's own push recipe meets.

---

## In the SDKs

Every SDK error extends a base (`LewboxError` / `LewboxException`) with `status`, `code`, `message`, `requestId`, and the extra field. Classes, generated from the registry's `class` hints:

| Raised for | TypeScript | PHP |
|---|---|---|
| Any `401` | `AuthenticationError` | `AuthenticationException` |
| `403 permission_denied` | `PermissionDeniedError` | `PermissionDeniedException` |
| Other `403` | `ForbiddenError` | `ForbiddenException` |
| `404` | `NotFoundError` (`SandboxNotFoundError` on a sandbox) | `NotFoundException` (`SandboxNotFoundException`) |
| `409 operation_in_progress` | `OperationInProgressError` | `OperationInProgressException` |
| `409 plugin_disabled` | `PluginDisabledError` (with `plugin`) | `PluginDisabledException` |
| Other `409` | `ConflictError` | `ConflictException` |
| `412` | `PreconditionFailedError` | `PreconditionFailedException` |
| Any `422` | `ValidationError` | `ValidationException` |
| `402` | `CreditsExhaustedError` | `CreditsExhaustedException` |
| `429 limit_exceeded` | `LimitExceededError` | `LimitExceededException` |
| `429 rate_limited` | `RateLimitError` | `RateLimitException` |
| `503 cluster_unavailable` | `ClusterUnavailableError` | `ClusterUnavailableException` |
| Other `5xx` | `ServerError` | `ServerException` |
| A failed operation | `OperationFailedError` | `OperationFailedException` |

## Build plan

`internal/httpapi/apierr`:

```go
type Code struct {
    Name    string        // "operation_in_progress"
    Status  int           // 409
    Message string        // default message, may be overridden per use
    Extra   string        // name of the extra field, if any
    Doc     string        // one line for the docs table
    Since   string        // milestone
}

var OperationInProgress = Register(Code{Name: "operation_in_progress", Status: 409, Extra: "operation", ...})

type Error struct { Code *Code; Message string; Fields map[string][]string; ExtraValue any; cause error }
```

- `apierr.New(apierr.OperationInProgress).With(op)` in services; services return typed errors and handlers map them, so services don't know HTTP.
- An unknown Go error becomes `internal_error`, logged with its stack and the request id, never shown.
- `go generate` writes the OpenAPI `ErrorCode` enum and per-operation error responses, the docs tables, and the SDKs' code-to-class maps; CI checks they're current.

Tasks: registry and type; mapping from service errors; validation error builder; generator; the docs table; tests that every registered code is reachable (a test calls something that answers it) or marked `Later`.

## Open questions

1. Should `wrong_key_kind` stay one code for every principal mismatch, or split (`wrong_key_kind`, `session_required`)? (Proposed: one code; the message says which credential to use.)
2. Should field error messages be localisable (a `errors` entry as `{code, message}` objects)? The dashboard is English-only for now. (Proposed: keep strings; add codes in `/v2` if ever needed.)
3. `already_exists` vs a code per resource (`name_taken`, `slug_taken`)? (Proposed: `already_exists` everywhere, and drop `name_taken` from the worktree plan.)
