---
name: resource-naming
description: The one naming vocabulary for API routes, handlers, page files, page routes, API client calls, permissions, events and rate limits, based on Laravel resource controllers (index, show, create, store, edit, update, destroy) but framework- and language-agnostic. Use before adding or renaming an endpoint, handler, page, page route, client method, permission, event or rate limit, or whenever you're unsure what to call something.
---

# Resource naming

One vocabulary runs through every layer, so a new name needs no thought and you can work out one name from another. If you know the page is `project/[project]/edit`, you know it calls `projects.show` and `projects.update`, the route is guarded by `project.update`, and the change records `project.updated`.

A project's `AGENTS.md` may record deliberate deviations from this skill; where it does, the project's deviations win.

A wrong name doesn't stay local. The next person (or agent) copies the nearest example, so one off-convention name turns into ten. If this skill doesn't settle a name, **propose one with a reason and ask**. Don't copy a nearby file that might itself be wrong.

## When to use

- Adding or renaming an API endpoint, route, or handler/controller.
- Adding a page, a page route constant, or a link to a page.
- Adding a method to the frontend API client.
- Adding a permission, an audit event or a rate limit.
- Reviewing code for naming drift.

## The vocabulary

| Action    | Kind | Meaning                                                        |
| --------- | ---- | -------------------------------------------------------------- |
| `index`   | API + page | list a collection (one page of it, with search/filter/sort) |
| `show`    | API + page | one record                                               |
| `create`  | page only  | the form that creates a record                           |
| `store`   | API only   | persist a new record                                     |
| `edit`    | page only  | the form that changes a record                           |
| `update`  | API only   | persist changes to a record                              |
| `destroy` | API only   | delete a record                                          |

Pages are things you **look at** (`index`, `show`, `create`, `edit`). API actions are things that **happen** (`index`, `show`, `store`, `update`, `destroy`). A `create` page submits to `store`, and an `edit` page submits to `update`.

**NEVER** use synonyms: `list`, `all`, `get`, `fetch`, `view`, `new`, `add`, `save`, `put`, `remove`, `delete` (as an action name), `detail`, `single`.

## Singular or plural

| Where                       | Form                                | Example                                       |
| --------------------------- | ----------------------------------- | --------------------------------------------- |
| API path                    | plural, kebab-case                  | `/projects`, `/email-blocks`                  |
| API route name              | plural                              | `projects.index`, `email-blocks.destroy`      |
| API handler / controller    | plural, in the language's case      | `Projects`, `EmailBlocks` (file `projects.<ext>`, `email-blocks.<ext>`) |
| Path parameter              | singular resource name, never `id`  | `{project}`, `[project]`, `:project`          |
| Page folder                 | **singular**, kebab-case            | `pages/project/`, `pages/email-block/`        |
| Page route constant         | singular, mirrors the page folder   | `routes.project.index()`                      |
| API client                  | plural, mirrors the API route name  | `api.projects.index()`                        |
| Permission                  | singular, dot-separated, snake_case | `project.store`, `email_block.destroy`        |
| Event                       | singular, dot-separated, snake_case | `project.created`, `email_block.deleted`      |
| Rate limit                  | kebab-case feature                  | `project:per-user`, `password-reset:per-email`|

The rule of thumb: the **API talks about collections** (plural), and **everything else talks about the resource** (singular).

## API routes and handlers

| Action    | Method     | Path                      | Success                    | Handler            | Route name         |
| --------- | ---------- | ------------------------- | -------------------------- | ------------------ | ------------------ |
| `index`   | `GET`      | `/projects`               | 200, one page of the list  | `Projects.index`   | `projects.index`   |
| `show`    | `GET`      | `/projects/{project}`     | 200, the record            | `Projects.show`    | `projects.show`    |
| `store`   | `POST`     | `/projects`               | 201, the created record    | `Projects.store`   | `projects.store`   |
| `update`  | `PATCH`    | `/projects/{project}`     | 200, the updated record    | `Projects.update`  | `projects.update`  |
| `destroy` | `DELETE`   | `/projects/{project}`     | 204, no body               | `Projects.destroy` | `projects.destroy` |

Status codes, error shapes and the list contract belong to `api-design`.

- **ALWAYS PATCH for `update`, NEVER PUT,** even when the body is the whole editable record. One method means "update" everywhere, so nobody has to ask which one this endpoint uses.
- **Paths are plural nouns nested under their parent,** with path IDs being public IDs: `/workspaces/{workspace}/projects/{project}`. Nest only as deep as ownership requires. Two levels is normal; three is a smell.
- **One handler per resource, one resource per file.** The file holds that resource's actions, their input types and their validation. Each action is a public method/function named exactly after the action; helpers stay private.
- **Nested resources get their own handler,** named after the nested resource: `/projects/{project}/tasks` → `Tasks.index` (or `ProjectTasks` if `Tasks` is ambiguous in that area), route name `projects.tasks.index`.
- **Register every route in one registry file** (see `project-structure`). Reading that file should tell you every endpoint and its guard.
- **File-based API routers** (Nuxt/Nitro, SvelteKit, Next.js, etc.) are fine. Map the HTTP method in the file to the action and keep the action name in the handler: `GET` in `projects/index` is `index`, `POST` there is `store`, and `GET`/`PATCH`/`DELETE` in `projects/[project]` are `show`/`update`/`destroy`. If the framework lets you name the function, name it after the action.

