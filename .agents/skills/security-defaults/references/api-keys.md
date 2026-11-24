# API keys and personal access tokens

Read with `SKILL.md` section 9. Budgets and quotas numbers are defaults; their rate-limit mechanics are in `production-hardening`.

## Two kinds

| Kind | Authority | Managed by | Survives |
| --- | --- | --- | --- |
| Tenant (service) key | Its saved scopes and resource selection, independent of who created it | Tenant owners/admins | Creator leaving, suspension or deletion |
| Personal access token | Saved scopes **intersected with the owner's live membership, role and eligibility**, evaluated per request | Its owner (tenant admins may inspect metadata and revoke, never edit) | Password change/reset, MFA changes, email change, browser logout |

- A personal token is **permanently revoked**, in the same transaction, when its owner is suspended or leaves/is removed from the tenant. Rejoining or unsuspending never revives it.
- Demotion removes effective actions immediately; promotion restores only what was saved. Saved scopes can't exceed the owner's role at creation/edit.
- Each key belongs to exactly **one tenant**.

## Credential format

```
acme_tk_apk_8kQ2mZ1xT4bLwN_q3Jx...43 base64url chars    # tenant key
acme_pk_apk_8kQ2mZ1xT4bLwN_Zr9w...                     # personal token
<app prefix>_<kind>_<public id>_<32 random bytes>
```

- The **app prefix** lets GitHub/GitLab secret scanning and your own log scrubber recognise leaked keys.
- The **public ID** names the key in UIs, logs and audit events without revealing it.
- Store only `SHA-256(complete credential)`. Look up by digest; constant-time compare. Scrub whole credentials in logs **before** generic rules (an all-lowercase/digit secret may not look like a token).

## Transport

- Exactly one `Authorization: Bearer <credential>` header, HTTPS in production. No query string, body, cookie or `X-API-Key` alias. Two headers = 400/401.
- Machine routes live under their own prefix (`/api/v1/...`) and middleware branch: JSON required, **no Origin/CSRF** (there is no browser), but every other default stays (no-store, headers, request ID, timeouts, caps, client IP resolution, IP blocks).
- Browser cookies have **no authority** on machine routes. Keys have **no authority** on browser routes, page gates or metrics.
- Browser mutations keep Origin + JSON + CSRF **even if** they carry a bearer header.
- CORS stays off. Machine traffic is never CAPTCHA-challenged.
- Never fabricate a human session or member for a service key; authorise against the key's own principal.

## Responses

| Case | Status |
| --- | --- |
| Missing, malformed, unknown, expired, revoked, ineligible owner | **401** generic, `WWW-Authenticate: Bearer`, no redirect, no cookie |
| Wrong tenant, unknown resource, resource outside the key's selection | **404**, identical |
| In scope for the tenant but lacking this action | **403** |

## Scopes and selection

- Scopes reuse permission names: `project.index`, `project.show`, `project.store`, `project.update`, `project.destroy`. UI groups them (Read = index + show). Default is read-only.
- **All resources** = current and future resources of that type in the bound tenant only, never other tenants or future resource types.
- **Selected resources** = 1-100 explicit IDs; can't include create. Apply the selection as a **SQL predicate before** search, counts, sort and pagination, and again on writes. Deleting the last selected resource leaves zero access, never "all".
- Unknown kinds, scopes, roles or modes fail closed.

## Lifecycle

- **Creation and broadening** (adding scopes/resources, selected to all) need a live browser session, CSRF and **fresh proof** chosen by the server (password, or code with MFA), checked in the creating transaction. Renaming, narrowing, shortening expiry and revoking need only the session and CSRF.
- A key can never manage keys. Management is browser-session only.
- **Expiry is explicit** at creation: a finite date (UI default 90 days; range 1 hour to 365 days) or `null` for never. Omitted = 422. Finite expiry can be shortened, never extended or changed to never; never can be shortened. Ended keys can't be edited or reactivated.
- **Revocation** clears the digest immediately; repeated revoke is 204 with no second event. No positive credential cache, so the next request fails. An already-admitted read may finish; writes re-authorise inside their transaction.
- **Quotas:** e.g. 100 live keys per tenant (both kinds), 50 live personal tokens per user. "Live" includes unexpired never-expiring keys.
- **Rotation:** create replacement, deploy it, revoke old. At quota, free a slot first. No automatic revocation, no secret recovery.

## Shown once

- `201 {key: <metadata>, secret: <credential>}` with `Cache-Control: no-store`. The only time the plaintext exists outside the caller.
- The UI holds it in the creation dialog's component state, masked behind Reveal, with Copy that works while masked. Done, navigation, tenant switch or sign-out discards it. Closing early warns but does not revoke.
- Never in the client data cache, browser storage, toasts, screenshots in docs, events, errors, logs or data exports. Never auto-retry an uncertain creation (it could mint two keys).
- Metadata responses: ID, kind, name, tenant, owner/creator name, scopes (saved and effective), selection, created/expires/last-used/revoked times. **Never** emails, digests or internal IDs. `lastUsedAt` is coarse (at most hourly writes) and never updates the human's "last seen".
- Another tenant's key, someone else's personal token and an unknown ID are the same 404 in management routes.

## Idempotency-Key (optional, on creates)

```
POST /api/v1/tenants/tnt_.../projects
Idempotency-Key: 9f1c2b7e-4a1d-4c55-9a7e-2f3d0b8a61c4
```

- 16-128 chars of `[A-Za-z0-9._~-]`, caller-generated randomly. Malformed or repeated header = 400. Missing = ordinary create, no retry guarantee.
- Scope: credential + tenant + operation + key value, for **24 hours**.
- Same canonical validated input = return the **original 201 snapshot** without creating, spending quota or emitting an event. Different input, or the original since deleted = **409**.
- Store digests of the key value and the input, the created public ID (a snapshot, not a foreign key) and a bounded response snapshot. **Never the raw header; never log it or put it in a URL.**
- Reservation, quota spend, record and event share one transaction; a rollback leaves no reservation.
- Every retry re-authenticates and rechecks authority: a replay can't return data the credential has since lost access to.
- Cap retained rows per tenant (e.g. 5,000; then 503 + `Retry-After` for new idempotent creates, replays still served). Clean up expired rows in bounded batches.

## Audit

Lifecycle events (e.g. `workspace.api_key.created/updated/revoked`, `auth.api_key.*` for personal tokens) are security audit events. Writes made by a key carry the key's public ID. What a key only reads isn't recorded. Never the secret.

## Seeds and tests

Seeders never issue usable keys. Tests cover: wrong-tenant 404, scope 403, revoked 401, demoted owner loses actions, removal revokes, browser cookie ignored on machine routes, bearer ignored for CSRF on browser routes, idempotent replay and conflict.
