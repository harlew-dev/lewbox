# API conventions

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

The rules every `/v0` endpoint follows, so a client that has used one endpoint can guess the rest. They're the conventions documented at [lewbox.com/docs/basics](https://lewbox.com/docs/basics/requests-and-responses), which legacy's gateway followed and its Admin API mostly followed, now written down once and enforced by shared code and the OpenAPI linter ([openapi-and-codegen](openapi-and-codegen.md#linting)).

## Today

- The gateway follows them (`lewbox-gateway-legacy/docs/api-conventions.md`, `internal/server/respond.go`).
- The Admin API follows them through Laravel API resources (`app/Http/Resources/Admin/*`), with differences: no `Lewbox-Request-Id` (planned, never built), Laravel's own pagination meta in places, validation messages in Laravel's style.
- The gateway↔app internal API returns plain JSON without `data`, a third style.
- GitHub repository and branch lists are paged by `page` number with a `meta` object, the only lists that are.
- Times: the docs say "RFC 3339, in UTC, to the second"; the gateway writes `+00:00`, some app resources write `Z` or microseconds.

## Goals

- One style for every endpoint, enforced by code (one response package) and by CI (the linter), not by review.
- Keep the documented conventions; fix the inconsistencies.

## Keep, change, drop

| | |
|---|---|
| **Keep** | JSON, snake_case, `data` envelope, `links.next` cursors, explicit nulls, RFC 3339 UTC, prefixed ids, durations in seconds, resource-shaped methods, unknown request fields refused |
| **Change** | Times are always `Z`, never `+00:00`, and never fractional, except where a field says milliseconds (agent events' `at`) |
| **Change** | GitHub lists use cursors like every other list |
| **Change** | `Lewbox-Request-Id` on every response, from every surface, including errors before authentication |
| **Drop** | Plain JSON without `data` (the internal API is gone) |

---

## Design

### Base URL and hosts

- The API is at `https://<instance>/v0`, e.g. `https://api.lewbox.com/v0`. The dashboard is on the same origin, at `/` ([api](../api.md#surfaces)).
- Links in responses (`links.next`, `url`s Lewbox makes) use the scheme and host the request came in on, honouring `X-Forwarded-Proto` and `X-Forwarded-Host` only from trusted proxies (`LEWBOX_TRUSTED_PROXIES`, CIDRs). So a provider serving Lewbox from its own domain never leaks the canonical one.
- `LEWBOX_PUBLIC_URL` is the canonical URL, used where there's no request: emails, webhooks' links, agentd's upload URL, the install command.

### Requests

- **Bodies are JSON** with `Content-Type: application/json` (charset optional, UTF-8 only). Other content types on a JSON endpoint: `415 unsupported_media_type`.
- **Keys are snake_case.** Request field names match response field names.
- **Unknown fields are refused** with `422 validation_failed` on that field ("This field isn't accepted here"). Strictness catches typos (`idle_timout`) that would otherwise silently do nothing. SDKs never send fields the server doesn't know, because they're generated from the same document.
- **A body that isn't JSON**, or is the wrong type at the top level, is `422 validation_failed` on `body`.
- **Empty bodies:** an endpoint whose fields are all optional takes no body, `{}`, or `null` alike.
- **Durations** are whole seconds (`idle_timeout`, `timeout`, `lifetime`). Only the TypeScript SDK converts to milliseconds.
- **Sizes** carry their unit in the name: `memory_mb`, `disk_gb`, `size_mb`, `max_bytes`. `cpu` is in vCPUs, as a number with up to 3 decimals (stored as millicores).
- **Money and credits** are decimal strings, e.g. `"12.50"`, never floats ([credits](../credits.md)).
- **Booleans** are JSON booleans; query parameters take `true`/`false` (and `1`/`0`).
- **Query parameters** for lists: snake_case; repeated or comma-separated for several values (`kind=a,b` or `kind=a&kind=b`); maps as `metadata[key]=value`.
- **Path parameters** that name a cluster take its id or its name (`clu_…` or `eu-west`); a template takes its slug. Everything else takes an id.
- **Methods are resource-shaped:**

| Method | Does | Answers |
|---|---|---|
| `GET` | Reads one, or a list | `200` |
| `POST` | Creates one in a collection, or creates an action-shaped resource (`…/tests`, `…/redeliveries`, `…/secret-rolls`) | `201` with it; `202` with an operation when the work carries on |
| `PATCH` | Changes some fields, including `status` | `200` with it; `202` when it starts an operation |
| `PUT` | Creates or replaces one at a known path (`ports/{port}`, `ssh-keys/{name}`, `limits`, singletons) | `200` with it (`201` if it was created) |
| `DELETE` | Deletes one | `204`; `202` with an operation when the work carries on |

- **No verbs in paths.** Starting a sandbox is `PATCH {status: running}`. Where an action has no natural resource, it becomes one: a `test`, a `redelivery`, a `secret-roll`, an `operation`. The SDKs give them natural names.
- **`PATCH` semantics:** a field that's present is changed; a field that's absent is left alone; `null` clears it, or for a limited setting means "the limit" (`idle_timeout: null`, [sandboxes](../sandboxes.md)). Nested objects (`metadata`) are replaced whole unless the field's docs say they're merged.

### Responses

- **JSON**, snake_case, `Content-Type: application/json`.
- **One resource is wrapped in `data`:** `{"data": {...}}`. **A list** is `{"data": [...], "links": {"next": "…" | null}}`.
- **Nulls are explicit.** Every documented field is present; no value is `null`. The one exception is `errors` in an error body, present only on `422` ([errors](errors.md)).
- **Embedded references** are small objects, never bare ids where the client would need a second call to show something: `"cluster": {"id": "clu_…", "name": "eu-west"}`, `"profile": {"id": "prf_…", "name": "Ada", "external_id": "usr_8812"}`. No `?expand=`: each resource decides what it embeds, once.
- **Write-only fields** (`env`, secrets, credentials) are never returned. Where a client needs to know one is set, there's a companion field: `"has_secret": true`, `"secret_last_four": "a9f2"`.
- **Secrets shown once** (keys, webhook secrets) are returned in the create response only, under `token` or `secret`, and the resource says so.
- **Enums are open.** Fields like `status`, `kind`, `step` and error codes may gain values; the docs say which, and clients must handle an unknown one (by its status class for codes, as "other" for states).
- **`204`** has no body. **`202`** returns the resource with its `operation` ([sandboxes](../sandboxes.md)).

### Times

- RFC 3339, UTC, `Z`, to the second: `2026-10-29T18:40:12Z`.
- Fields end in `_at` (`created_at`, `expires_at`). Dates without times end in `_on` (none yet).
- Fields that need milliseconds say so and use them: agent events' `at` (`2026-10-29T18:40:12.345Z`), `duration_ms`.
- Accepted on input in any RFC 3339 offset; stored and returned in UTC.

### Ids

- `<prefix>_` + 14 base62 characters, random, so they reveal neither order nor count ([foundation/ids-and-keys](../foundation/ids-and-keys.md)).
- Ids are globally unique and enough to find a resource: no cluster, project or profile is ever needed beside one.
- Exceptions: templates (a slug chosen by their maker), clusters (also have a `name`), messages are numbered within a conversation as `turn` besides their `msg_` id, and agent events are numbered within a conversation.
- `req_` + 24 hex for request ids, which aren't resources.

### Pagination

Every list is paged by cursor.

- `per_page`: 1 to 100, default 50. Out of range is `422 validation_failed`.
- `links.next` is the next page's full URL, with the same filters and a `cursor`, or `null` on the last page. Clients follow it as it is, with the same credentials; they never build cursors.
- **Cursors are opaque**: base64url of a small JSON position (`{"k": [<sort key>, <internal id>], "v": 1}`), HMAC-signed so a client can't forge positions into rows it couldn't otherwise reach (it couldn't read them anyway, but signing keeps cursors stable as an interface). A cursor from another list or another principal is `422 validation_failed` on `cursor`.
- **Keyset pagination**, never offsets: each list has a fixed order and a unique tiebreaker (the internal bigint id), so pages don't skip or repeat rows when rows are added. Since public ids are random, the order is by a time column plus the internal id.
- **Order** is fixed per list and documented: sandboxes oldest first (as documented), activity and operations newest first, conversations most recently active first. No `sort` parameter until someone needs one.
- **No totals.** Counting is expensive and racy. Where a count is useful (dashboard badges) the resource that owns it carries it (`"running_sandboxes": 3` on a profile).
- **Lists that come from elsewhere** (GitHub repositories, branches): the cursor wraps the upstream page and position; `per_page` is passed through where the upstream allows. `search` filters by name.
- `?after=<n>` on numbered streams (agent events, messages by turn) is not a cursor: it's a position in a sequence clients already know ([streaming](streaming.md)).

### Filtering

- Lists take filters as query parameters named after the field they filter: `status`, `cluster`, `profile`, `kind`.
- `metadata[key]=value`: exact string match, up to 10, all must match.
- Times: `since` (inclusive) and `until` (exclusive).
- A filter value that can't match anything the principal reaches (a cluster the key doesn't reach) gives an empty list, not an error, as documented.
- Text search, where offered, is `q`, case-insensitive, substring.

### Request ids

- Every response from every surface carries `Lewbox-Request-Id: req_<24 hex>`, made by the server. A client-sent one is ignored (a client can't choose ids that appear in logs and activity).
- Clients may send W3C `traceparent`; it's honoured for tracing only, linking the request's spans to the client's trace ([foundation/observability](../foundation/observability.md)).
- The id is logged on every line of the request's work, carried into jobs it enqueues (`request_id` on the job), recorded on activity events, and shown by the SDKs and CLI with every error.

### Versioning and deprecation

- **The version is in the path.** The API starts at `/v0`, which is **not stable**: we may make breaking changes while we learn, each listed in the changelog and the OpenAPI diff, and the SDKs pin minor versions so an upgrade is a deliberate step (decided 2026-10-29).
- **`/v1` comes once the API is stable.** From then on a version only grows: new endpoints, new optional request fields, new response fields, new enum values, new error codes. Nothing is removed, renamed or changes meaning without `/v2`, enforced by a breaking-change check in CI ([openapi-and-codegen](openapi-and-codegen.md#breaking-changes)). When `/v1` ships, `/v0` keeps answering for a deprecation period, so clients move on their own schedule.
- **Internal surfaces** (`/node/v0`, `/agentd/v0`) are versioned the same way, but follow the binary: the node agent and agentd are upgraded with the control plane, and a control plane answers the previous version for one release.
- **Deprecating** a field or endpoint: it's marked `deprecated: true` in the document, responses that use it carry `Deprecation: @<unix time>` (RFC 9745) and, once a removal date is set, `Sunset: <date>` (RFC 8594) and a `Link: <docs>; rel="deprecation"`. The access log counts deprecated use per key, so we can tell projects before removing anything in `/v2`.
- `/v0/meta` answers the server's version, the API's `min_sdk_version`, so SDKs can warn when they're too old, and the plugins compiled in with whether each is on, so clients know which plugin operations exist on this instance ([plugins](../plugins.md#api)). A plugin's operations are part of `/v0` and follow the same rules; a plugin that's off answers `409 plugin_disabled`.

### Content types

| Content | Type |
|---|---|
| Resources, errors | `application/json` |
| File downloads and uploads | `application/octet-stream` (downloads also `Content-Disposition: attachment; filename="…"`) |
| Session logs | `text/plain; charset=utf-8` |
| Streams | `text/event-stream` ([streaming](streaming.md)) |
| WebSockets | `Upgrade: websocket` ([streaming](streaming.md)) |
| The dashboard | `text/html`, assets by extension |

Responses may be compressed (`gzip`, `zstd`) when the client asks; streams and file downloads aren't, so their bytes flow as they're written.

### Limits on requests

| What | Limit | Error |
|---|---|---|
| JSON body | 1 MiB by default; per endpoint where larger (a Dockerfile's 64 KiB fits; agent prompts with images up to 20 MiB) | `413 payload_too_large` |
| File uploads | Streamed; no limit in the API itself, the sandbox's disk is the limit; a per-project cap is configurable | `413 payload_too_large` |
| Headers | 64 KiB | `431` (Go's default handling) |
| URL | 8 KiB | `414` |
| Header read timeout | 10 s | connection closed |
| Long waits in a request | Up to 60 s (a create waiting for its sandbox) | `202` instead |

There's no timeout on a whole response, so downloads and streams run as long as they need. Anything that can take longer than a minute is an operation or a stream, never a request held open: a lesson from legacy's host cutting requests at about 20 s ([lessons](../lessons.md)).

### CORS

Two rules, because there are two kinds of credentials:

1. **Bearer credentials (keys)** work from any origin. Keys travel in a header, never a cookie, so a page can only use a key it already has. Preflights answer:
   - `Access-Control-Allow-Origin: <the request's Origin>` (not `*`, so it works with `Vary: Origin` caching),
   - `Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE`,
   - `Access-Control-Allow-Headers: Authorization, Content-Type, Idempotency-Key, X-Lewbox-Label, Last-Event-ID, If-Match, If-None-Match, If-Unmodified-Since, traceparent`,
   - `Access-Control-Expose-Headers: Lewbox-Request-Id, Idempotent-Replayed, Retry-After, RateLimit, RateLimit-Policy, Content-Disposition, Content-Length, ETag, Location, Deprecation, Sunset`,
   - `Access-Control-Max-Age: 7200`,
   - no `Access-Control-Allow-Credentials`.
   Adding `Last-Event-ID` to the allowed headers fixes the legacy gap that pushed Lewagent to `?after=` ([streaming](streaming.md#resuming)).
2. **Cookies (sessions)** work only from the instance's own origin. Responses never send `Access-Control-Allow-Credentials`, so a browser won't send the session cookie cross-origin with CORS; `SameSite=Lax` stops it on cross-site subrequests; and the CSRF check stops the rest ([authentication](authentication.md#csrf)). A request carrying both a cookie and a bearer key is authenticated by the key, and the cookie is ignored.

The internal surfaces (`/node/v0`, `/agentd/v0`, `/hooks`) answer no CORS at all.

### Labels

Clients may label work they ask for, so their activity log reads well: a `label` field on commands, sessions and agent prompts, or the `X-Lewbox-Label` header on any request inside a sandbox. Labels match `^[a-z0-9][a-z0-9._-]{0,63}$` (e.g. `git.push`, `gh.pr.create`). An invalid label is `422 validation_failed` on `label` ([activity-and-webhooks](../activity-and-webhooks.md)).

---

## Build plan

`internal/httpapi/respond`:

- `JSON(w, status, v)`, `Data(w, status, v)`, `List(w, page)`, `NoContent(w)`, `Accepted(w, v)`.
- `Decode(r, &v)`: strict JSON (`DisallowUnknownFields`, single value, size limit), mapping decode errors to field errors.
- `Page` type: the cursor codec (signed with a key derived from the keyring, [foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), `per_page` parsing, `links.next` from the request URL.
- Time type that marshals as `Z` to the second.
- `Link(r, path)`: absolute URLs from the trusted request host.

Tasks: respond package; strict decoder with field-error mapping; cursor codec and keyset helpers for sqlc queries; filters parser (`metadata[k]`, lists, times); CORS middleware; body limit middleware; trusted proxy handling; tests for each.

## Testing

- Golden tests for the envelope, lists, nulls and times.
- Fuzz the strict decoder and the cursor codec.
- A linter rule per convention ([openapi-and-codegen](openapi-and-codegen.md#linting)): snake_case properties, `data` envelopes, `_at` times with `format: date-time`, every list has `per_page` and `cursor` and `links`.

## Open questions

1. Should list responses carry a `has_more` boolean besides `links.next`? (Proposed: no; `next` being `null` says it.)
2. Do we want `ETag`s on every resource (for `If-None-Match` caching by the dashboard), or only on files? (Proposed: files only, for conditional writes; revisit if the dashboard's polling hurts.)
3. Prompts with images: base64 in JSON (as ACP content blocks are) with a 20 MiB body limit, or upload to the sandbox first and reference a path? (Proposed: both, [agents](../agents.md).)
