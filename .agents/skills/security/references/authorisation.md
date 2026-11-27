# Authorisation

Read with `SKILL.md` section 7. Permission names follow `naming` (`<resource>.<action>`, e.g. `project.store`, `tenant.member.destroy`).

## The model

Data belongs to a tenant (e.g. workspace, organisation, team). A user reaches a tenant through a membership row with a role. Each role is a set of named permissions, granted in **one table** in code:

```
grants = {
  owner:  {tenant.update, tenant.destroy, tenant.member.index, tenant.member.update, tenant.member.destroy,
           tenant.invitation.index, tenant.invitation.store, tenant.invitation.destroy,
           project.store, project.update, project.destroy},
  admin:  {tenant.update, tenant.member.*, tenant.invitation.*, project.store, project.update, project.destroy},
  member: {project.store, project.update},
  viewer: {},
}

func (role) can(permission) bool:
  return grants[role]?.contains(permission) ?? false      # unknown role -> false
```

- Reading the tenant and its ordinary records requires membership only. Lists that expose other people's personal data (member emails, invitations) need their own permission; viewers don't get them.
- The same permission names are exported to the client's types; a test checks the server list and the client list agree.
- **Fail closed everywhere**: unknown role, unknown permission, unknown scope, unknown access mode, a `switch` without a matching case all deny. Never `default: allow`.

## Route registration enforces it

```
tenant := router.group("/api/tenants/{tenant}", requireSession, requireTenant)
tenant.get("/projects", projects.index)                                   # reads: membership
tenant.can("POST",   "/projects",           "project.store",   projects.store)
tenant.can("PATCH",  "/projects/{project}", "project.update",  projects.update)
tenant.can("DELETE", "/membership",         ANY_MEMBER,        membership.destroy)

# inside the group type:
func handle(method, path, h):
  if method not in {GET, HEAD}: panic("mutation " + path + " must name a permission")
```

- The tenant group **panics/throws at startup** if a mutation is registered without a permission. "Every member may do this" is an explicit `ANY_MEMBER`, so an omission is never mistaken for intent.
- A **role x route matrix test** walks every registered tenant route with every role and asserts the status (2xx/403/404). A new route without a matrix row fails the test.

## Loading the tenant

```
func requireTenant(req, next):
  id = parsePublicId("tnt", req.pathParam("tenant"))      # wrong prefix/shape -> 404, no query
  if !id: return 404
  row = SELECT t.*, m.role FROM tenants t
        JOIN memberships m ON m.tenant_id = t.id AND m.user_id = :sessionUserId
        WHERE t.public_id = :id
  if !row: return 404                                      # missing and not-a-member look identical
  ctx.tenant = row
  return next(req)
```

- One query through the membership: there is no code path that loads a tenant and checks membership afterwards (and forgets).
- Missing tenant, not a member, malformed ID: **the same 404**, so tenant IDs can't be probed.
- `requirePermission(p)` returns **403** only when the caller is a member whose role lacks `p`.

## Never trust the request for identity or scope

| Value | Comes from | Never from |
| --- | --- | --- |
| Acting user | Session / credential in context | Body, query, header |
| Tenant | Path param, resolved through membership into context | Body (`tenantId` in a body is rejected as an unknown field) |
| Role | Membership row (or the key's saved scopes intersected with it) | Body, client-side state |
| Owner of a new record | Context | Body |

```
-- every tenant query takes the tenant from context
SELECT ... FROM projects WHERE tenant_id = :ctxTenantId AND public_id = :projectId
```

A project ID belonging to another tenant, even one the caller is in, returns the same 404 as a missing one. Public IDs are unguessable, but **access never depends on that**.

## Recheck inside the transaction, under locks

Middleware checks happen before the transaction; between check and write, a concurrent request can demote, remove or delete. So authority-dependent writes do:

```
tx:
  tenant = SELECT ... FROM tenants WHERE id = :ctxTenantId FOR UPDATE   # serialise membership changes
  actor  = SELECT role FROM memberships WHERE tenant_id = :t AND user_id = :me
  if !actor || !actor.role.can("tenant.member.update"): return 403
  target = SELECT ... FROM memberships WHERE tenant_id = :t AND user_id = :target
  if !target: return 404
  if !actor.role.assignable.contains(target.role) || !actor.role.assignable.contains(newRole): return 403
  if target.role == owner && newRole != owner && countOwners(t) == 1: return 409 "last owner"
  UPDATE ...
```

- **Global lock order**, documented once and used everywhere, e.g. user row, then session, then tenant, then children by ascending ID. Two flows locking the same rows in opposite orders deadlock under load (invitation accept vs revoke is the classic case).
- Multi-row locks (deleting an account that owns several tenants) take them **in ID order**.
- Typed confirmations (a tenant's name before deletion) are rechecked under the lock, so a tab opened before a rename can't delete under the old name.
- A suspended actor discovered under the lock answers like a dead session (401).

## Role rules worth copying

- **Assignable roles:** an owner may assign any role; an admin only roles below admin; others none. Acting on a member needs the permission **and** both the target's current role and the new role in your assignable set.
- **Invitations never grant owner.** Invite as admin, then promote. A leaked link can't hand over a tenant.
- **Last owner invariant:** demoting, removing or leaving as the only owner is 409. Two owners demoting each other concurrently leave one, because both run under the tenant lock.
- **Acting on yourself** goes through dedicated routes (leave = `DELETE /tenants/{tenant}/membership`), not the member-management routes (422 there).
- Ownership transfer = promote someone, then step down. No separate flow to secure.
- **Removal revokes derived authority in the same transaction** (the removed member's personal API keys for that tenant), so the next request from any credential fails.
- Creating a tenant makes the creator owner **in the same transaction**.

## Telling the UI

The tenant response includes `role`, sorted `permissions` and `assignableRoles` (empty arrays, never null), computed from the grants table. The UI hides or disables controls with `can(tenant, "project.store")`. It never re-implements the rules, and the API always enforces them.

## Account-level routes

Routes not under a tenant (`/api/auth/*`) take the user from the session only. "Who am I acting on" is always the session user; there is no `/users/{user}` write for ordinary users.

## Cross-tenant code

Code that legitimately crosses tenants (operator/admin tooling) lives behind its own authentication, route group and audit (`ops`). Ordinary routes never query without a tenant predicate. If you have a registry of tenant-owned tables, every table with a `tenant_id` is listed there and tested.
