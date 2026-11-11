# Lewbox: MVP specification

> Repo: `harlew-dev/lewbox`
> Status: Draft v0.4 (2026-11-10). v0.1 was rewritten after the research in [REVIEW.md](REVIEW.md).
> Language: Go, as a single binary
> Licence: MIT
> Upstream: [AerolVM](https://github.com/aerol-ai/microvm), **stock, not a fork**. The minimum version is **v0.6.13**. The one exception: offload on stop is our own pull request to AerolVM, and until it ships in a release, clusters that want it run AerolVM with that PR applied (§10.3).

---

## 1. Overview

Lewbox lets a company run an **E2B-style sandbox business on its own servers.**

- **What an organization brings.** An *organization* (the business) runs one or more AerolVM clusters, for example `eu-west`, `us-north` and `asia-main`.
- **Who uses the sandboxes.** The organization's customers work in **workspaces**. A customer can have many workspaces, and each workspace has its own limits, API keys, sandboxes, snapshots, usage and credits. Each workspace can be given access to some or all of the organization's clusters.
- **What Lewbox provides.** Lewbox handles accounts, per-cluster API keys, limits, usage metering, credits, webhooks and a dashboard. The organization charges its customers per hour of compute, a fixed monthly allowance, or both.

**Lewbox is not in the sandbox request path.**
- Each cluster is reached at an endpoint the organization defines, such as `https://eu-west.api.acme.dev`.
- Customers use the **official AerolVM SDKs, the E2B SDK or the Daytona SDK, unchanged**, with a key that works on that one cluster.
- Lewbox drives AerolVM's built-in multi-tenancy (teams, scoped tokens, team quotas and audit export) through each cluster's admin API.
- If Lewbox is down, sandboxes and their APIs keep working. Only sign-up, key management, metering and billing pause.

**Customer portal.** An organization can also turn on **customer accounts**. Its customers then sign in to the portal, switch between their workspaces, and see each workspace's sandboxes on every cluster, its usage, credits, billing history and keys.

The portal is **part of the orchestrator** (`lewbox orchestrator`): the same binary, process, API and database, embedded in the same web app as the organization console. It is served on the **orchestrator's own domain** (`/o/{org}`), and there is nothing separate to deploy.

Every account has one of two kinds:
- **`admin`:** organization members, who use the console;
- **`customer`:** an organization's customers, who use the portal.

**Hosting.** We run the first Lewbox instance ourselves. Organizations bring their own clusters and define their own endpoints, so Lewbox never assumes a domain. The same binary stays self-hostable. The repo ships a Docker image and a Compose file, so `docker compose up` gives a ready-to-go instance with Postgres (§14.1).

### What changed since v0.1

| v0.1 | v0.2 | Why |
|---|---|---|
| AerolVM is single-tenant, so Lewbox proxies everything and enforces ownership | AerolVM's identity mode (v0.6.8 and later) gives teams, scoped `avm_` tokens, 404 across teams, team quotas and audit. Lewbox **configures** it and **doesn't proxy** | [REVIEW.md](REVIEW.md) §2 |
| A Lewbox node agent, mTLS tunnel, CA, join tokens, heartbeats and a scheduler | **Removed.** An AerolVM cluster already does placement, join, PKI, drain, forwarding and ingress | REVIEW §3 |
| Lewbox `sbx_` IDs, with upstream IDs rewritten and hidden | **Removed.** Upstream IDs leak in ways that can't be stopped, and with per-cluster endpoints Lewbox doesn't need IDs of its own | REVIEW §5.1 |
| One Lewbox API key for everything | **One key per cluster.** It is a team-scoped `avm_` token minted on that cluster | REVIEW §4 |
| An E2B-compatible layer built by Lewbox | **Use AerolVM's `/e2b` facade** on each cluster | REVIEW §5.3 |
| SQLite only | **Postgres only**, shipped with a ready-to-go Docker image and Compose file | A hosted, multi-organization ledger with background jobs |
| No dashboard in the MVP (SvelteKit later) | **React dashboard on `@harlew-dev/ui`**, embedded in the binary. It has an organization console and an optional **customer portal** | Organizations and their customers both need a UI |
| All runtimes | **`docker` and `firecracker` only, chosen by the admin, never by the customer** | gVisor caused too many problems in practice. The runtime is part of what an organization sells |
| No billing | **Metering and credits** are core. Running sandboxes are charged at compute rates; stopped sandboxes and snapshots at a storage rate | Organizations resell compute and storage |
| Per-cluster limits | **Global limits shared live across clusters, never exceeded by default** (`strict`, §8) | One plan, many regions |
| Project / profile (v0.2) | **Organization** (the business) and **Workspace** (a customer's unit; a customer can have many) | Clearer names; customers run several workspaces |
| No persistence design | **Stop offloads to S3/R2, and start pulls it back on any node**, as old Lewbox did (§10.3) | Stopped work survives node loss and doesn't pin a node |

---

## 2. Glossary

| Term | Meaning | ID / prefix |
|---|---|---|
| **Instance** | One Lewbox installation: the orchestrator and its database. Ours comes first. | |
| **Orchestrator** | `lewbox orchestrator`: the single Lewbox server process. It serves the API, the organization console, the customer portal and every background job. | |
| **Instance admin** | Someone who runs the instance. Set in configuration. | |
| **Account** | A person who signs in to Lewbox. Each account has one **kind**: `admin` (an organization member, using the console) or `customer` (an organization's customer, using the portal). | `usr_` |
| **Customer account** | An account of kind `customer`. It belongs to exactly **one organization**, and can be a member of **many** of that organization's workspaces. Its role on each workspace is `owner`, `member` or `viewer`. | `usr_`; membership `wmb_` |
| **Portal** | The customer-facing part of the dashboard for one organization, served by the orchestrator at `/o/{org}`. The organization can turn it on or off. | |
| **Organization** | A business using Lewbox. It owns clusters, workspaces, rate cards and webhooks. Its admin accounts are its **members**. | `org_` |
| **Member** | An admin account's role in an organization: `owner`, `admin`, `member` or `viewer`. | `mem_` |
| **Admin key** | An organization's key for the Lewbox API, used from the organization's own backend (sign-up flows, provisioning). | `key_`, token `lba_…` |
| **Cluster** | One AerolVM cluster (or one single node) registered by an organization. It has an organization-defined **endpoint** and **one runtime** (`docker` or `firecracker`), which the admin sets. | `clu_`, plus a slug such as `eu-west` or `eu-west-vm` |
| **Endpoint** | The public URL customers point their SDK at, for example `https://eu-west.api.acme.dev`. The organization defines it. | |
| **Workspace** | The unit a customer works and is billed in. It has its own cluster entitlements, limits, keys, sandboxes, snapshots, usage and credits, and its members are customer accounts. Its sandboxes and snapshots are isolated from every other workspace, including other workspaces of the same customer. | `wsp_` |
| **Team** | The AerolVM team that stands for a workspace on a cluster. It is AerolVM's isolation unit. | `w<26 base32>` (lowercase) |
| **Entitlement** | That a workspace may use a cluster. Granting it provisions the workspace's team on that cluster. | |
| **Cluster key** | A workspace's API key for **one** cluster: an AerolVM `avm_` runner token bound to the workspace's team. | `key_`, token `avm_…` |
| **Limit** | A cap on a workspace, either **global** (shared across all its clusters) or for **one cluster**. It is enforced by AerolVM team quotas. | |
| **Headroom** | What is left of a global limit: the limit minus the workspace's live usage on every cluster. Lewbox pushes it to every cluster continuously (§8). | |
| **Rate card** | An organization's prices: compute units per vCPU-hour, GiB-hour and disk-GB-hour for running sandboxes, per runtime, plus a **storage** rate per GB-hour for stopped sandboxes and snapshots. | `rc_` |
| **Run** | One stretch of a sandbox in one billable state (`running`, or `stopped` = stored in S3/R2) at one size, or of a snapshot existing. This is the unit of metering. | `run_` |
| **Bucket** | Credits a workspace holds: a subscription allowance or a one-time top-up. | `cbk_` |
| **Budget** | A cap on compute units spent per day or month. | |

IDs are a prefix followed by 14 base62 characters. Lewbox's own secrets look like `<prefix><58 hex><6 base62 checksum>` so secret scanners can spot them. Lewbox stores only their SHA-256 and their last 4 characters. The format follows the archived plans' `foundation/ids-and-keys`.

---

## 3. Goals and non-goals

### MVP goals

1. **Run on stock AerolVM**, v0.6.13 or later. Use no long-lived fork and no private APIs: only AerolVM's public `/v1` admin routes and its audit export. Changes Lewbox needs in AerolVM go upstream as pull requests (§10.3, §17).
2. **Stay out of the request path.** Customers' SDKs talk to cluster endpoints directly.
3. **Let organizations define their own clusters and endpoints.** Lewbox validates them and shows customers the right SDK settings.
4. **Workspaces and entitlements.** A workspace can use all of an organization's clusters, or the ones an admin picks.
5. **Per-cluster keys.** Keys can be created, listed and revoked, and can **optionally never expire**.
6. **Isolation.** A workspace can never see, use or start another workspace's sandboxes or snapshots on any cluster (§10).
7. **Limits** per workspace, **global** (shared across clusters) or per cluster, enforced by AerolVM team quotas (§8).
8. **Metering** of every running and stopped sandbox, and every snapshot, per workspace, cluster and runtime.
9. **Credits.** Rate cards, a per-hour pay-as-you-go model, monthly allowances (concurrency caps and/or a bucket of compute-hours), top-ups and budgets. Enforcement happens automatically when credits run out.
10. **Runtimes: `docker` and `firecracker` only.** The admin decides which runtime a workspace gets, and the customer has no control over it (§11).
11. **Outbound webhooks and a Lewbox API**, so an organization can automate sign-up, provisioning and billing.
12. **A dashboard** (React, `@harlew-dev/ui`) with two areas:
    - a **organization console** for organization members;
    - an optional **customer portal**, where a workspace's users see their sandboxes, usage, credits and billing history, and manage their keys.
13. **One Go binary** (`lewbox`), plus **Postgres only**. It ships as a Docker image with a Compose file for a ready-to-go instance.

### Non-goals for the MVP

- **Proxying or rewriting sandbox traffic.** This includes Lewbox-owned IDs, hostnames and routers.
- **One key for all clusters.** Keys are always per cluster (decided).
- **Lewbox's own scheduler, node agent, tunnel or PKI.**
- **Moving sandboxes between clusters**, and placement across clusters.
- **gVisor, WASM, V8 isolate and Kata.**
- **Charging money.** Lewbox meters and keeps credits. Organizations bill their customers through their own processor, using Lewbox's usage API and webhooks.
- **Per-workspace per-sandbox size caps.** AerolVM's create policy applies to a whole cluster (§8).
- **More than one runtime on one cluster.** Each cluster offers exactly one runtime until upstream supports a per-team runtime (§11, U4).
- **Lewbox carrying out offload on stop itself.** It has to live in AerolVM's stop and start for IDs to survive. We build it as a pull request to AerolVM (§10.3, milestone MU).
- **Everything from the archived plans outside this scope:** agents, agentd, portd, Git and GitHub, setup links, sign-ins, SSH keys, hooks, and template builds.
- **SSO, MFA and passkeys** for members and customers. These come later; `@harlew-dev/ui` already has the MFA components.
- **Payments.** The portal shows balances and history, and links to the organization's own checkout page for top-ups (§12.1). There is no payment-provider integration yet; credits are added through the API or the console.
- **Portals on an organization's own domain.** The orchestrator's domain serves every portal.
- **`harlew-dev/kit`.** Lewbox is a plain `net/http` app.
- **A terminal or exec in the portal.** That would put Lewbox in the data path.
- **SQLite or any other database.**

---

## 4. Architecture

```
     Organization members        Customers (portal,            Organization backend
     (console)              if the organization enables it)    (admin key lba_)
          │                          │                           │
          ▼                          ▼                           ▼
   ┌──────────────────────────────────────────────────────────────────────────┐
   │                  lewbox orchestrator  (+ Postgres)                       │
   │  API /v0 · console + portal (React, embedded) · webhooks out ·           │
   │  audit ingest in · /metrics                                              │
   │  workers: provisioner · quota sync · poller · metering · credits ·       │
   │           exhaustion · webhook delivery · cluster health                 │
   └────┬──────────────────────────────┬──────────────────────────────▲───────┘
        │ admin avm_ token (HTTPS)     │                              │ audit export
        │ tokens · quotas · list ·     │                              │ webhook (HMAC)
        │ stop · snapshots             │                              │
        ▼                              ▼                              │
  ┌───────────────┐            ┌───────────────┐             ┌───────┴───────┐
  │ eu-west       │            │ us-north      │             │ asia-main     │
  │ AerolVM       │            │ AerolVM       │             │ AerolVM       │
  │ cluster       │            │ cluster       │             │ cluster       │
  │ (identity on) │            │ (identity on) │             │ (identity on) │
  └──────▲────────┘            └──────▲────────┘             └──────▲────────┘
         │ https://eu-west.api.acme.dev      │                              │
         │ (AerolVM / E2B / Daytona SDK,     │                              │
         │  cluster key avm_…)               │                              │
   Workspace A's apps ─────────────────────────┘                    Workspace B's apps
```

### What runs where

| Where | What | Talks to |
|---|---|---|
| `lewbox orchestrator` (ours) | API, console, **customer portal** and every background job, in one process | Postgres; each cluster's admin API; organizations' webhook endpoints; SMTP |
| Each organization's cluster | AerolVM: sandboxes, placement, ingress, registry and snapshots | Lewbox's audit-ingest URL (the audit export) |
| Customers' code | SDKs | Cluster endpoints only |

### Trust boundaries

1. **Sandboxes are untrusted.** Isolation is AerolVM's job: the runtime, and per-team owner scoping.
2. **Clusters belong to the organization.** Lewbox holds an **admin** `avm_` token per cluster. It is fleet power, so it is encrypted at rest, never shown again and never logged (§13).
3. **Organizations are isolated from each other in Lewbox.** Every query is scoped by organization.
4. **Customers see only the workspaces they are members of.** Every portal query is scoped by organization *and* by a workspace the account is a member of. Customers never see cluster admin tokens, rate cards or other workspaces.
5. **Workspaces are isolated from each other on clusters.** Each workspace has its own AerolVM team, and its keys are bound only to that team.

---

## 5. Cluster requirements

A cluster must meet these before Lewbox will use it. Lewbox checks what it can when the cluster is registered and on every health check, and the dashboard shows the config snippet for each item.

| Requirement | Setting | Checked by Lewbox |
|---|---|---|
| AerolVM v0.6.13 or later on every node | | Yes: version from `/health` and `GET /v1/cluster/members` |
| Identity on | `SB_IDENTITY_ENABLED=true` | Yes: `GET /v1/whoami` with the admin token |
| PAT kept to the host | `SB_PAT_SCOPE=host`. Lewbox uses an admin `avm_` token, never the PAT | Indirectly |
| Not in enterprise mode, if non-expiring keys are wanted (enterprise mode caps keys at 90d) | `SB_ENTERPRISE_MODE` unset | Yes: test-mint and revoke at registration |
| Long key lifetimes allowed | `SB_TOKEN_MAX_TTL=876000h` (100 years; AerolVM's parser allows up to `36500d`) | Yes: test-mint |
| **One runtime per cluster** | A `docker` cluster has a create policy with `runtimes: [docker]` on every node. A `firecracker` cluster has `runtimes: [firecracker]` on every node, and every node runs `install.sh --with-firecracker` on bare metal with `/dev/kvm` (a container engine is still needed). gVisor, WASM and isolate are not enabled anywhere | Yes: runtimes advertised per node must match the cluster's runtime |
| Snapshot push on | `SB_SNAPSHOT_PUSH_ENABLED=true`, `SB_AUTO_IMPORT_CLUSTER_ID`, `SB_AUTO_IMPORT_CLUSTER_PAT_PATH` (the registry password file) | Spike |
| **Registry: the organization's own, backed by S3/R2**, needs auth, and **can't be reached from inside sandboxes** | A CNCF Distribution v3 registry on the organization's S3 or R2 bucket (§10.2). Set `SB_IMAGE_DISTRIBUTION_AOCR_HOST` (and `SB_MIRROR_PUSH_HOST`) to it, **never** the `aocr.aerol.ai` default. Firewall it from sandbox egress | No. It's a documented requirement plus a spike test |
| Offload on stop (recommended; required on our own clusters) | An AerolVM release with offload on stop, or our tagged build until then, with the stop mode set to offload (§10.3). Without it, stopped sandboxes stay on their node | Yes: shown on the cluster page |
| Audit export to Lewbox | `SB_AUDIT_EXPORT_BACKEND=webhook`, the URL Lewbox shows, and `SB_AUDIT_EXPORT_WEBHOOK_HMAC_KEY` (Lewbox generates it) | Yes: the first batch received |
| Public endpoint and previews | API host (for example `eu-west.api.acme.dev`) and preview wildcard (for example `*.eu-west.sbx.acme.dev`) with TLS | Yes: Lewbox fetches the endpoint's `/health` |

---

## 6. Data model (Postgres)

Migrations use `goose` and queries use `sqlc`. Every organization-owned table has a `org_id` column, and every query filters on it.

```
users              id | kind (admin|customer) | org_id (customers only; NULL for admins)
                   | email (citext) | email_verified_at | name | password_hash (argon2id)
                   | created_at | disabled_at
                   UNIQUE(email) WHERE kind = 'admin'
                   UNIQUE(org_id, email) WHERE kind = 'customer'
                   -- the same email can be an admin and a customer of several organizations, as separate accounts
sessions           id | user_id | kind (admin|customer, copied from the account) | org_id (customer)
                   | token_hash | expires_at | ip | user_agent
email_tokens       id | user_id | kind (verify|reset|login) | token_hash | expires_at | used_at
organizations           id | slug | name | status (active|suspended) | settings_json | created_at
org_portals    org_id | enabled | signup (off|invite|open) | display_name | logo_url | support_email
                   | default_clusters (text[] | 'all') | starter_bucket_units | key_self_service (bool)
                   | allow_sandbox_stop_delete (bool) | topup_url | billing_portal_url
                   | customer_workspaces (bool)      -- customers may create more workspaces themselves
                   | max_workspaces_per_customer (int)
memberships        id | org_id | user_id (kind=admin) | role (owner|admin|member|viewer)      -- console
workspace_members    id | workspace_id | user_id (kind=customer, same organization) | role (owner|member|viewer)  -- portal
                   UNIQUE(workspace_id, user_id)
invitations        id | org_id | workspace_id (NULL = organization invite) | email | role | token_hash
                   | expires_at | accepted_at
admin_keys         id | org_id | name | prefix | key_hash | last4 | created_by | last_used_at | expires_at | revoked_at

clusters           id | org_id | slug | name | region_label
                   | admin_url              -- how Lewbox reaches the admin API
                   | endpoint_url           -- what customers use (may equal admin_url)
                   | preview_domain         -- informational, e.g. eu-west.sbx.acme.dev
                   | admin_token_enc        -- envelope-encrypted admin avm_ token
                   | admin_token_expires_at
                   | audit_hmac_key_enc | audit_ingest_secret_hash
                   | runtime (docker|firecracker)   -- set by the admin; one per cluster
                   | aerolvm_version | status (pending|ready|degraded|unreachable|disabled)
                   | last_check_at | last_check_error | created_at
                   UNIQUE(org_id, slug)

workspaces           id | org_id | external_id | name | team (AerolVM team, unique per instance)
                   | status (active|suspended) | suspend_reason | metadata_json | created_at | deleted_at
                   UNIQUE(org_id, external_id) WHERE deleted_at IS NULL

entitlements       workspace_id | cluster_id | status (provisioning|active|revoking|revoked)
                   | provisioned_at | revoked_at
                   PRIMARY KEY(workspace_id, cluster_id)

cluster_keys       id | org_id | workspace_id | cluster_id | name
                   | upstream_token_id     -- AerolVM token id (for revoke/list)
                   | last4 | expires_at (NULL = never, i.e. 100y upstream) | created_by
                   | last_used_at (from audit ingest) | revoked_at

limits             id | org_id | workspace_id (NULL = organization default)
                   | scope (global|cluster) | cluster_id (scope=cluster only)
                   | kind (sandboxes|vcpus|memory_mb) | value
                   | mode (strict|shared; default strict; global scope only)
quota_state        workspace_id | cluster_id | kind | used (live, from events + quota reads)
                   | slice | pushed_quota | pushed_generation | recent_demand | updated_at
                   PRIMARY KEY(workspace_id, cluster_id, kind)
                   -- strict: pushed_quota = used_c + slice_c, Σ slice ≤ headroom (§8.2)
                   -- shared: pushed_quota = used_c + headroom (§8.3)

rate_cards         id | org_id | effective_from | currency_label
                   | vcpu_hour | memory_gib_hour | disk_gb_hour | free_disk_gb      -- running sandboxes
                   | runtime_multipliers_json (e.g. {"docker":1.0,"firecracker":1.25})
                   | storage_gb_hour     -- stopped sandboxes AND snapshots, the same rate
                   | minimum_charge_seconds

sandboxes          cluster_id | upstream_id | org_id | workspace_id | runtime | cpu | memory_mb | disk_gb
                   | status | status_reason | created_at | first_seen_at | last_seen_at | deleted_at
                   PRIMARY KEY(cluster_id, upstream_id)          -- a mirror; AerolVM is the source of truth

sandbox_runs       id | cluster_id | upstream_id | workspace_id | runtime | phase (running|stopped)
                   | cpu | memory_mb | disk_gb
                   | rate (micro-units/h) | started_at | ended_at | end_source (event|poll|node_lost)
                   | charged_until

snapshots          cluster_id | upstream_id | org_id | workspace_id | name | runtime | size_bytes
                   | push_state | created_at | last_seen_at | deleted_at   -- mirror, for storage metering

credit_buckets     id | workspace_id | kind (subscription|one_time) | priority | allowance | interval (ISO 8601)
                   | balance | expires_at | renews_at
credit_txns        id | workspace_id | bucket_id | amount (micro-units, signed) | kind | reference (UNIQUE per workspace)
                   | run_id | created_at      -- immutable ledger
budgets            id | workspace_id | period (day|month) | limit | warn_at_pct
usage_hourly       org_id | workspace_id | cluster_id | runtime | hour
                   | vcpu_s | mem_mib_s | disk_gb_s | stopped_gb_s | snapshot_gb_s | units

audit_cursors      cluster_id | node_id | last_batch_id | last_offset | updated_at
webhook_endpoints  id | org_id | url | secret_enc | events[] | status | disabled_at
webhook_deliveries id | endpoint_id | event_id | attempt | status | next_attempt_at | response_code
events_outbox      id | org_id | type | payload_json | created_at   -- written in the same tx as the change
audit_events       id | org_id | actor_type | actor_id | action | subject | details_json | ip | request_id | created_at
jobs               (River)
```

### Team names

AerolVM team names must be 1 to 63 characters of `[a-z0-9-]`. Each workspace gets a team name `w` + 26 lowercase base32 characters, random and unique across the instance. It never changes, and it is the **same on every cluster**.

Team names are never reused. A deleted workspace's team stays reserved, so a new workspace can never inherit leftover sandboxes or snapshots.

---

## 7. Key flows

All upstream calls use the cluster's admin token, and they are made from River jobs with retries.

**Every write to a cluster is idempotent:**
- Lewbox records what it intends before calling the cluster.
- It reconciles against upstream state.
- It retries until the cluster matches.

### 7.1 Register a cluster (owner or admin)

1. The member gives Lewbox:
   - a slug and name;
   - `admin_url`, `endpoint_url` and the preview domain;
   - an **admin `avm_` token** (`aerolvm token create --role admin --team '*' --expires-in 36500d`).
2. Lewbox checks the cluster:
   - `GET /v1/whoami` returns `admin` on `*`.
   - The version is v0.6.13 or later on all members.
   - It test-mints and revokes a runner token with `expires_in: 36500d`. Failure means the token TTL is capped; Lewbox warns and marks keys as expiring.
   - Every member advertises the cluster's chosen runtime (`docker` or `firecracker`).
   - A test create with the other runtime is refused by the create policy, which proves the cluster offers exactly one runtime (§11).
   - `GET {endpoint_url}/health` answers.
3. Lewbox generates the audit-export HMAC key and ingest URL, and shows the `SB_AUDIT_EXPORT_*` snippet.
4. The cluster becomes `ready` once a signed audit batch arrives, or `degraded (no audit export)` until then.
5. A health job runs every 60 seconds and updates the status. Changes are sent as the `cluster.health_changed` webhook.

### 7.2 Create a workspace and entitle clusters

1. `POST /v0/workspaces {external_id, name, clusters: ["eu-west","us-north"] | "all"}`.
   - A duplicate `external_id` returns `409` with the existing ID, so sign-up flows can retry safely.
2. For each entitled cluster, Lewbox runs `PUT /v1/teams/{team}/quota` with that cluster's quota. The quota is the cluster limit or the live share of each global limit (`used_c + headroom`), whichever is lower (§8).
   - Teams need no other creation step.
   - With no limits set, the workspace gets the organization default, which may be "unlimited". AerolVM then gets no quota row.
3. "All clusters" also covers clusters the organization adds later.
4. Entitlements are how the admin picks a workspace's runtime. A workspace entitled only to `eu-west` (`docker`) can never get a Firecracker VM, and one entitled only to `eu-west-vm` (`firecracker`) can never get a container (§11).

### 7.3 Create a cluster key

1. `POST /v0/workspaces/{id}/keys {cluster: "eu-west", name, expires_in?: "90d" | null}`.
2. Lewbox calls `POST /v1/tokens {name: "lbx.<key id>", role: "runner", team: <team>, expires_in: <ttl or "36500d">}`.
   - Lewbox **never** mints `*`, `admin`, `auditor` or `viewer` tokens for workspaces.
3. Lewbox returns the plaintext token **once**, plus ready-to-paste settings for that cluster:
   - **AerolVM SDK:** `SB_API_URL=<endpoint>`, `SB_API_TOKEN=<key>`. On a `firecracker` cluster, the create example includes `runtime: "firecracker"` (§11).
   - **E2B SDK (≤ 2.50.0):** API URL `<endpoint>/e2b`, `E2B_SANDBOX_URL=<endpoint>/e2b/runtime`, `E2B_API_KEY=<key>`. Shown for `docker` clusters only, until U4.
   - **Daytona SDK:** `<endpoint>/daytona`. Shown for `docker` clusters only, until U4.
   - **Every create example retries a `429` quota error** a few times over about 5 seconds. With `strict` limits (the default), a workspace close to its limit can briefly hit one on a cluster while headroom moves to it (§8.2).
4. Lewbox stores the upstream token ID, the last 4 characters and the expiry, **never the token**.

### 7.4 Revoke a key

`DELETE /v0/keys/{id}` calls `DELETE /v1/tokens/{upstream_id}`. Every node rejects the key within about 15 seconds.

### 7.5 Suspend or restore a workspace, or credits run out

**Suspend:**
1. On each entitled cluster, set the team's **blocking quota**: `{"max_cpu": 0.001}`.
   - AerolVM treats `0` as "no limit" on a field, and refuses an all-zero quota (`pkg/models/team_quota.go:38-46`), so "quota 0" can't be expressed.
   - With a tiny CPU cap instead, every create is refused, because any sandbox asks for more CPU than that. Every start and resize is refused too, because a team holding any sandbox is over its quota (`checkTeamQuotaForStart`). All of these get `429 team_quota_exceeded`.
2. Stop every running sandbox of that team: list via the mirror, then `POST /v1/sandboxes/{id}/stop`.
3. Keys stay valid, so customers can still read and delete their sandboxes. A full lock-out (`status_reason: locked`) also revokes the keys.

**Restore:** push the normal quotas back. Sandboxes are **not** restarted automatically.

### 7.6 Remove an entitlement or delete a workspace

1. Revoke the workspace's keys on the cluster.
2. Set the blocking quota (§7.5).
3. Stop its sandboxes.
4. After `deprovision_grace` (default 7 days, set per organization), delete the team's sandboxes and snapshots: `DELETE /v1/sandboxes/{id}` and `DELETE /v1/snapshots/{id}`.
5. Delete the quota.

A workspace with live sandboxes can only be deleted with `?force=true`.

### 7.7 Customer accounts (portal)

**Where it runs.** The portal is served by the orchestrator itself, under `/o/{org}/*`, and calls the orchestrator's `/v0/portal/*` routes. It is not a separate service.

**Turning it on.** An organization owner or admin enables the portal in the console. The settings are `org_portals` in §6.

**How customers join:**
- **Invite.** The organization invites an email to a workspace, either from the console or with `POST /v0/workspaces/{id}/invitations`. The person sets a password and becomes the workspace's `owner`. If they already have a customer account in this organization, the workspace is simply added to it.
- **Open sign-up** (`signup: open`). A new user verifies their email, and Lewbox creates their **first workspace**:
  1. it creates the workspace (`external_id` = `portal:<workspace id>`), with the new user as `owner`;
  2. it entitles the workspace to the organization's `default_clusters`;
  3. it applies the default limits;
  4. it adds an optional starter credit bucket (`starter_bucket_units`);
  5. it emits `workspace.created` with `source: portal`, so the organization's backend can attach its own records.
- **Workspace owners** can invite more people to their workspace as `member` or `viewer`.

**Many workspaces per customer.**
- A customer account can be a member of any number of the organization's workspaces, with a different role in each. The portal has a **workspace switcher**, and every portal page shows one workspace at a time.
- **Each workspace is fully separate:** its own entitlements, limits, keys, sandboxes, snapshots, usage, credits and billing.
- **Creating workspaces.** If the organization allows it (`customer_workspaces`, capped by `max_workspaces_per_customer`), a customer can create more workspaces in the portal. Each new workspace gets the same defaults as a sign-up: default clusters, default limits and starter bucket. Otherwise only the organization (console or API) creates workspaces and invites customers to them.
- **Isolation between workspaces.** Two workspaces of the same customer are still two AerolVM teams. A key from one never reaches the other's sandboxes or snapshots.

**What customers can do:**

| | owner | member | viewer |
|---|---|---|---|
| See sandboxes and snapshots (all entitled clusters, from the mirror), usage, credits, transactions and budgets | yes | yes | yes |
| Create and revoke cluster keys (if `key_self_service`) | yes | yes | no |
| Stop or delete their sandboxes and snapshots (if `allow_sandbox_stop_delete`) | yes | yes | no |
| Invite and remove workspace members | yes | no | no |

Lewbox carries out stop and delete with the cluster admin token, **only** after checking that the sandbox belongs to the caller's workspace. Every such action goes into Lewbox's audit log, and AerolVM records it with the caller as Lewbox's admin token.

**Accounts and signing in.** Every account has a **kind**:

| Kind | Signs in at | Belongs to | Can use |
|---|---|---|---|
| `admin` | `https://<orchestrator>/login` | Any number of organizations, through `memberships` | The console and `/v0/*` (never `/v0/portal/*`) |
| `customer` | `https://<orchestrator>/o/{org}/login` | Exactly one organization, and its workspaces through `workspace_members` | The portal and `/v0/portal/{org}/*` only |

- Both kinds use email and password (argon2id), with email verification and password reset.
- An account's kind never changes. If the same person is an organization member and also a customer, those are two separate accounts.
- Instance admins are `admin` accounts listed in configuration.

---

## 8. Limits

Limits are pushed to AerolVM as **team quotas**, and AerolVM enforces them atomically on each cluster. In a cluster, the Raft leader decides.

| Lewbox limit | AerolVM quota field | Notes |
|---|---|---|
| `sandboxes` | max sandboxes | AerolVM counts sandboxes **held**, running *or stopped*, not just running ones. The name reflects that. |
| `vcpus` | total CPU | Counted at what each sandbox asked for, running or stopped. |
| `memory_mb` | total memory | Same as above. |

A limit has a **scope**:
- **`global`:** one total, **shared** across every cluster the workspace is entitled to. For example, "10 sandboxes, 16 vCPU in total, anywhere".
- **`cluster`:** a cap on one cluster. For example, "at most 4 vCPU in asia-main".

Both can apply at once. Each can be an organization default or set per workspace; the per-workspace value wins. **Reserved-concurrency plans** such as "8 vCPU / 16 GB at once" are global limits.

### 8.1 Sharing a global limit live across clusters

Each AerolVM cluster counts only its own sandboxes, and there is no API to tell a cluster "this team also uses X elsewhere". What Lewbox can do is keep every cluster's quota up to date, live, from what the workspace holds everywhere.

For each workspace, kind and global limit `G`:

```
used_c    = what the team holds on cluster c right now
headroom  = max(0, G − Σ used over all entitled clusters)
quota_c   = min(used_c + slice_c, cluster_limit_c)          → PUT /v1/teams/{team}/quota on c
```

How `slice_c` is chosen depends on the limit's **mode**:

| Mode | `slice_c` | Can the total go over `G`? | What the customer may see |
|---|---|---|---|
| **`strict`** (default) | The headroom is **divided** between clusters, and Lewbox **moves it live** to where it's being used (§8.2). `Σ slice_c ≤ headroom` at every moment. | **Never.** | Occasionally, a `429` quota error on one cluster while another holds the free headroom, for the 1–3 seconds it takes to move. A retry then works. |
| `shared` (opt-in, per limit) | The **whole** headroom on every cluster at once. | Briefly, by one or two sandboxes, when several regions create at the limit within the same 1–3 seconds (§8.3). | Never a `429` while there's room anywhere. |

**How live it is.** Lewbox recomputes and pushes on every change:
- **Audit-export events** for creates, starts, resizes, stops and deletes. The export flushes every 1 second by default (`SB_AUDIT_EXPORT_FLUSH_INTERVAL`).
- **Every poll** (30 seconds), as a safety net.
- **Every limit or entitlement change.**

`used_c` comes from the quota read itself (`GET /v1/teams/{team}/quota` returns `usage`), so every update uses real numbers. The total delay is about the export flush plus a cross-region `PUT`, typically **1–3 seconds**.

Lowering a quota below what a team holds **removes nothing**; it only stops the team growing (`internal/service/team_quotas.go:76-78`). So pushing a tighter quota is always safe.

### 8.2 Strict mode (the default)

Strict mode guarantees the total never goes over `G`. Without a hook in AerolVM's create path (U11), the only way to guarantee that is for the clusters' free room to add up to no more than the headroom. So the headroom is divided, but **the division follows demand and changes within seconds.** It is not a fixed split like "5 per region".

**Who gets the headroom:**
1. **A minimum slice on every entitled cluster, while there's enough headroom.** That's one sandbox, or the cluster's smallest allowed sandbox size for `vcpus` and `memory_mb`. A first create anywhere then succeeds without waiting.
2. **The rest goes to the clusters in use right now**, in proportion to their creates and starts over the last few minutes. A workspace that works in one region gets nearly all its headroom there.
3. **When there isn't enough for a minimum slice everywhere,** the clusters with the most recent use get it first. A cluster with nothing free and nothing held gets the blocking quota (§7.5).

**Moving headroom.** When a cluster's free slice drops below its minimum, Lewbox moves free headroom to it from the other clusters, always in this order:
1. **Lower the donor first.** Set its quota to `used + what it keeps`.
2. **Read the donor's usage again**, after the lower has taken effect. A create may have landed in between.
3. **Raise the receiver** only by what is actually free.

The order is what makes it strict: at no moment do the clusters' quotas add up to more than `G`. A move takes one or two cross-region round trips, typically **1–3 seconds**. Moves for one workspace are serialized with a Postgres advisory lock.

**The cost.** A create on a cluster whose slice is used up gets AerolVM's normal `429` quota error, even when another cluster holds free headroom. Lewbox can't change that error's text (U5). It moves headroom to that cluster within seconds, so a retry works. The SDK snippets in §7.3 show a retry on `429`. This only happens when a workspace is close to its limit **and** switches regions or bursts. Removing it entirely needs U11, where AerolVM asks Lewbox before each create, start or resize.

### 8.3 Shared mode (opt-in)

An organization can set a limit to `mode: shared` when an occasional `429` would hurt more than a brief overshoot. **The whole headroom is then available on every cluster at once.** If 3 of 10 sandboxes are in use anywhere, every cluster can create up to 7 more right now.

The trade-off is a race. AerolVM checks each cluster's quota atomically, but **two clusters can each use the same headroom** if creates land on both inside the 1–3 second window, before either update arrives.

- **How big it can be.** Overshoot only happens when the workspace is near its limit and is creating in several regions within the same few seconds. It is at most `(clusters − 1) × headroom` at that moment, and in practice one or two sandboxes. For example, `G = 10`, 9 in use, and two regions each create one in the same second: 11 for a moment.
- **What happens next.** Lewbox sees the total above `G` and immediately sets every cluster's quota to its current usage. Nothing can grow anywhere until usage is back under `G`. Lewbox emits `limit.exceeded` with the amount, and the extra time is metered and charged normally.

### 8.4 Both modes

**Exact sharing with no race and no `429`** needs a hook in AerolVM's create path, which stock AerolVM doesn't have. This is upstream ask **U11**: an external admission webhook, where AerolVM asks Lewbox before a create, start or resize, with the team and size. Lewbox would then check one counter in Postgres atomically. AerolVM already has this seam internally for its managed service (`controlplane.Admitter`), but without the size and only in custom builds. When a cluster has U11, Lewbox uses it, and `strict` gives the whole headroom to every cluster with no race.

**Edge cases:**
- **A zero quota.** AerolVM can't express a zero limit (`0` means "no limit"). A cluster whose quota must block everything gets the **blocking quota** from §7.5.
- **An unreachable cluster.** Lewbox can't see its usage, so it assumes the worst: that cluster is using its whole last-pushed quota. Its slice stays where it is, and Lewbox lowers the other clusters to match. A dead region can shrink the usable limit until it comes back, but it can't cause overspend.
- **A cluster limit and a global limit together.** A cluster's quota is the lower of its cluster limit and its slice of the global limit. Headroom a cluster can't use because of its cluster limit goes to the others.

`GET /v0/workspaces/{id}/limits` shows each global limit with its mode, live usage and slice per cluster, the current headroom, and the last time each cluster was updated.

### 8.5 Not enforceable on stock AerolVM in the MVP

- **Per-workspace per-sandbox size caps.** The create policy is cluster-wide, so workspaces on one cluster share the same size rules. Upstream ask U4.
- **Snapshot storage quotas.** Lewbox meters storage, charges for it and warns. Upstream ask U7.
- **Idle and lifetime timers forced per workspace.** AerolVM lifecycle settings are per sandbox and set by the caller.

---

## 9. Metering and credits

### 9.1 Sources

Lewbox keeps a **mirror** of every sandbox and snapshot on every cluster, attributed to a workspace through the `owner_ref` `team:<team>`. It feeds that mirror from two sources:

1. **The audit-export webhook. This gives the exact times of state changes.**
   - It records each `api` record (create, start, stop, resize, delete) and each `system:<subsystem>` record (automatic stops, reaps, failover) with `sandbox_id`, `owner_ref` and `time`.
   - Batches are verified with HMAC and processed idempotently by `BatchID`, with a cursor per node.
   - Audit events **don't carry sizes**. Sizes come from the poller, or from `GET /v1/sandboxes/{id}`.
2. **The poller.** Every 30 seconds per cluster it runs `GET /v1/sandboxes` with the admin token. This is cluster-wide and paged.
   - It records status, runtime, CPU, memory and disk per sandbox.
   - It detects anything the audit export missed.
   - If the list is partial (`X-Cluster-List-Partial`), the poller doesn't close runs for the members that are missing.
   - Snapshot sizes come from `GET /v1/snapshots`. On a cluster this is **node-local today** (upstream `TODOS.md:53-66`), so the MVP queries it on each member, or waits for the fix.

**Runs** have a **phase**. A sandbox is always in exactly one open run from create to delete:
- a `running` run while it runs;
- a `stopped` run while it is stopped.

A state change closes one run and opens the next. When the audit export has an event, the event time is used; otherwise it is the poll time. Delete closes the last run.

If a node is lost, the open run closes at the last time the sandbox was seen, with `end_source: node_lost`. If the sandbox comes back, a new run opens.

**Accuracy target:** the event time when the audit export works, and within one poll interval when it doesn't. Charging is **overlap-safe**: each run's cost is set from its own record, never added on top.

### 9.2 Pricing

These definitions come from the archived `credits` plan.

- **Compute units** are stored as integer micro-units and shown to 2 decimal places.
- **Running sandboxes** are charged at the compute rate, per hour:
  `rate = (vcpu_hour × cpu + memory_gib_hour × memory_mb/1024 + disk_gb_hour × max(0, disk_gb − free_disk_gb)) × runtime_multiplier[runtime]`
- **Stopped sandboxes and snapshots** are charged at **the same storage rate**, `storage_gb_hour` per GB:
  - **A stopped sandbox:** `rate = storage_gb_hour × stored_gb`. `stored_gb` is the size of its saved disk in S3/R2 (§10.3). A stop saves the disk only, never memory, so Firecracker sandboxes are charged the same way. Until upstream offload lands, `stored_gb` is the sandbox's `disk_gb`.
  - **A snapshot:** `rate = storage_gb_hour × size_bytes / 1 GB`. The size comes from AerolVM's snapshot record, or the registry manifest if AerolVM doesn't report it (spike).
  - Snapshots are metered as a `snapshot` run from creation to deletion, the same way as sandbox runs.
- **Cost:** `cost = floor(rate × seconds / 3600)`, with `minimum_charge_seconds` applied per **running** run.
- **Rate cards are versioned** with `effective_from`. A change closes open runs and reopens them at the new rate.
- **Workspaces see units, not prices.**

### 9.3 Billing models an organization can combine

| Model | How |
|---|---|
| Per hour, pay as you go | Usage is charged against the workspace's balance, or simply reported. The organization invoices from `GET /v0/usage` and the CSV export. |
| Fixed monthly, reserved concurrency | Limits (§8) |
| Fixed monthly, bucket of compute-hours | A `subscription` bucket: `allowance` plus `interval: P1M`. Renewal forfeits what's left and adds the allowance. |
| Top-ups | `one_time` buckets. Buckets are spent in priority order, oldest first. |
| Spending caps | Budgets per day or month, with a warning at 80% |

The ledger (`credit_txns`) is immutable. `reference` is unique per workspace, so a retried payment webhook can't credit twice.

### 9.4 Running out

- Lewbox charges at checkpoints: every run start and end, every ledger change, and hourly.
- It schedules `exhausts_at = now + balance × 3600 / Σ rate` for each workspace with credit enforcement on.
- **At zero, or when a budget is reached:**
  1. The workspace is suspended for billing (§7.5).
  2. The `credits.exhausted` or `budget.reached` webhook fires.
  3. Customers see AerolVM's `429 team_quota_exceeded` on create.
- **Storage keeps accruing while suspended.** Stopped sandboxes and snapshots still cost storage, so the balance goes below zero.
- **Stored data is kept for at least 30 days** after a workspace runs out of credits. Within those 30 days, **nothing is ever deleted** for non-payment: not by Lewbox, and not from the console or the API (the API refuses with `409 retention_period`).
  - The customer can still delete their own data at any time.
  - Lewbox sends `credits.storage_debt` and emails the workspace owners on day 1, day 23 and day 29.
- **After 30 days** the organization **may** delete the data, from the console ("delete stored data") or the API. It can also turn on automatic deletion after a set number of days (`storage_debt_delete_after_days`, at least 30; off by default). Each deletion is audited and sends `workspace.data_deleted`.
- **Restored** on a top-up, renewal or budget reset. A debt is paid off first.
- **Overspend** is limited by how long it takes to stop sandboxes, a few seconds. An optional per-organization `grace_units` threshold can sit below zero.

**Known gap.** Customers get a generic quota error rather than "out of credits", because Lewbox is not in the path. Webhooks and the organization's own UI explain it. Upstream ask U5.

---

## 10. Snapshots

### 10.1 Isolation (required)

A snapshot made by one workspace must never be usable by another. On stock AerolVM, the workspace's **team** is the owner of the snapshot, and three layers enforce it:

1. **API scoping.** List, get, delete and create-by-name resolve only the caller team's snapshots. A snapshot with no owner, published by the operator, is **not** resolvable by tenant tokens either (`internal/service/owner_scope.go:111-145`).
2. **Registry namespace.** Pushed snapshots are stored under a per-owner namespace: `<registry>/cluster/<id>/snapshots/tenant-<hash>/<name>`.
3. **Create-time guard.** `authorizeTenantImageRef` refuses three things, and each reads as "not found":
   - raw image IDs;
   - references into the snapshot registry outside the caller's namespace;
   - other owners' built images.

   This is in `internal/service/image_distribution.go:58-80`, and the upstream test is `snapshot_namespace_xtenant_test.go`.

**What Lewbox must guarantee on top:**
- **One team per workspace.** It is never shared and never reused (§6).
- **Workspace keys are never** `*`-bound or `viewer` on `*`. Upstream `TODOS.md:517-540` notes that a `*` viewer can read all teams' metadata.
- **The cluster registry** is the organization's own S3/R2-backed registry (§10.2). It requires auth, is **not** the default `aocr.aerol.ai`, and is firewalled from sandbox egress (§5).
- **Deleting a workspace** deletes its snapshots after the grace period (§7.6).
- **M0 spike test.** Workspace B must fail to use workspace A's snapshot, on `docker` and on `firecracker`, by every one of these routes:
  - name;
  - snapshot ID;
  - full registry reference;
  - image ID;
  - an E2B or Daytona template name.

  It must also fail through a `docker pull` or `skopeo` from inside a sandbox.

**Shared base images** (for example "Python 3.12 + Node") are AerolVM **templates**, which the operator manages per cluster, or public registry images. They are never tenant snapshots. Check in the spike that runner tokens can create from templates.

### 10.2 Offload to S3/R2 and start on any node

**The registry.** Each cluster pushes snapshots to the **organization's own registry, stored in S3 or R2**:
- **Software:** CNCF Distribution v3, the same choice as the archived Lewbox plan.
  - S3 or R2 storage driver: `forcepathstyle`, equal multipart chunks (100 MiB on R2).
  - `REGISTRY_STORAGE_DELETE_ENABLED=true`, because AerolVM deletes snapshot copies by digest.
  - htpasswd auth.
- **Where it runs:** one registry per cluster, on a node or beside the cluster. It must be reachable by every node and by no sandbox.
- **Credentials:** AerolVM pushes and pulls with ordinary registry basic auth. The username is the cluster ID (`SB_AUTO_IMPORT_CLUSTER_ID`) and the password is the contents of `SB_AUTO_IMPORT_CLUSTER_PAT_PATH` (`internal/service/snapshot_push.go:21-55, 182`). That file holds a registry-only secret matching the htpasswd entry. **It doesn't have to be the cluster PAT, and it shouldn't be.**
- **Point AerolVM at it:** `SB_IMAGE_DISTRIBUTION_AOCR_HOST=<registry host>` (and `SB_MIRROR_PUSH_HOST`). Leave AOCR-only features **off**: auto-import, the pull-through mirror (`SB_MIRROR_HOST`) and retention tag suffixes.
- **Garbage collection:** AerolVM deletes the manifest when a snapshot is deleted. Freeing the blobs needs the registry's `garbage-collect`, which can't run while pushes are in progress. Following the archived plan, Lewbox's docs give a scheduled GC job that runs directly against the bucket in a quiet window.
- **Spike (M0):** prove push, cross-node pull, Firecracker snapshots and delete against Distribution on R2 with **no** AOCR-specific behaviour. AOCR is Aerol's own registry service (token auth, a retention reaper, an import hook), and upstream only documents AOCR.

With `SB_SNAPSHOT_PUSH_ENABLED=true`:
- A snapshot is pushed to the cluster registry in the background (`pending` → `pushing` → `active`).
- Once it is `active`, a create from it can be placed on **any node of that cluster**, including a Firecracker snapshot started on another node.
- AerolVM can also start a Firecracker snapshot as a container. That doesn't apply with one runtime per cluster: a snapshot stays on its cluster, and therefore its runtime.
- Snapshots that aren't pushed yet are created on their home node.

**Limits** (upstream):
- A snapshot saves **the disk only**. It does not save:
  - memory or running processes;
  - `env` values;
  - the Firecracker extra data disk;
  - CPU, memory and disk settings;
  - network rules and ports.
- A running sandbox's disk writes pause during the copy, about 15 seconds per GB on standard disks. `allow_pause: false` refuses instead.
- Snapshots taken before push was turned on are not pushed later.

### 10.3 Stop offloads to S3/R2, start pulls it back (required)

**Decided:** stopping a sandbox works the way old Lewbox did.

| Step | What happens |
|---|---|
| **Stop** | The sandbox stops. Its disk is saved as a snapshot and pushed to the cluster's S3/R2 registry (§10.2). Once the push is `active`, the sandbox's container or VM **and its local image are deleted from the node**. The node keeps nothing. |
| **Stopped** | The sandbox keeps its **ID**, name, size and settings, and its status is `stopped`. It is charged at the storage rate for the size of its saved disk (§9.2). If any node dies, nothing is lost. |
| **Start** | The cluster places the sandbox on **any** node with capacity. That node pulls the disk from S3/R2 and boots it, under the **same ID**. This takes a few seconds (VMs cold-boot), plus the download (§10.4). |
| **Delete** | The sandbox and its saved disk in S3/R2 are deleted. |

**What a stop keeps and loses** (the same as a snapshot):
- **Kept:** the disk only.
- **Lost:** running programs and memory. Every start is a fresh boot, including on Firecracker.
- **Lost:** `env` values passed at create. They are set again on start from the sandbox's stored spec; this must be confirmed in the upstream design.
- **Lost:** an extra data disk.

**Where it must be built: in AerolVM, not in Lewbox.** Customers call `stop` and `start` with their SDK, directly on the cluster, and the sandbox must keep its ID. Lewbox can't do this from outside:
- If Lewbox snapshotted and deleted a stopped sandbox itself, the customer's next `start(id)` would get `404`.
- AerolVM's public API has no way to recreate a sandbox under a given ID. It does have `CreateSandboxWithID`, but that's internal, used by failover.

**Stock AerolVM v0.6.13 does not do this yet.**
- A stop leaves the disk on the node, and on Firecracker the memory too.
- A stopped sandbox can only start there, and it is lost if that node dies.
- The planned `durable` class ("also send the saved state to another node") is rejected today (`docs/.../durability.mdx:307`).

**Most of the parts already exist upstream:**
- snapshot export of a stopped sandbox, on Docker and Firecracker (v0.6.11);
- background push to a registry;
- creating from a pushed snapshot on any node;
- recreating under the same ID (failover `recreate`, `CreateSandboxWithID`).

What's missing is **wiring them into stop and start**, plus a `stopped (offloaded)` state that start understands.

**Decided: we build it,** as a pull request to AerolVM (MIT), in the parallel milestone **MU** (§18).

**Plan:**
1. **Agree the design upstream first.** During M0, open a discussion with our proposal, so the PR has the maintainer's agreement on the API before we write it:
   - A cluster setting, for example `SB_STOP_MODE=offload`, or the planned `durable` class.
   - An `offload_after` delay, so a sandbox restarted within a few minutes resumes warm on its node first.
   - A `stopped` sandbox reports where its disk is (`node` or `registry`) and its saved size.
   - Start on any node under the same ID, by reusing the failover recreate path (`CreateSandboxWithID`).
   - Delete removes the saved disk from the registry.
   - Push failure keeps the sandbox stopped on its node and retries. A stop never loses data.
   - Sizes are reported in the audit record (helps U6).
   The maintainer turned discussion #785 into a release within about two weeks, so it's worth asking whether they'd rather build it themselves. Either way, it follows the agreed design.
2. **Build and test it** in milestone MU, alongside M1–M5, on both runtimes.
3. **Run it ourselves until it's released.** Our own clusters run AerolVM with the PR applied, built from a tagged branch. Once a release includes it, we go back to stock releases and delete the branch. Other organizations can turn it on only on a release that includes it.
4. **Lewbox's part:**
   - Detect at registration whether the cluster has offload on stop turned on, and show it on the cluster page (§5). Clusters without it still work.
   - Meter stopped sandboxes from their saved-disk size.
   - Show "stopped, stored in S3/R2" in the console and portal.
   - Run the registry GC (§10.2).
5. **On clusters without it,** stops stay on the node (stock behaviour). They are still charged at the storage rate, on the sandbox's `disk_gb`.

This gates the **GA release**, not the M1–M7 build. The rest of Lewbox doesn't depend on how a stop is stored.

### 10.4 Snapshot speed

This was slow in the old design and is still unsolved upstream:
- A Firecracker snapshot is **one flat layer of the whole root filesystem**. A new node downloads all of it. Container snapshots add a layer on top of the base image.
- There is **no lazy pull** (eStargz, SOCI or nydus are only "future work"), no pre-pull, and placement ignores which nodes already hold the image.
- The archived measurements for Docker: compression was gzip-bound at about 26 MB/s on one core against a 147 MB/s link. 512 MB of changes took about 35 seconds to upload and about 17 seconds to resume.
- **Fast paths that do exist:**
  - Firecracker **templates**: 59 ms median create, with memory restored by mmap and pages faulted in on first touch.
  - Docker warm pools: about 40 ms.

  Recommend templates for "fast start" and snapshots for "keep my work".

Upstream asks U2 and U3. Benchmark cross-node snapshot starts in the spike.

### 10.5 Metering

Snapshots are charged at the storage rate, the same as stopped sandboxes (§9.2), from the snapshot mirror. Lewbox shows each workspace's snapshot count, size and running storage cost in the console and the portal, and warns at an organization-set threshold. A hard storage quota is upstream ask U7.

---

## 11. Runtimes

- **Offered:** `docker` (containers, on either the Docker or the containerd engine) and `firecracker` (microVMs).
- **Not offered:** gVisor (operational problems in our own testing), WASM, V8 isolate and Kata.
- **The admin chooses the runtime. The customer has no control over it.**

**Why per cluster.** On stock AerolVM, the runtime is a field in each create request, so the caller chooses it. The only server-side control is the create policy's `runtimes` allowlist, and that applies to the whole cluster, not per team. So in the MVP, the **runtime belongs to the cluster**:

| Cluster | Create policy on every node | Nodes | What customers get |
|---|---|---|---|
| `docker` (e.g. `eu-west`) | `runtimes: [docker]` | Any Linux | Containers. A request for `firecracker` gets `403` |
| `firecracker` (e.g. `eu-west-vm`) | `runtimes: [firecracker]` | Bare metal with `/dev/kvm`, `install.sh --with-firecracker` | MicroVMs. A request for `docker` gets `403` |

**How the admin chooses.** Entitlements decide which runtime a workspace gets (§7.2). An organization that sells both in one region runs two clusters there, for example `eu-west` and `eu-west-vm`, and entitles each workspace to the one it bought.

**One gap on Firecracker clusters.** AerolVM forbids `firecracker` as a node's *default* runtime (`internal/config/config.go:2701-2712`). A create that names no runtime falls back to `docker`, which the policy refuses. So on a `firecracker` cluster:
- AerolVM SDK callers must send `runtime: "firecracker"`. That's the only value accepted, so the customer still can't choose anything else. Lewbox's SDK snippet for that cluster includes it.
- **E2B and Daytona SDK callers can't send a runtime**, so those facades work only on `docker` clusters until upstream allows a default (U4).

**Pricing.** The runtime multiplier on the rate card (§9.2).

**Later, with upstream ask U4.** A per-team pinned runtime would let one mixed cluster serve both kinds of workspace. Lewbox would then set the runtime on the team instead of using separate clusters.

---

## 12. Lewbox API (`/v0`)

The API uses JSON with cursor pagination and `Idempotency-Key` on POSTs. Errors have the shape `{error: {code, message, details}}`.

**Three ways to authenticate:**
- a **console session** cookie (`admin` accounts), with CSRF protection;
- `Authorization: Bearer lba_…`, an admin key scoped to one organization;
- a **portal session** cookie (customers), scoped to one organization and the user's workspaces, on `/v0/portal/*` only.

The console and the portal call this same API.

```
# Instance (instance admins)
GET/POST          /v0/organizations                  PATCH /v0/organizations/{id}  (suspend/restore)

# Organization
GET               /v0/organization                   (the key's organization)
GET/POST/DELETE   /v0/members, /v0/invitations
GET/POST/DELETE   /v0/admin-keys

# Clusters
GET/POST          /v0/clusters                  POST registers + validates (§7.1)
GET/PATCH/DELETE  /v0/clusters/{id}             PATCH rotates the admin token / endpoints
POST              /v0/clusters/{id}/check       run validation now
POST              /v0/ingest/clusters/{id}/audit   audit-export webhook (HMAC; not a key route)

# Workspaces
GET/POST          /v0/workspaces                  POST {external_id, name, clusters, limits?, metadata?}
GET/PATCH/DELETE  /v0/workspaces/{id}             PATCH status=suspended|active
PUT/DELETE        /v0/workspaces/{id}/clusters/{cluster}     entitle / un-entitle
GET/PUT           /v0/workspaces/{id}/limits
GET/POST          /v0/workspaces/{id}/keys        POST {cluster, name, expires_in|null} → token once
DELETE            /v0/keys/{id}
GET               /v0/workspaces/{id}/sandboxes   from the mirror (all clusters)
GET               /v0/workspaces/{id}/snapshots   from the mirror

# Billing
GET/POST          /v0/rate-cards
GET/POST          /v0/workspaces/{id}/credits/buckets
GET/POST          /v0/workspaces/{id}/credits/transactions   POST = manual grant/adjust (reference required)
GET/PUT           /v0/workspaces/{id}/budgets
GET               /v0/usage?group_by=workspace|cluster|runtime|sandbox&from&to[&format=csv]

# Webhooks and audit
GET/POST/PATCH/DELETE /v0/webhooks             POST /v0/webhooks/{id}/test, /deliveries/{id}/redeliver
GET               /v0/audit-events

# Portal settings and customer invites (organization members / admin keys)
GET/PUT           /v0/portal-settings
GET/POST/DELETE   /v0/workspaces/{id}/invitations, /v0/workspaces/{id}/members

# Console auth
POST              /v0/auth/login, /v0/auth/logout, /v0/auth/password-reset, /v0/auth/verify-email
GET               /v0/me

# Portal (customer sessions; {org} = organization slug)
GET               /v0/portal/{org}            branding, sign-up mode (public)
POST              /v0/portal/{org}/auth/signup | login | logout | password-reset | verify-email
GET               /v0/portal/{org}/me         user + workspaces (with role in each)
GET/POST          /v0/portal/{org}/workspaces   list mine / create one (if customer_workspaces)
PATCH             /v0/portal/{org}/workspaces/{id}             rename (owner)
GET               /v0/portal/{org}/workspaces/{id}/overview    sandboxes running, usage this period, balance
GET               /v0/portal/{org}/workspaces/{id}/clusters    entitled clusters + endpoints + SDK snippets
GET               /v0/portal/{org}/workspaces/{id}/sandboxes   ?cluster&status
POST              /v0/portal/{org}/workspaces/{id}/sandboxes/{cluster}/{sandbox}/stop    (if allowed)
DELETE            /v0/portal/{org}/workspaces/{id}/sandboxes/{cluster}/{sandbox}         (if allowed)
GET/DELETE        /v0/portal/{org}/workspaces/{id}/snapshots[/{cluster}/{snapshot}]
GET/POST/DELETE   /v0/portal/{org}/workspaces/{id}/keys        (if key_self_service)
GET               /v0/portal/{org}/workspaces/{id}/usage       ?group_by=day|cluster|runtime|sandbox
GET               /v0/portal/{org}/workspaces/{id}/credits     buckets, transactions, budgets
GET/POST/DELETE   /v0/portal/{org}/workspaces/{id}/members, /invitations   (owner)
```

### 12.1 Portal pages

These are built from `@harlew-dev/ui` components.

| Page | What it shows |
|---|---|
| Workspace switcher | In the sidebar on every page: the customer's workspaces with their role, plus "New workspace" if allowed |
| Overview | `Stat` tiles for the selected workspace: running sandboxes, compute units this period, balance. A usage `Sparkline` |
| Sandboxes | A `PaginatedTable` across clusters (cluster, ID, runtime, size, status, started, cost so far), with stop and delete where allowed |
| Snapshots | Name, cluster, runtime, size, push state, storage cost, delete |
| Usage | A `BarChart` by day, cluster or runtime, with CSV export |
| Billing | Balance per bucket, renewals, a transaction ledger, budgets, and **Top up** / **Manage billing** buttons linking to the organization's `topup_url` and `billing_portal_url` |
| Keys | Keys per cluster (last 4 characters, expiry), create (token shown once with the cluster's SDK snippet), revoke |
| Team | Workspace members and invitations |
| Account | Email, password (`NewPasswordFields`, `ChangeEmailDialog`) |

Portal pages show the organization's display name, logo and support email, never Lewbox branding by default.

**Outbound webhook events** use Standard Webhooks signing and an outbox written in the same transaction as the change:
- `workspace.created`, `workspace.updated`, `workspace.suspended`, `workspace.restored`, `workspace.deleted`
- `key.created`, `key.revoked`, `key.expiring`
- `sandbox.started`, `sandbox.stopped`, `sandbox.deleted` (derived from the mirror)
- `credits.low`, `credits.exhausted`, `credits.restored`, `credits.renewed`
- `budget.warning`, `budget.reached`
- `cluster.health_changed`

Deliveries retry for about 2 days, and an endpoint is disabled after 72 hours of failures. Outbound URLs are checked against SSRF.

---

## 13. Security

- **Cluster admin tokens are fleet power.**
  - They are envelope-encrypted (XChaCha20-Poly1305, with a rotatable keyring), decrypted only in the job that uses them, and never logged or returned.
  - Lewbox warns 30 days before an admin token expires. Rotation is `PATCH /v0/clusters/{id}`.
- **Workspace tokens are only ever `runner` bound to the workspace's team.** Admin and fleet roles can never be minted through the workspace API, and there is a test for this.
- **Plaintext `avm_` tokens are never stored.** Only the upstream token ID and the last 4 characters are kept.
- **The audit ingest** checks the cluster's HMAC, rejects replays (by `BatchID`), and accepts batches only for registered clusters.
- **Isolation tests** run every release against real clusters: cross-workspace requests on every sandbox-scoped route and on every snapshot path (§10.1).
- **Passwords** use argon2id. Sessions have server-side records. CSRF protection covers dashboard writes. Lewbox's own API has per-IP and per-key rate limits.
- **Portal isolation.** Every `/v0/portal/*` handler resolves `(organization, workspace)` from the session and the user's `workspace_members` row, never from the request body. A table-driven test tries every portal route with another workspace's IDs and with another organization's slug.
- **Portal stop and delete** run with the cluster admin token, so ownership is checked in Lewbox first: the sandbox's mirror row must be `(cluster, team)` of the caller's workspace. They are audited on both sides.
- **Open sign-up abuse.** Signing up requires a verified email, sign-ups are rate-limited per IP, and an optional starter bucket defaults to 0. The organization can switch to invite-only at any time.
- **The account kind is enforced on the server.** A `customer` session is accepted only on `/v0/portal/{its organization}/*`, and an `admin` session never on `/v0/portal/*`. The two kinds use separate cookies with different paths, and the kind is read from the session record, never from the request.
- **AerolVM public preview URLs are public by default.** The docs tell organizations about `visibility: "token"` ports.
- **The audit log** records every member and admin-key action, written in the same transaction as the change and kept for 365 days.

---

## 14. Configuration

Settings are read in this order of precedence: flags, then environment (`LEWBOX_*`), then the config file (`lewbox.yaml`), then defaults.

```yaml
listen: ":8080"
public_url: https://lewbox.example.com      # for ingest URLs, links in emails
database_url: postgres://lewbox@localhost/lewbox
instance_admins: [you@example.com]
secrets:
  keyring_file: /etc/lewbox/keyring.json    # envelope-encryption keys
smtp: { host: ..., from: "Lewbox <no-reply@example.com>" }
poller:
  interval: 30s
health_check_interval: 60s
metering:
  charge_interval: 1h
deprovision_grace: 168h
bootstrap:
  admin_email: you@example.com   # first instance admin; gets a set-password email on first start
migrate_on_start: true
```

### 14.1 Docker image and ready-to-go instance

**Postgres is the only database.** To make a new instance easy to start, the repo ships:

- **`Dockerfile`.** A multi-stage build: the React dashboard is built with Node, then the Go binary (with the dashboard embedded through `go:embed`). The runtime image is `distroless/static` (non-root). It is published for linux/amd64 and linux/arm64 as `ghcr.io/harlew-dev/lewbox:<version>` and `:latest`.
- **`deploy/docker-compose.yml`.** `lewbox` plus `postgres:17` with a named volume and health checks. `lewbox` waits for Postgres, runs migrations (`migrate_on_start`), creates the keyring on first start if none is mounted, and bootstraps the first admin from `LEWBOX_BOOTSTRAP_ADMIN_EMAIL`.
- **`deploy/.env.example`.** `LEWBOX_PUBLIC_URL`, `LEWBOX_BOOTSTRAP_ADMIN_EMAIL`, SMTP settings and `POSTGRES_PASSWORD`.

To get a ready-to-go instance:

```bash
cp deploy/.env.example deploy/.env   # set public URL, admin email, SMTP
docker compose -f deploy/docker-compose.yml up -d
# → open LEWBOX_PUBLIC_URL, set the admin password from the email, create an organization
```

**Production** uses the same image with an external managed Postgres (`LEWBOX_DATABASE_URL`) and a mounted keyring file. The keyring must be backed up; without it, cluster admin tokens can't be decrypted. Put TLS in front with any reverse proxy, or with Caddy in the Compose file (an optional `caddy` workspace).

---

## 15. Tech stack and repository layout

| Concern | Choice |
|---|---|
| HTTP | `net/http` with Go 1.22+ pattern routing |
| Database | Postgres 17, `pgx/v5`, `sqlc`, `pressly/goose` |
| Jobs | River (Postgres), with a leader lock for periodic jobs |
| CLI | `spf13/cobra`: `lewbox orchestrator`, `lewbox migrate`, `lewbox admin`, `lewbox version` |
| AerolVM client | A small typed client for the roughly 12 admin routes we use. It is tested against a recorded fake and a real cluster in E2E. |
| Logs, metrics, traces | `log/slog` (JSON), Prometheus `/metrics`, OpenTelemetry |
| Secrets | XChaCha20-Poly1305 envelope encryption with a keyring |
| Passwords | `golang.org/x/crypto/argon2` |
| Dashboard | **React 19 + TypeScript + Vite**, using **`@harlew-dev/ui`** (`../ui`: Tailwind v4, Base UI, Heroicons, Recharts charts) for every component and layout (`SidebarLayout` for the console and portal, `CenteredLayout` for auth pages). Routing and data use TanStack Router and Query (proposed), with API types generated from Lewbox's OpenAPI document. Built into `web/dist` and embedded with `go:embed`. One single-page app with two route areas: `/console/*` and `/o/{org}/*` (portal) |
| Email | SMTP: verification, password reset, invitations, key expiry and credit warnings |
| Release | `goreleaser` (`CGO_ENABLED=0`, linux/amd64 and linux/arm64) plus a multi-arch Docker image on GHCR |

```
cmd/lewbox/            main, cobra commands
internal/config/       config loading and validation
internal/db/           migrations, sqlc queries, transactions
internal/jobs/         River setup, periodic jobs, leader lock
internal/httpapi/      router, middleware, errors, pagination, handlers by area
internal/auth/         users, sessions, admin keys, roles
internal/secrets/      keyring and encryption
internal/aerolvm/      admin client: whoami, tokens, team quotas, sandboxes, snapshots, members, health
internal/clusters/     registration, validation, health
internal/workspaces/     workspaces, entitlements, keys, limits → quotas, suspension
internal/metering/     audit ingest, poller, mirror, runs
internal/credits/      rate cards, buckets, ledger, budgets, exhaustion
internal/webhooks/     outbox, delivery, signing
internal/portal/       portal settings, customer sign-up/sign-in, workspace members, scoped handlers
internal/mail/         SMTP and templates
web/                   React app (Vite) on @harlew-dev/ui: src/console/, src/portal/, src/shared/
api/openapi.yaml       Lewbox API contract → generated TS client for web/
deploy/                docker-compose.yml, .env.example, optional Caddyfile
Dockerfile
docs/                  operator docs: cluster setup (§5), SDK snippets
test/e2e/              two real AerolVM clusters, driven by the official SDKs
```

---

## 16. Open questions

### Decided

| Date | Decision |
|---|---|
| 2026-11-10 | **Postgres only**, shipped with a ready-to-go Docker image and Compose file (§14.1) |
| 2026-11-10 | **React on `@harlew-dev/ui`** for the dashboard |
| 2026-11-10 | **Portal on the orchestrator's domain** (`/o/{org}`). Accounts are kind **`admin`** or **`customer`** (§7.7) |
| 2026-11-10 | **No `harlew-dev/kit`.** Plain `net/http` |
| 2026-11-10 | **No payment-provider integration yet.** The portal links to the organization's checkout; credits are added through the API or the console |
| 2026-11-10 | **Stopped sandboxes and snapshots are charged at the same storage rate** (§9.2) |
| 2026-11-10 | **The admin sets the runtime; customers have no control.** One runtime per cluster in the MVP (§11) |
| 2026-11-10 | **Snapshot registry on S3/R2**: the organization's own CNCF Distribution registry (§10.2) |
| 2026-11-10 | **Organization** (the business) and **Workspace** (the customer's unit). A customer account can belong to many workspaces, each with its own limits, keys, sandboxes and credits (§2, §7.7) |
| 2026-11-10 | **Global limits are shared live across clusters**, not split up front (§8) |
| 2026-11-10 | **Limits are `strict` by default**: the total never goes over. Headroom moves live to where it's used; `shared` is opt-in per limit (§8.2, §8.3) |
| 2026-11-10 | **We build offload on stop** as a pull request to AerolVM (§10.3, milestone MU) |
| 2026-11-10 | **Automatic deletion after 30 days is off by default.** Organizations can turn it on (§9.4) |
| 2026-11-10 | **Stop offloads to S3/R2, start pulls it back on any node**, with the same ID, as old Lewbox did (§10.3) |
| 2026-11-10 | **Stopped sandboxes and snapshots are charged on disk only.** Memory is never saved |
| 2026-11-10 | **Nothing is deleted for non-payment within 30 days**; after that the organization may delete (§9.4) |
| 2026-11-10 | **No key for all clusters.** Keys are per cluster |
| 2026-11-10 | **MIT licence** |

### Still open

1. **Build U11 ourselves too?** With `strict` as the default, a workspace near its limit can briefly get a `429` on one cluster while headroom moves (§8.2). An admission webhook in AerolVM (U11) removes that and the overshoot of `shared` mode. Since we're already writing the offload PR, U11 could be our second. It isn't needed for the MVP.

Settled on 2026-11-10 and moved out of this list:
- who builds offload on stop (us, as an AerolVM PR);
- the default limit mode (`strict`);
- automatic deletion after 30 days (off by default);
- one key per cluster (no global key);
- the licence (MIT);
- storage pricing (stopped sandboxes and snapshots: disk only, at the storage rate);
- retention (30 days minimum).

---

## 17. Upstream asks (to discuss with the AerolVM maintainer)

In priority order for Lewbox.

| # | Ask | Why |
|---|---|---|
| **U1** | **Offload on stop.** A stop saves the disk to the registry and frees the node; a start pulls it on any node under the **same ID**. Ideally with an `offload_after` delay for warm restarts. This could be the planned `durable` class, or a cluster `SB_STOP_MODE=offload`. | **Required** (§10.3). Only AerolVM can keep the ID across stop and start. **We build it** (milestone MU) |
| **U11** | **External admission webhook.** Before a create, start or resize, AerolVM calls a configured URL with the team, runtime and requested CPU, memory and disk, and gets allow or deny with a reason. Fail-open or fail-closed is configurable | Exact global limits across clusters with no `429` while headroom moves and no overshoot (§8.2–§8.4), plus clear "out of credits" and per-plan size errors. A candidate for our second PR (§16) |
| U4 | **Runtime set by the server, not the caller.** At minimum, a policy `default_runtime` (or `force_runtime`) that may be `firecracker`, so Firecracker-only clusters work with the E2B and Daytona facades. Ideally a **per-team** pinned runtime and per-sandbox size caps, so one mixed cluster can serve docker and firecracker workspaces | "The admin sets the runtime" without one cluster per runtime (§11) |
| U5 | **Explicit team suspension**, for example `PUT /v1/teams/{t}/suspend` with a message that customers see, and **a way to express a zero quota** (today `0` means "no limit") | A clear "out of credits" error instead of a quota error, and no `max_cpu: 0.001` sentinel (§7.5) |
| U2 | **Faster snapshot distribution:** zstd or parallel compression, lazy pull (SOCI, eStargz or nydus), and layered Firecracker snapshots | Starts after an offloaded stop download the whole disk (§10.4) |
| U3 | **Cache-aware placement** for snapshot and image creates, and optional pre-pull | Avoid downloads altogether when possible |
| U6 | **Sizes in audit records**, or a lifecycle event stream with state, reason, time and size | Exact metering without polling |
| U7 | **Snapshot storage quota per team** | Storage limits for resellers |
| U8 | **A cluster-wide `GET /v1/snapshots`** (`TODOS.md:53-66`) | Snapshot metering on clusters |
| U9 | **E2B SDK ≥ 2.51 support** | E2B migrators are pinned to 2.50.0 today |
| U10 | **No PAT ever sent to `aocr.aerol.ai` by default** on self-hosted clusters, and documented support for a plain S3/R2-backed registry | Security (old Lewbox finding F18), and our registry choice (§10.2) |

---

## 18. Milestones

Each milestone ends with passing tests and an acceptance check.

### M0: Validation spike (2–3 days). This gates everything after it.
Run on **three** small AerolVM v0.6.13 clusters:
- a `docker` cluster with mixed roles across 3 nodes;
- a single-node `docker` cluster;
- a single-node `firecracker` cluster on bare metal.

Each pushes snapshots to its own Distribution registry on R2.

1. **Identity, isolation and revocation:**
   - Runner tokens for two teams. Cross-team requests return 404 on get, exec (including the WebSocket stream), files, ports, delete and list, through the AerolVM, E2B 2.50.0 and Daytona surfaces.
   - A key from cluster 1 is rejected on cluster 2.
   - A revoked key is rejected on every node within about 15 seconds.
   - A `36500d` token can be minted.
2. **Snapshot isolation:** every path in §10.1, on `docker` and on `firecracker`. Runner tokens can create from templates.
3. **Quotas:**
   - `429` under concurrent creates arriving at different nodes.
   - The **blocking quota** (`max_cpu: 0.001`) plus stop works as suspension. Creates and starts are refused, including after a leader change and when the quota read fails (the start check fails open).
   - Lowering a quota below usage removes nothing.
4. **Shared global limit:** a workspace with `G = 4` sandboxes across two clusters.
   - With 1 in use, either cluster alone can create 3 more. The headroom is shared, not split.
   - Measure the update delay: the time from a create on cluster A to cluster B's quota changing (target ≤ 3 seconds).
   - Hammer creates on both clusters at once. Confirm `strict` mode never goes over 4, and record how often, and for how long, a create gets `429` while headroom moves. Record the worst overshoot in `shared` mode.
   - Switch the load from cluster A to cluster B, and time how long until B has the headroom.
   - Freezing one cluster never lets the total go over.
5. **Runtime per cluster:**
   - On the `docker` cluster, `runtime: "firecracker"` is refused.
   - On the `firecracker` cluster, a create with no runtime, or with `docker`, is refused, and one with `runtime: "firecracker"` works.
   - Record how the E2B and Daytona facades behave on the Firecracker cluster.
6. **Metering:** one hour of mixed sandboxes, including stopped periods and snapshots. Compare the mirror and runs (`running`, `stopped`, `snapshot`) against ground truth, including automatic stops, a killed worker and a partial list. Confirm where snapshot sizes come from.
7. **Scale:** mint 10,000–50,000 tokens and teams. Measure memory, token-mirror sync and auth latency.
8. **Snapshots on S3/R2:**
   - Push to Distribution on R2 with the registry-only password, with no AOCR features.
   - Cross-node pull, delete by digest, and GC.
   - Cross-node start times for a 1 GB and a 5 GB snapshot, on both runtimes.
9. **Offload on stop (U1):**
   - Open the upstream discussion with our design (§10.3) during M0.
   - Prototype the flow by hand with today's API: stop, snapshot, push, delete, then create from the snapshot on another node. Time each step for 1 GB and 5 GB disks, to size what customers will feel on start.

**Acceptance:** a written results file, plus a go / no-go on each line.
- A no-go on 1 or 2 stops Lewbox as designed.
- A no-go on 3, 4, 6 or 7 sends us back to [REVIEW.md](REVIEW.md) §6, Option C or D.
- A no-go on 8 means running AOCR, or waiting for upstream.
- Item 9 doesn't block the build. It sizes milestone MU, which gates the GA release (§10.3).

### MU: Offload on stop in AerolVM (our PR). Runs alongside M1–M5
- The design agreed in the upstream discussion (§10.3).
- AerolVM changes:
  - the stop mode setting and `offload_after`;
  - stop: snapshot, push, wait for `active`, then delete the local disk and image;
  - start: place on any node, pull, and recreate under the same ID;
  - delete: remove the saved disk from the registry;
  - push failure: stay stopped on the node and retry;
  - the stored location and size on the sandbox and in the audit record;
  - metrics, docs, and tests on `docker` and `firecracker`.
- Our tagged AerolVM build with the PR applied, used by our clusters until a release includes it.

**Acceptance:**
1. Stop a sandbox on node A and wait for the offload. Node A holds nothing for it.
2. Kill node A, then start the sandbox. It runs on node B with the same ID and its disk intact, on both runtimes.
3. Start times for 1 GB and 5 GB disks are recorded.
4. A push that fails leaves the sandbox stopped on node A, and a later start works.
5. The PR is open upstream with its tests passing.

### M1: Foundation
- The binary and its commands, config, Postgres, migrations, River, IDs, the secrets keyring, slog, metrics and CI.
- **The Dockerfile, multi-arch image and `deploy/docker-compose.yml`** (§14.1).
- Users, email and password sign-in, email verification and reset, sessions, instance admins.
- Organizations, members, invitations, admin keys, the audit log.
- The web app scaffold (Vite, React and `@harlew-dev/ui`), embedded and served. Console sign-in pages.

**Acceptance:**
1. `docker compose up` on a clean machine gives a working instance.
2. The bootstrap admin sets a password, creates an organization and invites a member.
3. That member creates an admin key, all in the console.

### M2: Clusters
- Registration and validation (§7.1).
- The AerolVM admin client.
- Health checks.
- Audit-export ingest with HMAC and cursors.

**Acceptance:** both spike clusters register, reach `ready`, and receive and store audit batches. Breaking the admin token shows `degraded`.

### M3: Workspaces, keys and limits
- Workspaces, entitlements, cluster keys (expiring or never), revocation.
- Limits pushed as quotas.
- Suspend and restore. Removing an entitlement and deleting a workspace, with grace.

**Acceptance:**
1. Create a workspace entitled to `eu-west` only.
2. Its key creates a sandbox through the official TS SDK, and through the E2B SDK 2.50.0, against `eu-west`'s endpoint.
3. It gets `401` on `us-north`.
4. A second workspace can't see the first workspace's sandbox or snapshot.
5. Suspending blocks creates and stops sandboxes within 10 seconds.
6. A `strict` global limit of 2 sandboxes across `eu-west` and `us-north` never goes over 2 under parallel creates on both, and a create retried after a `429` succeeds once headroom has moved.

### M4: Metering
- The mirror, the poller and runs.
- Snapshot mirror and storage.
- `usage_hourly` and the usage API, with CSV.
- Derived `sandbox.*` events.

**Acceptance:** usage for the test hour matches ground truth to within one poll interval per state change.

### M5: Credits and webhooks
- Rate cards, buckets, ledger, renewals, budgets.
- Exhaustion scheduling and enforcement.
- Outbound webhooks.

**Acceptance:** a workspace with a 1-unit bucket is stopped and blocked within seconds of running out, gets `credits.exhausted`, and is restored by a top-up.

### M6: Organization console
- Console pages for clusters (with setup snippets and health), workspaces, entitlements, keys, limits, usage, rate cards, credits, webhooks, members and audit.
- Operator docs for §5, and SDK snippets.

**Acceptance:** a new organization registers its cluster, provisions a workspace and a key, runs a sandbox, and sees its usage, all from the console and the docs alone.

### M7: Customer portal and release
- Portal settings.
- Invite and open sign-up, with an optional starter credit bucket.
- Workspace members.
- The portal pages in §12.1, with organization branding.
- The portal isolation test suite.
- The `goreleaser` and GHCR release.

**Acceptance:**
1. With open sign-up on, a new customer signs up, verifies their email, gets a workspace entitled to the default clusters, creates an `eu-west` key, and runs a sandbox with the SDK.
2. The sandbox, its live cost and the balance show up in the portal.
3. The customer can stop the sandbox from the portal.
4. A second customer's session can't reach the first customer's workspace on any portal route.
5. The first customer creates a second workspace (with `customer_workspaces` on), switches to it, and sees separate keys, sandboxes, usage and balance. A key from workspace 1 can't see workspace 2's sandbox.

---

## 19. Testing

- **Unit:**
  - limit resolution and quota mapping;
  - rate and cost math, with golden cases;
  - bucket spend order and renewals;
  - exhaustion scheduling;
  - HMAC and replay checks;
  - webhook signing;
  - ID and token formats.
- **Integration:** a fake AerolVM admin API (`httptest`) that records tokens, quotas, stops and deletes and plays back list pages, including partial pages and audit batches. It asserts that Lewbox never mints anything but `runner` on the workspace's team.
- **E2E (required before release):** two real clusters, driven by the official AerolVM TS SDK, E2B SDK 2.50.0 and Daytona SDK, covering:
  - workspace lifecycle;
  - keys;
  - quotas;
  - suspension;
  - metering against ground truth.
- **Security:** the cross-workspace matrix across every sandbox route and every snapshot path (§10.1) on both runtimes, run against every new upstream release before Lewbox raises its minimum version. Also the portal matrix (§13): every `/v0/portal/*` route, tried with another workspace's and another organization's identifiers.
- **Web:** component and page tests with Vitest and Testing Library. Playwright smoke tests for console sign-in, cluster registration, portal sign-up and the portal overview, run against the Compose stack in CI.
- **Packaging:** CI builds the Docker image and runs `docker compose up` with a health check against `/up`.
