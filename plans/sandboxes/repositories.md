# Repositories

> Status: Draft · Milestone: Alpha (M3) · Updated: 2026-10-29

A create can name Git repositories to clone into `/root/workspace` before the sandbox is handed back, so the first command, or the first agent prompt, finds the code in place. This plan covers the `repositories` field, how each entry is resolved and validated, how and when they're cloned, the shared time budget, the tokens used, and the per-repository report. Git tokens themselves (what they reach, how they're minted and refreshed) are [git/tokens](../git/tokens.md).

## Today

- **Resolving the field.** `POST /v1/sandboxes` takes `repositories`, up to 20 (`lewbox.git.max_repositories`). The `Repository` rule validates them (`lewbox-legacy/app/Rules/Repository.php`), and `ResolveSandboxRepositories` resolves them (`app/Actions/Sandboxes/ResolveSandboxRepositories.php`). Each entry is:
  - `owner/name`;
  - a URL;
  - or `{name | url, ref, branch, path}`.
- **URLs** are read by `ParseRepositoryUrl` (`…/ParseRepositoryUrl.php`):
  - GitHub's (https or `git@github.com:`) become `owner/name`;
  - other hosts' are https only, with no password, query or fragment, and named by where they are (`gitlab.com/acme/api`);
  - SSH is accepted only for GitHub.
- **Paths:** a missing path defaults to the repository's name, or `owner-name` when two share a name. The same repository twice, or two in one path, is `422`.
- **Cloning** is a step of the create operation, after setup, so git has the token (`lewbox-gateway-legacy/internal/operations/repositories.go`):
  - one at a time;
  - `git clone`, then `git checkout <ref>`, then `git checkout -b <branch>`;
  - with `GIT_TERMINAL_PROMPT=0`;
  - all within `GATEWAY_CLONE_TIMEOUT` (5 minutes); any not cloned in time are `failed`;
  - git's output redacted;
  - each result reported with `PUT sandboxes/{sbx}/repositories`;
  - a resumed create clones only those still `pending`.
- **A failed clone never fails the create.**
- **The App's token** is narrowed to the sandbox's repositories: those cloned on create, plus `git_repositories` granted later with `PUT /v1/sandboxes/{sbx}/git/repositories` (`lewbox-legacy/docs/profiles.md` § App tokens).
- **Lewagent** sends `repositories` with `name`/`url`/`ref`/`branch`/`path` on every computer create (`lewagent-legacy/app/Actions/Computers/CreateComputer.php`), and caps them at 20, "Lewbox's own limit".

## Goals

- A create returns with every repository it named cloned, or with a clear reason why not.
- Private GitHub repositories work with the profile's connection; public ones with or without it; public repositories on other hosts over https.
- No token ever lands in a remote URL, a command line or the clone's `.git/config`.
- One slow or huge repository can't hold a create for long.

**Non-goals:** private repositories on other hosts (GitLab, Bitbucket) and GitHub Enterprise, which belong to [git](../git.md) later; submodule credentials beyond GitHub; LFS (cloned without LFS objects; see open questions).

## Keep, change, drop

| | |
|---|---|
| **Keep** | The field's shapes; URL parsing rules; default paths; 20 at most; clone after setup; `GIT_TERMINAL_PROMPT=0`; the shared 5-minute budget; per-repository statuses; never failing the create; redaction; the remote is the plain https URL |
| **Change** | Clone **in parallel**, up to 4 at once, within the shared budget (legacy was serial). Use `--filter=blob:none` partial clones by default, with `depth` an option. The report has `commit` (the checked-out SHA). Cloning runs as one script per repository through the toolbox, with exit status written by the script. Granting repositories later lives in [git](../git.md) |
| **Drop** | The internal `PUT sandboxes/{sbx}/repositories` call (now a local write) |

## Design

### The field

`repositories`: an array of up to `LEWBOX_SANDBOX_MAX_REPOSITORIES` (20) entries, each one of:

