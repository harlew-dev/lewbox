# GitHub tokens: in sandboxes, and issued to the project

> Status: Draft · Milestone: Alpha (M3) · Core Git framework; minting by the Git provider plugin (`git/github`) · Updated: 2026-10-29

Lewbox mints GitHub tokens from the project's App ([github-app](github-app.md)) for two kinds of holder:

- **Sandboxes**, so `git` and `gh` work inside them for the agent and the person. Each token is an installation token for one account, **narrowed to the sandbox's repositories**, with **the access the user chose for each repository** (`read`, `write`, `full` or exact permissions; **default `full`**), valid an hour, refreshed by Lewbox before it expires, and reaching `git` through a credential helper reading a file on every call.
- **The project's servers**, through the **token API**: an installation token narrowed to the repositories and permissions asked for, or a named person's user token (admin keys and the dashboard only). This is how a project pushes, opens pull requests and creates repositories, on its own servers, without Lewbox doing it for them ([integrating](integrating.md)).

---

## Today

Legacy (`lewbox-legacy/docs/profiles.md` § *The token reaches git through files*, *App tokens are minted for each sandbox*, *Several installations per connection*; `app/Sandboxes/Setup/GitSetup.php`, `app/Actions/GitHub/IssueGitHubToken.php`, `PushGitHubAppToken.php`):

- **Files, never env or URLs.** `/root/.config/lewbox/github-token` (`0600`) for a pasted token; `/root/.config/lewbox/github-tokens/<account>` (lowercase, `0600`) per App installation. A credential helper script at `/root/.config/lewbox/git-credential-github` for `https://github.com` with `credential.useHttpPath`, answering with the owner's file, else the default. `gh` gets `/root/.config/gh/hosts.yml`, and with the App a wrapper at `/usr/local/bin/gh` that picks `GH_TOKEN` by owner (`-R`/`--repo`, a `repos/<owner>/` API path, else `origin`). `git@github.com:` is rewritten to HTTPS. Files Lewbox writes outside its directory are marked `Written by Lewbox`.
- **Why not `GH_TOKEN`**: a shell reads its environment once, so a refreshed token wouldn't reach a running agent, and `GH_TOKEN` would beat `hosts.yml`.
- **Why not in the remote URL**: it would be saved in `.git/config`, every snapshot, and `git remote -v`.
- **Read-only** since 2026-10-28: `contents`, `metadata`, `pull_requests` read (`IssueGitHubToken::SANDBOX_PERMISSIONS`). Fixed in code; no project could choose otherwise. A pasted token was written as it was, and could push.
- **Narrowed** to the sandbox's repositories on the installation's account: those its create cloned, plus `sandboxes.git_repositories` granted later (`PUT /api/v1/sandboxes/{sbx}/git/repositories`, tenant key only). A sandbox with none on an account gets no token for it.
- **Refreshed** every 5 minutes by `RefreshGitHubAppTokens`, for running sandboxes whose `github_token_expires_at` is within 15 minutes, through the gateway's files API with a sandbox key the app minted for itself.
- **Write tokens** were minted only inside Lewbox, for its own pushes (`IssueGitHubToken::forPush`: one repository, `contents: write`, `workflows: write` when allowed) and pull request calls. No API handed a token to the tenant.
- **Cleanup** before a stop removes the token files, `hosts.yml`, helper, wrapper and rewrite, leaving the identity, so snapshots don't hold tokens.

What's wrong:

1. The only way for a consumer to act on GitHub with its customers' connection was a Lewbox endpoint for that action, so Lewbox grew a push pipeline and a pull request API ([git](../git.md#today)).
2. Sandboxes were read-only whether the project wanted it or not; a project whose agents should push from the sandbox had to use pasted tokens, which reach everything.
3. A sweep every 5 minutes instead of a job at each token's known refresh time; writes through the public API with self-minted keys; JSON columns that can't say which installation's token is due.

## Goals

- **Sandbox tokens:** the same guarantees (narrowed, short-lived, files only, cleaned before a stop), with the access **chosen by the user per sandbox and per repository**: `read`, `write`, `full` or exact permissions, default `full`. Legacy was read-only, fixed in code; the new default changes because the user decides, and a project that needs read-only sets a cap.
- **A refresh job per sandbox per installation**, at `expires_at − 15 min`, cancelled when the sandbox stops; writes through the toolbox client directly, as Lewbox's own.
- **The token API:** any token the project's servers need, narrowed and capped, logged, without Lewbox in the path of what's done with it.
- Scripts as real files in the repository (`internal/git/sandboxfiles/*.sh`), embedded and tested.

