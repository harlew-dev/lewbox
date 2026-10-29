# Integrating with Git and GitHub

> Status: Draft · Milestone: Alpha (M4), documentation and examples · Updated: 2026-10-29

Lewbox clones repositories into sandboxes, keeps narrowed tokens in them, and **issues tokens** to the project's servers ([tokens](tokens.md)). It doesn't push, open pull requests, review or create repositories: `git` and GitHub's API already do those, and a project holding a token can call them directly. This plan is the guide for doing so, with the recipes legacy Lewbox used to run itself, so a project (Lewagent first) can keep every feature it had, on its own servers.

It's a plan for documentation (`docs/guides/git.md` on lewbox.com), examples in Go and TypeScript, and an end-to-end test that keeps the push recipe working. No new API.

---

## Today

Legacy Lewbox did these itself, and Lewagent's Git panel calls them with the tenant key:

| What | Legacy Lewbox | Lewagent caller |
|---|---|---|
| Pushes | `app/Actions/Pushes/{PackPush,UploadPush,VerifyPush,UpdatePushBranch,CleanUpPush}.php`, `app/Actions/GitHub/PushGitHub.php` (`receivePack`: `git-receive-pack` spoken in PHP), `app/Jobs/Pushes/RunPush.php`, `app/Console/Commands/Push/CleanUpPushes.php`, `app/Enums/Push/PushStatus.php`; `pushes` table | `app/Http/Controllers/Computer/PushController.php` (`index(active)`, `store`, `show`, `confirm(allow_workflows)`, `destroy`) |
| Pull requests and reviews | `app/Actions/GitHub/GitHubPullRequests.php` (719 lines: REST and GraphQL) | `app/Http/Controllers/Computer/Repository/PullRequestController.php` (`store`, `update`, `merge`, `files`, `viewed`, `threads`, `comment`, `addToReview`, `submitReview`, `discardReview`, `reply`, `converse`, `resolve`) |
| Repository creation and owners | `app/Actions/GitHub/CreateGitHubRepository.php`, `ListGitHubRepositoryOwners.php` | `CreateGitHubRepository` action, `OwnerController` |
| Narrowing a computer's token | `PUT /api/v1/sandboxes/{sbx}/git/repositories` | `GrantGitRepositories`, before every start |

