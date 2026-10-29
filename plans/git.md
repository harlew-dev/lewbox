# Git

> Status: Draft · Milestone: Alpha (M3) for the GitHub connection, sandbox tokens and the token API; Alpha (M4) for forwarded GitHub events · Core Git framework; GitHub is the `git/github` plugin · Updated: 2026-10-29

Lewbox connects a profile to GitHub **through the project's own GitHub App, and nothing else**, so its sandboxes can clone private repositories and agents can run `git` and `gh` in them. Lewbox does four things with that connection:

1. **Clones** the repositories a sandbox is created with ([sandboxes/repositories](sandboxes/repositories.md)).
2. **Keeps tokens in sandboxes**: short-lived installation tokens, narrowed to the sandbox's repositories, with **the access the user chose for each repository** (`read`, `write`, `full` or exact permissions; default `full`), written as files that `git` and `gh` read on every call, and refreshed before they expire.
3. **Issues tokens** to the project's own servers through the API: installation tokens narrowed to the repositories and permissions asked for, and a named person's user token (admin keys and the dashboard only), so the project's product can push, open pull requests and create repositories itself.
4. **Forwards GitHub's events** to the project's webhook endpoints, since the App's webhooks come to Lewbox.

Lewbox is **not** a Git or GitHub client for its consumers. Legacy grew a push pipeline (`git-receive-pack` spoken in PHP), a 719-line pull request API and repository creation, all re-implementing what `git` and GitHub's API already do. The rebuild drops them: a project that needs them does them on its own servers with a token Lewbox issues, as described in [git/integrating](git/integrating.md). Lewagent, the largest consumer, moves that code into itself.

| Plan | What it covers | Milestone |
|---|---|---|
| [git/github-app](git/github-app.md) | The project's GitHub App (manifest registration, or bringing one), connecting a profile by installing or authorizing it, installations on personal accounts and organizations, the user token, listing installations, repositories and branches, commit identity | Alpha (M3) |
| [git/tokens](git/tokens.md) | Tokens in sandboxes (files, credential helper, `gh` wrapper, refresh jobs, the sandbox's `git` setting and the project's cap), and the token API for the project's servers | Alpha (M3) |
| [git/integrating](git/integrating.md) | Recipes for what Lewbox doesn't do: pushing from your servers, pull requests and reviews, creating repositories, reacting to forwarded events; how Lewagent ports legacy's push and pull request code; security guidance | Alpha (M4), documentation |

Inbound GitHub webhooks (installation changes, and forwarding) are in [activity-and-webhooks/github-webhooks](activity-and-webhooks/github-webhooks.md). The Git step of profile setup is in [sandboxes/profile-setup](sandboxes/profile-setup.md).

### Core and the `git/github` plugin

Git is split so another host can be added without touching core ([plugins/git-providers](plugins/git-providers.md)):

| Core (the Git framework) | The `git/github` plugin (`plugins/git/github/`) |
|---|---|
| The sandbox's `git` setting (`access` per repository, the project's cap) and its API | The project's GitHub App: manifest registration, bringing one, its keys |
| Cloning on create, for any `https` URL ([sandboxes/repositories](sandboxes/repositories.md)) | Connecting people and installations; the per-profile limit; listing repositories and branches |
| The in-sandbox credential helper, the repository map, the `gh` wrapper, writing token files and refreshing them on schedule ([git/tokens](git/tokens.md)) | Minting installation and user tokens for repositories and permissions; mapping `access` presets to GitHub permissions |
| The provider-neutral routes (`…/git`, `…/git/tokens`, `…/git/people`, `…/git/accounts`, `…/git/repositories`, `…/git/install-links`, each with `provider`), the error codes (`git_not_connected`, `git_provider_refused`, …) and `git_connection.*` webhooks; the token API's permissions (`git.tokens`, `git.tokens.user`), activity and reuse cache | Its project settings (`/v0/projects/{prj}/settings/git/github`: the App), its tables (`github_*`), its webhook ingress (`/hooks/github/{prj}`), its browser callbacks, and forwarded `github.<event>` events |
| The commit identity | Its setup-page section and settings page |

