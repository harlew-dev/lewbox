# Permissions

> Status: Draft · Milestone: Alpha (M0 framework; permissions arrive with their features) · Updated: 2026-10-29

What each principal may do, once [authentication](authentication.md) knows who it is. One registry of permissions in Go drives the checks, the key-creation forms, the OpenAPI document's descriptions and the docs. Keys get permissions when they're made; users get them from their role in a project; sandbox keys get a subset of their maker's; setup sessions get sections.

## Today

- **Profile keys** have permissions, chosen when made, full access (`null`, including ones added later) or a list: `sandboxes.create|update|start|stop|delete|exec|ports|ports.view|agents|activity|activity.view` and `templates.create|delete` (`lewbox-legacy/app/Enums/ApiKey/Permission.php`). Every key can read its profile and list and read its sandboxes, operations, ports and SSH keys, and list conversations.
- **Tenant keys have no permissions**: a tenant key can do everything the Admin API offers.
- **Dashboard users have no permissions**: "every account is an administrator". `UserRole` has `member` and `admin`, unenforced.
- **Sandbox keys** may have the "inside a sandbox" permissions, defined **twice**: in the app (`Permission::insideSandbox()`, six permissions) and in the gateway (`internal/server/keys.go`, whose validation message lists four). A deployment where the two disagreed refused `sandboxes.agents` asked for by name, so Lewagent mints keys with no permissions listed, which gives them *everything* the profile key has, and falls back on a validation error (`lewagent-legacy/app/Http/Controllers/Computer/AgentKeyController.php`). A bug in a permission list turned into broader keys.
- Permission checks were in the gateway (`requirePermission`, `refusePermission`) and in the app's form requests: two places again.

## Goals

- **One registry**: each permission defined once, with its name, label, group, which principals can hold it, and whether a sandbox key can. Everything else is generated from it, so the sandbox-key bug can't happen again.
- **Every principal has permissions**, including admin keys and users: least privilege for project automation, and roles for teams.
- **Fixed at creation for keys.** Changing a key's permissions means making a new key, as now: a key never quietly gains or loses power, and the auth cache never holds stale permissions.
- **Checked once**, by route metadata, before the handler; handlers that only know the permission after reading the body (a `PATCH` changing `status` vs settings) call one helper.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Every existing permission name and meaning. Full access vs a chosen list. Reading is free: every key can read what it reaches. `404` for what a key can't reach, `403 permission_denied` for what it can see but can't do |
| **Change** | Admin keys get permissions (new management permissions, plus the sandbox ones) |
| **Change** | Users get roles in each project, which map to permissions |
| **Change** | One list of sandbox-key permissions; asking for any of them by name works |
| **Add** | `profiles.*`, `clusters.*`, `webhooks.*`, `members.read`, `project.*`, `git.*` permissions |

---

## Design

### The registry

```go
type Permission struct {
    Name        string   // "sandboxes.exec"
    Group       string   // "Sandboxes"
    Label       string   // "Run commands, sessions and files"
    Description string   // for docs and the key form
    Holders     Holders  // which principal kinds may hold it
    SandboxKey  bool     // may a sandbox key have it
    Implies     []string // e.g. sandboxes.activity implies sandboxes.activity.view
    Since       string   // milestone
}
```

`internal/auth/permissions.go` registers the core's; **plugins register their own** in `Register` ([plugins/extension-points](../plugins/extension-points.md#permissions)), with which roles include them and whether a sandbox key may hold them. Names are unique across core and plugins (a clash fails start-up). Generated from the merged registry: the OpenAPI document's `Permission` enum and each operation's `x-lewbox-permission`; the key creation forms in the dashboard; the docs table below; the SDKs' types. A test checks every route's permission exists and every permission is used by some route, for the default build and the core-only build.