### Singletons

A resource with exactly one instance per parent has no ID in its path, but it still uses the same actions:

| Intent                         | Route                                   | Handler                  |
| ------------------------------ | --------------------------------------- | ------------------------ |
| Read my profile                | `GET /profile`                          | `Profile.show`           |
| Change my profile              | `PATCH /profile`                        | `Profile.update`         |
| Leave a workspace              | `DELETE /workspaces/{workspace}/membership` | `Membership.destroy` |
| Suspend a user (admin)         | `POST /admin/users/{user}/suspension`   | `UserSuspension.store`   |
| Lift the suspension            | `DELETE /admin/users/{user}/suspension` | `UserSuspension.destroy` |
| Send an email verification code | `POST /email-verification`             | `EmailVerification.store`|

### Named actions (non-CRUD)

Before inventing a verb, **ask whether the action is really a `store` or `destroy` of some other resource.** It usually is:

| Tempting verb                      | Usually really                                                  |
| ---------------------------------- | --------------------------------------------------------------- |
| `POST /projects/{p}/archive-it`    | `PATCH /projects/{p}` with `{"status":"archived"}`; or, if archiving has its own rules, permission and event, a singleton: `POST`/`DELETE /projects/{p}/archive` → `ProjectArchive.store`/`.destroy` |
| `POST /users/{u}/ban`              | `POST /users/{u}/suspension` → `UserSuspension.store`            |
| `POST /workspaces/{w}/leave`       | `DELETE /workspaces/{w}/membership` → `Membership.destroy`       |
| `POST /sessions/logout-all`        | `DELETE /users/{u}/sessions` → `UserSessions.destroy`            |

When it genuinely isn't one, use a **named verb sent as POST** on the record or collection, with the verb as the handler method: `POST /invitations/{invitation}/accept` → `Invitations.accept`, route name `invitations.accept`. Common legitimate verbs: `accept`, `decline`, `retry`, `check`, `verify`, `login`, `logout`, `register`.

**Authentication is the accepted exception.** Sign-in flows are actions on a session, not resources (`POST /auth/login` → `Sessions.login`, `POST /auth/register/verify` → `Registrations.verify`). Don't bend these into CRUD; remodelling them costs more than it gains.

## Pages

### Files

A page file's name is **always** one of these:

| File                                | Page action | URL                         | Default export / component |
| ----------------------------------- | ----------- | --------------------------- | -------------------------- |
| `pages/project/index.<ext>`         | `index`     | `/project`                  | `Index`                    |
| `pages/project/create.<ext>`        | `create`    | `/project/create`           | `Create`                   |
| `pages/project/[project].<ext>`     | `show`      | `/project/prj_8Xk…`         | `Show`                     |
| `pages/project/[project]/edit.<ext>`| `edit`      | `/project/prj_8Xk…/edit`    | `Edit`                     |
| `pages/…/layout.<ext>`              | (layout)    | wraps the folder's pages    | `Layout`                   |

**Anything else is a folder with an `index` inside.** A page that isn't a resource action gets its own folder:

```
pages/settings/index.<ext>            ✅  /settings
pages/settings.<ext>                  ❌  never a bare page file
pages/dashboard/index.<ext>           ✅
pages/register/verify/index.<ext>     ✅  /register/verify
pages/project/[project]/task/index.<ext>     ✅ nested resource index
pages/project/[project]/task/[task].<ext>    ✅ nested resource show
```

Why this matters: every file name tells you what the page is. `index` is always "the page at this folder's URL", `[x]` is always one record, `create`/`edit` are always forms, and there's no third kind of name to guess. All of a resource's pages sit together in one folder, so a check can enforce the rule with no exception list.

