# Git providers

> Status: Draft · Milestone: Alpha (M3) for the interface and `git/github`; other hosts Later · Updated: 2026-10-29

A Git host (GitHub today; GitLab, Bitbucket, Gitea or Forgejo later) is a **Git provider plugin**. The core's Git framework ([git](../git.md)) owns what's the same for every host: the sandbox's Git setting and access levels, the token files under `/run/lewbox/git/`, the credential helper and its repository map, the CLI wrapper, refreshing tokens before they expire, the token API's shape, cloning on create, and the snapshot scrub. A provider owns what's particular to its host: how a profile connects, how tokens are minted and narrowed, how repositories and branches are listed, how its access levels map to its permissions, and its webhooks.

## Today

GitHub is written into the app (`lewbox-legacy/app/Actions/GitHub/*`, `app/Sandboxes/Setup/GitSetup.php`, `IssueGitHubToken.php`), and the rebuild's Git plans describe it as GitHub throughout ([git/github-app](../git/github-app.md), [git/tokens](../git/tokens.md)). This plan draws the line between the framework and the GitHub plugin, so a second host is a plugin, not a rewrite.

## Goals

- The framework never names GitHub: no `github.com` outside `plugins/git/github` except as configuration.
- A sandbox's Git setting can hold repositories on several hosts, each with tokens from its provider.
- Access levels (`read`, `write`, `full`, exact permissions) mean the same thing on every host, as near as the host allows.

## Design

### What's framework and what's provider

| Framework (core, `internal/git`) | Provider (`plugins/git/<host>`) |
|---|---|
| The sandbox's `git` setting: repositories, access per repository, the project's cap ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)) | What `read`, `write` and `full` mean in the host's permissions; which exact permission names exist |
| Minting for a sandbox: grouping repositories, asking the provider, writing token files and the repository map, the credential helper, refresh jobs at `expires_at − 15 min`, partial applies | Minting: the host's API calls, grouping by its unit of authorization (GitHub: installation per account), expiry |
| The token API's routes, permission checks, activity, reuse cache and audit | Issuing: installation-like and user tokens on the host |
| Repository references in `repositories` and `git.repositories`: parsing `owner/name`, URLs and `host/owner/name` | Recognising its host's URLs, canonical names, SSH forms |
| Cloning on create (core runs `git clone` through the toolbox) | The clone URL and which token clones it |
| Repository and branch list routes (paging, search) | Listing on the host |
| The connection's place on profiles: linked people and accounts, the per-profile limit | The connection itself: app registration, OAuth, installations, its tables and routes |
| Inbound hooks route `/hooks/<host>/{prj}`, deduplication, forwarding to project webhooks as `<host>.<event>` | Verifying the signature, parsing events it acts on, which events exist |
| The CLI wrapper's mechanism (choose a token by repository) | The CLI's name and token variable (`gh` and `GH_TOKEN`; `glab` and `GITLAB_TOKEN`) |

### The interface

```go
package gitprovider // github.com/harlew-dev/lewbox/plugin/gitprovider

type Provider interface {
    Info() Info

    // References
    Match(ref string) (Repo, bool)            // "acme/web" (if default host), "https://github.com/acme/web(.git)",
                                              // "git@github.com:acme/web.git", "github.com/acme/web"
    CloneURL(r Repo) string                   // https://github.com/acme/web.git

    // Access
    Levels() Levels                           // read, write, full → this host's permissions
    ValidatePermissions(p Permissions) error  // exact permissions: names and values this host takes

    // Tokens
    MintSandbox(ctx context.Context, c Connection, req SandboxRequest) ([]Token, error)
    Issue(ctx context.Context, c Connection, req IssueRequest) ([]Token, error)            // installation-like
    IssueUser(ctx context.Context, c Connection, person string, req IssueRequest) (Token, error) // optional: ErrNotSupported
    Revoke(ctx context.Context, t Token) error                                             // optional

    // Listing (for pickers)
    Repositories(ctx context.Context, c Connection, q ListQuery) (Page[RepoInfo], error)
    Branches(ctx context.Context, c Connection, r Repo, q ListQuery) (Page[Branch], error)

    // Identity for commits, from the profile's default person
    Identity(ctx context.Context, c Connection) (name, email string, err error)

    // Inbound webhooks
    Webhooks() WebhookHandler                 // nil if the host has none
}

type Info struct {
    ID        string   // "github": also the path segment in …/{provider}/tokens
    Host      string   // "github.com" (a self-hosted provider takes it from settings)
    Title     string   // "GitHub"
    CLI       CLIInfo  // {Name: "gh", TokenEnv: "GH_TOKEN", HostsFile: "/root/.config/gh/hosts.yml"}
    Default   bool     // owns bare "owner/name" references; at most one provider per build
}

type Connection interface {          // the profile's connection, as the provider stored it
    Profile() plugin.ProfileRef
    Accounts() []Account             // the units tokens are minted for (GitHub: linked installations)
    People() []Person                // identities user tokens act as
}

type Token struct {
    Value       string               // never logged; redacted from every output
    ExpiresAt   time.Time
    Account     string               // the unit it was minted for
    Repos       []Repo
    Permissions Permissions          // as granted
    Reused      bool
}

type SandboxRequest struct {
    Repos  []RepoAccess              // each repository with its effective access (level or exact)
    Sandbox plugin.SandboxRef
}
```

