# Dashboard: cluster and template pages

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

Pages for a project's infrastructure: clusters, adding nodes through installations, nodes and their metrics; and templates and their builds, which are built per cluster. The behaviour behind them is owned by [clusters](../clusters.md) and [templates](../templates.md).

## Today

| Legacy page (`lewbox-legacy/resources/js/pages/tenants/`) | What it does | Lines |
|---|---|---|
| `Clusters.svelte` | List: name, slug, status, health | 59 |
| `clusters/Create.svelte` | Name, slug (`external_id`), architecture, runtime, AerolVM version; cloud storage (R2/S3 bucket and keys, "Test connection"); DNS (Cloudflare domain and token, "Test connection"); the API token | 494 |
| `clusters/Show.svelte` | Health, nodes, suspend | 196 |
| `clusters/nodes/Create.svelte` | A requirements checklist to accept before getting an install command | 69 |
| `clusters/installations/Show.svelte` | The one-time install command (copy, expiry), advanced settings (node name, subdomain, public IP, firewall), a progress stepper through `pending → started → configuring_dns → installing → provisioning_storage → awaiting_seed → joining → online`, failure and expiry, cancel | 473 |
| `nodes/Show.svelte` | Details, metrics charts (CPU, load, memory, disk) and an uptime bar, polled every 30 s | 156 |
| `Templates.svelte` | List of templates | 67 |
| `templates/Create.svelte`, `Edit.svelte` | Slug, name, Dockerfile, cluster access (all or some; only clusters with storage), ports, default size, environment | 269, 241 |
| `templates/Show.svelte` | Dockerfile, ports, environment, builds | 219 |
| `templates/Builds.svelte`, `builds/Show.svelte` | Builds across clusters; one build | 157, 121 |
| `templates/clusters/Show.svelte`, `Logs.svelte` | A template on one cluster; "Build logs" (only the last error exists, as AerolVM has no build logs) | 88, 80 |

Components: `ClusterHealth`, `ClusterStatus`, `NodeStatus`, `UptimeBar`, `UsageChart`, `TemplateBuildList`, `TemplateBuildStatus`, `TemplateDefaultFields`, `TemplatePortFields`, `ImportTemplateDialog`, `RebuildTemplateDialog` (`resources/js/components/`).

Problems: installation progress and node metrics poll; the requirements checklist is a checkbox nobody reads; build "logs" are one line; there's no view of what runs on a node, or of capacity.

## Goals

- Adding a node feels like one guided task, start to finish, with live progress and clear failures.
- A cluster page answers: is it healthy, how full is it, what's on it, and what version is it on?
- A template page answers: where can I use it now, and if not, why?

## Design

### List

`/p/{project}/clusters`: one row per cluster with name and slug, isolation levels and architecture (e.g. "gVisor, microVM · amd64"), health badge (pending, healthy, degraded, error, offline, from [clusters/health-and-metrics](../clusters/health-and-metrics.md)), nodes online of total, running sandboxes, CPU and memory reserved as a bar, storage (yes or no), status (active or suspended). Health and counts update live.

Empty state: what a cluster is, what a node needs, and "Create cluster".

### Creating a cluster

`/p/{project}/clusters/new`: a page in sections, each saved only when the cluster is created.

