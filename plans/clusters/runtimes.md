# Runtimes and isolation

> Status: Draft · Milestone: Alpha (M1: `container` only); `gvisor` in the Beta; `microvm` Later, once upstream ships Firecracker · Updated: 2026-10-29

Lewbox supports every runtime [AerolVM](aerolvm-fork.md) supports, Docker containers, gVisor containers and Firecracker microVMs, and lets people choose how strongly each sandbox is isolated ([D3](../README.md#decisions)). A cluster **offers** one or more **isolation levels**, each backed by a runtime on its nodes. A sandbox is created at one level and keeps it for life. A project decides which levels each profile may use. This plan says what each level is, how they're offered, chosen and enforced, and what Lewbox does differently for each.

**The alpha offers `container` (Docker) only** ([D16](../README.md#decisions)). gVisor comes in the **beta**, once the fork handles it properly (F1, F21, F22, F24), and Firecracker **later**, once upstream ships it with snapshots ([#785](https://github.com/aerol-ai/microvm/discussions/785)) and a fork release includes it. A level is offered only when the fork runs it without Lewbox working around anything ([D15](../README.md#decisions)). The model below (levels, what a cluster offers, what a profile may use) is built in the alpha, so adding `gvisor` and `microvm` later is offering them, not changing the API.

## Today

- **One runtime per cluster,** chosen by the project (then: tenant) when it creates the cluster, and never by a profile (`lewbox-legacy/docs/clusters.md` § *One runtime per cluster*). It's enforced twice:
  - the create-sandbox API has no runtime field;
  - each node is installed with `SB_CONTAINER_RUNTIME=<runtime>` and `SB_HOST_RUNTIMES`, so it refuses any other ("runtime … not supported on this host", `pkg/capacity/capacity.go`).
- **Docker** is always installed, and is the only runtime with GPUs.
- **gVisor** is enabled by `install.sh --with-gvisor`. Nodes list `docker,gvisor`, so image builds can be placed (`292f3f7`); runsc gets `--overlay2=none`, so snapshots keep files (`bfa60f5`). So **every gVisor node already runs two runtimes**: AerolVM's scheduler takes a runtime per request and places by `supported_runtimes`.
- **Firecracker** has no `install.sh` flag, needs `/dev/kvm`, and is **not selectable** (`Runtime::isSelectable()`), as its sandboxes can't be snapshotted ([aerolvm-fork F26](aerolvm-fork.md#runtimes-and-installation)).
- **GPU** is a separate setting (`gpu_vendor`: `nvidia` or `amd`), allowed with Docker only. AMD needs amd64 and Ubuntu (ROCm).
- `App\Enums\Cluster\Runtime` holds `installFlag()`, `hostRuntimes()`, `runscFlags()`, `supportsGpu()` and `isSelectable()`.
- Lewagent's site promises "every computer runs in its own microVM, with a dedicated kernel and hardware-level virtualization", which today's gVisor clusters don't deliver. Firecracker is the runtime that would.
- **Legacy's live clusters run gVisor** (EU West), so the alpha's `container`-only clusters isolate *less* than today's until gVisor returns in the beta. How legacy clusters and Lewagent's computers move is in [migration/data](../migration/data.md).

## Goals

- Support every runtime AerolVM supports, each once the fork runs it properly: Docker in the alpha, gVisor in the beta, Firecracker once upstream ships it.
- **People choose their isolation:** the project offers levels on its clusters and allows them per profile; a sandbox picks one on create, or gets a default.
- Model what each level can and can't do in one place, so the API refuses what a level can't do with a clear error, rather than failing late.
- No workarounds per runtime: a level whose runtime still needs one isn't offered yet ([D15](../README.md#decisions)).
- One architecture per cluster.

### Non-goals

- WASM and Isolate (AerolVM runs modules and JS bundles there, not images).
- Changing a sandbox's isolation after create. A sandbox keeps its level through every stop and start; a different level is a new sandbox.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Architecture fixed per cluster once its first node enrolls. GPU as a cluster setting, Docker only. The node-side enforcement (`SB_HOST_RUNTIMES`). The create call to AerolVM never takes a runtime from a consumer as-is |
| **Change** | One runtime per cluster → **a set of isolation levels per cluster**, with a default. A sandbox's `isolation` on create. A profile's **allowed isolation** and default, set by the project ([profiles](../profiles.md#isolation)). Runtime rules in a PHP enum → a Go `Isolation` with `Capabilities` that the API, the node spec, placement and the dashboard all read. Hand-run node fixes → fixes in the fork and a node spec ([node-agent](node-agent.md#desired-state)). gVisor: offered today → back in the beta, once the fork fixes it (F1, F21, F22, F24). Firecracker: not selectable → offered once upstream ships it with snapshots and a fork release includes it |
| **Drop** | "One runtime per cluster" as a rule. `LEWBOX_EXPERIMENTAL_FIRECRACKER` and any other flag for a half-supported runtime: a level is offered when the fork supports it, not before. Exit-status traps for gVisor (fixed in the fork, F1) |

## Design

### Isolation levels

The API names **levels**, ordered from weakest to strongest. Each maps to one AerolVM runtime today:

| Level | Runtime | What isolates the sandbox | Strength |
|---|---|---|---|
| `container` | Docker | Linux namespaces and cgroups, sharing the host kernel | Weakest. For trusted code, or development |
| `gvisor` | gVisor (runsc) | A user-space kernel (the Sentry) between the sandbox and the host kernel; the host sees a small set of syscalls | Strong; much smaller host-kernel attack surface |
| `microvm` | Firecracker | A KVM microVM with its own guest kernel; hardware virtualization | Strongest. What "microVM" means on Lewagent's site |

**Why these names** rather than the runtimes' (`docker`, `gvisor`, `firecracker`): they say what you get, not which program provides it, so `microvm` can later be served by another VMM (Cloud Hypervisor, say) without an API change, and "the sandbox runs in a container" is how people already say it. gVisor has no common generic name ("user-space kernel" isn't one people use), so its level is named after it. The order is part of the API (`container` < `gvisor` < `microvm`), so "at least `gvisor`" means something. The runtime behind a level is shown alongside it (`runtime` on the sandbox and the cluster), for people who need to know. Open question 1 keeps the alternative open.

### The runtimes

| | Docker (`container`) | gVisor (`gvisor`) | Firecracker (`microvm`) |
|---|---|---|---|
| **Images** | Any OCI image | Any OCI image, run by Docker with runsc | An ext4 rootfs built from an OCI image (an AerolVM *template*), plus an optional paused-VM memory snapshot |
| **Node needs** | Docker (installed by AerolVM) | cgroup v2 (`/sys/fs/cgroup` is `cgroup2fs`); runsc | `/dev/kvm` (bare metal, or nested virtualization); Firecracker, jailer and a guest kernel, pinned per cluster |
| **Snapshots (persist)** | Yes: `docker commit` of the writable layer, pushed to the registry | Yes, with `--overlay2=none` (F21) | Once upstream ships snapshots with registry push and pull ([#785](https://github.com/aerol-ai/microvm/discussions/785)); `microvm` isn't offered before then |
| **Memory kept on stop** | No | No | Possible later: Firecracker can snapshot memory, but restores only on the same CPU model, host kernel and Firecracker version |
| **GPU** | Yes (NVIDIA or AMD) | No | No |
| **Exit codes** | Reliable | Lost by toolboxd (F1) in today's AerolVM; fixed in the fork before `gvisor` is offered | Reliable (toolboxd in a full guest kernel) |
| **Nested sandboxing in the sandbox** (bubblewrap, Docker-in-sandbox) | Possible with privileges we don't give | bubblewrap fails (`RTM_NEWADDR`), so Codex runs in full-access mode only (F28); no Docker inside | Works: a real kernel |
| **File I/O speed** | Native | Slower, and slower again with `--overlay2=none` | Near native, on a virtio block device |
| **Start time** | Fast | Fast | Fast from a memory snapshot; slower cold boot |
| **Memory overcommit** | Shared page cache; overcommit works | As Docker | A VM's memory is reserved; overcommit capped at 1.0 ([placement](placement-and-capacity.md#overcommit)) |
| **Image builds** | On the node's Docker | On the node's Docker (so nodes list `docker`, F22) | Built as an OCI image with Docker, then converted to an AerolVM template ([templates/builds](../templates/builds.md#firecracker-clusters)) |
| **Snapshot format** | OCI image layers | OCI image layers (the same as Docker's) | A whole `rootfs.ext4` (plus memory, later) |

### Capabilities

`internal/clusters/isolation.go`:

```go
type Isolation string // "container" | "gvisor" | "microvm"; ordered

func (i Isolation) Runtime() aerolvm.Runtime // "docker" | "gvisor" | "firecracker"
func (i Isolation) AtLeast(min Isolation) bool

type Capabilities struct {
    Persist          bool // snapshots to the registry, so sandboxes can persist and resume elsewhere
    GPU              bool
    NestedSandbox    bool // bubblewrap, Docker in the sandbox
    MemorySnapshots  bool // later: resume with memory
    ImageKind        ImageKind // oci | firecracker_template
}

// For one level on one cluster: its nodes' fork releases, storage and GPU.
func (c *Cluster) Capabilities(i Isolation) Capabilities
```

Capabilities depend on the cluster as well as the level: the levels its nodes' fork releases run ([aerolvm-fork](aerolvm-fork.md#versions-in-lewbox)), whether it has storage, and its GPU. They describe what a level *can do*; they never switch on a workaround, because a level that needs one isn't offered ([D15](../README.md#decisions)).

They decide:

| Capability false | What Lewbox does |
|---|---|
| `Persist` | A create with `persist: true` is refused, `422 persistence_unsupported` with `reason: isolation` and the level, as on a cluster without storage ([sandboxes/persistence](../sandboxes/persistence.md)) |
| `GPU` | `gpu` on create is refused at that level (`422 validation_failed` on `gpu`) |
| `NestedSandbox` | Codex's modes other than full access are refused or mapped, and `GET /v0/sandboxes/{sbx}/agents` says why ([agents/models-and-modes](../agents/models-and-modes.md)) |

### What a cluster offers

- A cluster has `isolation_levels` (one or more of the three) and a `default_isolation` among them, set by the project. **In the alpha the only level is `container`**, so every cluster is `["container"]`; asking a cluster to offer another is `422 validation_failed` on `isolation_levels` ("not supported yet"). Later a homelab cluster might offer only `container`, and lewbox.com's `gvisor` and `microvm`, defaulting to `gvisor`.
- **Each node says which levels it can run.** The node agent's preflight detects what the host supports (Docker always; `gvisor` with cgroup v2; `microvm` with `/dev/kvm`), installs the runtimes for the levels the cluster offers that the host can run, and reports `isolation_levels` on the node ([node-agent](node-agent.md#desired-state)). A node without `/dev/kvm` in a cluster offering `microvm` still joins; it just won't run microVMs.
- **A level is offered only while some online node runs it.** A cluster whose only KVM node goes offline keeps `microvm` in its settings, but creates at that level wait for capacity as any full cluster does, then fail `cluster_capacity_exceeded` ([placement](placement-and-capacity.md#when-a-cluster-is-full)).
- **Changing what a cluster offers** is allowed at any time. Adding a level installs its runtime on nodes that can run it (the node spec changes). Removing one is refused while sandboxes at that level exist on the cluster (`409 isolation_in_use`, with a count); stopped persistent ones count, as they'd resume at that level.
- Architecture and GPU stay **locked** once the first node enrolls, as changing them means reinstalling every node and rebuilding every template and snapshot.

### What a profile may use

Set by the project, per profile ([profiles § Isolation](../profiles.md#isolation)):

- `allowed_isolation`: the levels this profile's sandboxes may use, or `null` for any level a cluster offers. A minimum is a list that starts there: `["gvisor", "microvm"]` means "at least gVisor".
- `default_isolation`: used when a create doesn't name one, or `null` for the cluster's default.
- The project has the same two settings as defaults for new profiles ([projects/settings](../projects/settings.md)).
- The instance can limit what projects may offer at all (`LEWBOX_ALLOWED_ISOLATION`), e.g. `gvisor,microvm` on lewbox.com.

A profile key can't change these; they're the project's, like limits.

### Choosing a sandbox's level

On create ([sandboxes/lifecycle](../sandboxes/lifecycle.md#create)), after the cluster is chosen:

1. **`isolation` given:** it must be offered by the cluster (else `422 isolation_unavailable`, `reason: not_offered`) and allowed for the profile (else `422 isolation_unavailable`, `reason: not_allowed`).
2. **Not given:** the profile's `default_isolation` if the cluster offers it; else the cluster's `default_isolation` if the profile is allowed it; else the strongest level both allow; else `422 isolation_unavailable` (`reason: none_allowed`).
3. Then capability checks for the chosen level: `persist`, `gpu` ([above](#capabilities)), and the template's availability at that level ([templates/builds](../templates/builds.md#firecracker-clusters)).

The sandbox records `isolation` (and `runtime`), and keeps them for life: a start resumes it at the same level, on a node that runs it. A snapshot is only resumed by the runtime that took it.

`isolation_unavailable` carries `isolation` (the level asked for or `null`), `cluster` (`id`, `name`), `reason`, and `available`: the levels this profile may use on this cluster, so a client can offer a choice.

### Enforcing it

- **In Lewbox:** the AerolVM create call takes the runtime from the sandbox's recorded level, never from anything else. A level not allowed for the profile never reaches AerolVM.
- **On the node,** by the node spec:
  - `SB_HOST_RUNTIMES`: the runtimes for the levels this node runs, plus `docker` for image builds where AerolVM needs it (F22). Because builds need Docker on every node, a node that runs only `gvisor` or `microvm` sandboxes still has Docker, used only for builds ([templates/builds](../templates/builds.md#security)).
  - `SB_CONTAINER_RUNTIME`: the cluster's default runtime, so a create that somehow omits it lands at the default rather than the weakest.
  - `SB_ENABLE_FIRECRACKER`, runsc flags, Firecracker artifacts.

  The node agent re-asserts the spec when it changes, and on every start ([node-agent](node-agent.md#desired-state)). This is how a fix like `--overlay2=none` reaches existing nodes without a script run by hand.
- **Placement:** AerolVM places by runtime among nodes that list it; Lewbox's own node choice for calls ([placement](placement-and-capacity.md#choosing-the-node-to-call)) filters by level too.

### Docker (`container`)

- The only level with GPUs.
- The dashboard labels it **"container isolation: shares the host's kernel; for trusted code"** wherever a level is chosen. Whether a project offers it for untrusted agents is their call, but they should know.
- **GPU:**
  - The node agent's preflight checks the driver: NVIDIA's `nvidia-smi -L`; AMD's `/dev/kfd` plus Ubuntu.
  - The cluster's GPU vendor is passed to AerolVM's `install.sh` (`--with-nvidia-gpu`, `--with-amd-gpu`).
  - Sandboxes ask for GPUs with a `gpu` field on create (count), only at `container` (open question 3).

### gVisor (`gvisor`): Beta

Not in the alpha. It's offered once the fork fixes what legacy worked around; each issue and its fix:

| Issue | Handled by |
|---|---|
| Writes hidden from `docker commit` unless `--overlay2=none` (F21) | The node spec sets the runsc flag in `/etc/docker/daemon.json` and reloads Docker (SIGHUP, which doesn't touch running containers). Only sandboxes started afterwards get it, so after a spec change the node agent reports how many running sandboxes still lack it, and the dashboard says to restart them |
| Builds need `docker` listed (F22) | Node spec: `docker` always in `SB_HOST_RUNTIMES`. Only Lewbox holds the PAT, so no caller can ask AerolVM for a Docker sandbox on its own |
| Exit codes lost (F1) | Fixed in the fork; Lewbox has no traps |
| bubblewrap can't start (F28) | Codex in full-access mode only; Claude Code's own sandboxing likewise if it uses bubblewrap. Documented to consumers, and shown by `GET …/agents` |
| Slower file I/O | Documented. Later: snapshots through `runsc tar rootfs-upper` so the faster overlay can stay |

Install-time requirements: cgroup v2, and runsc as AerolVM's `install.sh --with-gvisor` installs it.

### Firecracker (`microvm`): Later

Firecracker gives a dedicated kernel per sandbox: the isolation Lewagent promises. It isn't in the alpha, as upstream hasn't released it. **`microvm` is offered once upstream ships Firecracker with snapshots and registry push and pull (#785, F26, F27), and a fork release includes it**: we test upstream's pre-releases on a bare-metal runner meanwhile, but don't carry Firecracker in the fork ahead of upstream.

1. **No memory snapshots of sandboxes.** A stop saves the disk only, as on the other levels. A memory snapshot would hold every credential in every process's memory and in `/run/lewbox`, and memory can't be scrubbed, so it's not offered (decided 2026-10-29; [sandboxes/profile-setup](../sandboxes/profile-setup.md#memory-is-never-snapshotted)).

Firecracker specifics Lewbox handles:

- **Credentials and snapshots:** `/run/lewbox` is a `tmpfs` inside the guest, so it's never on the VM's disk; the snapshot's scrub works on the stopped VM's `rootfs.ext4` from the host (`debugfs -w`), before the disk is snapshotted ([aerolvm-fork F46, F48](aerolvm-fork.md#snapshots-resume-and-the-registry)).
- **Templates are AerolVM templates** (`POST /v1/templates` on AerolVM, operator only): a `rootfs.ext4` plus a paused-VM memory snapshot (taken at build, before any profile setup, so it holds no credential), built from the template's OCI image, and pushed to `<registry>/cluster/<id>/templates/<tid>`. Peers pull them lazily, and placement prefers nodes that already have one cached. A Lewbox template build on a cluster offering `microvm` therefore adds a conversion step ([templates/builds](../templates/builds.md#firecracker-clusters)).
- **Public images** (`image: ubuntu:24.04`) need the same conversion on create, so a `microvm` create from an image is slower the first time. Conversions are cached per image digest per cluster.
- **Pinned artifacts:** the Firecracker version, jailer and guest kernel are part of the cluster's node spec. Upgrading them is a cluster upgrade ([node-agent](node-agent.md#upgrading-aerolvm)).
- **Node requirements:** `/dev/kvm` readable by sandboxd, which in practice means bare metal or a VM type with nested virtualization; the preflight checks it and the node reports whether it can run `microvm`.
- **Sizing:** a VM's memory is reserved, not shared, so overcommit matters more ([placement-and-capacity](placement-and-capacity.md#overcommit)).

### Architecture

- `amd64` or `arm64`, one per cluster. Images, snapshots and Firecracker templates are all architecture-specific, and `/v1/images/build` builds only for the node it runs on.
- The node agent maps `uname -m` (`x86_64` → `amd64`, `aarch64` → `arm64`). Enrollment refuses a mismatch; the agent's own check exists only for a clear message.
- Templates are built per cluster anyway ([templates](../templates.md)), so a template works on clusters of both architectures if its Dockerfile does.

## API

Management (paths per [api](../api.md)):

- `POST /v0/projects/{prj}/clusters` takes `isolation_levels`, `default_isolation`, `architecture`, `gpu_vendor`. `PATCH` changes `isolation_levels` and `default_isolation` at any time (refused `409 isolation_in_use` when removing a level still in use), and refuses `architecture` and `gpu_vendor` once a node has enrolled: `409 cluster_locked`.
- `GET …/clusters/{clu}` returns `isolation_levels`, `default_isolation`, `architecture`, `gpu_vendor`, and per level: `runtime`, `nodes` (online nodes running it) and `capabilities` (`persist`, `gpu`, `nested_sandbox`, `memory_snapshots`, `reliable_exit_codes`).
- Profiles' `allowed_isolation` and `default_isolation`: [profiles § Isolation](../profiles.md#isolation).

Consumer:

- `POST /v0/sandboxes` takes `isolation` ([sandboxes/lifecycle](../sandboxes/lifecycle.md#create)); the sandbox shows `isolation` and `runtime`.
- `GET /v0/profile` lists each reachable cluster's levels **that this profile may use**, with their capabilities and which is the default for this profile, so an app can choose before it creates.

Errors:

| Status | Code | When |
|---|---|---|
| 409 | `cluster_locked` | Changing architecture or GPU after a node enrolled |
| 409 | `isolation_in_use` | Removing a level from a cluster while sandboxes there use it (carries `count`) |
| 422 | `isolation_unavailable` | The level isn't offered on the cluster, or isn't allowed for the profile, or none is both. Carries `isolation`, `cluster`, `reason` (`not_offered`, `not_allowed`, `none_allowed`) and `available` |
| 422 | `validation_failed` | A GPU with a level other than `container`; AMD on arm64; a level the instance doesn't allow, or this release doesn't support yet (in the alpha, anything but `container`) |
| 422 | `persistence_unsupported` | `persist: true` at a level whose runtime can't snapshot there (carries `cluster`, `isolation`, `reason: isolation`) |

## Security

- The isolation level is the main boundary between an agent's code and the node, so it's shown wherever a sandbox or a cluster is, and `container` is labelled as sharing the host's kernel.
- A profile can't raise its own reach: it can only ask for levels the project allows it. A project wanting "untrusted code never runs in a plain container" sets `allowed_isolation` to `["gvisor", "microvm"]` on its profiles (or as the project default for new ones).
- Docker on every node, for builds, means a node offering only `gvisor` still runs the Docker engine. Builds run under plain Docker ([templates/builds § Security](../templates/builds.md#security)); that's a separate question from sandboxes' isolation.
- gVisor's `--host-uds=open` (AerolVM's default) lets sandboxes connect to host Unix sockets that are bind-mounted into them. Confirm nothing sensitive is mounted, and drop it if AerolVM doesn't need it (open question 5).

## Observability

- Per cluster: its levels, how many online nodes run each, capabilities per level, and on the cluster page why a capability is off (e.g. "Persistence at `microvm` needs every KVM node on v0.7.0-lewbox.1 or later; 1 of 3 isn't").
- Per node: which levels it runs and why not the others (no KVM, cgroup v1), kernel version, CPU model, runsc version, Firecracker version.
- Metrics: `lewbox_sandboxes{isolation}`, `lewbox_sandbox_creates_total{isolation, outcome}`.
- Activity: a sandbox's `operation` events for create carry its `isolation`.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_ALLOWED_ISOLATION` | `container` (the alpha supports nothing else) | Which levels projects may offer on their clusters, among those this release supports |

## Testing

- Unit tests:
  - `Capabilities` for each level × feature set × storage × GPU;
  - level resolution on create: every combination of `isolation` given or not, profile default and allowed list, cluster default and offered levels;
  - API refusals for each capability that's off, and each `isolation_unavailable` reason;
  - node spec rendering for each mix of levels (env, runsc flags, install flags, Docker always present).
- The fork's Docker end-to-end runner runs Lewbox's live suite nightly ([aerolvm-fork](aerolvm-fork.md#testing)); gVisor and Firecracker runners join with their levels.
- From the beta: a cluster offering `container` and `gvisor` on one node: a sandbox at each, both persisted, stopped, and resumed at their own level.
- A persistence test per level: write a file, stop, start on another node, read it back.

## Build plan

1. **M1 (alpha):**
   - `Isolation`, `Capabilities` and level resolution;
   - clusters with levels (only `container` offered); nodes reporting theirs; node spec;
   - `isolation` on create and on the sandbox; the profile's allowed and default levels;
   - `container` end to end;
   - the API refusals.
2. **M1:** GPU at `container` (preflight, install flags, `gpu` on create).
3. **Beta:** `gvisor`, once F1, F21, F22 and F24 are in a fork release: runsc install, node spec, both levels on one node, the gVisor end-to-end runner.
4. **Later, when upstream ships Firecracker (#785):** `microvm`: artifacts, preflight, template conversion, persistence.
5. ~~**Later:** memory snapshots at `microvm`.~~ Not offered ([Firecracker](#firecracker-microvm-later)).

## Open questions

1. ~~**Level names or runtime names?**~~ Decided (2026-10-29): levels, `container`, `gvisor` and `microvm`, which describe the guarantee and survive a change of VMM.
2. **Should Lewagent's hosted computers default to `microvm`** once it's offered, to match "every computer runs in its own microVM"? Until the beta they run at `container`, weaker than today's gVisor clusters ([migration/data](../migration/data.md)).
3. **GPUs for consumers:** a `gpu` count on create, and a per-profile GPU limit? Legacy never exposed GPUs to profiles.
4. ~~**Memory snapshots.**~~ Decided (2026-10-29): not offered; memory can't be scrubbed of credentials ([Firecracker](#firecracker-microvm-later)).
5. **gVisor's `--host-uds=open`:** what does AerolVM need it for, and can we turn it off?
6. **Per-level limits:** should a profile's limits (e.g. running sandboxes) be settable per level, so `microvm` (reserved memory) can be capped separately? Proposed: Later; one set of limits for now.
