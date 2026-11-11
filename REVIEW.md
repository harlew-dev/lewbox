# Lewbox MVP: feasibility review

> Reviews: `MVP.md` (Draft v0.1)
> Upstream examined: `github.com/aerol-ai/microvm` at commit `1c56709` (`main`, 2026-11-10). Latest release is **v0.6.13** (2026-11-09).
> Date: 2026-11-10 (revision 2, which adds the multi-cluster reseller model)
> Method: reading the upstream source and docs. **Nothing was run.** "Verified" below means "read in code or docs", not "tested end to end".
>
> **Update (2026-11-10, after decisions).** The decisions from this review now live in [MVP.md](MVP.md) v0.2. Three corrections to the text below:
> 1. **Key expiry is not capped at 365 days.** `SB_TOKEN_MAX_TTL` has no upper limit outside enterprise mode, and the parser accepts up to `36500d` (`pkg/identitywire/token.go:189-206`, `internal/config/config.go:2026-2067`). Keys that effectively never expire work on stock AerolVM.
> 2. **Snapshots.** Since v0.6.11 they can be pushed to the cluster registry (opt-in, `SB_SNAPSHOT_PUSH_ENABLED`) and started on any node, Firecracker included. They are owner-scoped at the API, the registry namespace and the create-time image guard. A **stop does not offload the disk**, and there is no lazy pull. See MVP.md §10.
> 3. **Runtimes.** Lewbox supports `docker` and `firecracker` only, with one runtime per cluster.
> 4. **"Quota 0" doesn't exist.** AerolVM treats a `0` quota field as "no limit" and rejects an all-zero quota (`pkg/models/team_quota.go:38-46`). Suspension uses a blocking quota (`max_cpu: 0.001`) instead (MVP.md §7.5).

---

## 0. Product context

This review is written against the following picture of the product. Correct it if it's wrong.

Lewbox is a **self-hosted E2B alternative for companies that own their servers**.

- **Who runs it: an operator.** This is Lewbox's customer. An operator runs **2–3 AerolVM clusters**, for example `eu-west`, `us-north` and `asia-main`.
- **What the operator does: resells compute.** They sell to their own customers, who are the **accounts** in Lewbox. They charge **per hour of compute**, or a **fixed monthly price for X compute**.
- **What an account can reach.** An account can use every cluster, or only the 1–2 clusters an admin picks.
- **How a request reaches a cluster.** Two options came up: **a URL per cluster** (`eu-west.api.<domain>`), or a `region` / `cluster` parameter on requests.
- **Keys.** Leaning towards **separate API keys per cluster**: each key is bound to one cluster.

---

## 1. Summary

1. **MVP.md's premise is out of date.** It says "AerolVM is single-tenant: one host, one PAT, full access". That stopped being true in **v0.6.8 (2026-10-19)**. AerolVM now ships the following itself:
   - **named, revocable API tokens** (`avm_…`), each with a **role** and a **team**;
   - **isolation by team**: another team's sandbox answers 404 on every route;
   - **per-team quotas** on sandbox count, CPU and memory;
   - **OIDC single sign-on**;
   - **a hash-chained audit log** with webhook or S3 export.

   **Inside one cluster**, AerolVM also does scheduling, node join, PKI, drain, routing and failover. About **70% of the MVP duplicates upstream.**