- **Folders are singular,** and named after the resource in kebab-case: `project`, `email-block`.
- **The param is named after the resource,** never `[id]`: `[project]`, `[task]`, `[token]`. The page reads `params.project`.
- **The extension is the framework's own** (`.tsx`, `.vue`, `.svelte`, …). The rule is about names, not file types.
- **Frameworks with mandatory file names** (e.g. SvelteKit `+page.svelte`, Next.js app router `page.tsx`): keep the same tree and let the action name become the folder: `project/create/+page.svelte`, `project/[project]/edit/+page.svelte`, `project/[project]/+page.svelte`. The framework's required file is fine; the action vocabulary is what must match.
- **Component / default export names follow the file:** `index` → `Index`, `create` → `Create`, `[x]` → `Show`, `edit` → `Edit`, `layout` → `Layout`. Never `ProjectList`, `ProjectDetailPage` or `NewProject`.
  - A `[token]` page that *does* something (reset a password, accept an invitation) still exports `Show`: the file name is the rule, and the folder and URL carry the meaning.
  - Tests may keep descriptive **local** import bindings (`import ProjectShow from ".../project/[project]"`) when they import several pages; the binding is local to the test, so only the module's own export name is checked.

### Delete is NEVER a page

Deleting is a **confirmation dialog** opened from the `index` row menu or the `show` page, and it calls `destroy`. There is no `pages/project/[project]/delete`, and no `/delete` URL. Likewise, `create` and `edit` are pages only when the form genuinely needs a page; a short form can be a dialog on the `index` or `show` page, calling `store`/`update` directly.

### Which page calls which API actions

| Page                              | Reads              | Writes                          |
| --------------------------------- | ------------------ | ------------------------------- |
| `project/index`                   | `projects.index`   | `projects.destroy` (row dialog) |
| `project/create`                  | –                  | `projects.store`                |
| `project/[project]`               | `projects.show`    | `projects.destroy` (dialog)     |
| `project/[project]/edit`          | `projects.show`    | `projects.update`               |
| `settings/index` (edits the tenant) | `workspaces.show` | `workspaces.update`             |

The last row shows that **URLs read like the product, not the convention.** Users think "settings", not "workspace edit", so the page lives in a plain `settings/` folder and calls the actions of the resource it actually edits. There's no `settings` API resource.

### Page route constants

Never hard-code a page URL in a link or redirect. Every page has a constant in one module, grouped by resource with the page actions:

```
routes.project.index()                 → /project
routes.project.create()                → /project/create
routes.project.show(project)           → /project/prj_8Xk…
routes.project.edit(project)           → /project/prj_8Xk…/edit
routes.project.task.index(project)     → /project/prj_8Xk…/task
```

Pages that aren't a resource get plain names: `routes.home()`, `routes.login()`, `routes.settings()`, `routes.dashboard()`, `routes.termsOfService()`.

When pages nest under a tenant (`pages/[workspace]/project/…`), the tenant's ID is the first argument: `routes.project.show(workspace, project)`, `routes.settings(workspace)`.

## API client

The frontend's API client mirrors the API's route names exactly, plus named verbs. One module per resource:

```
api.projects.index(query)          GET    /projects?…
api.projects.show(project)         GET    /projects/{project}
api.projects.store(input)          POST   /projects
api.projects.update(project, input) PATCH /projects/{project}
api.projects.destroy(project)      DELETE /projects/{project}
api.invitations.accept(invitation) POST   /invitations/{invitation}/accept
```

When the resource is nested under a tenant, the tenant is an explicit argument: `api.projects(workspace).index()`. Typed path builders (if you have them) use the same names: `paths.projects(workspace).store()`.

## Permissions

`<resource>.<action>`, where the action is the **API action of the route the permission guards**:

```
project.store            guards  POST   /workspaces/{w}/projects
project.update           guards  PATCH  /workspaces/{w}/projects/{p}
project.destroy          guards  DELETE /workspaces/{w}/projects/{p}
workspace.update         guards  PATCH  /workspaces/{w}
workspace.member.destroy guards  DELETE /workspaces/{w}/members/{m}
workspace.invitation.store
```

- The resource is singular and snake_case; a resource inside another adds a segment (`workspace.member`).
- In code, the constant is the same words in the language's style: `ProjectStore`, `WorkspaceMemberDestroy`, `PROJECT_STORE`.
- Every mutation route names its permission. Reads are normally guarded by membership/scoping rather than a permission; if a read genuinely needs one, use `.index` / `.show`.

## Events

`<resource>.<fact>`. The fact is the last segment, **past tense** ("what happened", not a command). Segments are snake_case, with **at most three segments**.

| API action | Event                                                             |
| ---------- | ----------------------------------------------------------------- |
| `store`    | `project.created`                                                 |
| `update`   | `project.updated`, or a more specific fact: `workspace.renamed`   |
| `destroy`  | `project.deleted`, or a more specific fact: `workspace.member.removed`, `workspace.member.left`, `workspace.invitation.revoked` |
| named verb | the verb's past tense: `workspace.invitation.accepted`            |

