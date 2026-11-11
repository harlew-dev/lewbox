# Phase 1: customers, sign-up, plans and limits

**Goal:** anyone can sign up on the instance and get an organization of their own, on a plan the operator controls. The operator can approve, suspend and re-plan customers. Customers can't see anything that belongs to the operator.

No AerolVM calls in this phase. Limits are stored here and pushed to clusters in Phase 3.

## 1. Lock down instance routes (security fix)

Today `ListClusters`, `GetCluster`, `ResolveCluster`, `ListNodes` and `GetNode` use `requireUser`, so any signed-in account can read cluster admin URLs and node details. Make them `requireAdmin`. Customers get **regions** instead:

- `GET /v0/regions` → `{"data":[{"slug","name","endpoint_url","runtime","status"}]}`: clusters whose status is `ready` or `degraded`, ordered by slug. Any signed-in principal, including organization API keys. No admin URL, token, node or version.
- CLI: `lewbox region list`.

## 2. Schema (migration `00006_customers.sql`)

```sql
-- Instance settings, one row per key. Values are JSON.
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE plans (
    id            TEXT PRIMARY KEY,           -- UUIDv7
    slug          TEXT NOT NULL UNIQUE,       -- DNS label, never a UUID
    name          TEXT NOT NULL,
    max_sandboxes INTEGER,                    -- NULL = unlimited
    max_vcpus     INTEGER,                    -- NULL = unlimited (whole vCPUs)
    max_memory_mb INTEGER,                    -- NULL = unlimited
    created_at    DATETIME NOT NULL
);

ALTER TABLE organizations ADD COLUMN team TEXT;           -- AerolVM team, set once, never reused
ALTER TABLE organizations ADD COLUMN plan_id TEXT REFERENCES plans(id);
ALTER TABLE organizations ADD COLUMN max_sandboxes INTEGER; -- per-organization overrides, NULL = plan's
ALTER TABLE organizations ADD COLUMN max_vcpus INTEGER;
ALTER TABLE organizations ADD COLUMN max_memory_mb INTEGER;
ALTER TABLE organizations ADD COLUMN status_reason TEXT NOT NULL DEFAULT '';
UPDATE organizations SET team = 'lbx-' || replace(id, '-', '');
CREATE UNIQUE INDEX organizations_team ON organizations(team);
```

Seed in the migration: a plan `free` ("Free": 2 sandboxes, 2 vCPUs, 4096 MB) and settings `default_plan` = its id, `signup` = `"closed"`, `instance_name` = `"Lewbox"`. Existing organizations get the `free` plan.

`team` is `lbx-` + the organization ID's 32 hex digits: unique, valid as an AerolVM team name (`[a-z0-9-]`, ≤ 63), never reused because IDs are never reused. New organizations set it on insert.

Organization `status` gains `pending` (signed up, waiting for approval). `status_reason` says why an organization is suspended: `""`, `admin`, `credits` (Phase 5).

## 3. Settings

`GET /v0/settings` (admin) and `PATCH /v0/settings` (admin), CLI `lewbox settings` and `lewbox settings set --signup open --default-plan free --name "Acme Sandboxes"`.

```json
{"instance_name":"Acme Sandboxes","signup":"open","default_plan":"free"}
```

- `signup`: `open` (sign-up creates an active organization), `approval` (sign-up creates a `pending` organization the operator approves), `closed` (no sign-up; operator creates accounts).
- `default_plan`: a plan slug; the plan new sign-ups get.

`GET /v0/instance` is **public** (no auth; like `/v0/health`): `{"name":"Acme Sandboxes","signup":"open"}`. The login page uses it to show "Create an account". HTTP-only, no CLI command (documented exception).

## 4. Plans

Admin only. `GET /v0/plans`, `POST /v0/plans {slug,name,max_sandboxes,max_vcpus,max_memory_mb}`, `GET /v0/plans/{ref}`, `PATCH /v0/plans/{ref}` (any field; `null` means unlimited, so use pointer-to-pointer or a `json.RawMessage` aware decode for the three limits), `DELETE /v0/plans/{ref}` (refused with 409 while organizations use it or it is the default plan). Each plan in a response also has `"organizations": <count>` and `"default": bool`.

