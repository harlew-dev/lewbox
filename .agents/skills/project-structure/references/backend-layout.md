# Backend layout

Read when creating or moving backend packages, controllers, middleware, data-layer code or entrypoints.

## Example tree

Shown with Go-style `cmd/` + `internal/`; the shape is the same in any stack (mapping table below).

```
api/
  cmd/                         entrypoints: composition only
    server/main.<ext>          load config → logger → db → store → http server → listeners/workers → serve → graceful shutdown
    migrate/main.<ext>         migrate up / status / rollback (previews unless confirmed)
    seed/main.<ext>            flags only; refuses production
    prune/main.<ext>           flags only; previews unless confirmed
    admin/main.<ext>           operator commands (grant/revoke admin, reset MFA, unblock…); preview unless confirmed
  internal/
    config/                    env loading + validation (refuse to start on bad config)
    observe/                   log handler, request IDs, error scrubbing, metrics — imports nothing else of the app
    database/                  connection, migration runner, advisory locks, slow-query hook
    http/                      the core: Server, middleware, JSON responses, request context, sessions, limits
      input/                   leaf: query params, paging, public-ID parsing, text validation
      ratelimit/               leaf: what a limit is, the counter
      clientip/                leaf: resolving the client address behind trusted proxies
      endpoints/
        account/               /api/auth/*, signed-out link pages
          sessions.<ext>       Sessions: login, logout
          registrations.<ext>
        workspace/             /api/workspaces/*, /api/invitations/*
          workspaces.<ext>     Workspaces: index, show, store, update, destroy
          projects.<ext>       Projects + its input type + validation
          members.<ext>
          invitations.<ext>
        admin/                 /api/admin/* — the only area allowed to cross tenants
          users.<ext>
      router/
        routes.<ext>           THE registry of API routes and pages
      apitest/                 helpers for integration tests; only tests import it
    store/                     models + every query (the data layer)
    auth/                      primitives: hashing, tokens, digests, cookies, OTP, sealing
    mail/                      sender, templates, mail listeners
    events/                    event types, bus, listeners, delivery worker
    web/                       serving the built frontend(s), CSP, page headers
    factory/                   fake records per table — seeders and tests only
    seed/                      seeders
  migrations/                  SQL up/down pairs, one create migration per table
```

## Import rules (draw them as a tree)

```
cmd/* ──▶ router ──▶ endpoints/<area> ──▶ http core ──▶ leaves (input, ratelimit, clientip)
                                     └──▶ store ──▶ database
                                     └──▶ auth, events, mail
observe ◀── everyone            (observe imports nothing of the app)
factory, seed ◀── cmd/seed, tests only
apitest ◀── tests only
```

- Only `router` imports areas. Areas never import each other. The core imports no area.
- Leaves import nothing of the app (they may import the standard library and their own wrapped dependency).
- `observe` imports nothing of the app, so logging can be installed first by every entrypoint and can never cause a cycle.
- `cmd/server` never imports `factory`, `seed` or `apitest`.
- **Break a cycle with a small interface owned by the lower package.** Example: the events worker needs the store (claim, mark delivered, retry) and the store needs the event types. `events` declares a `Queue` interface with just those methods; `store` implements it and imports `events`; the server entrypoint wires them. Why: the dependency points one way, and the worker is testable with a fake queue.
- **Never name a package `base`, `util`, `common` or `helpers`.** The name says nothing about the concern, so it collects everything. When subpackages share code, the **parent is the core and the children import it** (like `net/http` and `net/http/httptest`); a parent can't also import its children, so the registry that imports every area gets its own small package.

## Helper shape

- **Stateless helpers are package functions** (`http.Fail(w, 404, "Project not found.")`, `http.Tenant(r)`), like the standard library's `http.Error`. **Only code that needs dependencies** (store, config, keys) is a method on the server object. Why: a function's signature shows everything it touches; a method hides it.
- **Export only what another package uses.** Everything else stays private, reached through a method when the core owns it (e.g. derived keys are used through methods such as `EmailCodeDigest(code)`, never exported as values). Why: each export is an API other areas will start depending on.
- **Prefer a flat data layer, one file per table or resource**, rather than subpackages. Queries share private transaction and event-recording helpers, and models refer to each other; splitting would force exporting them.

## Controller file shape

