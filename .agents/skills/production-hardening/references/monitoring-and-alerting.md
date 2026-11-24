# Monitoring and alerting

Framing:

- **A dashboard can't report its own outage.** It runs inside the app it watches. "Is the app up?" needs an **external uptime check**. "Is the alert loop alive?" needs a **heartbeat** (dead man's switch). Plan both from day one.
- **A dashboard only helps when someone looks at it.** A signal that stays bad must open an alert that is pushed out (email, webhook) within minutes.
- **Anyone with an account can raise a level on purpose:** fail their own passwords, hit limits, send junk. A level means "something is happening", not "an attack". Alerting must survive deliberate noise, whether it's meant to cry wolf or to hide a real attack in the flood. That is why the damping and caps below exist.
- **Compute on read, don't store signals.** Signals are bounded aggregate queries. When one gets too slow at real volume, move it to an hourly rollup table. Don't raise its timeout.

## Define each signal once

The dashboard tile, the alert rule and the docs all read the **same definition**, so the page and the alert can never disagree.

```
signal "http.5xx_share" {
  label:      "Server errors"
  unit:       percent            # count | percent | ms | minutes | per_minute | bytes
  window:     1h                 # the signal's own window: how things stand now
  warn:       1                  # 0 = this level doesn't exist
  critical:   5
  min_sample: 100                # below it the level is "ok", whatever the value
  measure:    one bounded, indexed query
  drill:      { viewer: events, names: [...], window: same length, exact: true }
}
```

- **Level rule:** a value **at or over** a threshold is at that level, and the higher level wins. Below `min_sample` the level is `ok`, because a share of a handful of requests means nothing (one failure in two requests is not a 50 % error rate). Typical minimum samples: 100 requests for 5xx share, 20 for p95, 50 attempts for "unknown user" share.
- **Drill-down:** every signal links to a filter on the raw-event viewer for the rows behind the number. Flag it as `exact` (lists exactly the rows counted) or `superset`. Floor the signal's start to the viewer's bucket so the drill-down covers everything counted.
- **Response shape:** `{name, label, unit, value, sample, level, warn, critical, minSample, windowSeconds, drill, unavailable?}`.
- **Stable names** (`area.metric`, e.g. `auth.failed_sign_ins`). Alert rules, subscriptions and docs key on them, so renaming one breaks subscriptions.
- **Governance:**
  - change thresholds only through a reviewed commit, with the docs table updated alongside;
  - size starting thresholds from your rate limits, and test that the limits they read exist;
  - a well-formedness test checks unique names, drill-downs naming real event names and allowed windows, and `warn < critical`;
  - unit-test the level function: at-or-over, higher-wins, under-min-sample.
- **Percentiles** come from summed histogram buckets with linear interpolation. Running totals give only an average, so say which one you show.
- **Metrics off ≠ zero.** If metric storage is off, show "metric storage is off", not 0. Zero means "no traffic". In a mixed fleet, say which servers aren't counted.

## Per-section time budgets with rollups

A monitoring page must stay fast and must never let one slow query hide everything:

- **One endpoint per page**, `?window=1h|24h|7d` (default 24h). Reject unknown parameters (400) and unknown pages (404).
- **Concurrency:** one page load in flight per admin and two per server. Wait at most 1 s for a slot, then 503 "busy" with `Retry-After: 1`.
- **Budgets:** 5 s for the whole page and **2 s for each signal or section**. A part that misses its budget comes back `unavailable: true` (or in an `unavailable` list) while the rest of the page answers. Return 503 ("try a shorter window") only when nothing at all could be read.
- **Run the costliest reads last** (integrity checks after the security tiles). Within a batch, run the cheapest first. If time runs out, return `partial: true` with how many were read. **A check that wasn't read is never shown as OK.**
- **Memoise per page load,** so a check needed by several sections runs once.
- **Query rules:**
  - every query has a window of at most 7 days and uses an index;
  - review lists return 20 rows (top-N 10);
  - **count up to a cap** ("1,000+"), so a huge tenant costs no more than the cap;
  - avoid quadratic comparisons. Compare a sign-in against the user's last 50 sign-ins, not all of them; that once cut a 6.2 s query to within budget.
