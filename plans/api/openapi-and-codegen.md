# OpenAPI and code generation

> Status: Draft · Milestone: Alpha (M0 skeleton and CI; grows with every feature) · Updated: 2026-10-29

One OpenAPI document is the contract for `/v0`. The dashboard's client types, the TypeScript, PHP and Go SDKs' types, the reference docs, and the server's request and response types are generated from it or checked against it, and CI fails when any of them drift. This plan chooses how the document is written (spec-first), what's generated from it, and the checks that keep it honest.

## Today

- The gateway has a hand-written OpenAPI 3.1 document, `lewbox-gateway-legacy/api/openapi.yaml` (3,870 lines, version 0.3.0), covering the Consumer API. Nothing checks it against the server: `GET/PATCH /v1/conversations/{cnv}/tasks…` and `POST /v1/agent-uploads` are routed in `internal/server/server.go` but missing from it.
- The Admin API has no machine-readable contract; its docs are hand-written markdown (`lewbox-legacy/docs/admin/**`).
- The PHP SDK (`packages/lewbox-php-sdk`) is hand-written against both, and is copied into Lewagent by a Composer hook (`SyncLewboxSdkCommand`), so an API change needs two repositories changed in lockstep.
- The public docs (lewbox.com/docs) are hand-written with examples in four languages, which is where they shine: the prose and examples are excellent and worth keeping as the way people learn the API.
- Response shapes are defined four times (app resources ×3, gateway Go types), and drifted.

## Goals

- **One document**, covering every `/v0` route, every schema, every error code, every permission.
- **Drift is a CI failure**, not a review comment: routes, responses, errors and permissions are checked mechanically.
- **Generated where generation is good** (types, enums, error maps, reference pages), **hand-written where people read it** (SDK ergonomics, guides, examples).
- **Reviewable API design**: a change to the API shows up as a diff to the document, reviewed with its plan, before or with the code.

## Decision: spec-first

