# Inbound GitHub webhooks

> Status: Draft · Milestone: Alpha (M3) for installation events; Alpha (M4) for forwarding · Plugin: `git/github` (its webhook ingress) · Updated: 2026-10-29

Each project's GitHub App ([git/github-app](../git/github-app.md)) sends its webhooks to Lewbox, since GitHub gives an App one webhook URL. Lewbox uses the installation events to keep profiles' connections current without polling GitHub (installations added, removed, suspended, repositories changed, permissions accepted, user authorizations revoked), and **forwards every other event to the project**, as it came from GitHub, to the webhook endpoints that subscribe to it (`github.pull_request`, `github.push`, `github.*`). Lewbox doesn't interpret them: a project that acts on GitHub with an [issued token](../git/tokens.md#the-token-api) follows GitHub with these.

---

## Today

Legacy (`lewbox-legacy/app/Http/Controllers/Api/GitHub/v1/WebhookController.php`, `app/Actions/GitHub/HandleGitHubAppWebhook.php`; `docs/profiles.md` § *App tokens are minted for each sandbox*, *Several installations per connection*):

- `POST /api/github/v1/apps/{tenant}/webhooks`, verified with the tenant App's webhook secret (`X-Hub-Signature-256`).
- `installation`: `created` adds the installation to the tenant's connections made by its `sender` or `requester` (an owner approving a request); `deleted` removes its rows, and the connection when it was the last; `suspend`/`unsuspend` record GitHub refusing it or not; `new_permissions_accepted` re-mints tokens in running sandboxes at once. Each re-applies setup to the affected profiles' running sandboxes.
- `installation_repositories`: the repositories are read again next time.
- `github_app_authorization` `revoked`: forgets the user tokens of connections made by that login.
- Anything else ignored. No deduplication by `X-GitHub-Delivery`. Nothing forwarded.

Lewagent asks for (`lewagent-legacy/docs/todo.md` § *Waiting on Lewbox*; `lewbox-legacy/docs/todo.md` line 146): `pull_request` (`closed`, merged or not, with base, head and who merged) forwarded, for branch setups merged on GitHub; `gollum` and `push` (to the default branch) forwarded, for keeping a wiki's copy in step.

## Goals

- The same installation handling, idempotent and deduplicated.
- **Every other event forwarded, whole**, to the project's webhook endpoints subscribed to it, with the profiles that reach the repository. No trimmed shapes of our own to keep in step with GitHub.
- Fast acknowledgement: verify, store, answer `202`, process as a job.
- Deliveries kept a while, readable through the API (for payloads too large to forward) and visible in the dashboard.

### Non-goals

- Interpreting GitHub's events beyond installations: what a merged pull request means is the project's business.
- Forwarding to anyone but the project's own webhook endpoints.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Signature verification with the App's webhook secret; the installation, `installation_repositories` and `github_app_authorization` handling |
| **Change** | The URL becomes `/hooks/github/{prj}`. Deliveries are stored and deduplicated by `X-GitHub-Delivery`, and handled as jobs. Installations are the project's rows, linked to connections ([git/github-app](../git/github-app.md#tables)) |
| **Drop** | Ignoring every other event. Legacy's plan for a trimmed, Lewbox-defined `pull_request`/`push`/`gollum` shape: we forward GitHub's own payload instead |

## Design

### Endpoint

`POST /hooks/github/{prj}`