- **Chart buckets:** 1h → 1-minute, 24h → 30-minute, 7d → 3-hour buckets. List every bucket, empty ones as zero, with windows starting on a bucket boundary. A 7-day read uses the hourly rollup plus the minutes after it, never counting anything twice.
- **Volume-test:** seed about 1M events and record each page's served time per window. Document the pages that miss their budget and need a rollup.
- **No auto-refresh** on admin pages, since every load is an audited read.

## Alert rules

Evaluated every minute by **one** server (see "Leader election" below). Integrity rules run hourly.

| Field | Purpose |
| --- | --- |
| `signal` | the shared definition |
| `from` / `cap` | narrow the levels that alert: critical only; a panic signal capped at warn; a noisy signal's critical alerting only as a warning |
| `for` (hold) | the level must persist this long before an alert opens; a reading back at ok restarts the timer. 5xx share 5 min; p95 latency 10 min; security, integrity and stalled workers "at once" |
| `meaning` | one sentence: what it usually means and where to look. This is an inline runbook. Notifications carry no links |

A test checks every rule names a real signal, has a level to alert at, has a meaning, and isn't duplicated.

## Alert lifecycle

One stable alert row per signal. Each opening is an **incident cycle** with its own acknowledgement and comments.

| Transition | When | Notifies |
| --- | --- | --- |
| **opened** | the level holds for `for` | yes (subject to damping) |
| **escalated** | warn rises to critical **in place**. No second alert | once more |
| **recovered** | **confirmed** health: minute rules need 11 distinct healthy checks spanning 10 minutes; hourly rules need their first healthy reading. The alert **stays open** | yes |
| **reopened (relapse)** | bad again after a confirmed recovery. Same cycle; acknowledgement and reminder clock reset | yes |
| **resolved** | **only an operator**, only while recovery is confirmed and fresh | yes |
| **reopened (recurrence)** | bad for `for` after resolution. A new cycle on the same alert | yes |
| **reminded** | an unacknowledged, unsilenced **critical** alert still open: every 4 h. Warnings are never reminded | yes |

- **Recovery is evidence, never a decision. NEVER auto-resolve.** A human resolves, picking a comment from the cycle as the resolution note and passing the revision they saw. A stale revision gets 409 "reload", so a failure the operator didn't see wins.
- "Healthy" means below **this alert's** trigger level. An unavailable reading, or a gap over 2 minutes (65 minutes for hourly rules), restarts the healthy run. A check counts once per scheduled bucket, so two evaluators can't inflate it. Use the **database clock**.
- **Derived states for the UI:** `active`, `confirming`, `recovered` (awaiting an operator), `unknown` (unavailable reading or gap), `resolved`. Sort active and unknown first, critical first.
- **Frozen notices:** each transition stores an immutable snapshot with a monotonic `sequence` and the rendered email/webhook bodies. A retry sends the same bytes under the same delivery ID. Before sending, the worker **skips a notice that a later transition has superseded**.

## Damping: hold, cooldown, caps

Size the system for "a few emails an hour" even under deliberate noise:

| Mechanism | Rule |
| --- | --- |
| **Hold** | the `for` duration above. Stops one-off blips |
| **One open alert per signal** | enforced by a unique index |
| **Cooldown** | a signal that opened an alert in the last hour opens the next one **quiet**, and a quiet alert's escalation and resolution notify nobody. Flapping is reported at most once an hour |
| **Per-signal email cap** | 3 emails an hour across every kind and cycle; send times stored on the alert |
| **Global burst limit** | more than 5 emails in 10 minutes system-wide turns further openings into a **digest** (at most one per 10 minutes). Other kinds past the burst are not emailed; the dashboard and webhooks still show them |
| **Per-webhook cap** | 12 posts an hour per signal |

- **Make the notification decision in the transaction that opens the alert** (`mail` / `held` / `digest` / `quiet`), so a late or repeated delivery decides the same way. A notice blocked by a cap is recorded as blocked, never promised.
- **Acknowledge** stops reminders only; the alert stays open and webhooks continue. It needs only an operator session, because it hides nothing.
- **Silence** a signal for 1, 4 or 24 h. It needs **step-up and a reason**, because it can hide a security alert. Listeners re-check silences at send time.

