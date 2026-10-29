# Webhooks

> Status: Draft · Milestone: Alpha (M4) · Core; plugins register their event types · Updated: 2026-10-29

A project registers HTTPS endpoints, each subscribed to the event types it wants, and Lewbox posts each event to them as signed JSON, as it happens: a sandbox becoming `running`, stopping on its idle timeout, an operation failing, a session ending, an agent's turn ending, a pull request merged on GitHub (forwarded from the project's GitHub App). Delivery is **at least once, in no particular order**, signed per [Standard Webhooks](https://www.standardwebhooks.com/), retried for about two days, and recorded attempt by attempt.

Lewagent relies on these to keep its computers' status current and to continue builds when sessions end (`lewagent-legacy/app/Jobs/Computers/HandleLewboxWebhook.php`).

---

## Today

Built in legacy on 2026-10-26 (Lewbox `d283c22`, `287690b`, `abdd652`; `lewbox-legacy/app/Actions/Webhooks/*`, `app/Models/Webhook*`, `app/Console/Commands/Webhook/SendDueWebhookDeliveries.php`, `app/Jobs/Webhooks/SendWebhookDelivery.php`; docs in `docs/admin/webhooks/{webhooks,events,delivery,verifying-signatures}.md`):

- **Endpoints** (`whk_`): `url` (https), `events` (names or `*` prefixes), `description`, `enabled`, `disabled_reason` (`failing`), `failing_since`, `secret` (`whsec_`, shown on create and roll), previous secret valid 24 h after a roll.
- **Events** (`whe_`): `{id, type, created_at, data}`; `sandbox.<status>` with `sandbox`, `previous_status`, `status`, `status_sequence`, `reason {code, message}`; `operation.started|succeeded|failed` with `operation` and `sandbox` or `template_build`; `activity.<kind>` with `event` and `sandbox`; `webhook.test`.
- **Raised in the change's transaction** (`RaiseWebhookEvent`, `RaiseSandboxStatusEvent`, `RaiseOperationEvent`), only when an enabled endpoint wants the type; delivery jobs queued after commit; a backstop every minute sends due deliveries and re-sends ones in flight past 120 s.
- **Delivery**: `POST`, 10 s timeout, any `2xx` succeeds, redirects not followed. Retries after 1 m, 5 m, 30 m, 2 h, 6 h, 12 h, 24 h (8 attempts). Each attempt kept (`webhook_delivery_attempts`) with the first 1 KB of the response body.
- **Disabling**: every delivery failing for 72 h disables the endpoint (`disabled_reason: failing`), fails its pending deliveries, and makes a tenant notification (stored, not shown).
- **SSRF** (`CheckWebhookUrl`, `ResolveWebhookHost`): https only; the host's addresses resolved and refused if private, loopback, link-local or reserved, on save and before each delivery; the connection pinned to the checked address.
- **Admin API**: `CRUD /webhook-endpoints`, `POST …/{whk}/secret`, `POST …/{whk}/tests`, `GET …/{whk}/deliveries`, `POST /webhook-deliveries/{whd}/redeliveries`. Dashboard: Settings → Webhooks, 7-day stats per endpoint (attempts per day, error rate, mean response time), deliveries by status, id or type, each attempt, redeliver, roll, test.
- **Retention**: events and deliveries pruned with the tenant's activity retention.
- **Not built** (`docs/todo.md` lines 257–259): showing notifications, per-profile filters, a reason for sandboxes lost with a node.

Lewagent's receiver drops duplicates by `webhook-id` (cached 3 days), drops stale statuses by `status_sequence`, handles `sandbox.*`, `operation.failed`, `activity.request.refused`, `activity.session.ended`, `activity.agent.turn_ended`, and still polls every minute as a backstop.

## Goals

- Keep the wire format, guarantees, retry schedule, signing and SSRF protection exactly, so receivers (Lewagent's included, via the SDK's verifier) keep working with only the URL changed.
- Transactional outbox with the job queue: event, deliveries and their jobs commit together; no backstop needed for lost jobs.
- Per-profile filters; new event families; notifications shown.
- Delivery throughput that scales with replicas, with per-endpoint concurrency limits so one slow receiver can't starve the rest.
- Self-hosters can deliver to private addresses when they choose to.

### Non-goals

- Ordering guarantees.
- Non-HTTPS transports (queues, email).
- Exactly-once.

## Design

### Tables

`webhook_endpoints`:

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | `whk_…` |
| `project_id` | fk | |
| `url` | text | |
| `description` | text | |
| `events` | text[] | Names or prefixes ending in `*` |
| `profiles` | bigint[] | ★ Only these profiles' events; empty for all |
| `enabled` | bool | |
| `disabled_reason` | text | `failing`, `gone` (★ the endpoint answered `410`), `manual` |
| `failing_since` | timestamptz | |
| `secret` | bytea | encrypted |
| `previous_secret` | bytea | encrypted |
| `previous_secret_expires_at` | timestamptz | |
| `max_concurrency` | int | ★ Default 10 |
| `created_at`, `updated_at` | | |

At most `LEWBOX_WEBHOOKS_MAX_ENDPOINTS` (20) per project.

`webhook_events`: `id` (`whe_`), `project_id`, `profile_id` (nullable), `type`, `payload` (`jsonb`, the body as posted), `created_at`. Partitioned by day like activity; retention follows the project's activity retention.

`webhook_deliveries`: `id` (`whd_`), `endpoint_id`, `event_id`, `status` (`pending`, `succeeded`, `failed`), `attempts`, `next_attempt_at`, `last_attempt_at`, `last_response` (`{status, body ≤ 1 KB, duration_ms}`), `last_error`, `redelivery_of`, `created_at`. Partitioned with the events.

`webhook_delivery_attempts`: `delivery_id`, `attempted_at`, `url`, `response`, `error`, `duration_ms`, `replica`.

### Event envelope

```json
{
  "id": "whe_Lq8sV2nX4mKp9T",
  "type": "sandbox.stopped",
  "created_at": "2026-10-26T12:00:00Z",
  "project": "prj_…",
  "data": { … }
}
```

`id` is also the `webhook-id` header, the same on every retry and redelivery. New types may be added; receivers ignore types they don't know. (`project` is new; open question on adding `api_version`.)

### Event catalogue

Alpha unless marked.

| Type | `data` | Raised when |
|---|---|---|
| `sandbox.creating`, `.starting`, `.running`, `.stopping`, `.stopped`, `.deleting`, `.deleted`, `.failed` | `sandbox` (as `GET /v0/sandboxes/{sbx}` shows it, plus `profile {id, name, external_id}`, `cluster {id, name}`, `node {id, name}`), `previous_status`, `status`, `status_sequence`, `reason {code, message}` or null | Any change of the sandbox's status, whoever caused it |
| `operation.started`, `.succeeded`, `.failed` | `operation`, and `sandbox {id, profile}` or `template_build {id, template, cluster}` | An operation is taken up, or finishes |
| `activity.<kind>` | `event` (as the activity log lists it), `sandbox {id, profile}` | An activity event is recorded, e.g. `activity.command.ran`, `activity.session.ended`, `activity.agent.turn_ended`, `activity.app.*` |
| ★ `conversation.turn.started`, `.ended`, `conversation.waiting` | `conversation`, `message` (turn: status, stop reason, usage, `output` for structured output), `sandbox` | A turn starts, ends, or waits on an approval or question. Richer than `activity.agent.*`: carries the turn's `output` and what it waits on, for workflows ([agents/conversations](../agents/conversations.md)) |
| ★ `sandbox.idle_warning` | `sandbox`, `stops_at`, `last_active_at` | `warn_before` before an idle stop; activity before `stops_at` keeps it ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop)). The stop itself is `sandbox.stopping` with `reason.code` `idle_timeout`, so there's no separate `sandbox.idle` |
| ★ `worktree.created`, `.removed` | `worktree`, `sandbox` | [agents/worktrees](../agents/worktrees.md) |
| ★ `template_build.ready`, `.failed` | `template_build` | A build finishes on a cluster |
| ★ `sign_in.completed`, `.failed`, `.expired` | `sign_in`, `profile` | An agent sign-in ends ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)) |
| ★ `git_connection.installation_added`, `.installation_removed`, `.installation_suspended`, `git_connection.failed` | `provider`, `profile`, `installation` or `connection` | [git/github-app](../git/github-app.md) |
| ★ `github.<event>` for any GitHub event the App receives (`github.pull_request`, `github.push`, `github.issue_comment`, …) | GitHub's payload, whole (or `payload_truncated` over 512 KB), with `delivery`, `event`, `action`, `repository`, `installation` and `profiles`. Raised only when an endpoint subscribes | M4, [github-webhooks](github-webhooks.md#forwarding) |
| ★ `profile.credits_low`, `.credits_exhausted`, `.credits_restored`, `.budget_warning`, `.budget_reached`, `.budget_reset`, `credits.renewed` | `profile`, `limit` or `credits` | Beta, [credits](../credits.md) |
| ★ `profile.suspended`, `.resumed` | `profile` | [profiles](../profiles.md) |
| ★ `node.online`, `.offline`, `cluster.health_changed` | `node` or `cluster` | [clusters](../clusters.md) |
| `webhook.test` | `endpoint` | Only on request, to that endpoint |

