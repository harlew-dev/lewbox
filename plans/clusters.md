# Clusters

> Status: Draft · Milestone: Alpha (M0 fork, M1 core, M4 tunnel) · Core, with DNS and storage driver plugins · Updated: 2026-10-29

A **cluster** is a set of a project's servers, its **nodes**, that run sandboxes. Usually it's one region, with one architecture (amd64 or arm64), offering one or more **isolation levels** (`container` on Docker, `gvisor`, `microvm` on Firecracker) that each sandbox chooses from ([runtimes](clusters/runtimes.md)). Each node runs [our fork of AerolVM](clusters/aerolvm-fork.md) to run the sandboxes, and `lewbox node` (the [node agent](clusters/node-agent.md)) to keep itself as the control plane wants it. A project creates a cluster, adds nodes by running one command on each, and from then on Lewbox installs, configures, renews, upgrades and watches them.

*Project → Clusters* ([D5](README.md#decisions)): a cluster belongs to one project. Its profiles' keys reach the clusters the project allows them ([profiles](profiles.md)). A sandbox is created on one cluster and stays on it.

## Today

Legacy built most of this in the Laravel app, with the gateway calling AerolVM:

- **In the app:**
  - clusters, nodes, installations;
  - a 1,246-line bash install script (`lewbox-legacy/resources/scripts/install.sh`);
  - the node API (`app/Http/Controllers/Api/Node/v1`);
  - certificates signed by the app;
  - Cloudflare DNS;
  - a per-node registry on the tenant's R2 or S3 bucket;
  - ufw firewalls, heartbeats, cluster health;
  - registry GC.
- **In the gateway:** the calls to AerolVM, with the workarounds for its bugs (`lewbox-gateway-legacy/internal/aerolvm`, `internal/clusters`).

The design is recorded in `lewbox-legacy/docs/clusters.md`, `docs/private/node/*.md` and the gateway's `docs/aerolvm.md`.

It works. Single-node clusters run live (EU West on gVisor; an R2 cluster on Docker), and offload was checked live (a stop in 8 s, a resume in 5). Its weaknesses:

- **Installed nodes never change.** Scripts and timers written at install can only be fixed by hand, so legacy kept a list of manual fixes for nodes installed "before" each change.
- **AerolVM bugs are worked around in three places** (install script, gateway, app) instead of fixed.
- **AerolVM can't be upgraded** on an existing cluster.
- **Nodes need public IPs,** one each.
- **Unfinished:** node removal, membership sync, Firecracker, record clean-up, notifications.

## Goals

- Keep everything that works: the install flow, Lewbox as the cluster CA, a subdomain per node, the per-node registry on the project's bucket, the firewall, heartbeats, offload.
- Turn every fix into something that reaches existing nodes on its own, through the node agent's **desired state**.
- Fix AerolVM in our fork, and send the fixes upstream ([D3](README.md#decisions)).
- Support Docker, gVisor and Firecracker, as AerolVM does, and let people choose a sandbox's isolation from what its cluster offers and its profile is allowed.
- Reach nodes behind NAT through a tunnel, as well as directly.
- Make removing a node, upgrading a cluster, and seeing its health as easy as adding a node.

## How it fits together

```
 Project (dashboard / admin key)
   │ create cluster: isolation levels, arch, connectivity, DNS, storage, placement
   │ add node → installation token → `curl …/install.sh | sudo sh -s -- --token lbi_…`
   ▼
 Control plane (lewbox serve)
   ├─ node API /node/v0 ◀──── node agent: install, spec, heartbeats, certificates, ACME, tunnel
   ├─ AerolVM client ─────────▶ node's sandboxd (PAT; HTTPS direct, or through the tunnel)
   ├─ jobs: membership sync · offline detection · registry GC · DNS check · rollups · upgrades
   └─ relay (tunnel clusters): SNI pass-through and TCP to nodes
 Node
   ├─ lewbox node (systemd)     desired state · firewall · certs · cleanup · stats · upgrades
   ├─ AerolVM fork: sandboxd, toolboxd, Caddy (TLS, port URLs), Docker / runsc / Firecracker
   └─ registry 127.0.0.1:5000 ──▶ project's bucket (R2 / S3 / S3-compatible)
```

### A cluster's life

1. **Create:**
   - name and slug (`eu-main`);
   - isolation levels and their default, architecture and GPU ([runtimes](clusters/runtimes.md));
   - connectivity ([connectivity](clusters/connectivity.md));
   - optionally DNS ([pki-and-dns](clusters/pki-and-dns.md)) and storage ([storage-and-registry](clusters/storage-and-registry.md));
   - placement settings ([placement-and-capacity](clusters/placement-and-capacity.md)).

   Lewbox generates the PAT and gossip key, and pins the newest supported AerolVM release.
2. **Add the first node** ([installation](clusters/installation.md)): it becomes the seed, initialises the AerolVM cluster, and uploads the CA. Architecture and GPU are now locked; isolation levels can still be added or removed.
3. **Add more nodes:** each joins through Lewbox's CA. Firewalls on every node let it in within seconds.
4. **Run:**
   - sandboxes are placed by AerolVM;
   - the node agent sends heartbeats, renews certificates and keeps the spec;
   - the control plane syncs membership and computes health ([health-and-metrics](clusters/health-and-metrics.md)).
5. **Upgrade:** a new fork release rolls through the cluster one node at a time ([node-agent](clusters/node-agent.md#upgrading-aerolvm)).
6. **Remove nodes:** drain, move sandboxes off, leave, clean up.
7. **Retire:** `draining`, then delete once empty.

## Data model

Tables owned by this area. Columns are shown where they matter; each part plan has the rest.

| Table | What | Plan |
|---|---|---|
| `clusters` | `id` (`clu_`), `project_id`, `slug` (the consumer's name, e.g. `eu-main`, unique per project, fixed), `name`, `status` (`active`, `suspended`, `draining`), `health` (computed and stored), `isolation_levels` (text[]), `default_isolation`, `architecture`, `gpu_vendor`, `connectivity` (`direct`, `tunnel`), `aerolvm_target_version`, `placement` (jsonb: overcommit, reserves), `upgrade_window`, `firewall_enabled`, `locked_at` (first node enrolled), timestamps, `deleted_at` | here, [runtimes](clusters/runtimes.md), [placement-and-capacity](clusters/placement-and-capacity.md) |
| `cluster_secrets` | `cluster_id`, encrypted `pat`, `gossip_key`, `ca_certificate`, `ca_key`, `credential_key`, `rotated_at` per secret | [pki-and-dns](clusters/pki-and-dns.md) |
| `cluster_dns` | `cluster_id`, `provider`, `domain`, encrypted `credentials`, `acme_email`, `tested_at` | [pki-and-dns](clusters/pki-and-dns.md) |
| `cluster_storage` | `cluster_id`, `provider`, `endpoint`, `region`, `bucket`, `prefix`, encrypted `credentials`, `force_path_style`, `chunk_size`, `tested_at` | [storage-and-registry](clusters/storage-and-registry.md) |
| `node_installations` | `id` (`ins_`), `cluster_id`, `node_id`, token hash and encrypted token, `name`, `subdomain`, `role`, `status`, `failed_step`, `failure_reason`, `started_at`, `finished_at`, `expires_at`, `created_by` | [installation](clusters/installation.md) |
| `nodes` | `id` (`node_`, also AerolVM's `--node-id`), `cluster_id`, `name`, `is_seed`, `role`, `status`, `status_reason`, `domain`, `public_ip`, `lan_ip`, host facts (hostname, OS, kernel, CPU model and cores, memory, disk, KVM, GPU), `isolation_levels` (what it runs), `aerolvm_version`, `agent_version`, `spec_version_applied`, `token_hash`, `certificate_serial`, `certificate_expires_at`, `last_seen_at`, `capacity` (jsonb from membership), `drained`, timestamps, `deleted_at` | [node-agent](clusters/node-agent.md), [health-and-metrics](clusters/health-and-metrics.md) |
| `node_connections` | `node_id`, `replica_id`, `connected_at`, `remote_addr`, `tunnel` | [node-agent](clusters/node-agent.md#connection) |
| `node_events` | `node_id`, `kind` (status change, action, certificate, spec applied, upgrade), `details`, `actor`, `at` | [health-and-metrics](clusters/health-and-metrics.md) |
| `node_actions` | `id`, `node_id`, `kind`, `args`, `status`, `result`, `requested_by`, timestamps | [node-agent](clusters/node-agent.md#actions) |
| `node_metrics` (daily partitions), `node_metrics_5m`, `node_metrics_1h` | Samples and rollups | [health-and-metrics](clusters/health-and-metrics.md#metrics-storage) |
| `sandbox_stats`, `sandbox_stats_5m` | Latest and recent per-sandbox use | [health-and-metrics](clusters/health-and-metrics.md#sandbox-stats) |
| `capacity_waits` | Operations waiting for room | [placement-and-capacity](clusters/placement-and-capacity.md) |
| `sandbox_snapshots` | Snapshots pending, pushed and deleted, for safe GC | [storage-and-registry](clusters/storage-and-registry.md#garbage-collection) |

Notifications use the project's notifications table ([projects](projects.md)).

## API

The **Admin API**, project-scoped; the path shape follows [api](api.md). The dashboard calls the same endpoints.

| Area | Endpoints |
|---|---|
| Clusters | `GET/POST /v0/projects/{prj}/clusters`; `GET/PATCH/DELETE …/clusters/{clu}` (by `clu_` id or slug) |
| DNS and storage | `PUT/DELETE …/clusters/{clu}/dns`; `PUT/DELETE …/clusters/{clu}/storage`; `GET …/storage/usage`; `POST /v0/projects/{prj}/dns-tests`, `…/storage-tests` |
| Capacity and health | `GET …/clusters/{clu}/capacity`; `GET …/clusters/{clu}/metrics` |
| Upgrades | `PUT …/clusters/{clu}/aerolvm` (`target_version`, `upgrade_window`); `GET` for roll progress |
| CA | `POST/GET …/clusters/{clu}/ca/rotation` |
| Installations | `GET/POST …/clusters/{clu}/installations`; `GET/PATCH/DELETE …/installations/{ins}` |
| Nodes | `GET …/clusters/{clu}/nodes`; `GET /v0/projects/{prj}/nodes/{node}`; `GET …/nodes/{node}/metrics`, `…/uptime`, `…/events`; `POST/DELETE …/nodes/{node}/removal`; `POST …/nodes/{node}/actions` (diagnostics, renew, cleanup); `POST …/nodes/{node}/lost` |

Consumers (profile and sandbox keys) never see nodes. They see:

- their reachable clusters on `GET /v0/profile`: `id`, `slug`, `name`, `status`, `capabilities`;
- `cluster` on each sandbox;
- the errors below.

The **node API** (`/node/v0`, node and installation tokens) is in [installation](clusters/installation.md#node-api) and [node-agent](clusters/node-agent.md#node-api).

### Errors

Consumer-facing codes this area raises (registered in [api/errors](api/errors.md)):

| Status | Code | When |
|---|---|---|
| 409 | `cluster_suspended` | Create or start on a suspended or draining cluster (carries `cluster`) |
| 422 | `cluster_required`, `validation_failed` on `cluster` | See [sandboxes/lifecycle](sandboxes/lifecycle.md) |
| 422 | `persistence_unsupported` | No storage, or the sandbox's isolation level can't snapshot there (carries `cluster`, `reason`) |
| 422 | `isolation_unavailable` | The level asked for isn't offered on the cluster or allowed for the profile ([runtimes](clusters/runtimes.md#choosing-a-sandboxs-level)) |
| 422 | `template_unavailable` | `reason: no_cloud_storage` and others ([templates](templates.md)) |
| 503 | `cluster_capacity_exceeded` | No room after the capacity wait |
| 503 | `cluster_unavailable` | No quorum, offline, the sandbox's node unreachable (carries `cluster`, `reason`) |

Admin-only codes:

- `cluster_locked`, `isolation_in_use`;
- `cluster_has_nodes`, `cluster_has_snapshots`;
- `dns_connection`, `storage_connection`;
- `relay_unavailable`, `dns_required`.

## Webhooks

Project webhooks ([activity-and-webhooks/webhooks](activity-and-webhooks/webhooks.md)):

- `cluster.health_changed`, `cluster.capacity_low`, `cluster.capacity_exceeded`;
- `node.installation.updated`;
- `node.status_changed` (and `node.offline`, `node.online`), `node.degraded`;
- `node.certificate_expiring`, `node.spec_failed`, `node.upgrade_failed`, `node.disk_low`;
- `sandbox.unreachable`, `sandbox.reachable`.

## Security

- **The control plane holds each cluster's secrets** (PAT, gossip key, CA key, credential key, DNS and bucket credentials), encrypted with the keyring ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)).
- **Secrets are never reused across clusters,** and Lewbox's own credentials never reach a node.
- **Projects have root on their own nodes,** so they can read their cluster's secrets. That's acceptable: the cluster is theirs.
- **The node agent can only do a fixed set of things,** and only installs signed binaries.
- **Cluster ports (7000–7002) are never open to the internet:** the firewall opens them to the cluster's nodes only.

## Index

| Plan | What it covers | Milestone |
|---|---|---|
| [aerolvm-fork](clusters/aerolvm-fork.md) | The fork: branches, releases, upstreaming, versions in Lewbox, the **fix backlog** of every known AerolVM bug | M0, then by priority |
| [runtimes](clusters/runtimes.md) | Isolation levels (`container`, `gvisor`, `microvm`) on Docker, gVisor, Firecracker: what each can do, what a cluster offers, what a profile may use, choosing on create, GPU, architecture | M1 (`microvm` as soon as the fork has Firecracker) |
| [installation](clusters/installation.md) | `lewbox node install`: the bootstrap, statuses, steps, seed selection, retries, removing a node | M1 |
| [node-agent](clusters/node-agent.md) | `lewbox node`: connection, desired state, firewall, certificates, heartbeats, cleanup, actions, upgrades | M1–M3 |
| [connectivity](clusters/connectivity.md) | Direct and tunnel modes, the relay, nodes behind NAT | M1 direct, M4 tunnel |
| [pki-and-dns](clusters/pki-and-dns.md) | The cluster CA, node certificates and renewal, rotation, DNS providers, records, public TLS, DNS-01 through the control plane | M1, M4 |
| [storage-and-registry](clusters/storage-and-registry.md) | The project's bucket, the registry on each node, offload, flattening, GC, usage | M1 |
| [health-and-metrics](clusters/health-and-metrics.md) | Node statuses, heartbeats, membership, cluster health, sandboxes on lost nodes, metrics storage, sandbox stats, notifications | M1, M2 |
| [placement-and-capacity](clusters/placement-and-capacity.md) | Choosing nodes to call, the capacity queue, impossible sizes, overcommit, roles, cluster status, capacity view | M1 |

Related plans:

- [sandboxes/persistence](sandboxes/persistence.md): stop and resume, using the registry;
- [sandboxes/reconciler](sandboxes/reconciler.md): reporting what AerolVM did on its own;
- [inside-a-sandbox/toolbox](inside-a-sandbox/toolbox.md): the toolbox client;
- [templates/builds](templates/builds.md): building on clusters;
- [lessons](lessons.md): what legacy learned about all of this.

## Plugins

Clusters, nodes, installation, the node agent, PKI, placement and health are core. Two things a cluster needs from outside are **driver plugins** ([plugins](plugins.md)), so a self-hoster can add their own without touching core:

| Extension point | Default driver | Plan |
|---|---|---|
| DNS provider: creates node records and answers DNS-01 challenges | `dns/libdns`: one plugin with many libdns providers (Cloudflare by default; Route 53, DigitalOcean, Hetzner, Google Cloud DNS, Azure, Gandi, Porkbun, deSEC, RFC 2136) | [pki-and-dns](clusters/pki-and-dns.md#dns-providers), [plugins/dns-providers](plugins/dns-providers.md) |
| Storage provider: the bucket snapshots and templates live in, and the registry settings nodes get | `storage/s3` (AWS S3, Cloudflare R2, any S3-compatible store) | [storage-and-registry](clusters/storage-and-registry.md#the-bucket), [plugins/storage-providers](plugins/storage-providers.md) |

A cluster records which driver it uses (`cluster_dns.provider`, `cluster_storage.provider`). Turning a driver off doesn't change running clusters' sandboxes; it refuses what would need the driver (enrolling a node, a persistent create, a template build) with `409 plugin_disabled`, and keeps every setting.

## Build plan

1. **M0:**
   - the fork and its release pipeline;
   - `internal/aerolvm` (client, `Dialer`, versions and `Features`);
   - the fake AerolVM cluster and toolbox ([foundation/testing](foundation/testing.md)).
2. **M1:**
   - clusters (create, lock, status, secrets);
   - installation and the node API;
   - `lewbox node install` and `lewbox node run` (spec, firewall, certificates, heartbeats);
   - DNS through the `dns/libdns` plugin (Cloudflare supported; the other providers compiled in as experimental);
   - storage and registry, with GC, through the `storage/s3` driver plugin;
   - membership and health;
   - placement and the capacity queue.

   `container` and `gvisor` end to end, both on one node.
3. **M2:** sandbox stats; capacity view; unreachable detection; actions and diagnostics.
4. **M3:**
   - AerolVM rolling upgrades and agent self-update;
   - node removal;
   - node roles;
   - Route 53, Hetzner and RFC 2136 through `dns/libdns`'s live suite to supported;
   - flattening (with F14).

   `microvm` (Firecracker) as soon as the fork supports it, ephemeral until its snapshots land (#785).
5. **M4:**
   - the tunnel and relay;
   - DNS-01 through the control plane;
   - dashboard pages ([dashboard](dashboard.md)).
6. **Beta:**
   - `microvm` persistence, once the fork has Firecracker snapshots;
   - CA and secret rotation;
   - migrating legacy clusters ([migration](migration.md)).

## Open questions

1. **`microvm` for Lewagent:** Lewagent promises a microVM per computer. Do its computers default to `microvm` as soon as it persists ([runtimes](clusters/runtimes.md#open-questions))? Firecracker itself isn't gated: it's offered as soon as the fork supports it.
2. **Tunnel scaling:** one tunnel per relay replica, or forwarding between replicas ([connectivity](clusters/connectivity.md#open-questions))? And can lewbox.com afford relaying desktop streams?
3. **Instance-provided storage and shared clusters on lewbox.com** (README open question 1): may a project use Harlew's bucket, or Harlew's clusters, rather than its own?
4. **How far ahead of upstream to take the fork:** events (F35) and async builds (F32) are big. Propose them upstream before building ([aerolvm-fork](clusters/aerolvm-fork.md#open-questions))?
5. **AerolVM's `install.sh`:** keep using the fork's for host dependencies, or port it into the node agent ([installation](clusters/installation.md#aerolvms-own-installer))?
6. **Who in a project may manage clusters:** admins only ([projects](projects.md))? Legacy let any member.
