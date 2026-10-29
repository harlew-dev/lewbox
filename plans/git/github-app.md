# The project's GitHub App and profiles' connections

> Status: Draft · Milestone: Alpha (M3) · Plugin: `git/github` · Updated: 2026-10-29

Each project has **its own GitHub App**, and it's the only way a profile connects GitHub. A profile holds **any number of GitHub people and accounts**, up to a limit (25 by default): each person authorizes the App, and installs it on the accounts they work on (their personal account and organizations) or links it where it's already installed. Lewbox keeps who each person is, their user token, and the installations the profile uses. Everything else in [git](../git.md) rests on it: tokens in sandboxes, tokens issued to the project's servers, clones, and forwarded events.

This plan covers registering the App, connecting people and accounts to a profile, the limit, user tokens, listing repositories and branches, and the commit identity. Tokens are in [tokens](tokens.md); the App's inbound webhooks in [activity-and-webhooks/github-webhooks](../activity-and-webhooks/github-webhooks.md).

---

## Today

Legacy (`lewbox-legacy/docs/profiles.md` § *Git integration*, *Register the tenant's GitHub App*, *Connect a profile with the App*, *Several installations per connection*):

- **Registration** (`app/Actions/GitHub/BuildGitHubAppManifest.php`, `RegisterGitHubApp.php`): Settings > Profiles > *Register on GitHub* posts a manifest from the tenant's browser to GitHub, under the organization the tenant names or its own account, with a random `state` in the session. The manifest has the tenant's name, its homepage, `redirect_url` (back to the dashboard), `callback_urls` (the setup page's installation callback), `hook_attributes.url` (`/api/github/v1/apps/{tenant}/webhooks`), `request_oauth_on_install: true`, `public: true`, the permissions in `lewbox.git.github_app.permissions` (Administration, Contents, Issues, Pull requests, Workflows write; Checks, Statuses, Metadata read) and `default_events: []`. The code is exchanged at `POST /app-manifests/{code}/conversions` for the id, slug, private key, client secret and webhook secret; the bot's user id is looked up. Stored as the tenant's `GitHubApp` credential in the polymorphic `credentials` table.
- **Git mode** (`tenants.git_mode`, `App\Enums\Tenant\GitMode`): `token` (default) or `github_app`, deciding what every profile's setup page offers.
- **Pasted tokens**: the setup page links to GitHub's new-token form, pre-filled (`CredentialProvider::createUrl()`), checks the token with `GET /user` and `GET /user/emails`, stores `{token}` and `hint`, `login`, `name`, `email`. Such a token can't be narrowed: whatever it reaches, the sandbox reaches, and it can push.
- **Connecting with the App** (`SetupLink\Git\InstallationController`, `ConnectGitHubInstallation.php`): *Connect GitHub* → `github.com/apps/<slug>/installations/new` with a state; *Already installed?* → OAuth authorize. The callback exchanges the code for the person's user token, reads who they are, and lists `GET /user/installations`; the `installation_id` GitHub sent must be one of them. Every installation they reach is connected.
- **Several installations** (2026-10-29, `522ae4d`): `profile_github_installations` (`installation_id`, `account`, `account_type`, `repository_selection`, `error`), unique per connection on installation and on account, so `harrison` and `harlew-dev` both work. Disconnect one account, or the whole connection. Installation rows are per connection, so the same organization's installation is stored once per profile using it.
- **Repositories** (`ListGitHubRepositories`): `GET /installation/repositories` per installation, merged, sorted, kept a minute. Branches likewise.
- **Owners and creation** (`ListGitHubRepositoryOwners.php`, `CreateGitHubRepository.php`): the accounts to create a repository under, and creation as the person with their user token.
- **Removing the App** (`RemoveGitHubApp.php`) disconnects every profile using it, removes tokens from running sandboxes, and sets the mode back to tokens.
- **Not built:** the Admin API's install link, the user token's refresh (GitHub user tokens expire after 8 hours with a refresh token valid 6 months when *expiring user tokens* is on).

## Goals

- **GitHub App only.** One kind of connection, so every token is narrowed and short-lived, and the setup page has one button.
- A project registers its App in two clicks, or brings one it already has, and its customers never see Lewbox.
- A profile holds **several GitHub people and accounts** (a person and a machine user; several people on one team's profile; personal accounts and organizations), up to `LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` (25), and adds or removes them one at a time.
- Each person connects every GitHub account they work on in one go, and adds more later.
- A project's backend can connect a profile without a setup page: an install link with its own `return_url`.
- Each person's user token is kept and refreshed, so the [token API](tokens.md#the-token-api) can hand it to the project's own servers (admin keys and the dashboard only) for what must be done as that person.

### Non-goals

- Pasted personal access tokens, fine-grained or classic.
- **Organizations that forbid third-party Apps.** Their members can't connect. A **known limitation, accepted for the alpha** (decided 2026-10-29), to revisit when a project needs it. Today's ways round it: an organization owner approves the App, or the project brings an App the customer owns ([registering](#registering-the-app)).
- Repository creation and the repository owners list. A project creates repositories with the person's user token ([integrating](integrating.md#creating-a-repository)).
- More than one App per project.
- GitHub Enterprise Server (an open question).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | The manifest flow and its permissions; `request_oauth_on_install`; `public: true`; the check that a redirected installation id is one the person's user token reaches; several installations per connection; listing repositories and branches, cached a minute |
| **Change** | The App gets its own table (`github_apps`). **People and installations are the project's** (`github_people`, `github_installations`), linked to profiles, so a person or an organization's installation is one row however many profiles use them. **A profile holds several people and accounts**, up to a limit (legacy: one person). The webhook URL becomes `/hooks/github/{prj}`. The manifest subscribes to every event its permissions allow, for [forwarding](../activity-and-webhooks/github-webhooks.md#forwarding). The user token is **kept, encrypted, and refreshed**. Install links exist for the API. A project can bring an App it registered by hand |
| **Drop** | Pasted tokens; `tenants.git_mode` and the setup page's token box; `ListGitHubRepositoryOwners`, `CreateGitHubRepository` and their endpoints; the polymorphic `credentials` row for the App; installation rows per connection; one person per profile |

## Design

### People and accounts

The words GitHub uses, and how they map:

| GitHub | Lewbox | How many |
|---|---|---|
| A **GitHub App** | The project's App (`github_apps`) | One per project |
| An **installation** of it on an account (personal or organization), choosing all or selected repositories | `github_installations`: an **account** | One per account the App is installed on, known to the project |
| A **person** who authorizes the App, getting a user token | `github_people` | One per GitHub user who has connected anywhere in the project |
| The installations a person can reach (`GET /user/installations`) | `github_person_installations` | Refreshed whenever the person connects, and on installation webhooks |
| What a profile uses | `profile_github_people`, `profile_github_accounts` | **Any number of each, together at most the limit** (25 by default) |

So a profile might hold two people, `harrisonclewis` and the machine user `harlew-bot`, and four accounts: `harrisonclewis` (personal), `harlew-dev`, `acme` and `harlew-bot`. Its sandboxes get tokens for each account's repositories; the token API issues installation tokens on any of them, and user tokens for either person.

**Why several people per profile.** A profile is whoever a project's sandboxes belong to: often one person (Lewagent makes one per user), sometimes a team, a person and a machine user they push with, or a customer with accounts on two GitHub identities. Each person still acts only as themselves: a user token is always for a named person ([user tokens](tokens.md#user-tokens)), so "as you" is never ambiguous.

**Why people and installations are the project's.** An organization installs an App once, and a person authorizes it once. When two profiles use the same organization, or the same person connects two profiles (staging and production, say), they share the rows: when an installation is suspended or a person revokes the App, one webhook changes one row and every profile sees it at once, and a person's single refresh token is never refreshed twice from two copies (GitHub invalidates a refresh token when it's used).

**Why a profile links them, and only what its people reach.** What a profile may use is decided by the people who connected it: an account is linked only when one of the profile's people reaches its installation (`github_person_installations`), checked when they connect and when it's linked by hand. Unlinking a person also unlinks the accounts only they reached for this profile.

### The limit

`LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` (instance configuration, default **25**): the most people and accounts **together** a profile may hold, so two people and four accounts count six.

- It's an instance setting, not a project one, because it guards the instance: every account a profile holds is another installation token to mint per sandbox and another `GET /installation/repositories` per repository listing. An instance that serves big organizations raises it. A per-project override isn't needed for the alpha (Later, if a project asks).
- **On connect**, when a person brings more accounts than fit: the person is linked first, then the installation GitHub redirected with, then the rest by account name until the limit. Those that don't fit are listed on the connection as `available_accounts` (reachable but not linked), and the setup page says so; unlink another account to make room, then link one (`PUT …/git/accounts/{account}`).
- A person or account that doesn't fit at all is refused with `429 limit_exceeded`, carrying `limit: {kind: "git_connections", value: 25}` ([api/errors](../api/errors.md)), as other profile limits are; on the browser callback, the redirect carries `status=failed&code=limit_exceeded`.
- Lowering the limit doesn't unlink anything; profiles over it can't link more until they're under it.

### Tables

`github_apps`: one per project at most.

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | Public id `gha_…` |
| `project_id` | fk, unique | |
| `app_id` | bigint | GitHub's |
| `slug` | text | `https://github.com/apps/<slug>` |
| `name`, `owner_login`, `owner_type`, `html_url` | text | As GitHub reported them |
| `client_id` | text | |
| `client_secret` | bytea | encrypted |
| `private_key` | bytea | encrypted PEM |
| `webhook_secret` | bytea | encrypted |
| `bot_user_id` | bigint | For the bot identity `<bot_user_id>+<slug>[bot]@users.noreply.github.com` |
| `permissions` | jsonb | What it has, read from `GET /app` on registration, daily, and on demand |
| `events` | jsonb | What it's subscribed to, likewise |
| `user_tokens_expire` | bool | Whether GitHub issues refresh tokens, from the first OAuth exchange |
| `webhook_verified_at` | timestamptz | Last `ping` or signed delivery |
| `source` | text | `manifest` or `brought` |
| `created_at`, `updated_at` | timestamptz | |

`github_installations`: the App's installations the project knows of (accounts).

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | |
| `project_id` | fk | |
| `installation_id` | bigint | GitHub's. Unique per project |
| `account`, `account_type` | text | `harlew-dev`, `Organization` or `User`; matched case-insensitively |
| `account_id` | bigint | Survives renames |
| `repository_selection` | text | `all` or `selected` |
| `permissions` | jsonb | What the account accepted (may lag the App's until `new_permissions_accepted`) |
| `suspended_at` | timestamptz | From `installation.suspend` |
| `error_code`, `error` | text | GitHub refusing it last, e.g. `not_installed` |
| `created_at`, `updated_at`, `deleted_at` | timestamptz | Kept a while after `installation.deleted`, for the activity log's sake |

`github_people`: GitHub users who have authorized the project's App.

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | |
| `project_id` | fk | |
| `github_user_id` | bigint | Unique per project |
| `login`, `name`, `email` | text | From `GET /user`, `GET /user/emails` (verified primary, else the `noreply` address); `login` updated on every refresh, as people rename |
| `user_token` | bytea | encrypted |
| `refresh_token` | bytea | encrypted, null without one |
| `user_token_expires_at`, `refresh_token_expires_at` | timestamptz | |
| `user_authorized` | bool | Whether the user token works (false after a revocation or a failed refresh) |
| `verified_at` | timestamptz | Last time GitHub accepted it |
| `error_code`, `error` | text | Why GitHub refused it last |
| `created_at`, `updated_at` | | |

`github_person_installations`: (`github_person_id` fk cascade, `github_installation_id` fk cascade, `seen_at`), primary key on both: which installations a person reaches, from `GET /user/installations`.

`profile_github_people`:

| Column | Type | Notes |
|---|---|---|
| `profile_id` | fk, cascade | |
| `github_person_id` | fk, cascade | |
| `is_default` | bool | Exactly one per profile with people (partial unique index): the person whose identity and own account are used when nothing names one |
| `linked_at`, `linked_by` | timestamptz, actor | A setup link, a user, an admin key's install link |

`profile_github_accounts`:

| Column | Type | Notes |
|---|---|---|
| `profile_id` | fk, cascade | |
| `github_installation_id` | fk, cascade | |
| `via_person_id` | fk | A linked person who reaches it; re-pointed to another when that person is unlinked, or the account is unlinked when none reaches it |
| `linked_at`, `linked_by` | timestamptz, actor | |

Primary keys on both columns of each. The limit is checked in the linking transaction, under a row lock on the profile, counting both tables.

### Registering the App

1. Project settings › GitHub › **Register on GitHub** asks for an organization (optional) and posts the manifest from the browser to `https://github.com/settings/apps/new` (or `/organizations/<org>/settings/apps/new`), with a random `state` stored server-side for 10 minutes against the user's session.

   Manifest:

   ```json
   {
     "name": "<project name>",
     "url": "<project homepage or the instance URL>",
     "hook_attributes": { "url": "https://<instance>/hooks/github/<prj>", "active": true },
     "redirect_url": "https://<instance>/github/apps/<prj>/created",
     "callback_urls": ["https://<instance>/github/apps/<prj>/authorized"],
     "request_oauth_on_install": true,
     "setup_on_update": true,
     "public": true,
     "default_permissions": {
       "administration": "write", "checks": "read", "contents": "write", "issues": "write",
       "metadata": "read", "pull_requests": "write", "statuses": "read", "workflows": "write"
     },
     "default_events": [
       "check_run", "check_suite", "create", "delete", "gollum", "issue_comment", "issues",
       "pull_request", "pull_request_review", "pull_request_review_comment",
       "pull_request_review_thread", "push", "status"
     ]
   }
   ```

   The permissions are what the project's servers can then ask the [token API](tokens.md#the-token-api) for: `contents: write` and `workflows: write` to push, `pull_requests: write` and `issues: write` for pull requests and comments, `administration: write` so a user token can create repositories in organizations. Sandbox tokens use a read-only subset unless the project raises its cap. The events are every one these permissions allow that a consumer has asked for; installation events (`installation`, `installation_repositories`, `github_app_authorization`) are always sent to Apps and need no subscription. GitHub refuses account permissions in a manifest ("Default permission records resource is not included in the list"), so there are none, as legacy found.

2. GitHub redirects back with `code` and `state`. The state must match. `POST /app-manifests/{code}/conversions` gives the id, slug, PEM, client secret and webhook secret; `GET /users/<slug>[bot]` gives the bot's user id; `GET /app` (as the App) the permissions and events. Stored in `github_apps`.

**Bringing an App registered by hand** (e.g. one the project shares with its other products): `PUT /v0/projects/{prj}/settings/git/github` with `app_id`, `client_id`, `client_secret`, `private_key`, `webhook_secret`. Lewbox checks it with `GET /app` (a JWT from the key, so a wrong key is refused with `422 validation_failed` on `private_key`), reads its permissions and events, and answers with the webhook URL and callback URL the project must set on GitHub. Until a signed `ping` arrives, the App page warns that webhooks aren't reaching Lewbox.

**Permissions and events the App lacks**: GitHub's manifest can't be changed after registration, and Apps registered before Lewbox asked for something, or brought, may lack it. The App object reports `missing_permissions` and `missing_events` against what Lewbox would register today, and the dashboard links to the App's settings on GitHub. Nothing breaks: a token asking for a permission the App lacks is refused ([tokens](tokens.md#errors)), and an event it isn't sent can't be forwarded.

**Removing it**: `DELETE /v0/projects/{prj}/settings/git/github` deletes every person, installation and profile link in the project (tokens are removed from running sandboxes by [tokens](tokens.md#revoking)), then the row. The project deletes the App itself on GitHub; the dashboard links to its settings.

### Connecting a profile

A profile gains people and accounts one connect at a time. Each connect is one person going through GitHub:

```
person ─ Connect GitHub ─▶ github.com/apps/<slug>/installations/new?state=…
                         ◀── /github/apps/{prj}/authorized?installation_id&setup_action&code&state
Lewbox: code → user token (POST /login/oauth/access_token, client id + secret)
        GET /user, /user/emails      → identity
        GET /user/installations      → every installation of this App the person reaches
        installation_id ∈ that list?  else refuse
        upsert github_people (by github_user_id); replace github_person_installations
        upsert github_installations
        lock the profile; link the person (the default if it's the profile's first); link accounts
          up to the limit: the redirected installation first, then the rest by name; the others
          become available_accounts
        apply_setup on the profile's running sandboxes
        back to the setup page (or the install link's return_url)
```

- **Add a person**: the setup page's *Add a GitHub account* (and install links) run the same flow for whoever is signed in to GitHub in that browser. A person already linked to the profile is a no-op apart from refreshing what they reach and linking new accounts. The setup page shows which GitHub user the browser is signed in as, so a person can sign out and connect a machine user.
- **Already installed?** sends them to `https://github.com/login/oauth/authorize?client_id=…&state=…`, since GitHub sends no one back from reconfiguring an installation they already have. The callback is the same, without `installation_id`; every installation they reach is linked, up to the limit. This is how a second person in an organization connects: the organization's installation already exists, and they link it.
- **`setup_action=request`**: an organization owner must approve. The page says so. When the owner approves, GitHub's `installation.created` webhook (with `requester`) links the installation to every profile that has the requester as one of its people, up to each profile's limit ([github-webhooks](../activity-and-webhooks/github-webhooks.md#handling)).
- **Install on another account** sends them to install again; the new installation is linked.
- **Link an available account** (`PUT …/git/accounts/{account}`): one of the profile's people must reach it; within the limit.
- **Unlink an account** (`DELETE …/git/accounts/{account}`): the installation stays the project's while other profiles use it.
- **Unlink a person** (`DELETE …/git/people/{login}`): accounts linked through them are re-pointed to another of the profile's people who reaches them, or unlinked when none does. If they were the default, the longest-linked remaining person becomes it. A person no profile links any more is deleted, with their tokens, and Lewbox revokes their grant (`DELETE /applications/{client_id}/grant`) so the App no longer acts for them.
- **Disconnect GitHub** (`DELETE …/git`): unlinks every person and account from the profile.
- **Change the default person**: `PATCH …/git/people/{login}` `{default: true}`.
- Every change re-applies setup on the profile's running sandboxes. The App stays installed on GitHub; the page links to each installation's settings to uninstall it there.
- The state is stored server-side with the profile (and the setup link or install link it came from) for 10 minutes, single use.

### Install links

So a project's own product can connect its customers' GitHub without a Lewbox setup page:

`POST /v0/projects/{prj}/profiles/{prf}/git/install-links` `{return_url, mode: "install" | "authorize"}` answers `{url, expires_at}`: a one-time Lewbox URL, valid 10 minutes, that redirects to GitHub with a state bound to the profile and `return_url`. After the callback, Lewbox redirects to `return_url?status=connected|requested|failed&code=…`. `return_url` must be `https` (or `http://localhost` in development) and on one of the project's allowed return origins ([projects/settings](../projects/settings.md)), so a link can't send a person somewhere else.

### The user token

Each person's user token is what lets the project's servers act **as that person** on GitHub: open pull requests "as you, not a bot", create repositories in their accounts, list the organizations they belong to. Lewbox keeps it so the [token API](tokens.md#user-tokens) can issue it, to admin keys and dashboard sessions only, for a named person.

- Kept encrypted on the person (`github_people`), with its refresh token when the App has *expiring user tokens* (GitHub's default for new Apps: 8 hours, refresh 6 months). One copy per person per project, however many profiles link them.
- Refreshed when it has less than 15 minutes left and someone asks for it (`POST /login/oauth/access_token` with `grant_type=refresh_token`), in a transaction holding a row lock on the person: GitHub invalidates the old refresh token at once, so two refreshes racing must not both run.
- A refresh job also runs once a week for people whose refresh token would otherwise lapse unused (6 months), so a person who connected and went quiet stays connected.
- When it can't be refreshed (revoked, or the refresh token expired), `user_authorized` becomes false. Installation tokens keep working for the accounts linked through them; user tokens for that person are refused with `409 git_person_not_authorized`, saying to authorize again (an install link with `mode: authorize`, or the setup page).
- `github_app_authorization.revoked` clears it at once ([github-webhooks](../activity-and-webhooks/github-webhooks.md#handling)).
- **User tokens are never written into sandboxes.** Sandboxes get installation tokens only ([tokens](tokens.md#whats-written)); `hosts.yml` names the default person's login as `user`, with an installation token.

### Repositories and branches

`GET /v0/profile/git/repositories` (and `/v0/projects/{prj}/profiles/{prf}/git/repositories`): for the repository pickers in the dashboard and in projects' products, and for choosing what a sandbox clones.

- `GET /installation/repositories` for each of the profile's linked accounts (an installation token minted for listing, `metadata: read`, kept in memory until 10 minutes before it expires), fetched concurrently (at most 8 at once), merged, de-duplicated and sorted by `full_name`. With the limit at 25, that's at most 25 listings per refresh of the cache.
- Filters: `search` (substring of `owner/name`), `visibility` (`public`, `private`), `account`. Page-numbered over the merged list (`page`, `per_page` ≤ 100), capped at `LEWBOX_GITHUB_LISTED_REPOSITORIES_MAX` (1000), `meta.truncated` when capped.
- Each repository: `{name: "owner/repo", owner, private, default_branch, description, pushed_at, html_url, installation: {id, account}}`.
- Cached per profile for `LEWBOX_GITHUB_LISTED_REPOSITORIES_CACHE` (60 s), in memory per replica; the cache key includes the linked installation ids and is dropped on `installation_repositories` webhooks.

`GET …/git/repositories/{owner}/{repo}`: one repository, `404 repository_not_found` when no linked installation reaches it.

`GET …/git/repositories/{owner}/{repo}/branches`: `GET /repos/{o}/{r}/branches` through the owner's installation, page-numbered, the default branch first, each `{name, commit: {sha}, protected}`; `search` filters by prefix.

A public repository can always be named on create even when no installation reaches it ([sandboxes/repositories](../sandboxes/repositories.md)).

### Commit identity

What `git config user.name` and `user.email` are set to in the profile's sandboxes, the first of:

1. The profile's `git_name` and `git_email` ([profiles](../profiles.md)).
2. The profile's default person (`github_people.name`, `.email`, through `profile_github_people.is_default`).
3. The App's bot, when the profile's identity is set to `bot`: `<slug>[bot]` and `<bot_user_id>+<slug>[bot]@users.noreply.github.com`.
4. None: git refuses to commit until one's set, as anywhere else.

## API

### The project's App

| Method and path | Does | Answers |
|---|---|---|
| `GET /v0/projects/{prj}/settings/git/github` | The App, without secrets: `{id, app_id, slug, name, owner, html_url, source, permissions, events, missing_permissions, missing_events, webhook_url, callback_url, webhook_verified_at, created_at}` | `200`, `404` |
| `PUT /v0/projects/{prj}/settings/git/github` | Bring an App registered by hand | `200` |
| `POST /v0/projects/{prj}/settings/git/github/manifests` | Start the manifest flow: answers `{post_url, manifest, state}` for the browser to post | `200` |
| `POST /v0/projects/{prj}/settings/git/github/conversions` | Finish it: `{code, state}` | `201` |
| `DELETE /v0/projects/{prj}/settings/git/github` | Remove it, disconnecting every profile | `204` |
| `GET /v0/projects/{prj}/settings/git/github/installations` | Every installation the project knows, with the number of profiles linking each | `200` |

### A profile's connection

Under `/v0/projects/{prj}/profiles/{prf}/git` (admin key or session) and `/v0/profile/git` (the profile's own key; reads, and installation tokens only).

| Method and path | Does | Answers |
|---|---|---|
| `GET …/git` | The profile's people, accounts and available accounts | `200` (empty lists when nothing's connected) |
| `DELETE …/git` | Unlink every person and account (management only) | `204` |
| `POST …/git/install-links` | `{return_url, mode}` → `{url, expires_at}` (management only) | `201` |
| `PATCH …/git/people/{login}` | `{default: true}` (management only) | `200` |
| `DELETE …/git/people/{login}` | Unlink a person (management only) | `204` |
| `PUT …/git/accounts/{account}` | Link an available account (management only; within the limit) | `200`, `429 limit_exceeded` |
| `DELETE …/git/accounts/{account}` | Unlink an account (management only) | `204` |
| `GET …/git/repositories`, `…/{owner}/{repo}`, `…/{owner}/{repo}/branches` | Repositories and branches, across every linked account | `200` |
| `POST …/git/tokens` | Issue a token ([tokens](tokens.md#the-token-api)). On `/v0/profile/git/tokens`, installation tokens only | `201` |

The connection object:

```json
{
  "limit": { "value": 25, "used": 5 },
  "people": [
    { "login": "harrisonclewis", "name": "Harrison Lewis", "email": "harrison@example.com",
      "default": true, "user_authorized": true, "verified_at": "2026-10-29T12:00:00Z", "error": null },
    { "login": "harlew-bot", "name": "Harlew Bot", "email": "bot@harlew.com",
      "default": false, "user_authorized": true, "verified_at": "2026-10-29T12:05:00Z", "error": null }
  ],
  "accounts": [
    { "installation_id": 51234567, "account": "harrisonclewis", "account_type": "User",
      "repository_selection": "all", "via": "harrisonclewis", "suspended": false, "error": null,
      "settings_url": "https://github.com/settings/installations/51234567" },
    { "installation_id": 51234568, "account": "harlew-dev", "account_type": "Organization",
      "repository_selection": "selected", "via": "harrisonclewis", "suspended": false, "error": null,
      "settings_url": "https://github.com/organizations/harlew-dev/settings/installations/51234568" },
    { "installation_id": 51234570, "account": "harlew-bot", "account_type": "User",
      "repository_selection": "all", "via": "harlew-bot", "suspended": false, "error": null,
      "settings_url": "https://github.com/settings/installations/51234570" }
  ],
  "available_accounts": [
    { "installation_id": 51234571, "account": "acme", "account_type": "Organization", "via": "harrisonclewis" }
  ],
  "identity": { "name": "Harrison Lewis", "email": "harrison@example.com", "source": "person" }
}
```

`identity.source` is `profile`, `person` (the default person), `bot` or `none` ([commit identity](#commit-identity)). People's tokens are never in it.

### Callbacks

| Path | Who |
|---|---|
| `GET /github/apps/{prj}/authorized` | GitHub, after an install or authorize. Not an API endpoint: it redirects to the setup page or the install link's `return_url` |
| `GET /github/apps/{prj}/created` | GitHub, after the manifest. Handled by the dashboard, which calls `…/github-app/conversions` |

### Errors

| Status | Code | When |
|---|---|---|
| 404 | `not_found` | No App registered, or no such person or account on the profile |
| 404 | `repository_not_found` | GitHub doesn't have it, or no linked installation reaches it |
| 409 | `git_provider_not_configured` | An install link, or connecting, without an App |
| 409 | `git_not_connected` | Linking an account none of the profile's people reaches |
| 422 | `validation_failed` | A brought App GitHub refuses, a `return_url` not allowed |
| 429 | `limit_exceeded` | Linking a person or account past the profile's limit; carries `limit: {kind: "git_connections", value}` |
| 502 | `git_provider_refused` | GitHub refused (revoked, uninstalled, suspended) |
| 502 | `git_provider_unavailable` | GitHub down or rate-limited |

## Security

- The state parameter is server-side, single use, bound to the profile and to the setup session or install link, and expires in 10 minutes.
- An `installation_id` from a redirect is accepted only when the person's own user token reaches it; a profile only links accounts one of its people reaches.
- The App's secrets are never returned; `GET …/github-app` shows their presence only.
- User tokens are returned only by the token API, **to admin keys and dashboard sessions, never to profile keys** ([tokens](tokens.md#user-tokens)), and only for a person the profile links.
- A person who stops being linked anywhere in the project is deleted and their grant revoked, so the App doesn't keep acting for someone who left.
- A project's allowed return origins bound where install links send people.

## Observability

- Activity (profile scope, see [activity-log](../activity-and-webhooks/activity-log.md#kinds)): `github.person_added`, `github.person_removed`, `github.installation_added`, `github.installation_removed`, `github.authorization_lost`, `github.limit_reached`.
- Webhooks: `git_connection.person_added`, `git_connection.person_removed`, `git_connection.installation_added`, `git_connection.installation_removed`, `git_connection.installation_suspended`, `git_connection.failed` (each with `provider: "github"`; GitHub refusing a connection), each with the profile.
- Metrics: `lewbox_github_requests_total`, `lewbox_github_rate_limit_remaining`, `lewbox_github_user_token_refresh_total{outcome}`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_GITHUB_API_URL` | `https://api.github.com` | For a fake, or GitHub Enterprise later |
| `LEWBOX_GITHUB_URL` | `https://github.com` | |
| `LEWBOX_GITHUB_LISTED_REPOSITORIES_MAX` | `1000` | |
| `LEWBOX_GITHUB_LISTED_REPOSITORIES_CACHE` | `60s` | |
| `LEWBOX_GITHUB_LISTED_BRANCHES_MAX` | `1000` | |
| `LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` | `25` | People and accounts together, per profile ([the limit](#the-limit)) |

The manifest's permissions and events are in code; changing them affects only Apps registered afterwards, and the App page shows what older Apps lack.

## Testing

- Fake GitHub: manifest conversion, `GET /app`, OAuth (code and refresh, with refresh tokens invalidated on use), `/user`, `/user/emails`, `/user/installations`, `/installation/repositories`, branches.
- The state check; the installation-id check (an id the person doesn't reach is refused); several installations; two profiles linking one organization's installation, then one disconnecting; the installation deleted while two profiles link it.
- Two concurrent refreshes of one user token: one refresh reaches GitHub. Two profiles linking one person share one token.
- Several people on one profile: the default; unlinking the default; unlinking a person re-points or unlinks the accounts only they reached; a person no profile links is deleted and their grant revoked.
- The limit: people and accounts counted together; a connect bringing more accounts than fit links the redirected one first and lists the rest as available; linking past it is `429 limit_exceeded`; two connects racing on one profile can't both pass it (the profile lock).
- Bringing an App with a wrong key, and with missing permissions.

## Build plan

1. `github_apps`, `github_installations`, `github_people`, `github_person_installations`, `profile_github_people`, `profile_github_accounts` migrations; sqlc queries.
2. `plugins/git/github` (the `git/github` plugin): JWT (RS256, `iss` client id, 9 minutes, backdated 60 s), installation tokens, OAuth and refresh, REST client.
3. Manifest flow and bring-your-own endpoints; the dashboard's GitHub settings page.
4. Connect callback; install links; linking and unlinking people and accounts; the limit; the default person.
5. Repositories and branches.
6. Re-applying setup on connection changes (hook into [sandboxes/profile-setup](../sandboxes/profile-setup.md)).

## Open questions

1. ~~**Organizations that forbid third-party Apps.**~~ Decided (2026-10-29): a known limitation, accepted for now; revisit when a project needs it ([non-goals](#non-goals)).
2. **GitHub Enterprise Server**: a per-project API URL. Not for the alpha.
3. **Allowed return origins**: per project or per admin key?
4. **Bring-your-own App in the alpha**, or manifest only? Proposed: both; bringing one is a form and a `GET /app`.
5. ~~**Replacing a connection's person.**~~ Decided (2026-10-29): a profile holds several people, so connecting another person adds them, never replaces ([people and accounts](#people-and-accounts)).
6. **A per-project limit.** The limit is the instance's for now. Does a project need a lower one for its profiles? Later, if asked.