1. **Basics:** name; slug (how consumers name it, e.g. `eu-west`; lowercase, unique in the project); architecture (`amd64`, `arm64`); isolation levels offered: **in the alpha only `container` (Docker)**, shown with a note that `gvisor` comes in the beta and `microvm` (Firecracker) later; once they come, one or more, with a line on each and what it needs from a node, and which is the default ([clusters/runtimes](../clusters/runtimes.md)); GPU vendor (none, NVIDIA, AMD). Levels can be changed later in Settings; architecture and GPU can't once a node has joined.
2. **Connectivity:** how the control plane reaches nodes. **The alpha has public only** (each node has a public IP and a DNS name), so the step isn't shown; from the beta, public or tunnel (nodes dial out, for homelabs behind NAT), from [clusters/connectivity](../clusters/connectivity.md), and the choice decides which of the next sections apply.
3. **DNS:** a domain, a provider (**Cloudflare only in the alpha**, with a note to scope the token to the zone, as it's copied to nodes; after the alpha, any the instance offers through `dns/libdns` or another DNS plugin, experimental ones labelled) and its credentials, in a form built from the provider's schema ([plugins/dns-providers](../plugins/dns-providers.md)), with "Test connection", which writes and removes a TXT record and reports each step. Optional for tunnel clusters with no public ports. Without DNS, private ports and HTTPS port URLs aren't available, and the page says so ([clusters/pki-and-dns](../clusters/pki-and-dns.md)).
4. **Storage:** a provider (one of the project's `storage/*` plugins; S3-compatible in the default build, with flavours for AWS, R2 and MinIO) and its settings, in a form built from its schema ([plugins/storage-providers](../plugins/storage-providers.md)): endpoint, region, bucket, access key, secret, with "Test connection" (write, read, list, delete a test object). Optional; without it, sandboxes can't persist and templates can't be built, and the page says so ([clusters/storage-and-registry](../clusters/storage-and-registry.md)).
5. **AerolVM version:** the fork's release to install, defaulting to the one this Lewbox release pins ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)).

"Create cluster" goes straight to [Adding a node](#adding-a-node). Tests run through the API, so the browser never talks to the bucket or DNS provider, and secrets are never sent back to the page once saved (fields show "••••• set" with Replace).

### A cluster

`/p/{project}/clusters/{clu}`:

- **Header:** name, slug, id, health, isolation levels (with how many online nodes run each) and architecture, status. Actions: Add node, Suspend or Resume (with what suspending does: no new sandboxes or starts; stops and deletes still work), Settings.
- **Health:** the cluster's state and why, e.g. "degraded: 1 of 3 nodes offline", with the quorum explained for multi-node clusters ([clusters/health-and-metrics](../clusters/health-and-metrics.md)).
- **Capacity:** vCPU, memory and disk reserved by sandboxes vs available, per node and in total; running sandboxes; the largest sandbox that fits now. This is the question behind `cluster_capacity_exceeded`.
- **Nodes:** each node's name, status, IP or tunnel, CPU and memory in use (sparkline), sandboxes, AerolVM and node agent versions, certificate expiry, last heartbeat. A node on an older AerolVM than the cluster's target shows "Update", and one below Lewbox's minimum fork release shows **Outdated: not used until upgraded** ([clusters/node-agent](../clusters/node-agent.md#upgrading-aerolvm), [clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)).
- **Installations in progress**, each linking to its page.
- **Templates built here:** each with its latest build's status on this cluster.
- **Settings** (`/settings`): name; status; replacing DNS or storage credentials; the pinned AerolVM version, with "Roll out" to update nodes one at a time; delete (refused while it has nodes).

### Adding a node

`/p/{project}/clusters/{clu}/nodes/new`:

1. **Requirements**, as a short list generated from the cluster's isolation levels and architecture: OS versions supported, CPU virtualisation (`/dev/kvm`) for `microvm`, kernel version, disk, ports open (or none, for tunnels), root access. Instead of a checkbox, each requirement is checked by the installer itself and reported on the installation page.
2. **Options** (collapsed): node name, subdomain, public IP (detected if blank), whether the installer manages the firewall.
3. **Create installation** makes a one-time install token and goes to the installation page.

### An installation

`/p/{project}/clusters/{clu}/installations/{ins}`:

- **The command**, in a `SecretReveal`:
  ```
  curl -fsSL https://lewbox.example.com/install.sh | sudo sh -s -- --token lbi_…
  ```
  with copy, expiry (and a live countdown), and the same command for `lewbox node install --token …` if the binary is already there ([clusters/installation](../clusters/installation.md)).
- **Progress:** a stepper of the installation's states, each with a time and a line of detail from the node ("Installed AerolVM v0.6.4-lewbox.3", "Wrote DNS record node-… → 203.0.113.10"). It updates live from the event stream, and shows the installer's requirement checks as they pass or fail.
- **Failure:** the step that failed, the node's error, what to try, and "Retry" (a new token) or "Cancel".
- **Done:** the new node's page.
- Re-showing the command after leaving works until it's used or expires, as in legacy.

### A node

`/p/{project}/clusters/{clu}/nodes/{node}`:

- **Header:** name, id, status, cluster, seed or not, address, versions (AerolVM, node agent, OS, kernel, runsc, Firecracker), and the isolation levels it runs, with why not the others (e.g. "no /dev/kvm").
- **Metrics:** CPU %, load (1, 5, 15), memory, disk, over 1 h, 24 h, 7 d, 14 d (retention), from the per-minute series ([clusters/health-and-metrics](../clusters/health-and-metrics.md)). Charts follow the dashboard's chart component and the `dataviz` guidance. New points stream in live; no 30 s poll.
- **Uptime:** a bar of the last 30 days in hourly buckets, from heartbeats.
- **Sandboxes on this node:** running now, with their size and profile.
- **Certificate:** expiry and last renewal.
- **Actions:** Drain (no new sandboxes; running ones stay), Update (AerolVM and node agent), Remove (after draining; tells you how to uninstall). Drain and remove come from [clusters/node-agent](../clusters/node-agent.md).

### Templates

`/p/{project}/templates`: provided templates and the project's own, each with its name and slug, kind, and a build status per cluster as a row of dots with tooltips ("eu-west: ready, 2 h ago · us-east: failed"). Filter by cluster.

### Creating and editing a template

`/p/{project}/templates/new` and `/{tpl}/edit` ([templates](../templates.md)):

- **Slug:** lowercase words joined by hyphens, at most 64; fixed after creation, and the page says so.
- **Name**, for display.
- **Dockerfile:** CodeMirror with Dockerfile highlighting. As you type, the API's checker ([templates/dockerfile-rules](../templates/dockerfile-rules.md)) runs (debounced) and marks the first problem on its line: no `FROM` first, `COPY` without `--from`, heredocs or BuildKit flags. The rules are summarised beside the editor, with the tip to try `DOCKER_BUILDKIT=0 docker build - < Dockerfile`.
- **Clusters:** all (including later ones) or some; clusters without storage are shown disabled with why.
- **Ports:** rows of port, protocol, access, with the private-proxy range (61000–61999) refused inline.
- **Defaults:** cpu, memory, disk, as sandboxes from it get them unless they ask.
- **Environment:** name and value pairs, write-only once saved.
- Saving an edit asks whether to rebuild now, when the Dockerfile changed; changing ports or defaults needs no build.
- **Import and export JSON**, as legacy's `ImportTemplateDialog.svelte` does: export gives the template as the API's create body; import fills the form from one, keeping the slug ([open question](#open-questions)).

### A template

`/p/{project}/templates/{tpl}`:

- **Availability per cluster:** for every cluster in the project, whether a sandbox can be created from it there and, if not, the `reason` the API would give (`building`, `failed`, `not_built`, `not_on_cluster`, `no_cloud_storage`), with the fix as a button (Build here, Rebuild, Edit clusters).
- **Builds:** newest first, per cluster, with status, size, duration, who started it. Live.
- **Dockerfile, ports, defaults, environment names.**
- **Sandboxes using it:** count, and a link to the sandbox list filtered by it. Delete is refused while any sandbox uses it (`template_in_use`), and the page says which.
- **Actions:** Rebuild (all clusters, or one), Edit, Delete.

### A build

`/p/{project}/templates/{tpl}/builds/{bld}`: cluster, status, timings, the image reference, size, the error if it failed, and the **build log**, streamed live while it builds. AerolVM has no build logs today; adding them is a fix for the fork ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)), and until then the page shows the last error with the local `docker build` tip.

## API

Owned by [clusters](../clusters.md) and [templates](../templates.md). The pages need: cluster CRUD with tests for DNS and storage; installations with their progress; nodes with metrics series, uptime buckets, versions, drain, update, remove; cluster capacity; template CRUD, the Dockerfile check on its own (`POST …/templates/checks`), builds, build logs (streamed); per-cluster availability with reasons.

## Testing

Playwright with the fake cluster: create a cluster (the fake DNS and storage pass their tests, then one fails), add a node and walk the installation stepper as the fake node reports each step, see the node's metrics stream in, take it offline and watch health turn degraded; create a template with a bad Dockerfile and see the line marked, fix it, watch the build finish live, and create a sandbox from it.

## Build plan

1. Clusters list and a cluster's page. (M1)
2. Create cluster with DNS and storage tests. (M1)
3. Add node, installation page with live progress. (M1)
4. Node page with metrics and uptime. (M1)
5. Templates list, create and edit with the Dockerfile checker, a template, builds. (M1)
6. Capacity, drain, update, remove; build logs once the fork has them. (M4)

## Open questions

1. ~~**DNS providers beyond Cloudflare** in the alpha?~~ Decided (2026-10-29): Cloudflare only in the alpha; the page is provider-shaped, so more come without changing it.
2. **Should the cluster page let you run the installer's checks** on a prospective node before creating an installation (e.g. `lewbox node check`)?
3. **Template JSON import and export.** Legacy `ImportTemplateDialog.svelte` and `lib/templateJson.ts` export a template as JSON and import it into the form, replacing everything but the slug; Lewagent's `templates/lewagent-computer/template.json` uses the same shape. Keep it, and make the JSON the same body the API's create takes, so `lewbox templates create --file template.json` and the dashboard agree? (Proposed: yes.)
