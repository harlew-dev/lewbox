# Strict input

Every byte from the client is hostile until a validator says otherwise. "Be liberal in what you accept" is wrong for an API: lenient parsing hides client bugs, opens mass-assignment holes, and creates parser differentials (your proxy, your framework and your validator read the same input differently). Be strict, reject early, and reject with 400 or 422 (see [status-codes.md](status-codes.md#400-vs-422)).

## Order inside a handler

```
1. allow-list and parse query params      -> 400 on anything unexpected
2. decode the body (capped, strict)        -> 400
3. validate values with shared validators  -> 422 {error, field}
4. call the data layer with typed values + the tenant's internal ID
```

Decode and validate in one helper (e.g. `decodeValid(r, &input, validateProject)`) so a handler can't decode and forget to validate.

## Bodies

| Rule | Why |
| - | - |
| Cap the size (**Prefer** 16 KiB; a route that needs more raises its own cap with a reason) | A 1 GB JSON body is a memory DoS before any validation runs |
| Exactly one JSON object: no top-level array, no second value after it (`{}{}`), no trailing garbage | Prevents smuggling a second document past one parser and not another |
| Unknown fields are 400 | `{"name": "x", "role": "owner", "tenantId": 9}` must not reach a struct that a later refactor binds straight to the database. Also turns client typos (`"descripton"`) into errors instead of silent data loss |
| Wrong JSON type is 400 | `"perPage": "20"` is not a number; no coercion |
| `Content-Type: application/json` on POST, PATCH and DELETE with a body, else 415 | A cross-site HTML form can only send `form-urlencoded`, `multipart` or `text/plain`; requiring JSON blocks that class of CSRF (see `security`) |
| Decode into a dedicated input type per action, never the database model | The model has fields (`id`, `tenant_id`, `created_at`) the client must not set |

Example (pseudocode):

```
type ProjectInput { name: string, description: string, status: string, color: string }

input = decodeStrict(request.body, ProjectInput, maxBytes = 16 KiB)   // 400 on any shape problem
if err = validateProject(input): return 422 {error: err.message, field: err.field}
```

In stacks whose JSON decoder ignores unknown keys by default (most do), turn on the strict mode or validate the key set yourself: e.g. a schema with `additionalProperties: false`, `.strict()` in a schema library, a "disallow unknown fields" decoder option.

## Query parameters

Each handler declares the parameters it accepts; the shared parser rejects everything else.

```
params = allowQuery(request, "page", "perPage", "q", "status", "sort")   // 400 for unknown or repeated
page   = parsePage(params)                                              // 400 for malformed
```

| Rule | Example rejected (400) |
| - | - |
| Unknown parameter | `?pgae=2`, `?limit=50` |
| Repeated parameter | `?status=Planned&status=Completed` (unless the route documents a multi-value parameter) |
| Integers are plain ASCII digits only, in range | `+2`, ` 2`, `2 `, `1e3`, `0x10`, `2.0`, out of range (`perPage=0`, `perPage=101`). **Prefer** rejecting leading zeros (`02`) too |
| Booleans are exact tokens | `true`/`false` only where both make sense; a filter that implements one value accepts only that value (`canLeave=false`; `true`, `0`, empty are 400) |
| Enums exact, case-sensitive | `status=planned` when the value is `Planned` |
| Empty values | `?q=` is either "no search" (document it) or 400; decide once in the shared parser |

Why so strict for GET? A tolerant parser makes `?perPage=1e9` mean something, a cache key differ from what the handler used, or a filter silently ignored so the user sees everything instead of the subset they asked for.

## The shared text validator

One function (e.g. `validText(value, min, max, multiline)`) checks every human-typed string: names, titles, descriptions, reasons, search terms. A new field gets every rule by calling it.

| Rule | Why |
| - | - |
| Trim leading/trailing whitespace before checking length | `"   "` is not a name |
| Length in characters (Unicode code points), not bytes | `"Zoë"` is 3 characters; byte limits punish non-English users. The database `CHECK` uses `char_length`, matching |
| Valid UTF-8 | Invalid sequences break JSON encoding, logs and other systems downstream |
| No control characters (C0/C1: `U+0000`-`U+001F`, `U+007F`-`U+009F`), except `\n` and `\t` in multi-line fields | NUL truncates strings in C libraries; `\r` and escapes forge log lines and break CSV exports |
| No invisible formatting characters (Unicode category Cf): bidi controls `U+202A`-`U+202E`, `U+2066`-`U+2069`, zero-width `U+200B`-`U+200D`, `U+2060`, BOM `U+FEFF`, soft hyphen `U+00AD` | A bidi override makes `invoice_fdp.exe` render as `invoice_exe.pdf`; zero-width characters make two names that look identical, enabling impersonation in member lists and audit logs |
| Single-line fields refuse line breaks and tabs | They break layouts, emails' subject lines and log lines |

Typical limits: names 1-80 or 1-100, descriptions 0-500 (multi-line), free-text reasons 10-500, search `q` 0-100. The same numbers go in the table's `CHECK` constraints (see [migrations.md](migrations.md)) and in the web app's form validation, so all three agree.

Email addresses: parse with a real address parser, lower-case the stored form, cap at 254 characters. Record an email in an event only once it parses (people type passwords into email fields).

## Enums

- Closed sets defined once in server code (`ProjectStatuses = ["Planned", "In progress", "Completed"]`), checked on input (422 with `field`), mirrored as a `CHECK` in the table, and exported to the client's types.
- An enum with a natural order (workflow states, roles from most to least access) sorts in that order, using the server's list passed as a bound parameter (`array_position(?::text[], status)` in PostgreSQL, a `CASE` built from the list elsewhere), never alphabetically and never with SQL built from the value.
- A role you may assign is checked against the caller's own `assignableRoles`, not just "is a valid role" (an admin can't make someone owner).

## Public IDs in the path

```
id = parsePublicID("prj", request.pathParam("project"))
if !id.ok: return 404 {"error": "Not found."}       // no query at all
row = data.getProject(ctx, tenant.internalID, id)  // WHERE public_id = ? AND tenant_id = ?
if row missing: return 404 {"error": "Not found."}
```

- Shape: `^prj_[0-9A-Za-z]{14}$`. Exact prefix for this route's type.
- Checking before querying saves a round trip on garbage, keeps a bigint parse error from surfacing as a 500, and keeps the 404 identical for every kind of miss.
- IDs in a body or query (`?project=prj_...` as a filter, `{"projectId": ...}`) go through the same parser; a well-formed ID that matches nothing in the tenant is "no match" for a filter (empty list) and 404 or 422 for a write, documented per route.

## Tokens never in the URL

Invitation links, password-reset links, email-verification links, unsubscribe and block links carry a secret token. The API takes it **only in the JSON body**:

```http
POST /api/invitations/check    {"token": "k7J..."}   -> 200 {workspace, invitedBy, email, role, expiresAt} | 422 field "token"
POST /api/invitations/accept   {"token": "k7J..."}   -> 200 the tenant | 422 field "token"
```

Why: request URLs are written to access logs, proxy and CDN logs, browser history, analytics tools, error trackers and `Referer` headers sent to third parties. Bodies are not. The same goes for any secret, personal data (emails) or search terms you'd rather not log: if it's sensitive, it's a POST body. How the web page reading a link handles the token in its own URL is covered by `security`.

- Store only a digest of the token (SHA-256) and compare digests; the raw token exists only in the email and the request.
- A dead link (expired, used, unknown) is 422 with `field: "token"`, one message for all three.

## Things the server derives, never accepts

| Value | Comes from |
| - | - |
| Tenant | The path, resolved through membership |
| Acting user | The session or credential |
| `created_at`, `updated_at`, IDs | The database |
| Role of a new tenant's creator | Fixed (`owner`) |
| Prices, totals, permissions, quotas | Server-side tables |

If a client sends one of these, it's an unknown field: 400.
