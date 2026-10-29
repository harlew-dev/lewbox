# Observability

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

How the people running Lewbox see what it's doing: logs, metrics, traces, request ids, health and readiness checks, profiling, and an audit log of administrative actions. This is Lewbox's *own* observability. It's separate from [profiles/telemetry](../profiles/telemetry.md), which exports *agents'* traces to a profile's own OTLP endpoint, and from the [activity log](../activity-and-webhooks/activity-log.md), which shows a project's users what happened to their sandboxes.

## Today

- **Gateway** (`lewbox-gateway-legacy/docs/deployment.md`): JSON lines on stdout in production, text locally. One `request` line per request with `request_id`, `method`, `path`, `status`, `duration_ms`, and `profile` and `tenant` once authenticated. Never logged: the `Authorization` header, keys, the service token, PATs. `GET /up` answers `200 {"status":"ok"}` without calling the app, so a slow app doesn't take every replica out of rotation. No metrics, no traces.
- **App**: Laravel logging, Nightwatch for monitoring. The Admin API doesn't send `Lewbox-Request-Id` yet. No metrics endpoint.
- **Nodes**: per-minute metrics (CPU, load, memory, disk, uptime, `aerolvm_healthy`) posted to the app and shown on the node page ([clusters/health-and-metrics](../clusters/health-and-metrics.md)). That's *about* nodes, for project users, and stays a product feature.
- The two services' logs are joined by `request_id`, sent from gateway to app as `Lewbox-Request-Id`.

## Goals

- Every request, job and operation traceable end to end by one id.
- Metrics an operator can alert on, in the format everyone already scrapes (Prometheus), with bounded cardinality.
- Traces when an OTLP endpoint is configured, and no cost when it isn't.
- Health checks that mean something: liveness that doesn't depend on the database; readiness that does.
- No secrets in any of it.

## Design

### Logs

