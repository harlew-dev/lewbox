# Rate limits

> Status: Draft · Milestone: Alpha (M0 framework and defaults; M1 sign-in and node limits) · Updated: 2026-10-29

Rate limits keep one runaway client from degrading Lewbox for everyone else, and blunt abuse of the endpoints anyone can call (sign-in codes, setup links). They're separate from **profile limits** (running sandboxes, vCPUs, budgets: `429 limit_exceeded`, [profiles](../profiles.md)), which are product rules; rate limits are traffic rules (`429 rate_limited`). Lewbox has no Redis ([README](../README.md#goals)), so limits are kept in memory per replica, with a Postgres-backed counter only for the few limits that must be exact.

## Today

- **Admin API**: Laravel's throttle, 120 requests a minute per tenant key, in the database cache (`throttle:admin-api`, `app/Providers/AppServiceProvider.php`).
- **Node API**: 60 a minute per token, 6 metrics posts a minute, 1200 a minute per IP.
- **Client activity events**: 60 a minute per key (`throttle:client-activity`), the only Consumer API limit; the gateway passed on the app's `429`.
- **Setup links**: `throttle:10,1` on exchanging a link.
- **Sign-in codes**: the passwordless package's own throttle.
- The gateway planned per-key limits "shared across replicas through Redis" and never built them, so a profile key can send as many requests as it likes.

## Goals

- Every credential and every anonymous endpoint has a limit.
- No new infrastructure: in-memory per replica, Postgres for the few exact ones.
- Standard headers so clients and SDKs back off correctly.
- Limits generous enough that legitimate use (Lewagent's dashboard, an agent's tight command loop) never hits them, and configurable per instance.

## Keep, change, drop

| | |
|---|---|
| **Keep** | `429 rate_limited` with `Retry-After`; 60 activity events a minute per key; node limits |
| **Change** | Limits on every credential, not just activity events and admin keys |
| **Change** | `RateLimit` and `RateLimit-Policy` headers |
| **Drop** | The database cache as a counter store for hot paths; the Redis plan |

---

## Design

### Two kinds of limiter

**1. Approximate, in memory (most limits).** Each replica runs a GCRA (generic cell rate algorithm) limiter per key, in a sharded map with LRU eviction. With *N* replicas, each enforces `limit / N`, where *N* is the number of live replicas, read from the `replicas` heartbeat table every 10 s ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)). Behind a load balancer that spreads requests evenly, this approximates the global limit within a small factor, at zero cost per request. A client pinned to one replica by connection reuse gets `limit / N`, which is why the defaults are generous and `N` is small in practice.

**2. Exact, in Postgres (few limits).** Where the limit is a security control or a product promise, a fixed-window counter in Postgres:

```sql
rate_counters (
  bucket text not null,          -- "signin_email:<sha256(email)>", "activity:<key_…>"
  window_start timestamptz not null,
  count int not null,
  primary key (bucket, window_start)
)
-- INSERT … ON CONFLICT (bucket, window_start) DO UPDATE SET count = rate_counters.count + 1 RETURNING count
```

One upsert per limited request; pruned every few minutes. Used only where the request already writes to the database (an activity event, a sign-in code, a setup link exchange), so it adds one cheap statement to a transaction that exists anyway.

### Classes and defaults

Each route's metadata names its class. Defaults (per minute unless said), configurable with `LEWBOX_RATE_LIMITS` (a small YAML or JSON map) and per project by instance admins (Later):

| Class | Key | Limit | Burst | Kind | Notes |
|---|---|---|---|---|---|
| `api` | credential | 1200 | 200 | memory | Default for every `/v0` route |
| `api.sandbox_key` | credential | 600 | 100 | memory | Sandbox keys in browsers |
| `api.session` | user | 1200 | 200 | memory | The dashboard polls; generous |
| `create.sandbox` | profile | 120 | 30 | memory | Sandbox creates; profile limits are the real control |
| `create.conversation` | profile | 120 | 30 | memory | |
| `stream.open` | credential | 120 | 30 | memory | Opening SSE and WebSocket streams |
| `activity.append` | credential | 60 | 10 | Postgres | As today, exact, documented |
| `keys.mint` | credential | 120 | 30 | memory | Sandbox key mints; the dashboard mints once per 13 min per sandbox |
| `auth.code.email` | email | 5 per 10 min | 5 | Postgres | Sign-in codes sent to one address |
| `auth.code.ip` | IP | 20 per hour | 10 | Postgres | Sign-in codes requested from one IP |
| `auth.session` | email | 10 per 10 min | 5 | Postgres | Code attempts; each code also allows only 5 tries |
| `setup.exchange` | IP | 10 | 10 | Postgres | Setup link exchanges |
| `anonymous` | IP | 120 | 30 | memory | `/v0/meta`, `/v0/openapi.json`, `/install.sh`, downloads |
| `node` | node token | 60 | 20 | memory | Node API |
| `node.metrics` | node token | 6 | 2 | memory | |
| `node.ip` | IP | 1200 | 200 | memory | All node API traffic from one address |
| `agentd.upload` | conversation | 600 | 120 | memory | agentd uploads every second while busy |
| `hooks` | project | 600 | 200 | memory | Inbound GitHub webhooks |

