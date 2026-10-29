# The AerolVM fork

> Status: Draft · Milestone: Alpha (M0 for the fork and its pipeline; the P0 fixes in M0–M2) · Updated: 2026-10-29

Lewbox runs sandboxes on [AerolVM](https://github.com/aerol-ai/microvm) (`aerol-ai/microvm`, MIT), a Go daemon (`sandboxd`) on every node, with a daemon inside every sandbox (`toolboxd`). Legacy Lewbox used upstream releases as they were, and worked around their bugs in three places: the install script, the gateway and the app. We now keep **our own fork**. Bugs are fixed in the fork first and offered upstream as pull requests, and the fork is rebased onto each upstream release ([D3](../README.md#decisions)). **Lewbox carries no workarounds** ([D15](../README.md#decisions)): every legacy workaround becomes a fix in the fork, and Lewbox requires a minimum fork version instead of keeping fallback code for older ones ([No workarounds in Lewbox](#no-workarounds-in-lewbox)).

**The alpha runs Docker only** ([D16](../README.md#decisions)): clusters offer the `container` isolation level. gVisor comes in the beta, once the fork handles it properly, and Firecracker once upstream ships it ([runtimes](runtimes.md)). So the alpha's must-fix list is what a Docker cluster needs; gVisor- and Firecracker-only items wait for their runtime.

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
4. Know exactly what each node runs, from the versions nodes report, not from a pin.
5. **No workarounds in Lewbox.** A bug is fixed in the fork, and Lewbox requires the release with the fix. Lewbox has one code path per behaviour, not one per fork version.

### Non-goals

- Diverging from upstream: a feature only Lewbox wants goes behind a flag or an extension point, never into a rewrite.
- AerolVM's managed control plane (`pkg/controlplane`, private upstream), and its hosted registry (AOCR).
- Maintaining WASM or Isolate runtimes. Lewbox doesn't use them.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | AerolVM as the runtime: its clustering (Raft, gossip, mTLS), scheduler, toolbox, Caddy ingress, and Docker support (gVisor in the beta, Firecracker when upstream ships it). The snapshot/registry offload model ([storage-and-registry](storage-and-registry.md)) |
| **Change** | Upstream releases → fork releases `vX.Y.Z-lewbox.N`. One pin for everything → each node reports its version, and Lewbox works out each cluster's features from it. Workarounds → fixes in the fork, and a **minimum fork version** each Lewbox release requires; no fallback code. Hand-run fix scripts → node specs applied by the node agent ([node-agent](node-agent.md#desired-state)) |
| **Drop** | `packages/laravel-aerolvm` (the unused PHP client). **Every legacy workaround**: exit-status traps, `container_command` and the `lewbox-command` session, listing sessions before starting a proxy, phrase matching on capacity errors, finding nodes through the sandbox index, asking each node for snapshot status through the Daytona facade, touching the toolbox while streams move, guessing readiness from `502`/`503`, the verified `chmod`, lowercasing the cluster id, installing `file` before `install.sh`, the snapshot-image cleanup timer, and the hand-run fix scripts. Each is a fix in the fork ([backlog](#fix-backlog)) |

## Design

### The repository

- **Name:** `github.com/harlew-dev/aerolvm`, a GitHub fork of `aerol-ai/microvm`, MIT, keeping upstream's copyright and adding ours for our changes. Its **Go module path is `github.com/harlew-dev/aerolvm`**, so it can be imported alongside upstream.
- **Lewbox imports the fork's own Go client** (`github.com/harlew-dev/aerolvm/pkg/client`, with the API types it already has under `pkg/api`) rather than keeping its own copy, as the legacy gateway did. Where upstream has no client package, the fork adds one and offers it upstream. Lewbox's `internal/aerolvm` shrinks to a thin layer: the table of releases and the minimum ([Versions in Lewbox](#versions-in-lewbox)), mapping the fork's coded errors to Lewbox's, and the dialer that reaches a node (direct now, the tunnel in the beta).
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
4. Run CI ([Testing](#testing)), including the end-to-end suite (Docker for the alpha; gVisor and Firecracker runners when those runtimes are supported).
5. Tag `vX.Y.Z-lewbox.1` and publish its assets ([Release assets](#release-assets)).
6. Update Lewbox's supported-version table ([Versions in Lewbox](#versions-in-lewbox)) in a Lewbox pull request. Nodes then upgrade by their cluster's policy ([node-agent](node-agent.md#upgrading-aerolvm)).

Target: a rebased release within 7 days of upstream's, or a note in the tracking issue saying why not, such as a regression found by our end-to-end suite and reported upstream.

### Upstreaming

- Every fix is written so it can go upstream: one concern per PR, upstream's style and tests, and no Lewbox names in code or messages.
- A PR goes upstream when the fix is in a fork release and has run on a live cluster for at least a few days, so we don't send fixes that are wrong.
- Fixes that only Lewbox needs, such as a Caddy DNS provider that asks Lewbox to set DNS-01 records, are proposed upstream as extension points (a pluggable DNS provider), not as Lewbox code.
- Security fixes are reported privately to the maintainer first, then fixed in the fork, and released once upstream agrees or after 14 days.
- One person owns the fork's upstream relationship (to be named). The maintainer has asked to be told when we test pre-releases (#785).

### Schedule and checkpoint

Decided 2026-10-29. The alpha needs every P0 below, which is a lot of fork work before 2026-12-21, so:

1. **Talk to the maintainer now.** They've been responsive on #785. Share the backlog, and propose the two biggest changes, the **event stream (F35)** and **notify-mode timers (F50)**, upstream as features *before* building them, so they're designed with upstream and we don't carry large patches for long. The rest go upstream as each fix proves itself ([Upstreaming](#upstreaming)).
2. **Work the P0s in the order the milestones need them:** M1's fixes first (sessions, readiness, coded errors, resume command, snapshots, capacity codes, `install.sh`, release assets), then M2's (events, notify timers, pre-stop, scrub and config reset, port traffic, touch).
3. **Checkpoint at the end of M1 (2026-11-23).** Any alpha feature whose fork fix isn't done (in a fork release, passing the contract suite) by then **moves to the beta**, with its plan and the README updated. There are no workarounds to fall back on ([No workarounds in Lewbox](#no-workarounds-in-lewbox)): if F50 slips, Lewbox-owned idle stops and `on_stop` hooks on timer stops move together; if F46/F47 slip, persistence ships without credential scrubbing only if the feature is cut, never with credentials left in snapshots.
4. **The checkpoint is written down** as a dated note in this plan: what made it, what moved, and the new fork targets for the beta.

### Release assets

Each fork release publishes, for `linux/amd64` and `linux/arm64`:

| Asset | What |
|---|---|
| `sandboxd-linux-<arch>` | The node daemon |
| `toolboxd-linux-<arch>` | The in-sandbox daemon, bind-mounted into every sandbox |
| `install.sh`, `cluster-init.sh`, `cluster-join.sh`, `uninstall.sh` | AerolVM's own scripts, fixed (see backlog); still used for host dependencies (Docker, gVisor, Firecracker, Caddy), as upstream maintains them ([installation](installation.md#aerolvms-own-installer)) |
| `firecracker-artifacts-<arch>.tar.zst` | *When Firecracker is supported (not the alpha):* the pinned Firecracker binary, jailer and guest kernel, until upstream's `install.sh` installs them (#785) |
| `checksums.txt` | SHA-256 of every asset |
| `checksums.txt.sig` | A signature of `checksums.txt` with the release key (minisign or cosign keyless, open question 2) |
| `sbom.spdx.json` | The software bill of materials |

- **CI refuses to publish a release that's missing an asset,** so a release like upstream's `v0.6.5` (no assets) can't happen in the fork.
- **The node agent verifies the signature and checksums** before installing anything ([node-agent](node-agent.md#upgrading-aerolvm)).

### Versions in Lewbox

Lewbox's code holds a table of the fork releases it knows (`internal/aerolvm/versions.go`), and one number that matters most, the **minimum** release it works with:

```go
// The oldest fork release this Lewbox release runs on. Every fix Lewbox
// relies on is in it; there is no code for releases without them.
const MinimumRelease = "v0.6.8-lewbox.3"

type Release struct {
    Version      string     // "v0.6.8-lewbox.3"
    Upstream     string     // "v0.6.8"
    Assets       Checksums
    Isolation    []string   // levels it can run: ["container"], later ["container", "gvisor"], ...
    Status       Status     // supported | deprecated | unsupported
}
```

- **Every node reports its running version,** `sandboxd --version`, with each heartbeat ([health-and-metrics](health-and-metrics.md)), recorded as `nodes.aerolvm_version`.
- **A node below the minimum isn't used.** It can't join a cluster (the installation fails with `aerolvm_version_unsupported`, naming the minimum); an existing node that falls below it after a Lewbox upgrade is marked `outdated`: placement skips it, its cluster shows `degraded` with *"Upgrade AerolVM on node-… to v0.6.8-lewbox.3 or later"*, and its running sandboxes keep running until they stop. The node agent upgrades it by the cluster's policy ([node-agent](node-agent.md#upgrading-aerolvm)).
- **A Lewbox release raises the minimum** only together with the fork release it needs, published first; its release notes say so, and the node agent can upgrade a cluster before the control plane is upgraded.
- **A cluster has a target version,** `clusters.aerolvm_target_version`, which the project or the instance's default sets, never below the minimum. Nodes converge on it.
- **Capabilities, not workarounds.** A release's `Isolation` says which levels its nodes can run, so a cluster offers `gvisor` only once its nodes run a release that supports it properly. That's choosing what to offer, not a second code path for a bug.
- **Deprecating a release:** `deprecated` in a Lewbox release, with dashboard warnings on clusters that run it; `unsupported` (below the minimum) one Lewbox minor release later.
- **New clusters** get the newest `supported` release.
- **Mixed versions within a cluster** are allowed only during a rolling upgrade, one minor version at a time, all at or above the minimum; AerolVM's Raft state must be compatible between them (checked in the fork's upgrade test).

### No workarounds in Lewbox

Legacy worked around AerolVM's bugs in the install script, the gateway and the app, and paid for it: workarounds hid bugs that were never fixed or reported, needed hand-run scripts on older nodes, and every new feature had to know about each of them. The rebuild doesn't carry any ([D15](../README.md#decisions)):

1. **A bug in AerolVM is fixed in the fork,** with a test in the fork's suite, and offered upstream ([Upstreaming](#upstreaming)).
2. **Lewbox raises its minimum release** to one with the fix, and is written as if the bug never existed.
3. **If a fix isn't ready, the feature that needs it waits,** or ships with a documented limitation. It doesn't get a workaround. The [backlog](#fix-backlog) marks which fixes block the alpha.
4. **Ordinary resilience isn't a workaround.** Retrying a network error, or waiting for a readiness signal the fork reports, is normal client behaviour. Guessing a state from a `502`, or a reason from an error's wording, is a workaround, and the fork reports it properly instead.
5. **The fake plays the fixed fork.** `internal/testing/aerolvmfake` models the minimum release's behaviour, not legacy bugs ([Testing](#testing)).

## Fix backlog

Every known AerolVM bug or gap Lewbox has hit, from legacy docs, commits and live clusters, with what we do about it. **P0** blocks the alpha (every fix that replaces a legacy workaround on Docker, and every fork change an alpha feature depends on), **P1** is wanted for the alpha (if it slips, the feature waits or ships with a documented limitation, never a workaround), **P2** comes after the alpha, **P3** is nice to have. **gVisor** and **Firecracker** mark items that only matter for those runtimes, which aren't in the alpha: they're due when the runtime is ([runtimes](runtimes.md)).

**Where** says where it's fixed: *Fork* means the fork fixes it and Lewbox requires the release with the fix; *Lewbox* means it's Lewbox's own job, not a workaround (a policy, or configuration it sets).

### Process and exec (toolboxd)

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F1 | **Exit statuses lost under gVisor.** toolboxd, as PID 1, reaps a command before it waits for it; it recovers from `ECHILD` but gVisor answers `ESRCH`, so the command reports `exit_code: -1` and `waitid: no such process`. Hits Execute, sessions (reported `killed`), exec streams, and login shells | Live on EU West, `v0.6.8` (`febb176`, `cf9813a`, `590bbc2`) | toolboxd: treat `ESRCH` as `ECHILD` and take the status from the reaper's table; better, register commands with the reaper before starting them so it never reaps them unseen. Test under runsc in CI | Fork, with gVisor. Docker clusters don't lose exit statuses, so the alpha needs nothing; Lewbox has no traps | gVisor |
| F2 | **Session names aren't unique.** Every create starts a new session, even with a running one's name; only SSH attach reuses one (`GetOrCreate`). A second private-port proxy was started, failed to bind, and the failed expose then killed the working proxy | Live, Lewagent's `sbx_S2NLeK88raX9nw`, 2026-10-27 (`5f3d19e`) | toolboxd: `POST sessions` takes `if_absent: true`, returning the running session with that name instead of starting another, atomically | Fork. Lewbox creates proxies and named sessions with `if_absent` and never lists first | P0 |
| F3 | **No pings on exec streams and attach.** Idle WebSockets get cut by proxies | Gateway design (`websocket.go` pings itself) | toolboxd: ping every 30 s | Fork | P3 |
| F4 | **Toolbox not ready for a few seconds after a start**: proxy `502 toolbox unavailable` (plain text) or `503 sandbox not adopted` | Gateway retries for `GATEWAY_TOOLBOX_WAIT` | sandboxd: a sandbox's `toolbox_ready` in `GET /v1/sandboxes/{id}`, and a JSON error with a code | Fork. Lewbox waits for `toolbox_ready` (from the event stream, F35, or the sandbox), then calls; it doesn't retry on `502`/`503` to guess | P0 |
| F5 | **Activity is counted only when a request starts,** not while a stream moves data, so a long download or a busy terminal lets the idle timeout stop the sandbox. Status calls count too, so polling a session keeps the sandbox awake | Gateway touches `toolbox/health` every `GATEWAY_ACTIVITY_INTERVAL`; watched sessions keep sandboxes awake (`cf9813a`) | toolboxd: count bytes moving on streams (at most once a minute); a header or route class that marks a request as not activity (status reads, watchers, stats) | Fork. Lewbox doesn't touch the toolbox while streams move, and watchers and status reads don't keep sandboxes awake | P0 |
| F6 | **Errors in three shapes:** AerolVM's `{"error"}`, envd's `{"code","message"}`, the proxy's plain text; `code` only sometimes; `400` for nearly everything, including an image that can't be pulled | `lewbox-gateway-legacy/docs/aerolvm.md` § Errors | One JSON error shape with a stable `code` (`not_found`, `capacity_exceeded`, `image_pull_failed`, `toolbox_unavailable`, …) on every route | Fork. Lewbox maps codes, never text | P0 |
| F53 | **No way to ask whether a port inside a sandbox is listening,** so a link to a desktop that isn't up opens an error page; Lewagent shells out `curl` before every link (`CheckPort.php`) | Lewagent | `GET toolbox/ports/{port}/status`: dials `127.0.0.1:<port>` from inside the sandbox (optionally `HEAD /`), answers `{listening, http_status}`, not counted as activity | Fork. Lewbox's port `check` and `wait` need it; no command-based probe ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md#readiness)) | P1 |

### Files

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F7 | **Downloads read whole into memory** (`os.ReadFile`) in envd and `toolbox/files/download`. A 1 GB artifact (Lewagent's `ImportArtifact`) holds 1 GB in toolboxd | Gateway docs | Stream with `io.Copy`, and support `Range` | Fork | P1 |
| F8 | The old multipart upload caps at 256 MB (`SB_UPLOAD_MAX_BYTES`) | Gateway uploads through envd instead | None needed; Lewbox uses envd's streamed upload | Lewbox | — |
| F9 | **No file watching through the API we use.** envd has `Filesystem/WatchDir`, unused | Planned (`docs/editor.md` in Lewagent) | Confirm `WatchDir` works under gVisor and Firecracker; fix if not | Fork (and check it on gVisor and Firecracker when they come) | P2 |
| F51 | **No conditional writes:** a write can't say "only if the file is still the one I read", so two editors overwrite each other (legacy compared `modified_at` first and lost the race) | Lewagent's editor (`lewagent-legacy/docs/editor.md`) | envd and `toolbox/files`: an ETag on reads (size + mtime + inode, or a content hash), and `If-Match` / `If-None-Match` on writes and moves, answering `precondition_failed` | Fork. Lewbox offers conditional writes once it's in the minimum release, with no compare-then-write fallback ([inside-a-sandbox/files](../inside-a-sandbox/files.md)) | P1 |
| F52 | **Helpers are uploaded into every sandbox at every start** (`portd`, `agentd`) | Gateway (`internal/privateports`, `internal/agents`) | Mount a node directory read-only into every sandbox, as `toolboxd` is mounted (`/opt/lewbox` → `/usr/local/lib/lewbox`) | Fork. Uploads go entirely once it's in the minimum release ([inside-a-sandbox/toolbox](../inside-a-sandbox/toolbox.md#lewboxs-helpers-in-the-sandbox)) | P3 |

### Snapshots, resume and the registry

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F10 | **A resumed sandbox never starts its image's command.** `docker commit` copies the container's entrypoint (`toolboxd`) into the snapshot; creating from it puts `toolboxd` in front again, so PID 1 is `toolboxd toolboxd <cmd>`, the inner one can't bind 2280 and exits. Each stop and start adds another | Live on EU West 2026-10-22, `v0.6.8` and `main` `v0.6.8-11`; desktop templates lost their desktop (`6e36f0a`) | `CreateSnapshot`: commit with `changes` restoring the image's own `ENTRYPOINT` and `CMD`; and on create, drop a leading `toolboxd` from the image's entrypoint | Fork, including legacy snapshots: on create, a leading `toolboxd` in a snapshot's entrypoint is dropped, so migrated sandboxes resume too. Lewbox records no command and starts no `lewbox-command` session | P0 |
| F11 | **Snapshot registry path not lowercased.** `snapshotAOCRRef` lowercases the name but not `SB_AUTO_IMPORT_CLUSTER_ID`, so every push fails for ids with capitals | Live, first R2 cluster | Lowercase the whole repository path | Fork. Lewbox passes the cluster id as it is | P0 |
| F12 | **Snapshot status and deletion exist only on the Daytona facade** (`GET/DELETE /daytona/snapshots/{name}`), **only on the node that took the snapshot**, and aren't forwarded | Gateway asks each node in turn | `GET/DELETE /v1/snapshots/{name}` on `/v1`, forwarded to the owner like sandbox routes | Fork. No Daytona facade, no asking node by node | P0 |
| F13 | **A node without push reports snapshots `active` at once,** though they never left it | Gateway offloads only when the cluster has cloud storage | `GET /v1/snapshots/{name}` reports `local` vs `pushed` | Fork | P2 |
| F14 | **Layers stack with every stop.** Each snapshot is a `docker commit` on top of the last; overlay caps an image at ~125–127 layers, deleted files still take space and download on every resume | Legacy `docs/todo.md` l.61, `docs/clusters.md` § Risks | `POST /v1/sandboxes/{id}/snapshot` takes `flatten: true` (export and re-import the filesystem as one layer, or squash); Lewbox asks for it when a sandbox's layer count passes a threshold ([storage-and-registry](storage-and-registry.md#flattening)) | Both: the fork flattens; when to ask is Lewbox's policy | P1 |
| F15 | **Snapshot uploads are slow:** `docker push` gzips each layer on one core (~26 MB/s), though the node uploads at ~147 MB/s. 512 MB of new data took ~65 s to stop (29 snapshot, 35 upload), 17 s to resume | Live, R2 cluster (`docs/todo.md` l.47) | Push with zstd, or parallel gzip (pigz); configurable | Fork | P2 |
| F16 | **Snapshot images pulled for a resume stay on the node forever**, offloaded or deleted (~5 GB after a few test resumes), and there's no API to delete an image | Live (`docs/todo.md` l.44–46) | Remove a snapshot's pulled image when its sandbox is destroyed; `DELETE /v1/images/{ref}` | Fork. No snapshot-image cleanup in the node agent | P0 |
| F17 | **`SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` ignores a bare number** and keeps 5 minutes; failed uploads held stops that long | `docs/clusters.md` § Registry | Validate every `SB_*` setting at start-up, and refuse to start on a bad value | Fork | P3 |
| F18 | **`SB_IMAGE_DISTRIBUTION_AOCR_HOST` defaults to `aocr.aerol.ai`, and the cluster PAT is presented to every host in that list.** A node not configured otherwise sends its PAT to a third party | `docs/clusters.md` § Registry | Default to empty; never send credentials to a host not configured | Fork, and report upstream as a security issue (privately) | P0 |
| F19 | `SB_AUTO_IMPORT_ENABLED` calls an AOCR-only service | Left off | Nothing; the node spec keeps it off | Lewbox | — |
| F46 | **Snapshots keep whatever is on the container's disk, credentials included,** and timer stops give nothing inside the sandbox a chance to clean up. Legacy accepted it; the rebuild doesn't ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)) | Legacy `docs/clusters.md` § Sandbox offload; `sandboxes/persistence` | `scrub: {remove, read}` on `POST /v1/sandboxes/{id}/snapshot`, and `POST /v1/sandboxes/{id}/scrub` alone: on the **stopped** container, from the host, delete the paths from the overlay upper directory (whiteouts where a lower layer has them; symlinks removed, not followed) or from a Firecracker `rootfs.ext4` (`debugfs -w`); return `read` paths' contents first (64 KiB each); check the committed layer holds none of the paths except as whiteouts; push only after the check; answer with a per-path report | Fork. Lewbox checks the report and refuses a snapshot without one; there is no scrub-by-restart fallback | P0 |
| F47 | **`docker commit` copies the container's config, `Env` included,** so a sandbox's create-time `env` (often secrets) lands in the snapshot's image config in the registry. Legacy commits with only `ENV SB_POOL_PARKED=` changed (`docs/todo.md` l.58), so the rest is the container's. To confirm on a live node first | Code reading; confirm live | `reset_config: true` on snapshot: commit with the base image's `Env`, `Entrypoint` and `Cmd` (folding F10 in); Lewbox already sends `env` again on resume | Fork. Lewbox sends `env` again on resume | P0 |
| F48 | **No memory-only place for secrets:** setup has to write credentials to the container's disk | Design | Every sandbox gets a `tmpfs` at `/run/lewbox` (0700, 16 MiB; `HostConfig.Tmpfs` on Docker and gVisor, mounted by the guest's init on Firecracker), never committed | Fork. No plain-directory fallback | P0 |
| F49 | **No pre-stop script:** AerolVM's own stops (timers, and the F50 backstop) run nothing in the sandbox first, so the user's `on_stop` hooks and Lewbox's in-sandbox cleanup can't run | Legacy `sandboxes/persistence` design; the user's hooks ([sandboxes/hooks](../sandboxes/hooks.md#when-lewbox-cant-act)) | `pre_stop: {command, timeout}` on create and `PUT …/lifecycle`: before any stop AerolVM makes itself, the toolbox runs it (as a login shell, with the sandbox's env) for up to `timeout` (≤ 15 minutes), then stops; the result (exit code, duration) is in the `stopped` event. Lewbox registers `sh /run/lewbox/hooks/on-stop.sh` with `stop_grace` + 15 s | Fork | P0 |
| F20 | **The registry is never cleaned:** AerolVM never deletes snapshots or template builds from it | Legacy `CleanUpRegistry` on the bucket | Stays Lewbox's: retention is Lewbox's policy ([storage-and-registry](storage-and-registry.md#garbage-collection)) | Lewbox | — |

### Runtimes and installation

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F21 | **gVisor keeps a sandbox's writes in its own overlay** (`/.gvisor.filestore.<id>`), which `docker commit` can't read: a persisted sandbox lost all its files over a stop and start | Live, EU West, 2026-10-21 (`bfa60f5`, legacy `acp.md` § *A stop and start lost the sandbox's files*) | `install.sh --with-gvisor` registers runsc with `--overlay2=none`; later, snapshot with `runsc tar rootfs-upper` to keep the faster overlay | Fork, with gVisor; the node spec asserts the flag | gVisor |
| F22 | **gVisor nodes must list `docker` in `SB_HOST_RUNTIMES`** or image builds can't be placed (`SelectPlacement` with `RuntimeDocker`, `pkg/api/v1/build.go`) | Live, first gVisor cluster (`292f3f7`) | Place builds on any node with a Docker engine, whatever its sandbox runtimes | Fork, with gVisor | gVisor |
| F23 | **`install.sh` needs `file` but never installs it,** so a valid Caddy binary is refused as "did not return an ELF binary" | `docs/clusters.md` § Known upstream issues | Install `file`, or check the ELF header in shell | Fork. The node agent doesn't install `file` first | P0 |
| F24 | **Re-running `install.sh` rewrites runsc's entry and drops our flags** | `docs/clusters.md` | Merge into `daemon.json` rather than replace it | Fork, with gVisor | gVisor |
| F25 | **Upstream releases can lack assets** (`v0.6.5`) | Live | Fork CI refuses to publish incomplete releases | Fork | P0 |
| F26 | **Firecracker snapshots aren't implemented** (`CreateSnapshot` answers "not implemented"), so Firecracker sandboxes can't persist or move | #785 | Upstream is building it, with registry push and pull. We test pre-releases on a bare-metal Firecracker cluster and contribute fixes; the fork carries it early if upstream's lands late | Upstream, then fork | Firecracker |
| F27 | **`install.sh` doesn't install Firecracker** (binary, jailer, guest kernel) | `docs/clusters.md` § Runtime options | Upstream's `--with-firecracker` (#785); until then the fork's `firecracker-artifacts` asset and the node agent | Upstream, then fork | Firecracker |
| F28 | **Codex's bubblewrap can't start under gVisor** (`bwrap: loopback: Failed RTM_NEWADDR: No child process`), so Codex only runs in full-access mode | Legacy `acp.md` l.143, 668 | Not AerolVM's bug. Investigate runsc network options; on Firecracker it works. Documented per runtime ([runtimes](runtimes.md#gvisor-gvisor-beta)) | Not AerolVM's bug. Under gVisor, documented when gVisor comes. Under Docker, whether Codex's own sandbox works in a container is a spike for the alpha ([agents/models-and-modes](../agents/models-and-modes.md)) | gVisor |
| F29 | **Image builds reuse a cached build** of the same Dockerfile on the same node for a day; no way to skip the cache | `docs/clusters.md` § Templates | `no_cache: true` on `POST /v1/images/build` | Fork | P2 |
| F30 | **Builds have no build context:** a Dockerfile alone, so templates inline files as base64 (Lewagent's `{{ lewagent-drop.js }}` placeholders) | Lewagent `ComputerTemplateCommand` | `POST /v1/images/build` takes a context tarball (multipart or a URL) | Fork | P2 |
| F31 | **Build timeout too short** for desktop templates (10 minutes default) | Live (`a207232`) | Nothing: `SB_IMAGE_BUILD_TIMEOUT` set by the node spec (20 m) | Lewbox | — |

### API, clustering and lifecycle

| # | Problem | Seen | Fix | Where | P |
|---|---|---|---|---|---|
| F32 | **Builds block, and a create whose client disconnects is dropped.** `POST /v1/images/build` answers only when done (up to 25 min); killing the gateway mid-create left nothing | Live | Async jobs: `POST /v1/images/builds` and creates return an id at once, carry on without the client, take an `Idempotency-Key`; `GET` to follow, with **build logs** streamed (F33) | Fork | P1 |
| F33 | **No build logs:** a failed build reports only Docker's last error | Gateway docs; Lewbox docs say "no build logs yet" | Build log stream (`GET /v1/images/builds/{id}/log`, follows) | Fork | P1 |
| F34 | **Capacity is refused, not queued, without a code:** `503` with `host capacity exceeded: …`, `cluster: target capacity exceeded after pending reservations`, `cluster: no worker placement target available`, matched by words (`e1573fa`) | Live | `code: capacity_exceeded` with a `reason` (`memory`, `cpu`, `disk`, `no_placement_target`, `gpu`) | Fork. No phrase matching in Lewbox | P0 |
| F35 | **AerolVM stops idle and old sandboxes silently,** so Lewbox's reconciler polls every 60 s, and works out why from the timers (`fb1fdc0`) | Gateway reconciler | An event stream: `GET /v1/events` (SSE, resumable) with sandbox state changes (and the reason: `idle`, `age`, `error`), session ends, snapshot pushes, node membership | Fork. Lewbox's reconciler stays, every 5 minutes, as a safety net for missed events ([sandboxes/reconciler](../sandboxes/reconciler.md)), not as a substitute | P0 |
| F36 | **Session ends aren't notified,** so watched sessions are polled every 5 s, and the polls keep sandboxes awake | `cf9813a` | Session ends in the event stream (F35) | Fork. Watched sessions aren't polled | P0 |
| F50 | **Timers stop sandboxes themselves,** so a timer stop can't run the sandbox's `on_stop` hooks through Lewbox, can't be deferred while Lewbox knows the sandbox is busy (an agent waiting on a person), and gives no warning | User feedback 2026-10-29; [sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop) | **Notify-mode timers:** `lifecycle.on_expiry: notify` with `warn_before` and `backstop_after`. In notify mode the timers emit `idle_warning` (with `stops_at`), `idle` and `lifetime_reached` on the event stream (F35) and don't stop; activity re-arms idle as usual; `PUT …/lifecycle {armed: false}` disarms (Lewbox's stop does this first). If neither happens within `backstop_after`, AerolVM runs the `pre_stop` script (F49), stops or destroys as `stop_*`/`destroy_*` says, and emits `stopped` with `backstop: true`. Events go to a local Unix socket too, so the node agent gets them without the network | Fork; proposed upstream with F35 | P0 |
| F54 | **Traffic through an exposed port doesn't count as activity,** so a person using a desktop or a dev server through a port can be stopped as idle | Lewagent's desktops ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#fork-changes) T2) | AerolVM's proxy (Caddy route or TCP proxy) counts a request or a connection with bytes moving, at most once a minute per sandbox | Fork. No Lewbox touch from a page that shows a port | P0 |
| F55 | **No explicit touch, and every request counts:** Lewbox can't say "this sandbox is busy" for activity AerolVM can't see (an agent turn), and its own maintenance calls (token refreshes, setup) keep sandboxes awake | Gateway touched `toolbox/health`; refreshes every 5 minutes could keep a sandbox awake for good | `POST /v1/sandboxes/{id}/activity` (touch), and requests marked `X-Lewbox-Maintenance: 1` not counted ([timers-and-activity](../sandboxes/timers-and-activity.md#fork-changes) T3) | Fork | P0 |
| F37 | **No node named on a sandbox:** only `GET /v1/cluster/sandbox-index` says which node owns it; calls with `X-Cluster-Forwarded` from outside are refused (`403 cluster internal endpoint requires mTLS`) | Gateway docs | `owner_node_id` on `GET /v1/sandboxes/{id}` | Fork. Lewbox doesn't use the sandbox index | P0 |
| F38 | **`stop_at_age` counts from AerolVM's create,** and a start doesn't reset it; the gateway moves it on each in-place start (`PUT …/lifecycle`) | Gateway docs | `lifecycle.reset_age_on_start: true` | Fork. Lewbox doesn't move `stop_at_age` itself | P0 |
| F39 | **No per-sandbox resource usage** in the open-source build: its usage emitter is a no-op; only `network/usage` per sandbox | Legacy `docs/todo.md` l.240 | `GET /v1/sandboxes/{id}/stats` (cgroup CPU, memory, writable-layer size, network), not counted as activity | Fork. Sandbox stats come when it lands; the node agent doesn't read cgroups | P2 |
| F40 | **One credential for everything:** the operator PAT is the API credential *and* the registry password; per-tenant tokens need the private control plane | `docs/clusters.md` § Credentials | Scoped tokens in the open build: a registry-only credential; a read-only token for the node agent | Fork | P3 |
| F41 | **AerolVM doesn't renew node certificates** (90 days), though it reloads a replaced `node.crt` | `docs/clusters.md` | Stays Lewbox's ([pki-and-dns](pki-and-dns.md#renewal)) | Lewbox | — |
| F42 | **The trust bundle holds one CA,** so a cluster CA can't be rotated without cutting nodes off | Design | Accept several CAs in `ca.crt` for verification | Fork | P3 |
| F43 | **AerolVM's Caddy only does DNS-01 with Cloudflare,** with a zone-wide token copied to every node | `docs/clusters.md` § DNS | A Caddy DNS provider that asks the control plane to set the challenge record ([pki-and-dns](pki-and-dns.md#dns-01-through-the-control-plane)) | Fork (as a pluggable provider, proposed upstream). The alpha uses Cloudflare as legacy does, with a zone-scoped token on nodes for Caddy ([pki-and-dns](pki-and-dns.md)) | P2 |
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
- Nodes below the minimum release, as `outdated` on the cluster page, and metric `lewbox_nodes_outdated{cluster}`.
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
  - an end-to-end suite on a Docker runner for the alpha, covering each backlog fix: a resume running its command, a session `if_absent`, coded capacity errors, notify-mode timers and the backstop, a scrubbed snapshot, a streamed 2 GB download, a snapshot that flattens. gVisor and bare-metal Firecracker runners join when those runtimes do;
  - an upgrade test: a 3-node cluster on release N rolled to N+1 node by node, with sandboxes running throughout.
- **In Lewbox,** `internal/testing/aerolvmfake` plays the **minimum release** as the fork's contract says it behaves: coded errors, readiness events, notify-mode timers (F50), the pre-stop script (F49), the scrub report (F46), unique sessions (F2). It doesn't model legacy bugs, since Lewbox has no code for them. A contract test runs the same cases against a real node on the minimum release, so the fake can't drift from it.
- **A nightly job** runs Lewbox's live suite ([foundation/testing](../foundation/testing.md)) against a real cluster on the newest fork release.

## Build plan

1. **M0:**
   - Create `harlew-dev/aerolvm` (module `github.com/harlew-dev/aerolvm`), with the upstream mirror, the CI pipeline, release assets and signing, and `LEWBOX_PATCHES.md` (F25); its `pkg/client` Go package, which Lewbox imports.
   - Contact the maintainer with the backlog; propose F35 and F50 upstream.
   - Release `v0.6.8-lewbox.1` (or upstream's newest) with no patches, to prove the pipeline.
   - `internal/aerolvm/versions.go` with `MinimumRelease`; nodes report versions; nodes below it are refused.
   - First fixes, as they're small or urgent: F18 (AOCR default, security), F11 (lowercase path), F23 (`file` in `install.sh`), F6 and F34 (coded errors, capacity codes).
2. **M1 (P0s the sandbox lifecycle needs):** F10 resume command (including legacy snapshots); F12 snapshots on `/v1`; F16 image cleanup; F37 owner node; F38 lifetime on start; F46 snapshot scrub and F47 config reset; F48 `/run/lewbox`; F35 event stream with F4 readiness; F50 notify-mode timers and F49 the pre-stop script (Lewbox decides when to stop).
3. **M2 (P0s inside a sandbox):** F2 unique sessions; F5 activity on streams; F36 session ends in the event stream.
4. **M1–M3 (P1s), roughly in order:** F32/F33 async builds with logs; F7 streamed downloads; F14 flatten. A P1 that misses the alpha leaves its feature with a documented limitation, never a workaround.
5. **Beta: gVisor** (F1, F21, F22, F24, F28 and a gVisor end-to-end runner), and the P2s, including F43 (DNS-01 through the control plane, so more DNS providers) and F39 (sandbox stats).
6. **When upstream ships Firecracker (#785):** F26, F27 and a bare-metal Firecracker runner.
7. **Later:** the P3s.
8. **Throughout:** each fix upstreamed once it has run live; `MinimumRelease` raised with each fix Lewbox relies on.

## Open questions

1. ~~**The fork's name and home.**~~ Decided (2026-10-29): `harlew-dev/aerolvm`, Go module `github.com/harlew-dev/aerolvm`. Lewbox imports the fork's own Go client (`pkg/client`, `pkg/api`) rather than keeping its own copy ([The repository](#the-repository)).
2. **Signing:** cosign keyless (GitHub OIDC, verifiable without our key) or minisign (small, no transparency log)? The node agent needs one of them compiled in.
3. ~~**How far ahead of upstream do we go?**~~ Decided (2026-10-29): propose the event stream (F35) and notify-mode timers (F50) upstream before building them; checkpoint at the end of M1, where any alpha feature whose fix isn't done moves to the beta ([Schedule and checkpoint](#schedule-and-checkpoint)).
4. ~~**Who owns the upstream relationship.**~~ Decided (2026-10-29): one owner, still to be named; the action is to contact the maintainer now about a shared roadmap (#785) ([Schedule and checkpoint](#schedule-and-checkpoint)).
5. ~~**Firecracker's timeline.**~~ Decided (2026-10-29): not in the alpha, as upstream hasn't released it. Clusters offer `microvm` once a fork release runs Firecracker with upstream's snapshot support (#785) ([runtimes](runtimes.md#firecracker-microvm-later)). gVisor isn't in the alpha either: it comes in the beta, once the fork handles it properly.