- **The resource is spelled as in its permission:** `workspace.member.update` guards the route that records `workspace.member.updated`. Filtering by resource is then a prefix match (`workspace.member.%`).
- **Account and session facts** live under `auth`: `auth.registered`, `auth.logged_in`, `auth.password_changed`, `auth.password_reset.requested`.
- **Admin actions** live under `admin`: `admin.user.suspended`, `admin.user.viewed`.
- **One event type per fact.** Put the variable details in the payload, never in the name: `rate_limit.reached` with `{ "limit": "auth:per-ip" }`, not `rate_limit.auth_per_ip_reached`.
- **Need a fourth segment?** That means the rule needs revisiting, not that you should add another dot.
- **Listener names are independent of event names** (`mail.password_reset` listens to `auth.password_reset.requested`). Renaming an event **never** renames its listeners: queued deliveries are keyed by listener name, so a rename would orphan them.

## Rate limits

`<feature>:<counted-by>`, where counted-by is one of `per-ip`, `per-account`, `per-email`, `per-user`, `per-key`, `per-workspace` (per tenant) or `global`:

```
auth:per-ip    login:per-email    register:per-email    register:global
project:per-user    password-reset:per-email    invitation:per-email
```

When a limit level is reached, the level is appended: `auth:per-ip:challenge`, `register:global:block`. The mechanics are covered in `production-hardening`.

## Other names

- **Files** in shared code (components, lib, types, tests) are kebab-case: `confirmation-dialog.<ext>`, `api-client.<ext>`. A component file can export a family of named parts (`fieldset` exports `Fieldset`, `Field`, `Label`).
- **Alternatives** of one component use a `variant` prop (`variant="danger"`). Never `tone`, `kind` or `type`, and never one component per alternative (`DangerButton`). A prop that carries data is named for the data (`level`, `status`).
- **Public IDs** are type-prefixed: `prj_`, `wsp_`, `usr_`, `req_` (see `api-design`).

## Choosing a name for something new

1. **Is it CRUD on an existing resource?** Use the action: `index`, `show`, `store`, `update`, `destroy`.
2. **Is it CRUD on a sub-resource or a singleton?** Model it that way: `UserSuspension.store`, `Membership.destroy`, `UserSessions.destroy`.
3. **Is it really a status change?** Prefer `update` with the field (`PATCH { "status": "archived" }`), unless the change has its own rules, permission and event. In that case it's a singleton.
4. **Otherwise, use a named verb** as POST: `Invitations.accept`. Its permission is `<resource>.<verb>` and its event is the verb in the past tense.
5. **Still unsure?** Propose the name and the reason, and ask. Don't guess, and don't loosen a naming check to fit.

## Common mistakes

| ❌ Wrong                                 | ✅ Right                                         |
| ---------------------------------------- | ------------------------------------------------ |
| `pages/settings.<ext>`                   | `pages/settings/index.<ext>`                     |
| `pages/projects/index.<ext>`             | `pages/project/index.<ext>` (folders are singular) |
| `pages/project/[id].<ext>`               | `pages/project/[project].<ext>`                  |
| `pages/project/new.<ext>`                | `pages/project/create.<ext>`                     |
| `pages/project/[project]/delete.<ext>`   | a confirmation dialog calling `projects.destroy` |
| `pages/project/details.<ext>`            | `pages/project/[project].<ext>`                  |
| export `ProjectListPage`                 | export `Index`                                   |
| `PUT /projects/{project}`                | `PATCH /projects/{project}`                      |
| `GET /project/{id}`                      | `GET /projects/{project}`                        |
| `POST /projects/create`, `POST /projects/{p}/delete` | `POST /projects`, `DELETE /projects/{p}` |
| `Projects.list`, `Projects.create`, `Projects.delete` | `Projects.index`, `Projects.store`, `Projects.destroy` |
| `api.getProjects()`, `api.createProject()` | `api.projects.index()`, `api.projects.store()` |
| permission `projects.create`, `can_edit_project` | `project.store`, `project.update`        |
| event `project.create`, `ProjectCreated`, `create_project` | `project.created`               |
| `href="/project/" + id`                  | `routes.project.show(id)`                        |

## Checklist

- [ ] Every API action is one of `index`, `show`, `store`, `update` or `destroy`, or a justified named verb sent as POST.
- [ ] `update` is PATCH; there's no PUT anywhere.
- [ ] API paths are plural, and params are named after the resource and hold public IDs.
- [ ] Every page file is `index`, `create`, `[param]`, `edit` or `layout`; anything else is a folder with `index`.
- [ ] Page folders are singular, and page exports are `Index` / `Create` / `Show` / `Edit` / `Layout`.
- [ ] No delete page exists; delete is a confirmation dialog calling `destroy`.
- [ ] Links and redirects use route constants, never string URLs.
- [ ] Client methods mirror the API route names.
- [ ] Every mutation's permission is `<resource>.<action>`, and its event is `<resource>.<past-tense fact>` with three segments or fewer.
- [ ] If a convention check exists, it passes without new exceptions (see `conventions-and-docs`).
