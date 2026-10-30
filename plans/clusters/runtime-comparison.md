# Microagent and AgentENV comparison

> Research note · Reviewed: 2026-10-30 · Decision pending · Firecracker-only target

AgentENV has stronger current maintenance activity and supplies more of the image-build and distributed-storage machinery. Microagent offers a Go library and a disk-preserving shutdown that fits Lewbox's no-memory persistence rule, but leaves clustering and efficient object-storage offload to Lewbox. Neither has been validated as a complete replacement on Lewbox's hardware.

This compares the runtime-facing requirements in the cluster, sandbox, template and inside-a-sandbox plans. It includes the current admin-controlled `cached` template proposal. Docker and gVisor execution are excluded by the requested Firecracker-only scope. Existing milestone decisions still need revision after a backend is selected.

## Evidence and status definitions

The source reviewed was Microagent main at `8679f4407e24084521d20f656d19e26f67b87d95` and AgentENV main at `4a94ccc9988cd8f675b62a25d7629a98492454ee`. Main includes changes after the latest releases; pin and validate the actual release or commit selected for deployment. This is documentation and source inspection, not a KVM benchmark, security audit or contract-test result.

- **Yes:** the relevant primitive is documented or implemented; Lewbox still needs an adapter and validation.
- **Partial:** useful primitives exist, but the complete Lewbox requirement needs work.
- **Build:** Lewbox must implement it or extend upstream.
- **Verify:** support or the required semantics have not been established.

## Runtime, resources and deployment

| Lewbox requirement | Microagent | AgentENV | Implication |
|---|---|---|---|
| Linux Firecracker microVM per sandbox | Yes | Yes | Both can serve the requested isolation model. |
| Self-host on operator-owned KVM servers | Yes | Yes | Neither requires its vendor's cloud account. |
| Integrate directly as Go packages | Yes | No equivalent Go runtime library verified | AgentENV's runtime is Rust; integrate through its node API. Its optional gateway and scheduler are Go. |
| Keep Lewbox's control plane in one Go binary | Yes | Yes, with external runtime service | Microagent also needs supervisor executables, guest assets and host dependencies. Neither is a dependency-free deployment. |
| Integer vCPU count and guest RAM allocation | Yes | Yes | Both configure guest resources; this is distinct from total VMM host-resource containment. |
| Explicit disk capacity | Yes | Yes | Use explicit sizes. Microagent's auto-sizing and profile defaults may grow to fit image contents. |
| Host CPU quota, VMM memory ceiling and PID limit | Build / verify | Build / verify | Guest sizing alone does not establish cgroup enforcement, CPU shares or host-overhead limits. |
| Cluster CPU, memory and disk reservations; fair capacity queue | Build | Partial | AgentENV supplies scheduling primitives; Lewbox's reservation, overcommit and fairness contract remains integration work. |
| amd64 and arm64 release support | Verify end to end | Verify end to end | Require the chosen server binaries, kernels, guest tools, storage helpers and templates on each architecture. CLI architecture support alone is insufficient. |
| VMM confinement suitable for hostile multi-tenancy | Partial / verify | Verify | Microagent has jailer/rootless modes but `auto` can fall back to off. AgentENV's inspected launcher directly starts Firecracker through a privilege helper; per-VM UID, filesystem confinement and quotas need examination. |
| Reproducible runtime/kernel/tool releases and upgrades | Partial | Partial | Both manage artifacts. Lewbox still needs compatibility floors, rollout, rollback and restore tests. AgentENV pins a patched Firecracker distribution. |