A Git provider plugin implements one interface: resolve a repository reference to its provider, owner and name; mint a token for repositories and an access level; refresh; list repositories and branches; and, optionally, receive webhooks. Turned off, `git/github` stops minting (sandboxes keep cloning public repositories), its routes answer `plugin_disabled`, and its connections are kept. GitLab or Bitbucket would each be another plugin beside it.

---

## Today

Legacy built all of this in the Laravel app, ported from Lewagent and grown over October 2026. The design is written up in `lewbox-legacy/docs/profiles.md` § *Git integration*.

- **Two kinds of connection per profile** (`profile_credentials` with `agent` null): a pasted fine-grained token (`github`), or the tenant's own GitHub App (`github_app`), registered from a manifest (`app/Actions/GitHub/RegisterGitHubApp.php`, `BuildGitHubAppManifest.php`) and installed by the profile's person from the hosted setup page (`ConnectGitHubInstallation.php`). The tenant picks one **Git mode** for its customers (`tenants.git_mode`).
- **Several installations per connection** (2026-10-29, Lewbox `522ae4d`, not yet deployed): `profile_github_installations`, one per GitHub account the person reaches, so a person's own account and an organization (`harrison` and `harlew-dev`) both work.
- **Tokens in sandboxes** (`IssueGitHubToken.php`, `app/Sandboxes/Setup/GitSetup.php`): installation tokens minted for each sandbox, read-only (`contents`, `metadata`, `pull_requests` read, since 2026-10-28), narrowed to the sandbox's repositories on each account, one file per account, a credential helper with `useHttpPath`, a `gh` wrapper choosing `GH_TOKEN` by owner. Refreshed every 5 minutes by `RefreshGitHubAppTokens`, through the gateway's files API **with sandbox keys the app minted for itself** (`PushGitHubAppToken.php`).
- **Pushes through Lewbox** (2026-10-28, `app/Actions/Pushes/*`, `app/Jobs/Pushes/RunPush.php`): pack in the sandbox, stream to GitHub's `git-receive-pack` from PHP, verify against the sandbox's account of the change, confirm workflow changes and mismatches, move the branch forward. Admin API only.
- **Pull requests through the Admin API** (`app/Actions/GitHub/GitHubPullRequests.php`, 719 lines of REST and GraphQL): create, edit, ready, merge, files, viewed, threads, comments, replies, resolve, pending reviews. Lewagent's Git panel is built on them.
- **Repositories:** listing, branches, repository owners (`ListGitHubRepositoryOwners.php`), creating a repository as the person (`CreateGitHubRepository.php`).

What's wrong with it:

1. **Lewbox re-implements Git and GitHub.** A `git-receive-pack` client, a push state machine with its own cleanup command, and a pull request API that wraps GitHub's REST and GraphQL one call at a time. Every new thing a consumer wants (comment as the App, a pull request by number, a wiki) is a new Lewbox endpoint, release and SDK method. The consumer could call GitHub itself if it had a token.
2. **Pasted tokens can't be narrowed.** A sandbox of a profile connected with one can push with it, and every repository the token reaches is reachable from the sandbox. Two kinds of connection double the setup page, the docs and the tests.
3. **The app reaches sandboxes through its own public API.** Token refreshes, pushes and clean-ups mint sandbox keys and go app → HTTP → gateway → toolbox.
4. **Polling for refreshes**, every 5 minutes, when each token's expiry is known exactly.
5. **Two auth paths for one feature.** Lewagent's Git panel uses its profile key for repositories and commands, and the tenant key for pushes, pull requests and `git/repositories`.
6. **State spread over JSON columns:** `sandboxes.git_repositories`, `sandboxes.repositories`, `github_token_expires_at`, connection `metadata`.
7. **Events never reach the consumer.** The App's webhooks come to Lewbox, which ignores everything but installations, so Lewagent can't follow pull requests merged on GitHub.