| Option | For | Against |
|---|---|---|
| **Spec-first** (write the OpenAPI document; generate types; hand-write handlers; test conformance) | The API is designed in review, in the language every consumer reads. Docs prose lives beside the schema. Four consumers (dashboard, TS, PHP, Go SDK) and the docs all start from one artifact. Streams, raw bodies and WebSockets don't fight a framework | YAML is verbose. Drift is possible, so it must be tested (below) |
| **Code-first** (e.g. [huma](https://huma.rocks): Go types and handlers produce OpenAPI 3.1) | No drift by construction for what the framework models | The framework owns routing, validation and errors, and Lewbox's interesting endpoints (SSE, WebSockets, streamed file bodies, `202`-or-`201` creates, idempotency) are the ones frameworks model worst. API review happens in Go diffs |
| **Generated server** (e.g. `ogen`: the document generates a full server with validation) | Strong typing end to end | Same streaming problems, more so; generated routers are hard to wrap with our middleware chain and principal metadata |

**We write the document by hand (spec-first), generate Go types from it, keep handlers hand-written on `net/http`, and prove conformance in tests.** The Go registries that are already the source of truth for errors and permissions generate their fragments of the document, so those can't drift either.

Which Go generator: a **spike in M0** compares `oapi-codegen` (types-only mode) and `ogen` (schemas only) against our document, on: OpenAPI 3.1 support (`type: [string, "null"]` for explicit nulls, `const`, `oneOf` with discriminators), output readability, `time.Time` and custom types (our `Z`-only time, ids), and generation speed. Fallback: hand-written Go types, with a test that marshals examples from the document and validates every response, which is enough on its own.

## Design

### Layout

```
api/
  openapi.yaml                  root: info, servers, security schemes, tags, $refs to paths
  paths/
    meta.yaml  auth.yaml  instance.yaml  projects.yaml  profiles.yaml  clusters.yaml
    sandboxes.yaml  inside.yaml  agents.yaml  templates.yaml  activity.yaml
    webhooks.yaml  git.yaml  plugins.yaml
  components/
    schemas/                    one file per resource: sandbox.yaml, operation.yaml, …
    parameters/                 cursor, per_page, path ids, Idempotency-Key, …
    responses/                  errors by status, generated
    events/                     SSE event schemas: turn events, command output, live changes
  generated/                    written by `go generate`, never by hand
    error-codes.yaml            from internal/httpapi/apierr
    permissions.yaml            from internal/auth
  webhooks.yaml                 OpenAPI 3.1 `webhooks`: every outbound event's payload
  internal/
    node.yaml  agentd.yaml      the internal surfaces, for contributors; not bundled into the public document
  dist/openapi.json             bundled public document, committed, embedded in the binary
```

The bundle is built by `make api` (Redocly CLI or `vacuum` bundling), committed so diffs are reviewable, and embedded with `go:embed` to serve `GET /v0/openapi.json`.

### Plugin fragments

Each plugin ([plugins](../plugins.md)) keeps its part of the contract beside its code, `plugins/<name>/openapi.yaml`: its paths, its schemas, its error codes and webhook events, and fields it adds to core schemas (`x-lewbox-extends: Sandbox` with the properties to add). The plugin generator ([plugins/building-and-enabling](../plugins/building-and-enabling.md#the-generator)) merges the core's document with every compiled-in plugin's fragment into the bundle:

- Each plugin operation is tagged with its plugin's name and marked `x-lewbox-plugin: <name>`, so docs group them and SDKs place them under the plugin's namespace.
- Every plugin operation gets `409 plugin_disabled` added to its responses.
- A clash (a path or schema name another document has) fails the merge.
- The bundle reflects the build: the default build's bundle is the one we publish and generate SDKs from; a custom build serves its own at `GET /v0/openapi.json`; the core-only build's has no plugin paths. CI lints all three and checks breaking changes on the default one.
- Route conformance ([below](#route-conformance)) compares the router's routes, plugins' included, with the merged document.

### What the document records

Beyond standard OpenAPI, each operation carries Lewbox extensions, which the route conformance test compares with the router's metadata:

| Extension | Holds |
|---|---|
| `x-lewbox-principals` | `[session, admin_key, profile_key, sandbox_key, setup_session]` |
| `x-lewbox-permission` | The permission, or `none`, or `handler` with the list it may need |
| `x-lewbox-idempotent` | `true` if it takes `Idempotency-Key` ([idempotency](idempotency.md)) |
| `x-lewbox-rate-limit` | Its class ([rate-limits](rate-limits.md)) |
| `x-lewbox-milestone` | `M1`…`Later`: operations not yet built are in the document, marked, and excluded from the published bundle until built |
| `x-lewbox-plan` | The owning plan, e.g. `sandboxes/lifecycle` |
| `x-lewbox-stream` | `sse` or `websocket`, and the events or messages schema |

**Streams.** OpenAPI 3.1 can't describe an event stream's items. SSE endpoints declare `text/event-stream` responses and point `x-lewbox-stream.events` at a `oneOf` of event schemas discriminated by `type`; WebSocket endpoints point at their message schemas. When tooling supports OpenAPI 3.2's `itemSchema` for sequential media types, we move SSE to it.

**Explicit nulls.** Every response property is `required`, and nullable ones are `type: [x, "null"]`, matching [conventions](conventions.md#responses).

**Examples.** Every operation has a request and response example, using the docs' fixed example ids (`sbx_Qm4pL0sX2bN8cV`, …). Tests validate examples against schemas.

### What's generated

| Artifact | From | How | Consumer |
|---|---|---|---|
| Go request and response types, enums | The document | Generator chosen in the spike; `internal/api/types` | The server's handlers; the Go SDK re-exports them |
| Error code enum and per-status responses | Go `apierr` registry | `go generate` → `api/generated/error-codes.yaml` | The document; then everything below |
| Permission enum | Go `auth` registry | `go generate` → `api/generated/permissions.yaml` | The document; dashboard key forms |
| Dashboard client types | Bundled document | `openapi-typescript` | `web/src/api/schema.d.ts`, used with `openapi-fetch` and TanStack Query ([dashboard](../dashboard.md)) |
| TypeScript SDK types | Bundled document | `openapi-typescript` | `sdks/typescript`, under hand-written classes (`Sandbox`, `Conversation`) |
| PHP SDK models | Bundled document | A small generator of our own (readonly classes with `fromArray`), or `jane-php` if good enough | `sdks/php`, under hand-written clients |
| Error class maps | Go `apierr` registry | `go generate` | Each SDK's `code → class` table |
| Reference docs | Bundled document | Scalar or Redoc, as static pages | lewbox.com/docs/api, next to the hand-written guides |
| `llms.txt` API section | Bundled document | A small script | AI coding agents reading the docs |

**Hand-written, on purpose**: SDK ergonomics (resource handles, waiting for operations, streaming iterators, retries, idempotency keys, auto-paging), the CLI, the guides and their examples in every language. Generated code is the floor those stand on, so a field added to the API appears in every SDK's types the same day.

### Where SDKs live

In this repository, under `sdks/`, released with versions of their own ([sdks-and-cli](../sdks-and-cli.md)). One repository means an API change and its SDK change are one pull request: the cross-repo copy hook Lewagent uses today goes away (Lewagent installs the published package).

### Route conformance

A Go test builds the real router and:

1. Lists every registered `/v0` route (method and pattern) with its metadata.
2. Lists every operation in the bundled document (including unbuilt ones, by milestone).
3. Fails if a built route isn't in the document, a documented route marked built isn't registered, or any `x-lewbox-*` value disagrees with the route's metadata.

The same for `/node/v0` and `/agentd/v0` against `api/internal/*.yaml`.

### Response validation

In integration tests, a middleware validates **every response** against the document (`kin-openapi`'s `openapi3filter` or `libopenapi-validator`): status listed for the operation, body matches the schema, required headers present, error code in the enum. A response that doesn't match fails the test that caused it. Requests the tests send are validated too, so test fixtures can't use undocumented fields. This runs in CI only, not in production.

### Linting

`vacuum` or Spectral with our ruleset, in CI:

- operationIds `camelCase`, unique; every operation tagged with its area.
- Properties snake_case; ids `pattern`-ed by prefix; times `format: date-time` and named `*_at`; durations integers named for seconds.
- Single resources wrapped in `data`; lists have `data` and `links.next` and take `cursor` and `per_page`.
- Every `4xx`/`5xx` response uses the generated error responses.
- Every operation has `x-lewbox-principals`, `x-lewbox-permission`, `x-lewbox-milestone`, `x-lewbox-plan`, a summary, a description and examples.
- `POST` creates that are idempotent declare the `Idempotency-Key` parameter.
- No `additionalProperties: true` on request bodies (we refuse unknown fields).

### Breaking changes

From the beta on ([conventions](conventions.md#versioning-and-deprecation)), CI runs `oasdiff breaking` between the bundled document and the last release's. A breaking change fails the build unless the pull request is labelled for `/v2` work. During the alpha it reports but doesn't fail, and the release notes list every breaking change it found.

### Generated files stay current

CI runs `go generate ./...` and `make api` and fails if anything changed (`git diff --exit-code`): the bundle, the generated YAML fragments, the Go types, the dashboard and SDK types.

---

## Build plan

M0:

1. Skeleton document: info, servers, security schemes (bearer, session cookie), shared parameters, error responses, the meta and health operations.
2. The generator spike, and the chosen generator wired into `go generate`.
3. Error and permission registries generating their fragments.
4. Bundling (`make api`), embedding, `GET /v0/openapi.json`.
5. Route conformance test, response validation middleware for tests, linter ruleset, generated-files check, all in CI.
6. `openapi-typescript` in the dashboard's build.

Then each feature's milestone writes its operations into the document with its plan, before or with its handlers.

## Testing

The checks above are the tests. Plus: every example in the document validates against its schema; the bundle round-trips through the TypeScript and PHP generators without warnings.

## Open questions

1. **Generator** (`oapi-codegen`, `ogen` or hand-written types). Spike (M0): generate the server types for three areas (sandboxes, files with streaming, conversations with SSE) with each; the one that handles OpenAPI 3.1, streaming bodies and our error shape with the least hand-written glue wins.
2. ~~**Reference docs.**~~ Decided (2026-10-29): served in both places, on lewbox.com/docs and by each instance at `/docs/api`, as a static page over `/v0/openapi.json`. The renderer (Scalar or Redoc) is picked in the M0 generator spike.
3. ~~**Internal surfaces in the document.**~~ Decided (2026-10-29): kept in separate documents, never in the public bundle.
4. **PHP model generation** (our own small generator or `jane-php`). Deferred (Beta), with the PHP SDK; decided with Lewagent's maintainers, its main user.