Evidence: [Microagent library](https://github.com/geoffbelknap/microagent/blob/main/docs/library/go.md), [resource options](https://github.com/geoffbelknap/microagent/blob/main/docs/cli/create.md), [machine configuration](https://github.com/geoffbelknap/microagent/blob/main/pkg/supervisors/firecracker/config_linux.go), [confinement](https://github.com/geoffbelknap/microagent/blob/main/pkg/supervisors/firecracker/confinement_linux.go); [AgentENV architecture](https://github.com/kvcache-ai/AgentENV/blob/main/docs/src/internals/architecture.md), [resource configuration](https://github.com/kvcache-ai/AgentENV/blob/main/src/sandbox/firecracker/factory.rs), [VMM launcher](https://github.com/kvcache-ai/AgentENV/blob/main/src/sandbox/firecracker/instance.rs), [dependency manifest](https://github.com/kvcache-ai/AgentENV/blob/main/config/deps_manifest.toml).

## Templates, builds and persistence

| Lewbox requirement | Microagent | AgentENV | Implication |
|---|---|---|---|
| Consume OCI images and private-registry credentials | Yes | Yes | Retain Dockerfile/OCI as the template input format. |
| Execute Dockerfile builds with a context inside a microVM | Build | Yes | Microagent consumes built images; Lewbox must supply BuildKit and its builder VM. AgentENV documents a managed BuildKit-in-VM path. |
| Build logs, status and cancellation | Build | Partial | AgentENV has build jobs/logs; verify cancellation, idempotency and crash recovery against Lewbox's operation contract. |
| Cached base disk on each node | Yes | Yes | Microagent uses reusable ext4 baselines; AgentENV uses layered block storage and node-local caching. |
| Warm template captured before profile credentials | Yes | Yes | Both support snapshot-based startup. Validate host compatibility, identity renewal and supported resource shapes. |
| Local template operation without S3/R2 | Partial | Partial | Microagent stores local artifacts; AgentENV supports a POSIX repository. Neither establishes Lewbox's complete `cached` contract by itself. |
| Pin complete local templates across restart, inventory holders, restrict placement, retain live references | Build | Build / verify | A runtime cache is not the admin-controlled durable template store specified in `templates.md`. Do not silently evict live pinned artifacts. |
| Disk-only stop/start on the same node | Yes | No native path established | Microagent halt preserves the root disk and a later start cold-boots. AgentENV's standard pause preserves memory as well as disk. |
| Never capture customer sandbox memory, including automatic stops | Partial | Build / blocking | Microagent must use halt and disk checkpoints, disabling memory snapshots for customer sandboxes. AgentENV needs a true disk-only capture/offload path. Exporting a full snapshot's rootfs afterwards is insufficient. |
| S3-compatible durable artifact publication | Build | Yes | Microagent has OCI registry push, which is different from a managed S3/R2 snapshot repository. |
| R2 integration | Build | Partial | AgentENV documents S3-compatible endpoint/addressing configuration; exercise the actual R2 bucket, credentials and multipart/retry paths. |
| Remote base reuse and incremental disk layers | Build | Yes | Microagent local reflinks do not create remote delta objects. Its OCI commit emits a full-filesystem layer. AgentENV has shared content-addressed layers and incremental writable layers. |
| Compression and lazy reads of remote layers | Build | Yes | AgentENV supplies storage machinery; benchmark cold metadata-heavy workloads and object-store latency. |
| Background offload with retries and durable completion | Build | Partial | AgentENV supports remote publication, but ordinary pause is node-local and the inspected explicit snapshot request awaits publication. Neither proves Lewbox's complete automatic offload contract. |
| Restore a stopped sandbox on another node | Build | Partial | Microagent needs artifact distribution/import. AgentENV can launch committed snapshots on compatible nodes, but its standard artifacts include memory; Lewbox must map its stable sandbox ID to the runtime ID. |
| Preserve file ownership, modes, symlinks and image startup command | Partial / blocking test | Verify | Microagent's current OCI commit explicitly loses original UID/GID ownership. Raw disk offload avoids that particular export, but restore and startup semantics still need tests. |
| Imported credentials excluded from disks, config and all uploaded artifacts | Build end-to-end guarantee | Build end-to-end guarantee | Microagent offers tmpfs and secret-purge primitives, but these do not remove credentials already copied into process memory. AgentENV's standard snapshot retains memory and runtime context. Both need Lewbox's scrub/harvest/verify contract. |
| Snapshot deletion, shared-layer GC, retention and disk-pressure handling | Build | Partial | AgentENV has deletion and shared layers; establish safe reclamation, reference ownership and bounded cache use. Neither supplies Lewbox's full retention and local-template pinning policy. |

Evidence: [Microagent storage](https://github.com/geoffbelknap/microagent/blob/main/docs/concepts/storage.md), [snapshot implementation](https://github.com/geoffbelknap/microagent/blob/main/pkg/supervisors/firecracker/snapshot_linux.go), [halt/start](https://github.com/geoffbelknap/microagent/blob/main/docs/guides/persistent-workspaces.md), [commit limitations](https://github.com/geoffbelknap/microagent/blob/main/pkg/commit/commit.go), [build limitations](https://github.com/geoffbelknap/microagent/blob/main/docs/concepts/limitations.md), [secret-bearing memory caveat](https://github.com/geoffbelknap/microagent/blob/main/docs/security.md); [AgentENV builds](https://kvcache-ai.github.io/AgentENV/latest/concepts/templates/creating.html), [storage configuration](https://kvcache-ai.github.io/AgentENV/latest/configuration/reference.html), [artifact inventory](https://github.com/kvcache-ai/AgentENV/blob/main/docs/src/internals/persistence-artifact-inventory.md), [snapshot publication API](https://github.com/kvcache-ai/AgentENV/blob/main/src/api/impls/sandbox.rs), [local pause persistence](https://github.com/kvcache-ai/AgentENV/blob/main/src/orchestrator/persistence/file_backed.rs).

## Guest operations, networking and lifecycle

| Lewbox requirement | Microagent | AgentENV | Implication |
|---|---|---|---|
| Commands with stdout, stderr and exit status | Yes | Yes | Validate exit status, user/workdir/env, deadlines and disconnect behaviour on all paths. |
| Streaming command output | Yes | Yes | Adapt to Lewbox's stream protocol, limits and cancellation rules. |
| Interactive terminals | Partial | Partial | Microagent exposes console/PTY negotiation; AgentENV exposes guest shell facilities. Neither is verified against every Lewbox terminal requirement. |
| Named sessions, idempotent creation, reconnect logs, resize, signals, completion events | Build / verify | Partial / verify | Do not equate an interactive shell or E2B compatibility with Lewbox's full session contract. |
| Upload/download while running | Yes primitives | Yes primitives | Confirm streaming, ranges, limits and permission handling. Microagent also supports offline copying. |
| Full file API: stat/list/move/remove, atomic writes, conditional writes and watching | Build / verify | Partial / verify | Adapt existing primitives and test coverage; no verified ETag/If-Match contract in this review. |
| Expose a guest TCP port | Yes local forwarding | Verify raw TCP ingress | AgentENV documents HTTP proxy ingress; raw SSH/TCP must be established separately. |
| HTTP/WebSocket app previews with TLS and custom domains | Build edge integration | Partial | Lewbox owns DNS/TLS and its URL contract. Verify WebSocket routing, auth and idle accounting. |
| Signed private-port links and browser embedding | Build | Partial | AgentENV has traffic access tokens; Lewbox's signed links and cookie/embedding flow remain its own work. |
| SSH keys and SSH access | Build / adapt TCP or guest sshd | Build / verify transport | Key policy, synchronization and audit remain Lewbox's. |
| Sandbox egress isolation and host/metadata protection | Partial / verify | Partial / verify | Both have network controls. Apply operator-owned guardrails and test them from inside hostile guests, including IPv6. |
| on_create, on_start and bounded on_stop hooks | Build | Partial | AgentENV has custom-extension lifecycle callbacks, but these are not the user-hook ordering and execution contract in Lewbox's plans. |
| Idle/lifetime warnings, busy deferral, maintenance requests not counted as activity | Build | Partial | Existing runtime TTLs are not the full activity model. Lewbox must own the policy. |
| Node-side hooks and stop backstop while the control plane is down | Build | Build / verify | This is a gate for safe automatic stops and credential cleanup. |
| Lifecycle events, guest stats and host metrics | Partial | Partial | Both expose useful information. Replay, session-end events, accounting semantics and missed-event reconciliation remain integration work. |
| Install and run agentd/portd; Claude Code/Codex and desktop workload | Partial | Partial | A Linux guest can host the binaries; Lewbox must package them, apply credentials and validate the representative workload. |

Evidence: [Microagent execution](https://github.com/geoffbelknap/microagent/blob/main/docs/cli/exec.md), [file copying](https://github.com/geoffbelknap/microagent/blob/main/docs/cli/cp.md), [console support](https://github.com/geoffbelknap/microagent/blob/main/pkg/workspace/console.go); [AgentENV guest operations](https://github.com/kvcache-ai/AgentENV/blob/main/docs/src/concepts/sandboxes/working.md), [E2B integration](https://github.com/kvcache-ai/AgentENV/blob/main/docs/src/integration/e2b.md), [network controls](https://kvcache-ai.github.io/AgentENV/latest/concepts/sandboxes/networking.html), [custom extension API](https://github.com/kvcache-ai/AgentENV/blob/main/src/custom_extension_api/openapi.yml).

## Clusters and Lewbox product responsibilities

| Lewbox requirement | Microagent | AgentENV | Implication |
|---|---|---|---|
| Multi-node routing and basic scheduling | Build | Yes optional gateway/scheduler | AgentENV static discovery requires a configured node list and scheduler restart for membership changes. |
| Enrollment, node PKI renewal/rotation, direct/tunnel/relay connectivity | Build | Build / adapt | AgentENV's documented deployment uses HTTP plus external TLS/VPN protection; it does not supply Lewbox's enrollment and tunnel protocol. |
| Cluster HA, drain, orphan cleanup, reservations and node-loss recovery | Build | Partial | Review actual failure semantics. Recent scheduler HA work does not by itself establish durable placement bindings or Lewbox's recovery contract. |
| Node artifact upgrades and verified inventory | Build / adapt helpers | Build / adapt runtime distribution | Pin the complete Firecracker, kernel, guest-tools and storage stack. |
| Projects, profiles, roles, API keys, quotas, credits and audit | Lewbox | Lewbox | Neither replaces the product's tenant/auth/usage model. |
| GitHub integration, agent sign-ins, agent conversations and approval flows | Lewbox | Lewbox | Keep these above the runtime adapter. |
| Dashboard, SDKs, CLI, webhooks, API operations and Postgres jobs | Lewbox | Lewbox | These plans remain product work with either backend. |

Evidence: [AgentENV static cluster deployment](https://kvcache-ai.github.io/AgentENV/latest/deployment/static-multi-node.html), [runtime architecture](https://github.com/kvcache-ai/AgentENV/blob/main/docs/src/internals/architecture.md), [Microagent caller-owned orchestration boundary](https://github.com/geoffbelknap/microagent/blob/main/docs/library/index.md). Lewbox requirements: [node agent](node-agent.md), [connectivity](connectivity.md), [placement](placement-and-capacity.md), [hooks](../sandboxes/hooks.md), [persistence](../sandboxes/persistence.md), [templates](../templates.md).

## Maintenance comparison

| Signal checked on 2026-10-30 | Microagent | AgentENV |
|---|---|---|
| Latest released version | v0.10.0, 2026-08-26 | v0.2.4, 2026-10-23 |
| Latest default-branch commit | 2026-09-26 | 2026-10-30 |
| Latest feature work visible in recent commits | 2026-09-06 | 2026-10-30 |
| Non-bot GitHub author accounts across latest 100 commits | 1 | 20 |
| Published releases since 2026-08-01 | 1 | 8 |
| Licence | Apache-2.0 | MIT |
| Integration model | Go library plus supervisor/guest artifacts | Rust runtime service; optional Go gateway/scheduler; specialised block-storage stack |

AgentENV is the more actively maintained project by these observed signals. This measures activity and contributor breadth, not security quality, guaranteed support or the number of maintainers. GitHub push timestamps can include activity that is not merged default-branch code; the comparison uses actual default-branch commits. Both projects are pre-1.0, and recent main features must not be assumed present in the published releases.

Sources: [Microagent releases](https://github.com/geoffbelknap/microagent/releases), [commits](https://api.github.com/repos/geoffbelknap/microagent/commits?per_page=100), [contributors](https://api.github.com/repos/geoffbelknap/microagent/contributors?per_page=100); [AgentENV releases](https://github.com/kvcache-ai/AgentENV/releases), [commits](https://api.github.com/repos/kvcache-ai/AgentENV/commits?per_page=100), [contributors](https://api.github.com/repos/kvcache-ai/AgentENV/contributors?per_page=100). Counts describe the latest 100 commits for each project, which span different periods; they are not equal-window contribution rates.

## Microagent summary and implementation checklist

Microagent is the better architectural fit for importing VM mechanics into `lewbox node` and retaining control over disk-only persistence. It supplies OCI conversion, kernels, guest execution and local lifecycle. The main cost is that Lewbox becomes responsible for placement, distributed artifact storage and most lifecycle policy. Select it if the project accepts owning that infrastructure.

Gate the choice before building the integration:

- [ ] Pin a release/commit and qualify the Linux Firecracker supervisor on target hosts. Require confinement explicitly; reject unavailable confinement rather than accepting `auto` fallback.
- [ ] Prove the actual Lewagent workload: desktop, agentd, Claude Code/Codex, streamed commands, multiple reconnectable PTYs, exit statuses, file transfers and preview traffic.
- [ ] Prove a disk-only halt, scrub, upload and cold restore onto a second node. Check UID/GID, modes, symlinks, startup commands, all declared disks and absence of imported credentials/config secrets. Address the OCI commit ownership limitation before relying on that export.

Then implement:

- [ ] A runtime adapter around exported Go packages, supervisor packaging and typed error/readiness mapping.
- [ ] Host cgroup containment, resource reservations, placement, a fair capacity queue and reconciliation after restart/node loss.
- [ ] BuildKit in a dedicated builder microVM, build-context handling, logs, cancellation and OCI publication.
- [ ] Clean warm-template generation and compatibility keys; local `cached` template inventory, pinning, replication, holder-aware placement and safe unpinning.
- [ ] A durable S3/R2 artifact format. Choose whole sparse/compressed disks first or explicitly budget base/delta chunk storage, manifests, integrity checks, compaction and shared-object GC.
- [ ] Background upload jobs with retry, crash recovery and an explicit remote-durability state. Keep local immutable data until verification; never read an actively changing disk as a coherent checkpoint.
- [ ] The credential memory-only mounts, refresh harvest, hook ordering, offline scrub and upload verification from the current plans; disable customer memory snapshots on all automatic and manual paths.
- [ ] TLS/DNS ingress, raw TCP/SSH, private links, activity accounting, idle warnings and a node-side stop backstop.
- [ ] Upgrade/rollback, disk-full and interrupted-upload tests, retention accounting, metrics and operator diagnostics.

Largest new subsystems: clustering/scheduling and distributed persistence. The number of checklist items is not an effort estimate; those subsystems dominate the work.

## AgentENV summary and implementation checklist

AgentENV supplies more of what Lewbox needs: managed image builds, shared incremental storage, remote reads and optional multi-node routing. It has stronger current maintenance activity. Its standard snapshot contract captures memory and runtime context, which conflicts with the current no-memory credential guarantee. Resolve that incompatibility before making it the selected backend.

Gate the choice before building the integration:

- [ ] Establish a true disk-only checkpoint and offload path that never creates customer memory artifacts, even temporarily and on TTL expiry. Publishing a rootfs extracted from an earlier full snapshot does not pass this gate.
- [ ] Establish scrub/harvest/verify ordering before any remote publication, including metadata and every disk. Verify hooks still run when the Lewbox control plane is unreachable.
- [ ] Audit and qualify per-VMM confinement, UID/capabilities, seccomp, cgroups and guest-to-host/network boundaries. Firecracker use alone is not proof that the complete deployment meets the hostile-tenant threat model.
- [ ] Prove the representative Lewagent workload, reconnectable PTYs, exit codes, raw TCP/SSH and authenticated WebSocket previews.

Then implement:

- [ ] Pin the full runtime distribution, patched Firecracker, kernel, guest tools and block-storage dependencies; select released versus main-only features explicitly.
- [ ] A Go node-API adapter with runtime-ID mapping, typed errors, idempotent adoption and operation reconciliation.
- [ ] Choose one placement authority: integrate AgentENV's gateway/scheduler or let Lewbox own placement through node APIs. Add Lewbox reservations, quota checks, fair waiting, draining and failure recovery.
- [ ] Map Lewbox Dockerfile/context/build options and logs onto managed BuildKit; preserve immutable build references and normal startup behaviour.
- [ ] Configure per-project S3/R2 storage and test addressing, credentials, concurrency, interrupted/multipart uploads, retries and restoration after loss of the source node.
- [ ] Separate local pause from remote durable publication in the operation state machine; implement automatic offload and truthful completion reporting.
- [ ] Add the `cached` template contract: local repository layout, pinning/inventory, holder-only placement, no implicit object-store fallback and reference-safe retention.
- [ ] Adapt runtime callbacks to Lewbox hooks; add idle warnings, busy deferral, maintenance classification, refresh harvest and node-side fallback.
- [ ] Add enrollment/mTLS, DNS/TLS edge routing, private links and SSH transport where not supplied by the verified backend.
- [ ] Establish cache/layer GC and object ownership; qualify mixed-version restore, architecture compatibility, upgrades and operational metrics.

Largest blockers: disk-only credential-safe publication and verified VMM hardening. If those are resolved, AgentENV has the stronger case for reducing total runtime/storage development. If disk-only capture requires a deep fork of its memory/storage model, compare that real patch scope with Microagent plus a Lewbox-owned disk store before choosing.