Permissions owned by plugins in the default build: `git.tokens`, `git.tokens.user` (`git/github`; the names are the Git framework's, so a second Git host uses the same ones), `profiles.credits` (`credits`). A key holding a permission of a plugin that's off keeps it; requests are refused with `409 plugin_disabled` ([plugins](../plugins.md#turning-plugins-on-and-off)). A key with full access gets plugins' permissions too, including those of plugins compiled in later.

### Sandbox permissions

Held by admin keys, profile keys and users (through roles); those marked **K** also by sandbox keys.

| Permission | K | Allows |
|---|---|---|
| *(none)* | | Reading what the principal reaches: sandboxes, their operations, ports and SSH keys; listing and reading conversations; templates; `/v0/profile` for a profile key |
| `sandboxes.create` | | Creating sandboxes |
| `sandboxes.update` | | Changing a sandbox's metadata, idle timeout, lifetime and `warn_before`. With `sandboxes.exec`, its `hooks`, `stop_grace` and `scrub` paths ([sandboxes/hooks](../sandboxes/hooks.md#permissions)) |
| `sandboxes.start` | | Starting a stopped sandbox. Restarting needs this and `sandboxes.stop` |
| `sandboxes.stop` | | Stopping a running sandbox |
| `sandboxes.delete` | | Deleting sandboxes |
| `sandboxes.keys` | | Making sandbox keys for browsers. A sandbox key's permissions are a subset of its maker's ([profiles/profile-keys](../profiles/profile-keys.md)) |
| `sandboxes.exec` | ✓ | Everything through the toolbox: commands, sessions (reading them too, as their logs may hold secrets), files, directories, SSH keys on the sandbox. Also needed, beside `sandboxes.create` or `sandboxes.update`, to set a sandbox's hooks or scrub paths, as hooks run code; a sandbox key never can ([sandboxes/hooks](../sandboxes/hooks.md#permissions)). Touching a sandbox (`POST …/touch`) needs this, `sandboxes.ports.view` or `sandboxes.agents` |
| `sandboxes.ports` | ✓ | Exposing and closing ports, public or private |
| `sandboxes.ports.view` | ✓ | Making links to private ports |
| `sandboxes.agents` | ✓ | Prompting agents, following turns, replying to approvals and questions, cancelling, steering, tasks, deleting conversations, reading stored messages and chunks, `GET …/agents` |
| `sandboxes.activity.view` | ✓ | Reading activity logs |
| `sandboxes.activity` | ✓ | As `sandboxes.activity.view`, and adding `app.*` events. Implies `sandboxes.activity.view` |
| `templates.create` | | Creating and rebuilding templates |
| `templates.delete` | | Deleting templates |
| `sandboxes.git` | ✓ | Changing a sandbox's Git setting (`PUT /v0/sandboxes/{sbx}/git`): its access (`read`, `write`, `full` or a permission set) and repositories, each with its own access, up to the project's cap. The cap, not the key, is the ceiling ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)) (M3) |
| `git.tokens` | | Issuing installation tokens for the profile's GitHub connection (`POST …/git/tokens`), narrowed to repositories and permissions ([git/tokens](../git/tokens.md#the-token-api)) (M3) |
| `git.tokens.user` | | **Admin keys only.** Issuing a named person's GitHub user token, which can't be narrowed. Profile keys can't hold it, and sandbox keys never issue tokens (M3) |

Legacy's `git.grants`, and the proposed `git.pushes`, `git.pulls` and `git.repositories`, are gone with the push, pull request and repository-creation routes ([git](../git.md#keep-change-drop)).

### Management permissions

Held by admin keys and users (through roles). Profile keys and sandbox keys never hold them.

| Permission | Allows |
|---|---|
| `project.read` | Reading the project, its settings and notifications |
| `project.write` | Changing the project's name and settings, which plugins are on and their project settings, its GitHub App (sessions only for registration) |
| `members.read` | Listing members and invitations |
| `profiles.read` | Listing and reading profiles, their limits, keys (not tokens), agents' credential status, Git, telemetry, SSH keys, repositories |
| `profiles.write` | Creating, changing, suspending and deleting profiles; setting limits |
| `profiles.keys` | Creating and revoking profile keys |
| `profiles.credentials` | Setting agent credentials, running sign-ins, Git, telemetry, profile SSH keys; making setup links |
| `profiles.credits` | Credit buckets and transactions (Beta; the `credits` plugin) |
| `clusters.read` | Reading clusters, installations, nodes and their metrics |
| `clusters.write` | Creating and changing clusters, storage and DNS settings, installations, cordoning, draining and removing nodes, upgrades |
| `webhooks.read` | Reading webhook endpoints and deliveries |
| `webhooks.write` | Creating, changing, testing and deleting endpoints; rolling secrets; redelivering |

Not permissions at all, because only some users may, never keys ([what stays human](../api.md#what-stays-human)): managing admin keys, members and invitations, deleting or transferring the project, instance administration. These check the user's role directly.

### Admin key scopes

An admin key has **full access** (`permissions: null`: every sandbox and management permission, including ones added later) or a **chosen list** of any of the above. Lewagent's backend, for example, needs `profiles.read`, `profiles.write`, `profiles.keys`, `profiles.credentials`, the sandbox permissions and `templates.create`, but not `clusters.write` or `webhooks.write`.

An admin key reaches every profile's sandboxes in its project, inside too: **admins are admins** ([api.md, A3](../api.md#decisions)). There's no separate profile scope, and profiles can't refuse it. A project that wants an operations key that can't read its customers' files leaves out `sandboxes.exec` and `sandboxes.agents`; the permission list already says it.

### Profile keys

As today: full access, or a chosen list of the [sandbox permissions](#sandbox-permissions). **Full access leaves out `git.tokens`**: a key that may mint GitHub write tokens must name it when it's made, so a key made for a dashboard or a job runner can't. **`git.tokens.user` can't be held by a profile key at all** (`422 validation_failed` when making one with it): user tokens are for the project's own servers and its dashboard (decided 2026-10-29, [git/tokens](../git/tokens.md#user-tokens)). Plus **cluster access**: all of the profile's clusters (including later ones), or a chosen list. Both fixed at creation.

<a id="profile-alias"></a>On `/v0/profile/…` (the [alias](../api.md#where-resources-live) of its own profile), a profile key may: read the profile, its limits and usage; change `default_cluster`; read its GitHub connection, repositories and branches; with `git.tokens`, issue GitHub installation tokens (never user tokens). It may not change credentials, keys, limits or anything else: those are the project's to manage, by admin key, session or setup link.

### Sandbox keys

- **Eligible permissions** are exactly those marked **K** in the registry: `sandboxes.exec`, `sandboxes.ports`, `sandboxes.ports.view`, `sandboxes.agents`, `sandboxes.activity`, `sandboxes.activity.view`, `sandboxes.git`. The mint endpoint's validation, the docs, the SDK types and the dashboard all read that one list.
- **Asked for by name**: each must be eligible (`422 validation_failed` on `permissions.N` naming the eligible ones) and held by the minter (`403 permission_denied` naming it). A key can never be broader than its maker.
- **Left out**: the key gets every eligible permission its maker holds. Lewagent's workaround (ask for none) still works, but there's no reason to use it.
- **Reading is free**: the sandbox itself, its operations, ports, SSH keys, and listing its conversations, with no permission.
- **Regression test** for the legacy bug: minting with each eligible permission alone, and all together, succeeds for a full-access minter.

### Roles

Users hold one role in each project they belong to ([projects](../projects.md)). Roles are fixed sets of permissions, plus the human-only abilities.

| Role | Permissions | Human-only abilities |
|---|---|---|
| **Owner** | All | Everything: members, admin keys, delete or transfer the project. At least one per project (`409 last_owner`) |
| **Admin** | All | Members (not owners), invitations, admin keys |
| **Member** | All sandbox permissions; `templates.*`; `project.read`, `members.read`, `profiles.read`, `clusters.read`, `webhooks.read` | — |
| **Viewer** | Reading only: `project.read`, `members.read`, `profiles.read`, `clusters.read`, `webhooks.read`, `sandboxes.activity.view` | — |

A viewer can see a sandbox but not open a terminal in it. Custom roles are Later.

Instance admins are separate: a flag on the user (set by configuration, `LEWBOX_INSTANCE_ADMINS`, or by another instance admin). Being an instance admin doesn't make someone a member of every project; it lets them see and suspend projects through `/v0/instance`, and, if they join a project to help, they get a role like anyone.

### Setup sessions

Not permissions but **sections**, chosen when the link is made: `agents` (credentials and sign-ins), `git` (token or GitHub App, identity), `ssh_keys`, `telemetry`. A setup session reaches `/v0/profile` (read), and the sub-resources of its sections. Anything else under `/v0/profile`: `403 section_forbidden`. Anything outside: `403 wrong_key_kind`.

### How a route is checked

Route metadata declares, for each route: the principal kinds accepted, and the permission needed (or `none`, or "decided in the handler"). Middleware, in order:

1. **Kind**: the principal's kind is accepted, else `403 wrong_key_kind`.
2. **Reach**: the resource is resolved through the principal's scope (project, profile, clusters, sandbox, user's memberships). Not found or out of reach: `404 not_found`. This happens in the loader, with the scope in the query (`WHERE project_id = $principal_project …`), never after a fetch, so an unscoped load can't leak through a forgotten check.
3. **Permission**: held, else `403 permission_denied` with `permission`. For sessions, the user's role in the resource's project.

Handlers that decide late (a `PATCH /v0/sandboxes/{sbx}` that may be a start, a stop or a settings change) call `auth.Require(ctx, perm...)`, which answers the same error.

### Effective permissions

`GET /v0/me` answers, for each of the user's projects, their role and effective permissions; `GET /v0/profile` answers the key's permissions and clusters; the sandbox key mint answers the key's. Clients use these to hide what they can't do, rather than probing.

---

## Security

- Least privilege by default in the dashboard's key forms: new admin keys start with nothing ticked except reading; "Full access" is a deliberate choice with a warning.
- Permissions can't be widened after creation, so a leaked key's power is what its creator saw.
- Scope enforced in queries ([reach](#how-a-route-is-checked)), tested by the principal matrix ([api](../api.md#testing)).

## Testing

- Registry tests: every route's permission exists; every permission is used; every `K` permission can be minted alone.
- Role tests: each role against each route's metadata, generated.
- The principal matrix covers kinds; permission-denied paths are covered by calling each route with a key holding everything but its permission.

## Build plan

`internal/auth`: registry; principal's `Has(perm)`; roles; scope helpers for loaders; generator for OpenAPI, docs and dashboard. M0: registry, sandbox permissions, middleware. M1: management permissions, roles. M3: sections, `sandboxes.git`, `git.tokens`, `git.tokens.user`.

## Open questions

1. ~~**Admin keys and customer data.**~~ Decided: admins are admins. Admin keys reach inside every sandbox in their project; no profile scope, no opt-out for profiles. Leaving out `sandboxes.exec` and `sandboxes.agents` makes a key that can't read customers' files.
2. **Member role and profiles.** Should members be able to create profiles and keys? (Proposed: no; profiles are customers, and admins manage them.)
3. **Custom roles**: wanted by anyone before Beta?
4. **Per-profile access for users** (a user who only sees one profile's sandboxes, e.g. a customer's own staff)? (Proposed: Later; a project per customer covers it today.)
5. **Splitting `sandboxes.exec`** into `sandboxes.commands` and `sandboxes.files` (with `exec` implying both), so a file browser's sandbox key can't run commands? Decided for the alpha: keep `sandboxes.exec` as one permission; the split stays open for after it ([sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md#open-questions)).
