---
title: Data map
description: What personal data Lewbox stores, where, for how long, who can see it, and how to export or delete it.
---

Lewbox is a single process with one SQLite database and one keyring file. It holds accounts, the workspaces they belong to, and the metadata of the sandboxes those workspaces run. It does not hold sandbox contents, and it never sends email.

This page is the data map an operator or auditor needs. The [security reference](../reference/security.md) covers how secrets are protected; this page covers what is kept and for how long.

## Tables

Every table the migrations create is listed, including tables with no personal data. The map is checked by a test (`internal/docs/data_map_test.go`), so a migration that adds a table without a row here fails the build.

| Table | Personal data | Why | Kept | Who sees it |
|---|---|---|---|---|
| `users` | Email address, password hash, account state | Sign-in and account administration | Until an instance admin removes the account | The account itself; instance admins (never the hash); co-members see the email in a workspace's member list |
| `sessions` | Account link, token hash, sign-in and last-use times | Keep a CLI or browser signed in | CLI 30 days; browser 7 days at most and 12 hours without a request. Expired rows are deleted by the daily prune | Never through the API. The database holds only a SHA-256 hash of the token |
| `access_tokens` | Account link, token name, prefix and last four characters, hash, use and expiry times | Long-lived `lbp_` tokens for scripts and CI | Until revoked or the account is removed; revoked rows stay for recognition | The account itself; instance admins see the metadata, never the token |
| `organizations` | Workspace name and slug, status and its reason | The customer tenant | Until the row is removed. Lewbox exposes no command that removes a workspace | Workspace members; instance admins |
| `memberships` | Account link, role, join time | Who belongs to a workspace and what they may do | Until the account or workspace is removed | Workspace members (through the member list); instance admins |
| `admin_keys` | Creator link, key name, prefix and last four characters, hash, use and expiry times | `lba_` management keys for a workspace | Until revoked or the workspace is removed; revoked rows stay | Workspace owners and admins; instance admins |
| `events` | Actor (account or key) and its email or name at the time, the target account's email, the client IP, the request ID, and field-level changes (never passwords, hashes, tokens or secrets). Failed sign-ins name the account only when it exists | The audit trail: who changed what, failed sign-ins and failed password confirmations | 90 days by default (`--audit-retention` / `LEWBOX_AUDIT_RETENTION`; `forever` keeps them), deleted by the daily prune. IPs are not shortened before then | Instance admins only (`lewbox events`, the console's Audit trail) |
| `sandbox_keys` | Creator link, key name, last four characters, upstream token ID | `avm_` keys a customer's backend uses against its cluster | Until revoked, expired, or its creator's account is removed; rows stay | Workspace members with key access; instance admins |
| `setup_links` | Creator and claimer links, free-text note, token hash, claim and use times | Single-use onboarding links | Unused links until revoked or expired; used and expired rows stay | Instance admins; the claimer sees the workspace it created |
| `plans` | None | Workspace limits | Until deleted; a subscribed plan is refused | Everyone (names and limits) |
| `settings` | None (instance name, sign-up mode, default plan and region) | Instance configuration | Until changed | Instance admins |
| `subscriptions` | None (workspace, region, plan, limit overrides) | A workspace's plan in its one region | Until the subscription is removed | Workspace members; instance admins |
| `clusters` | No account data. Domain, URLs and status. Cluster admin token sealed, last four characters shown | A region's AerolVM cluster | Until the cluster is removed; removal cascades its nodes, sandboxes, samples, archives, subscriptions, quota state and DNS records | Instance admins; customers see the region name, endpoint and runtime |
| `nodes` | None (name, URL, role, version, liveness) | A cluster's machines | Until the node or cluster is removed | Instance admins |
| `sandboxes` | Customer-supplied name and image, status and resource sizes | The sandbox mirror used for listing, metering and quotas | Until the cluster is removed. A destroyed sandbox stays marked gone | Workspace members; instance admins |
| `usage_hourly` | None (per-workspace and per-region counters) | Usage reports and limits | 400 days, then the daily prune deletes it | Workspace members (their own); instance admins |
| `quota_state` | None (workspace, region, last push and error) | What was last pushed to a cluster | Until the subscription or cluster is removed | Instance admins |
| `cluster_samples` | None (health, latency) | 30-day uptime charts | 31 days, then the daily prune deletes it | Instance admins; customers see their region's uptime |
| `node_samples` | None (capacity and liveness) | Node and cluster metrics | 31 days, then the daily prune deletes it | Instance admins |
| `archives` | Workspace link, sandbox name and image, customer-supplied tags, snapshot and restore metadata | Stopped sandboxes archived to a bucket | Until deleted. A deleted row stays marked deleted; its bucket repository is removed, but blobs shared with other archives are not. Removing a cluster drops the rows without removing their bucket repositories | Workspace members; instance admins |
| `dns_profiles` | ACME account email; Cloudflare API token sealed, last four characters shown | Create a cluster's DNS records and certificates | Until deleted; deletion is refused while a cluster uses the profile | Instance admins |
| `dns_records` | None (FQDN, type, content, Cloudflare record ID) | The records Lewbox created, so it deletes only its own | Until the cluster is removed or the check reconciles them | Instance admins |
| `storage_profiles` | Access key ID; secret access key sealed, last four characters shown | Archive buckets | Until deleted; deletion is refused while a cluster uses the profile | Instance admins |

