# AgentENV evaluation lab

Tested on 2026-10-30. **AgentENV's Firecracker, build and layered-storage paths work on this machine.** Three full 20 GiB guests ran on a bounded 75 GB filesystem, and the legacy desktop and actual agentd executable (with a scripted ACP peer) passed the recorded experiments. The desktop's initial remote-image resize required a local-prefetch workaround.

**Current policy: RAM snapshots are accepted.** Earlier rows marked blocked against the disk-only/no-memory requirement describe the original experiment contract, not a current rejection of native memory capture. Credential exclusion is still a separate unresolved requirement. Current problems, workarounds, validation gaps and links to the affected Lewbox plans are maintained in **[PROBLEMS.md](PROBLEMS.md)**. Raw results below are preserved as historical evidence.

## Checkouts and versions

- Lewbox worktree: `/home/homelab/dev/lewbox-agentenv`, branch `experiments/agentenv`, based on `planning` at `8bf98fe`. The original worktree and its uncommitted planning changes were preserved.
- AgentENV clone: `/home/homelab/dev/AgentENV`, branch `lewbox/v0.2.4-lab`, tag **v0.2.4**, commit `9f62cbe07cebfecf416dc9bdcf497e2a97687644`. No upstream source modifications.
- Also retained in that clone: `lewbox/experiments` at main commit `4a94ccc9988cd8f675b62a25d7629a98492454ee`. The tests used the release, not that newer main.
- Downloaded CLI/server release archives; SHA256 values are pinned in [download.py](download.py) and checked against GitHub release asset digests. [Release metadata](release-metadata.json) is retained.
- Release dependencies: Firecracker `1.15.1-patch-v1`, guest kernel `6.1.175`, guest tools `0.1.1`, OverlayBD `v1.0.18-aenv.1`, regctl `v0.11.5`. Source: [pinned dependency manifest](https://github.com/kvcache-ai/AgentENV/blob/9f62cbe07cebfecf416dc9bdcf497e2a97687644/config/deps_manifest.toml).
- Host: Ubuntu 26.04.1, kernel `7.0.0-42-generic`, Intel i5-6400T, 4 logical CPUs, 15 GiB RAM, KVM API 12. KVM VM creation was verified. No Docker daemon was installed or required.
- Binaries, state and disposable credentials: `/home/homelab/.local/share/lewbox-agentenv-lab` (about 11 GiB allocated including retained synthetic S3 objects and retired experiment state after the extended tests), outside Git. AgentENV source occupies another 37 MiB.

## Observed results

These are functional tests on one desktop host with synthetic Alpine workloads. Timings are individual measurements, not throughput benchmarks or production latency guarantees. API timing ends at the API response; subsequent guest commands verified readiness separately.

| Experiment | Result | Evidence / implication |
|---|---|---|
| OCI → Firecracker without Docker | Pass | `alpine:3.22`, cached-image cold creation around 2.2–2.8 seconds. Includes a real cold boot; no vendor warm-start claim. |
| vCPU and RAM allocation | Pass | 1 CPU/512 MiB and 2 CPUs/768 MiB; guest `nproc` and `/proc/meminfo` matched. Kernel overhead reduces guest `MemTotal`. |
| Disk capacity override | Pass for tested cold-start shapes | 64 and 65 GiB work with the default config. Smaller requests are rejected while `allow_shrink=false`. A follow-up with `allow_shrink=true` verified a 20 GiB device and matching smaller ext4 filesystem, then snapshot capture and restore at the same size. The later three-guest pressure test below reached guest ENOSPC on all three. |
| Internet egress | Pass | Guest fetched `https://example.com`. Private-network and tenant-isolation policies were not qualified. |
| Exec | Pass | Separate stdout/stderr and exit code 42 propagated through the CLI. |
| Interactive PTY | Pass | Attached shell executed commands with a 40×100 terminal. Multiple named sessions, disconnect/reconnect and dynamic resize remain untested. |
| File transfer | Pass | 1 MiB upload/download round trip matched SHA256. |
| Local pause/resume | Pass | Same sandbox ID, kernel boot ID, background process and file metadata. Measured pause ~0.12 seconds, resume API ~0.15 seconds in the first full run. |
| Hot snapshot and fork | Pass | ~1 second capture, ~0.17 second local fork API in the first full run. UID 1234/GID 2345, mode 0640, symlink and running process preserved; a child write did not alter the source. |
| Server restart | Pass | Paused record recovered; resume retained kernel/process/files. A committed snapshot also forked after restart. |
| Dockerfile/context build | Pass | Managed BuildKit VM bounded to 2 CPUs/2 GiB; `COPY`, `RUN`, build arg, logs, ownership/mode, `CMD` and `HEALTHCHECK` worked. First provisioning/build ~31 seconds, later cached builds ~6 seconds. |
| Build cache | Pass | BuildKit logs mark both `COPY` and `RUN` as `CACHED`. |
| Rebuild same template name | Rejected | HTTP 400: existing alias cannot be rebound. Use immutable per-build names/IDs and map Lewbox's “latest ready” above AgentENV. |
| POSIX disk-layer reuse | Pass | First run: 78.08 MiB referenced disk layers; writing 8 MiB random data added 8.06 MiB, reusing the old layers. A repeat added 9.12 MiB; deltas also include filesystem/other guest writes. This does not store a full 64 GiB disk per checkpoint. |
| Memory exclusion | Historical no-memory requirement failed; RAM now accepted | Actual memory layers contained the synthetic create-time environment canary. Pause and snapshot preserve memory/process state. This remains relevant to the separate credential policy in [PROBLEMS.md](PROBLEMS.md). No real imported credentials were used. |
| S3-compatible publication | Pass, local fixture | Silo, the same version selected by upstream tests; loopback HTTP, path addressing, disposable credentials. Actual R2/S3 accounts were not tested. |
| Restore with no publisher-local state | Pass | Publisher stopped; a different, initially empty state/cache/runtime directory restored the remote snapshot in ~0.52 seconds API time. Process, file metadata and 8 MiB file checksum matched. Same physical CPU/host and pinned guest dependencies; this is not a two-host cluster test. |
| Pause automatically offloads to S3 | **No in this test** | Pausing added no objects to the bucket. Explicit snapshot publication uploaded artifacts before returning. Lewbox still needs an offload state machine and retry/durability contract. |

Raw evidence: [smoke runs](results/), [restart](results/restart.json), [build results](results/build.json), [cached build log](results/build-cached.log), [first local layers](results/local-storage-first.json), [repeat local layers](results/local-storage.json), [remote objects and restore](results/remote.json). Early failed runs record lab setup/harness problems separately from the successful full runs.

#### Lewbox-specific experiments

The [Lewbox harness](lewbox.py) tests the requested stop/start/restart/delete lifecycle without calling pause. It creates a disposable **75,000,000,000-byte (75 GB) ext4 loop filesystem** for AgentENV's node state and runtime. The OS, downloaded binaries and local S3 fixture sit outside this filesystem. Three guests each receive a **128 MiB random payload**, an ownership/mode/symlink fixture and a background process. This measures reclamation and recovery; it does **not** fill three 20 GB disks or qualify production capacity.

The explicit size request is **20,480 MiB = 20 GiB**, slightly larger than 20 decimal GB. In these runs, `allow_shrink` retained its default value, **false**, and all three requests failed because they were smaller than the converted 64 GiB base. The remaining lifecycle experiments therefore used three **64 GiB sparse virtual disks**. Those fallback guests must not be mistaken for a passing 20 GB quota test. The subsequent opt-in shrink experiment below succeeded without a source patch.

| Lewbox experiment | Result | Observed behaviour |
|---|---|---|
| Three 20 GB sandbox disks with default settings | Requires configuration change | All three initial 20 GiB requests returned HTTP 400 with `allow_shrink=false`. Enabling the existing setting resolves the disk-size rejection, as verified below. |
| 20 GiB sandbox disk with modified settings | **Pass** | With `allow_shrink=true`, one sandbox booted with a 20 GiB device and matching smaller filesystem. Files and permissions survived snapshot → delete → restore, and the restored device remained 20 GiB. [Test results](results/shrink-20261030-203809.json). A subsequent three-sandbox full-disk test also passes; see the pressure results below. |
| Three VMs on a 75 GB node filesystem | Pass with fallback sizes | Three 64 GiB logical disks booted together. Their node files occupied about **461.8 MiB**, including three 128 MiB payloads. This proves sparse allocation, not 20 GB quota enforcement. |
| Stop: publish → verify → delete | Pass for existing snapshot primitives | Catalog and every managed layer were checked in S3; layer bytes matched their SHA256 digests **before** deleting each runtime. Deleted IDs returned HTTP 404 and the runtime list became empty. |
| Local space reclaimed by runtime deletion | Pass; shared data retained | Node file allocation fell from about **461.8 MiB to 77.3 MiB**: about **384.5 MiB reclaimed**. About 77 MiB of image cache remained. Later restore/restart reads grew retained node files to about **273 MiB** before offline eviction; 77 MiB is not a fixed cache ceiling. A 20 GB reservation is not a 20 GB physical allocation. |
| Temporary publication space | Measured | With the three payloads, sampled node file allocation reached about **660 MiB** while publishing the first checkpoint, before deleting that runtime. Publication needs headroom beyond steady running storage. This is a sampled value, not a continuously measured maximum. |
| Disk-only stop with no pause | **Blocked** | There were no pause calls, but explicit checkpoints still published RAM layers. Removing the pause operation does not provide disk-only persistence. |
| Start after deleting the runtime | Pass for full snapshot restore | File hashes, UID/GID, mode, symlink and contents matched. A new AgentENV runtime ID was created. |
| Start performs a fresh cold boot | **Blocked** | Kernel boot ID and the background process were retained. The available snapshot restore is not a disk-only cold boot. |
| Restart while preserving writes | Pass for an emulated snapshot cycle | Write → checkpoint → delete → restore preserved a new marker. It retained RAM/process state; this is not a native cold reboot or a completed Lewbox restart implementation. |
| Evict all local stopped-node data | Pass through harness cleanup | With all runtimes deleted and publication verified, the harness stopped the node and removed its disposable local state. Allocated node file bytes became zero; filesystem metadata still occupied about 2 MiB. This is **offline whole-node eviction**, not an AgentENV per-sandbox cache-eviction API. |
| Start all three after complete local eviction | Pass | A fresh state/cache/runtime directory restored all three checkpoints from S3 and verified every payload and metadata fixture. Same physical host/CPU; the original local state no longer existed. |
| Delete persisted state without breaking siblings | Partial | The deleted snapshot IDs returned HTTP 404, their catalog/artifact objects disappeared, and a different sandbox still restored correctly. **Four unreferenced managed objects (~191.5 MiB) remained** in the second run. Logical deletion succeeds; reclaiming their bytes still requires shared-layer GC. |

Three default-configuration runs completed with **8 passing scenarios and 3 blocked requirements** each. These are historical results for that configuration; the later opt-in shrink experiment resolves the cold-create disk-size rejection. The memory and cold-boot findings remain unchanged. The publication/deletion steps here are an experiment adapter around AgentENV's existing APIs, not a completed disk-only lifecycle.

Evidence: [first run](results/lewbox-20261030-202324.json), [run with publication-space measurements](results/lewbox-20261030-202529.json), [final run with explicit reclamation and surviving-layer assertions](results/lewbox-20261030-202730.json). The JSON includes physical file blocks counted once per inode, filesystem used/available bytes, cache/workspace allocation and timing. A sparse loop backing file can retain host blocks after files inside it are removed; it is removed during cleanup. The node filesystem's own free-space accounting is recorded separately.

Run from the experiment worktree with the other lab services stopped:

```bash
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/lewbox.py
```

The harness configures the required host access, starts isolated fixtures, runs the tests, stops the services, restores host prerequisites, unmounts/detaches the loop device and removes the disposable filesystem image. It refuses to overwrite existing fixture paths. Reports, logs and remote synthetic objects are retained; API keys and fixture credentials remain outside Git. Each run uses a separate object prefix.

Follow-up pressure and failure tests are recorded below. Actual R2, two physical hosts, OS/build overhead within the 75 GB budget, selective cache eviction with live references and shared-layer GC remain unqualified. Guest capacity does not establish a host physical-storage quota.

### 64 GiB default and shrink investigation

**Correction to the initial conclusion:** 64 GiB is the standard OCI conversion size, not an unavoidable Firecracker minimum. The original experiments left the documented shrink switch disabled. On the same v0.2.4 binaries, the [follow-up probe](shrink.py) passed all four checks after enabling it; [raw results](results/shrink-20261030-203809.json).

The source/history explain the two separate decisions:

1. [`LAYER_VIRTUAL_SIZE_GIB = 64`](https://github.com/kvcache-ai/AgentENV/blob/v0.2.4/src/image/oci_image.rs#L225-L228) sizes every converted OCI lower layer. The source comment says this accommodates common base images; sparse storage means it does not preallocate that capacity. The constant was present in the [initial open-source release](https://github.com/kvcache-ai/AgentENV/commit/8f028b1369a4ce9a66d140174d0a646e72398517) and remains in checked main `4a94ccc`. No deeper rationale for that exact number was found in the reviewed history.
2. [`allow_shrink = false`](https://github.com/kvcache-ai/AgentENV/blob/v0.2.4/config/default.toml#L301-L312) rejects cold-start requests smaller than their source. Enabling it makes the runtime invoke `overlaybd-resize` against a fresh writable upper, then verify the resulting device size. This performs an ext4/filesystem resize, not just a change to the reported capacity. [Runtime implementation](https://github.com/kvcache-ai/AgentENV/blob/v0.2.4/storage/ublk-daemon/src/runtime.rs#L144-L248), [documented scope](https://github.com/kvcache-ai/AgentENV/blob/v0.2.4/docs/src/concepts/sandboxes/starting.md#L89-L94).

The tested setting and cold-start request are:

```toml
[ublk.overlaybd]
allow_shrink = true
```

```bash
aenv start --cold alpine:3.22 --disk-size-mb 20480 -d
```

The fresh guest exposed **21,474,836,480 bytes (20 GiB)**, and `df /` reported **20,630,224 KiB** of ext4 filesystem capacity. A 32 MiB random file, UID/GID 1234:2345, mode 0640 and symlink survived checkpoint → runtime deletion → snapshot restore; the restored device was still exactly 20 GiB. No upstream source changes were made. The probe stopped its services and restored host settings afterwards.

For reusable warm images, an available path is **prepare a clean 20 GiB cold guest → snapshot that shape → launch the snapshot**. We tested the capture/restore mechanics with Alpine, not the complete Lewbox template build integration. Existing 64 GiB snapshots cannot be resized by requesting a different size on resume.

Managed Dockerfile template builds still resolve resources with [`disk_size_mib: 0`](https://github.com/kvcache-ai/AgentENV/blob/v0.2.4/src/api/impls/template_helpers.rs#L15-L32), and the build request has no disk-size field. They therefore retain the source default. A potential Lewbox adapter can use image-only builds followed by the sized cold-create/capture path; exposing size directly in managed template builds would require upstream work. Current [issue #453](https://github.com/kvcache-ai/AgentENV/issues/453) requests both build-time disk sizing and physical write bounds. It is an open request, not a shipped fix.

Release review: v0.2.4 remains the latest published release checked. The 64 GiB default and opt-in shrink were already present in the initial release. The v0.2.4 [disk-size warning fix (#420)](https://github.com/kvcache-ai/AgentENV/pull/420) changes layer detection/diagnostics and explicitly retains the rejection when shrinking is disabled. A version upgrade is not required to try this setting.

Limits: the image's contents must fit the smaller filesystem; oversized-image failure, ENOSPC and heavy rewrite/compaction behaviour were not exercised. A smaller guest filesystem does not itself enforce a hard host-physical byte quota over writable layers, retained checkpoints, caches and logs. It also does not change the memory-inclusive snapshot contract.

To repeat the probe with the lab stopped, source `lab.env.sh` and run `python3 experiments/agentenv/shrink.py`. It keeps its disposable state under `$LAB_ROOT/s`; move that retired directory aside before repeating, because the probe refuses to reuse existing state.

#### Disk-only stop/start feasibility

Under the original disk-only requirement, the [internal-interface prototype](disk-only.py) completed **5 passing scenarios and 2 blocked native behaviours**; [complete result](results/disk-only-20261030-205825.json). This proves that AgentENV's existing disk format and cold-create resolver can support a full-root disk-only round trip. These counts retain their historical meaning; RAM capture is now accepted and this custom disk-only path is optional. They do **not** qualify the complete public stop/start lifecycle.

| Experiment | Result | Evidence / scope |
|---|---|---|
| Capture the complete root disk without RAM | Pass through prototype | Quiesce guest ext4 with `fsfreeze`, hold vCPUs through the private Firecracker API, and seal the disk through the private ublk daemon RPC. No VM-state/memory snapshot API is invoked. The temporary CPU hold is not a persisted pause or a RAM checkpoint. |
| Publish disk layers and portable image metadata | Pass through prototype | Upload/verify immutable layer hashes and commit a content-addressed `PublishedImage` description last. The fixture's 20 GiB root disk contains a 64 MiB random file plus changes under `/etc` and `/root`. Only disk layers and an image description were published. |
| Exclude a memory-only credential | Pass for synthetic fixture | A synthetic create-time environment canary and tmpfs credential file were present in the source. The canary was absent from every exported disk layer and the published description; no local memory/VM-state artifacts were created by this path. This is not a complete production scrub/harvest implementation. |
| Delete local state, then cold restore | Pass | Delete the original runtime and publisher state, remove its sealed local capture files, and boot the published image from a fresh node-state directory. Full file hashes, UID/GID, mode, hardlink count and symlink match; the kernel boot ID changes and the disk remains 20 GiB. |
| Run the image startup command automatically | **Blocked in tested cold-create path** | The image description's `Cmd` was not executed automatically. The prototype explicitly starts the restored script and verifies its marker contains the new boot ID. A Lewbox adapter must preserve startup configuration and execute startup/readiness on cold restore. |
| Failed disk-only upload and retry | Pass through prototype | Stop the S3 fixture after sealing. Publication fails; retain the live sealed layer, restart S3, resume/thaw the source, and verify its files. A fresh capture/retry restores the new write; the older durable checkpoint still restores its original data. This tests the harness's policy, not native snapshot error recovery. |
| Native graceful shutdown captures RAM | Accepted native behaviour; historical no-memory test failed | As a separate control, gracefully shut down a node with an active `autoPause=false` sandbox. AgentENV creates local `vm_state.bin`, `mem_image.json` and a memory layer. Current lifecycle and credential-policy integration is tracked in [PROBLEMS.md](PROBLEMS.md). |

No upstream Rust or Go code was changed. The prototype directly uses private daemon/control interfaces and writes the existing repository image-description format; these operations need supported node APIs, lifecycle locking, ownership/rollback guarantees and compatibility coverage before product use. Its small-fixture publisher reads whole blobs, refuses blobs above 512 MiB and does not implement production streaming, multipart upload, compression or GC. It uses trusted fixture startup metadata and a whitelist excluding create-time env; it does not yet harvest refreshed credentials or scrub arbitrary on-disk paths. Roots with extra drives/volumes were not tested.

The feasibility result demonstrates that the storage format can support disk-only persistence. With RAM snapshots now accepted, a supported disk-only integration is optional; native lifecycle integration and the credential policy remain the relevant work. The prototype itself never exported a rootfs from an earlier full memory snapshot.

Run `python3 experiments/agentenv/disk-only.py` after sourcing `lab.env.sh`, with the other lab services stopped. The script retains its synthetic state under `$LAB_ROOT/d`; move that retired directory aside before repeating. Failed early prototype reports are retained separately; they include fixture readiness, optional descriptor and relative-path handling fixes, plus discovery of the cold-start `Cmd` behaviour.

#### Three full 20 GiB disks on 75 GB

The [pressure harness](pressure.py), with `allow_shrink=true`, created three concurrent **20 GiB** devices and wrote incompressible data until each guest returned ENOSPC. Each retained a **21,099,548,672-byte (~19.65 GiB)** file; ext4 metadata consumes the rest of the virtual disk. All three sentinel files remained readable. The node filesystem used **63,417,954,304 bytes**, with **10,964,176,896 bytes (~10.21 GiB)** still available. [Raw report](results/pressure-20261030-211242.json) and the three fill logs in `results/` retain the evidence.

Deleting the first full guest reclaimed **21,112,385,536 bytes (~19.66 GiB)** of node file allocation while the two neighbours remained readable. Deleting all three left **80,965,632 bytes (~77.2 MiB)** of node files, mostly shared image cache. The loop backing file also returned its freed extents to the host after host-side `fstrim`.

This passes simultaneous guest-size enforcement, guest ENOSPC isolation and physical reclamation for the tested Alpine image. It does **not** qualify the entire deployment budget: OS/binaries and the S3 fixture are outside the bounded filesystem, and three large-image checkpoints, build cache, retained layer chains, logs and publication staging need additional capacity policy. Three 20 GiB disks are about 64.4 decimal GB before those costs, rather than 60 GB.

The same run then created a durable baseline, added a 4 GiB incompressible write, and reserved node free blocks until only about 128 MiB remained. A native checkpoint failed with ENOSPC while compacting a memory layer. After releasing the reserve, the source was running and its baseline hash remained readable; no new catalog record was committed. The previous checkpoint restored with its baseline intact and the later write absent. This is a standard memory-inclusive checkpoint failure result, not qualification of a supported disk-only stop. All **six** pressure scenarios passed and fixture cleanup completed.

The earlier [interrupted run](results/pressure-20261030-205907.json) verified creation only; its tool session crashed during fill. Its stale namespace mounts and loop filesystem were explicitly recovered before the successful rerun. It is not counted as a capacity pass.

#### Legacy desktop workload

The legacy checkout at `/home/homelab/dev/legacy/lewbox`, commit `df12cac2c62f04080829f8ba85cb5842689dbd57`, supplies `docs/examples/templates/desktop.Dockerfile`. The [workload harness](workload.py) copies only that Dockerfile into its build context, without editing it or importing account credentials. Its source SHA256 is `57a41c4a0e83e72b1987b4264f7db02c3f1176f4e7ec5bb69087d0cba8407bbe`.

The unchanged Dockerfile built through `aenv build --image` and published successfully in **591.143 seconds** on this host. Its immutable image description references **1,693,986,282 bytes (~1.58 GiB)** of managed layers. This is the reusable image-layer total, not peak build disk use or the total of every temporary/builder object in the repository. [Build/result](results/workload-20261030-212321.json), [build log](results/workload-20261030-212321-build.log), [Dockerfile copy](results/workload-20261030-212321.Dockerfile). The committed build log normalizes terminal carriage returns/trailing whitespace; its raw output is retained under the lab logs outside Git.

A **20 GiB cold start from that S3-published image failed**, despite `allow_shrink=true`; repeating from fresh node state reproduced it. The converter diagnostic is `__open_ro_remote: empty repoBlobUrl for remote layer`, then `failed to open files`. The resize helper is C++, while the published-image resolver supplies remote-layer references to the Rust storage path. This points to a remote-layer materialization incompatibility rather than evidence that the desktop contents exceed 20 GiB. [Fresh-state retry](results/workload-20261030-213433.json), [converter diagnostics](results/workload-20261030-213433.diagnostics.log).

The opt-in workaround **passed eight scenarios** in [the follow-up run](results/workload-20261030-213514.json). It boots a 64 GiB control, downloads/hash-verifies all 13 published layers, then rewrites only the isolated source-cache config to local file references before resizing. This is a private-interface workaround, not a supported production cache API. The original failed result remains a failed native path.

| Legacy workload experiment | Result | Evidence / scope |
|---|---|---|
| Original published image at 64 GiB | Pass | Boots through the native remote path; Node 24.22.0, Claude Code 2.1.302, Codex 0.163.0 and Chromium report their versions. |
| Prefetched image at 20 GiB | Pass through workaround | Matching 20 GiB device; 2 CPUs/4096 MiB allocated. API creation took ~2.12 seconds; desktop readiness is checked afterwards. |
| Desktop startup and capture | Pass | Explicit adapter startup starts XFCE, Xvfb, sshd and Selkies. Selkies serves HTTP; both scrot and the computer-use API produce images. Browser video/WebSocket performance and interactive Chromium use were not tested. |
| Interactive terminal | Pass | Attached shell executes commands at 40×100. Reconnectable named sessions remain untested. |
| Node preview | Pass inside guest | A persisted Node HTTP application answers on loopback port 3000. This does not test Lewbox's external proxy or access controls. |
| Root-disk publication without RAM | Pass through prototype | Reuses **13 existing image layers** and uploads **21,213,184 bytes (~20.23 MiB)** of new disk layer plus image metadata. No guest VM-state/memory artifacts are created by this capture. |
| Restore after removing publisher data | Pass | Remove publisher state, the sealed capture and the prefetched files. Fresh-state S3 cold creation takes ~1.38 seconds API time; file hashes, UID/GID/mode, symlink and SSH host key match, boot ID changes, and explicitly restarted desktop/screenshot services work. The prepared 20 GiB image no longer needs resizing on this path. |

These layer sizes are not the entire bucket footprint. Image-build/builder publications leave additional objects: the run prefix contained **5,239,145,929 bytes (~4.88 GiB)** at the end. The 1.58 GiB base and 20.23 MiB delta describe the tested image chain; retaining, collecting and budgeting unrelated/build artifacts remains necessary.

The app clone documents `lewbox-agentd`; its implementation is in the separately supplied gateway checkout `/home/homelab/dev/legacy/lewbox-gateway`. The follow-up below exercises that implementation. Authenticated vendor-agent calls, the real app/gateway, browser WebSocket streaming and external SSH/proxy traffic remain separate integration tests.

#### Legacy lewbox-agentd inside AgentENV

The gateway checkout at `/home/homelab/dev/legacy/lewbox-gateway`, commit **`c9f21ab2b14944a7e10d47cc1e935b7899abed8d`**, contains `cmd/lewbox-agentd` and `internal/agentd`. No source changes were made there. [The build helper](prepare-agentd.py) downloads Go **1.27.1** (the module's declared version), verifies its archive against [official download metadata](https://go.dev/dl/?mode=json&include=all), and builds with `CGO_ENABLED=0`, an isolated HOME/cache/temp environment, and no system-wide Go installation.

**All 91 existing daemon tests passed**; [build/provenance and binary hashes](results/agentd-build-20261030-214955.json), [unit test events](results/agentd-unit-20261030-214955.jsonl). The stripped daemon is 7,725,216 bytes. Earlier host-test attempts used temporary paths too long for the Unix sockets created by the tests; those harness failures are retained in their own reports and were resolved by shortening TMPDIR, without changing the gateway source. The race detector and the entire gateway package suite were not run.

The [VM harness](agentd.py) then passed **all eight integration scenarios** using the actual daemon executable, the repository's compiled **fake ACP adapter**, and a guest-loopback upload receiver. [Complete result](results/agentd-20261030-215105.json). The VM boots the previously prepared 20 GiB desktop image, so this does not repeat or fix the initial remote-image resize issue.

| Agentd integration experiment | Result | Evidence / scope |
|---|---|---|
| Upload/run the real binary | Pass | Both binary hashes match; calling without a serving daemon exits 3. No Go toolchain is installed inside the guest. |
| CLI, events and duplicate start | Pass | Prompt yields the scripted Hello response, numbered events and cursor filtering work, and a second `serve` leaves the running daemon alive. |
| Approval/reply | Pass | The real CLI receives an approval request, sends `allow_once`, and observes answered/finished events. The adapter's action is scripted. |
| Queue/idempotency | Pass | An active slow turn queues a prompt. Repeating its message ID leaves exactly one queued message. |
| Disk-only checkpoint | Pass through prototype | Seal and publish while a turn is active and another is queued. Adds **21,327,872 bytes (~20.34 MiB)** to the existing image chain; no guest VM-state/memory snapshot is taken. |
| Cold recovery | Pass | Remove publisher state/capture, restore from fresh node state, and restart agentd. Boot ID changes, the daemon binary survives without re-upload, the old turn ends interrupted, the queued turn completes once, and event IDs continue in order. |
| Pending upload replay | Pass to local fixture | Before capture the receiver returns 503 and uploaded-through is zero. After cold restore the receiver accepts the synthetic authorization and agentd replays through event 7, matching its latest event. This does not validate real gateway JWTs, app persistence or expiry/refresh. |
| Continue an existing conversation | Pass | A new prompt after cold restore completes turn 2 in the existing conversation. |

**Credential-policy finding:** the actual daemon persists its gateway `upload_token` in the conversation's JSON metadata. The experiment confirms the field exists with a public synthetic token; no account login or real bearer token is imported. Production stop/start must account for these daemon-owned credentials as well as CLI credential paths, including refresh/expiry and any scrub-before-publication rule. Credentials and the upload destination must be restored before starting agentd, because queued work and upload replay begin automatically on daemon startup.

These results qualify daemon transport/state/recovery with a controlled ACP peer. They do **not** qualify the actual Claude/Codex adapters, authenticated model calls, the real gateway/app, or browser-facing SSE. To repeat with the lab stopped and its synthetic S3 image chain retained:

```bash
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/prepare-agentd.py
python3 experiments/agentenv/agentd.py
```

#### Imported credential cleanup

The [credential cleanup probe](credential-cleanup.py) tested the legacy paths `/root/.config/lewbox/env` and `/root/.codex/auth.json` with **synthetic** 8 KiB credential files. It flushed them, performed the legacy-style `rm -f`, verified both paths were absent, then sealed the root disk through the disk-only helper. **The canary remained in the raw sealed layer.** No RAM snapshot was taken and these layers were never uploaded. [Both results](results/credential-cleanup-20261030-213650.json) are blocked against the credential-exclusion requirement.

A second fresh guest tried `fstrim /` before capture. It failed with `the discard operation is not supported`, and the canary remained. This is guest filesystem discard through the VM block device. The successful host-side trim in the pressure test instead punches free extents out of the lab's loop backing file; it does not establish guest discard support or credential erasure.

This is a concrete failure of unlink-only cleanup for these paths, not proof that every possible scrubbing design fails. A supported design must prevent imported credentials from reaching retained disk layers, or export/verify a sanitized live filesystem without carrying deleted blocks and earlier immutable secret-bearing layers. Refresh/harvest handling, atomic file replacement and tool-specific credential paths also need qualification. Passing the memory-only canary test alone does not satisfy the complete imported-credential contract. No personal credentials or real agent logins were used in any probe.

#### Native storage failure recovery

The [fault harness](faults.py) exercises AgentENV's checkpoint API with synthetic files and a previously verified durable checkpoint. [The complete-store outage run](results/faults-20261030-211157.json) passed four scenarios: the source remained running with no dangling live-layer paths, both files were readable after S3 returned, retry restored the latest files, and the older checkpoint remained restorable. The failed request committed no new catalog record.

That outage failed during a catalog read, before a layer upload. It is a preflight failure result, not proof of recovery after sealing or partial publication. The harness also supports a separate managed-layer PUT failure through a loopback-only [proxy](faultproxy.py), selected with `LEWBOX_FAULT_KIND=upload`. No actual R2 account is involved.

The [partial-publication run](results/faults-20261030-213617.json) also passed all four scenarios. Its loopback proxy allowed **one new managed layer (16,874,286 bytes)** to reach S3, then rejected subsequent managed-layer PUTs with HTTP 500 (four rejected attempts). The native snapshot request failed during upload; the source remained running, both files were readable, and no live layer paths were dangling. No new catalog checkpoint was committed. Once failures were disabled, retry restored both latest files, and the older checkpoint still restored its original state. This tests partial object publication and retry, not a transport disconnect halfway through one object's bytes or actual R2 behaviour. Uncommitted objects still need a GC policy.

With the lab otherwise stopped, repeat each bounded test separately:

```bash
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/pressure.py
python3 experiments/agentenv/faults.py
LEWBOX_FAULT_KIND=upload python3 experiments/agentenv/faults.py
python3 experiments/agentenv/workload.py
python3 experiments/agentenv/credential-cleanup.py
```

`pressure.py` requires at least 70 GiB free on the host, writes three genuinely full guest filesystems and guards a 12 GiB host reserve. Its node-full injection uses `posix_fallocate` to reserve filesystem space without writing 70 GB of extra payload. The workload's original native 20 GiB published-image path currently fails as documented above. To repeat the passing private-cache workaround without rebuilding, keep the synthetic S3 fixture data and run:

```bash
LEWBOX_WORKLOAD_IMAGE=sha256:8e2a1bf136a1c5d0a13997a7fed3cfc213ddf9bd4bdd2762ffa47238c0d43570 \
LEWBOX_WORKLOAD_PREFIX=workload-20261030-212321/ \
LEWBOX_WORKLOAD_HYDRATE=1 python3 experiments/agentenv/workload.py
```

### Remote storage size

For the separate S3 experiment, the first snapshot stored **29.29 MiB total**: about 5.83 MiB disk layers, 23.45 MiB memory, plus metadata. After an 8 MiB random write, the next snapshot reused both existing disk layers and uploaded about **8.06 MiB of new disk layers**, **32.59 MiB of memory**, and metadata: **40.67 MiB of new objects**. Older objects remained present. These are compressed object sizes for this tiny workload, not estimates for a desktop image or R2 billing.

Thus base/delta disk reuse works, but memory can dominate the checkpoint cost. The POSIX and S3 measurements came from separate runs and settings apply publication compression to the remote path; their totals are not directly comparable as a compression benchmark. Those original size measurements did not exercise interrupted uploads, retries, GC or compaction; separate failure experiments are described above.

### Hardening observations

The running VMM used UID/GID 1000, zero effective/ambient capabilities, `NoNewPrivs=1`, and seccomp filter mode 2 on its running threads. Idle pooled processes differ before VM startup. [Process inspection](results/process-inspection.json) retains these observations.

In this lab the VMM root was `/`, its UID was shared, and its cgroup was the launching editor's scope; no per-VM cgroup/jailer deployment was configured or demonstrated. Guest allocation is not proof of a VMM host-memory limit or CPU quota. These observations are not a security audit or a production deployment recommendation.

The committed snapshot records reported `kernel_version` and `firecracker_version` as `unknown` despite pinned binaries. Explicit compatibility identity and rejection tests remain necessary for Lewbox's cross-node restore contract.

## Run the local lab

The lab was stopped and temporary host configuration restored after testing. Downloads, configs, synthetic snapshots and test-store data are retained. No systemd service was installed. Use a fresh terminal:

```bash
cd /home/homelab/dev/lewbox-agentenv
source experiments/agentenv/lab.env.sh

# Already downloaded. Rechecks archive hashes; extracts only into empty targets.
python3 experiments/agentenv/download.py
python3 experiments/agentenv/configure.py

# Already available on this host. curl and umoci were installed during setup.
# A fresh host also needs the commands in AgentENV's dependency manifest.
# sudo apt-get install curl jq umoci zstd acl iproute2 iptables e2fsprogs util-linux
"$LAB_ROOT/server/server" --setup-only --config "$AENV_CONFIG_PATH"
bash experiments/agentenv/setup-host.sh
bash experiments/agentenv/start.sh
```

`start.sh` runs in the foreground using upstream's non-root capability wrapper and a lab lock. It binds the API to `127.0.0.1:18080` and storage metrics to `127.0.0.1:19103`. The wrapper obtains KVM supplementary-group membership without restarting the desktop session. Long custom home paths can exceed Firecracker's Unix-socket pathname limit; keep the default or use a shorter path.

In another fresh terminal:

```bash
cd /home/homelab/dev/lewbox-agentenv
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/auth.py
python3 experiments/agentenv/smoke.py
python3 experiments/agentenv/inspect-storage.py
python3 experiments/agentenv/build.py
aenv list --output json
```

`smoke.py` leaves a paused synthetic sandbox and named snapshots. `build.py` leaves reusable synthetic templates. Each run uses new names. For the restart check, run `stop.sh`, start the server again, then `python3 experiments/agentenv/restart.py`; run this while the test background process is still alive (20 minutes of guest runtime). The source and reference IDs are in the newest successful smoke result.

## Repeat the S3 experiment

Stop the POSIX server first. Start the fixture in its own terminal:

```bash
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/start-s3.py
```

In a second terminal, generate the two configs and start the publisher:

```bash
source experiments/agentenv/lab.env.sh
python3 experiments/agentenv/configure-s3.py
AENV_CONFIG_PATH="$LAB_ROOT/config-oss-publisher.toml" \
AENV_HOME_PATH="$LAB_ROOT/op" AENV_RUNTIME_PATH="$LAB_ROOT/rop" \
  bash experiments/agentenv/start.sh
```

In a third terminal:

```bash
source experiments/agentenv/lab.env.sh
export AENV_CONFIG_PATH="$LAB_ROOT/config-oss-publisher.toml"
export AENV_HOME_PATH="$LAB_ROOT/op" AENV_RUNTIME_PATH="$LAB_ROOT/rop"
python3 experiments/agentenv/auth.py
python3 experiments/agentenv/remote.py publish
bash experiments/agentenv/stop.sh
```

With the publisher stopped, move aside the old restore state before repeating the fresh-state test. Then start the restore server in the second terminal:

```bash
mv "$LAB_ROOT/or" "$LAB_ROOT/or.before-$(date +%s)"
AENV_CONFIG_PATH="$LAB_ROOT/config-oss-restore.toml" \
AENV_HOME_PATH="$LAB_ROOT/or" AENV_RUNTIME_PATH="$LAB_ROOT/ror" \
  bash experiments/agentenv/start.sh
```

In the third terminal, change the exported settings to `config-oss-restore.toml`, `or`, and `ror`, run `auth.py`, then `remote.py restore`. The retained bucket contains older experiment objects; aggregate size totals from repeated publication runs will include those objects. No production credentials are used; generated fixture credentials/configs stay outside Git with mode 0600.

## Stop and restore host settings

```bash
bash experiments/agentenv/cleanup-host.sh
```

This stops the lab and fixture, removes their five subnet-specific firewall rules, restores original IP forwarding and KVM ACLs, removes the added KVM group membership and lab udev/OverlayBD files, and unloads ublk if the lab loaded it. It retains downloads/state and the installed `curl`/`umoci` packages. It does not install a service or flush unrelated firewall rules. Baseline settings are recorded outside Git under `host-before`. The [extended-run cleanup audit](results/host-cleanup-continued.json) confirms ports/processes stopped, forwarding/group/device/config/firewall changes restored, and the bounded filesystem/loop device removed.

Setup discoveries: OverlayBD requires `/etc/overlaybd/overlaybd.json` even when conversion supplies its own config; desktop logind/udev can reset temporary KVM ACLs; long paths prevent Firecracker's API socket from binding. The scripts account for these. An early duplicate-server attempt also produced stale namespace errors; the launcher now takes a lock. Subnet firewall rules remained after the startup/shutdown experiments and were explicitly removed by cleanup.

## Current integration problems

[PROBLEMS.md](PROBLEMS.md) is the maintained register of problems, potential fixes, validation gaps and related Lewbox plans. RAM-inclusive snapshots are accepted, so the historical no-memory failures are not independent adoption blockers. The register covers credential handling, 20 GiB image preparation, lifecycle/placement, storage retention, deployment limits and the remaining application/provider tests.

AgentENV Rust/Go source unit suites were **not run**: that evaluation used published release binaries and real KVM integration experiments. Separately, the unchanged legacy gateway daemon passed its 91 existing Go tests and eight VM integration scenarios with the scripted adapter. No upstream code or product implementation was changed. Full-root disk-only cold restoration is also demonstrated through an optional experimental adapter; the latest acceptance decision permits the native memory-inclusive path.