The provider stores its connection in its own tables (`github_apps`, `github_installations`, `github_people`… for GitHub, declared as owned, [git/github-app](../git/github-app.md#tables)), and implements `Connection` over them. The framework only links people and accounts to profiles (`profile_git_people`, `profile_git_accounts`, keyed by provider), counting them against the per-profile limit.

### What the framework calls when

| When | Framework does | Provider called |
|---|---|---|
| Sandbox create, with `repositories` | Parse each with every provider's `Match` (default provider for `owner/name`); a public URL no provider matches is cloned without a token | `Match`, `CloneURL` |
| Setup (create, start, change, refresh) | Group the sandbox's repositories by provider; per provider, ask for tokens; write files, the map, the helper, the wrapper | `MintSandbox` |
| Refresh job at `expires_at − 15 min` | Partial `git` apply for that provider's tokens | `MintSandbox` |
| `POST /v0/profile/git/tokens`, `…/profiles/{prf}/git/tokens` (with `provider`) | Auth, permission (`git.tokens`, `git.tokens.user`), validation, reuse cache, activity, audit | `Issue` or `IssueUser` |
| Repository and branch pickers | Routes, paging, caching (`LEWBOX_GIT_LIST_CACHE_TTL`) | `Repositories`, `Branches` |
| Commit identity in setup | `git config --global user.name/email` | `Identity` |
| `POST /hooks/{provider}/{prj}` | Deduplicate deliveries, store, forward `<provider>.<event>` to subscribed endpoints | `Webhooks().Verify`, `.Handle` |
| Sandbox delete | Revoke what it holds, best effort | `Revoke` |

**Routes are provider-neutral** (decided 2026-10-29): `…/git/tokens`, `…/git/people`, `…/git/accounts`, `…/git/repositories`, `…/git/install-links`, and `GET`/`DELETE …/git` for a profile's connections. Each takes `provider` (a body field, or `?provider=` on reads), defaulting to the only Git provider on for the project; with several on and none named, `422 validation_failed` on `provider`. Lists answer every provider's entries, each with its `provider`. A second host adds nothing to the paths ([git/tokens](../git/tokens.md#the-token-api)). What stays provider-specific: the provider's project configuration under its plugin settings (`/v0/projects/{prj}/settings/git/github`), its inbound webhooks (`/hooks/github/{prj}`), its browser callbacks, and forwarded raw events (`github.<event>`, the host's payloads verbatim).

### Access levels

```go
type Levels struct {
    Read  Permissions   // GitHub: every App permission at read
    Write Permissions   // GitHub: contents and pull_requests write, the rest read; not workflows
    Full  func(app Permissions) Permissions // GitHub: everything the App has, at its level
}
```

A host that can't narrow a token as finely as the level asks (some hosts' tokens are all-or-nothing per project) must round **down** and report what it granted in `Token.Permissions`; the sandbox's Git report shows the difference. Rounding up is a contract-test failure.

### The credential helper and the wrapper

The framework writes one helper for every host: Git calls it with `protocol`, `host` and `path` (`credential.useHttpPath`), and it looks the repository up in `/run/lewbox/git/map` to find the token file. Each provider's `CLI` info adds a wrapper for its CLI (`/usr/local/bin/gh`, `/usr/local/bin/glab`) that picks the token the same way and sets the CLI's token variable. Nothing provider-specific runs in the sandbox beyond those two names.

### Errors

| Code | Status | When |
|---|---|---|
| `plugin_disabled` | 409 | A repository, token or list on a host whose plugin is off |
| `git_provider_not_configured` | 409 | The project hasn't configured this host yet (GitHub: no App) |
| `git_not_connected` | 409 | The profile has no connection to that host (was `github_not_connected`) |
| `git_person_not_authorized` | 409 | A user token for a person who hasn't authorized the host, or whose authorization lapsed |
| `repository_not_found` | 404 | The host doesn't have it, or the connection doesn't reach it |
| `repository_not_installed` | 422 | Reachable by the person but not by the token's unit (GitHub: not chosen for the installation) |
| `git_provider_refused` | 502 | The host refused (suspended, uninstalled, revoked); was `github_refused`. No alias: `/v0` may break |
| `git_provider_unavailable` | 502 | The host couldn't be reached; was `github_unavailable` |

Every one carries `provider`. These are the contract: a provider maps its host's failures onto them and never invents its own codes for them. Lewbox's own connection webhooks are `git_connection.*` (`person_added`, `person_removed`, `installation_added`, `installation_removed`, `installation_suspended`, `failed`), each with `provider`.

### Contract tests

`plugin/gitprovider/gittest.Run(t, provider, opts)` against the provider's fake host (`githubtest` for GitHub, [foundation/testing](../foundation/testing.md)):

- `Match` accepts every documented reference form and refuses others; `CloneURL` round-trips.
- Levels: `read` grants no write; `write` grants code push and nothing that changes CI; `full` never exceeds what the connection has; exact permissions validated.
- `MintSandbox` narrows to exactly the requested repositories, per access; expiry set; failure for one account doesn't fail the others.
- `Issue` and `IssueUser` respect permissions, reuse, and refuse what the connection can't do.
- Listing: paging and search; a repository the connection can't reach isn't listed.
- Webhooks: signature checked; replays deduplicated.
- **The sandbox end-to-end:** with the fake toolbox, a sandbox with two repositories at different access gets the right token per repository through the helper, and `ScrubPaths` covers every token file.

## Adding a new Git host

Taking **GitLab** as the example:

1. **Decide the unit of authorization.** GitLab has OAuth applications and group or project access tokens; the closest to a GitHub App installation is an OAuth application per project (Lewbox's project) with a group access token per group, or user OAuth tokens. Write it down in the plugin's docs: what a person connects, what an account is, how tokens are narrowed.
2. **Make the plugin:** `plugins/git/gitlab/` with `Info{Name: "git/gitlab", Kind: KindGit}`, its tables (`gitlab_…`) and migrations, its connection routes (registering the OAuth app, connecting a person, linking groups), and its settings (self-managed instance URL).
3. **Implement `Provider`**: references for `gitlab.com` (and the configured host for self-managed), clone URLs, levels (`read_repository`, `write_repository`, `api`), minting (project or group access tokens with expiry, narrowed where the API allows; round down otherwise), listing, identity, webhooks (`X-Gitlab-Token`).
4. **CLI info:** `glab`, `GITLAB_TOKEN`, its config file (written without a token, and scrubbed).
5. **Setup pages:** the connection's UI is the plugin's slot on the setup page ([dashboard-slots](dashboard-slots.md)).
6. **Fake host:** `gitlabtest`, enough of the API for the contract suite.
7. **Run `gittest`**, then a live test against gitlab.com nightly.
8. **Docs:** what each access level means on GitLab, and what can't be narrowed.

## Build plan

1. `plugin/gitprovider` and the framework's calls through it; `profile_git_people` and `profile_git_accounts` keyed by provider (M3).
2. GitHub as `plugins/git/github`, moved from the framework (M3).
3. `gittest` with `githubtest` (M3).

## Open questions

1. ~~**Error code names.**~~ Decided (2026-10-29): provider-neutral codes (`git_not_connected`, `git_provider_not_configured`, `git_person_not_authorized`, `git_provider_refused`, `git_provider_unavailable`), routes under `…/git/…` with `provider`, and `git_connection.*` events. GitHub-specific: plugin settings, inbound hooks, callbacks, forwarded `github.<event>` and the plugin's tables.
2. **Bare `owner/name`.** Always the default provider (GitHub in the default build), or refused when more than one Git provider is on for the project?
3. **Profile linking tables.** The Git plans name them `profile_github_people` and `profile_github_accounts`; generalise to `profile_git_people`/`profile_git_accounts` with a `provider` column (proposed), or let each provider keep its own?