## Goals

- **GitHub App only.** One kind of connection, always narrowed, always short-lived.
- **A profile holds several GitHub people and accounts**: any number of people (a person, a teammate, a machine user) and every account they work on, personal and organizations, each an installation of the project's App, up to a configurable limit, 25 by default ([github-app](git/github-app.md#people-and-accounts)).
- **Lewbox issues tokens; consumers do Git.** The token API gives the project's servers exactly the token they ask for (repositories, permissions), never more than the App has, logged each time.
- **The user decides how much a sandbox may do on GitHub**, per sandbox and per repository: `read`, `write`, `full` (the default) or exact permissions. A project that wants nothing in its sandboxes to push, like Lewagent, sets a cap of `read`, and then no key can give a sandbox more.
- Refreshes as jobs at known times; sandbox writes through the toolbox client directly; proper tables.
- **Every GitHub event the App receives can reach the project**, forwarded as it came.

### Non-goals

- **Pushing, pull requests, reviews, comments, repository creation** through Lewbox's API. [git/integrating](git/integrating.md) shows how to do each with an issued token.
- **Pasted personal access tokens**, OAuth Apps, and the Git mode that chose between them.
- **Organizations that forbid third-party Apps.** Their members can't connect: a known limitation, accepted for now (decided 2026-10-29), to revisit when a project needs it.
- **Other Git hosts.** Clones from other hosts' public `https` URLs keep working, without a token ([sandboxes/repositories](sandboxes/repositories.md)). GitLab and Bitbucket would come later as their own Git provider plugins.
- **A GitHub API proxy.** Lewbox hands out tokens; it doesn't sit in the path.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | The project's own App via the manifest flow; several installations per profile; narrowed sandbox tokens; files, not env or URLs; the credential helper with `useHttpPath`; the `gh` wrapper; the commit identity rules; listing repositories and branches; installation webhooks; the check that a redirected installation id is one the person reaches |
| **Change** | Tenant → project. **People and installations belong to the project and are linked to profiles**, so one organization's installation or one person serves several profiles, and **a profile holds several people and accounts**, up to a limit (legacy: one person). Refreshes scheduled per sandbox at `expires_at − 15 min`. Toolbox calls in-process. A sandbox's Git access is a setting on the sandbox (`git.access`, and `access` per repository in `git.repositories`), **chosen by the user, default `full`** where legacy was read-only in code, and optionally capped by the project. Each person's user token is kept and refreshed, and handed out by the token API to admin keys and the dashboard only. GitHub's events are forwarded, whole, to webhook endpoints that subscribe to them. Normalized tables |
| **Drop** | Pasted tokens and `tenants.git_mode`. Pushes (`pushes`, `push_steps`, `git-receive-pack`, the push error codes). The pull request API. Repository creation and the repository owners list. Sandbox keys minted for internal work. JSON columns for grants |

## Design

### Data model (summary)

Full columns in the part plans.