`goose_db_version` is Goose's own migration bookkeeping and holds no application data.

### What is deliberately not stored

- Sandbox contents, files, environment variables and process output.
- The plaintext of any session, access token, admin key, sandbox key, setup link, cluster admin token, DNS token or storage secret. Only hashes or AES-256-GCM ciphertext are stored.
- Request bodies, query strings, cookies and `Authorization` headers in logs. One access line holds the method, route pattern, status, duration and request ID.
- IP addresses. The sign-in rate limiter keeps them in memory only, for a minute.
- Email addresses elsewhere: there is no mail queue, no address book and no email change history. Lewbox cannot send email at all.

## Processors and copies

| Processor or copy | What it receives |
|---|---|
| The VPS or machine running the orchestrator | The database, keyring, container logs and backups |
| A customer's S3-compatible bucket | Archived sandbox images and their metadata. The customer controls the bucket and its retention |
| Cloudflare | The DNS records Lewbox creates for a cluster: FQDNs and node IPs, with the operator's token |
| GitHub releases | The version check and release download. No account data is sent |

There are no analytics, trackers or third-party scripts in the console, and one strictly necessary cookie: the session cookie (`__Host-lewbox_session`, or `lewbox_session` over plain HTTP). It is `HttpOnly`, `SameSite=Strict`, lives 7 days at most and 12 hours without a request, and carries no personal data beyond the session token.

## Retention jobs

One prune runs from the cluster-check loop, at most once a day, and deletes:

- monitoring samples older than **31 days** (`cluster_samples`, `node_samples`);
- expired sessions (`sessions.expires_at < now`);
- audit events older than the audit retention (90 days by default);
- usage rows older than **400 days** (`usage_hourly`).

Everything else is kept until an operator deletes it. That includes revoked tokens and keys, setup links, destroyed sandboxes, and archives marked deleted. There is no separate `prune` command and no way to pause the job; copy the database before it runs if you need older samples as evidence.

An archive's bucket repository is deleted with it, apart from blobs shared with other archives. A cluster's nodes may keep a local copy for `archive_local_retention` (`instant`, `1h`, `1d`, `7d` or `never`) before their own cleanup timer removes it.

## Export

Lewbox has no self-service data export and no `export` command. An instance admin can read what the API returns:

```bash
lewbox user list --json
lewbox org list --json
lewbox org get SLUG --json
lewbox member list --org SLUG --json
lewbox token list --user EMAIL --json
lewbox admin-key list --org SLUG --json
lewbox key list --org SLUG --json
lewbox sandbox list --all --json
lewbox usage --json
```

For a subject-access request, collect those outputs for the account and its workspaces, plus the workspace's archives from `lewbox snapshot list --json`. Do not include password hashes, token hashes or sealed values; the API never returns them. There is no field-level allowlist, so review a JSON dump before handing it over.

## Deletion

```bash
lewbox user remove ID|EMAIL --yes
lewbox key revoke ID|NAME --org SLUG --yes
lewbox admin-key revoke ID|NAME --org SLUG --yes
lewbox token revoke ID|NAME --user EMAIL --yes
lewbox setup-link revoke ID --yes
lewbox cluster remove SLUG --yes     # a region and everything under it
lewbox dns-profile delete NAME --yes
lewbox storage-profile delete NAME --yes
```

Removing an account:

- deletes its sessions, access tokens and workspace memberships;
- revokes the live sandbox keys it created, then deletes the account;
- sets its creator and claimer links on admin keys, sandbox keys and setup links to null, so those records survive;
- is refused for the **last enabled instance admin**.

It does **not** delete workspaces the account owned, and the API exposes no way to delete a workspace. A workspace can be suspended (`lewbox org suspend SLUG`), which blocks its keys and stops its sandboxes through the sync loop, but its name, members and records remain. Removing a cluster deletes that cluster's nodes, sandboxes, archives, samples, DNS records and subscriptions in one cascade. `usage_hourly` has no foreign key to a cluster and survives it until its 400-day prune.

Backups retain deleted data until they rotate out: the VPS setup keeps **14 daily** backups and the **last 5 pre-deploy** ones. Copying backups off the server is the operator's job, so an off-site copy can keep data longer.

## Known gaps

- No self-service export or account deletion; both are operator actions.
- No workspace deletion command, so a workspace and its history outlive its last member.
- No audit or event log, and no per-user activity history. Access logs name the route but not the account or IP, so incident scoping needs the database.
- Revoked tokens, keys and setup links, destroyed sandboxes, and archives marked deleted are kept forever.
- Removing a cluster drops its archive rows but leaves their snapshot repositories in the bucket; delete archives first if the bucket must be cleaned.
- The keyring has no key IDs and no rotation. Losing it makes every sealed value unreadable; leaking it together with the database exposes every sealed value.
- Backups are local unless the operator copies them elsewhere, and a restored backup brings back accounts deleted since it was taken.
- There is no notification channel, so a privacy notice or breach notice must be sent outside Lewbox.