### Non-goals

- Tokens for hosts other than GitHub.
- Tokens for sandbox keys, or for anything in a browser.
- Revoking an issued installation token before it expires (GitHub can't, unless the holder revokes it itself: [revoking](#revoking)).

## Design

### Tables

`sandbox_git_repositories`: repositories a sandbox's tokens reach.

| Column | Type | Notes |
|---|---|---|
| `sandbox_id` | fk, cascade | |
| `repository` | text | `owner/name`, lowercase |
| `source` | text | `create` (cloned on create) or `setting` (in the sandbox's `git.repositories`) |
| `access` | jsonb, null | This repository's own `access`: a preset (`"read"`, `"write"`, `"full"`) or a permission set. Null: the sandbox's |
| `added_by` | actor | |
| `created_at` | timestamptz | |

Primary key (`sandbox_id`, `repository`). At most `LEWBOX_GIT_MAX_SANDBOX_REPOSITORIES` (100) per sandbox.

`sandboxes.git_access` (`jsonb`, not null, default `"full"`): the sandbox's default access, a preset or a permission set. Effective permissions are worked out at mint time ([minting](#minting-for-a-sandbox)), not stored here, so a permission the App gains later is picked up.

`sandbox_git_tokens`: what's written into a running sandbox. Never the token.

| Column | Type | Notes |
|---|---|---|
| `sandbox_id` | fk, cascade | |
| `github_installation_id` | fk, cascade | |
| `account` | text | |
| `group` | text | Short hash of the effective permissions; with `account`, the file's name |
| `repositories` | text[] | What it was minted for |
| `permissions` | jsonb | What it was minted with |
| `capped` | boolean | The project's cap lowered what was asked |
| `expires_at` | timestamptz | |
| `written_at` | timestamptz | |
| `refresh_job_id` | bigint | The scheduled refresh, so it can be cancelled |
| `error_code`, `error` | text | Last mint's failure for this account |

Primary key (`sandbox_id`, `account`, `group`). Rows are deleted before every snapshot, when the files are removed ([credentials never enter a snapshot](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)), and recreated by setup on start.

`github_issued_tokens`: every token the API issued.

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | Public id `ght_…` |
| `project_id`, `profile_id` | fk | |
| `type` | text | `installation` or `user` |
| `github_installation_id` | fk, null for `user` | |
| `github_person_id` | fk, null for `installation` | Whose user token it was |
| `repositories` | text[] | Null for `user` |
| `permissions` | jsonb | Null for `user` |
| `reuse_key` | bytea | SHA-256 of (installation, sorted repositories, permissions), for reuse; null for `user` |
| `token` | bytea | Encrypted, kept only while it may be reused; nulled once it's within 10 minutes of expiring |
| `token_hint` | text | Last four characters, to match a token someone reports |
| `expires_at` | timestamptz | |
| `issued_by` | actor | The key or user |
| `purpose` | text | The caller's label, e.g. `push`, `pull_request` |
| `sandbox_id` | fk, null | The sandbox the caller said it's for, for the activity log |
| `created_at` | timestamptz | |

Rows are kept 30 days (the activity log keeps the event longer), pruned daily.

### The sandbox's Git setting

**The user decides how much a sandbox may do on GitHub, per repository.** A sandbox's `git` says which repositories its tokens reach and with what access:

```json
"git": {
  "access": "full",
  "repositories": [
    "acme/web",
    { "name": "acme/docs", "access": "read" },
    { "name": "acme/api", "access": { "contents": "write", "pull_requests": "write", "workflows": "write" } }
  ]
}
```

- **`access`** is the default for every repository in the sandbox. **Default `full`.**
- **`repositories`**: GitHub repositories as `owner/name`, or objects with `name` and their own `access`, which overrides the sandbox's. Repositories cloned on create ([sandboxes/repositories](../sandboxes/repositories.md)) are always in it; a clone entry may carry `access` too, as shorthand for listing it here. A repository here but not cloned can be cloned or fetched in the sandbox with `git clone`.

#### Access levels

`access` is a preset or an exact set of permissions:

| `access` | Permissions the tokens get | For |
|---|---|---|
| `read` | Every permission the project's App has, at `read` | Clone, fetch, `gh` reads (issues, pull requests, checks). Nothing in the sandbox can change anything on GitHub |
| `write` | `contents: write` and `pull_requests: write`; every other permission the App has at `read`; never `workflows` | Agents push branches and open pull requests with `gh`, but can't change CI |
| `full` **(default)** | Every permission the App has, at the level the App has it (`workflows: write`, `issues: write`, …) | Everything the App can do, on the sandbox's repositories |
| `{…}` | Exactly these GitHub App permission names → `read` or `write` | Anything in between, e.g. `{"contents": "write"}` to push but not open pull requests |

- `metadata: read` is always included, as GitHub includes it.
- **Presets resolve against what the installation has.** `full` and `write` take the App's permissions as the installation accepted them, so an App with no `workflows` permission simply gives none, and a permission the App gains later is included once the installation accepts it ([github-app](github-app.md)).
- **A custom set must fit the App:** a permission or level the App or the installation lacks is refused (`422 validation_failed` on `git.access`), not silently dropped.
- **Whatever the access, a sandbox's tokens reach only the sandbox's own repositories**: those cloned on create and those in `git.repositories`, never the rest of the installation. `full` means everything on *these* repositories, not everything the App reaches.

The answer also shows what's written now:

```json
"git": {
  "access": "full",
  "repositories": [
    { "name": "acme/web", "access": "full", "permissions": { "contents": "write", "metadata": "read", "pull_requests": "write", "workflows": "write", "issues": "write" } },
    { "name": "acme/docs", "access": "read", "permissions": { "contents": "read", "metadata": "read", "pull_requests": "read", "issues": "read" } }
  ],
  "tokens": [
    { "account": "acme", "repositories": ["acme/web"], "permissions": { … }, "expires_at": "2026-10-29T13:00:00Z", "error": null },
    { "account": "acme", "repositories": ["acme/docs"], "permissions": { … }, "expires_at": "2026-10-29T13:00:00Z", "error": null }
  ],
  "capped": false
}
```

`permissions` is each repository's effective set, and `capped` says whether the project's cap lowered anything.

#### Changing it

Set on create (`git` in the create body, [sandboxes/lifecycle](../sandboxes/lifecycle.md)) or changed with `PUT /v0/sandboxes/{sbx}/git` `{access, repositories}`:

- `access` replaces the sandbox's default. Omitted, it's left as it is.
- `repositories` replaces the `setting` rows (`create` rows stay, with their access unless the entry names them again). Omitted, left as it is.
- On a running sandbox the change is applied at once: re-mint the groups affected, write, remove files no longer needed. The answer waits for the write, up to 30 s (`503 toolbox_unavailable` otherwise; the setting is kept and the next refresh or start writes it).
- A stopped sandbox just records it; setup writes it on start.

**Who may change it:** `sandboxes.git`, held by profile keys, admin keys, and sandbox keys made with it ([api/permissions](../api/permissions.md)). Any level up to the project's cap; there's no separate permission for raising it.

#### The project's cap

`project_git_settings.sandbox_access_max` ([projects/settings](../projects/settings.md)): a preset or a permission set, the most any sandbox in the project may be given. **Default none**: no ceiling but the App's own permissions.

- A project that promises "nothing on the computer can push" (Lewagent) sets it to `read`. Then no key, profile or admin, can give a sandbox more, and the project pushes from its own servers instead ([integrating](integrating.md)).
- Asking above the cap, on create or change, is `422 validation_failed` on `git.access` (or `git.repositories.N.access`), naming the cap.
- Lowering the cap re-mints running sandboxes over it at once, down to the cap, and sets `capped: true` on them. Their own setting is left as it was, so raising the cap again restores it.

### Minting for a sandbox

`MintSandboxTokens(sandbox)` (core), which calls the `git/github` provider for each GitHub account's token:

1. Read the profile's connection. None: no tokens (public clones still work).
2. Work out each repository's effective permissions: its own `access`, else the sandbox's; resolved against the installation's accepted permissions; capped by the project's cap.
3. **Group by (owner, effective permissions).** A GitHub token has one permission set, so a sandbox with `acme/web` at `full` and `acme/docs` at `read` gets two tokens for `acme`. Most sandboxes have one group per account. For each group with a linked installation (not suspended): `POST /app/installations/{id}/access_tokens` with
   ```json
   { "repositories": ["web"], "permissions": { "contents": "write", "metadata": "read", "pull_requests": "write", "workflows": "write", "issues": "write" } }
   ```
   - Owners without a linked installation get no token; their public repositories clone without one.
   - GitHub `422` (a repository the installation doesn't reach): that group's token fails, with `error_code: repository_not_installed` and the repositories named, and the Git step fails saying to choose them for the App on that account; other groups are still written.
   - `404` (uninstalled) / `403` (suspended): recorded on the installation ([github-app](github-app.md#tables)); that account's tokens fail with `git_provider_refused`.
   - A sandbox with repositories but no tokens at all still gets the helper and wrapper, so a token added later works without re-running setup.
4. Tokens are redacted from every output Lewbox keeps, by value, for the life of the process that saw them.

### What's written

All files are written through the toolbox's file API, as root, with the modes shown. **Every token lives under `/run/lewbox/git/`**, on the sandbox's memory-only mount ([sandboxes/profile-setup § where secrets live](../sandboxes/profile-setup.md#where-secrets-live)), so a stop empties it and no commit of the container's disk can include it. What's on disk holds no secret. Files Lewbox writes outside `/root/.config/lewbox/` and `/run/lewbox/` start with the marker `# Written by Lewbox`, and only marked files are replaced or removed.

**This is Git's part of the credential inventory** that [sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot) removes before every snapshot: the rows marked *secret* below, plus `hosts.yml` (written without a token, scrubbed in case `gh` wrote one). Nothing else Git-related holds a credential; user tokens are never written into a sandbox.

| Path | Mode | Content |
|---|---|---|
| `/run/lewbox/git/token` | `0600` | **Secret.** The default token: the narrowest of the default person's own account's, else of the first account by name |
| `/run/lewbox/git/tokens/<account>.<group>` | `0600` | **Secret.** One per token (account and permission group) |
| `/run/lewbox/git/repositories` | `0600` | **Secret** (it names the sandbox's repositories). The map the helper and wrapper read: one line per repository, `owner/name <account>.<group>`, and one per account, `owner/* <account>.<group>` naming its narrowest token |
| `/root/.config/lewbox/git-credential-github` | `0755` | Credential helper ([script](#the-credential-helper)); no secret |
| `/root/.config/gh/hosts.yml` | `0600` | `github.com: {user: <default person's login>, git_protocol: https}`, **without a token**: the wrapper always supplies `GH_TOKEN` |
| `/usr/local/bin/gh` | `0755` | Wrapper, ahead of the image's `gh` in `PATH`; no secret |

And git config (`--global`, in one command):

```
git config --global --replace-all credential.https://github.com.helper '!sh /root/.config/lewbox/git-credential-github'
git config --global credential.https://github.com.useHttpPath true
git config --global url.https://github.com/.insteadOf git@github.com:
git config --global user.name  '<identity name>'     # when there is one
git config --global user.email '<identity email>'
```

Replacing any helper `gh auth setup-git` set, since `gh`'s helper would read `hosts.yml`'s single token. The identity is [github-app § Commit identity](github-app.md#commit-identity); it's written even without a token, and the cleanup before a snapshot leaves it (a name and an email aren't credentials).

The in-sandbox cleanup before a stop that's asked for removes `/run/lewbox/git/`, the helper, the wrapper, `hosts.yml` (if marked) and the helper and rewrite config entries. The node-side scrub before every snapshot removes `/run/lewbox` and `hosts.yml` whatever stopped the sandbox; the helper, wrapper and config entries hold no secret and may stay, and setup rewrites them on start.

#### The credential helper

```sh
#!/bin/sh
# Written by Lewbox: answers git with the GitHub token for the repository, read on every call.
test "$1" = get || exit 0
repo=
while IFS= read -r line; do
  case "$line" in path=*) repo=${line#path=}; repo=${repo%.git} ;; esac
done
repo=$(printf '%s' "$repo" | tr '[:upper:]' '[:lower:]')
dir=/run/lewbox/git
token=$dir/token
case "$repo" in
  '' | *[!a-z0-9._/-]* | */*/*) ;;
  */*)
    name=$(awk -v r="$repo" -v o="${repo%%/*}/*" '$1 == r { print $2; f = 1; exit } $1 == o { d = $2 } END { if (!f && d) print d }' "$dir/repositories" 2>/dev/null)
    case "$name" in '' | *[!a-z0-9.-]*) ;; *) [ -r "$dir/tokens/$name" ] && token=$dir/tokens/$name ;; esac ;;
esac
test -r "$token" || exit 0
echo username=x-access-token
echo "password=$(cat "$token")"
```

The repository's own token, else its owner's narrowest, else the default. A repository the sandbox doesn't list gets the owner's narrowest token, which GitHub refuses for it anyway, since every token is narrowed to the sandbox's repositories.

Ported from legacy `GitSetup::CREDENTIAL_HELPER_SCRIPT` (which chose by owner only), POSIX `sh` and `awk` so it works on any image with `git`.

#### The `gh` wrapper

Ported from `GitSetup::GITHUB_CLI_WRAPPER_SCRIPT`: finds the real `gh` further along `PATH`; runs it unchanged when `GH_TOKEN` or `GITHUB_TOKEN` is set; otherwise finds the **repository** from `-R`/`--repo`/`--repo=`, an `api` path starting `repos/<owner>/<name>/`, or the current directory's `origin` remote, looks it up in `/run/lewbox/git/repositories` as the helper does, and exports `GH_TOKEN` from that token's file: the repository's own, else its owner's narrowest, else the default token (`/run/lewbox/git/token`). With no token at all, `gh` runs unauthenticated, as `hosts.yml` holds none.

### When sandbox tokens are written

| When | What |
|---|---|
| **Profile setup** on create, start and `apply_setup` ([sandboxes/profile-setup](../sandboxes/profile-setup.md)) | The whole Git step: mint, write, configure, check |
| **A refresh is due** (`expires_at − 15 min`) | Mint that account's token again; write its file (and `hosts.yml` if it's the default). No check, no operation |
| **The sandbox's Git setting changes** (`PUT …/git`) | Mint again for the groups affected; rewrite the map; remove files no longer needed |
| **The project's cap changes** | Re-mint running sandboxes it now limits (lowered), or no longer limits (raised), and set `capped` |
| **The profile's GitHub changes** (a person or account linked or unlinked, the default person changed, the App removed) | `apply_setup` on every running sandbox of the profile |
| **`installation.new_permissions_accepted`** | Re-read the installation's permissions; re-mint **at once** for that installation in every running sandbox using it, so `full` and `write` sandboxes get the new permissions without waiting for a refresh ([github-webhooks](../activity-and-webhooks/github-webhooks.md#handling)) |
| **Before every snapshot** (any stop of a persistent sandbox: asked for, idle timeout, lifetime, credits, suspension) | Cleanup: remove every file and entry in [what's written](#whats-written) but the identity; cancel refresh jobs; delete `sandbox_git_tokens` rows ([credentials never enter a snapshot](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)) |

No snapshot holds a Git token, whatever stopped the sandbox: the cleanup runs before the snapshot is taken, including stops the idle timeout or lifetime start, as [sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot) designs.

### Refreshing

```
token written (expires_at = T)
  └─ jobs.Schedule("git.refresh_token", {sandbox, account}, run_at: T − 15m)  ── stored on the row

job runs:
  sandbox not running, or row gone           → nothing
  row's expires_at later than T − 10m        → nothing (another write refreshed it)
  mint; write file via toolbox; update row; schedule next
  toolbox unavailable                        → retry with backoff until expires_at
  GitHub refuses                             → record error; leave the old token to expire; webhook git_connection.failed
```

**Every mint works out the effective permissions afresh** from the sandbox's access and the installation's accepted permissions, so a `full` or `write` sandbox picks up a permission the App gains at its next refresh at the latest, and at once when GitHub reports the installation accepted it.

The job is unique per (`sandbox`, `account`), so double scheduling is harmless. Writing a file is idempotent. No sandbox key, no public API, no `apply_setup` operation: a sandbox busy with another operation (other than `stop` or `delete`) still gets its token.

### The token API

For the project's servers: the token they need to do something on GitHub themselves.

`POST /v0/profile/git/tokens` (profile key) or `POST /v0/projects/{prj}/profiles/{prf}/git/tokens` (admin key or dashboard session).

**Installation tokens:**

```json
{
  "type": "installation",
  "repositories": ["acme/web"],
  "permissions": { "contents": "write", "workflows": "write" },
  "purpose": "push",
  "sandbox": "sbx_Qm4pL0sX2bN8cV"
}
```

| Field | Type | Description |
|---|---|---|
| `provider` | string | The Git provider, e.g. `github`. Optional: defaults to the only Git provider on for the project; with several on and none named, `422 validation_failed` on `provider`. The answer always says which. Routes and errors are provider-neutral ([plugins/git-providers](../plugins/git-providers.md)) |
| `type` | string | `installation` (default) or `user` |
| `repositories` | array | Required for `installation`: 1 to 100 GitHub repositories as `owner/name`. They may be on several accounts; one token is issued per account |
| `permissions` | object | Required for `installation`: permission names to `read` or `write`, as GitHub takes them. Each must be one the App has and the installation accepted |
| `purpose` | string | Optional label for the activity log, e.g. `push`, `pull_request` (the same rules as a [command's label](../inside-a-sandbox/commands.md)) |
| `sandbox` | string | Optional: the sandbox it's for, so the issue shows in that sandbox's activity too. It doesn't change what the token reaches |

Answers `201`:

```json
{
  "data": {
    "provider": "github",
    "type": "installation",
    "tokens": [
      {
        "token": "ghs_…",
        "expires_at": "2026-10-29T13:00:00Z",
        "account": "acme",
        "installation_id": 51234567,
        "repositories": ["acme/web"],
        "permissions": { "contents": "write", "metadata": "read", "workflows": "write" },
        "reused": false
      }
    ]
  }
}
```

- One token per account named, each narrowed to that account's repositories. If any account has no linked installation, nothing is issued: `409 git_not_connected` with `accounts` naming them.
- **Reuse:** an installation token for the same installation, repositories and permissions, issued to the same profile and with more than 10 minutes left, is returned again (`reused: true`, same `expires_at`) instead of minting another. GitHub has no documented cap on minting, but repeated mints are wasteful and every token is another live credential; reuse keeps one per scope. The cached token is encrypted in `github_issued_tokens.token` and nulled when it's no longer reusable. `"fresh": true` asks for a new one anyway.
- Installation tokens last an hour (GitHub's fixed lifetime). Callers ask again when they need one.

**User tokens:**

```json
{ "type": "user", "person": "harrisonclewis", "purpose": "pull_request" }
```

| Field | Type | Description |
|---|---|---|
| `person` | string | The GitHub login of one of the profile's people. **Required when the profile holds more than one person** (`422 validation_failed` on `person` without it), so a caller never acts as the wrong one by accident; may be left out when it holds one |

Answers `201` with `{type: "user", tokens: [{token: "ghu_…", expires_at, login, scopes: null}]}`: that person's user token, refreshed first if it has less than 15 minutes left. A login the profile doesn't link is `404 not_found`. See [user tokens](#user-tokens).

**Who may:**

| Key | Installation tokens | User tokens |
|---|---|---|
| Admin key | ✓ (any profile in its project) | ✓ |
| Dashboard session | ✓, for project owners and admins | ✓, for project owners and admins |
| Profile key | With `git.tokens`, for its own profile | **Never**: `403 wrong_key_kind` |
| Sandbox key | `403 wrong_key_kind` | `403 wrong_key_kind` |
| Setup session | `403` | `403` |

New profile keys don't get `git.tokens` unless asked for ([profiles/profile-keys](../profiles/profile-keys.md)), so a key made for a dashboard or a job runner can't mint write tokens. **User tokens are for the project's own servers and its dashboard only** (decided 2026-10-29): a profile key is often handed to a customer's own systems, and a user token can't be narrowed.

**Limits:** 120 issues a minute per profile (`429 rate_limited`, `Retry-After`), reused tokens included. Installation tokens count against the App's GitHub rate limit per installation, which Lewbox reports in `lewbox_github_rate_limit_remaining`.

#### User tokens

A user token is a person's: what it does on GitHub is done as them, and it reaches every repository the person and the App both reach. GitHub can't narrow it to repositories or permissions, so:

- **Only admin keys and dashboard sessions** (project owners and admins) can get one. An admin key with a permission list needs `git.tokens.user`; profile keys can't hold it at all ([api/permissions](../api/permissions.md)).
- **It names a person** (`person`), one of the profile's people ([github-app](github-app.md#people-and-accounts)).
- It's the same token for every caller until it's refreshed (GitHub issues one per authorization, and Lewbox keeps one per person per project), so Lewbox can't revoke it alone; revoking means the person revokes the App's authorization, or the project unlinks the person everywhere (which deletes Lewbox's copy and revokes the grant).
- Each issue is in the activity log with the actor, the person and the purpose.
- Use it for what must be done as the person: opening and merging pull requests "as you", replying to reviews, creating repositories. Use installation tokens for everything else ([integrating](integrating.md)).

### Revoking

- **Unlink an account or a person, disconnect, App removed, installation deleted**: the affected token files are removed from running sandboxes at once, their refresh jobs cancelled, and cached issued tokens for them deleted. Installation tokens already written or handed out stay valid until they expire (GitHub's only revoke, `DELETE /installation/token`, must be called with the token itself; Lewbox calls it for tokens it still holds, cached or in memory).
- **Profile suspended**: no tokens are issued (`403 profile_suspended`); sandbox tokens are removed as the sandboxes stop ([profiles](../profiles.md)).
- **A sandbox's access lowered** (by its setting or the project's cap): re-minted at once at the new level, and the old token file replaced; a copy of the old token taken by something in the sandbox lapses within the hour.

## API

| Method and path | Who | Does |
|---|---|---|
| `GET /v0/sandboxes/{sbx}/git` | any key that sees the sandbox | The sandbox's Git setting and token status |
| `PUT /v0/sandboxes/{sbx}/git` | `sandboxes.git` | Change the sandbox's access and repositories, each repository's own access, within the project's cap |
| `POST /v0/profile/git/tokens` | profile key with `git.tokens` | Issue an installation token (user tokens: `403 wrong_key_kind`) |
| `POST /v0/projects/{prj}/profiles/{prf}/git/tokens` | admin key (`git.tokens`, `git.tokens.user`), owner or admin session | Issue an installation token, or a named person's user token |
| `GET /v0/projects/{prj}/profiles/{prf}/git/tokens` | admin key, session | Tokens issued in the last 30 days, without the tokens |

### Errors

| Status | Code | When |
|---|---|---|
| 403 | `wrong_key_kind` | A sandbox key or setup session asking for any token, or a profile key asking for a user token |
| 403 | `permission_denied` | Without `git.tokens`, `git.tokens.user` (admin keys with a list), or `sandboxes.git` |
| 404 | `not_found` | A `person` the profile doesn't link |
| 403 | `profile_suspended` | The profile is suspended |
| 404 | `repository_not_found` | A repository its account's installation doesn't reach (GitHub's `422`), named in the message |
| 409 | `git_not_connected` | No connection, or no linked installation on an account named. Carries `accounts` |
| 409 | `git_person_not_authorized` | A user token, when the person hasn't authorized the App or the authorization lapsed |
| 422 | `validation_failed` | No `person` for a user token on a profile with several people, bad repository names, over 100, an unknown preset, permission or level, a custom permission the App or installation doesn't have, or access above the project's cap (`git.access`, `git.repositories.N.access`, naming the cap) |
| 429 | `rate_limited` | Over 120 a minute |
| 502 | `git_provider_refused` | The installation is suspended or gone, the App's key refused |
| 502 | `git_provider_unavailable` | GitHub couldn't be reached, or rate-limited the App |
| 503 | `toolbox_unavailable` | A sandbox setting change couldn't be written yet (it's kept) |

The sandbox's `setup.steps` shows the Git step's status (`ready`, `configured`, `failed`, `not_configured`, `not_applicable`), with `message` naming accounts whose tokens failed.

## Security

- **The default is `full`, and that's a real grant.** Anything running in a sandbox (the agent, a package it installed, a page that injected a prompt) can read the token files, so with the default it can push to the sandbox's repositories, open and merge pull requests, and with `workflows: write` change their CI, for up to an hour per token, refreshed for as long as the sandbox runs. That's the user's choice to make, so Lewbox makes it visible rather than refusing it.
- **What limits it:**
  - tokens reach **only the sandbox's own repositories**, never the rest of the installation;
  - they're short-lived (an hour), refreshed only while the sandbox runs, removed before every snapshot whatever stopped the sandbox ([credentials never enter a snapshot](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)), and gone with it on delete;
  - every mint is in the activity log, with the permissions;
  - per-repository `access`, so a sandbox can push to one repository and only read another;
  - the project's cap, which no key can exceed.
- **For untrusted work, use `read`** and push from your own servers with an issued token ([integrating](integrating.md)). A project that promises its users nothing in a sandbox can push sets its cap to `read`.
- **User tokens never reach sandboxes or profile keys.** Only admin keys and the dashboard, for a named person.
- **Narrowed always.** Sandbox tokens to the sandbox's repositories per account; issued installation tokens to the repositories named. Never "all repositories".
- **Tokens never in env, remote URLs, command lines** (files are written through the toolbox's file API, not `echo`), logs, activity or webhooks. Issued tokens appear once, in the response.
- **Anything in the sandbox can read its token files.** By design: the tokens are only as strong as the sandbox's own Git setting.
- **The dashboard shows the access** on every sandbox and in the create dialog, and warns when it's `write` or `full` ([dashboard/sandbox-pages](../dashboard/sandbox-pages.md)).
- **Issued tokens leave Lewbox.** What the project does with them is the project's; [integrating](integrating.md#security) says what not to do, first of all: never write an issued write token into a sandbox.
- Scripts validate the owner (`[a-z0-9-]` only) before using it in a path.

## Observability

- Sandbox activity: `git.token_written` (`details: {accounts, permissions, reason: setup|refresh|setting}`), `git.token_failed`, `git.repositories_changed`, `git.access_changed` (with the effective permissions per repository). Actor `lewbox` for refreshes.
- Profile activity: `github.token_issued` (`details: {type, accounts, repositories, permissions, purpose, reused, token_id}`), also on the named sandbox's log.
- Metrics: `lewbox_git_token_mint_total{holder=sandbox|issued,outcome}`, `lewbox_git_token_refresh_lag_seconds`, `lewbox_github_tokens_issued_total{type,reused}`.
- An alert when refreshes fail repeatedly for an installation.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_GIT_TOKEN_REFRESH_BEFORE` | `15m` | |
| `LEWBOX_GIT_MAX_SANDBOX_REPOSITORIES` | `100` | Per sandbox |
| `LEWBOX_GIT_MAX_REPOSITORIES` | `20` | Cloned per create ([sandboxes/repositories](../sandboxes/repositories.md)) |
| `LEWBOX_GITHUB_TOKENS_PER_MINUTE` | `120` | Token API, per profile |
| `LEWBOX_GITHUB_ISSUED_TOKENS_RETENTION` | `30d` | |

## Testing

- Shell tests for the helper and wrapper (`internal/git/sandboxfiles/*_test.sh`, run by Go tests in a container with `sh`, `git` and a fake `gh`): repositories from paths, `-R`, `--repo=`, `repos/` paths, `origin`; the map's repository and owner lines; invalid names; missing files; `GH_TOKEN` set. `shellcheck` in CI.
- Minting by owner, partial failures, `422` handling, owners without installations.
- Access: presets resolved against an App with and without `workflows`; per-repository overrides; grouping by (owner, permissions) into separate tokens; the helper and wrapper picking a repository's own token, then its owner's narrowest.
- The cap: none by default; above the cap refused on create and change; lowering the cap re-mints down to it and sets `capped`; raising it restores the sandbox's own setting.
- The token API: one token per account, `git_not_connected` naming accounts, reuse within the window and not across profiles, `fresh`, permissions the App lacks, user tokens refreshed under a lock, rate limit.
- Refresh scheduling: job at the right time, skipped when superseded, cancelled on stop.
- Snapshot cleanup: no token file or git config entry survives any stop of a persistent sandbox, asked for, idle or lifetime (the fake toolbox inspects files at snapshot time).
- New App permissions: `installation.new_permissions_accepted` re-mints running `full` and `write` sandboxes at once; a refresh picks them up without the webhook; custom sets are unchanged.
- User tokens: `person` required with several people, refused for profile keys, for a login the profile doesn't link, and for a person whose authorization lapsed.

## Build plan

Core (`internal/git`) owns the setting, the files, refresh and the token API; it asks the Git provider plugin for each repository's provider and for the token itself ([plugins/git-providers](../plugins/git-providers.md)). The GitHub specifics in this plan (installation tokens, permission names, the `access` presets' GitHub mapping) are the `git/github` plugin's.

1. Tables and queries.
2. `internal/git/sandboxfiles`: embedded scripts and templates, with tests.
3. Access presets and resolution, `MintSandboxTokens` with grouping, the Git setup step (with [sandboxes/profile-setup](../sandboxes/profile-setup.md)), the sandbox's Git setting and the project's cap.
4. Refresh jobs; revocation hooks.
5. The token API, reuse, activity, rate limit.

## Open questions

1. ~~**Default token.**~~ Decided: the narrowest token for the profile's default person's own account, else the first account by name ([what's written](#whats-written), [github-app](github-app.md#people-and-accounts)).
2. **Reuse across callers**: reuse is per profile; should two admin-key callers asking for the same scope get the same token, or should each caller get its own so revoking one doesn't affect the other? Proposed: per profile, `fresh: true` for callers that want their own.
3. **Adding repositories from inside the sandbox**: should agentd be able to ask for one (e.g. through an MCP tool), with the person approving? Later.
4. ~~**Write tokens in snapshots.**~~ Decided (2026-10-29): every credential Lewbox put into a sandbox is removed before any snapshot, Git's included ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)).
5. ~~**Presets that grow.**~~ Decided (2026-10-29): running `full` and `write` sandboxes get new permissions at once when the installation accepts them, and at their next refresh at the latest ([refreshing](#refreshing)).
