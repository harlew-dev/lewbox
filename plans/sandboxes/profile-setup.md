# Profile setup

> Status: Draft · Milestone: Alpha (M1 mechanism, env file and SSH keys; M3 agent, Git and telemetry steps) · Core runner; steps contributed by plugins · Updated: 2026-10-29

Before a sandbox is handed back from a create or start, Lewbox writes its profile into it: the coding agents' sign-ins, the Git token, credential helper and commit identity, telemetry settings for the agents, the profile's SSH keys, and an env file every login shell loads. So the first command finds `claude` and `codex` signed in and `git` able to clone. This plan covers the **setup manifest** (what's written), **applying** it (how), **cleanup** (removing it before a stop), **re-applying** it when the profile changes, and the **report** a sandbox carries. *What* each credential is and how it's set lives in [profiles](../profiles.md) ([agent credentials](../profiles/agent-credentials.md), [telemetry](../profiles/telemetry.md), [SSH keys](../profiles/ssh-keys.md)) and [git](../git.md); this plan is how it reaches the sandbox.

## Today

- **Built by the app.** `BuildSandboxSetup` builds a manifest from the profile (`lewbox-legacy/app/Actions/Sandboxes/BuildSandboxSetup.php`). The steps live in `app/Sandboxes/Setup/`: `ClaudeCodeSetup`, `CodexSetup`, `GitSetup`, `SshSetup` and `TelemetrySetup`.
- **A step** (`SetupManifest`) has:
  - `configured`;
  - `files` (JSON merged into what's there; `secret` files at mode 0600; `create_only` files written only when missing);
  - `env`;
  - `commands`;
  - `check`;
  - `cleanup`;
  - `secrets` to redact;
  - `requires` (a command, e.g. `claude`).
- **Applied by the gateway** (`lewbox-gateway-legacy/internal/setup/setup.go`), which fetches it over HTTP (`GET sandboxes/{sbx}/setup`). The order is:
  1. cleanup;
  2. base commands and files;
  3. `command -v` for each step's requirement;
  4. the env file;
  5. each applicable step's files, commands and check.

  It reports the result with `PUT sandboxes/{sbx}/setup`.
- **Paths** (`app/Sandboxes/Setup/SandboxPaths.php`):
  - `/root/.config/lewbox/env` holds the profile's variables; `/etc/profile.d/lewbox.sh` loads it into every login shell;
  - `/root/.config/lewbox/telemetry.env` is read by agentd only;
  - the GitHub token goes in `/root/.config/lewbox/github-token`, plus per-account `github-tokens/<account>`;
  - `git-credential-github` is the credential helper;
  - `/usr/local/bin/gh` is a wrapper, with `/root/.config/gh/hosts.yml`;
  - repositories go under `/root/workspace`.
- **Statuses:** `ready`, `configured`, `failed`, `not_configured` and `not_applicable` (`app/Enums/Sandbox/SetupStatus.php`).
- **Keeping it current.**
  - A changed credential re-applies setup to running sandboxes as `apply_setup` operations (`ApplyProfileSetup`). Busy sandboxes are skipped.
  - GitHub App tokens are refreshed by writing files directly, every 5 minutes (`RefreshGitHubAppTokens`).
  - ChatGPT logins are refreshed and pushed back (`RefreshSubscriptionLogins`).
  - An SSH key change returns a `sync` command that the gateway runs.
- **Everything runs in a login shell** (`bash -lc`, else `sh -lc`), so commands, sessions, terminals and SSH logins all get the env file.

**What's wrong with it.**
- The manifest is built in PHP and applied in Go, so it crosses HTTP as JSON with secrets in it.
- The token refresh and the ChatGPT refresh go app → public API (with a sandbox key minted for itself) → gateway → toolbox.
- Every refresh is a separate code path from apply.

## Goals

- One Go package that builds and applies the manifest, with no secrets crossing a process boundary except into the sandbox itself.
- A create or start returns with the profile applied: agents signed in, Git ready, SSH keys in place.
- Setup **never fails a sandbox.** It's best effort, reported per step.
- Credentials don't outlive their need in the sandbox: removed when they're removed from the profile, refreshed before they expire, and **never in a snapshot, whatever stopped the sandbox** ([below](#credentials-never-enter-a-snapshot)).
- Setup writes only where the profile has something to write, and only for tools the sandbox has.

## Keep, change, drop

| | |
|---|---|
| **Keep** | The manifest's shape (files, env, commands, check, cleanup, requires, secrets). The apply order. JSON merge, `secret`, `create_only`. Login shells everywhere. Statuses. Never failing the sandbox. Cleanup before a stop. Re-apply on change. Redaction |
| **Change** | Built and applied in one process (`internal/setup`). Token and login refreshes become **partial applies** of the same manifest (one step's files), not separate code. SSH key changes are a partial apply of the `ssh` step. Applies are batched into one toolbox call where possible (a single script) to cut round trips. The manifest is versioned (`setup_version`), so we can tell which sandboxes have an old one |
| **Change (credentials)** | Legacy cleaned credentials only before stops it asked for, and accepted that AerolVM's timer stops left them in snapshots, in the project's bucket. Now **no snapshot holds a credential Lewbox imported**: secrets live on a memory-only mount, the snapshot is scrubbed on the node at the filesystem level, and the result is verified before anything is uploaded ([below](#credentials-never-enter-a-snapshot)). Secret paths move from `/root/.config/lewbox/` to `/run/lewbox/` |
| **Drop** | The internal `GET`/`PUT sandboxes/{sbx}/setup` API. Sandbox keys minted for Lewbox's own use. The gateway-returned `sync` command. Accepting credentials in snapshots after timer stops |

## Design

### The manifest

```go
type Manifest struct {
    Version  int        // bumped when steps change shape
    Base     Base       // files and commands every sandbox gets
    EnvFile  string     // /run/lewbox/env (secret; memory-only mount)
    Steps    []Step     // in order: claude-code, codex, git, telemetry, ssh, …
    Secrets  []string   // every secret value written, to redact
    Scrub    []ScrubPath // every path that may hold a secret, for the snapshot scrub
}

type ScrubPath struct {
    Path    string // absolute; a file, or a directory removed whole
    Harvest bool   // read it back before removing (a login the agent may have refreshed)
}

type Step struct {
    Name       string     // "claude-code", "codex", "git", "telemetry", "ssh"
    Label      string     // "Claude Code"
    Configured bool       // the profile has something for it
    Requires   []string   // commands that must exist, e.g. "claude"; empty = any sandbox
    Files      []File     // {Path, Content, Mode, Merge: json|none, CreateOnly, Secret}
    Env        map[string]string
    Commands   []Command  // {Script, Timeout}
    Check      *Command   // exits 0 when it works
    Cleanup    []Command  // removes what it wrote
}
```

Built by `setup.Build(ctx, profile, sandbox)` from the profile's current credentials. It's never stored: it's built fresh for each apply, so it's always current.

### The steps

The manifest, its runner, `/run/lewbox`, the env file, the scrub and the harvest are **core**. Each step is contributed by the core feature or **plugin** that owns it, through the **setup contributor** extension point ([plugins/extension-points](../plugins/extension-points.md)): a contributor returns, for a sandbox, the files and env it writes (each marked secret or not), the tools it needs (`command -v`), its check, its cleanup, the paths to scrub and the paths to harvest before a snapshot. A disabled plugin contributes nothing, so its files are removed at the next apply. Owners: `base`, `ssh` and `env` are core; `claude-code` is `agents/claude-code`; `codex` is `agents/codex`; `git` is the core Git framework with the provider plugin minting (`git/github`); `telemetry` is the `telemetry` plugin.

| Step | Requires | Writes | Check | Cleanup | Milestone |
|---|---|---|---|---|---|
| **base** | | `/root/.config/lewbox/` (0700, nothing secret); `/run/lewbox/` (0700, the secrets, [below](#where-secrets-live)); `/etc/profile.d/lewbox.sh` (sources `/run/lewbox/env` if it's there; holds no secret itself); `/root/workspace` | | | M1 |
| **claude-code** | `claude` | `/root/.claude.json` merged (onboarding done; `/root` and `/root/workspace` trusted; an API key's last 20 chars approved). Env (in `/run/lewbox/env`): `ANTHROPIC_API_KEY`, or `CLAUDE_CODE_OAUTH_TOKEN` for a subscription | `claude auth status` | Remove its env lines and the approved key suffix | M3 |
| **codex** | `codex` | `/root/.codex/config.toml` (create-only: `cli_auth_credentials_store = "file"`, trusted dirs; never a secret). `/run/lewbox/codex/auth.json` (secret), written by Lewbox for a subscription *and* for an API key (Codex's own `{"OPENAI_API_KEY": …}` shape, rather than running `codex login`, which would write the key to disk), and `/root/.codex/auth.json` a symlink to it. Harvested before every scrub ([below](#harvest-first)) | `codex login status` | Remove both | M3 |
| **git** | `git` | Token files, the default token and the repository map under `/run/lewbox/git/` (secret); credential helper and `gh` wrapper (no secrets); `hosts.yml` **without a token** (the wrapper always supplies `GH_TOKEN`); `git config --global` identity, helper and `url.https://github.com/.insteadOf git@github.com:` | `git ls-remote` against one repository the token reaches, if any | Remove `/run/lewbox/git/`, helper, wrapper, `hosts.yml` | M3 ([git/tokens](../git/tokens.md#whats-written)) |
| **telemetry** | | `/run/lewbox/telemetry.env` (secret: endpoint headers), read by agentd only, which also hands Codex its exporter settings as config overrides, so nothing secret goes in `config.toml` ([profiles/telemetry](../profiles/telemetry.md)) | | Remove it | M3 |
| **ssh** | | The profile's and the sandbox's public keys in `/root/.ssh/authorized_keys`, between `# lewbox begin` / `# lewbox end` markers, keeping the user's own lines. 0700 and 0600. Public keys aren't secrets, so they stay on disk and in snapshots | | Remove the block | M1 ([profiles/ssh-keys](../profiles/ssh-keys.md)) |
| **env** | | The profile's own env variables, if profiles get them (open question) | | | Later |

More agents (Cursor, Grok, OpenCode, pi, Antigravity) follow the same pattern later, each as an agent provider plugin with its own setup contributor ([plugins/agent-providers](../plugins/agent-providers.md)), porting Lewagent's `app/Sandboxes/Setup`. Adding one adds a row here without changing the runner.

### Applying

`setup.Apply(ctx, toolbox, manifest, opts) (Report, error)`:

1. **One script.** The manifest is rendered into one shell script (POSIX `sh`, so it works without bash). It holds secrets, so it's uploaded to `/run/lewbox/setup-<n>.sh` (0700) through the files API, and run with `sh -l` through `Execute`. The script does the steps below, writes a JSON report to stdout between markers, and deletes itself. This replaces legacy's tens of toolbox calls per apply with three: write the script, run it, read its report.
   1. Run every step's cleanup (so a removed credential disappears).
   2. Base commands and files.
   3. `command -v` for each step's requirements.
   4. Write the env file from the env of every step that applies (0600).
   5. For each applicable, configured step: files (JSON merge via a tiny embedded merge helper, `lewbox agentd json-merge`, if agentd is installed, else a `python3`/`jq`-free fallback written in `sh`+`awk` for flat merges; see [open questions](#open-questions)), commands, check.
2. **Large files** (over 64 KiB, e.g. a Codex `auth.json` is small, but future steps may not be) are written with the files API before the script runs, and referenced by path.
3. **Each step's result** is written by the script itself into the report, one structured line per step, so one script run reports every step's outcome at once.
4. **Redaction:** every value in `Secrets` is replaced with `[redacted]` in check output and errors before the report is stored. Output is capped at 2 KiB per step.
5. **Timeouts:** the whole script 120 s; each command its own (default 30 s).
6. **Retries:** if the toolbox can't be reached (warming up after a start, `502`/`503`), retry as a transient failure within the operation's deadline. If it never could, the report's `error` says so, and the operation still succeeds.

A **partial apply** runs the same script for named steps only, without the global cleanup, e.g. `steps: ["git"]` to refresh a GitHub token. That serves the token refresher, a credential change for one agent, and an SSH key added to a running sandbox.

### When setup runs

| When | What | How |
|---|---|---|
| `create`, after `running` | Full apply | Operation step `setup_applied` |
| `start` and `restart`, after `running` | Full apply | Step `setup_applied` |
| A profile credential, Git connection, identity or telemetry changes | Full apply on each running sandbox of the profile | `apply_setup` operation per sandbox (reason `setup_changed`); a busy sandbox is skipped, as its own create or start builds the manifest fresh |
| GitHub App token near expiry (15 min) | Partial: `git` | Job `git.refresh_token` per sandbox and account, at `expires_at − 15 min` ([git/tokens](../git/tokens.md#refreshing)). Not an operation, so it reaches busy sandboxes too |
| ChatGPT login refreshed | Partial: `codex` | Job ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)) |
| SSH key added or removed (profile's or sandbox's) | Partial: `ssh` | Inline in the request, before it answers, while the sandbox runs |
| A node comes back online | Full apply on its running sandboxes | `apply_setup` (reason `node_back_online`), since changes made while it was offline were skipped |
| Before a stop that Lewbox or a user asks for | Harvest, then cleanup | Steps `harvested`, `setup_cleaned` ([operations](operations.md#stop)) |
| Before any snapshot, whatever stopped it | Scrub on the node, and verify | Step `scrubbed` ([below](#credentials-never-enter-a-snapshot)) |

### Cleanup

- Runs before every stop that's asked for (not before a restart, which takes no snapshot, nor a delete), after the [harvest](#harvest-first), and clears `/run/lewbox/`.
- A step's cleanup is in the manifest while the profile has something for it, **and once more after**, when the sandbox's last report says it wrote to it. So a credential removed from the profile is removed from the sandbox, and a login the user made inside the sandbox themselves, with nothing in the profile, is left alone. The report stores `wrote: true` and the paths written per step for this.
- Cleanup is the polite first layer, not the guarantee: a sandbox AerolVM stopped on a timer can't run it, and a step's cleanup can fail. The guarantee is the node-side scrub ([next](#credentials-never-enter-a-snapshot)).

### Credentials never enter a snapshot

**Rule: no snapshot of a sandbox ever holds a credential Lewbox imported into it.** Not after a stop that's asked for, not after an idle-timeout or lifetime stop (which AerolVM makes itself, with the container already stopped and the toolbox gone), not after a stop for credits, a budget or a suspension, not when a node is drained and its sandboxes moved, and not for any checkpoint added later. Snapshots go to the project's bucket and stay there for days; a credential in one outlives every rotation and revocation Lewbox makes.

"Imported" means what Lewbox wrote: everything below. A secret a process in the sandbox copies elsewhere (an agent pasting a key into a file in `/root/workspace`) is the sandbox's own data, and can't be told apart from it; the docs say so.

#### What's imported

| Credential | Where it is | Step | Harvest |
|---|---|---|---|
| Claude Code: API key or subscription token | `ANTHROPIC_API_KEY` / `CLAUDE_CODE_OAUTH_TOKEN` in `/run/lewbox/env` | `claude-code` | No (static) |
| Codex: API key or ChatGPT login | `/run/lewbox/codex/auth.json`, with `/root/.codex/auth.json` a symlink to it, or a regular file if Codex replaced the link | `codex` | **Yes**: ChatGPT refresh tokens rotate |
| GitHub installation tokens, default token, repository map | `/run/lewbox/git/` | `git` | No (Lewbox mints them) |
| `gh`'s login | `/root/.config/gh/hosts.yml`, written with no token; listed anyway, in case `gh` wrote one | `git` | No |
| Telemetry exporter headers | `/run/lewbox/telemetry.env` | `telemetry` | No |
| agentd's channel token | `/run/lewbox/agentd/channel.token` | agentd ([agents/agentd](../agents/agentd.md#files-on-disk)) | No |
| The setup script while it runs | `/run/lewbox/setup-<n>.sh` | all | No |
| The sandbox's create-time `env` | The container's configuration (AerolVM passes it as the container's `Env`), which `docker commit` copies into the snapshot image's config | create | No |
| Not secret, and kept: the commit identity, `authorized_keys`' public keys, `config.toml` without `[otel]`, the helper and wrapper scripts, the approved key's last 20 characters in `.claude.json` (removed by the in-sandbox cleanup; not usable to authenticate, so the node scrub doesn't edit JSON for it) | | | |

Each apply records the exact paths it wrote in the setup report (internal, not in the API), and the manifest's `Scrub` list is built from them plus the fixed paths above. So a step added later, or a path that moves, is scrubbed without a separate list to keep in step.

**The user's paths are scrubbed the same way.** A sandbox's own `scrub` paths, and its template's ([hooks](hooks.md#scrub)), are added to the `remove` list of every scrub, and checked in the report like Lewbox's: a user path not accounted for fails the scrub as one of Lewbox's would, and the snapshot is never uploaded. They're removed only on the node, after the stop, never by the in-sandbox cleanup, so `on_stop` hooks and the sandbox's processes can still use them until the end. Two more files join the lists when the fork's backstop stopped the sandbox itself ([hooks](hooks.md#when-lewbox-cant-act)): `/root/.lewbox/hooks/backstop.jsonl` is in `read` (the hooks' results) and `remove`; the backstop script itself is on `/run/lewbox`, so it's never on disk.

#### Four layers

1. **<a id="where-secrets-live"></a>Secrets live in memory.** Every sandbox gets a `tmpfs` at `/run/lewbox` (0700, 16 MiB), mounted by AerolVM when it creates the container (Docker's and gVisor's `tmpfs` mounts; the guest's init on Firecracker): fork change F48 ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)). `docker commit` doesn't include mounts, and a stop empties it, so a timer stop leaves nothing on disk to scrub. Tools that expect a file elsewhere get a symlink into it (`/root/.codex/auth.json`). A tool that replaces a file by renaming a new one over it turns the link back into a regular file on disk; that's why the paths are scrubbed anyway (layer 3), and why Codex's file is harvested from whichever it is. A fork release without F48 gets the same paths on the container's disk (`/run/lewbox` as a plain directory), and relies on layers 2 and 3.
2. **The in-sandbox cleanup**, before every stop that's asked for, while the toolbox still answers: harvest, run every step's cleanup, empty `/run/lewbox` ([cleanup](#cleanup)). Best effort; it also covers fork releases without layer 3 for stops Lewbox makes.
3. **The snapshot scrubs itself, on the node** (fork change F46). AerolVM's snapshot call takes the paths, and before committing, works on the **stopped** container's filesystem from the host, so it needs nothing running inside:
   ```json
   POST /v1/sandboxes/{id}/snapshot
   { "name": "sbx_…-op_…", "scrub": { "remove": ["/run/lewbox", "/root/.codex/auth.json", "/root/.config/gh/hosts.yml", …],
                                      "read":   ["/root/.codex/auth.json"] },
     "reset_config": true }
   ```
   - **Docker** (and gVisor from the beta: gVisor's writes land in the container's overlay upper directory, as the fork runs it with `--overlay2=none`, F21): each path is deleted from the upper directory; a path that also exists in a lower layer (a previous snapshot, or the image) gets an overlay whiteout, so the committed layer hides it. Symlinks are removed, not followed.
   - **Firecracker** (later): the stopped VM's `rootfs.ext4` is opened on the host (`debugfs -w`, no mount needed) and the paths unlinked, before the disk is snapshotted. Only disk is ever snapshotted ([Firecracker](#memory-is-never-snapshotted)).
   - **`read`** paths are read before they're removed and returned in the answer (64 KiB each at most), over the node's TLS API with the cluster's PAT: the [harvest](#harvest-first) for sandboxes AerolVM stopped itself.
   - **`reset_config`** (fork change F47) commits the image with the base image's own `Env`, `Entrypoint` and `Cmd` (folding in F10's entrypoint fix), so the create-time `env`, which can hold secrets, isn't copied into the snapshot's image config in the registry. Lewbox already sends `env` again on every resume ([persistence](persistence.md#resume)), so nothing is lost.
   - The same scrub runs on a stop **without** a snapshot (a cluster without storage, or a sandbox left stopped on its node): `POST /v1/sandboxes/{id}/scrub` with the same body, so a stopped container on a node's disk doesn't hold them either.
4. **Verify before upload.** The fork checks its own work before it pushes anything: none of the `remove` paths exists in the committed layer except as a whiteout, and the image config's `Env` is the base image's. It answers with a `scrub` report (each path `absent`, `removed` or `whited_out`; `config_reset: true`), which Lewbox stores on the snapshot row ([persistence](persistence.md#data-model)) and checks itself: any path missing from the report, or any failure, and Lewbox doesn't accept the snapshot. The push to the registry starts only after the check passes, so a snapshot that fails it never leaves the node.

#### Harvest first

A ChatGPT login's refresh token works once. If Codex refreshed its copy inside the sandbox and Lewbox then deleted it, the profile's stored copy would already be dead, and every sandbox of the profile signed out. So before anything is removed, Lewbox reads back every path marked `Harvest` (today only Codex's `auth.json`) and adopts it if it's newer than the stored login (a later `last_refresh` and a different refresh token), under the credential's row lock, then writes it to the profile's other running sandboxes, as the refresh job does ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md#refreshing-chatgpt-logins)):

- A stop that's asked for: read through the toolbox, before the in-sandbox cleanup (operation step `harvested`).
- A sandbox AerolVM already stopped: read by the snapshot's own scrub (`read`), at step `scrubbed`.
- Unreadable (a broken file, a fork without F46 on a timer stop): the stored login is kept, the event is logged, and the refresh job's next pass finds out whether it still works.

#### When a scrub fails

A snapshot that may hold a credential is never uploaded, and never kept:

- The fork doesn't push it, and deletes the committed image; the container stays **stopped on its node**, untouched.
- The stop operation retries the scrub as a transient failure within its deadline ([operations](operations.md#retries-and-failure-classes)).
- At the deadline the stop ends `failed`, `error_code: scrub_failed`, with the sandbox left `stopped` on its node (as a stop that couldn't upload is, [operations](operations.md#what-a-failure-leaves)): starting it again resumes it in place, and setup writes the credentials again. An operator alert fires (`lewbox_snapshot_scrub_failures_total`), the project gets a notification, and the activity log an `operation` event with the reason.
- The reconciler doesn't try to offload it again on its own; a new stop does, once the cause is fixed.

#### No fallback for older fork releases

F46, F47 and F48 are P0 and in Lewbox's minimum fork release, so every node Lewbox uses has them, and there's one way a stop works ([D15](../README.md#decisions)). Legacy's alternative, accepting credentials in snapshots after timer stops, and the interim idea of scrubbing by restarting a stopped sandbox in place, are both gone.

#### Memory is never snapshotted

A stop saves disk, never memory, on every runtime, including Firecracker once its snapshots are in the fork ([persistence](persistence.md#firecracker)). Memory can't be scrubbed: `/run/lewbox` is in it, and so is every process's environment, and a shell started an hour ago still holds `ANTHROPIC_API_KEY`. So a "pause that keeps processes" (persistence's old open question) isn't offered, because it can't keep this rule. Firecracker *templates* keep their build-time memory snapshot ([clusters/runtimes](../clusters/runtimes.md#firecracker-microvm-later)): it's taken at build, before any setup, so it holds no credential.

#### What's tested

- **Golden scrub lists** per manifest: every path a step writes is in `Scrub`, checked by a test that renders each step and compares the paths it writes with the scrub list.
- **Fake AerolVM** honours `scrub` and `reset_config`, can fail either, and can leave a path behind; Lewbox must refuse the snapshot in each case.
- **Live, on each supported runtime (Docker in the alpha):** apply setup with canary credentials (distinct random strings per path), stop by each route (asked for, idle timeout, lifetime, credits, drain), pull the snapshot from the registry, and search every layer and the image config for the canaries. Any hit fails the release. Nightly on `container` and `gvisor`; on `microvm` once its snapshots ship.

### The report

Stored on `sandboxes.setup`, returned as the sandbox's `setup`:

```json
{
  "applied_at": "2026-12-01T10:00:00Z",
  "version": 3,
  "error": null,
  "steps": [
    {"name": "claude-code", "label": "Claude Code", "status": "ready",          "message": null},
    {"name": "codex",       "label": "Codex",       "status": "failed",         "message": "Not logged in. … [redacted]"},
    {"name": "git",         "label": "Git",         "status": "configured",     "message": null},
    {"name": "telemetry",   "label": "Telemetry",   "status": "not_configured", "message": null},
    {"name": "ssh",         "label": "SSH keys",    "status": "configured",     "message": null}
  ]
}
```

| Status | Means |
|---|---|
| `ready` | Applied, and its check passed |
| `configured` | Applied; no check to run |
| `failed` | A command or the check failed; `message` says why, redacted |
| `not_configured` | The profile has nothing for it |
| `not_applicable` | The sandbox doesn't have the tool (`requires` not found) |

`error` is set when nothing could be applied (the toolbox unreachable until the deadline); `steps` then holds the last good report, if any.

A **webhook** `sandbox.setup_applied` (new) carries the report, so Lewagent can show "Claude Code: ready" without polling. Partial applies update their step only.

### Templates without a shell

The script needs `/bin/sh`. An image without one (distroless) gets `error: "The sandbox has no /bin/sh, so the profile's setup can't be applied."`, and every step `not_applicable`. Documented.

## API

No endpoints of its own: `setup` on the sandbox object, the `sandbox.setup_applied` webhook, and the profile endpoints that change what's in it ([profiles](../profiles.md)).

## Security

- Secrets are decrypted only to build the manifest, in memory, and written into the sandbox over the toolbox (TLS to the node, or the tunnel). The script and every file holding a secret are 0600 or 0700, root-owned, on the memory-only `/run/lewbox`. The script deletes itself.
- **No snapshot holds them** ([above](#credentials-never-enter-a-snapshot)): memory-only storage, in-sandbox cleanup, a filesystem scrub on the node, and a check before upload.
- **A sandbox can read its credentials.** That's their purpose, and so whatever runs there can copy them. The dashboard and docs say so, and suggest scoped keys with spend limits at the provider ([profiles/agent-credentials](../profiles/agent-credentials.md)).
- Git tokens are narrowed to the sandbox's repositories, with the access the user chose per repository: default `full`, so by default anything in the sandbox can push to them. `read`, or a project cap of `read`, keeps pushing out of the sandbox ([git/tokens](../git/tokens.md#security)).
- Tokens never go in remotes, env (`GH_TOKEN`) or command lines; they go in files read per call.
- Redaction runs on all stored output: the report, activity, logs.

## Observability

- **Metrics:**
  - `lewbox_setup_apply_duration_seconds{kind=full|partial}`;
  - `lewbox_setup_steps_total{step,status}`;
  - `lewbox_setup_apply_errors_total`.
- **Activity:** each apply records an event (`setup.applied`, with the step statuses and no secrets).
- **Webhook:** `sandbox.setup_applied`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SETUP_TIMEOUT` | `120s` | The whole script |
| `LEWBOX_SETUP_COMMAND_TIMEOUT` | `30s` | Each command |
| `LEWBOX_GIT_TOKEN_REFRESH_BEFORE` | `15m` | [git](../git.md) |

## Testing

- **Golden tests** of the rendered script per step, with fixtures of profiles.
- **Running the script** in real containers (Ubuntu, Debian, Alpine/busybox `sh`, an image with no `claude`), asserting the files written, the JSON merges keeping existing keys, `create_only`, modes, and cleanup removing exactly what was written.
- **Redaction:** secrets in check output, in errors, and split across lines.
- **Partial applies** leave other steps alone.
- **Fake toolbox** (warm-up `503`s, a lost exit status) for the apply's retries.
- **Live:** a `claude-code` template sandbox with an API key is `ready`; removing the key removes it on the next apply.
- **Snapshots:** canary credentials never appear in a snapshot, by any stop route ([what's tested](#whats-tested)).

## Build plan

1. `internal/setup`: manifest types; the setup contributor interface and registry; renderer to `sh`; report parser; redaction; `Apply` and `ApplyPartial`.
2. Base and `ssh` steps (M1); the `env` file mechanism.
3. `claude-code`, `codex`, `telemetry` contributors (M3), in `plugins/agents/claude-code`, `plugins/agents/codex` and `plugins/telemetry`, ported from `lewbox-legacy/app/Sandboxes/Setup`.
4. `git` step with [git/tokens](../git/tokens.md) (M3).
5. Secrets under `/run/lewbox`, the `Scrub` list built from what each apply wrote, the harvest, and the scrub's use in `stop` (M1 for the mechanism, with the fork's F46–F48; the steps that write secrets arrive in M3).
6. Triggers: `apply_setup` on profile changes; partial applies from refresh jobs and SSH key changes.
7. The `sandbox.setup_applied` webhook.

## Open questions

1. **JSON merge without tools.** The script needs to merge into `~/.claude.json`, and `sh`+`awk` merging of arbitrary JSON is fragile. Options:
   - upload `lewbox agentd` (already a static binary we put in sandboxes for agents) and run `lewbox agentd json-merge`;
   - do the merge in the control plane: read the file, merge, write it back. That's two toolbox calls, and races with the CLI writing it.

   Proposed: the agentd binary, installed at the start of setup if missing ([agents/agentd](../agents/agentd.md)).
2. **Profile env variables.** Should profiles have their own env that every sandbox gets (beside the sandbox's create-time `env`)? Lewagent writes `.env` files itself; a profile-wide env would cover API tokens for tools. Later?
3. **Per-conversation setup** (Lewagent's ask: `env`, skills, MCP servers, allowed tools per conversation) belongs to [agents](../agents.md), not here, but may reuse the file-writing machinery.
4. **Check cost.** `claude auth status` and `codex login status` add a few seconds to every start. Run checks only on create and when credentials change, not on every start?
5. **Codex and the symlink.** Does Codex write `auth.json` in place (keeping the link) or by renaming a new file over it? Either is handled, but in place is better: a refreshed login then never touches disk. A spike before M3; if it renames, consider `CODEX_HOME` on `/run/lewbox` with its sessions directory linked back to disk, as conversations resume from those transcripts ([agents/agentd](../agents/agentd.md#files-on-disk)).
6. **Codex API keys as a file.** Writing `{"OPENAI_API_KEY": …}` ourselves instead of `codex login --with-api-key` depends on Codex's `auth.json` shape, which isn't a public contract. Pin it with a check in the adapter-version tests.
