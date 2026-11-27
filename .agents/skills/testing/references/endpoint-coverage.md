# Endpoint coverage and matrices

Read this when you add or change an endpoint, a permission, a role, an API-key scope or an event, or review someone else's endpoint tests. Every case below is mandatory for every endpoint. Route and action names follow `naming`. Status codes and error shapes follow `api`.

## The ten cases

The examples use `PATCH /api/tenants/{tenant}/projects/{project}` (`update`, permission `project.update`, event `project.updated`).

### 1. Success

- Status: `store` 201, `show`/`index`/`update` 200, `destroy` 204 with an empty body.
- Body shape: the documented fields, public IDs type-prefixed (`prj_…`), timestamps in the documented format.
- Every role the matrix allows. (The matrix covers that; the endpoint's own test covers one role in depth.)

### 2. Validation (table-driven)

```
cases = [
  {name: "empty name",          body: {name: ""},               status: 422, field: "name"},
  {name: "name too long",       body: {name: "x" * 81},         status: 422, field: "name"},
  {name: "80 multibyte chars",  body: {name: "é" * 80},         status: 200},             # characters, not bytes
  {name: "NUL",                 body: {name: "a\u0000b"},       status: 422, field: "name"},
  {name: "bidi override",       body: {name: "a‮b"},       status: 422, field: "name"},
  {name: "zero-width space",    body: {name: "a​b"},       status: 422, field: "name"},
  {name: "emoji with ZWJ",      body: {name: "👩‍💻"},             status: 200},
  {name: "unknown status",      body: {status: "nope"},         status: 422, field: "status"},
  {name: "unknown field",       body: {nmae: "x"},              status: 400},
  {name: "not JSON",            raw:  "name=x", type: "text/plain", status: 415},
]
for c in cases:
  run c.name:
    resp = send(...)
    assert resp.status == c.status
    if c.field: assert resp.body.field == c.field
    assert projectInDB(...).name == original.name                 # nothing persisted on refusal
    assert eventsNamed("project.updated") is empty               # nothing recorded
```

- Pure parsing and validation functions (pagination params, filters, text rules) also get their own table-driven **unit** tests. The integration table proves they are wired in.
- **Common mistake:** asserting only the status. A 422 that still wrote half the change is a bug.

### 3. 401: not signed in

- No cookie, an expired session, a session revoked (logout, password change). Each gets 401 and changes nothing.
- For token-authenticated routes: no token, a malformed one, a revoked one, an expired one.

### 4. 403: forged or not permitted

- **No CSRF token** on a mutation → 403.
- **Missing or foreign `Origin`** on a mutation → 403.
- **A member whose role lacks the permission** → 403 (e.g. a viewer on `update`).
- Where the product gates unverified accounts or suspended users, those too.
- Refused requests change nothing, record no domain event, and don't spend the user's rate-limit budget unless the limiter is meant to count them.

### 5. 404: not yours

- **A non-member on every tenant route** → 404, not 403. A 403 would confirm the tenant exists.
- **Cross-tenant ID swap:** your own tenant in the path, another tenant's `{project}` → 404. This is the classic IDOR. Test it on every route that takes a nested ID.
- An unknown ID, a malformed ID and an already-deleted record (a second `destroy`) → 404.
- All of these answers are **identical** to each other (compare bodies).
- Lists: another tenant's rows never appear, even when they match the filter.

### 6. No internal IDs

```
walk(json, path):
  for key, value in json:
    assert key != "id" or isPublicId(value)          # "prj_…", never 42
    assert not key.endsWith("Id") or isPublicId(value)
    recurse into objects and arrays
```

Run the walk on every response the endpoint can give, refusals included. Internal numeric IDs make records enumerable and couple clients to storage.

### 7. Persistence

- Read the record back from the database (or with a fresh `show`) after the request. The response alone isn't proof.
- `update` changed only the fields sent. Untouched fields and other records are unchanged.
- `destroy` really removed it (and its dependents, per the schema's rules), and kept what must be kept (e.g. audit events with the tenant nulled).

### 8. Ordering, pagination and filters (for `index`)

- **Every sort key, both directions, across page boundaries:** create more than one page (e.g. 45 rows at 20 per page) and concatenate the pages. No repeats, no gaps, correct order.
- **Ties are stable:** give several rows the same sort value (equal timestamps) and check that cursors still neither skip nor repeat.
- **Filters combine:** each filter alone, then together.
- **Search treats `%`, `_` and `\` literally:** a search for `50%` doesn't match everything.
- **Past the end:** an empty page with the right total, not an error.
- **Bad parameters → 400:** an unknown sort key, a page of `-1`, a cursor from another tenant or a forged one.

### 9. Events

```
assert onlyEvent(db, "project.updated") matches {
  actor: session.userPublicId, tenant: tenant.publicId,
  payload: {project: project.publicId, changed: ["name"]},
  requestId: resp.headers["X-Request-ID"],
}
# and for every refused case above:
assert eventsNamed(db, "project.updated") is empty
```

- **Exactly one** event per successful action, not zero (a forgotten audit) and not two (a retry or a listener double-firing).
- **None** for 401, 403, 404, 422 or 429, except the refusal events the design asks for (e.g. `rate_limit.reached` once when the limit is crossed).
- Payloads hold public IDs and changed field names, never secrets, tokens or full request bodies. Check that every ID in the payload resolves.

### 10. Rate limit

Pre-fill the counter to its threshold, send one request, and assert 429, `Retry-After`, nothing created and the refusal event recorded once. See [security-tests.md#rate-limits](security-tests.md#rate-limits).

## Matrices

### Role × route permission matrix

```
matrix = {
  "GET    /api/tenants/{tenant}/projects":            [owner, admin, member, viewer],
  "POST   /api/tenants/{tenant}/projects":            [owner, admin, member],
  "PATCH  /api/tenants/{tenant}/projects/{project}":  [owner, admin, member],
  "DELETE /api/tenants/{tenant}/projects/{project}":  [owner, admin],            target: fresh project per request
  "DELETE /api/tenants/{tenant}/members/{member}":    [owner, admin],            target: fresh member per request
  …
}

test "every tenant route × every role":
  for route in router.registry() where route is tenant-scoped:
    row = matrix[route] or fail(route + " is registered but missing from the permission matrix")
    for role in allRoles:
      status = request(route, as=role, target=row.target?())
      if role in row.allowed: assert status is 2xx
      else:                   assert status == 403
    assert request(route, as=outsider) == 404
  for key in matrix: assert key in registry               # no stale rows
```

- **It walks the real registry.** A route added without a matrix row fails the build. That's how a new endpoint is stopped from shipping without an authorization decision.
- Rows that destroy their target build a new target per request, or a fresh tenant every role belongs to. Otherwise the first allowed role deletes it and the rest get 404 for the wrong reason.
- Rows that need a unique body (an invite needs a new email) take a body factory.
- Companion registry tests:
  - **Every mutation declares a permission** (no POST, PATCH or DELETE registered without one).
  - **Exceptional sets are exact lists:** "only these N routes allow unverified accounts", "admin routes stay in the admin group", "routes acting on your own sessions are only these two". Adding one is then a visible, reviewed diff.
- A refactor of the authorization code keeps this matrix green **without editing it**.
- **Unit invariants on the grants table** catch a bad edit before the matrix runs, and say what's wrong in one line:
  - each role's permissions are a **subset** of the role above's (an admin never gains something the owner lacks);
  - every permission constant appears in the grants **at least once** (a permission nobody holds is a dead check or a typo);
  - the lowest role (e.g. viewer) holds **none**;
  - the client's permission names are checked against the server's source (see `frontend-unit.md`, contract drift);
  - frontend tests build a tenant with one fixture (`testTenant(role)`), the **only** place outside the server that spells out each role's permissions, so a grants change is one edit on the client side.

### API keys × scope

```
for route in registry where route.acceptsApiKeys:
  row = keyMatrix[route] or fail("unlisted public route")
  for kind in [personal, tenant]:
    for ownerRole in allRoles:
      for scopePresent in [true, false]:
        key = apiKeys.for(kind, ownerRole, scopes = scopePresent ? [row.scope] : otherScopes)
        expect = scopePresent and ownerRole allowed by row and kind allowed by row
        assert request(route, bearer=key).status == (expect ? 2xx : 403)
```

- **Live roles:** demote the key's owner, and the very next request is narrowed. Remove the owner from the tenant, and the key stops working. Re-adding them does not revive a revoked key.
- Revoked and expired keys get 401. A key from another tenant gets 404 on this tenant's resources.
- Keys are compared by digest. Credential parsing has its own table test (canonical form only, independent random secrets).
- **Writes re-check authority after waiting on a lock:** start a write, revoke the key or narrow its scope while the write holds or waits for the lock, and assert nothing changed.

## Endpoint test skeleton

```
TestProjectUpdate:
  db, srv = server(t); f = testFaker(t)
  owner  = signedIn(t, db, f); tenant = ownedTenant(t, db, f, owner.userId)
  project = projectsIn(tenant.id).createOne(…)
  path = tenantPath(tenant.publicId, "/projects/" + project.publicId)

  run "updates the name and records one event":            # 1, 7, 9
  run "refuses invalid input and saves nothing" (table):   # 2
  run "401 without a session":                             # 3
  run "403 without CSRF, with a foreign Origin, as viewer":# 4
  run "404 for outsiders and another tenant's project":    # 5
  run "exposes no internal id":                            # 6 (walk every response above)
  run "429 when project:per-user is used up":              # 10
```

Lists (`index`) add case 8 in their own test (`TestProjectPagination`). The matrix row lives in the router's matrix test.
