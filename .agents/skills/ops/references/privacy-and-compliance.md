# Privacy and compliance

No software makes an organisation pass GDPR, SOC 2 or ISO 27001. Those also cover contracts, policies, training, risk assessments and an auditor's judgement. What the code owes the organisation is:

- **technical controls**;
- **evidence** that they run;
- an honest list of **known gaps**.

Build those three. Never claim more.

## The data map, enforced by a test

Keep a table in the docs (e.g. `docs/compliance.md`) with **one row per database table**:

| Column | Example (`users`) |
| --- | --- |
| Table | `users` |
| Personal data | name, email, password hash, created, email verified, last seen (to the hour) |
| Why (lawful basis) | the account (contract) |
| Kept | until the account is deleted |
| Who sees it | the person; members of their tenants (name, email); admins (never the hash) |

- **A test MUST fail** when a migration adds a table the map doesn't name, or the map names a table that no longer exists. Parse the markdown table and compare it with `information_schema.tables`. That's the only way the map stays true.
- Tables with no personal data still get a row ("nobody", "until the window ends"). Rate-limit counters keyed by a hash, for example, are "nobody can see it".
- Also list, outside the table:
  - **processors:** hosting (servers, database, backups, logs), SMTP provider, CDN/WAF, CAPTCHA provider, alert webhook/heartbeat services (which receive no personal data);
  - **cookies:** see below;
  - **logs:** what an access line holds;
  - **backups:** rotation, and that erased data leaves only as backups expire.
- **No third-party scripts, analytics or trackers**, apart from the CAPTCHA provider, which loads only when a challenge is needed. The CSP allows only the app's own scripts.

## Tiered retention with registered prune jobs and evidence

Every age in the schedule is a **registered prune target**: a named job in one registry, so running `prune` with no target lists the whole schedule and nothing ages out by accident or not at all.

| Data | Kept | How |
| --- | --- | --- |
| **Audit events** (`auth.*`, `admin.*`, `server.*`, `prune.*`, `alert.*`) | **1 year** | `prune audit_events` |
| Other events (tenant, project, `rate_limit.*`, `email.*`) | 90 days | `prune events` |
| IPs in events | full for 30 days, then cut to /24 or /48 | `prune pseudonymise` |
| Emails in events (attempts, resets, invitations, admin searches) | readable 30 days, then a keyed pseudonym | `prune pseudonymise` |
| Grouped errors | 30 days after last seen | `prune app_errors` |
| Resolved alerts and their cycles | 1 year after resolution; open ones kept | `prune alerts` |
| Metrics per minute / per hour | 48 h (the rollup deletes) / 90 days | rollup / `prune *_hours` |
| Daily table sizes | 400 days (year-on-year comparison) | `prune table_stats_daily` |
| Sessions, limits, links, codes, challenges, pending sign-ups, IP blocks | until they end | the server's 15-minute cleanup |
| Accounts, tenants, user content | until the owner deletes them | self-service, or the `erase` CLI |
| Server logs | **30 days** | the log store |
| Audit log lines | **1 year** | an immutable log store |
| Backups | provider rotation, 30 days recommended | the operator |