Notes:

- **Streams count once** when opened (`stream.open`), not per message. Concurrent streams are capped separately (below).
- **Inside-sandbox requests** aren't singled out: an agent running thousands of small commands is legitimate, and the sandbox's own capacity is the natural limit. The `api` class covers them.
- **Keyed by credential** means the `key_` id (or user for sessions), so a profile with ten keys gets ten budgets; profile-wide limits (`create.*`) stop that being used to multiply creates.
- **IP**: the client IP after trusted-proxy handling ([conventions](conventions.md#base-url-and-hosts)).

### Concurrency caps

Separate from rates, enforced in memory per replica (and per sandbox where it matters):

| Cap | Default | Error |
|---|---|---|
| Open streams per credential | 200 | `429 rate_limited` |
| Open streams per sandbox | 400 | `429 rate_limited` |
| Commands in flight per sandbox | 64 | `429 rate_limited` |
| Uploads in flight per sandbox | 8 | `429 rate_limited` |

These protect the toolbox and agentd in one sandbox from a client that opens far more than a person or agent would.

### Headers

On every response from a limited route (the IETF `RateLimit` header fields draft, as the SDKs already understand `Retry-After`):

```
RateLimit-Policy: "api";q=1200;w=60
RateLimit: "api";r=1137;t=12
```

- `q` the quota, `w` the window in seconds, `r` remaining, `t` seconds until it resets. With approximate limits, `r` and `t` are this replica's view.
- On `429`: `Retry-After: <seconds>`, always. The body is `{"message": "Too many requests. Wait 12 seconds, then try again.", "code": "rate_limited"}`.
- CORS exposes `RateLimit`, `RateLimit-Policy` and `Retry-After` ([conventions](conventions.md#cors)).

### What the SDKs do

Retry `429 rate_limited` after `Retry-After`, up to `maxRetries` (3), never `429 limit_exceeded` ([sdks-and-cli](../sdks-and-cli.md)). Creates are safe to retry because of [idempotency](idempotency.md).

---

## Observability

- Metrics: `lewbox_rate_limited_total{class}`; `lewbox_rate_limiter_keys{kind}` (memory size); `lewbox_streams_open{scope}`.
- Each `429` is logged with the class and key, sampled after the first per key per minute so a hammering client doesn't flood the logs.
- A `429` inside a sandbox is a `request.refused` activity event, sampled the same way ([activity-and-webhooks](../activity-and-webhooks.md)).

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_RATE_LIMITS` | built-in table | Overrides per class: `{"api": {"limit": 2400, "burst": 400}}` |
| `LEWBOX_RATE_LIMITS_DISABLED` | `false` | For tests and private instances |
| `LEWBOX_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-For` is believed |

## Testing

- GCRA unit tests with an injected clock.
- Replica division: two servers on one database, a client alternating between them gets about the configured limit in total.
- Exact limits under concurrency: 100 concurrent activity posts, exactly 60 succeed.
- Headers on success and on `429`.

## Build plan

`internal/httpapi/ratelimit`: GCRA limiter (sharded map, LRU), replica count from the heartbeat table, Postgres fixed-window counter, class table and config, middleware reading the route's class, concurrency caps with release on response or stream end, headers. M0 for the API classes; M1 for sign-in, setup and node classes; M4 for activity.

## Open questions

1. ~~**Per-project overrides.**~~ Decided (2026-10-29): global configuration only in the alpha; per-project overrides Later.
2. ~~**Advisory counters for uneven load balancing?**~~ Decided (2026-10-29): no, until we see it matter.
3. ~~**A lower limit for profile keys?**~~ Decided (2026-10-29): no, the same per credential; total load is bounded by profiles' sandbox limits.