Each type is declared once in Go (`webhooks.Type` with its family, payload type and docs), from which the docs' event list and the OpenAPI `webhooks` section are generated. Core declares the sandbox, operation, activity, conversation, worktree, template, profile, node and cluster families; **plugins declare their own** through the events extension point ([plugins/extension-points](../plugins/extension-points.md)): `git_connection.*` and `github.<event>` by `git/github`, `sign_in.*` flows' outcomes by the agent plugins through core's sign-in machinery, `profile.credits_*`, `profile.budget_*` and `credits.renewed` by `credits`. A type whose plugin is off for a project is never raised for it, and an endpoint subscribed to it keeps the subscription.

### Subscriptions

An endpoint gets an event when it's enabled, the type matches one of its `events` (exact, or a prefix: `sandbox.*`, `activity.app.*`, `*`), and, when `profiles` is set, the event's profile is in it (events without a profile, like `node.*`, go to endpoints without a profile filter only).

`Raise` matches against an in-memory snapshot of the project's endpoints (loaded on first use, invalidated by `NOTIFY webhook_endpoints_<project>` when one changes), and **writes nothing** when no endpoint matches. Busy families (activity) cost nothing for projects that don't subscribe.

### Raising: the outbox

```go
webhooks.Raise(ctx, tx, project, profile, "sandbox.stopped", payload)
```

