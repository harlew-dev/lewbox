# Activity and webhooks

> Status: Draft · Milestone: Alpha (M4) · Core · Updated: 2026-10-29

Two ways Lewbox tells people what happened. The **activity log** is a sandbox's history, kept for a while and read on demand: its operations, what was done inside it (never the text of a command or its output), requests refused, agents prompted, and events the project's own app adds. **Webhooks** push events to the project's HTTPS endpoints as they happen, signed, retried and at least once: sandboxes changing status, operations finishing, activity, and, through the project's GitHub App, what happens on GitHub.

They share one pipeline: whatever is recorded in the activity log, and every status change, can become a webhook event, raised in the **same transaction** as the change.

| Plan | What it covers | Milestone |
|---|---|---|
| [activity-and-webhooks/activity-log](activity-and-webhooks/activity-log.md) | Event kinds and groups, actors, outcomes, what's never kept and how it's redacted, app events, reading and filtering, live following, retention, the writer | M4 (Alpha) |
| [activity-and-webhooks/webhooks](activity-and-webhooks/webhooks.md) | Endpoints, the event catalogue and payloads, the outbox, signing (Standard Webhooks), retries, disabling, SSRF protection, secret rolls, tests, deliveries, redelivery, notifications | M4 (Alpha) |
| [activity-and-webhooks/github-webhooks](activity-and-webhooks/github-webhooks.md) | Inbound webhooks from each project's GitHub App: installations kept current, and every other event forwarded whole to the project as `github.<event>` | M3 for installations, M4 for forwarding |

---

## Today

- **Activity** (`lewbox-legacy` `sandbox_events`, `app/Enums/Sandbox/SandboxEventKind.php`; consumer API on the gateway, `internal/server/activity.go`, `activitylog.go`; docs at [lewbox.com/docs/sandboxes/activity](https://lewbox.com/docs/sandboxes/activity)). The app records operations and what it sees; the **gateway records what only it sees** (commands, terminals, files, port links, refused requests) into a bounded in-memory queue, flushed every 2 seconds in batches of up to 500 to `POST /api/gateway/v1/sandbox-events`, **dropped when the queue is full** (`internal/events/recorder.go`). Retention 30 days (successful commands 3), at most 90, pruned daily.
- **Webhooks** (`app/Actions/Webhooks/*`, `docs/admin/webhooks/*`, built 2026-10-26, Lewbox `d283c22`, `287690b`, `abdd652`): `webhook_endpoints`, `webhook_events`, `webhook_deliveries`, `webhook_delivery_attempts`; events written in the change's transaction, delivery jobs queued after commit, a minute backstop (`webhooks:send-due`); Standard Webhooks signatures; retries over ~2 days; disabled after 72 h failing; SSRF checks with the host pinned; secrets rolled with 24 h overlap; tests; redelivery; per-endpoint 7-day stats in the dashboard.
- **Inbound GitHub** (`app/Actions/GitHub/HandleGitHubAppWebhook.php`, `POST /api/github/v1/apps/{tenant}/webhooks`): `installation`, `installation_repositories`, `github_app_authorization`. Nothing forwarded to tenants.
- **Lewagent** receives them at `POST /webhooks/lewbox` (`lewagent-legacy/app/Http/Controllers/Webhooks/LewboxWebhookController.php`), verifies with the PHP SDK, drops duplicates by event id cached 3 days, queues `HandleLewboxWebhook`, and orders sandbox statuses by `status_sequence`. It still polls every minute as a backstop (`lewagent:refresh-computers`).

What's wrong:

1. Activity the gateway saw **can be lost** (dropped on a full queue, or with a crashed replica's queue), so the webhooks raised from it are lost too.
2. Two writers (app and gateway) with two shapes for one event.
3. No live stream of a sandbox's activity (`lewbox-legacy/docs/todo.md` line 260); dashboards poll with `since`.
4. No per-profile webhook filters (`docs/todo.md` line 258); a project with many customers gets every customer's events at one URL.
5. Stored-only notifications (`tenant_notifications`) that nothing shows.

## Goals

- One writer, in-process: an activity event and the webhook events it raises commit together, or not at all.
- No silent drops. Under pressure, the writer applies backpressure to the busiest kinds (successful commands) before anything else, and counts what it sheds.
- Live following of a sandbox's activity (SSE).
- Webhooks: same guarantees and wire format as legacy (Standard Webhooks, `status_sequence`, reasons), plus per-profile filters, new event families, and a dashboard that shows notifications.
- GitHub's events forwarded to projects.

### Non-goals

- A general audit log of project administration (who changed a rate, who made a key). That's [projects](projects.md)' audit log, which may share the writer.
- Exactly-once delivery.

## How the pieces fit

```
 request / operation / reconciler / agent upload / GitHub webhook
        │
        ▼
 activity.Record(ctx, tx, event)          webhooks.Raise(ctx, tx, type, data)
        │  (same tx as the change)               │
        ▼                                        ▼
 activity_events (partitioned)     ┌─ webhook_events  (only if some endpoint wants it)
        │                          └─ webhook_deliveries + delivery jobs (same tx)
        │ NOTIFY activity:<sbx>                  │
        ▼                                        ▼
 SSE followers (any replica)        delivery workers ─▶ project endpoints (signed)
```

`activity.Record` raises `activity.<kind>` through `webhooks.Raise` in the same transaction. Sandbox status changes raise `sandbox.<status>` from the sandbox's state transition code; operations raise `operation.*`. High-volume events recorded outside a request's transaction (successful commands, file writes) go through the [batching writer](activity-and-webhooks/activity-log.md#the-writer), which writes activity and webhook events together in its batch transaction.

## Open questions

1. **Notifications**: a project inbox in the dashboard (webhook endpoint disabled, certificate expiring, budget warnings) — here, or in [projects](projects.md)? Proposed: [projects](projects.md) owns notifications; webhooks and activity raise them.
2. **Event payload versions**: do webhook payloads carry a version (`"api_version": "2026-12-01"`) so the API can change without breaking receivers?
3. **Activity on the profile and project level** (connections, keys, settings), beside sandbox activity: one log with a `scope`, or separate logs?