2. **AerolVM has no multi-cluster tenants.** That part of the brief is right. Each cluster is a separate world with its own token store, team quotas and audit log. There is no federation, and none is on the roadmap. **Multi-cluster accounts, metering and billing are the gap Lewbox exists to fill.** So are self-service accounts and keys, and a dashboard for customers (AerolVM's `/ui` is admin-only).

3. **Use AerolVM clusters. Do not do our own load balancing.** Inside a cluster, AerolVM placement is better than anything the MVP planned. Lewbox should never schedule onto nodes. The most Lewbox decides is **which cluster**, and with per-cluster URLs **the customer decides that**, so Lewbox does no scheduling at all (§3).

4. **Per-cluster URLs with per-cluster API keys is the better design. Adopt it.** It maps directly onto AerolVM's model, where tokens are per cluster. It also means:
   - no router;
   - no sandbox-ID-to-cluster mapping;
   - **Lewbox stays out of the request path entirely.**

   The trade-offs are small; §4 has the full comparison against one global key with a `region` parameter.

5. **Recommended architecture: Lewbox as the account and billing plane over stock AerolVM clusters** (Option B, §6).
   - For each account on each allowed cluster, Lewbox creates an AerolVM **team**.
   - It mints team-scoped `avm_` **runner tokens** on that cluster. These are the customer's per-cluster keys.
   - It sets **team quotas** on each cluster.
   - It **meters usage** by polling each cluster and reading its audit export.
   - SDKs (AerolVM, E2B, Daytona) talk **directly** to `eu-west.api.<domain>`.

   If stock AerolVM's metering or suspension proves too coarse, the upgrade path is a **custom `sandboxd` build** that plugs Lewbox into AerolVM's own control-plane seam (Option C). That seam is how Aerol's managed multi-tenant service works.

6. **Some MVP goals can't be met by any design:**
   - Hiding upstream sandbox IDs. They leak through the guest environment, signed sandbox tokens, SSH and E2B headers (§5.1).
   - The E2B host-pattern data plane (`<port>-<id>.<domain>`), which AerolVM doesn't serve (§5.3).

   Per-cluster URLs make the first one not matter: Lewbox no longer needs IDs of its own.

7. **"100% possible" isn't proven yet.** Option B looks feasible on stock AerolVM v0.6.8 or later, from reading the code. A **2–3 day spike** (§10) should confirm the following before we commit:
   - isolation;
   - quotas;
   - revocation;
   - metering accuracy;
   - how many tokens and teams a cluster can hold.

---

## 2. MVP features vs. what AerolVM already ships

| MVP item | In AerolVM today? | Notes and evidence (upstream paths) |
|---|---|---|
| Users + API keys | **Partly.** Only an admin can mint `avm_` tokens. There are no self-service users and no passwords. | `POST/GET/DELETE /v1/tokens` (`pkg/api/v1/routes.go:97-99`, `internal/service/api_tokens.go`). Tokens are SHA-256 hashed and revocable in about 15s across a cluster. **Expiry is mandatory**: `SB_TOKEN_MAX_TTL` is 365d by default and 90d in enterprise mode. Requires `SB_IDENTITY_ENABLED=true`. |
| Per-user sandbox ownership | **Yes.** The unit is the *team*. | `internal/service/owner_scope.go:13-75`: another owner's sandbox gets 404 "so a user token cannot probe IDs". Lists are filtered (`service.go:3637`). Every route must declare a permission, and a test enforces it (`pkg/api/route_registry_test.go`). It is a stated security boundary (`THREAT-MODEL.md:10`). |
| Teams / orgs (an MVP *non-goal*) | **Yes.** | A team is any `[a-z0-9-]{1,63}` name and needs no prior declaration. A token is bound to one team or to `*`. |
| OIDC / SSO (an MVP *non-goal*) | **Yes.** | Okta, Entra, Google, Keycloak, GitHub Actions and Kubernetes service-account tokens. |
| Multi-server scheduling | **Yes, within a cluster.** | Power-of-two-choices placement on CPU, memory, disk and network headroom, with Raft reservations (`internal/cluster/placement.go`). GPU, runtime and template are filters. A create can pass `placement: {zone, labels}`. |
| Join tokens, internal CA, mTLS | **Yes.** | kubeadm-style tokens, CA-hash pinning, and 90-day node certificates that renew themselves (`internal/cluster/pki.go`). |
| Heartbeats, status, drain | **Yes.** | Capacity is pulled every 5s and treated as stale after 15s. Gossip detects node death. Drain and cordon: `POST /v1/cluster/nodes/{id}/drain\|cordon\|uncordon` (`routes.go:272-279`). |
| Unique IDs and request routing | **Yes, within a cluster.** | IDs are `sb-` + 16 hex. The Raft FSM enforces uniqueness. Any node forwards to the owner node. |
| AerolVM-compatible API | It **is** the API. | About 151 routes under `/v1`. |
| E2B-compatible API | **Yes, with limits** (§5.3). | The `/e2b` and `/e2b/runtime/*` facades. Daytona and Runloop facades exist too. |
| Per-user max sandboxes | **Yes, per team, per cluster.** | `PUT /v1/teams/{team}/quota` caps count, total CPU and total memory, and returns 429 when exceeded (`internal/service/team_quotas.go`). |
| Create sanitisation | **Partly.** | `SB_CREATE_POLICY_FILE` covers registries, images, runtimes, CPU, memory, disk, GPU and non-root. It is **per server, not per tenant**. Host mounts are refused for non-admins. |
| Per-key rate limiting | **No** | Only list fair-share, audit reads and `/mcp` are limited. |
| Audit log | **Yes, and better than planned.** | Hash-chained records naming the `caller` and `owner_ref`. Export to webhook (HMAC-signed) or S3. |
| Metrics | **Yes.** | `GET /v1/metrics` in Prometheus format. |
| Usage metering for billing | **Only in a custom build.** | `controlplane.Reporter` samples (vCPU-s, MiB-s, GiB-s of disk, GPU-s, uptime, bytes in and out) are emitted **only** when a control-plane provider is wired in (§6, Option C). |
| Account suspension | **Only in a custom build.** | `FleetController.StopByOwner / RestoreByOwner / DeleteByOwner` and the `Admitter` create gate (`pkg/controlplane/controlplane.go`, `internal/service/fleet_control.go`). |
| Multi-cluster tenants | **No** | Not on `ROADMAP.md`. |
| Customer dashboard | **No** | `/ui` is admin and operator only. |

---

## 3. Cluster question: use AerolVM clusters or our own load balancing?

**Use AerolVM clusters, and treat each cluster as an opaque region.**

- **One endpoint per cluster.** Put DNS round-robin or a load balancer in front of the ingress or mixed nodes. Any node forwards to the node that owns a sandbox, and the client never needs to know where it was placed (`docs/.../engineering-placement-failover.mdx:529`).
- **Placement already accounts for more than the MVP's "most free capacity".** It also handles:
  - in-flight reservations;
  - disk and network-slot pools;
  - GPU, runtime and template filters;
  - zone and label constraints.
- **We can't steer it from outside anyway.** Create has no public node-ID field. A Lewbox node scheduler would only fight AerolVM's own.
- **Failover already exists.**
  - Sandboxes with `failover.policy="recreate"` are rebuilt elsewhere with the same ID and URL. Disk and processes are lost.
  - Other sandboxes on a dead node return 410 until the node returns.
- **Ingress is built in.** Caddy and caddy-l4, wildcard DNS, HTTP previews, and TCP and TLS ports.

The MVP's own node agent, mTLS tunnel, CA, join tokens, heartbeats, scheduler and `sbx_` ID mapping all go away. Choosing a region is the customer's job, through the URL.

**Cluster limits to plan around:**

| Limit | Value | Source |
|---|---|---|
| Mixed or combined-role cluster | at most **10 live nodes**; above that you need dedicated server, worker and ingress tiers | `internal/cluster/topology.go:13` |
| Server tier | at most **7** nodes (5 voting) | `topology.go` |
| Workers | "up to about 1,000", per the docs; no benchmark evidence checked | `docs/.../cluster/how-it-works.mdx` |
| Ingress nodes without a shard-aware router | at most **10** | `docs/.../cluster-ingress.mdx` |

Nodes need a private full mesh on ports 7000–7003. There is no outbound-only or NAT mode.

**Maturity:** cluster mode is still labelled "Phase-1". `plans/cluster-phase-2.md` is dated 2026-11-08, and there are skipped known-bug tests in `internal/cluster/fault_known_bugs_test.go`. Pin versions and test upgrades.

---

## 4. Per-cluster URLs and keys vs. one global key with a `region` parameter

**Recommendation: per-cluster URLs and per-cluster keys.**

| | **Per-cluster URL + per-cluster key** | One global key + `region` param or subdomain |
|---|---|---|
| Fit with AerolVM | **Native.** AerolVM tokens, teams, quotas and audit are already per cluster. | Fights it. One key must be valid on N independent token stores. |
| What sits in the request path | **Nothing.** DNS for `eu-west.api.<domain>` points at that cluster's ingress. | A Lewbox router or proxy, or a custom `sandboxd` that checks keys with Lewbox (Option C). |
| Sandbox ID → cluster mapping | **Not needed.** The key and URL decide the cluster. | Needed for every get, exec or delete after create: a lookup table, or IDs that encode the region. |
| A `region` field in request bodies | Not needed. | No AerolVM, E2B or Daytona SDK has one. A proxy would have to strip it. `placement.zone` is the closest field, and it works **within** a cluster only. |
| Stock SDKs (AerolVM, E2B ≤ 2.50, Daytona) | **Work unchanged.** Set the base URL and the key. | Work only through a proxy, or by passing the region in the URL anyway. |
| Leaked key | Hits one region. | Hits every region. |
| Data residency ("EU only") | **Built in.** An EU key cannot create in the US. | Has to be enforced by Lewbox. |
| Lewbox outage | **Sandboxes and the API keep working.** Only sign-up, key management and billing pause. | The API is down (proxy), or degraded (Option C: cached keys only). |
| Listing across regions | The customer calls each region, or uses the Lewbox dashboard or API. | Lewbox fans out. |
| What the customer has to manage | One key per region they use, which is slightly more. | One key. |
| Metering and enforcement | Same in both: Lewbox aggregates across clusters (§7). | Same. |

**Conclusion.** Per-cluster keys are simpler, more robust and safer. They cost the customer an extra key or two. If customers later ask for "one key everywhere", it can be added on top without changing clusters, in one of two ways:
- a Lewbox router that holds the per-cluster keys behind one Lewbox key (Option D);
- Option C.

**Details to decide for per-cluster keys:**

- **Hostnames.** In a self-hosted install the domain is the **operator's**, e.g. `eu-west.api.<operator-domain>`, not `lewbox.com`. Each cluster runs its own Caddy and TLS for its API host and its sandbox wildcard. Plan the DNS:
  - `eu-west.api.<domain>` for the API;
  - `*.eu-west.sbx.<domain>` for previews and ports.

  Check in the spike that AerolVM's API host and preview domain can be set separately.
- **E2B SDK.** It builds `api.<E2B_DOMAIN>` and `<port>-<id>.<E2B_DOMAIN>` by default. Against AerolVM, set the API URL to `https://eu-west.api.<domain>/e2b` and `E2B_SANDBOX_URL=https://eu-west.api.<domain>/e2b/runtime`, following `docs/.../using-e2b-sdk.mdx`. Write this up as a per-region snippet in the dashboard.
- **Key prefix.** Keys will be `avm_…`, not `lbx_…`, unless we use Option C or D. The dashboard can show region and name next to each key.
- **Key expiry.** It is mandatory on `avm_` tokens: at most 365d, or 90d in enterprise mode. Lewbox should show the expiry date, email before it, and offer one-click rotation. If keys must never expire, that pushes towards Option C or D.

---

## 5. Problems with the MVP as written

### 5.1 "Upstream IDs must not leak" is impossible, and with per-cluster URLs no longer needed

| Leak | Why a proxy can't fix it |
|---|---|
| `SB_SANDBOX_ID` in every guest's environment (`pkg/docker/client.go:497`) | It shows up in exec output, PTY streams, files and anything user code prints. |
| Sandbox tokens (`avmst_<b64 claims>.<Ed25519 sig>`) | The claims include `sbx` (the real ID) and `own`. They are readable by anyone and signed, so they can't be rewritten. The server also checks `sbx` against the path. |
| The SSH gateway | It runs over raw TCP, and the username is the sandbox ID. |
| `/e2b/runtime/*` | The ID travels in the `E2b-Sandbox-Id` header. |
| WebSocket and Connect frames, SSE, error strings | These are not JSON bodies, so they can't be rewritten. |

**Decision:** keep AerolVM's IDs and drop Lewbox's `sbx_` IDs.

### 5.2 Proxy details, which matter only for Option D

- **WebSocket auth.** The token also travels as `Sec-WebSocket-Protocol: sandbox.bearer, <token>` (`pkg/api/apihttp/bearer.go:30-47`).
- **Response fields that point back at AerolVM:** `public_url`, `host`, `host_port`, `ssh_gateway`, `custom_domains` and `container_ip`. Daytona builds `toolboxProxyUrl` from `X-Forwarded-Host`. E2B's `domain` field is copied from the request's `Host`.
- **Token-gated ports** use `?token=` → 302 → a `__Host-` cookie scoped to the port's hostname.
- **TCP and TLS-SNI ports and SSH** need an L4 proxy or a direct path.
- **Route allowlist.** Never allowlist `/v1/cluster/internal/*` (46 routes) or `/v1/cluster/*` (35 operator routes).

### 5.3 E2B limits (upstream, apply whatever we build)

- **The E2B SDK must be ≤ 2.50.0.** 2.51.0 and later fail create with 405 (`docs/.../using-e2b-sdk.mdx:32-34`).
- **The data plane is path-based** (`/e2b/runtime`). AerolVM does **not** serve `<port>-<sandboxId>.<domain>`, so MVP M4 as written won't work.
- **Not supported:**
  - template builds;
  - `network.rules` and `egressProxy` (both return 501);
  - `getHost` and `getMcpUrl`;
  - metrics and logs;
  - keeping memory across a pause (pause means stop).
- **Unverified:** Connect is JSON only. The protobuf codec and signed file URLs are unconfirmed.

E2B's template workflow is central to an "E2B-style business", and it is missing. Find out what AerolVM offers instead: `POST /v1/images/build`, snapshots, and admin-only templates. That is a product gap to plan for (§9).

### 5.4 Other corrections to MVP.md

- **§3 "AerolVM binds to 127.0.0.1".** That holds only for a single node. A cluster needs 7000–7003 on the private network and 443 on ingress. The modern way to keep the PAT off the network is `SB_PAT_SCOPE=host`.
- **M5 "installs AerolVM in `--local` mode".** `--local` is the laptop and Docker dev mode, and its public URLs return "a URL nothing answers". Use the normal install script, Ansible or Terraform.
- **§6.1 auth headers.** Bearer on every surface, `X-API-KEY` on E2B, plus the WebSocket subprotocol. The TS SDK reads `SB_API_URL`, `SB_PAT_TOKEN` or `SB_API_TOKEN`, and `SB_TEAM`.
- **§13 open questions:**
  - **Q4** (E2B surface): yes, `/e2b`; Daytona and Runloop too.
  - **Q5** (use cluster mode): yes, §3.
  - **Q6** (are previews public?): previews are public by default; `visibility:"token"` ports use sandbox tokens.

---

## 6. Options

### Option A: the MVP as written (proxy plus our own node agent and scheduler)
- Duplicates about 70% of upstream and fights cluster placement.
- Its ID-hiding and E2B host-pattern goals can't be met.
- **Not recommended.**

### Option B: account and billing plane over stock AerolVM clusters (recommended)

```
                           ┌───────────────── Lewbox (central) ─────────────────┐
 Operator admins ─────────►│ accounts · plans · cluster entitlements · keys UI  │
 Customers (dashboard) ───►│ usage ledger · invoices / credit · suspension      │
                           └──┬───────────────┬───────────────┬─────────────────┘
          admin avm_ token    │ mint/revoke   │ quotas        │ poll list + audit webhook
                              ▼               ▼               ▼
                    eu-west cluster     us-north cluster   asia-main cluster
                    (identity on)       (identity on)      (identity on)
                         ▲                   ▲                  ▲
 Customer SDKs ──────────┘ eu-west key       └ us-north key     └ (not entitled)
   https://eu-west.api.<domain>   (direct; Lewbox not in path)
```

**Upstream setup on each cluster:**
- AerolVM v0.6.8 or later, with `SB_IDENTITY_ENABLED=true` and `SB_PAT_SCOPE=host`.
- Audit export sent to Lewbox's webhook.
- An `admin` `avm_` token for Lewbox, which expires, so it needs rotation.

**What Lewbox does on each cluster:**
- **Account entitled to a cluster:** the account maps to team `t-<account-id>` there. It can have teams on several clusters.
- **Customer creates a key:** Lewbox picks the region, calls `POST /v1/tokens {role: runner, team: t-<id>, expires_in}` on that cluster, and shows the token once. It stores the token ID, region, expiry and a hash.
- **Plan concurrency caps:** `PUT /v1/teams/t-<id>/quota` (sandbox count, total vCPU, total memory) on each entitled cluster.
- **Suspend or out of credit:**
  1. Revoke the tokens, or keep them.
  2. Set the quota to 0, so creates get 429.
  3. Stop running sandboxes through the admin API.
- **Restore:** reverse those steps.
- **Remove a cluster entitlement:** revoke the keys on that cluster, set the quota to 0, and stop or delete the account's sandboxes there.

**Effort:** small to medium.
- CRUD, a dashboard, an AerolVM admin client, a usage poller and a webhook consumer.
- No streaming, WebSocket or L4 work.

**Weaknesses:**
- Keys are `avm_…` and must expire.
- No per-key rate limits.
- Create policy is per cluster, not per plan.
- Metering is reconstructed (polling plus audit, §7), not exact.

### Option C: a custom `sandboxd` build with a Lewbox control-plane provider

**What the seam is.** `pkg/daemon.Run(ctx, logger, ProviderFactory)` is the published seam that Aerol's managed multi-tenant service uses (`pkg/controlplane/controlplane.go`, `pkg/daemon/daemon.go:62-115`). A Lewbox provider would supply:
- **`Validator`:** turns a key into `Identity{OwnerRef: "team:<account>", Role: runner}`. Keys can then be `lbx_…`, never expire, and be scoped per cluster or global. Lewbox decides, and AerolVM calls it on every request, cached by the provider.
- **`Reporter`:** **exact usage samples**: uptime-s, vCPU-s, MiB-s, disk GiB-s and GPU-s per sandbox per window, plus bytes in and out, each with an `EventID`. This is billing-grade data (`internal/service/usage.go:150-161, 270-280`).
- **`Admitter`:** refuses creates for suspended or out-of-credit accounts (403), or returns 503 when standing is unknown. It sees only the owner, **not the requested size**.
- **`FleetController` and `Enforcement`:** `StopByOwner`, `RestoreByOwner`, `DeleteByOwner` and a webhook. Each node acts on its own sandboxes, so every node's enforcement loop must run.

Team quotas still apply, because they key off the `team:` prefix of `OwnerRef` (`pkg/models/team_quota.go:81`). **Verify** that the quota admin routes work with identity off.

**Costs and risks:**
- Operators must run **Lewbox's `sandboxd` build** on every node. Today's stock clusters would need a rolling binary swap.
- Install and upgrade tooling defaults to GitHub releases (`internal/nodeupgrade/paths.go:37`). `aerolvm upgrade` and `release pull` accept `--release-url`, so we'd host our own release feed. Integrity checks are unverified.
- **Mutually exclusive with identity mode.** The boot guard refuses `SB_IDENTITY_ENABLED=true` together with a token-validating provider (`pkg/daemon/identity_guard.go:15-31`). That means no OIDC and no `avm_` tokens.
- No stability promise on the `pkg/daemon` or `pkg/controlplane` Go APIs, and the build needs CGO. Rebuild on every upstream release, with CI contract tests.
- The auth path depends on Lewbox being available, softened by caching and grace windows we control.

**Verdict:** the best technical fit for a compute reseller, especially for exact billing and allowance enforcement. It costs a custom binary and tracking upstream, so make it **phase 2**, unless the spike shows Option B's metering can't support the pricing model.

### Option D: thin auth-swap router (an optional add-on to B)

**How it works:**
1. The customer sends an `lbx_` key to `api.<domain>` with a region, or to `<region>.api.<domain>`.
2. Lewbox swaps in that account's per-cluster `avm_` token, in `Authorization`, `X-API-KEY` and the WebSocket subprotocol.
3. It forwards the request **without rewriting IDs**.

**What it adds:**
- one key for every region;
- keys that never expire, because Lewbox rotates the `avm_` tokens behind them;
- per-key rate limits;
- per-plan create checks, such as allowed runtimes and per-sandbox size.

**What it costs:** Lewbox sits in the API path, including exec and file streams. Previews, TCP and SSH still go direct.

**Verdict:** only if one global key or rate limits become hard requirements.

### Option E: our own sandbox API on top of AerolVM
- Loses compatibility with the official AerolVM, E2B and Daytona SDKs, and means writing our own SDKs.
- **Not recommended.**

### Comparison

| | A | **B (recommended)** | C | D (B + router) | E |
|---|---|---|---|---|---|
| Stock AerolVM clusters | yes | **yes** | no (our build) | yes | yes |
| Official SDKs unchanged | mostly | **yes** | yes | yes | no |
| Lewbox in request path | yes | **no** | auth only (cached) | API yes | yes |
| One key for all regions | yes | no (per-cluster keys) | either | yes | yes |
| Exact billing usage | no | approximate | **yes** | approximate | depends |
| Suspend / restore | manual | via admin API | **built in** | via admin API | yes |
| Non-expiring, branded keys | yes | no | yes | yes | yes |
| Upstream-churn exposure | high | **low** (about 6 admin routes + list) | high (Go APIs) | medium | high |
| Effort | XL | **S–M** | M + ongoing | M | XL |

---

## 7. Billing and metering for a compute reseller

There are two pricing models. Each one needs different mechanics.

### Per-hour compute (pay as you go)

- **Option B: poll each cluster.** Every 30–60s, Lewbox calls `GET /v1/sandboxes` with its admin token. From each sandbox it reads the owner, status, CPU, memory, GPU and runtime, and adds up `vCPU·s`, `MiB·s` and `GPU·s` per account per region.
  - The audit-export webhook supplies exact lifecycle timestamps. Those are `api` records with `sandbox_id`, `owner_ref` and `time`, plus `system:<subsystem>` records for automatic stops and reaps.
  - Accuracy is roughly the poll interval per state change, which is fine for hourly billing.
  - Cluster list can be **partial**: check `X-Cluster-List-Partial` and don't bill gaps as zero.
  - Audit events **do not carry sizes** (`pkg/auditlog/event.go`). Sizes come from the poll, or from a `GET` per event.
- **Option C:** use the `Reporter` samples directly.
- **Network egress and ingress billing:** per-sandbox `network/usage` in Option B, or `Reporter` byte samples in Option C.

### Fixed monthly price for "X compute"

Pin down which of two things this means.

- **(a) Reserved concurrency**, for example "up to 8 vCPU and 16 GB running at once". This is **exactly AerolVM's team quota**: one per account per cluster. A single pool shared across regions needs Lewbox to split it, or to re-balance quotas dynamically.
- **(b) A bucket of compute-hours**, for example "500 vCPU-hours per month". This needs metering as described above, plus **enforcement when the bucket runs out**:
  - Option B: quota to 0 plus stop through the admin API.
  - Option C: `Admitter` plus `StopByOwner`.
  - Either way, enforcement lags by a poll interval, so expect small overruns.

Lewbox's core is the same in all cases: a **usage ledger**, **plans**, **entitlements** and **invoices or credit**. For the payment side, consider Stripe usage-based billing, or let operators plug in their own processor.

---

## 8. Risks

- **Upstream velocity.** v0.6.8 → v0.6.13 shipped in about three weeks. Pin minor versions and run contract tests against each upstream release.
- **Identity is recent.** It shipped 2026-10-19. `plans/identity-and-access.md` still says "draft" in its header. Expect bugs.
- **Overlap with Aerol's own product.** The roadmap keeps Aerol's "managed multi-tenant service", and Lewbox is in effect a self-hosted version of it. Aerol could add self-service accounts upstream, or change the control-plane seam. **Talk to the maintainers before building.** Consider contributing what's missing:
  - a team-admin role, so customers can mint their own keys;
  - per-team create policy;
  - long-lived service tokens.
- **Token and team scale is untested.** Every node mirrors every token in memory. Count keys × accounts per cluster in the spike.
- **`*`-bound credentials** see every team's metadata (`TODOS.md:517-540`). Only Lewbox's admin integration should ever hold one.
- **Lewbox's admin token on each cluster** is fleet-admin power. Rotate it, store it sealed, and audit its use.
- **Missing E2B templates** may block E2B migrators (§5.3).

---

## 9. Open questions

### Product (us)

1. **Who runs Lewbox?** Options:
   - each operator self-hosts it, so the domain is theirs (`eu-west.api.<operator>.com`);
   - we host it as a control plane over operators' clusters (the `*.lewbox.com` in the example suggests this).

   The second option needs Lewbox to reach every cluster's admin API, and clusters to reach Lewbox's webhook, across operator networks.
2. **"Fixed monthly X compute": reserved concurrency (a) or an hours bucket (b)?** (§7)
3. **Are per-cluster keys final?** If customers will want one key for all regions, plan Option D or C now.
4. **Are expiring keys (365d at most) acceptable to operators' customers?**
5. **Is there a tenant hierarchy?** Is it just operator → account, or does an account need its own members and sub-keys (E2B "teams")? An AerolVM team is the isolation unit, so members of one account would share sandboxes.
6. **Do plans need per-plan runtime and size rules** (for example "free tier: gVisor, 1 vCPU")? Stock AerolVM's create policy is per cluster. This would need Option D, C or upstream work.
7. **How do customers build templates and images** without E2B template builds?
8. **Licence:** MIT or Apache-2.0. Upstream is MIT, so both are compatible.

### For upstream, or for the spike

1. Is there a practical cap on `avm_` tokens or teams per cluster?
2. Is `pkg/controlplane` / `pkg/daemon.Run` a supported extension point for third parties?
3. Do the team quota routes work with identity **off** under a custom provider?
4. Can the API hostname and the preview / sandbox wildcard domain be configured separately per cluster?
5. Does `GET /v1/sandboxes` return CPU, memory, GPU and status for every owner to an admin token, and how do pagination and partial results behave at 10k+ sandboxes?
6. Do audit `api` records cover every state change that affects billing: create, start, stop, resize, delete, automatic stops, failover recreate?
7. When will E2B SDK ≥ 2.51 work, and is a host-based envd data plane planned?
8. What integrity checks does `aerolvm upgrade --release-url` apply? This matters for shipping our own `sandboxd` build (Option C).

### Unverified from reading

- Whether the create policy also applies to PAT and admin callers.
- E2B Connect protobuf and signed file URLs.
- Which error strings contain sandbox IDs.
- The benchmark behind "about 1,000 workers".

---

## 10. Next steps

### M0′: spike, 2–3 days, to answer "is it really possible?"

1. Build **two** small AerolVM v0.6.13 clusters, for example 3 mixed nodes plus 1 node, with identity on and `SB_PAT_SCOPE=host`. Give each its own API and preview hostnames.
2. On each cluster, mint runner tokens for `team:a` and `team:b`. Check that cross-team requests return 404 on:
   - get, exec (including the WebSocket stream), files, ports, delete and list;
   - the E2B surface with SDK 2.50.0;
   - the Daytona surface.

   Check that a cluster-1 key is rejected on cluster 2.
3. Set a team quota and check it returns 429 under concurrent creates from different nodes. Set it to 0 and stop sandboxes, to simulate suspension.
4. Revoke a token and time how long until every node rejects it.
5. **Metering:** run a mix of sandboxes for an hour. Compare polled usage plus audit export against ground truth, including automatic stops and a killed worker.
6. **Scale:** script-mint 10k–50k tokens and teams. Watch memory, mirror sync time and auth latency.
7. **Option C feasibility, half a day:** build a `sandboxd` with a stub provider whose `Validator` maps `lbx_test` to `team:a` and whose `Reporter` logs samples. Confirm it boots in a cluster and that quotas apply.

### If the spike passes, rewrite MVP.md around Option B

| Milestone | Scope |
|---|---|
| M1 | Operator setup: register clusters (URL plus admin token), health check |
| M2 | Accounts and auth (email and password with argon2, sessions), operator admin users |
| M3 | Entitlements: account × cluster; team provisioning; per-cluster key mint, list, revoke and rotate; expiry notices |
| M4 | Plans → team quotas; suspend and restore |
| M5 | Usage ledger: poller plus audit-webhook consumer, per account per region; usage API |
| M6 | Customer and operator dashboard (SvelteKit), per-region SDK snippets |
| M7 | Billing integration (Stripe or a pluggable processor); prepaid-bucket enforcement |
| Later | Option C provider for exact metering and branded keys, or Option D router for one global key and rate limits |

---

## Appendix: where to look upstream

| Topic | Path in `aerol-ai/microvm` |
|---|---|
| Actors, roles, permissions | `DESIGN.md`, `plans/identity-and-access.md`, `docs/src/content/docs/enterprise/sso.mdx` |
| Auth chain, headers | `pkg/api/auth.go`, `pkg/api/apihttp/bearer.go`, `pkg/api/pat_scope.go` |
| Owner scoping | `internal/service/owner_scope.go`, `pkg/controlplane/principal.go` |
| Tokens and quotas | `internal/service/api_tokens.go`, `pkg/models/tokens.go`, `internal/service/team_quotas.go`, `pkg/models/team_quota.go` |
| Managed provider seam | `pkg/controlplane/controlplane.go`, `pkg/daemon/daemon.go:62-115`, `pkg/daemon/identity_guard.go`, `internal/service/usage.go`, `internal/service/fleet_control.go` |
| Route table | `pkg/api/server.go:249-334`, `pkg/api/v1/routes.go` |
| Facades | `pkg/api/e2b/`, `pkg/api/daytona/`, `pkg/api/runloop/`, `docs/.../using-e2b-sdk.mdx` |
| Cluster | `internal/cluster/` (`cluster.go`, `placement.go`, `topology.go`, `pki.go`, `dead_owner.go`), `docs/.../engineering-placement-failover.mdx`, `docs/.../cluster-ingress.mdx` |
| Public URLs | `pkg/caddy/client.go:138-172`, `docs/.../public-urls.mdx` |
| Sandbox tokens | `pkg/sandboxtoken/sandboxtoken.go`, `docs/.../sandbox-tokens.mdx` |
| Audit and export | `pkg/auditlog/event.go`, `internal/config/config.go` (`SB_AUDIT_EXPORT_*`), `docs/.../enterprise/audit-evidence.mdx` |
| Upgrades and release feed | `internal/nodeupgrade/paths.go`, `cmd/aerolvm/upgrade.go`, `cmd/aerolvm/release.go` |
| Roadmap and scope | `ROADMAP.md`, `CHANGELOG.md` (v0.6.8 identity, v0.6.12 team quotas, v0.6.13 latest) |