In the caller's transaction:

1. Match endpoints. None: return.
2. Insert `webhook_events`.
3. Insert one `webhook_deliveries` per endpoint, `pending`, `next_attempt_at = now`.
4. Insert one delivery job per delivery into the job queue **in the same transaction** ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)).

So an event exists if and only if its change committed, and every delivery has a job. No backstop sweep is needed for lost jobs; a lease on running jobs covers crashed replicas.

`status_sequence` is incremented in the sandbox row in the same statement that changes `status`, and copied into the payload.

### Delivering

A delivery job:

1. Load the delivery, its endpoint and event. Endpoint disabled → mark `failed` (`endpoint disabled`), done.
2. Per-endpoint concurrency: take one of the endpoint's `max_concurrency` slots (a Postgres advisory lock from a small pool per endpoint, or the job queue's per-key concurrency limit). None free → re-schedule in 1–5 s with jitter.
3. Resolve the URL's host; refuse disallowed addresses ([SSRF](#ssrf)); dial the checked address with the original host for TLS SNI and `Host`.
4. Sign and `POST` (below), 10 s timeout, no redirects, response body read up to 1 KB.
5. Record the attempt. `2xx`: `succeeded`, clear the endpoint's `failing_since`. Otherwise:
   - `410 Gone`: disable the endpoint (`gone`) and fail its pending deliveries.
   - Otherwise schedule the next attempt after the next delay (`60s, 5m, 30m, 2h, 6h, 12h, 24h`), with ±10% jitter; a `Retry-After` on `429`/`503` is honoured when it's longer, up to the next delay after that. Out of delays → `failed`.
   - Set the endpoint's `failing_since` if unset.
