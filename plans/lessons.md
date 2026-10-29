# Lessons from legacy

> Status: Draft · Milestone: All · Updated: 2026-10-29

Legacy Lewbox ran on live clusters for a month (2026-10-06 to 2026-10-29) and found, the hard way, a long list of things that don't work the way you'd expect. This is that list, so the rebuild doesn't learn them again. Each lesson says what happened, where it's recorded (a commit in `lewbox-legacy` (app), `lewbox-gateway-legacy` (gw) or `lewagent-legacy` (la), or a doc), and which plan now handles it.

Each lesson about AerolVM now maps to **a fix in our fork** (its backlog id in [clusters/aerolvm-fork](clusters/aerolvm-fork.md#fix-backlog)), or to *gone with gVisor out of the alpha*, never to a workaround in Lewbox ([D15](README.md#decisions)). When you find a new one, add it here, with its source and the fix or plan that handles it.

## AerolVM and its toolbox

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **toolboxd loses exit statuses under gVisor** | As PID 1 it reaps a command before waiting for it. It recovers from `ECHILD`, but gVisor answers `ESRCH`, so a command that worked reports `exit_code -1` and `waitid: no such process`. Setup, the private-port proxy's install and SSH key writes were all reported failed. Sessions were reported `killed` with that as their signal. Still so in `v0.6.8` | gw `febb176`, `cf9813a`, `590bbc2`; app `docs/todo.md` l.51–54 | Gone from the alpha with gVisor; fixed in the fork (F1) before `gvisor` is offered in the beta. No traps in Lewbox ([clusters/aerolvm-fork](clusters/aerolvm-fork.md#process-and-exec-toolboxd)) |
| **Don't trust a lost exit status for anything, including `chmod`** | Installing the private-port proxy failed because `chmod`'s status was lost; the fix checks the file's mode with `stat` instead | gw `4f0da62` | Gone with gVisor out of the alpha; F1. Lewbox has no `stat` check ([D15](README.md#decisions)) |
| **A resumed sandbox doesn't run its image's command** | `docker commit` copies the entrypoint (`toolboxd`) into the snapshot, so a resume runs `toolboxd toolboxd <cmd>`. The inner one can't bind 2280 and exits, and a desktop template came back without its desktop. Each stop and start adds another layer of it | gw `6e36f0a`; gw `docs/aerolvm.md`; seen on EU West 2026-10-22 | Fork fix F10 (P0), legacy snapshots included; Lewbox records no command ([sandboxes/persistence](sandboxes/persistence.md#the-images-command)) |
| **toolboxd session names aren't unique** | Every create starts a new session, even with a running one's name. Exposing a private port again started a second proxy, which couldn't bind and exited; the failed expose then stopped the working one and forgot the port | gw `5f3d19e`; Lewagent's `sbx_S2NLeK88raX9nw`, 2026-10-27 | Fork fix F2 (P0, `if_absent`); Lewbox never lists first ([inside-a-sandbox/private-ports](inside-a-sandbox/private-ports.md)) |
| **Activity counts only when a request starts** | A long stream (download, quiet terminal) doesn't keep a sandbox awake, so the idle timeout stopped busy sandboxes. Conversely, polling a session's status *does* count, so watched sessions kept sandboxes from idling | gw `docs/aerolvm.md`; gw `cf9813a` | Fork fixes F5 and F55 (P0); Lewbox doesn't touch while streams move ([sandboxes/timers-and-activity](sandboxes/timers-and-activity.md)) |
| **The toolbox isn't there for a few seconds after a start** | The proxy answers `502 toolbox unavailable` (plain text) or `503 sandbox not adopted` | gw `docs/aerolvm.md` | Fork fix F4 (P0, `toolbox_ready`); Lewbox waits for the signal rather than retrying on `502`/`503` ([inside-a-sandbox/toolbox](inside-a-sandbox/toolbox.md)) |
| **A dropped exec stream leaves its command running** | The toolbox doesn't kill on disconnect; the gateway sends `KILL` first | gw `docs/aerolvm.md` | Lewbox's own design: a streamed command whose client leaves is killed ([inside-a-sandbox/toolbox](inside-a-sandbox/toolbox.md#streams)) |
| **Downloads are read whole into memory** | envd's and the older download routes use `os.ReadFile`; the old multipart upload caps at 256 MB | gw `docs/aerolvm.md` | Fork fix F7 (P1); until then a documented size limit ([inside-a-sandbox/files](inside-a-sandbox/files.md)) |
| **AerolVM errors come in three shapes** | `{"error"}`, envd's `{"code","message"}`, and the proxy's plain text; `400` for nearly everything, including an image that can't be pulled | gw `docs/aerolvm.md` | Fork fix F6 (P0); Lewbox maps codes only |
| **Capacity errors have no code** | Recognised by wording, which changed: builds prefix the message, so the match went from prefix to contains | gw `e1573fa` | Fork fix F34 (P0); no phrase matching ([clusters/placement-and-capacity](clusters/placement-and-capacity.md)) |
| **A create whose client disconnects is dropped** | Killing the gateway mid-create left nothing in AerolVM; the resumed operation created it afresh. So every step must be idempotent, and a retried create adopts the sandbox already tagged with its id | gw `docs/aerolvm.md`, `docs/operations.md` | F32; [sandboxes/operations](sandboxes/operations.md) |
| **AerolVM stops sandboxes silently** | Idle and age timers stop or destroy sandboxes without telling anyone; the reconciler finds out within a minute and works out why from the timers. Nothing can run in the sandbox first, and nothing can put the stop off when Lewbox knows the sandbox is busy | gw `fb1fdc0` | F35, F50 (notify-mode timers: Lewbox decides when to stop, the node stops only as a backstop), F49; [sandboxes/timers-and-activity](sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop), [sandboxes/reconciler](sandboxes/reconciler.md) |
| **`stop_at_age` doesn't reset on start** | Age counts from AerolVM's create, so an in-place start must move it on (`PUT …/lifecycle`, which replaces every timer) | gw `docs/aerolvm.md` | F38; [sandboxes/timers-and-activity](sandboxes/timers-and-activity.md) |
| **Finding a sandbox's node needs the sandbox index** | Sandboxes don't name their node. `X-Cluster-Forwarded` from outside is refused (`403 … requires mTLS`). `GET /v1/cluster/sandbox-index` (PAT, <200 ms live) has `owner_node_id` | gw `docs/aerolvm.md` | Fork fix F37 (P0); Lewbox doesn't use the sandbox index ([clusters/placement-and-capacity](clusters/placement-and-capacity.md)) |
| **Snapshot status and deletion live only on the Daytona facade, on the snapshotting node** | Not on `/v1`, and not forwarded | app `docs/clusters.md` § *Sandbox offload* | Fork fix F12 (P0); no Daytona facade |
| **A node without push reports snapshots `active` at once** | Though they never left the node, so offload only runs where the cluster has storage | gw `docs/aerolvm.md` | F13; [clusters/storage-and-registry](clusters/storage-and-registry.md) |
| **Build placement needs `docker` listed, even on gVisor nodes** | With `SB_HOST_RUNTIMES=gvisor`, every build failed: `no worker placement target available` | app `292f3f7`; `docs/examples/fix-gvisor-host-runtimes.sh` | Gone from the alpha with gVisor; fork fix F22 for the beta ([clusters/runtimes](clusters/runtimes.md#gvisor-gvisor-beta)) |
| **Builds reuse a cached image** | A Dockerfile is tagged by its hash and node, so rebuilding unchanged within a day reuses the image instead of fetching newer packages | app `docs/clusters.md` § *Templates* | F29; [templates/builds](templates/builds.md) |
| **Builds block, and report only Docker's last error** | `POST /v1/images/build` answers when done (up to 25 min), with no logs | gw `docs/aerolvm.md` | F32, F33; [templates/builds](templates/builds.md) |
| **AerolVM's 10-minute build timeout is too short** | A desktop template ran out (`image build exceeded timeout`); 20 minutes on nodes, 25 in the gateway around build and push | app `a207232` | [clusters/node-agent](clusters/node-agent.md#desired-state) (spec) |
| **Pin only releases with assets** | `v0.6.5` was published with no release assets, so it couldn't be installed | app `docs/clusters.md` § *AerolVM version* | F25; [clusters/aerolvm-fork](clusters/aerolvm-fork.md#release-assets) |
| **A pin isn't what nodes run** | Bugs were seen on `v0.6.8` and `main` while the pin said `v0.6.4` | gw `docs/aerolvm.md` | Nodes report versions: [clusters/aerolvm-fork](clusters/aerolvm-fork.md#versions-in-lewbox) |
| **The PAT is presented to every AOCR host** | `SB_IMAGE_DISTRIBUTION_AOCR_HOST` defaults to `aocr.aerol.ai`, and the cluster PAT goes to every host listed | app `docs/clusters.md` § *Registry* | F18 (security) |
| **Env durations need units** | `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30` is ignored (keeps 5 minutes); it must be `30s` | app `docs/clusters.md` | F17; the spec renders durations |

## gVisor

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Persisted gVisor sandboxes lost their files** | runsc kept writes in its own overlay (`/.gvisor.filestore.<id>`), which `docker commit` can't read; a stop snapshotted the bare template. Fix: `--overlay2=none` (slower I/O). Re-running AerolVM's `install.sh` drops the flag | app `bfa60f5`; `acp.md` § *A stop and start lost the sandbox's files*; `docs/examples/fix-gvisor-overlay.sh` | Gone from the alpha with gVisor; fork fixes F21, F24 for the beta ([clusters/runtimes](clusters/runtimes.md#gvisor-gvisor-beta)) |
| **Docker reloads runtimes on SIGHUP** | So runsc flags change without stopping running containers. Only sandboxes started afterwards get them | `fix-gvisor-overlay.sh` | [clusters/node-agent](clusters/node-agent.md#desired-state) |
| **Codex's bubblewrap can't start under gVisor** | `bwrap: loopback: Failed RTM_NEWADDR: No child process`, so only Codex's `agent-full-access` mode works | app `docs/acp.md` l.143, 668 | Gone from the alpha with gVisor (F28); in Docker an M3 spike checks it ([agents/models-and-modes](agents/models-and-modes.md)) |
| **gVisor needs cgroup v2** | Preflight checks `/sys/fs/cgroup` is `cgroup2fs` | app `install.sh` preflight | Beta, with gVisor ([clusters/installation](clusters/installation.md)) |

## Snapshots, the registry and storage

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Registry paths must be lowercase** | AerolVM puts the cluster id in snapshot paths without lowercasing it; Docker refuses capitals, so every push failed | app `docs/todo.md` l.42; `docs/clusters.md` | Fork fix F11 (P0); Lewbox passes ids as they are |
| **Pulled snapshot images pile up on nodes** | A resume pulls the snapshot; AerolVM never removes it (~5 GB after a few test resumes) | app `docs/todo.md` l.44–46 | Fork fix F16 (P0); no clean-up timer anywhere ([clusters/node-agent](clusters/node-agent.md#docker-disk-use)) |
| **Layers stack with every stop** | Each snapshot is a commit on the last; overlay caps at ~125 layers; deleted files still cost space and download | app `docs/todo.md` l.61 | F14; [clusters/storage-and-registry](clusters/storage-and-registry.md#flattening) |
| **Offload is limited by gzip, not the network** | 512 MB of new data: ~65 s to stop (29 snapshot, 35 upload at ~26 MB/s on one core, though the node uploads at ~147 MB/s), 17 s to resume | app `docs/todo.md` l.47 | F15 |
| **Timer stops snapshot whatever is in the sandbox, credentials included** | AerolVM stops idle and old sandboxes itself, with no hook, so legacy's cleanup before a stop never ran for them, and agent logins and Git tokens went into the project's bucket. Legacy accepted it. `docker commit` also copies the container's `Env`, so create-time `env` may be in snapshot image configs (to confirm live) | app `docs/clusters.md` § *Sandbox offload*; gw `internal/operations/sandboxes.go` | F46–F48; [sandboxes/profile-setup](sandboxes/profile-setup.md#credentials-never-enter-a-snapshot) |
| **A failed upload held a stop for 5 minutes** | AerolVM's default retry interval; set to 30 s | app `docs/clusters.md` § *Registry* | The spec |
| **R2 needs equal multipart parts** | "All non-trailing parts must have the same length"; `chunksize` 100 MiB, `forcepathstyle: true` with `regionendpoint` in Distribution v3; `MemoryMax=512M`, `GOMEMLIMIT=400MiB` | app `docs/clusters.md` § *Registry* | [clusters/storage-and-registry](clusters/storage-and-registry.md) |
| **`garbage-collect` can't run while any registry pushes** | So GC works on the bucket's layout directly, with a grace period, listing blobs before links | app `CleanUpRegistry`, `docs/clusters.md` | [clusters/storage-and-registry](clusters/storage-and-registry.md#garbage-collection) |
| **A template build per repository** | So clean-up can delete an old build with its repository | app `5b781f7`, `docs/clusters.md` | [clusters/storage-and-registry](clusters/storage-and-registry.md#whats-stored) |
| **Without a registry, built images vanish after a day** | AerolVM's janitor deletes node-local builds unused for 24 h, so templates need storage | app `docs/clusters.md` | [templates](templates.md) |

## Nodes and installation

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Nothing updates installed nodes** | Every fix (firewall TCP pool, cleanup timer, build timeout, push interval, gVisor flags, heartbeats) needed a by-hand step on nodes installed before it, or a reinstall | app `docs/todo.md` l.45, 49, 74, 111; `docs/clusters.md` § 8 | [clusters/node-agent](clusters/node-agent.md#desired-state) |
| **An old firewall script refused new rule shapes** | The TCP pool had to go in a new `public_ranges` key, and old nodes still needed their script replaced | app `docs/private/node/installed-nodes.md` | The spec, versioned |
| **`curl \| bash` swallows stdin** | Anything that reads stdin eats the rest of the script, so AerolVM's scripts ran with `/dev/null` | app `docs/clusters.md` § 3 | [clusters/installation](clusters/installation.md) (Go installer, bootstrap only) |
| **Arguments are visible to every process** | Request bodies with `ca.key` went to curl on stdin, and headers in files | app `docs/clusters.md` § 3; `installed-nodes.md` | Secrets via files ([clusters/installation](clusters/installation.md#security)) |
| **AerolVM's scripts print secrets** | `install.sh` prints the PAT, `cluster-init.sh` the gossip key; output was redacted | app `docs/clusters.md` § 3 | [clusters/installation](clusters/installation.md#security) |
| **apt prompted during install** | `DEBIAN_FRONTEND=noninteractive` | app `7c30ca6` | [clusters/installation](clusters/installation.md) |
| **AerolVM's `install.sh` needs `file`** | Without it, a valid Caddy binary is refused as "did not return an ELF binary" | app `docs/clusters.md` | Fork fix F23 (P0); the node agent doesn't install it first |
| **`cluster-join.sh` exits 2 on success** | Pass 1's "CSR ready" is exit `2`, not a failure | app `docs/clusters.md` § *How nodes join* | [clusters/installation](clusters/installation.md#steps) |
| **The node id must come from Lewbox** | `cluster-sign-node.sh` trusts `--node-id`; a node choosing its id could impersonate another over cluster mTLS | app `docs/clusters.md` | [clusters/pki-and-dns](clusters/pki-and-dns.md#node-certificates) |
| **Healthy isn't joined** | A node with the wrong gossip key or blocked ports answers `/health` on its own, so joining checks another node is alive in membership | app `docs/clusters.md` § 3 | Membership-confirmed `online` ([clusters/installation](clusters/installation.md#statuses)) |
| **AerolVM doesn't renew node certificates** | 90 days; an expired one cuts a node off. It reloads a replaced `node.crt` without restarting, for the same id only | app `docs/clusters.md` | [clusters/pki-and-dns](clusters/pki-and-dns.md#renewal) |
| **Node API limits must be per token, not per IP** | Nodes behind one NAT shared a limit | app `0c244e2` | [clusters/node-agent](clusters/node-agent.md#node-api) |
| **A finished installation's token must be refused** | So a command run again opens nothing (`410`) | app `af6762a` | [clusters/installation](clusters/installation.md) |
| **One public IP per node** | Homelabs behind one router weren't supported: every cluster address was the public IP | app `docs/clusters.md` § 3 | The tunnel ([clusters/connectivity](clusters/connectivity.md#nodes-behind-one-public-ip)) |
| **Underscores can't get certificates** | Node subdomains are `node-<id>` with a hyphen | app `Node::subdomainOf()` | [clusters/pki-and-dns](clusters/pki-and-dns.md#records) |
| **Port-URL records must not be proxied** | A proxied record terminates TLS at the CDN, and AerolVM routes by SNI | app `docs/clusters.md` § *DNS* | [clusters/pki-and-dns](clusters/pki-and-dns.md#records) |
| **Let's Encrypt: 50 new certificates per domain per week** | Each node takes two | app `docs/clusters.md` § *Not built yet* | [clusters/pki-and-dns](clusters/pki-and-dns.md#public-tls-on-nodes) |
| **Cluster ports must never face the internet** | AerolVM's guide: gossip carries auth tokens, Raft the full state; the trial node had them open | app `docs/clusters.md` § 7 | [clusters/node-agent](clusters/node-agent.md#firewall) |

## Private ports

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Tell services the browser came over HTTPS** | AerolVM terminates TLS in front of the proxy, so Laravel made `http://` links (mixed content) until portd set `X-Forwarded-Proto: https` | gw `7149a8c` | [inside-a-sandbox/private-ports](inside-a-sandbox/private-ports.md) |
| **Iframes need a partitioned third-party cookie, and a fallback** | `SameSite=None; Partitioned`; where it's blocked, the port answers `401` with a page posting `lewbox:port-unauthorized` | gw `internal/portd`; docs | [inside-a-sandbox/private-ports](inside-a-sandbox/private-ports.md) |
| **A link to a port that isn't up lands on an error page** | Lewagent curls the desktop port through commands before making a link | la `CheckPort` | [inside-a-sandbox/private-ports](inside-a-sandbox/private-ports.md) (`port_unreachable`) |

## Coding agents

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **One argument is at most 128 KiB on Linux** | Sending agentd's request as `--base64` failed for an 88 KB image (the edge answered 502); requests now go in a file | gw `4776361` | [agents/agentd](agents/agentd.md) |
| **Start agents in a login shell** | As a plain session, agentd's adapters lacked the profile's logins and Claude said "Authentication required" | gw `37a5808`; app `acp.md` l.246 | [agents/agentd](agents/agentd.md) |
| **Replace old agentd builds only when idle** | Each call names the expected build; an idle daemon of another build answers `upgrade` and stops | gw `37a5808` | [agents/agentd](agents/agentd.md) |
| **Claude's cost is cumulative** | `total_cost_usd` is the session's running total; each turn's is the delta | gw `4c370f2` | [agents/turns-and-events](agents/turns-and-events.md) |
| **Codex's command output is in `_meta.terminal_output_delta`** | It was dropped, so listings showed only exit codes | gw `b89e993` | [agents/agentd](agents/agentd.md) |
| **Agents leave tool calls open** | Codex ends a turn without closing a call (e.g. `ffplay`), so chats showed it running forever; open calls are closed as `unreported` | gw `84a9a50` | [agents/turns-and-events](agents/turns-and-events.md) |
| **Hold `finished` until what's stored is uploaded** | So a client can prompt again, or read the stored turn, at once (up to 5 s) | gw `56a4a76` | [agents/turns-and-events](agents/turns-and-events.md) |
| **A turn waiting on an approval is quiet** | Following stopped after 5 minutes without an event; open turns are now followed for up to an hour | gw `f91b9f6` | [agents/approvals-and-questions](agents/approvals-and-questions.md) |
| **The title can arrive after the turn ends** | Claude's `session_info_update` follows the prompt's response | app `acp.md` | [agents/turns-and-events](agents/turns-and-events.md) |
| **Each conversation peaks at ~1 GB** | A 4 GB sandbox runs about three; idle chats keep their processes (17 chats ~2.5 GB) | app `acp.md` § *Memory*; la `docs/todo.md` l.521 | [agents/agentd](agents/agentd.md) (unload idle) |
| **Two Codex chats starting at once race on `~/.codex`** | "failed to initialize sqlite state runtime" | la `docs/todo.md` l.520 | [agents/agentd](agents/agentd.md) |
| **`npm install --omit=optional`** | Adapters install at 57 MB instead of 756 MB, without bundled agents | app `acp.md` | [agents/agentd](agents/agentd.md) |
| **Instructions must survive idle stops and restarts** | Applied on every `session/new` and `session/resume`: Claude's `systemPrompt` append, Codex's `developer_instructions` via `CODEX_CONFIG` | gw `6d95391`; app `acp.md` l.287 | [agents/conversations](agents/conversations.md) |
| **Claude subscription tokens: legal question** | Anthropic's terms on storing them; ask before building on it | app `acp.md` | [profiles/agent-sign-ins](profiles/agent-sign-ins.md) |

## API, streaming and clients

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Wait for the operation, not the status** | A sandbox's status changes a moment before its operation finishes, so a request sent on `stopped` can get `operation_in_progress` | Docs (*Operations*); gw `docs/operations.md` | [sandboxes/operations](sandboxes/operations.md) |
| **CORS blocks `Last-Event-ID` from browsers** | Lewagent resumes streams with `?after=` | la `docs/todo.md` l.83 | [api/streaming](api/streaming.md) |
| **WebSockets can't carry headers in browsers** | The key goes as subprotocols `lewbox.bearer, <key>`; only `lewbox.bearer` is echoed | gw `docs/authentication.md` | [api/authentication](api/authentication.md) |
| **Sandbox keys refused scopes by name** | `sandboxes.ports.view` and `sandboxes.agents` were refused, so Lewagent fell back to all-permission keys | gw `ddab60a`; la `AgentKeyController` | [api/permissions](api/permissions.md) |
| **The spec drifted from the server** | Tasks and agent-upload routes are missing from `openapi.yaml`; chunks' `external_id` was missing from responses until noticed | gw `api/openapi.yaml`; gw `c1a8d71` | [api/openapi-and-codegen](api/openapi-and-codegen.md) |
| **Split UTF-8 in streams** | SSE output is decoded on UTF-8 boundaries | gw `internal/server/sse.go` | [api/streaming](api/streaming.md) |
| **Colour terminals need `TERM`** | `TERM=xterm-256color`, `COLORTERM=truecolor` in PTYs | gw `e1fd978` | [inside-a-sandbox/sessions](inside-a-sandbox/sessions.md) |
| **Run commands in a login shell** | So `/etc/profile.d` and the setup env load (`bash -lc`, else `sh -lc`) | gw `inside.go`; app `acp.md` | [inside-a-sandbox/commands](inside-a-sandbox/commands.md) |
| **Shutdown races the toolbox's exit message** | A 1001 close can arrive before or after the exit message | gw `f7157ca` | [api/streaming](api/streaming.md) |

## Platform and operations

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **Edges cut plain requests at ~20 s** | Laravel Cloud's edge answered `504` on `POST commands` with `sleep 75`, and on a create's wait (the sandbox was still created). Streams with keepalives were fine | app `docs/todo.md` l.76 | [api/streaming](api/streaming.md) (keepalives; long waits as `202` + operation); deployment docs |
| **Cloudflare's Browser Integrity Check refuses Python's `urllib`** | `403` error 1010; `requests`, `httpx`, Guzzle, Node and Go get through | app `docs/todo.md` l.77; `acp.md` | SDKs set a User-Agent ([sdks-and-cli](sdks-and-cli.md)); deployment docs |
| **Trust only the load balancer's proxy headers** | Laravel Cloud's LB in production, localhost only in development | app `10ace2b` | [foundation/configuration](foundation/configuration.md) (`LEWBOX_TRUSTED_PROXIES`) |
| **A killed scheduled run held its lock for a day** | `withoutOverlapping()` defaults to 24 h; a deploy killed `sign-ins:advance` mid-run and every later run was skipped (sign-ins stuck "Starting"). `MarkOfflineNodes` still had the day lock | app `d63dea0`; `routes/console.php` | Leases that lapse in seconds ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md)) |
| **A 5 s operation poll was a stopgap** | Operations the app created waited for a replica's next poll (5 s, then 1 s), as there was no wake-up | gw `d330477` | `LISTEN/NOTIFY` ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md)) |
| **Every replica ran every loop** | Reconciler and session watcher on each replica; preconditions (`expected_status`, `expected_aerolvm_id`) made duplicates harmless | gw `internal/reconcile` | Leader-only jobs, with the same preconditions |
| **Make metering overlap-safe** | Metering sets usage from each run's cost rather than adding to it | app `d63dea0` | [credits/metering](credits/metering.md) |
| **MySQL limits index names to 64 characters** | A migration failed and left a half-made table | app `ac18298` | Postgres only (identifiers up to 63 bytes, so the same care applies; [foundation/database](foundation/database.md)) |
| **SQLite in development, MySQL in production** | Virtual columns, partial indexes recreated after table rebuilds, chunk sorting in PHP | App migrations | [D2](README.md#decisions): Postgres everywhere |

## GitHub

| Lesson | What happened | Source | Handled in |
|---|---|---|---|
| **An empty JSON object, not an empty list** | GitHub refused the App manifest code exchange with `[]` ("not a null or object") | app `4a47061` | [git/github-app](git/github-app.md) |
| **New permissions need new tokens** | On `new_permissions_accepted`, forget cached tokens and set up running sandboxes again | app `dbb6ca2` | [git/github-app](git/github-app.md) |
| **One installation per account** | A profile reaches installations on several accounts; one connection held only one | app `522ae4d` | [git/github-app](git/github-app.md) |
