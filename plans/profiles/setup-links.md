# Setup links

> Status: Draft · Milestone: Alpha (M3) · Core · Updated: 2026-10-29

A project's customer usually doesn't have the project's dashboard, yet they're the one who must sign in their Claude or ChatGPT subscription, connect their GitHub, add their SSH key or point their agents' telemetry at their own backend. A **setup link** is a one-time link to a hosted **setup page** for one profile, branded as the project, where they do exactly that and nothing else. The project's members see the same page from the profile's **Setup** button in the dashboard.

## Today

Built in legacy (`lewbox-legacy/docs/profiles.md` § *Setup links*; `app/Http/Controllers/SetupLink/*`; `resources/js/pages/setup/{Link,Profile,Done,Ended}.svelte`; components `components/profile/{AgentList,GitSettings,SshKeyList,TelemetrySettings}.svelte`):

- Made by `POST /api/v1/profiles/{prf}/setup-links` (`return_url`, `sections` of `agents`, `git`, `ssh_keys`, `telemetry`) or the profile page's *Create setup link*; answers `url` and `expires_at`.
- `{app_url}/profile/{token}`: 32 random bytes, stored hashed (`profile_setup_links`, `psl_`), expiring after a week unused.
- **Used on the first click, not the first visit**: the page shows the tenant's name and *Continue*; only pressing it exchanges the token (so Outlook Safe Links and other scanners don't burn it). The exchange regenerates the Laravel session and binds it to the link for an hour.
- `EnterSetupLink` middleware admits the session's link only, and only its sections; everything else is `404`; an ended session goes to `/profile/ended`; *Done* ends the session and goes to `return_url` or `/profile/done`.
- Shows only the tenant's name, no logo or colours; on Lewbox's domain.
- Lewagent's Profile › Setup (`Profile/SetupController`) makes a link with `returnUrl` and `sections: [agents, git, ssh_keys, telemetry]` and redirects the user to it; `SetupReturnController` re-reads agents and Git afterwards.

## Goals