6. After a failure, if the endpoint's `failing_since` is more than 72 h ago and no delivery has succeeded since, disable it (`failing`), fail its pending deliveries, notify the project ([notifications](#notifications)).

### Signing

Standard Webhooks:

```
webhook-id:        whe_Lq8sV2nX4mKp9T
webhook-timestamp: 1761480000
webhook-signature: v1,<base64 HMAC-SHA256(secret, "{id}.{timestamp}.{body}")> [v1,<…previous secret…>]
content-type:      application/json
user-agent:        Lewbox-Webhooks/<version> (+https://<instance>/docs/webhooks)
```

`secret` is `whsec_` + base64 of 32 random bytes; the HMAC key is the decoded bytes. During a roll, both secrets sign, space-separated, until `previous_secret_expires_at`. Each attempt is signed afresh with its own timestamp.

### SSRF

On save and before every attempt:

- Scheme `https` (or `http` only when `LEWBOX_WEBHOOKS_ALLOW_HTTP`).
- No userinfo in the URL; port any.
- Every resolved address must be public unicast: refused are loopback, private (RFC 1918, ULA `fc00::/7`), link-local (incl. `169.254.169.254`), CGNAT `100.64.0.0/10`, multicast, broadcast, unspecified, documentation, benchmarking, `0.0.0.0/8`, IPv4-mapped IPv6 of those, and the instance's own addresses.
- The dialer connects only to the checked address (no second resolution).
- **Self-hosters**: `LEWBOX_WEBHOOKS_ALLOWED_PRIVATE_CIDRS` (e.g. `10.0.0.0/8`) allows chosen private ranges, for a homelab whose receiver is on the LAN. Off by default; the hosted instance never sets it.

### Secret roll, tests, redelivery

- `POST …/webhook-endpoints/{whk}/secret` → new secret, shown once; the old one signs too for 24 h.
- `POST …/{whk}/tests` → a `webhook.test` event to that endpoint only, whatever it's subscribed to; `202` with the delivery.
- `POST …/webhook-deliveries/{whd}/redeliveries` → a new delivery of the same event (same `webhook-id`), with its own retries; `202`. Allowed while the event is retained.

### Notifications