Why it moves out: each was Lewbox re-implementing Git or wrapping GitHub one call at a time ([git](../git.md#today)), and each new need (comment as the App, a pull request by number, a wiki) meant a new Lewbox endpoint. Lewagent is PHP and Laravel like legacy Lewbox, so this code moves into Lewagent nearly as it is ([below](#lewagent)).

## Goals

- Every Git feature legacy offered is doable by a project with Lewbox's API and an issued token, and the docs show how.
- The push recipe keeps the guarantees Lewagent sells: nothing in the sandbox can push (its project caps sandbox access at `read`); what's pushed is what the person reviewed; workflow changes are confirmed.
- A project that's happy for agents to push from the sandbox doesn't need these recipes: sandbox tokens are `full` by default ([tokens](tokens.md#access-levels)). They're for pushing from your own servers, when the sandbox mustn't hold a token that writes.
- One example per recipe, in Go and TypeScript, run in CI against the fake GitHub.

### Non-goals

- A Lewbox library for pushes or pull requests (an [open question](../git.md#open-questions)).
- Covering every GitHub API. The recipes cover what legacy did; GitHub's docs cover the rest.

## Design

### What a project has to work with

| Need | From Lewbox |
|---|---|
| Run `git` in the sandbox (status, diff, log, bundle, pack) | `POST /v0/sandboxes/{sbx}/commands` ([inside-a-sandbox/commands](../inside-a-sandbox/commands.md)) |
| Read a file out of the sandbox, streamed (a bundle, a pack) | `GET /v0/sandboxes/{sbx}/files?path=…` ([inside-a-sandbox/files](../inside-a-sandbox/files.md)) |
| A token to push, or to call GitHub as the App | `POST …/git/tokens` `{type: "installation", repositories, permissions}` |
| A token to act as a person | `POST /v0/projects/{prj}/profiles/{prf}/git/tokens` `{type: "user", person: "<login>"}`, with an admin key (profile keys can't get user tokens) |
| More repositories reachable from the sandbox | `PUT /v0/sandboxes/{sbx}/git` |
| GitHub's events | Webhook endpoints subscribed to `github.*` ([github-webhooks](../activity-and-webhooks/github-webhooks.md#forwarding)) |
| A record of what was done | `POST /v0/sandboxes/{sbx}/activity` app events, e.g. `app.git.pushed` ([activity-log](../activity-and-webhooks/activity-log.md#app-events)) |

### Pushing from your servers

The sandbox's token only reads (unless the project raised its cap), so the agent commits and the project's server pushes. Legacy's four steps, done by the project:

```
 project server                                   sandbox (via Lewbox)                GitHub
 ──────────────                                   ────────────────────                ──────
 1. read GitHub's branch (installation token,
    contents: read) → base, or the default
    branch's head for a new branch
 2. POST commands: check + bundle  ───────────▶   git rev-parse; LFS check;
                                                  git bundle create /tmp/p.bundle
                                                  <base>..<expected_head>
 3. GET files /tmp/p.bundle (stream) ◀───────────
 4. token: contents: write (+ workflows: write
    if confirmed), the one repository
 5. git fetch bundle into a scratch bare repo;
    git push to refs/<you>/pushes/<id>  ──────────────────────────────────────────────▶ ref created
 6. verify: GitHub's compare base...head matches
    the sandbox's account (from step 2)  ◀───────────────────────────────────────────── compare
 7. move the branch: PATCH git/refs/heads/<b>
    force: false (or POST git/refs)  ─────────────────────────────────────────────────▶ branch moved
 8. delete the scratch ref; POST commands:
    git fetch; rm /tmp/p.bundle  ─────────────▶   tracking updated
```

**Step 2, in the sandbox**, one command (with `label: "git.push.pack"`), exiting with distinct codes the server maps to errors:

```sh
set -e
cd "$CHECKOUT"
test "$(git rev-parse HEAD)" = "$EXPECTED_HEAD" || exit 10          # head_moved
git lfs ls-files 2>/dev/null | grep -q . && exit 11                  # lfs_not_supported
if [ -n "$BASE" ]; then
  git cat-file -e "$BASE^{commit}" 2>/dev/null || git fetch --quiet origin "$BASE"
  git merge-base --is-ancestor "$BASE" "$EXPECTED_HEAD" || exit 12   # not_fast_forward
  range="$BASE..$EXPECTED_HEAD"
else
  range="$EXPECTED_HEAD"
fi
git diff --raw -z --no-renames "${BASE:-$(git hash-object -t tree /dev/null)}" "$EXPECTED_HEAD" > /tmp/p.diff
git diff --name-only "${BASE:-$(git hash-object -t tree /dev/null)}" "$EXPECTED_HEAD" | grep -q '^\.github/workflows/' && touch /tmp/p.workflows
git bundle create /tmp/p.bundle "$range"
```

Variables go in the command's `env`, never interpolated into the command, so a branch name can't inject shell ([inside-a-sandbox/commands](../inside-a-sandbox/commands.md)). The sandbox's token is enough to fetch `BASE`.

**A bundle, not a raw pack.** Legacy streamed a thin pack straight into `git-receive-pack`, which meant speaking Git's wire protocol in PHP. A bundle is a pack with its prerequisites named; on the server, `git fetch` from it into a scratch bare repository (which first fetches `BASE` from GitHub, shallow, with the write token) and `git push` with ordinary `git`. Any language with `git` installed does this in a dozen lines. A project that wants no `git` on its servers can stream the pack to `git-receive-pack` as legacy did ([Lewagent](#lewagent)).

**Workflows.** A change under `.github/workflows/` needs `workflows: write` on the token, and in legacy needed the person to confirm (`needs_confirmation`, reason `workflows`). Ask for `workflows: write` only after the person confirms.

**Verifying.** Push to a ref of your own (`refs/<you>/pushes/<id>`, which nothing on GitHub reacts to), then compare GitHub's `GET /repos/{r}/compare/{base}...{head}` file list and blob ids against the sandbox's `git diff --raw` from step 2. A difference means the sandbox's account didn't match what it bundled: ask the person (legacy's `mismatch`). Past 300 files (GitHub's compare cap), compare trees instead. Then move the branch with `force: false`, so a branch that moved on GitHub meanwhile is refused, not overwritten.

**Errors to expect**, as legacy named them: `head_moved` (exit 10), `lfs_not_supported` (11), `not_fast_forward` (12, or GitHub refusing the ref update), `pack_too_large` (legacy's cap: 1 GB; GitHub refuses pushes over 2 GB), `git_not_connected` / `git_provider_refused` from the token API, `stalled` (your own timeout; legacy used 45 minutes).

### Pull requests and reviews

With a **user token** to act as one of the profile's people ("as you, not a bot"; name them with `person`), or an **installation token** with `pull_requests: write` to act as the App (`<slug>[bot]`). Say which in your product; legacy silently fell back to the App, which made "you commented" sometimes untrue.

| Do | GitHub |
|---|---|
| Open (as a draft) | `POST /repos/{r}/pulls` `{title, body, head, base, draft: true}` |
| Edit, close | `PATCH /repos/{r}/pulls/{n}` |
| Mark ready | GraphQL `markPullRequestReadyForReview` |
| Merge | `PUT /repos/{r}/pulls/{n}/merge` `{merge_method, sha}`; delete the branch with `DELETE /repos/{r}/git/refs/heads/{b}` |
| Files, viewed state | `GET /repos/{r}/pulls/{n}/files`; GraphQL `viewerViewedState`, `markFileAsViewed`, `unmarkFileAsViewed` |
| Review threads | GraphQL `reviewThreads` with comments; `resolveReviewThread`, `unresolveReviewThread` |
| Comment on a line, reply | `POST /repos/{r}/pulls/{n}/comments`, `…/comments/{c}/replies` |
| Conversation comment, edit it | `POST /repos/{r}/issues/{n}/comments`, `PATCH /repos/{r}/issues/comments/{id}` |
| Pending review | GraphQL `addPullRequestReview`, `addPullRequestReviewThread`, `submitPullRequestReview`, `deletePullRequestReview` |
| A pull request by number, with checks | `GET /repos/{r}/pulls/{n}`; `GET /repos/{r}/commits/{sha}/check-runs`, `…/status` |

These are the calls legacy's `GitHubPullRequests.php` made; its queries can be lifted as they are.

### Creating a repository

With the person's **user token**: `POST /user/repos`, or `POST /orgs/{org}/repos` (the App has `administration: write`, so a user token can create in organizations where the person may). If the account's installation has selected repositories, the new one isn't reachable yet: add it with `PUT /user/installations/{installation_id}/repositories/{repository_id}` (user token; the person must be an admin of the repository, which its creator is), then add it to the sandbox with `PUT /v0/sandboxes/{sbx}/git`. The accounts to offer as owners: `GET /user/installations` with the user token gives the App's installations the person reaches; the profile's connection lists those already linked.

### Starting a sandbox from a pull request

Read it (`GET /repos/{r}/pulls/{n}` with an installation token, `pull_requests: read`), then create the sandbox with `repositories: [{name: head.repository, ref: head.ref}]`. For a fork, the head repository must be reachable by the profile's installations or public.

### Reacting to GitHub's events

Subscribe a webhook endpoint to `github.pull_request` (or `github.*`), verify the signature as for any Lewbox webhook ([webhooks](../activity-and-webhooks/webhooks.md)), deduplicate by `data.delivery`, and read GitHub's payload in `data.payload`; `data.profiles` says which of the project's profiles reach the repository. Lewagent's case: `github.pull_request` with `action: closed` and `merged: true` finishes a branch's setup.

### Security

- **Never write an issued write token into a sandbox**: not in a file, an env var, a command, or a remote URL. Anything there can read it. If agents should push from the sandbox, give the sandbox (or the repository) `write` or `full` access instead, so Lewbox narrows, refreshes and cleans the token ([tokens](tokens.md#the-sandboxs-git-setting)).
- **Ask for the least**: one repository, `contents: write` for a push, `workflows: write` only after confirmation, `pull_requests: write` for pull requests. Tokens last an hour; ask again rather than keep one.
- **Keep user tokens on servers.** They reach everything the person reaches with the App. Never send one to a browser or a sandbox. Lewbox only issues them to admin keys and the dashboard for this reason.
- **Verify what you push** against what the person reviewed, and rely on GitHub's branch protection for what goes further: a taken-over sandbox can lie consistently about what it packs and what it shows (legacy's push plan said the same).
- **Pass values to sandbox commands as `env`**, never interpolated.
- Record what you did as app events on the sandbox (`app.git.pushed`, `app.pull_request.opened`), so the activity log tells the whole story.

### Lewagent

Lewagent keeps every Git feature by moving legacy Lewbox's code into itself, against the new API:

| Lewagent | Moves in from legacy Lewbox | Changes |
|---|---|---|
| Pushes (`PushController`, a new `pushes` table and job) | `app/Actions/Pushes/*`, `PushGitHub.php`, `RunPush.php`, `CleanUpPushes.php`, `PushStatus.php` | The sandbox's commands and files through the Lewbox PHP SDK; `IssueGitHubToken::forPush` → `POST …/git/tokens` with `contents: write` (+ `workflows: write` after confirmation); the pack stream read from Lewbox's files API. Keeping `receivePack` as it is works; switching to the bundle recipe drops it |
| Pull requests (`PullRequestController`) | `GitHubPullRequests.php` | User tokens from `POST …/git/tokens {type: user}`; installation tokens for `as: app`; no Lewbox routes |
| Repository creation and owners (`CreateGitHubRepository`, `OwnerController`) | `CreateGitHubRepository.php`, `ListGitHubRepositoryOwners.php` | User tokens |
| Narrowing a computer's token (`GrantGitRepositories`) | — | `PUT /v0/sandboxes/{sbx}/git` with `repositories` |
| "Nothing on the computer can push" | — | Lewagent creates computers with `git: {access: "read"}` and sets its project's cap, `sandbox_access_max`, to `read`, so no key can give a computer a write token. Without both it would get Lewbox's default, `full` |
| Merged-on-GitHub detection | — | A webhook endpoint subscribed to `github.pull_request` |

Lewagent's backend uses its admin key for tokens: installation tokens, and user tokens for the person (`person`) a pull request is opened as, which profile keys can't get; see [migration/lewagent](../migration/lewagent.md). Since none of this waits on a Lewbox Beta, Lewagent can move once the alpha's token API ships.

## Testing

- `examples/git/push` (Go) and `sdks/typescript/examples/push.ts`: the push recipe end to end against the fake toolbox and the fake GitHub, which serves Git's smart HTTP with `git http-backend` over a real bare repository, run in CI.
- The pull request table's calls against the fake GitHub's REST and GraphQL handlers that exist for the contract tests; recorded against real GitHub nightly.

## Build plan

1. `docs/guides/git.md`: this guide, with the recipes.
2. The Go and TypeScript examples, and the CI test.
3. Smart HTTP in the fake GitHub (`git http-backend`).

## Open questions

1. **Bundle or pack in the docs?** The bundle recipe needs `git` on the project's servers; the pack recipe needs a `git-receive-pack` client. Proposed: bundle in the docs, pack mentioned for those porting legacy.
2. **A helper library** (`lewbox-git` for Go and TypeScript) for the push recipe, if more projects than Lewagent want it ([git](../git.md#open-questions)).