- `log/slog`, JSON by default (`LEWBOX_LOG_FORMAT=json`), text in development. Level by `LEWBOX_LOG_LEVEL`.
- **Every line** has `time`, `level`, `msg`, `replica`, `version`. Lines in a request add `request_id`; in a job, `job_id`, `job_kind`, `attempt`, and the originating `request_id`; in an operation, `operation`, `operation_kind`, `step`, `sandbox`.
- **Context-carried attributes**: a `log.With(ctx, attrs…)` helper adds attributes to the context, and the handler includes them on every line logged with that context. Code logs with `slog.InfoContext(ctx, …)`.
- **Access log**: one `request` line per request, at the end, with `request_id`, `method`, `route` (the pattern, e.g. `/v0/sandboxes/{sbx}`), `path`, `status`, `code` (the error code if any), `duration_ms`, `bytes_in`, `bytes_out`, `ip`, `user_agent`, and once authenticated `project`, `profile`, `key` (the key's `key_` id), `user`. Streams log a line when they *open* and when they *close*, with their duration and why they closed.
- **Levels**: `error` is for something an operator should look at (a bug, an invariant broken, a dependency down beyond retries). Expected failures (a 4xx, a toolbox warming up and retried) are `info` or `debug`. One failure is logged once, where it's handled, not at every layer.
- **Redaction**: the redacting handler from [secrets-and-encryption](secrets-and-encryption.md#secrets-in-memory-logs-and-errors). Request and response bodies are never logged.
- **Sampling**: none by default. If successful requests flood a busy instance, `LEWBOX_LOG_SAMPLE_SUCCESS=0.1` samples 2xx access lines (errors and slow requests always logged).

### Request ids

- `req_` + 24 hex, generated for every request on every surface: API, dashboard, node API, hooks, uploads ([ids-and-keys](ids-and-keys.md#request-ids)).
- Returned as `Lewbox-Request-Id` on every response, including errors, streams and WebSocket upgrades. CORS exposes it.
- Stored on: activity events (`request_id`), jobs enqueued by the request (job metadata), operations (`request_id` of the request that asked for them), webhook events (so a delivery can be traced to what caused it).
- Error bodies don't repeat it (the header has it), but the SDKs put it on every error, and the dashboard shows it on error toasts with a copy button.

### Metrics

Prometheus text format at `/metrics`.

**Where**: on a separate listener if `LEWBOX_METRICS_ADDR` is set (e.g. `127.0.0.1:9090`, with `/debug/pprof`), otherwise on the main listener behind `Authorization: Bearer $LEWBOX_METRICS_TOKEN`. Without either, off. Never public.

**Cardinality rule**: no label per project, profile, sandbox, user or key. Those go in logs and traces. Allowed labels: route pattern, method, status class, error code, operation kind and step, job kind, queue, cluster (bounded: a few per project, and instance operators run few projects; if an instance has many clusters, `LEWBOX_METRICS_CLUSTER_LABEL=false` drops it), agent kind, result.

| Metric | Type | Labels |
|---|---|---|
| `lewbox_build_info` | gauge (1) | `version`, `commit`, `go` |
| `lewbox_http_requests_total` | counter | `route`, `method`, `status`, `code` |
| `lewbox_http_request_duration_seconds` | histogram | `route`, `method` |
| `lewbox_http_streams_open` | gauge | `kind` (`sse_command`, `sse_turn`, `ws_command`, `ws_attach`, `dashboard`) |
| `lewbox_http_stream_duration_seconds` | histogram | `kind`, `close_reason` |
| `lewbox_auth_attempts_total` | counter | `kind`, `result` |
| `lewbox_rate_limited_total` | counter | `bucket` |
| `lewbox_db_*` | various | see [database](database.md#observability) |
| `lewbox_jobs_*`, `lewbox_leader`, `lewbox_notify_*` | various | see [jobs-and-scheduling](jobs-and-scheduling.md#observability) |
| `lewbox_operations_total` | counter | `kind`, `status`, `error_code` |
| `lewbox_operation_duration_seconds` | histogram | `kind`, `status` |
| `lewbox_operation_step_duration_seconds` | histogram | `kind`, `step` |
| `lewbox_operation_retries_total` | counter | `kind`, `step`, `reason` (`unreachable`, `capacity`, `toolbox_warming`, …) |
| `lewbox_sandboxes` | gauge, sampled every 30 s | `cluster`, `status` |
| `lewbox_aerolvm_requests_total` | counter | `cluster`, `endpoint` (pattern), `status` |
| `lewbox_aerolvm_request_duration_seconds` | histogram | `endpoint` |
| `lewbox_toolbox_requests_total` | counter | `endpoint`, `result` |
| `lewbox_toolbox_unavailable_total` | counter | `cluster` |
| `lewbox_toolbox_unexpected_total` | counter | `route` — answers outside the fork's contract (an uncoded error, `-1` exit codes); each is a fork bug to fix, not a case to handle ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#no-workarounds-in-lewbox)) |
| `lewbox_nodes_outdated` | gauge | `cluster` — nodes below the minimum fork release |
| `lewbox_nodes` | gauge | `cluster`, `status` |
| `lewbox_node_heartbeat_age_seconds` | gauge | `cluster`, `node` (nodes are few and stable; the one exception to the rule) |
| `lewbox_helper_installs_total` | counter | `helper`, `result` |
| `lewbox_agent_turns_total` | counter | `agent`, `stop_reason` |
| `lewbox_agent_turn_duration_seconds` | histogram | `agent` |
| `lewbox_agentd_starts_total` | counter | `result` (`started`, `upgraded`, `failed`) |
| `lewbox_agent_uploads_total` | counter | `result` |
| `lewbox_webhook_deliveries_total` | counter | `result` (`succeeded`, `failed`, `retrying`) |
| `lewbox_webhook_delivery_duration_seconds` | histogram | |
| `lewbox_webhook_endpoints_disabled_total` | counter | |
| `lewbox_emails_sent_total` | counter | `kind`, `result` |
| `lewbox_sign_ins_total` | counter | `agent`, `result` |
| `lewbox_github_requests_total` | counter | `api` (`rest`, `graphql`), `status` |
| `lewbox_github_rate_limit_remaining` | gauge | `app` (per GitHub App: few) |
| Go runtime and process | | standard `collectors` |

A starter set of alerts and a Grafana dashboard live in `docs/self-hosting/monitoring/` (both generated from this table where possible):

- No leader for > 1 minute.
- Operations failing with `cluster_unavailable` for a cluster.
- `lewbox_operations_rescued_total` rising (replicas dying mid-work).
- Node heartbeat age > 3 minutes.
- Webhook endpoints disabled.
- Job queue latency p95 > 30 s.
- 5xx rate > 1 % for 5 minutes.

### Traces

- OpenTelemetry SDK, enabled when `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_TRACES_EXPORTER`) is set; standard `OTEL_*` variables configure it. Without it, a no-op tracer: no cost.
- Spans: every HTTP request (`otelhttp`), every database query (pgx tracer, named by sqlc query), every AerolVM and toolbox call, every GitHub call, every job run, every operation step.
- **Across jobs**: the trace context is stored in the job's metadata at insert, and the worker starts a span *linked* to it (a link, not a child: a job may run minutes later). An operation's whole life is one trace rooted at its `operation.run` job, linked to the request that asked for it.
- Incoming `traceparent` headers are honoured only from `LEWBOX_TRUSTED_PROXIES` (so arbitrary clients can't inject trace ids), or always in development.
- Sampling: parent-based, ratio `OTEL_TRACES_SAMPLER_ARG` (default 1.0; operators turn it down).

### Health and readiness

| Endpoint | Answers 200 when | Used for |
|---|---|---|
| `GET /up` | The process is running and its HTTP server answers. **Doesn't touch the database**, so a database blip doesn't make every replica fail liveness at once and get restarted | Liveness; legacy `/up` kept |
| `GET /ready` | Startup is complete (migrations current, keyring loaded, listener connected), the database answers a ping within 1 s, and the process isn't shutting down | Load balancer readiness; drains a replica on shutdown |
| `GET /ready?verbose=1` | Same, with a JSON body naming each check's state (only on the metrics listener, or with the metrics token) | Debugging |

Both are unauthenticated, cheap, and not logged at `info` (they'd flood the log).

### Profiling

`net/http/pprof` on the metrics listener (`LEWBOX_METRICS_ADDR`) only. Never on the main listener.

### Audit

An audit log of administrative actions, separate from the activity log (which is about sandboxes):

- **What's audited**: instance admins' actions (projects created or suspended, users made admins); project admins' actions (members added or removed, roles changed, admin keys made or revoked, clusters created or suspended, nodes removed, webhook endpoints and secrets changed, GitHub App registered, settings changed); profiles made, suspended, deleted; profile keys made or revoked; secret and signing key rotations; `lewbox admin` commands.
- **Table** `audit_log`: `(id, occurred_at, project_id null, actor_type, actor_id, action, target_type, target_id, details jsonb, request_id, ip)`. Append-only: the application role has no `UPDATE` or `DELETE` on it, and pruning (default 1 year, `LEWBOX_AUDIT_DEFAULT_RETENTION`) runs as a separate statement allowed only for that.
- **Shown** to project admins in the dashboard (Settings → Audit log), and to instance admins for everything. Readable by `GET /v0/projects/{prj}/audit-log` with an admin key.
- **Not** in webhooks (for now).

## Configuration

`LEWBOX_LOG_LEVEL`, `LEWBOX_LOG_FORMAT`, `LEWBOX_LOG_SAMPLE_SUCCESS`, `LEWBOX_METRICS_ADDR`, `LEWBOX_METRICS_TOKEN`, `LEWBOX_METRICS_CLUSTER_LABEL`, `LEWBOX_AUDIT_DEFAULT_RETENTION`, standard `OTEL_*`: see [configuration](configuration.md#observability).

## Security

- `/metrics` and pprof never public; the metrics token is compared in constant time.
- Logs, metrics and traces carry ids, never secrets or request bodies. Span attributes are reviewed like log fields: no env values, no command text (commands may contain secrets; activity keeps only labels and outcomes, as the docs promise), no file contents, no prompts.
- Audit log rows can't be changed by the application.

## Testing

- The access log: one line per request, the right fields, no `Authorization`, the route pattern not the raw path.
- Every metric in the table is registered (a test compares the registry to the doc).
- Cardinality: a test drives many projects and sandboxes through the API and asserts no metric's series count grows with them.
- `/up` answers while the database is down; `/ready` doesn't; `/ready` drains on shutdown.
- Trace propagation into a job and an operation (with an in-memory exporter).
- Audit: each audited action writes one row; the application role can't update or delete rows.

## Build plan

1. `internal/log`: slog set-up, context attributes, redacting handler.
2. `internal/httpapi` middleware: request id, access log, recovery (logs a stack, answers `500 internal_error` with the request id).
3. `internal/metrics`: registry, the metrics above as they're built, `/metrics` listener and token.
4. `internal/trace`: OTel set-up, no-op default, job propagation.
5. `/up`, `/ready`.
6. `internal/audit` and the audit log table, endpoint and page.
7. Monitoring docs: alerts and dashboard.

## Open questions

1. ~~**A `cluster` label on hosted lewbox.com?**~~ Decided (2026-10-29): on by default; lewbox.com sets `LEWBOX_METRICS_CLUSTER_LABEL=false`, as it has many projects' clusters.
2. **Audit log by webhook** (`audit.*` events). Deferred (Beta), with [projects](../projects.md#open-questions)' question on which audit events projects may receive.
3. ~~**Error reporting (Sentry or similar)?**~~ Decided (2026-10-29): no; errors are recorded on OpenTelemetry traces and in logs.