When Lewbox disables an endpoint, a notification goes to the project ([projects](../projects.md) owns notifications: shown in the dashboard's inbox and emailed to project admins). Re-enabling (`PATCH … enabled: true`) clears `disabled_reason` and `failing_since`; pending deliveries already failed stay failed (redeliver them).

### Stats

The dashboard's per-endpoint 7-day stats (attempts per day, error rate, mean and p95 response time) come from `webhook_delivery_attempts`, summarised by a query over the last 7 days' partitions (cheap with an index on (`endpoint_id`, `attempted_at`)), cached a minute.

## API

All under `/v0/projects/{prj}` (admin key or dashboard):

| Method and path | Does |
|---|---|
| `GET /webhook-endpoints` | List |
| `POST /webhook-endpoints` | `{url, events, profiles?, description?, enabled?}` → `201` with `secret` |
| `GET /webhook-endpoints/{whk}` | One, with `stats` |
| `PATCH /webhook-endpoints/{whk}` | Any of `url`, `events`, `profiles`, `description`, `enabled`, `max_concurrency` |
| `DELETE /webhook-endpoints/{whk}` | `204`; deliveries go with it |
| `POST /webhook-endpoints/{whk}/secret` | Roll |
| `POST /webhook-endpoints/{whk}/tests` | Test |
| `GET /webhook-endpoints/{whk}/deliveries` | `status`, `type`, `event` filters; newest first |
| `GET /webhook-deliveries/{whd}` | One, with `attempt_history` and `payload` |
| `POST /webhook-deliveries/{whd}/redeliveries` | Redeliver |
| `GET /webhook-event-types` | ★ The catalogue, for pickers |

Errors: `422 validation_failed` (`url` not https, resolving to a refused address, unknown event types or prefixes, unknown profiles), `409 limit_reached` (too many endpoints), `404 not_found`.

The SDKs ship a verifier (`Webhook.verify(secret(s), headers, body, tolerance = 5 min)`) and typed event classes ([sdks-and-cli](../sdks-and-cli.md)).

## Security

- Secrets encrypted at rest; shown only on create and roll.
- SSRF as above; the dialer is the only network path for deliveries.
- Payloads carry what the activity log carries: no command text, no output, no prompt text, no env, no secrets.
- Receivers are told (docs) to verify the signature and timestamp tolerance, drop duplicates by `webhook-id`, and drop stale statuses by `status_sequence`.

## Observability

- Metrics: `lewbox_webhook_events_total{type}`, `lewbox_webhook_deliveries_total{outcome}`, `lewbox_webhook_attempt_duration_seconds`, `lewbox_webhook_pending{endpoint}` (top N), `lewbox_webhook_endpoints_disabled_total{reason}`.
- Logs per attempt at debug; per failure at info with endpoint, status and error.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_WEBHOOKS_TIMEOUT` | `10s` | |
| `LEWBOX_WEBHOOKS_RETRY_DELAYS` | `1m,5m,30m,2h,6h,12h,24h` | |
| `LEWBOX_WEBHOOKS_DISABLE_AFTER` | `72h` | |
| `LEWBOX_WEBHOOKS_PREVIOUS_SECRET_TTL` | `24h` | |
| `LEWBOX_WEBHOOKS_RESPONSE_BODY_BYTES` | `1024` | |
| `LEWBOX_WEBHOOKS_MAX_ENDPOINTS` | `20` | Per project |
| `LEWBOX_WEBHOOKS_ALLOW_HTTP` | `false` | |
| `LEWBOX_WEBHOOKS_ALLOWED_PRIVATE_CIDRS` | empty | Self-hosting only |

## Testing

- Signature vectors from the Standard Webhooks spec; the TS and PHP verifiers tested against Go-signed payloads.
- Outbox: a rolled-back change raises nothing; a committed one always has deliveries and jobs.
- Retry schedule with a fake clock; `Retry-After`; `410`; disabling at 72 h; re-enable.
- SSRF: every refused range, DNS rebinding (a resolver answering public then private), pinned dialing.
- Per-endpoint concurrency under a slow receiver doesn't delay other endpoints.
- Redelivery keeps the id.

## Build plan

1. Tables (partitioned events and deliveries), `Raise`, endpoint snapshot cache.
2. Delivery job, signer, SSRF dialer.
3. Admin API and dashboard pages; stats.
4. Event families wired in as their features land: sandbox and operation (M1), activity (M4), conversations, worktrees, templates, sign-ins, GitHub (M3–M4), credits (Beta).
5. SDK verifiers.

## Open questions

1. **`api_version`** in the envelope (and pinned per endpoint), so payloads can evolve? Proposed: yes, set to the API version the endpoint was created with.
2. **`conversation.*` events**: are they needed alongside `activity.agent.turn_ended`, or should the activity event carry `output`? Proposed: separate family, since activity never carries content and `output` is content.
3. **Fan-out cost**: projects subscribing to `activity.*` with many sandboxes produce many deliveries. Batch deliveries (several events per POST) as an endpoint option, later?
4. **Self-hosted private delivery**: is a CIDR allow-list the right knob, or a per-endpoint "allow private" that only instance admins can set?