```
// endpoints/workspace/projects.<ext>
Projects { server }                  // embeds/receives the server: repo, config, logger

ProjectInput { name, description, status }   // the resource's input type
validProject(input) -> error message | ok    // its validation (private)

Projects.index(req)   -> parse paging/search → repo.IndexProjects(tenant, query) → 200 page
Projects.show(req)    -> repo.FindProject(tenant, id) → 200 | 404
Projects.store(req)   -> decode+validate → repo.CreateProject(tenant, input) → 201 record
Projects.update(req)  -> decode+validate → repo.UpdateProject(...) → 200 record
Projects.destroy(req) -> repo.DeleteProject(...) → 204
```

Two stacks, same idea:

```go
// Go: endpoints/workspace/projects.go
type Projects struct{ *httpapi.Server }
func (h Projects) Index(w http.ResponseWriter, r *http.Request) { … }

// router/routes.go
projects := workspace.Projects{Server: s}
ws.HandleFunc("GET /projects", projects.Index)
ws.Can("POST /projects", auth.ProjectStore, s.Limit(httpapi.ProjectPerUser, projects.Store))
```

```ts
// Node: src/http/endpoints/workspace/projects.ts
export class Projects { constructor(private s: Server) {} index = async (req, res) => { … } }

// src/http/router/routes.ts
const projects = new Projects(s)
ws.get('/projects', projects.index)
ws.post('/projects', can('project.store'), limit('project:per-user'), projects.store)
```

## Request flow

The chain every request passes, each step one middleware owned by the core:

```
client address + request ID (+ access log line, metrics, once answered)
→ security headers, no-store, Origin + content-type checks on mutations, request timeout
→ panic recovery (generic 500 with the request ID)
→ mux (the registry; the matched pattern names the route in logs)
→ require session (cookie → session; CSRF on mutations)          private routes
→ require tenant ({workspace} → membership + role, else 404)       tenant routes
→ require permission (403 unless the role grants it)              tenant mutations
→ rate limit / quota
→ controller action → store (query scoped by tenant ID) → JSON
```

The details of each step are owned by `security-defaults` and `api-design`; the structural rule is that each is a **named middleware in the core, attached in the registry**, never re-implemented inside a controller.

## Data layer rules

- Every query is a named method on the store (`IndexProjects`, `FindProject`, `CreateProject`, domain verbs; names per `resource-naming`).
- Use the query builder with placeholders; raw SQL strings only in migrations and the `database` package's DB-level statements (schemas, advisory locks). See `security-defaults`.
- A store method receives the **authorised** tenant/principal from the controller and scopes by it; it never trusts an owner ID from the request body.
- One store method may serve several routes. A filter is a field on the query object, not a new method.
- Every table has a factory (in `factory/`) updated in the same change as its migration; tests build data with factories. See `testing`.

## Entrypoints in detail

- **Server:** install the logger first, then config, then everything else; start listeners and workers; serve; on signal, flip readiness to failing, drain, stop workers, flush logs/metrics, close the DB. Nothing else.
- **Commands** (`migrate`, `seed`, `prune`, operator `admin`): parse flags, call a package function, print the result. **Destructive commands preview unless explicitly confirmed** (`CONFIRM=1`) and refuse in production where they make no sense (seed, fresh-migrate). See `conventions-and-docs` for the etiquette.
- **No auto-migrate on server start.** Migrating is its own command, run deliberately.

## Mapping to other stacks

| Concept | Go | Node (Express/Fastify/Nest) | PHP (Laravel) | Python (FastAPI/Django) |
| --- | --- | --- | --- | --- |
| Entrypoint | `cmd/server/main.go` | `src/main.ts` | `public/index.php` + `bootstrap/app.php` | `main.py` / `manage.py` |
| Registry | `router/routes.go` | `src/http/router/routes.ts` | `routes/api.php`, `routes/web.php` | one `routes.py` / root `urls.py` including no per-app route lists |
| Area + controller | `endpoints/workspace/projects.go` | `src/http/endpoints/workspace/projects.ts` | `app/Http/Controllers/Workspace/ProjectsController.php` | `app/http/endpoints/workspace/projects.py` |
| Data layer | `internal/store` | `src/store` (query builder: Kysely, Knex, Drizzle) | repositories/query classes over Eloquent | `app/store` (SQLAlchemy Core) |
| Leaf packages | `httpapi/input` | `src/http/input` | `app/Support/Input` | `app/http/input` |
| Test-only helpers | `httpapi/apitest` | `src/test/` | `tests/Support` | `tests/support` |

Frameworks that encourage decorators on controllers (Nest, Spring, FastAPI routers) still get one registry: declare routes in the registry module and keep controller methods undecorated, or, if the framework forces decorators, generate and check a route table in a test so the full list (method, path, guards) is reviewable in one place.
