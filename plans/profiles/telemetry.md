# Telemetry

> Status: Draft · Milestone: Alpha (M3) · Plugin: `telemetry` · Updated: 2026-10-29

Claude Code and Codex emit OpenTelemetry of their own: metrics (tokens, cost, sessions, lines changed), log events (each prompt, model request, tool decision and result) and traces (a span per turn, with model requests and tool calls under it). A profile can point them at an **OTLP endpoint** of its customer's (Langfuse, Honeycomb, Grafana Cloud, LangSmith, Datadog, Phoenix, a collector) so developers watch and evaluate their agents in the tools they already use. Lewbox exports; it doesn't store traces. Telemetry is in the alpha ([D7](../README.md#decisions)).

## Today

Built in legacy on 2026-10-25, not yet checked live end to end (`lewbox-legacy/docs/profiles.md` § *Telemetry*; `app/Models/ProfileTelemetry.php`; `app/Enums/Profile/TelemetryProvider.php`; `app/Sandboxes/Setup/TelemetrySetup.php`; `app/Actions/Profiles/{SaveProfileTelemetry,RemoveProfileTelemetry}.php`; gateway `internal/agentd/agents.go` `withTelemetry`; public docs [lewbox.com/docs/profile/telemetry](https://lewbox.com/docs/profile/telemetry)):

- `profile_telemetry`: one per profile: `provider` (`langfuse`, `honeycomb`, `grafana_cloud`, `langsmith`, `custom`), `settings` (encrypted; a known provider's fields), `endpoint`, `protocol` (`http/protobuf`, `http/json`, `grpc`), `headers` (encrypted, up to 20), `signals` (`metrics`, `logs`, `traces`, all on by default), `content` (`prompts`, `responses`, `tool_details`, all off by default).
- **Providers** are set up from the fields their docs name; the app builds endpoint, protocol and headers:
  - Langfuse: `host` + `/api/public/otel`, `Authorization: Basic base64(public:secret)`, `x-langfuse-ingestion-version: 4`; traces only.
  - Honeycomb: `https://api.honeycomb.io` or `api.eu1.honeycomb.io`, `x-honeycomb-team`.
  - Grafana Cloud: the stack's OTLP endpoint, `Authorization: Basic base64(instance_id:token)`.
  - LangSmith: `https://{,eu.,apac.}api.smith.langchain.com/otel`, `x-api-key`, optional `Langsmith-Project`; traces only.
  - Custom: endpoint, protocol and headers as given.
- **Delivery into the sandbox**:
  - Claude Code's settings go in `/root/.config/lewbox/telemetry.env` (`0600`, `NAME=value` lines), **not** the profile env file, so the customer's own OTel-instrumented apps don't pick up `OTEL_*`. `lewbox-agentd` reads it each time it starts an adapter and adds `lewbox.conversation.id` and `lewbox.agent` to `OTEL_RESOURCE_ATTRIBUTES`.
  - Variables: `CLAUDE_CODE_ENABLE_TELEMETRY=1`, `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` (for traces), `OTEL_{METRICS,LOGS,TRACES}_EXPORTER` (`otlp`/`none`), `OTEL_EXPORTER_OTLP_PROTOCOL`, `_ENDPOINT`, `_HEADERS` (percent-encoded), `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_LOG_USER_PROMPTS`, `OTEL_LOG_ASSISTANT_RESPONSES` (set explicitly, as Claude Code otherwise follows prompts), `OTEL_LOG_TOOL_DETAILS`, `OTEL_LOG_TOOL_CONTENT`.
  - Codex reads OTel only from its config: an `[otel]` table in `~/.codex/config.toml` between `# lewbox telemetry begin`/`end`, one exporter per signal (`exporter` for logs, `trace_exporter`, `metrics_exporter`), each over HTTP with its signal's full URL (`{endpoint}/v1/logs`, … since Codex adds no path); `log_user_prompt` follows `prompts`. A customer's own `[otel]` table is left alone and the step fails saying so. (Codex used to get logs only, so Langfuse got nothing from it; fixed and checked live with Codex 0.163.0.)
  - **Check from the sandbox**: POST an empty OTLP export of the first signal sent, with the headers, from inside the sandbox, expecting `2xx`. The app never calls the endpoint (a customer sets it on a setup link; an app-side request would let them probe the app's network). gRPC, or no `curl`, isn't checked.
- Changes re-apply setup (`apply_setup`); a running adapter keeps its environment until it idles out (10 minutes) or a new conversation starts one.
- Open in legacy: Lewbox's own spans for turns, and taking the consumer's `traceparent`; a Lewbox collector holding the headers so they're not in sandboxes.

## Goals

- Port it as is: providers, delivery, content controls, the check from the sandbox.
- Same validation and mapping for the dashboard, the setup page and the API, from one Go registry.
- Add Lewbox's own spans for each turn (queued, waiting on approval, running), with the agents' spans under them, and accept a `traceparent` on prompts so a consumer's trace reaches into the agent (stretch for the alpha; see [Lewbox's own spans](#lewboxs-own-spans)).

### Non-goals

- A trace store or viewer in Lewbox (decided in legacy, 2026-10-25: it would compete with free, self-hostable tools).
- Several endpoints per profile (a collector the customer runs does fan-out).
- Telemetry for the customer's own processes in the sandbox (theirs to configure).

## Design

### Data model

```sql
create table profile_telemetry (
  profile_id  bigint primary key references profiles(id) on delete cascade,
  provider    text not null default 'custom',     -- langfuse | honeycomb | grafana_cloud | langsmith | custom
  settings    bytea,                              -- encrypted JSON of a known provider's fields; null for custom
  endpoint    text not null,                      -- up to 2048 chars, http(s)
  protocol    text not null,                      -- http/protobuf | http/json | grpc
  headers     bytea,                              -- encrypted JSON name → value, up to 20
  signals     jsonb not null default '{"metrics":true,"logs":true,"traces":true}',
  content     jsonb not null default '{"prompts":false,"responses":false,"tool_details":false}',
  last_check  jsonb,                              -- { at, sandbox, status, message } from the latest setup report
  updated_by  jsonb not null,
  updated_at  timestamptz not null default now()
);
```

### Providers

A Go registry in the plugin (`plugins/telemetry/providers.go`), served as `GET /v0/telemetry/providers` for forms:

| Provider | Fields | Builds | Signals |
|---|---|---|---|
| `langfuse` | `host` (default `https://cloud.langfuse.com`), `public_key`, `secret_key` (secret) | `{host}/api/public/otel`, `http/protobuf`, `Authorization: Basic …`, `x-langfuse-ingestion-version: 4` | traces |
| `honeycomb` | `region` (`us`, `eu`), `api_key` (secret) | `https://api.honeycomb.io` / `https://api.eu1.honeycomb.io`, `x-honeycomb-team` | all |
| `grafana_cloud` | `endpoint`, `instance_id`, `token` (secret) | the endpoint, `Authorization: Basic …` | all |
| `langsmith` | `region` (`us`, `eu`, `apac`), `api_key` (secret), `project` | `https://{,eu.,apac.}api.smith.langchain.com/otel`, `x-api-key`, `Langsmith-Project` | traces |
| `custom` | `endpoint`, `protocol`, `headers` | as given | all |

Later candidates: Datadog (via its OTLP intake), Arize Phoenix, Braintrust, Weights & Biases Weave, Helicone; each is a registry entry.

A provider that takes only some signals is sent only those, whatever `signals` says. Secret fields are never returned; left blank on update they keep their value; changing provider starts from scratch.

### Setting it

`PUT /v0/projects/{prj}/profiles/{prf}/telemetry`:

```json
{ "provider": "langfuse",
  "settings": { "host": "https://cloud.langfuse.com", "public_key": "pk-lf-…", "secret_key": "sk-lf-…" },
  "signals": { "metrics": false, "logs": false, "traces": true },
  "content": { "prompts": true, "responses": true, "tool_details": false } }
```

or for `custom`, `endpoint`, `protocol` and `headers` (a `null` `headers` removes them; omitted keeps them). At least one signal must be on. Saving without a change does nothing. Otherwise it stores, then enqueues `apply_setup` for running sandboxes and writes the audit and `profile.setup_changed` events. `DELETE …/telemetry` removes it (setup cleanup removes the files and the Codex table).

### What reaches the sandbox

The telemetry step of [profile setup](../sandboxes/profile-setup.md), after Codex's, `requires: sh`:

1. **Claude Code**: `/run/lewbox/telemetry.env` (`0600`, on the sandbox's memory-only mount, [profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)) with the variables listed in [Today](#today), and resource attributes `lewbox.profile.id`, `lewbox.sandbox.id`. Not loaded by login shells.
2. **Codex**: **changed from legacy, nothing in `config.toml`.** Legacy wrote a managed `[otel]` block, headers included, into `/root/.codex/config.toml`, which is on disk and in every snapshot. Now the same settings (one exporter per signal sent, each with its full per-signal URL and the headers, plus `log_user_prompt`) are written to `/run/lewbox/telemetry.env` beside Claude's, and agentd hands them to Codex as config overrides in `CODEX_CONFIG` when it starts `codex-acp`, as it already does for instructions ([agents/agentd](../agents/agentd.md#the-adapters-environment)). A customer's own `[otel]` table in `config.toml` is left alone, and the overrides win for agent runs. Codex run by hand in a terminal doesn't export the profile's telemetry; the docs say so.
3. **Check**: an empty OTLP export (`{}` JSON for `http/json`, an empty protobuf message for `http/protobuf`) POSTed to the first signal's URL with the headers, from inside the sandbox with `curl`, expecting `2xx`; the result (status code, first 200 bytes of body, redacted) goes into the setup report and `profile_telemetry.last_check`. gRPC isn't checked (`configured`).
4. **Cleanup**: remove `telemetry.env`, and a legacy `[otel]` block if a migrated sandbox still has one. `/run/lewbox` is never in a snapshot, and its paths are scrubbed from snapshots regardless ([profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)).

**agentd** ([agents/agentd](../agents/agentd.md)) reads `telemetry.env` each time it starts an adapter (`LEWBOX_AGENTD_TELEMETRY_FILE`), adds `lewbox.conversation.id=cnv_…` and `lewbox.agent=claude-code|codex` to `OTEL_RESOURCE_ATTRIBUTES`, and passes the rest to the adapter's environment. Its options probe (listing models) doesn't get telemetry. An adapter already running keeps its environment until it idles out (10 minutes) or exits; the docs say changes reach "the next agent to start".

Tagging every record with `lewbox.profile.id`, `lewbox.sandbox.id`, `lewbox.conversation.id` and `lewbox.agent` is what lets a customer go from a trace to the conversation's stored chunks and back. The agents add their own (`session.id`, `prompt.id`, Codex's `conversation.id` = the conversation's `agent_session_id`).

### Content

Off by default: the agents send metadata only (tokens, cost, durations, model names, tool names and results). Each of `prompts`, `responses` and `tool_details` is opt-in, because content holds code and secrets; an AI judge in the customer's backend needs it, so it's their choice. Claude Code's raw API bodies (the whole conversation on every request) are never enabled. The setup page explains each toggle in one sentence.

### Lewbox's own spans

Stretch for the alpha, otherwise early after:

- agentd emits a span per turn (`lewbox.turn`), with child spans for time queued, time waiting on approvals and questions, and the agent's run; Lewbox's control plane emits a span for the prompt request.
- The prompt endpoints accept `traceparent` (W3C) from the consumer; agentd parents the turn span on it.
- Getting the agent's own spans under ours needs the turn's trace context to reach the adapter per turn. Adapters are one process per conversation started with an environment, so `TRACEPARENT` can't change per turn. Spike: whether ACP's `_meta` on `session/prompt` can carry it to Claude Code's SDK and to Codex.

### A Lewbox collector (Later)

An OpenTelemetry Collector run by Lewbox (on each node, or in the control plane) that sandboxes send to and that forwards to each profile's endpoint, adding the headers itself, redacting, and fanning out. It would take the headers out of sandboxes. Not in the alpha.

## API

Under `/v0/projects/{prj}/profiles/{prf}`; owner, admin, member, setup session with `telemetry`, admin key with `profiles.credentials`:

| Method | Path | What |
|---|---|---|
| `GET` | `/telemetry` | `{ provider, settings (non-secret fields), endpoint, protocol, header_names, signals, content, last_check, updated_at }`, or `null` |
| `PUT` | `/telemetry` | Set or change |
| `DELETE` | `/telemetry` | Remove |
| `GET` | `/v0/telemetry/providers` | The registry |

### Errors

| Status | Code | When |
|---|---|---|
| `422` | `validation_failed` | Unknown provider; missing field; endpoint not http(s) or over 2048 chars; more than 20 headers; header names not tokens; no signal on |

## Security

- Settings and headers encrypted at rest, never returned (only header names), redacted from setup reports.
- **Readable in the sandbox**, as agent logins are: the setup page says to use an ingest-only key.
- The control plane never connects to the endpoint (no SSRF from customer input); the check runs from the sandbox, which is where the agents send from.
- `telemetry.env` isn't loaded by login shells, so the customer's own processes don't inherit the endpoint and headers by accident.
- **Never in a snapshot:** the headers live only on `/run/lewbox`, memory-only, and the path is in every snapshot's scrub list ([profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)). Nothing secret goes in Codex's `config.toml`.

## Observability

- Audit: `profile.telemetry_set`, `profile.telemetry_removed`.
- Setup report: the telemetry step's status and the check's result per sandbox.
- Metrics: `lewbox_telemetry_checks_total{provider,result}`.

## Testing

- Provider mapping: each provider's fields → endpoint, protocol, headers (golden tests).
- Setup step: env file contents and modes; percent-encoding of header values; nothing written to `config.toml`; a legacy `[otel]` block removed.
- agentd: Codex's exporter settings passed in `CODEX_CONFIG`, merged with instructions; a customer's own `[otel]` table untouched.
- agentd: resource attributes added per conversation; probe without telemetry.
- Check: fake OTLP receiver in the fake sandbox network accepting and refusing.
- Live before the alpha: Langfuse and Honeycomb receiving both agents' data (manual checklist).

## Build plan

The **`telemetry` plugin**, `plugins/telemetry/` ([plugins](../plugins.md)), on by default: the provider presets (Langfuse, Honeycomb, Grafana Cloud, LangSmith, custom OTLP) as data, storage and endpoints, its setup contributor (the files and env each agent reads, using the agent plugins' telemetry hooks), and its setup-page section; agentd's `withTelemetry` stays in the agent framework. Turned off, profiles keep their settings but sandboxes get no telemetry config and the endpoints answer `plugin_disabled`. Adding a provider preset is a data change in the plugin.

1. Registry and validation; table; endpoints.
2. Setup step ported from `TelemetrySetup`, writing `/run/lewbox/telemetry.env` for both agents, with the in-sandbox check.
3. agentd reading the file and adding attributes.
4. Setup-page component shared with the dashboard.
5. Stretch: Lewbox turn spans and `traceparent`.

## Open questions

1. **Lewbox's spans in the alpha?** Spike (M3): test whether ACP's `_meta` can carry a `traceparent` to both adapters; if it can, Lewbox's turn spans ship in the alpha, else in the Beta.
2. **Project-level default telemetry.** Deferred (Beta): one endpoint a project sets for all its profiles, with profiles able to override.
3. **A collector.** Deferred (Later): a Lewbox-run collector that keeps provider headers out of sandboxes.
4. **Codex's `[otel]` through `CODEX_CONFIG`.** Spike (M3): check that `codex-acp` applies an `otel` table from `CODEX_CONFIG`; if it doesn't, write legacy's block in `config.toml` and add the headers' file to the scrub manifest.