## Leader election for the evaluator

Run the loop on every server, but let only the lock holder evaluate:

- Take a **session-level advisory lock** with try-lock semantics (`pg_try_advisory_lock(hash(name || schema))`), never waiting, on a **dedicated connection**. Others try again next minute. On other stacks, use a Redis `SET key NX PX`-style lease with renewal, or your platform's leader election.
- **Failover:**
  - the lock dies with its connection, so a crashed leader is replaced within about a minute;
  - graceful shutdown releases it explicitly before closing the pool;
  - each turn the leader **verifies it still holds the lock** (5 s timeout); a hung connection counts as lost and is closed.
  - A leader that loses the lock drops its in-memory timers. The worst case is a delayed alert, never a duplicate.
- **Backstops in case two evaluators ever overlap:** every alert mutation takes a transaction-scoped advisory lock and re-reads what it decides by, and a unique index allows one open alert per signal.
- Give the evaluation a 30 s deadline.
- Use the same try-lock pattern for any once-per-fleet job (hourly rollup, daily table-size snapshot with `ON CONFLICT DO NOTHING` on `(day, table)`).

## External uptime check plus heartbeat

- **External uptime check:** from outside your network, from two or more regions, every minute, on `GET /api/health` and `/`. Page after two consecutive failures. Add a separate check on **TLS certificate expiry**.
- **Heartbeat (dead man's switch):** after each **successful** evaluation, the leader GETs each configured heartbeat URL (a Healthchecks.io / Better Stack / Cronitor-style service) with a 1-minute period and a grace of a few minutes. No ping is sent if the evaluation failed or no server leads. When the pings stop, the service pages someone. This catches every server down, the database unreachable, or the alert loop wedged: things nothing inside the app can report. One heartbeat failing doesn't stop the others.
- **Test the setup:** stop the servers for five minutes and see the heartbeat alert. Raise a harmless seeded warning in staging and see the email. There is no "send test alert" button in the reference design; add one if you want it, with the same no-PII rendering.

## Receivers: opt-in, no PII

- **Nothing is sent until someone subscribes.** No default recipient, no global "everything" setting, and **an empty selection receives nothing**, never "everything".
- Each admin subscribes per signal, choosing a minimum level and the event kinds. Subscriptions never replay history. **Filter recipients again at delivery time** against current membership, address and subscription.
- **Notification content is fixed text and numbers only:**
  - **include** the signal title, level word, value and unit, opened time (UTC), duration and the one-sentence meaning;
  - **never include** a link of any kind (the email says "open the admin dashboard"), the admin URL or key, an email address, an IP, a user or tenant name, or sample IDs.
  - The inbox and the chat channel are less trusted than the dashboard.
- **Subjects are built in code** against a strict pattern: `Critical alert: X`, `Warning: X`, `Now critical: X`, `Still open: X`, `Resolved: X`.
- **Test it:** render every notice kind for every rule, plus digests and every webhook format, from an alert whose details contain a real email and ID. Fail on any URL, href, email, IP, ID, name or key.
- **Email partial failure:** if some recipients succeed and others fail, log the failures and mark the delivery done. Retrying would email the successful ones twice. If all fail, retry.

### Webhooks

- **Formats:** `json` (`{version, alert, cycle, sequence, event, signal, level, value, occurredAt, state, title, unit, meaning, …}`) and chat (`{text}` with `& < >` escaped). The body holds what the email holds and nothing more.
- **Delivery is at least once and unordered.** Receivers must ignore a `sequence` lower than one they've already seen for that alert.
- **Fan-out:** one queued delivery per matching webhook, each with its own retries (backoff from 1 s up to 1 h). **Before sending, re-read the webhook.** If it was deleted, disabled, or its URL or format changed since the delivery was queued, skip it as completed-not-sent. Never send an old notice to a new receiver.
- **Store receiver URLs encrypted.** A chat webhook URL *is* a secret. Use a key derived from the app master key, bound to the receiver ID and field. URLs are write-only: re-enter to replace, never displayed, logged or echoed in errors. Losing the master key makes receivers unreadable and they fail closed.
- Changing a receiver needs step-up, a reason and the expected revision (409 if stale), and is audited with the changed **field names**, never their values. Caps: 20 webhooks, 5 heartbeats.

### Signed webhooks

- Give each webhook **its own random 32-byte key** and a public key ID.
- **Headers:**

  ```
  X-App-Signature: t=1731830400,v1=<hex HMAC-SHA256(key, "<t>.<raw body>")>
  X-App-Key-Id:    whk_…
  X-App-Delivery:  evt_…        # identical on every retry → receivers deduplicate
  ```

- **Receivers verify:**
  - recompute the HMAC over the raw bytes and compare in constant time;
  - reject a timestamp more than 5 minutes off;
  - deduplicate by delivery ID.
  - The timestamp only bounds replay age. Replay protection is delivery ID plus timestamp window.
- **Rotation:** for 15 minutes after a rotation, sign each post with **both** keys (two `v1=` entries over the same timestamp and body). A second rotation during that overlap gets 409. Receivers accept any matching `v1`. After the overlap the old key never signs and is erased. Replacing the URL creates a new key with no overlap.
- Reading a signing key needs an operator in a step-up window, `Cache-Control: no-store`, and an audit event recorded **without** the key value.

## SSRF guard on outbound requests

Any URL an operator (or user) can configure, such as webhooks, heartbeats or avatar fetches, can be pointed at your internal network or cloud metadata (`169.254.169.254`). Guard every outbound request:

1. **Validate when saved and again before each use** (validation sends no request):
   - an absolute URL with a host, **https only in production**;
   - no userinfo (`user:pass@`), no fragment, no whitespace or newlines, at most 2,048 characters;
   - no localhost, loopback, private or link-local hosts, unless an explicit `ALLOW_PRIVATE_TARGETS` flag is set for an internal relay.
   - Error messages **never echo the URL**.
2. **Check again at connection time, after DNS**, in the HTTP client's dial hook. The dialled IP must be public unicast. This defeats **DNS rebinding**, where a name resolves to a public address at validation and an internal one at send time. Block:
   - loopback, private (RFC 1918, `fc00::/7`), link-local (`169.254/16`, `fe80::/10`), multicast, unspecified;
   - CGNAT `100.64/10`, benchmarking `198.18/15`, reserved and documentation ranges;
   - NAT64, 6to4 (`2002::/16`), Teredo (`2001::/32`), `192.88.99.0/24`, IPv4-compatible and translated forms.
   - Judge IPv4-mapped IPv6 (`::ffff:10.0.0.1`) as its IPv4 address.
3. **No redirects** (a 3xx is a failure); otherwise a public URL redirects to an internal one.
4. **No proxy from the environment**, TLS 1.2+, certificates verified.
5. **Timeouts:** 5 s to connect, 10 s total (5 s for heartbeats). **Read at most 4 KiB** of the response and ignore it. Only 2xx counts as success.
6. Strip the URL from errors before logging. Config summaries show only whether private targets are allowed.

## Integrity checks: "things that should never be true"

Each check is a signal (`integrity.<check>`) that fails from **one row**. It is one bounded, indexed query that counts violating rows (capped, "1,000+") and names up to 10 by **public ID**. Run them on demand when the page is read, and **hourly** for alerting. A check that wasn't read is "not loaded", never OK.

| Level | Examples |
| --- | --- |
| critical | a tenant with no owner; a tenant strictly over a quota; an admin without MFA after their setup window; **queued jobs more than 10 min past their next-attempt time** (stalled worker, or all servers down) |
| warn | expired sessions, codes or links not cleaned up 30 min after expiry; ended rate-limit windows still stored; deliveries failed with "no listener" (a handler renamed while jobs were queued); **event names not in the catalogue**; **servers out of step** (different versions, or code expecting unapplied migrations) after a 30-minute release grace; a pending invitation to someone already a member |
| info (no level) | duplicates and retries: the same create by the same actor within 10 s (double submit), the same email sent twice for one request, deliveries that succeeded only after a retry, write routes that timed out. A nonzero count suggests idempotency keys |

- **Bound the work:** checks over rows without their own timestamp run only for tenants with an event in the last day. A tenant can only break a rule by writing, and writes record events.
- **Registry tests keep coverage automatic:**
  - a test reads `information_schema` and fails if any table with a tenant-ID column isn't in the monitored-tables registry, or lacks an index leading with the tenant ID;
  - another test keeps the migration tables, the data map and the known-tables list equal.
  - New tables are then monitored without anyone remembering to add them.

## Retention monitoring

For each prune target, show the oldest row a run *would* delete and the last confirmed run:

- **overdue:** the oldest prunable row is more than 7 days past the target's age;
- **not running:** no confirmed run in 2 days.

List targets automatically from the pruner registry. Track daily table sizes (one insert-only row per table per day, kept 400 days) and project growth 90 days ahead.

## The dashboard's own privacy

- **Show counts by default.** Emails and IPs appear only in admin review lists, and those reads are audited. Looking up an IP is "following someone" and is audited.
- **From database activity views,** read only counts by state and oldest start times, never query text, role or client address.
- **The config page** reads allow-listed settings field by field. Secrets show as set / not set. Flag settings that differ between servers.
- **Configuration warnings, each with the fix:**
  - not production;
  - mail off;
  - an admin IP allowlist without trusted-proxy config;
  - the previous admin key still set (and how many sessions use it);
  - metrics without a token;
  - migrations pending;
  - more than one version live;
  - no admin with MFA;
  - one IP sending over 80 % of events (the proxy's client-IP config is missing);
  - CAPTCHA off in production;
  - prune not running.
- **Accessibility:** show a level as a word plus an icon, never colour alone. Charts get a "show as table" option and a summary sentence.

## Signal catalogue (checklist; warn / critical, window)

| Area | Signals |
| --- | --- |
| Auth & security | failed sign-ins 100 / 1,000 per hour; credential stuffing (one IP or /64 trying 10+ emails) warn 1; stuffing **successes** critical 1; distributed guessing (one email failing from 5+ IPs); unknown-user share 50 % (min 50); success after 5+ failures critical 1; possible takeover (password/MFA/recovery change within 1 h of a sign-in from a network unseen for 30 days); 3+ failed MFA codes; global reset limit reached critical; sign-up global usage 50 % / 100 %; admin login failures; non-admins with valid credentials at the admin door critical; admin session from a new network |
| Rate limits | any shared/global limit reached (a feature off for everyone) critical; subjects currently blocked; top users and IPs at limits; quota refusals; 429s by route |
| Abuse | accounts created per IP per day 5 / 20; busiest-IP share 80 % (also detects proxy misconfiguration); users creating 5+ tenants a day; 20+ invitations to 15+ addresses a day; invitation acceptance under 10 %; links or markup in names; new-and-busy accounts |
| Tenants | fullest quota 80 % / 100 %; largest tenant; growth leaders |
| Usage | sign-ups, DAU/WAU/MAU, MFA adoption, activation, inactive for 2 years (listed for a human decision, never auto-deleted) |
| Fleet | live servers; distinct versions live (warn 2); config drift |
| HTTP | 5xx share 1 % / 5 % (min 100); p95 1 s / 3 s (min 20); slowest route p95 2 s / 5 s; per-route p50/p95/p99, 4xx, 429, 5xx, timeouts |
| Database | ping incl. pool checkout critical 500 ms; minutes with pool waits in the last 6 warn 5; average pool wait critical 1 s; longest open transaction 5 / 60 min; connections waiting on locks; connections vs max; idle-in-transaction; cache hit ratio; sequential-scan-heavy tables; tables needing vacuum; top statements |
| Async & mail | oldest pending delivery 2 / 10 min; permanently failed deliveries 1 / 10 per day; delivery lag p95 10 s; overdue jobs critical |
| CPU pools | requests refused because every password-hashing slot was busy |
| Errors | new error groups since this release warn; new panics critical; new server groups with 10+ occurrences (exclude browser reports, since anyone can send one) |
| Data | retention overdue; prune not running; growth projection |
| Disk | not visible from SQL: use the provider's monitoring |

## Deliberately not done

- No stored signals, no auto-resolve, no default recipients, no "empty = all", no links or PII in notifications, and no automatic deletion of inactive users (a business decision).
- **Known detection gaps** are written down rather than hidden. Example: a takeover change made more than 1 h after the new-network sign-in isn't flagged.