CLI: `lewbox plan list|show|create|update|delete`, flags `--name --max-sandboxes --max-vcpus --max-memory` (`--max-memory` accepts `4096`, `4096MB`, `4GB`; `0` or `unlimited` clears the limit).

## 5. Organizations as customers

`Organization` JSON gains:

```json
{
  "team": "lbx-0196…",
  "plan": {"slug":"free","name":"Free"},
  "status_reason": "",
  "limits": {"sandboxes": 2, "vcpus": 2, "memory_mb": 4096},    // effective: override, else plan; null = unlimited
  "overrides": {"sandboxes": null, "vcpus": null, "memory_mb": null}
}
```

`team` and `overrides` are only shown to instance admins. `limits` and `plan` are shown to members too.

`PATCH /v0/organizations/{ref}` gains (instance admin only): `plan` (slug), `max_sandboxes`, `max_vcpus`, `max_memory_mb` (number or `null` to fall back to the plan), and `status`: `active` | `suspended` (approving a `pending` organization is `status: active`). Setting `suspended` by an admin sets `status_reason: admin`; `active` clears it.

CLI: `lewbox org update <org> --plan pro --max-sandboxes 10 --max-vcpus unlimited`, `lewbox org approve <org>` (= status active).

`ListOrganizations` for an admin gains `?status=pending|active|suspended` filtering.

`CreateOrganization` (admin) takes an optional `plan` (default: the default plan).

## 6. Sign-up

`POST /v0/auth/signup {"email","password","organization"}` (public; `organization` is a display name, optional, default "<local part>'s organization").

1. Refused with `403 forbidden` when `signup` is `closed`.
2. Rate limited with the same limiter as sign-in (per IP).
3. Creates a normal account (never an instance admin), then an organization (slug: the email's local part lowercased to `[a-z0-9-]`, trimmed to 40, plus `-` and 6 random lowercase base32 characters; `team` set; plan = default plan; status `active` or `pending` by the sign-up mode), then the owner membership. On any failure after the account is created, delete what was created so the email can be retried.
4. Same password rules as `CreateUser`. A taken email answers `409 conflict` ("an account with this email already exists; sign in instead").
5. Signs the new account in as a **web** session and returns the same body as `POST /v0/auth/session`, setting the same cookie. HTTP-only, no CLI command (documented exception, like the web session).

The service method is `Signup(ctx, SignupRequest) (token string, resp SessionResponse, err error)`, shaped like `WebLogin`.

## 7. Who can do what

| | Instance admin | Org owner/admin | Org member/viewer | Org API key | Customer with no org |
|---|---|---|---|---|---|
| clusters, nodes, users, plans, settings | yes | no | no | no | no |
| regions | yes | yes | yes | yes | yes |
| change own org's plan, limits or status | — | no | no | no | — |
| see own org's plan and effective limits | yes | yes | yes | yes | — |

A `pending` organization can be seen by its members but not managed (same rule as suspended: `accessManage` refused).

## 8. `me`

`GET /v0/me` already returns the user. Add `"organizations": [{id, slug, name, role, status}]` for user principals, so the web app can choose portal vs console and the current organization without another call. Keep the existing fields.

## Done when

- Unit tests for: signup modes (open/approval/closed), slug collisions, conflict on taken email, rollback on failure, plan CRUD and in-use deletion refusal, override/effective limits, admin-only routes now refusing non-admins (clusters, nodes, plans, settings), regions visible to customers and keys without admin fields, `/v0/instance` public.
- Parity tests (`cmd/lewbox/parity_test.go`, `internal/backend/backend_test.go`) for every new CLI command.
- `docs/reference/cli.md`, `docs/reference/http-api.md` and a new `docs/accounts/customers-and-plans.md` updated; AGENTS.md lists the new HTTP-only exceptions (`/v0/instance`, `/v0/auth/signup`).
- `gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go build ./...` pass.