- Keep the flow: one-time link, used on click, session scoped to one profile and some sections, write-only values, return URL.
- Branded as the project ([projects/settings](../projects/settings.md#branding)).
- One implementation of each section, shared by the dashboard and the setup page, calling the same API handlers.
- A project can embed the page in its own product (iframe) if it chooses.
- A webhook when the customer finishes, so the project's backend doesn't have to poll.

### Non-goals

- Custom domains for the page (Later, with custom domains for the API).
- Letting the customer see sandboxes, keys, limits or credits.

## Design

### Data model

```sql
create table profile_setup_links (
  id               bigint generated always as identity primary key,
  public_id        text not null unique,          -- psl_…
  profile_id       bigint not null references profiles(id) on delete cascade,
  token_hash       bytea not null unique,         -- SHA-256 of lbl_…
  sections         text[],                        -- null = all
  return_url       text,
  embed_origin     text,                          -- the origin allowed to frame the page, if any
  created_by_user  bigint references users(id) on delete set null,
  created_by_key   bigint references api_keys(id) on delete set null,
  expires_at       timestamptz not null,          -- unused link: 7 days by default, 1h..30d
  used_at          timestamptz,
  session_ends_at  timestamptz,                   -- used_at + session length
  ended_at         timestamptz,                   -- Done, revoked, or expired
  created_at       timestamptz not null default now()
);

create table setup_sessions (
  id            bigint generated always as identity primary key,
  link_id       bigint not null references profile_setup_links(id) on delete cascade,
  token_hash    bytea not null unique,            -- the cookie
  expires_at    timestamptz not null,
  created_at    timestamptz not null default now()
);
```

Token: `lbl_` + 64 hex. Sections: `agents`, `git`, `ssh_keys`, `telemetry`.

### The flow

1. **Made** by a member or an admin key (`profiles.credentials`): `POST /v0/projects/{prj}/profiles/{prf}/setup-links { return_url?, sections?, expires_in?, embed_origin? }` → `{ id, url, expires_at }`. The URL is `{instance}/setup/{token}`, shown once.
2. **Opened**: `GET /setup/{token}` serves the dashboard app, which calls `GET /v0/setup-links/{token}` (public, rate-limited) for the project's branding and whether the link is usable. It shows the project's name and logo and **Continue**. A used, expired or unknown link shows "This link has been used or has expired. Ask {project} for a new one", naming the project only if the link was real.
3. **Used**: *Continue* posts `POST /v0/setup-links/{token}/sessions`. Under a row lock it checks the link is unused and unexpired, sets `used_at` and `session_ends_at`, creates a `setup_session`, and sets the cookie `__Host-lewbox_setup` (`Secure`, `HttpOnly`, `SameSite=Lax`, or `SameSite=None; Partitioned` when embedded). The link can never be used again.
4. **The page**: the same React components the dashboard uses for the profile's setup, for the link's sections, calling the API with the setup cookie.
5. **Done**: `DELETE /v0/setup-sessions/current` ends it (`ended_at`), and the page goes to `return_url` with `?setup=done&profile={prf_}` appended, or shows "All set". The webhook `profile.setup_completed` fires with what changed during the session.
6. The session ends after 1 hour (configurable, up to 4) or on Done. An agent sign-in still under way when the session ends carries on and completes; the customer just can't see it.

### Who may do what on the page

The setup session is a **principal** like a key or a user session ([api/authentication](../api/authentication.md)): kind `setup_session`, project, profile, sections. It may call only these management routes, for its own profile, and only for its sections; anything else is `404`:

| Section | Routes (under `/v0/projects/{prj}/profiles/{prf}`) |
|---|---|
| always | `GET` (name and project branding only, a reduced view) |
| `agents` | `GET /agents`; `PUT`, `DELETE /agents/{agent}/credential`; `POST /agents/{agent}/sign-ins`; `GET`, `PUT` (code), `DELETE /sign-ins/{asi}` |
| `git` | `GET`, `PUT` (identity), `DELETE /git` (the connections, on every provider); `POST /git/install-links`, `PATCH`, `DELETE /git/people/{login}`, `PUT`, `DELETE /git/accounts/{account}`, the GitHub App's callback routes, `GET /git/repositories` ([git/github-app](../git/github-app.md)) |
| `ssh_keys` | `GET`, `POST /ssh-keys`; `DELETE /ssh-keys/{ssh}` |
| `telemetry` | `GET`, `PUT`, `DELETE /telemetry`; `GET /telemetry/providers` |

So the setup page and the dashboard run the **same handlers**; the principal decides. That removes legacy's parallel `SetupLink\*` controllers.

**Sections come from plugins.** Each section is a setup-page slot ([plugins/dashboard-slots](../plugins/dashboard-slots.md)) filled by the plugin that owns it: `agents` by the agent provider plugins (one card per enabled agent), `git` by the Git provider plugin (`git/github`), `telemetry` by the `telemetry` plugin; `ssh_keys` is core. A section whose plugin is off for the project isn't offered: asking for it in `sections` is `422 plugin_disabled`, and a link made before the plugin was turned off simply leaves it out. The route allow-list above is registered by each plugin with its routes.

The dashboard's Setup button opens the page in the member's own session (no link), with every section, a header saying "Setting up {profile} as {member}", and Back to the profile instead of Done.

### Branding

From [project branding](../projects/settings.md#branding): display name, logo, accent colour, support link, "Powered by Lewbox" unless hidden. The page title is "{display name} setup". No Lewbox chrome, no navigation, no link to the dashboard.

### Embedding

By default the page can't be framed (`Content-Security-Policy: frame-ancestors 'none'`). A link made with `embed_origin: "https://app.acme.dev"` is served with `frame-ancestors https://app.acme.dev`, and its cookie as `SameSite=None; Partitioned` so it works as a third-party cookie under CHIPS. Browsers that block it anyway get a page that `postMessage`s `{ type: "lewbox:setup-unavailable" }` to the parent, as private ports do, so the project can fall back to a new tab. The page also posts `lewbox:setup-done` on Done when framed.

GitHub's installation and OAuth pages can't be framed; the Git section opens them in a popup and finishes on the callback, which `postMessage`s back to the page and closes ([git/github-app](../git/github-app.md)).

### Values stay write-only

Every section shows that something is set and how (a key's last four characters, a subscription's account email, a Git account's login, a telemetry provider and endpoint, key fingerprints), never a secret. Changes are attributed to the setup link in the audit log (actor `setup_link`, the link's `psl_` id and whoever made it), and the activity log of any sandbox an `apply_setup` touches.

### Revoking

`DELETE /v0/projects/{prj}/profiles/{prf}/setup-links/{psl}` ends an unused link or a live session at once. `GET …/setup-links` lists them (`id`, `sections`, `created_by`, `expires_at`, `used_at`, `ended_at`), so a project can see whether its customer opened it.

## API

| Method | Path | Who | What |
|---|---|---|---|
| `POST` | `/v0/projects/{prj}/profiles/{prf}/setup-links` | owner, admin, member; admin key `profiles.credentials` | Make one |
| `GET` | `/v0/projects/{prj}/profiles/{prf}/setup-links` | same | List |
| `DELETE` | `/v0/projects/{prj}/profiles/{prf}/setup-links/{psl}` | same | Revoke |
| `GET` | `/v0/setup-links/{token}` | anyone | `{ project: { name, branding }, usable, sections }`; `404` if unknown |
| `POST` | `/v0/setup-links/{token}/sessions` | anyone | Use it; sets the cookie; `409 setup_link_used` / `410 setup_link_expired` |
| `GET` | `/v0/setup-sessions/current` | setup cookie | `{ profile, project, sections, expires_at, return_url }` |
| `DELETE` | `/v0/setup-sessions/current` | setup cookie | Done |

### Errors

| Status | Code | When |
|---|---|---|
| `404` | `not_found` | Unknown link; a route outside the session's sections or profile |
| `409` | `setup_link_used` | Using a used link |
| `410` | `setup_link_expired` | Using an expired one |
| `401` | `setup_session_ended` | The session expired or was ended or revoked |
| `422` | `validation_failed` | `return_url` not http(s); `embed_origin` not an origin; unknown section |
| `429` | `rate_limited` | 10 uses a minute per IP; 60 link lookups |

## Security

- The token is single-use and used only by an explicit click; the session is short and scoped.
- `return_url` is the project's choice and goes in the response, not a server-side redirect, so it can't be used as an open redirect on Lewbox's domain without the project having made the link.
- CSRF: the setup cookie follows the same rules as user sessions (custom header plus origin check, [projects/users-and-login](../projects/users-and-login.md#the-dashboard-and-csrf)); embedded pages check the `embed_origin` too.
- Telemetry endpoints are never called by the server (no SSRF from a customer's input); see [telemetry](telemetry.md#what-reaches-the-sandbox).
- The page shows only what the link's sections allow; a guessed `prf_` id is `404`.

## Observability

- Audit: `setup_link.created`, `setup_link.used`, `setup_link.revoked`, `setup_link.completed`, and each change made in the session.
- Webhooks: `profile.setup_completed` (with `sections_changed`), and the usual `profile.setup_changed` for each change.
- Metrics: `lewbox_setup_links_created_total`, `lewbox_setup_links_used_total`, `lewbox_setup_sessions_completed_total`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SETUP_LINK_TTL` | `168h` | Unused link |
| `LEWBOX_SETUP_LINK_SESSION_TTL` | `1h` | After use; a link may ask for up to 4h |

## Testing

- Link lifecycle: open (not used), Continue (used), reopen (used), expiry, revoke mid-session.
- Principal scoping: every management route with a setup session for each section set; other profiles and projects `404`.
- Embedding: CSP header per link; cookie attributes; postMessage fallbacks (Playwright in an iframe).
- Mail-scanner simulation: `GET` the link N times, then Continue still works.

## Build plan

In `internal/profiles/setuplinks`; the principal in `internal/auth`; pages in `web/src/routes/setup`, with each plugin's section from its `web/` folder.

1. Table, create, list, revoke.
2. Public lookup and use; setup cookie and principal.
3. Route allow-list per section in the auth middleware.
4. React setup page reusing the dashboard's profile-setup sections; branding.
5. Embedding headers and messages.
6. Completion webhook.

## Open questions

1. **Admin-API sign-ins instead of links.** With [Admin API sign-ins](agent-sign-ins.md#from-the-management-api), a project can build its own setup UI. Do we still need embedding, or is "build your own or open ours in a tab" enough?
2. **Re-usable links.** Some projects want a permanent "manage your agents" link per customer. Allow `reusable: true` links that open a fresh session each click, until revoked?
3. **Session length.** Is an hour enough for a customer who wanders off mid sign-in?