1. Look up the project's App; none → `404`.
2. Verify `X-Hub-Signature-256` (`sha256=` + hex HMAC-SHA256 of the raw body with the App's webhook secret), constant-time; mismatch → `401`, logged with the delivery id (GitHub shows it to the App's owner).
3. Body up to 25 MB (GitHub's maximum).
4. Insert into `github_deliveries` with `X-GitHub-Delivery` as a unique key; a duplicate → `200` without doing anything (GitHub redelivers on request, and on timeouts).
5. Enqueue `github.handle_delivery` **in the same transaction**, answer `202`. GitHub's 10-second timeout is never at risk.

`github_deliveries`: `id`, `project_id`, `delivery_id` (unique), `event` (`X-GitHub-Event`), `action`, `installation_id`, `repository`, `sender_login`, `payload` (`jsonb`), `received_at`, `handled_at`, `forwarded_events` (the `whe_` ids it became), `error`. Kept 7 days (`LEWBOX_GITHUB_DELIVERIES_RETENTION`), partitioned by day.

### Handling

| Event | Action | Does |
|---|---|---|
| `installation` | `created` | Upsert the `github_installations` row and record that the `sender` and `requester` reach it (`github_person_installations`, matched on `github_people.github_user_id`). Link it to every profile that has the sender or requester as one of its people, up to each profile's [limit](../git/github-app.md#the-limit) (over it, it's listed as available instead). Re-apply setup on those profiles' running sandboxes. Raise `git_connection.installation_added` |
| `installation` | `deleted` | Mark the installation deleted and unlink it everywhere; remove that account's tokens from running sandboxes ([git/tokens](../git/tokens.md#revoking)). Raise `git_connection.installation_removed` with every profile that linked it |
| `installation` | `suspend` / `unsuspend` | Set or clear `suspended_at`; suspended installations mint no tokens. Raise `git_connection.installation_suspended` / `.installation_added` |
| `installation` | `new_permissions_accepted` | Update the installation's `permissions` (from the payload, confirmed with `GET /app/installations/{id}`); re-mint **at once** every running sandbox's tokens on that installation, so `full` and `write` sandboxes get the new permissions now rather than at their next refresh ([git/tokens](../git/tokens.md#refreshing)). Custom permission sets are unchanged |
| `installation_repositories` | `added` / `removed` | Update `repository_selection`; drop the repository list caches; for `removed`, sandboxes reaching removed repositories get a re-mint (which then fails for those, shown in setup) |
| `github_app_authorization` | `revoked` | Clear the user token and refresh token of the person who is that `sender` (`github_people`, by user id); `user_authorized = false`. Installation tokens for the accounts linked through them keep working |
| `installation_target` | `renamed` | Update `account` on rows matching `account_id` |
| `ping` | — | `github_apps.webhook_verified_at` |
| anything else | any | [Forward](#forwarding) |

Installation events aren't forwarded: Lewbox's own `git_connection.*` events report what they changed, with the profiles. Each handler is idempotent (upserts, deletes by id), so a redelivery or a retried job does nothing new.

### Forwarding

For every event that has an `installation.id` and isn't only Lewbox's to handle:

1. **Anyone listening?** Find the project's webhook endpoints subscribed to `github.<event>` or `github.*` ([webhooks](webhooks.md#subscriptions)). None → mark the delivery handled; nothing is raised. This keeps busy organizations' traffic from becoming events no one reads.
2. **Which profiles?** Profiles whose connections link the event's installation. For an installation with `selected` repositories and an event naming a repository, the repository must be among the installation's (checked against a cached list, refreshed on `installation_repositories`). An event with no repository (e.g. `installation`) names every linking profile.
3. **Raise one webhook event** per delivery, `github.<event>` (GitHub's event name, e.g. `github.pull_request`; the action is in the payload, as on GitHub), through the transactional outbox ([webhooks](webhooks.md)):

```json
{
  "type": "github.pull_request",
  "data": {
    "delivery": "72d3162e-cc78-11e3-81ab-4c9367dc0958",
    "event": "pull_request",
    "action": "closed",
    "repository": "acme/web",
    "installation": { "id": 51234567, "account": "acme" },
    "profiles": [ { "id": "prf_…", "external_id": "cus_123" } ],
    "payload": { "action": "closed", "number": 42, "pull_request": { "…": "GitHub's own" } },
    "payload_truncated": false
  }
}
```

- **`payload` is GitHub's, unchanged**, so GitHub's docs describe it and its changes are GitHub's to announce. Up to `LEWBOX_GITHUB_FORWARD_MAX_PAYLOAD` (512 KB) it's included; above that `payload` is `null`, `payload_truncated` is `true`, and the project reads it with `GET /v0/projects/{prj}/settings/git/github/deliveries/{delivery}` while the delivery is kept (7 days).
- Endpoints with a `profiles` filter get it when any listed profile matches; a `repositories` filter is an [open question](#open-questions).
- What the App is sent is decided by its subscriptions on GitHub ([git/github-app](../git/github-app.md#registering-the-app)); the App page shows events Lewbox would subscribe to that it lacks.

## API

| Method and path | Who | Does |
|---|---|---|
| `POST /hooks/github/{prj}` | GitHub | Receive |
| `GET /v0/projects/{prj}/settings/git/github/deliveries` | admin key, session | Recent deliveries: event, action, repository, handled, forwarded, error |
| `GET /v0/projects/{prj}/settings/git/github/deliveries/{delivery}` | admin key, session | One delivery, with GitHub's payload |
| `POST /v0/projects/{prj}/settings/git/github/deliveries/{delivery}/replays` | admin key, session | Handle a stored delivery again (after a fix) |

## Security

- Signature verified on the raw body before anything is parsed or stored.
- The project id in the URL only selects which secret to check; a valid signature from another project's App can't be used against this one.
- Installation events act only on rows in the same project, and `created` links an installation only to connections of the GitHub user who made or requested it.
- Forwarded payloads are GitHub's, which can include commit messages, branch names and comment bodies from the customer's repositories. They go only to the project's own endpoints, signed as any Lewbox webhook, and to no one else.

## Observability

- Metrics: `lewbox_github_webhooks_total{event,outcome}`, `lewbox_github_webhook_handle_seconds`, `lewbox_github_webhook_signature_failures_total{project}`, `lewbox_github_webhooks_forwarded_total{event}`.
- Activity (profile scope): `github.installation_added`, `github.installation_removed`.
- Dashboard: the App page lists recent deliveries, what each became, and missing events.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_GITHUB_DELIVERIES_RETENTION` | `7d` | |
| `LEWBOX_GITHUB_WEBHOOK_MAX_BODY` | `25MB` | |
| `LEWBOX_GITHUB_FORWARD_MAX_PAYLOAD` | `512KB` | Larger payloads are forwarded without `payload` |

## Testing

- Signed fixtures for every handled event and action, from GitHub's docs and recorded deliveries.
- Duplicate delivery ids; replays; out-of-order `created`/`deleted`.
- Forwarding: no subscribers (nothing raised), `selected` installations, several profiles linking one installation, profile filters, a payload over the cap.

## Build plan

1. M3: endpoint, `github_deliveries`, installation and authorization handlers.
2. M4: forwarding, the deliveries API, `missing_events` on the App page.

## Open questions

1. **Repository filters on endpoints**: should a webhook endpoint be able to subscribe to `github.*` for some repositories only, as well as some profiles?
2. **Retention of deliveries**: 7 days enough? They hold full GitHub payloads, which may include private repositories' content.
3. **Events without an installation** (e.g. `meta`, `security_advisory`): forward to the project as-is, or ignore? Proposed: ignore.