| Shape | Example |
|---|---|
| A string `owner/name` | `"acme/api"` |
| A string URL | `"https://github.com/acme/api"`, `"git@github.com:acme/api.git"`, `"https://gitlab.com/acme/api.git"` |
| An object | `{"name": "acme/api", "ref": "main", "branch": "feature/login", "path": "api", "depth": 1, "access": "read"}` |

Object fields:

| Field | Type | Rules |
|---|---|---|
| `name` | string | GitHub `owner/name`. Owner and name `[A-Za-z0-9._-]+`, never `.` or `..`. Give `name` or `url` |
| `url` | string | https URL, no userinfo, query or fragment, a path of plain segments; or `git@github.com:owner/name(.git)` |
| `ref` | string | Branch, tag or commit. Must not start with `-`. Default: the repository's default branch |
| `branch` | string | A new branch to make from `ref` and check out. Must pass `git check-ref-format --branch` rules. Optional |
| `path` | string | One directory name under `/root/workspace` (no `/`, not `.`, `..` or `.worktrees`). Default below |
| `depth` | integer | **New.** Shallow clone depth, 1 to 1000. Default: none (full history, partial blobs) |
| `access` | string or object | **New.** GitHub repositories only: the sandbox's token access to this repository, `read`, `write`, `full` or a permission set. Shorthand for listing it in the sandbox's `git.repositories` with that access ([git/tokens](../git/tokens.md#access-levels)). Default: the sandbox's `git.access`, itself `full` by default |

**Resolution** (`sandboxes.ResolveRepositories`, at create, in the transaction):

- A GitHub URL becomes `name: owner/name`, `url: null`.
- Any other URL keeps its `url`, and is named `host/path` without `.git`.
- **Path default:** the repository's name; when two share a name, `owner-name` for each of them.
- The same repository twice, or two in one path, is `422 validation_failed` on `repositories.N`.
- Each entry gets `status: "pending"`.
- Every GitHub repository cloned is added to the sandbox's Git setting (`source: create`), with its `access` if it has one. `access` on a non-GitHub URL is `422 validation_failed`: those clone without a token.

### Cloning

A step of the `create` operation, after `setup_applied` ([operations](operations.md#create)):

1. **Grant first.** The sandbox's token is minted for these repositories, plus any in its `git.repositories` setting ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)), during setup, which runs just before. A repository the connection can't reach clones only if it's public.
2. **Clone in parallel**, up to `LEWBOX_CLONE_CONCURRENCY` (4), each as one script run through the toolbox:

   ```sh
   set -eu
   export GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=/bin/false
   cd /root/workspace
   git clone --filter=blob:none [--depth N] --no-checkout -- "$URL" "$DIR"
   cd "$DIR"
   git checkout ${REF:+"$REF"} --            # the default branch when no ref
   [ -n "$BRANCH" ] && git checkout -b "$BRANCH"
   git rev-parse HEAD                         # → the report's commit
   ```

   - The URL is `https://github.com/<owner>/<name>.git` for GitHub (the credential helper supplies the token), or the given URL.
   - Arguments are passed through environment variables, never interpolated into the script.
   - `--filter=blob:none` makes clones of large repositories much faster and fetches blobs on checkout. A server that doesn't support partial clone falls back to a full one, which git does automatically.
   - With a `ref` that's a commit, `--filter` plus checkout fetches what's needed.
3. **Budget:** all clones share `LEWBOX_CLONE_TIMEOUT` (5 min) from the step's start. When it runs out, running clones are killed (the toolbox command's timeout), the directory is removed, and every repository not `cloned` is marked `failed` with "Not cloned within 5 minutes."
4. **Report** each result as it finishes, writing `sandboxes.repositories` (a JSON array) in its own small transaction. So `GET /v0/sandboxes/{sbx}` shows progress during a `202` create.
5. **Resuming** after a crash: entries still `pending` or `cloning` are retried; a `cloning` one has its directory removed first.
6. The step `repositories_cloned` is recorded when all are done, cloned or failed.

### The report

On the sandbox, `repositories`:

| Field | Notes |
|---|---|
| `name` | `owner/name`, or `host/path` |
| `url` | Null for GitHub |
| `ref`, `branch`, `path` | As resolved |
| `directory` | `/root/workspace/<path>` |
| `status` | `pending`, `cloning` (new), `cloned`, `failed` |
| `commit` | **New.** The checked-out SHA, once cloned |
| `message` | Git's error for `failed`: the last 2 KiB, redacted |

A webhook `sandbox.repositories_cloned` (new) carries the report once the step ends, for consumers who created with `wait: false`.

### After create

- Later clones are plain `git clone` commands; the credential helper authenticates them. A private repository must be in the sandbox's Git setting first (`PUT /v0/sandboxes/{sbx}/git`, [git/tokens](../git/tokens.md#the-sandboxs-git-setting)).
- Clones live on the sandbox's disk, so they're snapshotted and resumed with it; starting a sandbox doesn't clone again.
- Lewagent's "clone and build" for repositories added to a project after the computer was made uses commands, as it does today ([migration/lewagent](../migration/lewagent.md)).

## API

The `repositories` field on `POST /v0/sandboxes`, and the report on the sandbox object. Errors: `422 validation_failed` for any entry, with field paths like `repositories.2.ref`.

To let users choose what to clone, profiles list their repositories and branches: `GET /v0/profile/git/repositories` and `…/{owner}/{repo}/branches` ([git/github-app](../git/github-app.md#repositories-and-branches)).

## Security

- Remotes are plain URLs; tokens come only from the credential helper reading a 0600 file ([git/tokens](../git/tokens.md#whats-written)).
- Non-GitHub URLs never get a token: the helper is scoped to `https://github.com`.
- A cloned repository's token has the access the user chose, `full` by default, so anything in the sandbox can push to it unless `access` (or the project's cap) says `read` ([git/tokens § Security](../git/tokens.md#security)).
- Inputs never reach a shell unquoted; `ref` and `branch` can't start with `-` (no option injection); `path` is one segment.
- Git output is redacted (tokens, and any `https://user:pass@` shapes) before storage.

## Observability

- **Metrics:**
  - `lewbox_repository_clones_total{outcome}`;
  - `lewbox_repository_clone_duration_seconds`;
  - `lewbox_repository_clone_budget_exhausted_total`.
- **Activity:** one event per create summarising the clones (`repositories.cloned`, with counts and failures' names).

## Configuration

| Variable | Default |
|---|---|
| `LEWBOX_SANDBOX_MAX_REPOSITORIES` | `20` |
| `LEWBOX_CLONE_TIMEOUT` | `5m` |
| `LEWBOX_CLONE_CONCURRENCY` | `4` |

## Testing

- **Parsing table:** every accepted and refused URL and object shape, ported from legacy's `Repository` rule and `ParseRepositoryUrl` tests.
- **Path defaults** with name clashes.
- **Fake toolbox:** clone scripts succeed, fail and hang; the budget is shared across parallel clones; resume skips `cloned` ones.
- **Live:** a private GitHub repository via the App, a public one without a connection, a GitLab public repository, a `ref` that's a tag, a commit SHA, and a new `branch`.

## Build plan

1. `sandboxes.ResolveRepositories` (parsing and validation) and OpenAPI schema.
2. The `repositories_cloned` step in `internal/operations` with the clone script, parallelism, budget and progress writes.
3. The report on the sandbox object; the webhook.
4. Tests and the live check (with [git](../git.md) in M3).

## Open questions

1. ~~**LFS.**~~ Decided (2026-10-29): clone with `GIT_LFS_SKIP_SMUDGE=1` and document `git lfs pull`; an `lfs: true` per entry comes later.
2. ~~**Submodules.**~~ Decided (2026-10-29): off by default, with `submodules: true` per entry.
3. **Partial clones and agents.** Spike (M1): clone with `--filter=blob:none`, stop and start the sandbox, then run `git log -p` and `git blame` with the fresh token; if lazy fetches fail or are slow, fall back to full clones.
