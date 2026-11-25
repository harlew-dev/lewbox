---
title: Audit trail
description: Every change Lewbox makes, who made it, what changed and from where, and how long it is kept.
---

Lewbox records every state change made through the service in an audit trail: accounts, passwords, access tokens, organization API keys and sandbox keys, setup links, workspaces, members, plans and instance settings. A refused change records nothing. The trail is written in the same database transaction as the change, so a change that fails leaves no event, and an event that cannot be written rolls the change back.

Changes to clusters, nodes, DNS and storage profiles, subscriptions, and sandbox, snapshot and archive actions are not in the trail yet; they are the next step.

Each event carries:

| Field | |
|---|---|
| `created_at` | When, in UTC |
| `actor_kind` | `user`, `access_token`, `admin_key`, `local`, `system` or `anonymous` (a failed sign-in) |
| `actor_id`, `actor_label` | The account or key that acted, and the email or key name at the time |
| `name` | What happened, `<resource>.<past-tense fact>`, such as `plan.updated` or `user.suspended` |
| `target_type`, `target_id`, `target_name` | What it happened to, by public ID or name |
| `organization_id` | The workspace, when the change belongs to one |
| `changes` | For updates, `{field: {from, to}}` for the fields that actually changed |
| `ip` | The client address, for events made through the API |
| `request_id` | The server's request ID, matching the `X-Request-ID` header and the logs |

Passwords, password hashes, session tokens, access tokens, API keys, setup-link tokens, profile secrets and join tokens are never recorded. A deleted account's email stays on its events as the label of who acted, which is why the trail outlives the account.

## Event names

| Resource | Events |
|---|---|
| Account | `user.created`, `user.deleted`, `user.suspended`, `user.restored`, `user.password_changed`, `user.fresh_proof_failed` |
| Sign-in | `session.login_failed` |
| Credentials | `access_token.created`, `access_token.revoked`, `admin_key.created`, `admin_key.revoked`, `sandbox_key.created`, `sandbox_key.revoked` |
| Setup links | `setup_link.created`, `setup_link.revoked` |
| Workspace | `workspace.created`, `workspace.updated`, `workspace.suspended`, `workspace.restored` |
| Members | `member.created`, `member.updated`, `member.deleted` |
| Plans and settings | `plan.created`, `plan.updated`, `plan.deleted`, `settings.updated` |

## See it

Instance admins only:

- **Console**: **Audit trail** under **Manage**. Filter by actor, workspace, action and time, and load more pages.
- **CLI**: `lewbox events`, with the same filters:

  ```bash
  lewbox events --action plan.updated --limit 20
  lewbox events --workspace acme --from 2026-11-01T00:00:00Z
  lewbox events --actor you@example.com
  ```

- **API**: `GET /v0/events`, [documented here](../reference/http-api.md#audit-trail).

The list is newest first and cursor-paged: pass `before` (the `next` value of the previous page) to continue. A cursor from another filter, or one whose event has been pruned, answers `400` and the page starts again from the first.

## What is not recorded

- **Reads.** Listing or viewing records is not recorded. The exception is reading a DNS or storage profile's credentials, which records `dns_profile.credentials_read` / `storage_profile.credentials_read`.
- **Failed sign-ins** are recorded (`session.login_failed`), for every attempt, known account or not, with the account when it exists. A wrong or missing current password on a sensitive action records `user.fresh_proof_failed`.
- **Rate-limit refusals** are not recorded yet.
- **Background checks and samples** are not events; their results are already on the cluster and node pages. A workspace suspended or restored by the background sync will be a `system` event when the next step lands.

## Retention

Events are kept for **90 days** by default. Change it with `lewbox serve --audit-retention`, or `LEWBOX_AUDIT_RETENTION`:

```bash
lewbox serve --audit-retention 180d
lewbox serve --audit-retention forever
```

A duration such as `12h` or `90d` works, and `0` or `forever` keeps every event. The daily prune deletes a batch at a time, so a large trail is removed without holding the database.

## Known gap

Only the client IP is minimised by deletion, not by truncation: it is kept as recorded until the event is pruned. Address truncation (IPv4 `/24`, IPv6 `/48`) at 30 days is planned, along with keyed pseudonyms for account emails in old events.