- **Evidence:** each confirmed run records `prune.completed` (`target`, `deleted`, `kept`, `cutoff`), itself an audit event kept a year. This is what you show an auditor. A preview or cancelled run records nothing.
- **Monitoring:** for each target, show its oldest prunable row and its last run. Warn when a target is a week past its age or hasn't run in two days (see `monitoring-and-alerting.md`).
- **Scheduling:** daily from cron, each target separately, with the production environment (pseudonymising needs the master key). A failing run exits non-zero; have cron alert on it.
- **Never delete rows a pending job still needs.** Keep events with a pending delivery, since a listener may need the address to send its email.
- **The prune command's contract** (a typo here deletes a table, so it is strict):
  - `prune <target>` previews; only `--confirm` deletes. A **bare `prune` previews every target at its default age and never deletes, even with `--confirm`**: confirming needs a named target.
  - `--older-than` accepts only `<n>d` or `<n>h`, **at least 1 hour** (so `0d` can't wipe a table), and only together with a target; a run over every target uses each target's default age.
  - An unknown target, a bad age or a database error **exits non-zero**, and the usage lists the targets.
  - One fixed output line per target, the same in preview and confirm: `events: would delete 12408 older than 2026-08-01 00:00 UTC (90d); kept 3 with pending deliveries`.
  - The target registry **panics at startup on a duplicate name or a default age under the floor**, and lists targets sorted. Adding a target is two files: its count/delete query and its registration.
- **Inactive accounts:** list them (e.g. not seen for 2 years) for a human decision. Deleting them is a business policy, never automatic.
- **Track `last_seen` cheaply:** update it in the same statement as the session refresh, at most once an hour, with `SKIP LOCKED`, and without writing an event. A migration adding it needs a backfill, or every existing user looks inactive.

## Minimisation

- **Store counts, not names**, wherever a count answers the question.
- **Truncate IPs** after 30 days to IPv4 /24 or IPv6 /48. That keeps "which network" for abuse analysis without identifying a household. Also cut IPs inside payloads (searched IPs, an admin session's moved-from networks).
- **Pseudonymise emails with a keyed HMAC, not a plain hash:**

  ```
  pseudonym = "hmac:" + hex(first 16 bytes of HMAC-SHA256(k_pseudonym, lower(trim(email))))
  k_pseudonym = HKDF(master_key, info = "<app> email-pseudonyms v1")
  ```

  - A plain SHA-256 of an email can be reversed by hashing a list of guesses. A keyed HMAC can't, without the key.
  - With the key, an address can be **confirmed**, never read back. The admin search pseudonymises the typed query, so old rows still match, and "how many attempts against this address" still works.
- **Run it idempotently:**
  - set `minimised_at` and never look at the row again;
  - use batches of 1,000 with `FOR UPDATE SKIP LOCKED`, oldest first, through a partial index on rows not yet minimised;
  - skip events whose listener hasn't run.
  - A retry of a delivery must lock the event row first, so minimisation can't happen between the check and the retry.
- **Rate-limit keys are hashed** (see `rate-limits-and-quotas.md`). Error text is scrubbed, and access lines carry route patterns, not paths (see `observability.md`).
- Record a typed email only once it parses as an address. People sometimes type their password into the email field.

## Erasure

**Self-service "delete account"** (password-confirmed). In **one transaction**, delete:

- the account, its sessions, memberships, MFA, reset links, pending changes and admin row;
- events *about* it (the `user_id` foreign key cascades).

In the same transaction:

- null it as the actor of other people's events;
- **redact its email** (and the name beside it) to `"[deleted]"` in surviving events: `email`, `previousEmail`, exact admin searches for it. Use **indexed** expression updates, and test that each query plan uses its index;
- delete tenants only it belonged to.

Lock the account first, then owned tenants in ID order. **Refuse** when the user is the only owner of a tenant with other members ("make someone else an owner first"), and refuse for an admin (revoke first).

- **Former email addresses** (after email changes): erase them only up to their release time, so a later owner of the same address keeps their data. Delete events by account ID, never by email equality.
- **Staff-handled erasure** for someone who can't sign in is a **CLI command, previewing without `--confirm`**, after verifying who is asking **outside the app**. It records `admin.user.erased` with no actor.

### Known erasure gaps (write yours down)

- **Backups and shipped logs** keep data until they expire. Say so in the privacy notice.
- **Audit log lines** never held the email; they keep the public user ID and a truncated IP for their year.
- **An invitation to the address** sent by someone else stays with that tenant until it expires.
- **The suppression list** keeps addresses that asked for no mail. That's a legal obligation, and it must outlive everything else.
- **Partial searches** an admin typed ("ada@") and **free-text reasons** naming someone aren't found by exact-match erasure. They leave with the 1-year audit retention.
- **User agents** aren't minimised.

## Export ("download my data", GDPR art. 15 and 20)

- Password-confirmed, rate limited (`export:per-user`, 5 a day, counted only once the password is right), and recorded (`auth.data_exported`).
- **Synchronous JSON**, as long as quotas bound the size. Then there's nothing stored or emailed, and no export files to secure or expire.
- **Field allowlists:** each section is its own struct or serializer listing its fields. A new column is then **never exported by accident**.

  ```
  account:     id, name, email, createdAt, emailVerifiedAt, lastSeenAt, mfaEnabled
  sessions:    createdAt, lastSeenAt, expiresAt                 (never a token)
  memberships: tenant, tenantName, role, joinedAt
  owned:       tenants with their projects and the invitations THIS account sent
  events:      about the account (user_id) or caused by it (actor_id), newest first, ≤ 10,000,
               with an "eventsTruncated" flag; IP/user agent only on events it caused
  ```

- **Never exported:**
  - password hash, MFA secret, recovery codes, tokens, digests, CSRF tokens, internal IDs;
  - other people's data beyond what the account itself typed or did;
  - **anything done in the admin console** (flag events `admin_request` in the admin middleware, so new admin actions are excluded automatically);
  - operator notes;
  - **address-only events** such as reset or sign-up requests for the address. Anyone can type an address, so the IP on those could be an attacker's;
  - **admin searches for the address**. Exporting them would tip off someone under investigation. Whether to disclose them is the organisation's call under art. 15(4) / 23.
- **Test it:** walk the JSON for forbidden key names and search it for the actual secret values and the co-member's values.
- **A staff-run export** uses the same file, through a CLI, recorded as `admin.user.exported`. Send it only through a channel you've verified belongs to the person.

## Notifications: opt-in only

- Classify every email (see `email-delivery.md`). **Notifications about others' actions** (a project deleted in a tenant) are opt-in: off by default, and only the user can turn them on. Otherwise someone adds a person to 20 projects and deletes them all, sending 20 emails nobody asked for.
- Alert emails to admins are also opt-in per signal, with no default recipients.
- Mail that can reach someone who never signed up (invitations) carries a **block link** and `List-Unsubscribe`. That is GDPR art. 21, objection.

## Cookies: no consent banner when all are strictly necessary

Use only cookies that are strictly necessary for a service the user asked for:

- the session (CSRF goes in a header, not a cookie);
- a sign-in's MFA challenge;
- a pending sign-up;
- the admin session;
- the admin assets pass.

With no analytics, ads or cross-site tracking, ePrivacy/PECR need **no consent banner**. Use `__Host-` (or `__Secure-`) prefixes in production. **The moment you add analytics or marketing cookies, you need consent first.** That's a product decision, not a quiet dependency bump.

## Database roles

Two roles:

- a **migration role** that owns the schema and runs migrations;
- a **runtime role** for servers, cron jobs and the admin CLI, with `SELECT, INSERT, UPDATE, DELETE` only: no DDL, no `TRUNCATE`, not the owner, not superuser.

A compromised server then can't drop the audit table or change the schema (SQL in `deployment.md`). The events table can't be insert-only, because erasure and minimisation update it and prune deletes from it. **The shipped audit lines are the tamper evidence instead.**

## Audit lines: tamper evidence without a hash chain

- Write every audit event as a log line too, in the event's transaction: `msg: "audit"`, event ID, name, actor/user/tenant public IDs, **IP cut to /24 or /48**, time, request ID. **Never the payload**, so never an email or a name.
- Route those lines to **storage nobody on the server can change** (S3 Object Lock, a SIEM, a provider's immutable archive) and keep them **a year**. Keep everything else 30 days.
- A row altered or deleted in the database then disagrees with a record nobody can change. A line whose row is missing was either rolled back (its request's access line shows an error) or deleted since.
- **No hash chain:** it fights pruning, minimisation and erasure, which rewrite rows on purpose.
- Keep `LOG_LEVEL` at `info`, or audit lines aren't written. Warn at `error` level at startup if it's higher.

## Admin event CSV export

- Any admin may download the events page's filtered rows as CSV (it's a read): newest first, at most 10,000 rows with `X-Export-Truncated: true`.
- **Defuse formula cells:** prefix with `'` any cell starting (after spaces) with `=`, `+`, `-`, `@`, a tab or CR.
- Record `admin.event.exported` with the filters and row count **before** sending a byte.

## Control mapping (high level)

Guidance for an auditor's questions, not a certification.

| Requirement | Technical control |
| --- | --- |
| GDPR art. 5(1)(c) minimisation, art. 25 privacy by design | counts over names; IP/email minimisation at 30 days; no third-party scripts; scrubbed errors; route-only access lines |
| GDPR art. 5(1)(e) storage limitation | retention schedule as prune targets, evidenced by `prune.completed` |
| GDPR art. 15, 20 access and portability | download my data; staff export CLI |
| GDPR art. 16 rectification | self-service name and email change |
| GDPR art. 17 erasure | account deletion with email redaction; staff erase CLI |
| GDPR art. 21 objection (email) | block link and suppression list |
| GDPR art. 30 records of processing | the data map |
| GDPR art. 32 security | TLS, strong password hashing, MFA (required for admins), secrets sealed at rest, rate limits, CSP, monitoring |
| GDPR art. 33, 34 breach notification | incident runbook; event filters and CSV for scoping (see `incident-response.md`) |
| SOC 2 CC6.1 / ISO 27001 A.8.5 secure authentication | passwords, MFA, session expiry, step-up for admin actions |
| SOC 2 CC6.2, CC6.3 / ISO A.5.18, A.8.2 provisioning and privileged access | admins granted only via CLI, roles, admin list CSV for access reviews, stale-admin warnings |
| SOC 2 CC7.2 / ISO A.8.15, A.8.16 logging and monitoring | audit trail, audited admin reads, structured logs with request IDs, immutable audit lines, monitoring and alerts |
| SOC 2 CC7.3, CC7.4 / ISO A.5.24–A.5.26 incident handling | runbook; suspension, IP blocks, session revocation |
| SOC 2 CC8.1 change management | `server.started` with version and config summary; mixed-version detection; reviewed migrations |
| ISO A.8.10 deletion, A.8.11 masking | erasure, retention, minimisation |

HIPAA (health data) and PCI DSS (card data) are out of scope unless you hold that data. If you do, they are a separate project.

## Known gaps: write them down

Keep a "Known gaps" section, so nobody claims otherwise. Typical entries:

- the master key can't be rotated yet;
- no SSO or SCIM;
- a tenant's own activity log shows changes only;
- erasure misses partial admin searches and free-text reasons;
- user agents aren't minimised;
- backups, disaster recovery, data residency and encryption at rest depend on the hosting provider;
- contracts and policies (DPAs, sub-processor list, privacy notice, security policies, breach register) are the organisation's.
