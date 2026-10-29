# API

> Status: Draft · Milestone: Alpha (M0 for the framework; each route's own milestone below) · Updated: 2026-10-29

Lewbox has one HTTP API, on one host, under one version, `/v0`. Project servers manage their projects through it, profiles create and use sandboxes through it, browsers reach one sandbox through it, and the dashboard is just another client of it. This plan sets the API's shape: its surfaces, the principals that call it, the rule for where each resource lives, and a map of **every route** across every plan, with the plan that owns it. The parts go into conventions, errors, authentication, permissions, idempotency, rate limits, streaming and the OpenAPI contract.

## Parts

| Part | Covers |
|---|---|
| [conventions](api/conventions.md) | JSON, the `data` envelope, ids, times, nulls, cursor pages, filters, request ids, versioning and deprecation, CORS, bodies and limits |
| [errors](api/errors.md) | The error body, and the full catalogue of codes: kept, renamed, new and removed |
| [authentication](api/authentication.md) | Principals and token kinds (`lba_`, `lbp_`, `lbs_`, `lbn_`, `lbi_`, `lbl_`), dashboard sessions, CSRF, WebSockets, key lookup, revocation and expiry, signed tokens |
| [permissions](api/permissions.md) | Every permission, which principals can hold it, admin key scopes, roles, and sandbox key rules |
| [idempotency](api/idempotency.md) | `Idempotency-Key`: which endpoints, fingerprints, replays, races, storage |
| [rate-limits](api/rate-limits.md) | Limits per key without Redis, headers, defaults |
| [streaming](api/streaming.md) | SSE and WebSocket conventions, resuming, keepalives, close codes, cross-replica fan-out, shutdown |
| [openapi-and-codegen](api/openapi-and-codegen.md) | The OpenAPI 3.1 contract, generated clients and types, CI drift checks, reference docs |

---

## Today

Legacy Lewbox has **four HTTP APIs on two hosts**, plus dashboard routes:

| API | Served by | Path | Called with | Code |
|---|---|---|---|---|
| Consumer API | Gateway (Go) | `/v1` on `api.lewbox.com` | Profile key `lbp_`, sandbox key `lbs_` | `lewbox-gateway-legacy/internal/server/server.go`, `api/openapi.yaml` (3,870 lines) |
| Admin API | App (Laravel) | `/api/v1` on `box.harlew.com` | Tenant key `lbt_` | `lewbox-legacy/routes/api.php`, `app/Http/Controllers/Api/v1/*` |
| Gateway API (internal) | App | `/api/gateway/v1` | One shared static token, `LEWBOX_GATEWAY_TOKEN` | `app/Http/Controllers/Api/Gateway/v1/*`, the gateway's `internal/lewbox/client.go` (~50 methods) |
| Node API (internal) | App | `/api/node/v1` | Installation token, then node token | `app/Http/Controllers/Api/Node/v1/*` |
| Dashboard | App | `/t/{tenant}/…` | Session cookie (Inertia) | `routes/web.php` (128 routes) |
| Setup pages | App | `/profile/{token}` | Setup link, then a session | `app/Http/Controllers/SetupLink/*` |
| GitHub webhooks | App | `/api/github/v1/apps/{tenant}/webhooks` | `X-Hub-Signature-256` | `Api/GitHub/v1/WebhookController.php` |

What that costs:

- **Two base URLs, two kinds of key, one feature.** Each API refuses the other's key (`401 wrong_key_kind`). Lewagent's Git panel needs both: pushes, pull requests and Git grants go through the Admin API with the tenant key and a `profile_id`, while commands, files and conversations go through the Consumer API with a per-user profile key (`lewagent-legacy/app/Http/Controllers/Computer/PushController.php` vs `resources/js/lib/git.ts`). Lewagent stores both keys and configures both URLs (`LEWBOX_ADMIN_URL`, `LEWBOX_API_URL`).
- **The tenant can't work inside its own customers' sandboxes.** The Admin API can't create sandboxes or run commands, so the app mints *sandbox keys for itself* and calls the gateway's public API through the PHP SDK (`app/Actions/Sandboxes/ConnectToSandbox.php`). Those keys appear in customers' activity logs.
- **Four serialisation layers for the same entities**: `Http/Resources/Admin/*`, `Http/Resources/*` (dashboard), `Http/Resources/Gateway/*`, and the gateway's Go types, plus a hand-written OpenAPI spec that has drifted from the server (it's missing `GET/PATCH /v1/conversations/{cnv}/tasks…` and `POST /v1/agent-uploads`). `external_id` was missing on public chunks until someone noticed.
- **Two error catalogues** (the gateway's `docs/api-conventions.md` and the app's `docs/admin/basics/errors.md`), mostly the same, not quite.
- **The dashboard isn't an API client.** 128 Inertia routes duplicate the Admin API's behaviour with their own controllers and validation (`Tenant/*` vs `Api/v1/*`). Every feature is built twice, or exists in only one place: the dashboard can create sandboxes, the Admin API can't. Webhook settings, GitHub App registration, sign-in clusters and credit rates exist only in the dashboard.
- **Internal APIs exist only because of the split.** The ~30 gateway→app endpoints (`whoami`, `operations` polling, `sandbox-events`, `agent-chunks`, …) go away in one binary ([README](README.md#goals)).

## Goals

1. **One API, one host, one version.** `https://<instance>/v0`. A project's servers, its customers' servers, browsers and the dashboard all call the same endpoints.
2. **One path per resource.** Who may reach a resource depends on the principal, not on which API it called. No "admin" and "consumer" copies of the same sandbox.
3. **The dashboard is an API client.** Everything the dashboard does, an admin key can do (apart from the few things that must stay human: [below](#what-stays-human)).
4. **One error catalogue, one OpenAPI document,** generated or checked in CI, so docs, SDKs, the dashboard and the server agree ([openapi-and-codegen](api/openapi-and-codegen.md)).
5. **Keep what worked.** The conventions in [lewbox.com/docs](https://lewbox.com/docs) are good and users already read them: resource-shaped endpoints, the `data` envelope, explicit nulls, stable error codes, operations with `202`, idempotency keys, cursor pages, the SSE and WebSocket protocols. We keep them, and change paths and names only where there's a reason.
6. **Internal callers get their own surfaces.** Nodes and agentd aren't API clients; their routes are versioned with the binary and aren't in the SDKs.

### Non-goals

- Compatibility with the legacy paths ([D4](README.md#decisions)). Lewagent moves to the new API ([migration](migration.md)).
- GraphQL, gRPC, or a second protocol. JSON over HTTPS, with SSE and WebSockets for streams.
- A public API for instance administration in the alpha: instance admins use the dashboard (sessions only).

## Keep, change, drop

| | What | Why |
|---|---|---|
| **Keep** | Every Consumer API route and its request and response fields, as documented on lewbox.com | It's the best-designed part of legacy, and documented |
| **Change** | Start, stop and restart only through `POST /v0/sandboxes/{sbx}/operations` (`kind`), not `PATCH {status}` | One way to do it, answering with the operation; decided 2026-10-29 |
| **Keep** | `{message, code, errors}`, `Lewbox-Request-Id`, `Idempotency-Key`, `Idempotent-Replayed`, `links.next`, operations and `202` | Documented, used by Lewagent and the PHP SDK |
| **Keep** | The command WebSocket, session attach, SSE turn events, `lewbox.bearer` subprotocol | Protocols browsers already speak |
| **Change** | Admin API `/api/v1` on the app's host → management routes under `/v0/projects/{prj}` on the one host | One base URL ([decision A1](#decisions)) |
| **Change** | Tenant key `lbt_` → admin key `lba_`, and admin keys **can** act on sandboxes, inside them too, in any profile of their project | Removes the dual auth path and the app's self-minted sandbox keys ([A3](#decisions)) |
| **Change** | Dashboard routes `/t/{tenant}/…` (Inertia) → the React SPA calling `/v0` with a session cookie | One implementation of each feature ([A4](#decisions)) |
| **Change** | Setup pages `/profile/{token}` (Blade/Inertia) → SPA pages calling `/v0/profile/…` with a setup session | Same handlers as the Admin API ([A5](#decisions)) |
| **Change** | `POST /v1/agent-uploads` → events and uploads over the agent channel, `/agentd/v0/channel` | Not a public endpoint; one connection both ways ([A7](#decisions)) |
| **Change** | `gateway_restarting` → `server_restarting`; `app_unavailable` → `service_unavailable` | There's no gateway or app any more ([errors](api/errors.md#catalogue)) |
| **Change** | GitHub repository and branch lists paged by `page` number → cursor pages like every other list | One way to page ([conventions](api/conventions.md#pagination)) |
| **Add** | Instance routes `/v0/instance/…`, project and member routes, sessions and sign-in | Things that were dashboard-only |
| **Add** | `GET /v0/sandboxes/{sbx}/operations` (list), `GET /v0/conversations/{cnv}/messages/{msg}` (one turn without the stream), `GET /v0/events` (live changes) | Lewagent polls for these today ([streaming](api/streaming.md#live-changes)) |
| **Drop** | The gateway API `/api/gateway/v1` and the gateway's service token | Function calls now |
| **Drop** | `/api/` prefix, `/t/{tenant}` prefix | One host, the SPA owns `/` |

---

## Design

### Surfaces

One process serves every surface, on one listener (metrics optionally on a second, [foundation/observability](foundation/observability.md)).

| Surface | Prefix | Called by | In the SDKs | Versioned with |
|---|---|---|---|---|
| **The API** | `/v0` | Sessions, admin keys, profile keys, sandbox keys, setup sessions | Yes | The API (`/v0`, additive changes only once stable) |
| **Node API** | `/node/v0` | `lewbox node` with an install token or a node token | No | The binary: the control plane serves the node agents it supports ([clusters/node-agent](clusters/node-agent.md)) |
| **agentd API** | `/agentd/v0` | `lewbox agentd` inside sandboxes, with a signed conversation token | No | The binary: agentd is embedded in the server, so they ship together |
| **Inbound hooks** | `/hooks` | GitHub (and later others), signed by the sender | No | The sender's format |
| **Browser flows** | `/auth/…`, `/github/…` | Browsers following redirects (OAuth, GitHub App manifests, installs) | No | — |
| **Downloads** | `/install.sh`, `/downloads/…` | Servers installing a node | No | The binary |
| **Health** | `/up`, `/ready`, `/metrics` | Load balancers, Prometheus | No | — |
| **Dashboard** | everything else, `/` | Browsers: the embedded React SPA, falling back to `index.html` | No | The binary |

Private-port URLs (`https://<sandbox>-<port>.node-<id>.<domain>`) are served by the node's Caddy and `lewbox portd`, not by the control plane ([inside-a-sandbox](inside-a-sandbox.md)). The docs site (lewbox.com/docs) is separate from the binary ([sdks-and-cli](sdks-and-cli.md)); the binary serves its own contract at `GET /v0/openapi.json`.

### Principals

Every request to `/v0` is made by one principal. [Authentication](api/authentication.md) says how each is recognised; [permissions](api/permissions.md) says what each may do.

| Code | Principal | Token | Belongs to | Reaches |
|---|---|---|---|---|
| **S** | User session | `__Host-lewbox_session` cookie | A user | Every project the user is a member of, as their role allows. Instance routes if they're an instance admin |
| **A** | Admin key | `lba_…` | A project | Its project: management routes, and every profile's sandboxes, on every cluster |
| **P** | Profile key | `lbp_…` | A profile | Its profile's sandboxes on the clusters it reaches; its own profile (read, default cluster, repositories) |
| **K** | Sandbox key | `lbs_…` | A sandbox (minted by S, A or P) | One sandbox, inside it, for 15 minutes |
| **L** | Setup session | `__Host-lewbox_setup` cookie, from an `lbl_…` link | A profile | That profile's settings, in the sections the link was made for, for up to an hour |

Internal principals, on their own surfaces: **N** node token (`lbn_`), **I** install token (`lbi_`), **G** agentd token (signed, Ed25519).

### Where resources live

One rule decides every path:

1. **A resource that belongs to a profile, with a globally unique id, lives at the top level:** `/v0/sandboxes/{sbx}`, `/v0/conversations/{cnv}`, `/v0/templates/{tpl}`. A sandbox's id says which project, profile and cluster it's in, so the path needs nothing else, and every principal that reaches it uses the same path.
2. **A resource that belongs to the project lives under it:** `/v0/projects/{prj}/profiles`, `…/clusters`, `…/webhook-endpoints`. Admin keys reach only their own project; another project's id is `404`. Sessions reach every project the user belongs to. Putting `{prj}` in the path, rather than finding it from the key, lets one user's session work across projects with the same paths an admin key uses, and makes logs and links unambiguous.
3. **`/v0/profile` is an alias** for `/v0/projects/{prj}/profiles/{prf}`, the calling profile's own, for profile keys and setup sessions, which don't need to know their project's id. The router mounts the same handlers at both; the principal decides which sub-resources it may reach ([permissions](api/permissions.md#profile-keys)).
4. **Instance-wide resources live under `/v0/instance`**, for instance admins.

**Lists at the top level** (`GET /v0/sandboxes`, `/v0/conversations`, `/v0/templates`, `/v0/activity`, `/v0/events`) cover what the principal reaches: a profile key its profile's (on its clusters), an admin key its project's (filter with `profile=`), a session the project named by `project=` (required: `422 project_required` without it, since a user may belong to several).

### What stays human

A few things only a signed-in user can do, so a leaked key can't make itself permanent or take a project over:

- creating and revoking **admin keys**;
- inviting, changing and removing **members**; transferring or deleting a **project**;
- registering the project's **GitHub App** (it's a browser redirect flow anyway);
- everything under `/v0/instance`.

Admin keys get `403 wrong_key_kind` on these, with a message saying to use the dashboard.

### Decisions

| # | Decision | Alternatives considered |
|---|---|---|
| A1 | **One host, `/v0`, for all public routes.** Management under `/v0/projects/{prj}`. | Keep two APIs (refused: the dual-path pain above). `/v0/admin/…` (refused: the path would say who's calling, not what the resource is). |
| A2 | **`{prj}` in management paths**, not implied by the key. | A `Lewbox-Project` header (Stripe-style). Works, but invisible in logs and links, and awkward in a browser. |
| A3 | **Admin keys reach every sandbox in their project, and act inside them.** Creating a sandbox with an admin key needs `profile` (`422 profile_required` without it); the sandbox belongs to that profile, counts against its limits, and the activity log shows the admin key as actor. | Admin keys mint profile keys or sandbox keys to act (refused: that's what legacy does, and the keys pollute logs and add a hop). |
| A4 | **The dashboard uses `/v0` with a session cookie** and CSRF protection, same origin. | A separate BFF API for the SPA (refused: two implementations again). |
| A5 | **Setup pages use `/v0/profile/…` with a setup session.** | Their own routes (refused: legacy duplicates every profile setting this way). |
| A6 | **Profile-owned resources at the top level**, project-owned under the project. | Everything under `/v0/projects/{prj}` (refused: profile keys and browsers would need the project id for every call, and the public docs' paths would all change for nothing). |
| A7 | **Internal callers get their own prefixes** (`/node/v0`, `/agentd/v0`), not in the OpenAPI document the SDKs are built from. | Mixing them into `/v0` (legacy put agent uploads there, and they weren't in the spec). |
| A8 | **No instance keys in the alpha.** Instance administration is sessions only. | `lbx_` instance keys (open question 3). |
| A9 | **Plugins' routes are part of the one API** ([plugins](plugins.md)): same host, same `/v0`, same principals, middleware, errors, pagination and OpenAPI document (merged from each plugin's fragment). A plugin that's off answers its routes with `409 plugin_disabled`; a plugin not in the build has no routes (`404`). | A prefix per plugin (`/v0/plugins/credits/…`; refused: a plugin's resources belong where their owners are, e.g. a profile's credit buckets under the profile). |

---

## Route map

Every route Lewbox will serve, across every plan. The owning plan has the detail; this map is the index, and the OpenAPI document must match it ([openapi-and-codegen](api/openapi-and-codegen.md#route-conformance)).

**Who**: the principals that may call it ([codes above](#principals)). A principal not listed gets `403 wrong_key_kind` (or `404` where it can't see the resource at all, [authentication](api/authentication.md#wrong-principal)). **Permission**: what the principal needs ([permissions](api/permissions.md)); `—` means none beyond reaching the resource. Sessions need the permission through their role. **M**: milestone (M0–M4 are the alpha; Beta; Later).

`{prj}` project, `{prf}` profile, `{sbx}` sandbox, `{cnv}` conversation, `{msg}` message, `{tpl}` template id, `{clu}` cluster (id or name), `{node}` node, `{plugin}` a plugin's name (with its slash: `git/github`), `{provider}` a provider plugin's id (`github`).

**Routes owned by plugins** are listed with the rest, their plan naming the plugin. A route of a plugin that's off for the instance or the request's project answers `409 plugin_disabled`; a plugin not in the build has none of its routes. The plugins in the default build and what they own: `limits` (`…/limits`), `credits` (`…/usage`, `…/credit-*`, `…/settings/credits`), `git/github` (`…/github-app…`, `…/github/…`, `/hooks/github/{prj}`), `telemetry` (`…/telemetry…`, `/v0/telemetry/providers`), `agents/*` (their credential methods, sign-in flows and options, served through the core's agent routes), `dns/libdns` and `storage/s3` (their schemas, through the core's cluster routes) ([plugins](plugins.md#the-default-plugins)).

### Health and meta

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/up` | anyone | — | M0 | [foundation/observability](foundation/observability.md) |
| GET | `/ready` | anyone | — | M0 | [foundation/observability](foundation/observability.md) |
| GET | `/metrics` | anyone with the metrics token, or the metrics listener | — | M0 | [foundation/observability](foundation/observability.md) |
| GET | `/v0/meta` | anyone | — | M0 | this plan: instance name, version, sign-in methods, features, API URL, and `plugins` (each compiled plugin's name, kind, title, version, and whether it's on for the instance) ([plugins](plugins.md#api)) |
| GET | `/v0/openapi.json` | anyone | — | M0 | [openapi-and-codegen](api/openapi-and-codegen.md) |

### Sign-in and the current user

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| POST | `/v0/auth/codes` | anyone | — (rate limited by email and IP) | M1 | [projects/users-and-login](projects/users-and-login.md) |
| POST | `/v0/auth/sessions` | anyone | — (email and code) | M1 | [projects/users-and-login](projects/users-and-login.md) |
| GET | `/v0/auth/sessions` | S | — | M1 | [projects/users-and-login](projects/users-and-login.md): the user's own sessions |
| DELETE | `/v0/auth/sessions/{session}` | S | — (`current` signs out) | M1 | [projects/users-and-login](projects/users-and-login.md) |
| GET | `/v0/me` | S | — | M1 | [projects/users-and-login](projects/users-and-login.md) |
| PATCH | `/v0/me` | S | — (name; email needs a code) | M1 | [projects/users-and-login](projects/users-and-login.md) |

### Instance

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET, PATCH | `/v0/instance` | S (instance admin) | — | M1 | [projects](projects.md): name, who may create projects, defaults |
| GET | `/v0/instance/projects` | S (instance admin) | — | M1 | [projects](projects.md) |
| PATCH | `/v0/instance/projects/{prj}` | S (instance admin) | — (suspend, unsuspend) | M1 | [projects](projects.md) |
| GET | `/v0/instance/users` | S (instance admin) | — | M1 | [projects](projects.md) |
| PATCH | `/v0/instance/users/{usr}` | S (instance admin) | — (disable, make admin) | M1 | [projects](projects.md) |
| GET, POST | `/v0/instance/clusters` | S (instance admin) | — | Later | [clusters](clusters.md): shared clusters (README open question 1) |
| GET | `/v0/instance/plugins` | S (instance admin) | — | M0 | [plugins/building-and-enabling](plugins/building-and-enabling.md#api): read-only (instance enablement is configuration only); every compiled plugin, its instance settings (redacted), projects mode, dependencies and use |

### Projects and members

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/v0/projects` | S, A | — (A sees its one project) | M1 | [projects](projects.md) |
| POST | `/v0/projects` | S | — (if the instance lets users create projects) | M1 | [projects](projects.md) |
| GET | `/v0/projects/{prj}` | S, A | — | M1 | [projects](projects.md) |
| PATCH | `/v0/projects/{prj}` | S, A | `project.write` | M1 | [projects](projects.md): name, settings (retention, sign-in cluster, sandbox Git cap, rates) |
| DELETE | `/v0/projects/{prj}` | S (owner) | — | M1 | [projects](projects.md): refused with `409 project_not_empty` while it has sandboxes or nodes |
| GET | `/v0/projects/{prj}/members` | S, A | `members.read` | M1 | [projects](projects.md) |
| PATCH, DELETE | `/v0/projects/{prj}/members/{usr}` | S (owner, admin) | — | M1 | [projects](projects.md) |
| GET, PATCH | `/v0/projects/{prj}/settings/{area}` | S, A | `project.read` / `project.write` | M1 | [projects/settings](projects/settings.md): one route per settings area (branding, activity, sign-ins, Git, profile defaults…) |
| GET, PATCH | `/v0/projects/{prj}/settings/plugins` | S, A (PATCH: owner, admin, or A with `project.write`) | `project.read` / `project.write` | M0 | [projects/settings](projects/settings.md#plugins): which plugins are on for the project. `422 plugin_dependency`, `409 plugin_in_use` |
| GET, PATCH | `/v0/projects/{prj}/settings/{plugin}` | S, A | `project.read` / `project.write` | M0 | [plugins/extension-points](plugins/extension-points.md#settings): a plugin's project settings, from its schema (`credits`' rates at `…/settings/credits`) |
| GET, POST | `/v0/projects/{prj}/invitations` | S (owner, admin) | — | M1 | [projects](projects.md) |
| DELETE | `/v0/projects/{prj}/invitations/{inv}` | S (owner, admin) | — | M1 | [projects](projects.md) |
| POST | `/v0/invitations/{token}/acceptance` | S | — | M1 | [projects](projects.md) |
| GET, POST | `/v0/projects/{prj}/admin-keys` | S (owner, admin) | — | M1 | [projects](projects.md) |
| DELETE | `/v0/projects/{prj}/admin-keys/{key}` | S (owner, admin) | — | M1 | [projects](projects.md) |
| GET | `/v0/projects/{prj}/notifications` | S, A | `project.read` | M4 | [projects](projects.md): certificates expiring, webhook endpoints disabled, … |
| PATCH | `/v0/projects/{prj}/notifications/{ntf}` | S | — (mark read) | M4 | [projects](projects.md) |
| GET, DELETE | `/v0/projects/{prj}/settings/git/github` | S, A | `project.read` / `project.write` | M3 | [git/github-app](git/github-app.md) |
| PUT | `/v0/projects/{prj}/settings/git/github` | S, A | `project.write` | M3 | [git/github-app](git/github-app.md): bring an App registered by hand |
| POST | `/v0/projects/{prj}/settings/git/github/manifests` | S | `project.write` | M3 | [git/github-app](git/github-app.md): starts the manifest flow, answers the form to post to GitHub |
| POST | `/v0/projects/{prj}/settings/git/github/conversions` | S | `project.write` | M3 | [git/github-app](git/github-app.md): finishes it with GitHub's `code` |
| GET | `/v0/projects/{prj}/settings/git/github/installations` | S, A | `project.read` | M3 | [git/github-app](git/github-app.md): every installation the project knows |
| GET | `/v0/projects/{prj}/settings/git/github/deliveries[/{delivery}]` | S, A | `project.read` | M4 | [github-webhooks](activity-and-webhooks/github-webhooks.md): inbound deliveries, with payloads |
| POST | `/v0/projects/{prj}/settings/git/github/deliveries/{delivery}/replays` | S, A | `project.write` | M4 | [github-webhooks](activity-and-webhooks/github-webhooks.md) |

### Profiles

Every route here is also reachable at `/v0/profile/…` for the calling profile key or setup session ([where resources live](#where-resources-live)); the **Who** column says which.

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/v0/projects/{prj}/profiles` | S, A | `profiles.read` | M1 | [profiles](profiles.md): filter by `external_id`, `status` |
| POST | `/v0/projects/{prj}/profiles` | S, A | `profiles.write` | M1 | [profiles](profiles.md): idempotent |
| GET | `…/profiles/{prf}`, `/v0/profile` | S, A, P, L | — for P and L; `profiles.read` | M1 | [profiles](profiles.md) |
| PATCH | `…/profiles/{prf}`, `/v0/profile` | S, A, P | `profiles.write`; P may change only `default_cluster` | M1 | [profiles](profiles.md): name, external id, status (suspend), git identity, default cluster |
| DELETE | `…/profiles/{prf}` | S, A | `profiles.write` | M1 | [profiles](profiles.md): `409 profile_has_sandboxes` |
| GET, POST | `…/profiles/{prf}/keys` | S, A | `profiles.keys` | M1 | [profiles](profiles.md): profile keys |
| DELETE | `…/profiles/{prf}/keys/{key}` | S, A | `profiles.keys` | M1 | [profiles](profiles.md) |
| GET, PUT | `…/profiles/{prf}/limits` | S, A (PUT); P (GET, as `/v0/profile/limits`) | `profiles.read` / `profiles.write` | M1 | [limits](limits.md) (the `limits` plugin) |
| POST | `…/profiles/{prf}/setup-links` | S, A | `profiles.credentials` | M3 | [profiles](profiles.md): answers an `lbl_` URL |
| POST | `/v0/setup-sessions` | anyone with an `lbl_` token | — | M3 | [profiles](profiles.md): exchanges the link for a setup session cookie |
| DELETE | `/v0/setup-sessions/current` | L | — | M3 | [profiles](profiles.md) |
| GET | `…/profiles/{prf}/agents` | S, A, L | `profiles.credentials` (L: section `agents`) | M3 | [profiles](profiles.md): each agent's credential status |
| PUT, DELETE | `…/profiles/{prf}/agents/{agent}/credential` | S, A, L | `profiles.credentials` (L: `agents`) | M3 | [profiles](profiles.md): API keys and setup tokens, verified with the provider |
| POST | `…/profiles/{prf}/agents/{agent}/sign-ins` | S, A, L | `profiles.credentials` (L: `agents`) | M3 | [profiles/agent-sign-ins](profiles/agent-sign-ins.md): subscription sign-in, `asi_` |
| GET | `…/profiles/{prf}/sign-ins/{asi}` | S, A, L | as above | M3 | [profiles/agent-sign-ins](profiles/agent-sign-ins.md) |
| PATCH | `…/profiles/{prf}/sign-ins/{asi}` | S, A, L | as above | M3 | [profiles/agent-sign-ins](profiles/agent-sign-ins.md): submit the code the provider showed |
| DELETE | `…/profiles/{prf}/sign-ins/{asi}` | S, A, L | as above | M3 | [profiles/agent-sign-ins](profiles/agent-sign-ins.md): cancel |
| GET | `…/profiles/{prf}/git` | S, A, P, L | `profiles.read` (— for P; L: `git`) | M3 | [git/github-app](git/github-app.md): the profile's people, accounts, available accounts and limit |
| DELETE | `…/profiles/{prf}/git` | S, A, L | `profiles.credentials` (L: `git`) | M3 | [git/github-app](git/github-app.md): unlink every person and account |
| POST | `…/profiles/{prf}/git/install-links` | S, A, L | as above | M3 | [git/github-app](git/github-app.md#install-links): answers a URL to install or authorize the App at, with `return_url` |
| PATCH | `…/profiles/{prf}/git/people/{login}` | S, A, L | as above | M3 | [git/github-app](git/github-app.md#connecting-a-profile): `{default: true}` |
| DELETE | `…/profiles/{prf}/git/people/{login}` | S, A, L | as above | M3 | [git/github-app](git/github-app.md#connecting-a-profile): unlink a person |
| PUT | `…/profiles/{prf}/git/accounts/{account}` | S, A, L | as above | M3 | [git/github-app](git/github-app.md#the-limit): link an available account, within the limit (`429 limit_exceeded`) |
| DELETE | `…/profiles/{prf}/git/accounts/{account}` | S, A, L | as above | M3 | [git/github-app](git/github-app.md): unlink an account |
| POST | `…/profiles/{prf}/git/tokens` | S, A, P | `git.tokens`; for `type: user`, `git.tokens.user` (A with a list; S: owners and admins). **P: installation tokens only** | M3 | [git/tokens](git/tokens.md#the-token-api): issue an installation token (repositories, permissions) or a named person's user token |
| GET | `…/profiles/{prf}/git/tokens` | S, A | `profiles.read` | M3 | [git/tokens](git/tokens.md#the-token-api): tokens issued in the last 30 days, without the tokens |
| GET, PUT, DELETE | `…/profiles/{prf}/telemetry` | S, A, L | `profiles.credentials` (L: `telemetry`) | M3 | [profiles](profiles.md): OTLP export |
| POST | `…/profiles/{prf}/telemetry/tests` | S, A, L | as above | M3 | [profiles](profiles.md): sends a test span |
| GET, POST | `…/profiles/{prf}/ssh-keys` | S, A, L | `profiles.credentials` (L: `ssh_keys`) | M3 | [profiles](profiles.md): keys added to every sandbox of the profile |
| DELETE | `…/profiles/{prf}/ssh-keys/{ssh}` | S, A, L | as above | M3 | [profiles](profiles.md) |
| GET | `…/profiles/{prf}/git/repositories[/{owner}/{repo}]` | S, A, P, L | — for P and L; `profiles.read` | M3 | [git/github-app](git/github-app.md#repositories-and-branches): GitHub repositories the linked installations reach. `search`, `visibility`, `account`, pages |
| GET | `…/profiles/{prf}/git/repositories/{owner}/{repo}/branches` | S, A, P, L | as above | M3 | [git/github-app](git/github-app.md#repositories-and-branches) |
| GET | `…/profiles/{prf}/activity` | S, A | `sandboxes.activity.view` | M4 | [activity-and-webhooks](activity-and-webhooks.md): same as `GET /v0/activity?profile=` |
| GET | `…/profiles/{prf}/usage`, `/v0/profile/usage` | S, A, P | `profiles.read` (— for P) | Beta | [credits](credits.md) (the `credits` plugin) |
| GET, POST | `…/profiles/{prf}/credit-buckets` | S, A | `profiles.credits` | Beta | [credits](credits.md) (the `credits` plugin) |
| PATCH | `…/profiles/{prf}/credit-buckets/{cbk}` | S, A | `profiles.credits` | Beta | [credits](credits.md) (the `credits` plugin) |
| POST | `…/profiles/{prf}/credit-buckets/{cbk}/renewals` | S, A | `profiles.credits` | Beta | [credits](credits.md) (the `credits` plugin) |
| GET, POST | `…/profiles/{prf}/credit-transactions` | S, A | `profiles.credits` | Beta | [credits](credits.md) (the `credits` plugin): `reference` makes a POST idempotent |
| GET, PATCH | `…/profiles/{prf}/settings/{plugin}` | S, A, L | `profiles.credentials` (L: the plugin's section) | M0 | [plugins/extension-points](plugins/extension-points.md#settings): a plugin's profile settings, for plugins that declare them |

### Pull requests, pushes and repository creation

Not in Lewbox's API. A project does them on its own servers with a token from `…/git/tokens` ([git/integrating](git/integrating.md)); legacy's `pulls`, `pushes` and repository-creation routes are dropped ([git](git.md#keep-change-drop)).

### Clusters and nodes

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/v0/projects/{prj}/clusters` | S, A | `clusters.read` | M1 | [clusters](clusters.md) |
| POST | `/v0/projects/{prj}/clusters` | S, A | `clusters.write` | M1 | [clusters](clusters.md): name, runtime, architecture, storage, DNS |
| GET, PATCH | `/v0/projects/{prj}/clusters/{clu}` | S, A | `clusters.read` / `clusters.write` | M1 | [clusters](clusters.md): suspend, rename, defaults |
| DELETE | `/v0/projects/{prj}/clusters/{clu}` | S, A | `clusters.write` | Beta | [clusters](clusters.md): `409 cluster_not_empty` |
| POST | `/v0/projects/{prj}/storage-tests` | S, A | `clusters.write` | M1 | [clusters/storage-and-registry](clusters/storage-and-registry.md): checks a bucket before saving it |
| POST | `/v0/projects/{prj}/dns-tests` | S, A | `clusters.write` | M1 | [clusters](clusters.md): checks a DNS provider token |
| GET, POST | `…/clusters/{clu}/installations` | S, A | `clusters.write` | M1 | [clusters](clusters.md): answers the install command, once with the `lbi_` token |
| GET, DELETE | `…/clusters/{clu}/installations/{ins}` | S, A | `clusters.read` / `clusters.write` | M1 | [clusters](clusters.md): progress; cancel (`409 installation_finished`) |
| GET | `…/clusters/{clu}/nodes` | S, A | `clusters.read` | M1 | [clusters](clusters.md) |
| GET | `/v0/projects/{prj}/nodes/{node}` | S, A | `clusters.read` | M1 | [clusters](clusters.md) |
| GET | `/v0/projects/{prj}/nodes/{node}/metrics` | S, A | `clusters.read` | M1 | [clusters](clusters.md): `since`, `until`, `resolution` |
| PATCH | `/v0/projects/{prj}/nodes/{node}` | S, A | `clusters.write` | Beta | [clusters](clusters.md): cordon, drain |
| DELETE | `/v0/projects/{prj}/nodes/{node}` | S, A | `clusters.write` | Beta | [clusters](clusters.md): remove a drained node |
| POST | `…/clusters/{clu}/upgrades` | S, A | `clusters.write` | Beta | [clusters/aerolvm-fork](clusters/aerolvm-fork.md): roll a new AerolVM fork release across the nodes |

### Sandboxes

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| POST | `/v0/sandboxes` | S, A, P | `sandboxes.create`; with `hooks` or `scrub`, also `sandboxes.exec` | M1 | [sandboxes/lifecycle](sandboxes/lifecycle.md): idempotent; S and A send `profile`; `hooks`, `stop_grace`, `scrub` ([sandboxes/hooks](sandboxes/hooks.md), M2) |
| GET | `/v0/sandboxes` | S, A, P | — | M1 | [sandboxes/lifecycle](sandboxes/lifecycle.md): `status`, `cluster`, `profile` (S, A), `metadata[k]`, `project` (S) |
| GET | `/v0/sandboxes/{sbx}` | S, A, P, K | — | M1 | [sandboxes/lifecycle](sandboxes/lifecycle.md) |
| PATCH | `/v0/sandboxes/{sbx}` | S, A, P | `sandboxes.update` for settings (metadata, timers; not `status`, which is `422 validation_failed`: use operations); `sandboxes.update` and `sandboxes.exec` for `hooks`, `stop_grace`, `scrub` | M1 | [sandboxes/lifecycle](sandboxes/lifecycle.md), [sandboxes/hooks](sandboxes/hooks.md#changing-hooks) |
| DELETE | `/v0/sandboxes/{sbx}` | S, A, P | `sandboxes.delete` | M1 | [sandboxes/lifecycle](sandboxes/lifecycle.md): runs `on_stop` on a running sandbox unless `?hooks=false` |
| POST | `/v0/sandboxes/{sbx}/touch` | S, A, P, K | any of `sandboxes.exec`, `sandboxes.ports.view`, `sandboxes.agents` | M2 | [sandboxes/timers-and-activity](sandboxes/timers-and-activity.md#api): counts as activity, re-arming the idle timer |
| GET | `/v0/sandboxes/{sbx}/operations` | S, A, P, K | — | M1 | [sandboxes](sandboxes.md): new; newest first |
| POST | `/v0/sandboxes/{sbx}/operations` | S, A, P | `sandboxes.start` and/or `sandboxes.stop` | M1 | [sandboxes](sandboxes.md): `kind` start, stop, restart; the only way to start, stop or restart a sandbox |
| GET | `/v0/sandboxes/{sbx}/operations/{op}` | S, A, P, K | — | M1 | [sandboxes](sandboxes.md) |
| POST | `/v0/sandboxes/{sbx}/keys` | S, A, P | — (the key gets a subset of the caller's permissions) | M2 | [sandboxes](sandboxes.md), [authentication](api/authentication.md#sandbox-keys) |
| GET | `/v0/sandboxes/{sbx}/git` | S, A, P, K | — | M3 | [git/tokens](git/tokens.md#the-sandboxs-git-setting): repositories, permissions, tokens written |
| PUT | `/v0/sandboxes/{sbx}/git` | S, A, P, K | `sandboxes.git` | M3 | [git/tokens](git/tokens.md#the-sandboxs-git-setting): the sandbox's access (`read`, `write`, `full` or permissions; default `full`) and repositories with their own access, up to the project's cap |
| GET | `/v0/sandboxes/{sbx}/stats` | S, A, P, K | — | Later | [sandboxes](sandboxes.md): CPU, memory, disk in use |

### Inside a sandbox

All need the sandbox to be `running` (`409 sandbox_not_running`), apart from listing ports and SSH keys and closing ports. Who: S, A, P, K.

| Method | Path | Permission | M | Plan |
|---|---|---|---|---|
| POST | `/v0/sandboxes/{sbx}/commands` | `sandboxes.exec` | M2 | [inside-a-sandbox/commands](inside-a-sandbox/commands.md): whole result, or SSE with `Accept: text/event-stream` |
| GET | `/v0/sandboxes/{sbx}/commands/stream` | `sandboxes.exec` | M2 | [inside-a-sandbox/commands](inside-a-sandbox/commands.md): WebSocket, interactive |
| GET, POST | `/v0/sandboxes/{sbx}/sessions` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): sessions |
| GET, PATCH, DELETE | `/v0/sandboxes/{sbx}/sessions/{ses}` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): resize, signal, kill |
| GET | `/v0/sandboxes/{sbx}/sessions/{ses}/log` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): `tail` |
| GET | `/v0/sandboxes/{sbx}/sessions/{ses}/attach` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): WebSocket |
| GET, PUT, PATCH, DELETE | `/v0/sandboxes/{sbx}/files?path=` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): read, write (streamed), move, remove. `If-Match` / `If-Unmodified-Since` on PUT (Beta, Lewagent's editor) |
| GET | `/v0/sandboxes/{sbx}/entries?path=` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): stat |
| GET, POST | `/v0/sandboxes/{sbx}/directories` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md): list (`depth`), make |
| GET | `/v0/sandboxes/{sbx}/files/changes` | `sandboxes.exec` | Later | [inside-a-sandbox](inside-a-sandbox.md): SSE of file changes under a path |
| GET | `/v0/sandboxes/{sbx}/ports` | — | M2 | [inside-a-sandbox](inside-a-sandbox.md) |
| PUT, DELETE | `/v0/sandboxes/{sbx}/ports/{port}` | `sandboxes.ports` | M2 | [inside-a-sandbox](inside-a-sandbox.md): expose (`protocol`, `access`, `persist`), close |
| POST | `/v0/sandboxes/{sbx}/ports/{port}/links` | `sandboxes.ports.view` | M2 | [inside-a-sandbox](inside-a-sandbox.md): a one-minute signed link to a private port |
| GET | `/v0/sandboxes/{sbx}/ssh-keys` | — | M2 | [inside-a-sandbox](inside-a-sandbox.md) |
| PUT, DELETE | `/v0/sandboxes/{sbx}/ssh-keys/{name}` | `sandboxes.exec` | M2 | [inside-a-sandbox](inside-a-sandbox.md) |

### Agents

Who: S, A, P, K (a sandbox key reaches its own sandbox's conversations only).

| Method | Path | Permission | M | Plan |
|---|---|---|---|---|
| GET | `/v0/sandboxes/{sbx}/agents` | `sandboxes.agents` | M3 | [agents](agents.md): models, thinking levels, modes, read from the agents |
| POST | `/v0/sandboxes/{sbx}/conversations` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): idempotent |
| GET | `/v0/conversations` | — | M3 | [agents/conversations](agents/conversations.md): `sandbox`, `status`, `project` (S), `profile` (S, A) |
| GET | `/v0/conversations/{cnv}` | — | M3 | [agents/conversations](agents/conversations.md) |
| PATCH | `/v0/conversations/{cnv}` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): title, settings for the next turn |
| DELETE | `/v0/conversations/{cnv}` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): `force`, `keep_worktree` |
| GET, POST | `/v0/conversations/{cnv}/messages` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): `after`, `status=queued`; POST idempotent, `queue`, `steer` |
| GET | `/v0/conversations/{cnv}/messages/{msg}` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): new; a turn's status, text and output without the stream |
| PATCH | `/v0/conversations/{cnv}/messages/{msg}` | `sandboxes.agents` | M3 | [agents/conversations](agents/conversations.md): cancel, steer |
| GET | `/v0/conversations/{cnv}/messages/{msg}/events` | `sandboxes.agents` | M3 | [agents](agents.md): SSE, `after`, `Last-Event-ID`, `types` |
| GET | `/v0/conversations/{cnv}/chunks` | `sandboxes.agents` | M3 | [agents](agents.md): what's stored, for rendering history |
| POST | `/v0/conversations/{cnv}/replies` | `sandboxes.agents` | M3 | [agents](agents.md): approvals and questions |
| GET | `/v0/conversations/{cnv}/tasks` | `sandboxes.agents` | M3 | [agents](agents.md): background tasks |
| PATCH | `/v0/conversations/{cnv}/tasks/{task}` | `sandboxes.agents` | M3 | [agents](agents.md): stop |
| GET, POST | `/v0/sandboxes/{sbx}/worktrees` | `sandboxes.agents` | Later | [agents/worktrees](agents/worktrees.md): standalone worktrees (`wtr_`) |
| GET, DELETE | `/v0/worktrees/{wtr}` | `sandboxes.agents` | Later | [agents/worktrees](agents/worktrees.md) |
| POST | `/v0/sandboxes/{sbx}/completions` | `sandboxes.agents` | Later | [agents](agents.md): fill-in-the-middle for an editor, on the profile's credential (Lewagent's ask) |

### Templates

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/v0/templates` | S, A, P | — | M1 | [templates](templates.md): the project's templates and the profile's own. `cluster`, `profile` (S, A), `project` (S) |
| POST | `/v0/templates` | S, A, P | `templates.create` | M1 (S, A); Beta (P) | [templates](templates.md): S and A make project templates, or a profile's with `profile`; idempotent |
| GET | `/v0/templates/{tpl}` | S, A, P | — | M1 | [templates](templates.md) |
| PATCH | `/v0/templates/{tpl}` | S, A, P | `templates.create` | M1 | [templates](templates.md): name, ports, default size, env, clusters |
| DELETE | `/v0/templates/{tpl}` | S, A, P | `templates.delete` | M1 | [templates](templates.md): `409 template_in_use` |
| GET, POST | `/v0/templates/{tpl}/builds` | S, A, P | — / `templates.create` | M1 | [templates/builds](templates/builds.md): rebuild, optionally with a new Dockerfile |
| GET | `/v0/templates/{tpl}/builds/{bld}` | S, A, P | — | M1 | [templates/builds](templates/builds.md) |
| GET | `/v0/templates/{tpl}/builds/{bld}/log` | S, A, P | — | Beta | [templates/builds](templates/builds.md): needs build logs from the AerolVM fork |

Template ids are slugs chosen by their maker, unique within a project, so `{tpl}` resolves within the caller's project ([templates](templates.md)).

### Activity and live changes

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET | `/v0/activity` | S, A, P | `sandboxes.activity.view` | M4 | [activity-and-webhooks](activity-and-webhooks.md): every sandbox the principal reaches |
| GET | `/v0/sandboxes/{sbx}/activity` | S, A, P, K | `sandboxes.activity.view` | M4 | [activity-and-webhooks](activity-and-webhooks.md) |
| POST | `/v0/sandboxes/{sbx}/activity` | S, A, P, K | `sandboxes.activity` | M4 | [activity-and-webhooks](activity-and-webhooks.md): `app.*` events; idempotent; 60 a minute per key |
| GET | `/v0/events` | S, A, P, K | — (each event filtered by what the principal may read) | M4 | [streaming](api/streaming.md#live-changes): SSE of sandbox, operation, conversation and build changes, the same payloads as webhooks |

### Webhooks

| Method | Path | Who | Permission | M | Plan |
|---|---|---|---|---|---|
| GET, POST | `/v0/projects/{prj}/webhook-endpoints` | S, A | `webhooks.read` / `webhooks.write` | M4 | [activity-and-webhooks](activity-and-webhooks.md): the secret is shown once |
| GET, PATCH, DELETE | `/v0/projects/{prj}/webhook-endpoints/{whk}` | S, A | `webhooks.read` / `webhooks.write` | M4 | [activity-and-webhooks](activity-and-webhooks.md): events, URL, enable |
| POST | `…/webhook-endpoints/{whk}/secret-rolls` | S, A | `webhooks.write` | M4 | [activity-and-webhooks](activity-and-webhooks.md): both secrets sign for 24 hours |
| POST | `…/webhook-endpoints/{whk}/tests` | S, A | `webhooks.write` | M4 | [activity-and-webhooks](activity-and-webhooks.md): sends `webhook.test` |
| GET | `…/webhook-endpoints/{whk}/deliveries` | S, A | `webhooks.read` | M4 | [activity-and-webhooks](activity-and-webhooks.md): `status`, `event` |
| GET | `/v0/projects/{prj}/webhook-deliveries/{whd}` | S, A | `webhooks.read` | M4 | [activity-and-webhooks](activity-and-webhooks.md): with its attempts |
| POST | `/v0/projects/{prj}/webhook-deliveries/{whd}/redeliveries` | S, A | `webhooks.write` | M4 | [activity-and-webhooks](activity-and-webhooks.md) |
| GET | `/v0/projects/{prj}/webhook-event-types` | S, A | — | M4 | [activity-and-webhooks](activity-and-webhooks.md): the catalogue, for the endpoint form |

### Node API (internal)

Owned by [clusters](clusters.md); listed here so the whole surface is in one place. Shapes may change with the node agent, which is why it isn't in `/v0`.

| Method | Path | Who | M | What |
|---|---|---|---|---|
| GET | `/install.sh` | anyone | M1 | The installer: fetches the `lewbox` binary matching this server's version, and runs `lewbox node install` |
| GET | `/downloads/lewbox/{version}/{os}-{arch}` | anyone | M1 | The binary, from the release (redirect) or the server's own cache |
| POST | `/node/v0/enrollments` | I | M1 | Enroll: host facts in, node id and node token out (once per install token) |
| GET, PATCH | `/node/v0/installation` | I | M1 | The installation's settings; report progress steps |
| GET | `/node/v0/node` | N | M1 | Desired state: AerolVM fork version, agent version, cluster settings, storage, DNS |
| PUT | `/node/v0/node/certificate` | I, N | M1 | Send a CSR, get a certificate signed by the cluster's CA |
| GET | `/node/v0/node/firewall` | N | M1 | Firewall rules |
| POST | `/node/v0/node/metrics` | N | M1 | A minute's metrics (6 a minute at most) |
| PUT | `/node/v0/node/dns` | N | M1 | Ask the control plane to (re)write the node's records |
| GET | `/node/v0/node/tunnel` | N | M1 | WebSocket: the node's outbound tunnel, for nodes the control plane can't reach directly ([clusters/connectivity](clusters/connectivity.md)) |
| POST | `/node/v0/events` | N | M1 | AerolVM's lifecycle events (timer warnings and expiries, stops, session ends), in order; the fallback for the agent's connection ([clusters/node-agent](clusters/node-agent.md#lifecycle-events)) |

### agentd API (internal)

| Method | Path | Who | M | What |
|---|---|---|---|---|
| GET | `/agentd/v0/channel` | G | M3 | WebSocket agentd dials home on: commands down; events, uploads and results up ([agents/agentd](agents/agentd.md#the-agent-channel)) |

### Hooks and browser flows

| Method | Path | Who | M | Plan |
|---|---|---|---|---|
| POST | `/hooks/github/{prj}` | GitHub (`X-Hub-Signature-256` with the project's App secret) | M3 | [activity-and-webhooks](activity-and-webhooks.md), [git/github-app](git/github-app.md) |
| GET | `/github/apps/{prj}/created` | Browser, after GitHub's manifest flow | M3 | [git/github-app](git/github-app.md) |
| GET | `/github/apps/{prj}/authorized` | Browser, after a user authorizes or installs the App (a setup page or an install link) | M3 | [git/github-app](git/github-app.md#connecting-a-profile) |
| GET | `/auth/…` | Browser | Later | [projects/users-and-login](projects/users-and-login.md): OAuth or OIDC sign-in, if we add it |

### Later

| Path | What | Plan |
|---|---|---|
| `/e2b/…` | An E2B-compatible facade, so E2B SDKs work against Lewbox | [sdks-and-cli](sdks-and-cli.md) |
| `/v0/openai/responses` | An OpenAI-compatible endpoint that runs a turn | [agents](agents.md) |
| `/v0/projects/{prj}/domains` | Custom domains for the API (`401 wrong_domain`) | [projects](projects.md) |

---

## Security

- **One host widens the blast radius of a browser bug**, so the session cookie is `__Host-`, `HttpOnly`, `Secure`, `SameSite=Lax`, and every unsafe request with a session needs a CSRF header and a same-origin `Origin` ([authentication](api/authentication.md#csrf)). Bearer-key requests never read cookies, so CORS can stay open for them.
- **Sandbox content is never served from the API's origin.** Files are downloaded with `Content-Disposition: attachment` and `Content-Type: application/octet-stream` unless the caller asks otherwise, plus `X-Content-Type-Options: nosniff` and a `sandbox` CSP, so a hostile file can't run script on the dashboard's origin. Private ports live on node domains, never the control plane's.
- **The dashboard's CSP** forbids inline script and limits `connect-src` to the API's own origin and node domains (for terminals that connect directly, if we do that later).
- **Admin keys are powerful**; they can be scoped ([permissions](api/permissions.md#admin-key-scopes)) and can't do what stays human.
- **Internal surfaces fail closed**: `/node/v0` refuses anything but node and install tokens; `/agentd/v0` refuses anything but a valid agentd token for a conversation whose sandbox is still the one the token names.

## Observability

- Every request is logged once, with `request_id`, method, route pattern (not the raw path), principal kind and id, project, profile, sandbox, status, error code and duration ([foundation/observability](foundation/observability.md)).
- Metrics: `lewbox_http_requests_total{route,method,status,principal}`, `lewbox_http_request_duration_seconds`, `lewbox_http_streams_open{kind}`, `lewbox_http_refusals_total{code}`.
- A request refused inside a sandbox is a `request.refused` activity event ([activity-and-webhooks](activity-and-webhooks.md)), as in legacy.

## Testing

- **Route conformance:** a test lists every route registered on the router and every operation in the OpenAPI document, and fails on any difference ([openapi-and-codegen](api/openapi-and-codegen.md#route-conformance)).
- **Principal matrix:** a table-driven test calls every route with every principal kind (none, S, A of another project, A, P of another profile, P, K of another sandbox, K, L) and checks the status and code against the **Who** column. The matrix is generated from route metadata, so a new route can't skip it.
- **Response validation:** every response in the integration tests is validated against the OpenAPI document.
- **Plugins:** the conformance and matrix tests run on the default build (plugins' routes included) and on the core-only build; every plugin route also answers `409 plugin_disabled` when its plugin is off for the instance and for the project.

## Build plan

Package `internal/httpapi`:

1. Router on `net/http`, with route metadata: owner, principals, permission, idempotent, rate-limit class, milestone. Metadata drives middleware and the conformance and matrix tests.
2. Middleware, in order: recovery → request id → access log → CORS → body limits → authentication (principal) → CSRF (sessions) → rate limit → plugin enablement (`409 plugin_disabled`, for routes a plugin owns) → permission → idempotency → handler. Plugins' routes get exactly the same chain ([plugins/extension-points](plugins/extension-points.md#routes)).
3. `respond` package: `data` envelope, lists with cursors, errors from the [catalogue](api/errors.md), SSE and WebSocket helpers ([streaming](api/streaming.md)).
4. Handlers by area, one file per resource, calling services (`internal/sandboxes`, …), never the database directly.
5. The OpenAPI document and its CI checks ([openapi-and-codegen](api/openapi-and-codegen.md)).
6. `GET /v0/meta`, `/up`, `/ready`.

Tasks, M0: router and metadata; middleware chain; errors and the code registry; pagination; request ids; idempotency store; rate limiter; auth for all principals; principal-matrix test harness; OpenAPI skeleton, linting and conformance test.

## Open questions

1. ~~**`/v0` stability during the alpha.**~~ Decided (2026-10-29): the API is `/v0` and isn't stable; breaking changes are allowed, with a changelog, until `/v1` ([conventions](api/conventions.md#versioning-and-deprecation)).
2. ~~**Admin keys acting inside sandboxes.**~~ Decided (2026-10-29): admins are admins. Admin keys act inside every sandbox in their project ([A3](#decisions)); profiles can't opt out. A key without `sandboxes.exec` and `sandboxes.agents` can't read customers' files.
3. **Instance keys** (`lbx_`, for automation such as creating projects from Terraform). Deferred (Later).
4. **Custom domains** (one instance serving several projects' branded API hosts). Deferred (Later).
5. ~~**`project=` on top-level lists for sessions.**~~ Decided (2026-10-29): the query parameter; one path per resource.
6. ~~**Git repository lists as cursors.**~~ Decided (2026-10-29): yes. The cursor is opaque, so it can wrap a Git provider's page number.
