# Members and roles

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

A project's **members** are the users who manage it in the dashboard. Each has one **role**, which decides what they can do. People join by **invitation**. Legacy had a role column that nothing read and no way to invite anyone; this plan makes both real.

## Today

- `tenant_users` (`database/migrations/2026_10_06_022459_create_tenant_users_table.php`): `tenant_id`, `user_id`, `role` (`App\Enums\UserRole`: `member`, `admin`).
- The creator becomes `admin`. Nothing checks roles: "every account is an administrator".
- No invitations, no removing members, no leaving, no transferring a tenant.

## Goals

- Four roles, simple enough to explain in one table, enforced by one function every handler calls.
- Invitations by email that work whether or not the person has an account yet.
- At least one owner, always.
- Every membership change in the [audit log](../projects.md#audit-log).

### Non-goals

- Custom roles or per-resource grants (Later, if asked for).
- Restricting a member to some profiles or clusters (Later; see [Open questions](#open-questions)).

## Design

### Roles

| Role | For | In short |
|---|---|---|
| `owner` | Whoever is accountable for the project | Everything, including deleting the project, transferring ownership and managing other owners |
| `admin` | People who run it day to day | Everything but deleting the project and managing owners |
| `member` | Developers using it | Work with sandboxes, profiles' sandboxes, templates; read clusters, profiles and settings; no keys, no members, no settings |
| `viewer` | Support, finance, auditors | Read everything a member can read; change nothing; no terminals, files or agent prompts |

### What each role can do

`R` reads, `W` writes (create, change, delete). Instance admins can do everything on every project.

| Area | owner | admin | member | viewer |
|---|---|---|---|---|
| Project: name | W | W | R | R |
| Project: slug, delete, transfer ownership | W | | | |
| Members and invitations: owners | W | R | R | R |
| Members and invitations: admins, members, viewers | W | W | R | R |
| Admin keys | W | W | | |
| Audit log | R | R | | |
| Settings: branding, retention, sign-ins, defaults | W | W | R | R |
| Settings: GitHub App (register, remove) | W | W | R | R |
| Clusters and nodes: create, install, suspend, remove | W | W | R | R |
| Profiles: create, change, suspend, delete; limits | W | W | R | R |
| Profiles: keys | W | W | | |
| Profiles: credentials, Git, SSH keys, telemetry (the setup page), setup links | W | W | W | R |
| Profiles: start a sign-in | W | W | W | |
| Sandboxes: list, read, activity | R | R | R | R |
| Sandboxes: create, start, stop, delete, settings | W | W | W | |
| Inside sandboxes: terminal, commands, files, ports, SSH keys | W | W | W | |
| Agents: prompt, reply, cancel; read conversations | W | W | W | R |
| Templates: create, rebuild, delete | W | W | W | R |
| Webhooks: endpoints, secrets, redeliveries | W | W | R | R |
| Credits and rates ([credits](../credits.md), Beta) | W | W | R | R |
| Notifications | R | R | R | R |

This table is the spec. It lives in code as one map from `(role, action)` to allowed, `internal/auth.Can(role, action)`, and every handler calls it with a named action, never a role directly. The actions are the [permission names](../api/permissions.md) (e.g. `profiles.keys`), plus the human-only ones (managing members, invitations and admin keys, deleting or transferring the project); the two plans must agree, and a test checks this table against the roles in the registry. The role-matrix test walks every route with every role ([Testing](#testing)).

**A member can work inside sandboxes, which means they can read the profile's credentials there** (anything in a sandbox can). That's why members see credentials as write-only in the API but are trusted with sandboxes. A viewer can't open terminals or files for that reason.

### Data model

```sql
create table project_members (
  id          bigint generated always as identity primary key,
  project_id  bigint not null references projects(id) on delete cascade,
  user_id     bigint not null references users(id) on delete cascade,
  role        text not null,                  -- owner | admin | member | viewer
  invited_by  bigint references users(id) on delete set null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  unique (project_id, user_id)
);

create table project_invitations (
  id           bigint generated always as identity primary key,
  public_id    text not null unique,          -- inv_…
  project_id   bigint not null references projects(id) on delete cascade,
  email        citext not null,
  role         text not null,
  token_hash   bytea not null unique,         -- SHA-256 of lbv_… in the email's link
  invited_by   bigint references users(id) on delete set null,
  expires_at   timestamptz not null,          -- 7 days
  accepted_at  timestamptz,
  accepted_by  bigint references users(id) on delete set null,
  revoked_at   timestamptz,
  created_at   timestamptz not null default now()
);
create unique index on project_invitations (project_id, email) where accepted_at is null and revoked_at is null;
```

### Invitations

1. An owner or admin invites by email and role (`POST /v0/projects/{prj}/invitations`). Admins can't invite owners. Inviting someone already a member is `409 already_member`; re-inviting a pending email replaces the old invitation (new token, new expiry).
2. Lewbox emails a link, `{instance}/invitations/{token}`, naming the project and who invited them. The token (`lbv_` + 64 hex) is stored hashed.
3. Opening the link shows the project and the role, and a button to accept. If the person isn't signed in, they sign in (or sign up) with a code **to the invited address**; sign-up is allowed for an invited email even on an `invite_only` instance ([instance-administration](instance-administration.md#policies)).
4. Accepting checks the signed-in user's email matches the invitation's (case-insensitively), then makes them a member with the role, in one transaction with the audit event. A different signed-in email is refused with `403 invitation_email_mismatch`, saying which address it was for.
5. Signed-in users also see pending invitations to their address in the dashboard (`GET /v0/me/invitations`), and can accept there without the link (`POST /v0/me/invitations/{inv}/acceptance`) or decline.
6. Invitations expire after 7 days and can be revoked by owners and admins.

Later: invitation links that work for anyone on a domain ("anyone at acme.dev can join as member"), as a project setting.

### Ownership

- A project always has at least one owner. Removing, demoting or deleting the last owner is refused with `409 last_owner`.
- An owner can make another member an owner, then step down. `POST /v0/projects/{prj}/ownership-transfers { member }` does both in one step: the member becomes owner, the caller becomes admin. Needs [re-authentication](users-and-login.md#re-authentication).
- An instance admin can make any user an owner of any project, for recovering a project whose owners have left ([instance-administration](instance-administration.md)).

### Leaving and removing

- Any member can leave (`DELETE /v0/projects/{prj}/members/me`), except the last owner.
- Owners remove anyone; admins remove admins, members and viewers.
- A removed member's sessions stay valid for their other projects, but every request to this project is `404` from then on. Sandbox keys they made in the dashboard (15-minute keys for terminals and chats) are revoked at once, so an open terminal closes.
- What a member made stays: profiles, sandboxes, keys (`created_by` becomes null when the user is deleted, but not when they leave).

## API

| Method | Path | Role | What |
|---|---|---|---|
| `GET` | `/v0/projects/{prj}/members` | any | Members, with user `id`, `email`, `name`, `role`, `created_at` |
| `PATCH` | `/v0/projects/{prj}/members/{usr}` | owner, admin* | `{ role }` |
| `DELETE` | `/v0/projects/{prj}/members/{usr}` | owner, admin* | Remove; `me` to leave |
| `GET` | `/v0/projects/{prj}/invitations` | any | Pending invitations |
| `POST` | `/v0/projects/{prj}/invitations` | owner, admin* | `{ email, role }`, answers the invitation (without its token) |
| `DELETE` | `/v0/projects/{prj}/invitations/{inv}` | owner, admin* | Revoke |
| `GET` | `/v0/invitations/{token}` | anyone | The invitation's project name, inviter and role, for the accept page. `404` for unknown, used, revoked or expired |
| `POST` | `/v0/invitations/{token}/acceptance` | signed in | Accept |
| `GET` | `/v0/me/invitations` | signed in | Invitations to my email |
| `POST` | `/v0/me/invitations/{inv}/acceptance` | signed in | Accept without the link |
| `DELETE` | `/v0/me/invitations/{inv}` | signed in | Decline |
| `POST` | `/v0/projects/{prj}/ownership-transfers` | owner | `{ member }` |

\* Admins only for admins, members and viewers.

Admin keys can't manage members: membership is about people, and is done by people.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | The caller's role can't do it, e.g. an admin inviting an owner; the message names the role needed |
| `403` | `invitation_email_mismatch` | Accepting an invitation for another address |
| `409` | `already_member` | Inviting a member |
| `409` | `last_owner` | Removing or demoting the last owner, or the last owner leaving |
| `422` | `validation_failed` | An unknown role, an invalid email |

## Security

- The invitation token travels only in the email; the API never returns it. Acceptance needs the matching email signed in, so a forwarded link is useless to someone else.
- Invitations are rate-limited per project (50 per hour) so Lewbox can't be used to send mail at volume.
- Role checks happen in one function and are covered by the matrix test; there are no role checks in the dashboard that the API doesn't also make.

## Observability

Audit events: `member.invited`, `invitation.revoked`, `invitation.accepted`, `invitation.declined`, `member.role_changed` (with `from` and `to`), `member.removed`, `member.left`, `ownership.transferred`.

## Testing

- Role matrix: generated from the table above; for every route and role, the expected status.
- Invitations: new user on `invite_only`, existing user, wrong email, expired, revoked, re-invite replacing.
- Last owner cases: leave, demote, remove, delete account.
- Removal revokes the member's sandbox keys and closes an open WebSocket.

## Build plan

Package `internal/members`; the action map in `internal/auth/roles.go`.

1. Migrations.
2. The action map, `auth.Can`, and the middleware that loads the caller's membership.
3. Members endpoints with last-owner rules.
4. Invitations and their email; the accept page in the dashboard.
5. Ownership transfer.
6. The matrix test.

## Open questions

1. **Viewer in the alpha?** Or ship owner, admin and member, and add viewer when someone needs it?
2. **Restricting members to profiles.** An agency might want a contractor to see one customer's profile only. Per-profile membership is a big change to every query; defer?
3. **Admin keys acting for a member.** Should an admin key's audit entries name a member (e.g. Lewagent's user) via a header, so the audit log shows the person behind it?