| Table | Holds | Plan |
|---|---|---|
| `github_apps` | A project's GitHub App: id, slug, client id and secret, private key, webhook secret, bot user id, permissions and events. At most one per project | [github-app](git/github-app.md#tables) |
| `github_installations` | The App's installations the project knows of, one per GitHub account: installation id, account, selection, permissions accepted, suspended | [github-app](git/github-app.md#tables) |
| `github_people` | GitHub users who authorized the project's App: identity and user token (encrypted, refreshed). One per person per project | [github-app](git/github-app.md#tables) |
| `github_person_installations` | Which installations each person reaches | [github-app](git/github-app.md#tables) |
| `profile_github_people`, `profile_github_accounts` | The people and accounts a profile holds, one person the default; together at most `LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` (25) | [github-app](git/github-app.md#tables) |
| `sandbox_git_repositories` | The repositories a sandbox's tokens reach: cloned on create, or added later | [tokens](git/tokens.md#tables) |
| `sandboxes.git_access`, `sandbox_git_repositories.access` | The sandbox's access and each repository's own (`read`, `write`, `full` or a permission set; default `full`) | [tokens](git/tokens.md#the-sandboxs-git-setting) |
| `sandbox_git_tokens` | The token written into a sandbox for each installation, its repositories and `expires_at` (never the token) | [tokens](git/tokens.md#tables) |
| `github_issued_tokens` | Tokens issued through the API: who, for what, until when; the token itself encrypted only while it's reused | [tokens](git/tokens.md#tables) |
| `github_deliveries` | Inbound webhook deliveries, for deduplication, forwarding and debugging | [github-webhooks](activity-and-webhooks/github-webhooks.md) |

Commit identity stays on the profile (`profiles.git_name`, `profiles.git_email`), see [profiles](profiles.md).

### How the parts fit

```
 Setup page / install link / dashboard                    GitHub
   │  install the App on an account, or authorize it        ▲
   ▼                                                        │ App JWT (RS256) → installation tokens
 profile_github_people ── profile_github_accounts ──────────┤ user tokens (refreshed, per person)
   │                         │                              │
   │                         ▼                              │
   │                  github_installations ◀── installation webhooks (/hooks/github/{prj})
   │                                                        │
   ├─ profile setup (create, start, apply_setup) ───────────┤
   │    sandbox_git_repositories + git_access               │
   │    mint per (account, access), narrowed                │
   │    ▼                                                   │
   │  toolbox: token files, credential helper, gh wrapper   │  ── job at expires_at − 15 min refreshes
   │  sandbox: git clone / fetch / push / gh … (as chosen)  │
   │                                                        │
   └─ token API (project's servers) ────────────────────────┘
        POST …/git/tokens {type, repositories, permissions}
        ▼
      project's servers: git push, pull requests, reviews, repository creation  (git/integrating)

 GitHub events ─▶ /hooks/github/{prj} ─▶ installation handling, and github.<event> to subscribed webhook endpoints
```

### People and accounts

A profile holds **any number of GitHub people** (each authorized the project's App, each with their own user token) and **any number of accounts** (installations of the App, personal or organization, each reached by one of those people), **together at most 25** by default (`LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE`, an instance setting). People and installations are the project's: an organization installs the App once and a person authorizes it once, and every profile using them links the same rows. One of a profile's people is its default, used for the commit identity and `gh`'s `hosts.yml`. Details and the reasoning are in [github-app](git/github-app.md#people-and-accounts).

### Who can do what

| Action | Admin key | Profile key | Sandbox key | Dashboard |
|---|---|---|---|---|
| Register, bring or remove the project's App; set the sandbox Git cap | ✓ | | | ✓ (project owners and admins) |
| Make an install link; link or unlink a person or account; choose the default person | ✓ | | | ✓ |
| Read the profile's people, accounts, repositories, branches | ✓ | ✓ | | ✓ |
| Read a sandbox's Git setting | ✓ | ✓ | ✓ | ✓ |
| Change a sandbox's Git access and repositories, up to the project's cap | ✓ | ✓ (`sandboxes.git`) | ✓ (`sandboxes.git`, if asked for) | ✓ |
| Issue an installation token | ✓ | ✓ (`git.tokens`) | | |
| Issue a named person's user token | ✓ (`git.tokens.user` if the key has a list) | **never** | | ✓ (project owners and admins) |

New permissions (`sandboxes.git`, `git.tokens`, and the admin-key-only `git.tokens.user`) are in [api/permissions](api/permissions.md). Sandbox keys never issue tokens: a sandbox key is for a browser, and anything a browser holds can leak. Profile keys never get user tokens: a profile key often lives in a customer's own systems, and a user token can't be narrowed.

## API (summary)

Each part plan has the endpoints in full.

```
# The project's App (admin key or dashboard session)
GET    /v0/projects/{prj}/settings/git/github
PUT    /v0/projects/{prj}/settings/git/github                         bring an App registered by hand
POST   /v0/projects/{prj}/settings/git/github/manifests                start the manifest flow
POST   /v0/projects/{prj}/settings/git/github/conversions             finish it
DELETE /v0/projects/{prj}/settings/git/github
GET    /v0/projects/{prj}/settings/git/github/installations           every installation the project knows
GET    /v0/projects/{prj}/settings/git/github/deliveries[/{id}]       inbound deliveries, with payloads
POST   /v0/projects/{prj}/settings/git/github/deliveries/{id}/replays

# A profile's connection (admin key or session, under /v0/projects/{prj}/profiles/{prf}/git;
# the profile's own key under /v0/profile/git)
GET    …/git                                              people, accounts, available accounts, the limit
DELETE …/git                                              (management only) unlink every person and account
POST   …/git/install-links                                (management only) install or authorize link with return_url
PATCH  …/git/people/{login}                               (management only) {default: true}
DELETE …/git/people/{login}                               (management only) unlink a person
PUT    …/git/accounts/{account}                           (management only) link an available account, within the limit
DELETE …/git/accounts/{account}                           (management only) unlink an account
GET    …/git/repositories[/{owner}/{repo}]
GET    …/git/repositories/{owner}/{repo}/branches
POST   …/git/tokens                                       issue a token (user tokens: admin keys and sessions only)

# A sandbox's
GET    /v0/sandboxes/{sbx}/git
PUT    /v0/sandboxes/{sbx}/git

# Inbound and callbacks
POST   /hooks/github/{prj}                                   GitHub App webhooks
GET    /github/apps/{prj}/authorized                                install / authorize callback
```

## Errors (summary)

| Status | Code | When |
|---|---|---|
| 409 | `git_not_connected` | The profile has no connection, or none of its installations is on the repository's owner. Carries `accounts` (the owners without one) |
| 409 | `git_provider_not_configured` | An install link, or anything needing the App, before the project has one |
| 409 | `git_person_not_authorized` | A user token asked for, when the person hasn't authorized the App or their authorization lapsed |
| 404 | `repository_not_found` | GitHub doesn't have it, or the installation doesn't reach it |
| 422 | `validation_failed` | A permission GitHub doesn't have, one above the App's or the project's cap, too many repositories |
| 502 | `git_provider_refused` | GitHub refused: the App uninstalled from the account, the installation suspended, an authorization revoked |
| 502 | `git_provider_unavailable` | GitHub couldn't be reached, or answered with a server error or rate limit |

The full list lives in [api/errors](api/errors.md). The push error codes legacy had (`head_moved`, `lfs_not_supported`, `not_fast_forward`, `pack_too_large`, `stalled`, `push_in_progress`, `push_not_waiting`, `push_not_cancellable`) are gone with pushes.

## Security

- **The App's private key, client secret and webhook secret, and user tokens** are encrypted with the keyring ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)), and never returned by any API but the token API's user token.
- **Sandbox tokens** have the access the user chose per repository, default `full` (every permission the App has), for the sandbox's repositories only, expiring within the hour. The project's cap, if it sets one, is a ceiling no key can exceed ([tokens](git/tokens.md#the-sandboxs-git-setting)). Tokens are redacted from everything Lewbox stores.
- **Issued tokens** are narrowed to the repositories asked for; their permissions are capped by the App's. Each issue is in the profile's activity log, without the token. Only profile keys with `git.tokens` and admin keys issue them, never sandbox keys.
- **User tokens can't be narrowed** (GitHub's limit: they reach what the person and the App both can). So only admin keys and the dashboard get them, for a named person, never profile keys or sandboxes, and the docs say to keep them on servers ([integrating](git/integrating.md#security)).
- **Installation ids from GitHub's redirect are never trusted alone**: they must be among the installations the person's own user token reaches (`GET /user/installations`), as legacy does.
- **No credential enters a snapshot.** Git's token files, helper, wrapper and config are removed before every snapshot, whatever stopped the sandbox ([sandboxes/profile-setup](sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)).
- **Sandboxes can push by default.** With `full` the default, anything running in a sandbox (an agent, a hostile package, a prompt injection) can push to, and change the workflows of, the sandbox's own repositories, for up to an hour per token. The user chose it; Lewbox shows it in the dashboard and the activity log, narrows tokens to the sandbox's repositories, and lets a project cap it at `read` ([tokens § Security](git/tokens.md#security)). [integrating](git/integrating.md#security) says how to keep a push path like Lewagent's "nothing on the computer can push".

## Observability

- Activity: `git.token_written`, `git.token_failed`, `git.repositories_changed`, `git.access_changed` on sandboxes; `github.token_issued`, `github.person_added`, `github.person_removed`, `github.installation_added`, `github.installation_removed`, `github.limit_reached` on profiles. Never a token.
- Webhooks: `git_connection.*` (Lewbox's own, about connections), and forwarded GitHub events `github.<event>` ([activity-and-webhooks/github-webhooks](activity-and-webhooks/github-webhooks.md#forwarding)).
- Metrics: `lewbox_github_requests_total{endpoint,status}`, `lewbox_github_rate_limit_remaining{installation}`, `lewbox_git_token_refresh_total{outcome}`, `lewbox_github_tokens_issued_total{type,outcome}`.
- Logs carry the installation id and repositories, never a token.

## Testing

A fake GitHub (`githubtest` ([foundation/testing](foundation/testing.md))) serving what we use: manifest conversion, OAuth (code and refresh), `/user`, `/user/emails`, `/user/installations`, `/app`, installation tokens with repositories and permissions checked as GitHub checks them, `/installation/repositories`, branches. The fake toolbox ([foundation/testing](foundation/testing.md)) records the files written. Contract tests against real GitHub run nightly on a test App and a test organization. [integrating](git/integrating.md#testing) has an end-to-end test of the push recipe against the fake, so the recipe we document keeps working.

## Build plan

1. `plugins/git/github` (was planned as `internal/github`): App JWTs, installation tokens, OAuth and refresh, REST client with rate-limit handling, typed errors (`ErrNotInstalled`, `ErrSuspended`, `ErrRevoked`). Fake GitHub.
2. Tables; the App (manifest, bring your own); people, installations and profile links with the limit; install links; repositories and branches.
3. Sandbox tokens: the core Git framework (`internal/git`: the setting, the setup step, the helper and wrapper, refresh jobs, the project's cap) calling the provider plugin to mint.
4. The token API, with reuse of installation tokens and activity.
5. Inbound webhooks: installations (M3), forwarding (M4).
6. [integrating](git/integrating.md): the recipes, an example in Go and TypeScript, and the end-to-end test.

## Open questions

1. ~~**Several GitHub people per profile?**~~ Decided (2026-10-29): yes, people and accounts together up to a configurable limit, 25 by default ([people and accounts](#people-and-accounts)).
2. **All-repository tokens.** Should the token API issue an installation token reaching *every* repository of an installation (useful for listing or bulk work), for admin keys only? Proposed: not in the alpha; name the repositories.
3. ~~**User tokens on profile keys?**~~ Decided (2026-10-29): no. Admin keys and the dashboard only.
7. ~~**Organizations that forbid third-party Apps.**~~ Decided (2026-10-29): a known limitation for now ([non-goals](#non-goals)).
4. **Forwarding volume.** Forwarding whole payloads for every subscribed event could be heavy for busy organizations. Is a per-endpoint repository filter needed in the alpha ([github-webhooks](activity-and-webhooks/github-webhooks.md#open-questions))?
5. **A Lewbox-maintained helper library** for the push recipe (pack in sandbox, push from server, verify), published with the SDKs? It would be ours to keep working, but outside the API. Proposed: the recipe and an example first; a library if more than Lewagent needs it.
6. **GitHub Enterprise Server**: a per-project API URL. Not for the alpha.
