# The AerolVM fork

> Status: Draft · Milestone: Alpha (M0 for the fork and its pipeline, then each fix by its priority) · Updated: 2026-10-29

Lewbox runs sandboxes on [AerolVM](https://github.com/aerol-ai/microvm) (`aerol-ai/microvm`, MIT), a Go daemon (`sandboxd`) on every node, with a daemon inside every sandbox (`toolboxd`). Legacy Lewbox used upstream releases as they were, and worked around their bugs in three places: the install script, the gateway and the app. We now keep **our own fork**. Bugs are fixed in the fork first and offered upstream as pull requests, the fork is rebased onto each upstream release, and Lewbox drops a workaround once every node in a cluster runs a fork release with the fix ([D3](../README.md#decisions)).

## Today

- **Pinned to `v0.6.4`.** Lewbox pins one release, `AEROLVM_SUPPORTED_VERSION` in `lewbox-legacy/config/lewbox.php`, and records it per cluster (`clusters.aerolvm_version`). Each cluster is pinned when it's created, and nothing upgrades an existing cluster. `v0.6.5` was published without release assets, so it can't be installed (`lewbox-legacy/docs/clusters.md` § *AerolVM version*).
- **Live nodes run later builds than the pin.** The resume bug was seen on `v0.6.8`, and on `main` at `v0.6.8-11` (`lewbox-gateway-legacy/docs/aerolvm.md` § *Known upstream issues*), so a pin doesn't say what a node really runs.
- **Workarounds are spread out:**
  - **In the install script** (`lewbox-legacy/resources/scripts/install.sh`): it installs `file` before AerolVM's `install.sh`, lowercases `SB_AUTO_IMPORT_CLUSTER_ID`, sets `SB_HOST_RUNTIMES=docker,gvisor`, adds `--overlay2=none` to runsc, and adds a snapshot-image cleanup timer.
  - **In the gateway:**
    - exit-status traps: `aerolvm.ExitStatusTrap`, `SessionExitStatusTrap` (commits `febb176`, `cf9813a`, `590bbc2`);
    - `container_command` recorded from `/proc/1/cmdline` (`6e36f0a`);
    - a check before starting a private-port proxy (`5f3d19e`), and a verified `chmod` (`4f0da62`);
    - capacity errors recognised by their wording (`e1573fa`);
    - the sandbox index used to find a node;
    - snapshot status looked up on each node in turn;
    - a "touch" while streams move;
    - retries while the toolbox warms up.
  - **In the app:** registry GC done directly on the bucket (`app/Actions/Clusters/CleanUpRegistry.php`).
  - **By hand on nodes installed before a fix:** `docs/examples/fix-gvisor-host-runtimes.sh` and `fix-gvisor-overlay.sh`, replacing the firewall script, and setting `SB_IMAGE_BUILD_TIMEOUT` and `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` (legacy `docs/todo.md` lines 44–74, 110–111).
- **Upstream reports never sent.** `docs/todo.md` lists several "Report upstream" items that are still open: the lowercase path, lost exit statuses, the nested toolbox, an image-delete API, zstd, and flattening.
- **Firecracker isn't selectable,** as AerolVM's snapshot API answers "not implemented" for it. The maintainer has said Firecracker snapshots (with registry push and pull) and Firecracker in `install.sh` are on the roadmap, "targeting next week" as of 2026-10-24 ([discussion #785](https://github.com/aerol-ai/microvm/discussions/785)).

## Goals

1. Fix AerolVM's bugs where they start, and send every fix upstream.
2. Stay close to upstream: rebase onto each release within a week of it, and carry as few patches as we can.
3. Ship fork releases with assets for amd64 and arm64: checksummed, signed, and installable by `lewbox node` ([node-agent](node-agent.md)).
4. Know exactly what each node runs. Lewbox chooses its behaviour from the versions nodes report, not from a pin.
5. Delete each Lewbox workaround once no node in a cluster still needs it.

### Non-goals

- Diverging from upstream: a feature only Lewbox wants goes behind a flag or an extension point, never into a rewrite.
- AerolVM's managed control plane (`pkg/controlplane`, private upstream), and its hosted registry (AOCR).
- Maintaining WASM or Isolate runtimes. Lewbox doesn't use them.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | AerolVM as the runtime: its clustering (Raft, gossip, mTLS), scheduler, toolbox, Caddy ingress, and Docker, gVisor and Firecracker support. The snapshot/registry offload model ([storage-and-registry](storage-and-registry.md)) |
| **Change** | Upstream releases → fork releases `vX.Y.Z-lewbox.N`. One pin for everything → each node reports its version, and Lewbox works out each cluster's features from it. Workarounds → fixes in the fork, with Lewbox fallbacks kept only until the fleet is upgraded. Hand-run fix scripts → node specs applied by the node agent ([node-agent](node-agent.md#desired-state)) |
| **Drop** | `packages/laravel-aerolvm` (the unused PHP client). Phrase matching on errors once the fork answers with codes. The Daytona facade for snapshots once `/v1/snapshots` exists |

## Design

### The repository

- **Name:** `github.com/harlew-dev/aerolvm` (open question 1), a GitHub fork of `aerol-ai/microvm`, MIT, keeping upstream's copyright and adding ours for our changes.
- **Branches:**

| Branch | What |
|---|---|
| `upstream/main` | A mirror of upstream `main`, fast-forwarded by a scheduled workflow. Never committed to |
| `fix/<slug>`, `feat/<slug>` | One per patch, based on the upstream release we're on, each a small series of commits. Each is also an upstream pull request where it makes sense (see [Upstreaming](#upstreaming)) |
| `lewbox` | The release branch: the upstream release tag, plus every patch branch merged in a fixed order. Rebuilt, not merged into, on each upstream release |
| `lewbox-vX.Y` | A maintenance branch per upstream minor release we've shipped, for urgent fixes to clusters not yet upgraded |

- **The patch series is listed in one file,** `LEWBOX_PATCHES.md` at the root of `lewbox`: for each patch, its branch, its upstream PR and state (open, merged in `vX.Y.Z`, declined), why we carry it, and its tests. It is the backlog below, kept with the code.
- **Tags:** `vX.Y.Z-lewbox.N`. `X.Y.Z` is the upstream release it's based on; `N` counts our releases on it, starting at 1. Semver sorts them before upstream's `vX.Y.Z`, which is fine, as we never mix the two.

### Following upstream releases

A scheduled workflow watches upstream's releases. When one appears:

1. Open a tracking issue: *Rebase onto vX.Y.Z*.
2. For each patch branch, in order: rebase it onto the new tag. A patch merged upstream is dropped, and recorded in `LEWBOX_PATCHES.md` with the release that has it. A conflict is resolved by a person, and the branch force-pushed.
3. Rebuild `lewbox` from the tag plus the remaining branches.
4. Run CI ([Testing](#testing)), including the end-to-end suite on Docker, gVisor and Firecracker runners.
5. Tag `vX.Y.Z-lewbox.1` and publish its assets ([Release assets](#release-assets)).
6. Update Lewbox's supported-version table ([Versions in Lewbox](#versions-in-lewbox)) in a Lewbox pull request. Nodes then upgrade by their cluster's policy ([node-agent](node-agent.md#upgrading-aerolvm)).

Target: a rebased release within 7 days of upstream's, or a note in the tracking issue saying why not, such as a regression found by our end-to-end suite and reported upstream.

### Upstreaming

- Every fix is written so it can go upstream: one concern per PR, upstream's style and tests, and no Lewbox names in code or messages.
- A PR goes upstream when the fix is in a fork release and has run on a live cluster for at least a few days, so we don't send fixes that are wrong.
- Fixes that only Lewbox needs, such as a Caddy DNS provider that asks Lewbox to set DNS-01 records, are proposed upstream as extension points (a pluggable DNS provider), not as Lewbox code.
- Security fixes are reported privately to the maintainer first, then fixed in the fork, and released once upstream agrees or after 14 days.
- One person owns the fork's upstream relationship. The maintainer has asked to be told when we test pre-releases (#785).

### Release assets

Each fork release publishes, for `linux/amd64` and `linux/arm64`:

| Asset | What |
|---|---|
| `sandboxd-linux-<arch>` | The node daemon |
| `toolboxd-linux-<arch>` | The in-sandbox daemon, bind-mounted into every sandbox |
| `install.sh`, `cluster-init.sh`, `cluster-join.sh`, `uninstall.sh` | AerolVM's own scripts, fixed (see backlog); still used for host dependencies (Docker, gVisor, Firecracker, Caddy), as upstream maintains them ([installation](installation.md#aerolvms-own-installer)) |
| `firecracker-artifacts-<arch>.tar.zst` | The pinned Firecracker binary, jailer and guest kernel, for Firecracker clusters, until upstream's `install.sh` installs them (#785) |
| `checksums.txt` | SHA-256 of every asset |
| `checksums.txt.sig` | A signature of `checksums.txt` with the release key (minisign or cosign keyless, open question 2) |
| `sbom.spdx.json` | The software bill of materials |

- **CI refuses to publish a release that's missing an asset,** so a release like upstream's `v0.6.5` (no assets) can't happen in the fork.
- **The node agent verifies the signature and checksums** before installing anything ([node-agent](node-agent.md#upgrading-aerolvm)).

### Versions in Lewbox

Lewbox's code holds a table of the fork releases it supports (`internal/aerolvm/versions.go`):

```go
type Release struct {
    Version  string   // "v0.6.8-lewbox.2"
    Upstream string   // "v0.6.8"
    Assets   Checksums
    Fixes    Features // which backlog fixes it carries
    Status   Status   // supported | deprecated | unsupported
}
```

- **Every node reports its running version,** `sandboxd --version`, with each heartbeat ([health-and-metrics](health-and-metrics.md)), recorded as `nodes.aerolvm_version`.
- **A cluster has a target version,** `clusters.aerolvm_target_version`, which the project or the instance's default sets. Nodes converge on it ([node-agent](node-agent.md#upgrading-aerolvm)).
- **A cluster's features are what all its nodes have:** `Features` is the intersection of its online nodes' releases' fixes. The control plane checks this before each workaround:

```go
if !cluster.Features().Has(aerolvm.FixExitStatusUnderGVisor) {
    cmd = aerolvm.WithExitStatusTrap(cmd)
}
```

  A sandbox's own node would be enough for calls inside a sandbox, but a resumed sandbox may land anywhere, so the cluster-wide intersection is the safe default. Calls that can't move use the node's own features.
- **Deprecating a release:** marked `deprecated` in a Lewbox release, with dashboard warnings on clusters that run it; `unsupported` one Lewbox minor release later, when the workarounds it needed may be deleted from the code.
- **New clusters** get the newest `supported` release.
- **Mixed versions within a cluster** are allowed only during a rolling upgrade, which is one minor version at a time; AerolVM's Raft state must be compatible between them (checked in the fork's upgrade test).

### Removing a workaround

Each workaround in Lewbox is written as:

1. a `Features` flag naming the fork fix;
2. the fallback code, behind `!Has(flag)`, in one place, with a comment linking its backlog row;
3. a test running both paths against the fake toolbox, which can play either version ([Testing](#testing)).

Once no supported release lacks the fix, the flag, the fallback and its test are deleted in one pull request.

## Fix backlog

Every known AerolVM bug or gap Lewbox has hit, from legacy docs, commits and live clusters, with what we do about it. **P0** blocks the alpha, **P1** is wanted for the alpha, **P2** comes after the alpha, **P3** is nice to have.

**Where** says where it's fixed: *Fork* means the fork fixes it and Lewbox drops its workaround once the fleet has it; *Lewbox* means it stays Lewbox's job; *Both* means the fork fixes it and Lewbox keeps a safeguard.

### Process and exec (toolboxd)

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F1 | **Exit statuses lost under gVisor.** toolboxd, as PID 1, reaps a command before it waits for it; it recovers from `ECHILD` but gVisor answers `ESRCH`, so the command reports `exit_code: -1` and `waitid: no such process`. Hits Execute, sessions (reported `killed`), exec streams, and login shells | Live on EU West, `v0.6.8` (`febb176`, `cf9813a`, `590bbc2`) | toolboxd: treat `ESRCH` as `ECHILD` and take the status from the reaper's table; better, register commands with the reaper before starting them so it never reaps them unseen. Test under runsc in CI | Fork. Lewbox keeps the traps (`ExitStatusTrap`, `SessionExitStatusTrap`) behind `FixExitStatusUnderGVisor` | P0 |
| F2 | **Session names aren't unique.** Every create starts a new session, even with a running one's name; only SSH attach reuses one (`GetOrCreate`). A second private-port proxy was started, failed to bind, and the failed expose then killed the working proxy | Live, Lewagent's `sbx_S2NLeK88raX9nw`, 2026-10-27 (`5f3d19e`) | toolboxd: `POST sessions` takes `if_absent: true`, returning the running session with that name instead of starting another, atomically | Both: Lewbox still names sessions by `ses_` id and lists before starting proxies | P1 |
| F3 | **No pings on exec streams and attach.** Idle WebSockets get cut by proxies | Gateway design (`websocket.go` pings itself) | toolboxd: ping every 30 s | Both | P3 |
| F4 | **Toolbox not ready for a few seconds after a start**: proxy `502 toolbox unavailable` (plain text) or `503 sandbox not adopted` | Gateway retries for `GATEWAY_TOOLBOX_WAIT` | sandboxd: a sandbox's `toolbox_ready` in `GET /v1/sandboxes/{id}`, and a JSON error with a code | Both: Lewbox retries until ready | P2 |
| F5 | **Activity is counted only when a request starts,** not while a stream moves data, so a long download or a busy terminal lets the idle timeout stop the sandbox. Status calls count too, so polling a session keeps the sandbox awake | Gateway touches `toolbox/health` every `GATEWAY_ACTIVITY_INTERVAL`; watched sessions keep sandboxes awake (`cf9813a`) | toolboxd: count bytes moving on streams (at most once a minute); a header or route class that marks a request as not activity (status reads, watchers, stats) | Fork; Lewbox's touch goes | P1 |
| F6 | **Errors in three shapes:** AerolVM's `{"error"}`, envd's `{"code","message"}`, the proxy's plain text; `code` only sometimes; `400` for nearly everything, including an image that can't be pulled | `lewbox-gateway-legacy/docs/aerolvm.md` § Errors | One JSON error shape with a stable `code` (`not_found`, `capacity_exceeded`, `image_pull_failed`, `toolbox_unavailable`, …) on every route | Fork; Lewbox's error mapping becomes a table | P1 |

### Files

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F7 | **Downloads read whole into memory** (`os.ReadFile`) in envd and `toolbox/files/download`. A 1 GB artifact (Lewagent's `ImportArtifact`) holds 1 GB in toolboxd | Gateway docs | Stream with `io.Copy`, and support `Range` | Fork | P1 |
| F8 | The old multipart upload caps at 256 MB (`SB_UPLOAD_MAX_BYTES`) | Gateway uploads through envd instead | None needed; Lewbox uses envd's streamed upload | Lewbox | — |
| F9 | **No file watching through the API we use.** envd has `Filesystem/WatchDir`, unused | Planned (`docs/editor.md` in Lewagent) | Confirm `WatchDir` works under gVisor and Firecracker; fix if not | Fork | P2 |

### Snapshots, resume and the registry

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F10 | **A resumed sandbox never starts its image's command.** `docker commit` copies the container's entrypoint (`toolboxd`) into the snapshot; creating from it puts `toolboxd` in front again, so PID 1 is `toolboxd toolboxd <cmd>`, the inner one can't bind 2280 and exits. Each stop and start adds another | Live on EU West 2026-10-22, `v0.6.8` and `main` `v0.6.8-11`; desktop templates lost their desktop (`6e36f0a`) | `CreateSnapshot`: commit with `changes` restoring the image's own `ENTRYPOINT` and `CMD`; and on create, drop a leading `toolboxd` from the image's entrypoint | Fork. Lewbox keeps recording the command (`/proc/1/cmdline`) and sending `container_command` behind `FixResumeCommand`, and starting `lewbox-command` for legacy snapshots | P0 |
| F11 | **Snapshot registry path not lowercased.** `snapshotAOCRRef` lowercases the name but not `SB_AUTO_IMPORT_CLUSTER_ID`, so every push fails for ids with capitals | Live, first R2 cluster | Lowercase the whole repository path | Fork; Lewbox keeps lowercasing the id it sets (harmless) | P1 |
| F12 | **Snapshot status and deletion exist only on the Daytona facade** (`GET/DELETE /daytona/snapshots/{name}`), **only on the node that took the snapshot**, and aren't forwarded | Gateway asks each node in turn | `GET/DELETE /v1/snapshots/{name}` on `/v1`, forwarded to the owner like sandbox routes | Fork | P1 |
| F13 | **A node without push reports snapshots `active` at once,** though they never left it | Gateway offloads only when the cluster has cloud storage | `GET /v1/snapshots/{name}` reports `local` vs `pushed` | Both | P2 |
| F14 | **Layers stack with every stop.** Each snapshot is a `docker commit` on top of the last; overlay caps an image at ~125–127 layers, deleted files still take space and download on every resume | Legacy `docs/todo.md` l.61, `docs/clusters.md` § Risks | `POST /v1/sandboxes/{id}/snapshot` takes `flatten: true` (export and re-import the filesystem as one layer, or squash); Lewbox asks for it when a sandbox's layer count passes a threshold ([storage-and-registry](storage-and-registry.md#flattening)) | Both | P1 |
| F15 | **Snapshot uploads are slow:** `docker push` gzips each layer on one core (~26 MB/s), though the node uploads at ~147 MB/s. 512 MB of new data took ~65 s to stop (29 snapshot, 35 upload), 17 s to resume | Live, R2 cluster (`docs/todo.md` l.47) | Push with zstd, or parallel gzip (pigz); configurable | Fork | P2 |
| F16 | **Snapshot images pulled for a resume stay on the node forever**, offloaded or deleted (~5 GB after a few test resumes), and there's no API to delete an image | Live (`docs/todo.md` l.44–46) | Remove a snapshot's pulled image when its sandbox is destroyed; `DELETE /v1/images/{ref}` | Fork. Until then the node agent's cleanup ([node-agent](node-agent.md#snapshot-image-cleanup)) | P1 |
| F17 | **`SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` ignores a bare number** and keeps 5 minutes; failed uploads held stops that long | `docs/clusters.md` § Registry | Validate every `SB_*` setting at start-up, and refuse to start on a bad value | Fork | P3 |
| F18 | **`SB_IMAGE_DISTRIBUTION_AOCR_HOST` defaults to `aocr.aerol.ai`, and the cluster PAT is presented to every host in that list.** A node not configured otherwise sends its PAT to a third party | `docs/clusters.md` § Registry | Default to empty; never send credentials to a host not configured | Fork, and report upstream as a security issue (privately) | P1 |
| F19 | `SB_AUTO_IMPORT_ENABLED` calls an AOCR-only service | Left off | Nothing; the node spec keeps it off | Lewbox | — |
| F46 | **Snapshots keep whatever is on the container's disk, credentials included,** and timer stops give nothing inside the sandbox a chance to clean up. Legacy accepted it; the rebuild doesn't ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)) | Legacy `docs/clusters.md` § Sandbox offload; `sandboxes/persistence` | `scrub: {remove, read}` on `POST /v1/sandboxes/{id}/snapshot`, and `POST /v1/sandboxes/{id}/scrub` alone: on the **stopped** container, from the host, delete the paths from the overlay upper directory (whiteouts where a lower layer has them; symlinks removed, not followed) or from a Firecracker `rootfs.ext4` (`debugfs -w`); return `read` paths' contents first (64 KiB each); check the committed layer holds none of the paths except as whiteouts; push only after the check; answer with a per-path report | Fork; Lewbox checks the report and refuses a snapshot without one. Until the fleet has it, Lewbox scrubs by restarting in place | P0 |
| F47 | **`docker commit` copies the container's config, `Env` included,** so a sandbox's create-time `env` (often secrets) lands in the snapshot's image config in the registry. Legacy commits with only `ENV SB_POOL_PARKED=` changed (`docs/todo.md` l.58), so the rest is the container's. To confirm on a live node first | Code reading; confirm live | `reset_config: true` on snapshot: commit with the base image's `Env`, `Entrypoint` and `Cmd` (folding F10 in); Lewbox already sends `env` again on resume | Fork. Until then, Lewbox refuses `env` on persistent sandboxes on that cluster | P0 |
| F48 | **No memory-only place for secrets:** setup has to write credentials to the container's disk | Design | Every sandbox gets a `tmpfs` at `/run/lewbox` (0700, 16 MiB; `HostConfig.Tmpfs` on Docker and gVisor, mounted by the guest's init on Firecracker), never committed | Fork; on releases without it Lewbox uses a plain directory and relies on F46 | P1 |
| F49 | **No pre-stop script:** AerolVM's own stops (timers, and the F50 backstop) run nothing in the sandbox first, so the user's `on_stop` hooks and Lewbox's in-sandbox cleanup can't run | Legacy `sandboxes/persistence` design; the user's hooks ([sandboxes/hooks](../sandboxes/hooks.md#when-lewbox-cant-act)) | `pre_stop: {command, timeout}` on create and `PUT …/lifecycle`: before any stop AerolVM makes itself, the toolbox runs it (as a login shell, with the sandbox's env) for up to `timeout` (≤ 15 minutes), then stops; the result (exit code, duration) is in the `stopped` event. Lewbox registers `sh /run/lewbox/hooks/on-stop.sh` with `stop_grace` + 15 s | Fork | P1 |
| F20 | **The registry is never cleaned:** AerolVM never deletes snapshots or template builds from it | Legacy `CleanUpRegistry` on the bucket | Stays Lewbox's: retention is Lewbox's policy ([storage-and-registry](storage-and-registry.md#garbage-collection)) | Lewbox | — |

### Runtimes and installation

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F21 | **gVisor keeps a sandbox's writes in its own overlay** (`/.gvisor.filestore.<id>`), which `docker commit` can't read: a persisted sandbox lost all its files over a stop and start | Live, EU West, 2026-10-21 (`bfa60f5`, legacy `acp.md` § *A stop and start lost the sandbox's files*) | `install.sh --with-gvisor` registers runsc with `--overlay2=none`; later, snapshot with `runsc tar rootfs-upper` to keep the faster overlay | Fork; the node spec asserts the flag either way | P0 |
| F22 | **gVisor nodes must list `docker` in `SB_HOST_RUNTIMES`** or image builds can't be placed (`SelectPlacement` with `RuntimeDocker`, `pkg/api/v1/build.go`) | Live, first gVisor cluster (`292f3f7`) | Place builds on any node with a Docker engine, whatever its sandbox runtimes | Fork; the node spec sets `docker,gvisor` meanwhile | P2 |
| F23 | **`install.sh` needs `file` but never installs it,** so a valid Caddy binary is refused as "did not return an ELF binary" | `docs/clusters.md` § Known upstream issues | Install `file`, or check the ELF header in shell | Fork | P2 |
| F24 | **Re-running `install.sh` rewrites runsc's entry and drops our flags** | `docs/clusters.md` | Merge into `daemon.json` rather than replace it | Fork | P2 |
| F25 | **Upstream releases can lack assets** (`v0.6.5`) | Live | Fork CI refuses to publish incomplete releases | Fork | P1 |
| F26 | **Firecracker snapshots aren't implemented** (`CreateSnapshot` answers "not implemented"), so Firecracker sandboxes can't persist or move | #785 | Upstream is building it, with registry push and pull. We test pre-releases on a bare-metal Firecracker cluster and contribute fixes; the fork carries it early if upstream's lands late | Upstream, then fork | P1 for `microvm` |
| F27 | **`install.sh` doesn't install Firecracker** (binary, jailer, guest kernel) | `docs/clusters.md` § Runtime options | Upstream's `--with-firecracker` (#785); until then the fork's `firecracker-artifacts` asset and the node agent | Upstream, then fork | P1 for `microvm` |
| F28 | **Codex's bubblewrap can't start under gVisor** (`bwrap: loopback: Failed RTM_NEWADDR: No child process`), so Codex only runs in full-access mode | Legacy `acp.md` l.143, 668 | Not AerolVM's bug. Investigate runsc network options; on Firecracker it works. Documented per runtime ([runtimes](runtimes.md#gvisor-gvisor)) | Lewbox (docs and agent modes), gVisor upstream | P3 |
| F29 | **Image builds reuse a cached build** of the same Dockerfile on the same node for a day; no way to skip the cache | `docs/clusters.md` § Templates | `no_cache: true` on `POST /v1/images/build` | Fork | P2 |
| F30 | **Builds have no build context:** a Dockerfile alone, so templates inline files as base64 (Lewagent's `{{ lewagent-drop.js }}` placeholders) | Lewagent `ComputerTemplateCommand` | `POST /v1/images/build` takes a context tarball (multipart or a URL) | Fork | P2 |
| F31 | **Build timeout too short** for desktop templates (10 minutes default) | Live (`a207232`) | Nothing: `SB_IMAGE_BUILD_TIMEOUT` set by the node spec (20 m) | Lewbox | — |

### API, clustering and lifecycle

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F32 | **Builds block, and a create whose client disconnects is dropped.** `POST /v1/images/build` answers only when done (up to 25 min); killing the gateway mid-create left nothing | Live | Async jobs: `POST /v1/images/builds` and creates return an id at once, carry on without the client, take an `Idempotency-Key`; `GET` to follow, with **build logs** streamed (F33) | Fork | P1 |
| F33 | **No build logs:** a failed build reports only Docker's last error | Gateway docs; Lewbox docs say "no build logs yet" | Build log stream (`GET /v1/images/builds/{id}/log`, follows) | Fork | P1 |
| F34 | **Capacity is refused, not queued, without a code:** `503` with `host capacity exceeded: …`, `cluster: target capacity exceeded after pending reservations`, `cluster: no worker placement target available`, matched by words (`e1573fa`) | Live | `code: capacity_exceeded` with a `reason` (`memory`, `cpu`, `disk`, `no_placement_target`, `gpu`) | Fork; Lewbox keeps phrase matching as a fallback for one release | P1 |
| F35 | **AerolVM stops idle and old sandboxes silently,** so Lewbox's reconciler polls every 60 s, and works out why from the timers (`fb1fdc0`) | Gateway reconciler | An event stream: `GET /v1/events` (SSE, resumable) with sandbox state changes (and the reason: `idle`, `age`, `error`), session ends, snapshot pushes, node membership | Fork. Lewbox's reconciler stays, every 5 minutes, as a backstop ([sandboxes/reconciler](../sandboxes/reconciler.md)) | P0 (F50 needs it) |
| F36 | **Session ends aren't notified,** so watched sessions are polled every 5 s, and the polls keep sandboxes awake | `cf9813a` | Session ends in the event stream (F35) | Fork | P1 |
| F50 | **Timers stop sandboxes themselves,** so a timer stop can't run the sandbox's `on_stop` hooks through Lewbox, can't be deferred while Lewbox knows the sandbox is busy (an agent waiting on a person), and gives no warning | User feedback 2026-10-29; [sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop) | **Notify-mode timers:** `lifecycle.on_expiry: notify` with `warn_before` and `backstop_after`. In notify mode the timers emit `idle_warning` (with `stops_at`), `idle` and `lifetime_reached` on the event stream (F35) and don't stop; activity re-arms idle as usual; `PUT …/lifecycle {armed: false}` disarms (Lewbox's stop does this first). If neither happens within `backstop_after`, AerolVM runs the `pre_stop` script (F49), stops or destroys as `stop_*`/`destroy_*` says, and emits `stopped` with `backstop: true`. Events go to a local Unix socket too, so the node agent gets them without the network | Fork; proposed upstream with F35 | P0 |
| F37 | **No node named on a sandbox:** only `GET /v1/cluster/sandbox-index` says which node owns it; calls with `X-Cluster-Forwarded` from outside are refused (`403 cluster internal endpoint requires mTLS`) | Gateway docs | `owner_node_id` on `GET /v1/sandboxes/{id}` | Fork | P3 |
| F38 | **`stop_at_age` counts from AerolVM's create,** and a start doesn't reset it; the gateway moves it on each in-place start (`PUT …/lifecycle`) | Gateway docs | `lifecycle.reset_age_on_start: true` | Fork | P3 |
| F39 | **No per-sandbox resource usage** in the open-source build: its usage emitter is a no-op; only `network/usage` per sandbox | Legacy `docs/todo.md` l.240 | `GET /v1/sandboxes/{id}/stats` (cgroup CPU, memory, writable-layer size, network), not counted as activity | Fork. Until then the node agent reads cgroups ([health-and-metrics](health-and-metrics.md#sandbox-stats)) | P2 |
| F40 | **One credential for everything:** the operator PAT is the API credential *and* the registry password; per-tenant tokens need the private control plane | `docs/clusters.md` § Credentials | Scoped tokens in the open build: a registry-only credential; a read-only token for the node agent | Fork | P3 |
| F41 | **AerolVM doesn't renew node certificates** (90 days), though it reloads a replaced `node.crt` | `docs/clusters.md` | Stays Lewbox's ([pki-and-dns](pki-and-dns.md#renewal)) | Lewbox | — |
| F42 | **The trust bundle holds one CA,** so a cluster CA can't be rotated without cutting nodes off | Design | Accept several CAs in `ca.crt` for verification | Fork | P3 |
| F43 | **AerolVM's Caddy only does DNS-01 with Cloudflare,** with a zone-wide token copied to every node | `docs/clusters.md` § DNS | A Caddy DNS provider that asks the control plane to set the challenge record ([pki-and-dns](pki-and-dns.md#dns-01-through-the-control-plane)) | Fork (as a pluggable provider, proposed upstream) | P1 |
| F44 | **Memory overcommit** isn't settable per cluster the way Lewagent needs ("overprovision factor 1") | Lewagent `docs/todo.md` | Confirm the setting in sandboxd; expose it if missing ([placement-and-capacity](placement-and-capacity.md#overcommit)) | Fork if needed | P2 |
| F45 | **Disk isn't placed on by default** (`SB_DISK_RESERVATION_RATIO=0`), and counts only declared sizes | `docs/clusters.md` | Nothing: the node spec sets `SB_HOST_DISK_GB` and a ratio ([placement-and-capacity](placement-and-capacity.md#disk)) | Lewbox | — |

### To check on live nodes, before we rely on it

- containerd allows plain HTTP to `127.0.0.1:5000` without `hosts.toml`, as AerolVM moves OCI sandboxes to containerd.
- Sandboxes can't reach the host's `127.0.0.1:5000` (registry), `127.0.0.1:21212` (API) or `127.0.0.1:2019` (Caddy admin).
- Whether builds really use Docker's classic builder (heredocs must fail; legacy `docs/todo.md` l.96).
- Whether sandboxes on a node can reach each other's containers.
- Resuming on another node from a snapshot (built, never run live: `docs/todo.md` l.43).

## API

None of Lewbox's own. The fork's API is AerolVM's, plus the additions above, each documented in the fork's `docs/` and its OpenAPI spec, as upstream does.

## Security

- **Supply chain.** Assets are built by the fork's CI from a tagged commit, reproducibly where Go allows (`-trimpath`, fixed `-ldflags`), and signed. The node agent refuses anything unsigned, or not in Lewbox's supported table.
- **The fork's release key** is held by CI (keyless cosign with GitHub OIDC, or a minisign key in a protected environment). Its public part is compiled into `lewbox`.
- **Disclosure.** The fork has a `SECURITY.md` pointing at Harlew's address, and at upstream's process for issues that are theirs.
- **F18** (the PAT sent to AOCR by default) is reported privately first.

## Observability

- Each node's AerolVM version, and the cluster's features, on the cluster page and in `GET /v0/projects/{prj}/clusters/{clu}`.
- Metric `lewbox_aerolvm_workaround_total{fix="…"}`, counting each time a fallback ran, so we know when one is still needed.
- Dashboard warnings for clusters on `deprecated` releases.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_AEROLVM_DEFAULT_VERSION` | Newest `supported` in the binary | The release new clusters get |
| `LEWBOX_AEROLVM_RELEASES_URL` | `https://github.com/harlew-dev/aerolvm/releases/download` | Where the node agent downloads assets, so an air-gapped instance can mirror them |

## Testing

- **The fork's CI:**
  - upstream's unit tests;
  - our patches' unit tests;
  - an end-to-end suite on three self-hosted runners (Docker, gVisor, and a bare-metal Firecracker host with `/dev/kvm`), covering each backlog fix: an exit code under runsc, a resume running its command, a session `if_absent`, a streamed 2 GB download, a snapshot that flattens;
  - an upgrade test: a 3-node cluster on release N rolled to N+1 node by node, with sandboxes running throughout.
- **In Lewbox,** `internal/testing/aerolvmfake` plays a given release's behaviour, bugs included:
  - `SetExecLosesStatus`, `SetSessionsLoseStatus` and `ToolboxWarmUp` carry over from the legacy fake;
  - new: `NestedToolboxOnResume`, `CapacityErrorStyle(words|codes)`, `EventStream(on|off)`, `NotifyTimers` (F50), `PreStopScript` (F49).
  - Every workaround is tested both ways.
- **A nightly job** runs Lewbox's live suite ([foundation/testing](../foundation/testing.md)) against a real cluster on the newest fork release.

## Build plan

1. **M0:**
   - Create `harlew-dev/aerolvm`, with the upstream mirror, the CI pipeline, release assets and signing, and `LEWBOX_PATCHES.md`.
   - Release `v0.6.8-lewbox.1` (or upstream's newest) with no patches, to prove the pipeline.
2. **M0:** `internal/aerolvm/versions.go` with `Features`; nodes report versions.
3. **M1 (P0s):**
   - F1 exit statuses;
   - F10 resume command;
   - F21 gVisor overlay;
   - F18 AOCR default (P1, but a security fix, so first);
   - F46 snapshot scrub and F47 config reset (credentials never in snapshots);
   - F50 notify-mode timers, with the F35 events they ride on (Lewbox decides when to stop, so hooks run before timer stops), and F49 the pre-stop script for the backstop.
4. **M1–M3 (P1s), roughly in order:**
   - F36 session events (F35's stream lands with F50 in M1);
   - F32/F33 async builds with logs;
   - F34 capacity codes;
   - F12 snapshots on `/v1`;
   - F7 streamed downloads;
   - F5 activity;
   - F14 flatten;
   - F16 image cleanup;
   - F2 unique sessions;
   - F43 DNS provider;
   - F6 errors;
   - F11, F25;
   - F48 `/run/lewbox` tmpfs.
5. **As upstream lands #785:** test Firecracker snapshots and install on our bare-metal runner; `microvm` sandboxes persist. (`microvm` itself is offered as soon as a fork release runs Firecracker at all, ephemeral until then) ([runtimes](runtimes.md#firecracker-microvm)).
6. **Beta:** the P2s. **Later:** the P3s.
7. **Throughout:** each fix upstreamed once it has run live; each Lewbox workaround deleted once unsupported releases are gone.

## Open questions

1. **The fork's name and home:** `harlew-dev/aerolvm` (as the README suggests), or keep upstream's `microvm`? Does the fork's Go module path change (it must, to be importable alongside upstream), and does Lewbox import the fork's `pkg/api` types, or keep its own client types?
2. **Signing:** cosign keyless (GitHub OIDC, verifiable without our key) or minisign (small, no transparency log)? The node agent needs one of them compiled in.
3. **How far ahead of upstream do we go?** Events (F35) and async builds (F32) are big changes. Should we propose them upstream *before* building, so we don't carry large patches for long?
4. **Who owns the upstream relationship**, and should we ask the maintainer about a shared roadmap (they've been responsive on #785)?
5. ~~**Firecracker's timeline.**~~ Decided (2026-10-29): Firecracker isn't held back. Clusters offer `microvm` as soon as the fork runs Firecracker, without persistence until #785's snapshots are in a fork release ([runtimes](runtimes.md#firecracker-microvm)).
