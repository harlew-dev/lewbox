# Decisions

Every decision made while building the self-hosted E2B version of Lewbox, written down because you weren't around to ask. Each one has what was chosen, why, the other options, and the caveats. They are numbered in the order they were made. If you disagree with one, the "Other options" line says roughly what changing it would take.

---

## D1. One operator per instance; "organizations" are customer accounts

**Chosen.** A Lewbox instance belongs to one operator: the instance admins. Every other account is a **customer**. A customer's **organization** is their account with you: it holds their plan, limits, sandbox keys, sandboxes, usage and credits, and can have several members (owner/admin/member/viewer).

**Why.** That's the product you described: you own the servers and resell the compute. The MVP spec (MVP.md) describes a bigger platform where many businesses each bring their own clusters and their own customers (organization → workspaces → members). That's two levels of tenancy, a separate portal per business, and a lot more surface. The organization model that was already built (members, roles, `lba_` API keys) maps one-to-one onto "a customer's team", so nothing built had to be thrown away.

**Other options.** (a) Keep MVP.md's organization → workspace layering. Lets you host Lewbox for other operators, at roughly twice the data model and UI. (b) No organizations: one account = one customer. Simpler, but no teams, and E2B-style products all have teams.

**Caveats.** MVP.md is now partly out of date (it was left unedited, per AGENTS.md). Someone who is both your staff and a customer needs two accounts, or an admin account that is also a member of an organization.

## D2. SQLite stays; no Postgres

**Chosen.** Keep the existing SQLite database (one file, modernc driver, no cgo).

**Why.** One binary plus one file is the simplest thing to run, back up (`sqlite3 .backup` or copy while stopped) and restore. The load is small: a sync loop every 15 s and a few hundred rows of usage per customer per day.

**Other options.** Postgres, as MVP.md planned, for a highly available orchestrator or many thousands of customers.

**Caveats.** One orchestrator process at a time. That's fine because Lewbox isn't in the sandbox request path: if it's down, customers' sandboxes and keys keep working; only sign-up, key changes, limit syncing and usage recording pause.

## D3. Customers never see clusters; they see regions

**Chosen.** Cluster and node routes became admin-only (before this, any signed-in account could read cluster admin URLs and node details). Customers get `GET /v0/regions`: slug, name, public endpoint, runtime, status.

**Why.** Customers shouldn't learn your topology, admin URLs or versions. "Region" is the word E2B-style products use.

## D4. Sign-up without email

**Chosen.** Sign-up is email + password, with three modes the operator picks in Settings: `open`, `approval` (new organizations wait as `pending` until you approve them) and `closed` (you create accounts). There is no email verification and no emailed password reset; admins reset passwords from the console or CLI.

**Why.** Email needs SMTP credentials and deliverability work you'd have to set up before anything worked. `approval` mode covers abuse in the meantime: nobody gets compute until you click Approve.

**Other options.** Add SMTP (`LEWBOX_SMTP_URL`) for verification and self-service reset. The obvious next step for a public instance.

**Caveats.** In `open` mode, anyone can sign up with any email address (unverified). Use `approval` if that matters, or set the default plan to tiny limits.

## D5. Plans are named limit sets; limits are global across regions

> **Superseded by D20** (2026-11-12): plans are now assigned per region.

**Chosen.** A plan is `{max_sandboxes, max_vcpus, max_memory_mb}` (each nullable for unlimited). Every organization is on one plan, with optional per-organization overrides. New sign-ups get the default plan. Limits apply to the organization's total across all regions.

**Why.** The simplest model that covers "Free: 2 sandboxes" / "Pro: 20 sandboxes, 40 vCPU" and custom deals. Per-region limits and per-sandbox size caps were left out (AerolVM can't enforce per-team size caps anyway; MVP.md §8.5).

## D6. One API key per region, not one key for everything

**Chosen.** A customer's API key is an AerolVM `runner` token minted on one cluster and bound to their organization's team. Creating a key means picking a region; with one region there's nothing to pick. The create screen shows the key once, with `SB_API_URL`/`SB_API_TOKEN` (and the E2B variables on Docker regions).

**Why.** Lewbox stays out of the sandbox request path (if Lewbox is down, sandboxes keep working), and AerolVM's own token system does the isolation: a team-bound token gets `404` for everything outside its team. AerolVM generates the token itself, so the same string can't be valid on two clusters.

**Other options.** (a) A Lewbox-wide key with Lewbox proxying every SDK call: one key, but Lewbox becomes a bottleneck and single point of failure, and every byte of exec/file traffic flows through it. (b) Mint a token on every region at once and show a bundle: confusing.

**Caveats.** With several regions a customer manages several keys. E2B has one. Most self-hosters start with one region, where this is invisible.

## D7. AerolVM API facts came from reading v0.7.0's source

**Chosen.** Field names, paging and status codes for tokens, team quotas, the sandbox list, the sandbox index and node capacity were taken from AerolVM v0.7.0's source (with file:line references) and from the real responses already in `internal/aerolvm/testdata`. `e2e.sh` then proves the customer journey against real AerolVM.

**Caveats.** Things not proven against a live cluster are listed in STATUS.md.

## D8. Sandboxes are attributed with the cluster's sandbox index (cluster mode required)

**Chosen.** AerolVM's sandbox list doesn't say who owns a sandbox. Its cluster sandbox index (`/v1/cluster/sandbox-index`) does (`owner_ref: team:<t>`), so the sync joins the two by ID. This needs cluster mode, which every node Lewbox starts with `lewbox node add` uses.

**Other options.** Mint a `viewer` token per customer per cluster and list with each: works in single-node mode too, but costs one API call per customer per pass and one more stored secret per customer.

**Caveats.** A single-node install started with `lewbox install --mode local` can't attribute sandboxes. Customers can still use it, but their sandboxes don't appear in the portal and usage isn't recorded there. Start nodes with `node add --new-cluster` (the default flow) to get everything.

## D9. Usage is metered by polling every 15 s

**Chosen.** Each sync pass adds `interval × size` for every running sandbox to an hourly bucket per customer and region. The gap credited per pass is capped at 2 × the interval.

**Why.** It needs nothing configured on the clusters. Accuracy is within one interval per sandbox lifetime (±15 s), which is fine for hourly pricing.

**Other options.** AerolVM's audit export (a signed webhook per state change) gives exact start/stop times. It needs per-cluster config, an ingest endpoint and run reconstruction. Worth adding if per-second billing accuracy ever matters.

**Caveats.** If Lewbox is down, that time isn't metered (customers aren't charged for it). Stopped sandboxes and snapshots aren't charged for storage (AerolVM doesn't report snapshot sizes; see the research notes).

## D10. Limits are shared across regions in "shared" mode

> **Superseded by D20** (2026-11-12): each region has its own plan, so there is no cross-region headroom. Built in Phase 3, removed in Phase 3b.

**Chosen.** Every pass, for each customer and each limit: `headroom = limit − total use in all regions`, and each region's AerolVM team quota is set to `use there + headroom`. A region whose quota would be 0 gets a blocking quota (`max_cpu 0.001, max_memory_mb 1`), because 0 means "no limit" to AerolVM.

**Why.** No spurious "quota exceeded" errors while there's room anywhere, and nothing to tune. AerolVM enforces each region's quota atomically.

**Other options.** MVP.md's `strict` mode: split the headroom between regions and move it by demand, so the total can never exceed the limit, at the cost of occasional `429`s and a lot more logic.

**Caveats.** A customer creating sandboxes in two regions in the same ~15 s window, right at their limit, can briefly go over by a sandbox or two. The next pass sees it and blocks further growth until they're back under. With one region this can't happen.

## D11. Monitoring uses what AerolVM already reports; no node agent

**Chosen.** Every 60 s check stores one sample per cluster (status, latency) and one per node (alive, sandboxes, cores, reserved vCPU, memory total/free, disk free), from the `/v1/cluster/members` capacity heartbeat. Samples are kept 31 days. Uptime per day = OK samples ÷ samples; days without samples show as "no data", not "down".

**Why.** Nothing extra to install, secure or upgrade on your servers.

**Other options.** Install `node_exporter` (or a `lewbox node agent`) on each node for CPU utilisation, load, network and disk I/O. The usual next step if you want real CPU graphs.

**Caveats.** There's no CPU utilisation. The console shows CPU *allocation* (vCPUs reserved by sandboxes vs cores) instead. Disk total is only known if you set `SB_HOST_DISK_GB`. If the orchestrator itself is down, that time is "no data".

## D12. Billing is a credit ledger with no payment provider

> **Superseded by D24** (2026-11-12): no billing at all. Never built.

**Chosen.** Prices per vCPU-hour and GiB-hour (defaults $0.05 and $0.016, the E2B/Daytona level). Billing mode `off`, `show` (track and display cost) or `enforce` (prepaid credits; at zero the customer is suspended and their sandboxes stopped; topping up restores them). Credits come from the operator (console, CLI, API), an optional sign-up credit, and an optional monthly grant per plan. Every ledger entry has a unique reference, so retries never double-charge or double-credit.

**Why.** Reselling compute needs metering and a way to stop non-payers; taking card payments is a separate decision (Stripe account, tax, invoices) you'll want to make yourself.

**Other options.** Stripe Checkout for top-ups (a webhook calling `POST /v0/credits` with the payment ID as the reference is all Lewbox needs; the reference makes it idempotent). Or postpaid invoicing from `GET /v0/usage`.

**Caveats.** Charging happens per closed hour, so a customer can overspend by up to an hour of their usage before being stopped.

## D13. One web app: portal at `/`, console at `/admin`

**Chosen.** The same React app serves both. Customers land on the portal (`/`, `/sandboxes`, `/keys`, `/usage`, `/billing`, `/team`); instance admins land on the console (`/admin/...`). Old console URLs redirect.

**Why.** One build, one login, one origin and CSP. The URL tells you which side you're on.

**Other options.** A separate admin build on a secret path (lewcontext did this). More isolation, twice the build.

## D14. Limit values: a positive number or null, nothing else

**Chosen.** On a plan, `null` means unlimited. On a customer's override, `null` means "use the plan's value". `0` and negatives are refused everywhere. To give one customer unlimited use, put them on a plan without that limit.

**Why.** The first version treated `0` as "unlimited" on plans but as "zero" on overrides, and the CLI and API disagreed. One rule is easier to explain. "Block everything" already has a name: suspend.

**Other options.** A separate "unlimited" marker on overrides (`-1` or the string `"unlimited"`). That's more flexible, but every client has to learn a third value.

## D15. Sign-up has its own rate limit, and Lewbox can trust a reverse proxy

**Chosen.** Sign-up allows 5 attempts per IP per hour, separate from sign-in's budget. `--trusted-proxy CIDR` (`LEWBOX_TRUSTED_PROXIES`) makes Lewbox take the client IP from `X-Forwarded-For` when the connection comes from that proxy.

**Why.** Behind Caddy on the same host, every request otherwise comes from `127.0.0.1`. One person spamming sign-up would then lock everyone out of signing in. Set `--trusted-proxy 127.0.0.1/32` in production (RELEASING.md does).

**Caveats.** IPv6 users can rotate addresses. `approval` sign-up mode remains the real defence against abuse.

## D16. Removing a cluster refuses if its customer keys can't be revoked

**Chosen.** `cluster remove` revokes every customer key on the cluster first. If any revoke fails, it refuses, unless you pass `--force`.

**Why.** Otherwise those tokens keep working on the cluster while Lewbox has forgotten them. That's the "fail closed" rule from AGENTS.md.

## D17. Errors shown to customers never include cluster internals

**Chosen.** When a region can't be reached, customers see "region X is not reachable right now". The real error (which includes the admin URL) is logged and shown only to instance admins. AerolVM's own 4xx messages (for example a quota 429) are passed through.

## D18. Members are added directly, without an invitation

**Chosen.** An organization's owner or admin adds an existing account by email. A customer can't add an instance admin account. There are no invitation emails or accept links.

**Why.** There's no email (D4). Being added only grants access, so the risk is low.

**Other options.** Invitation links: a one-time URL the owner copies and sends; the new member signs up or signs in through it. This is the natural next step for teams, and works without SMTP.

## D19. MIT licence

**Chosen.** Added an MIT `LICENSE` (copyright Harrison Lewis), because MVP.md states "Licence: MIT". Change it before publishing if that's not what you want.

## D20. A plan per region, assigned by the operator (your call, 2026-11-12)

**Chosen.** A customer has a **subscription** in each region they may use: a plan, plus optional per-region overrides. Only the operator creates, changes or removes subscriptions. A customer sees only the regions they're subscribed to. They can create keys only there, and each region's AerolVM quota is exactly that region's plan. New sign-ups get the default plan in the default regions (both in Settings; either can be empty, and then the customer waits for you).

**Why.** You asked for limits per cluster, with customers subscribing per cluster. With the operator choosing (also your call), nobody can pick a bigger plan than they paid for. Limits are now enforced by AerolVM alone, atomically, with no race between regions (the D10 caveat is gone).

**Other options.** Customer self-serve from "public" plans. Easy to add later: a `public` flag on plans and a "choose a plan" step in the portal, gated by credits.

**Caveats.** Removing a customer's subscription revokes their keys in that region and stops their sandboxes there. It refuses if a key can't be revoked, unless forced (D16).

## D21. Plans have no price (your call, 2026-11-12)

> Moot after D24: there's no billing to price.

**Chosen.** Plans only set limits. Money is purely usage-based (vCPU-hours and GiB-hours from credits, D12). The "monthly credits per plan" idea was dropped.

**Other options.** A monthly price per plan, charged from credits per region subscription. A fixed monthly charge per subscription would slot into the same ledger.

## D22. Watch node certificate expiry

**Chosen.** AerolVM v0.7.0 node certificates last 90 days and renew themselves at two-thirds of that (`internal/cluster/cert_renew.go`). Lewbox's health checks read each node's `aerolvm_cluster_mtls_cert_not_after_unix` metric and warn when one is within 14 days of expiry, so a silently failing renewal shows up before the cluster splits.

**Why.** AerolVM PR #986 (an open plan) says older join paths never renew. v0.7.0's built-in join, which Lewbox uses, does. Watching costs one metrics call per node per check.

## D23. Customers' snapshots are listed with a read-only token per region

**Chosen.** When someone opens the Snapshots page, Lewbox mints a one-hour `viewer` token for the workspace's team, keeps it in memory only (reused for 50 minutes), and lists that team's snapshots on every node of the region, caching the result for 30 s. Deleting mints a one-hour `runner` token for the workspace's team, deletes, and revokes it. Nothing is stored.

**Why.** AerolVM v0.7.0's snapshot list doesn't say who owns a snapshot, is per node, and the admin token can't address a customer's snapshot by name. A team-bound token sees exactly that team's snapshots.

**Other options.** Only list snapshots that were pushed to a registry, and infer the team from the `tenant-<hash>` path segment. That needs a registry set up on every cluster.

**Caveats.** Listing costs a token mint per workspace and region per hour and one call per node per page view. No snapshot sizes.

## D24. No billing: plans only set limits (your call, 2026-11-12)

**Chosen.** There are no prices, credits, balances or automatic suspensions for non-payment (Phase 5 was dropped before it was built). Plans are limit sets that the operator assigns per region. Usage (vCPU-hours, GiB-hours, sandbox-hours) is still recorded, for charts and so you can see who uses what. Customer accounts normally come from you: the console's **New customer** form creates the account, its organization and its region plans in one go. Self sign-up still exists but defaults to `closed`.

**Why.** You said you don't need to take payments, just plans that set limits, assigned by an admin.

**Other options.** The design is kept in `05-billing.md`: a credits ledger with idempotent references, hourly usage charges, and suspension at zero. Usage is already metered hourly per customer and region, so it would slot in later without schema changes to usage.

**Caveats.** Nothing stops a customer using their plan's limits around the clock. Limits are the only control, plus Suspend.

## D25. A workspace's plan is split between its members (your call, 2026-11-12)

> **Superseded by D29**: built, then reverted.

**Chosen.** Each region's plan belongs to the workspace and is divided into fixed member shares. Owners and admins set shares; members without one split the rest equally (one member gets everything; 4 members on 4 GB get 1 GB each). Explicit shares can't add up to more than the plan, and AerolVM enforces each share exactly. A member whose share of something is 0 is blocked in that region until an owner gives them some.

**Why.** You wanted per-member limits inside a workspace (4 GB workspace, 1 GB each) and chose a fixed split over shared caps, so the workspace total can never be exceeded, not even briefly.

**Other options.** Caps plus a shared total: each member gets "their use + what's left in the workspace", updated every sync. More flexible (an idle member's share is usable by others), with the brief-overshoot race D10 described.

**Caveats.** An idle member's share sits unused. With small plans, an equal split can round to 0 (2 sandboxes between 4 members blocks two of them until the owner sets shares).

## D26. Each member has their own AerolVM team (your call, 2026-11-12)

> **Superseded by D29**: built, then reverted.

**Chosen.** AerolVM can only cap teams, so every workspace member gets a team, and their keys are bound to it. A member's keys see only that member's sandboxes and snapshots. Owners and admins see and stop everyone's from the portal.

**Caveats.** Members can't share a sandbox through the SDK. One member can't attach to another's sandbox with their own key.

## D27. Shares are set per region (your call, 2026-11-12)

> **Superseded by D29**: built, then reverted.

**Chosen.** A member's share is set separately in each region where the workspace has a plan.

## D28. "Workspace" in the UI, "organization" in the code

**Chosen.** Customers and the console call a customer's unit a **workspace**, your word for it. The API, CLI and database keep `organization` (`/v0/organizations`, `lewbox org`), which were already built and documented.

**Other options.** Rename the API and CLI too (`/v0/workspaces`, `lewbox workspace`). That's cleaner for API users, and a mechanical change best done before the first release if you want it.

## D29. Limits are per workspace only: one AerolVM team per workspace (your call, 2026-11-12)

**Chosen.** A workspace is exactly one AerolVM team. Its plan in a region is that team's quota on that cluster, pushed as-is. All members share it, and every member's keys see the workspace's sandboxes. There are no per-member limits.

**Why.** You want Lewbox to be a UI wrapper around what AerolVM supports natively, not to invent things on top. AerolVM v0.7.0 limits only teams (`PUT /v1/teams/{team}/quota`). It has no per-user or per-key limits (checked in its source). Per-member limits (D25–D27) were built and verified on a real cluster, but needed a team per member plus Lewbox's own split arithmetic, so they were reverted (`a9fa739`).

**Other options.** Re-apply `5821ce3` if you ever want per-member limits. It works and passed the real-cluster journey. Or wait for AerolVM to support per-token quotas and expose them directly.

**What Lewbox adds on top of AerolVM, for the record:** accounts, workspaces, members and sign-up (AerolVM has no users); plans as named limit sets (each pushed as a native team quota); the sandbox list and usage charts (read from AerolVM's lists every 15 s); monitoring history (read from AerolVM's members and metrics); snapshot listing (native team-scoped tokens). Enforcement of every limit is AerolVM's own.

## D30. A member's sandbox keys are revoked when they leave

**Chosen.** Removing someone from a workspace, making them a viewer, disabling their account or deleting it revokes the sandbox keys **they created**. If a revoke fails, the change is refused until it succeeds. The workspace's management keys (`lba_`) and other members' keys are untouched.

**Why.** Every key in a workspace drives the same AerolVM team (D29), so a departed member's key would still see and run every sandbox in the workspace. The final review flagged it.

**Other options.** Keep keys alive and warn the owner, the way some products treat keys as the team's rather than the person's. Safer to revoke by default. A workspace that needs a key to outlive a person should have an owner create it.

**Caveats.** A production app using a departed member's key stops working. Create long-lived keys from an account that will stay.

## D31. A workspace is in one region (your call, 2026-11-12)

**Chosen.** A workspace has at most one subscription: one plan in one region. Moving it means removing the current subscription first (`409` otherwise). The database enforces it with a unique index on `subscriptions(org_id)`; the instance setting is `default_region` (one cluster slug or null). A sandbox key without a region uses the workspace's region.

**Why.** You want a workspace to be a simple unit with one place its sandboxes live, not a fleet scattered across regions. It also matches the portal, which has no region pickers once a workspace has one region.

**Other options.** Keep several regions and make the portal pick one per action. That is what D20 built; it makes plans, limits, usage and the UI more complicated than the product needs.

**Caveats.** An existing workspace with subscriptions in several regions keeps only its oldest one; the migration deletes the rest. Leaving a region still revokes its keys and stops its sandboxes.

## D32. Users create their own workspaces; sign-up creates only the account (your call, 2026-11-12)

**Chosen.** `POST /v0/auth/signup` creates an account and nothing else. A signed-in account then creates a workspace with `POST /v0/organizations`, becoming its owner: active when sign-up is `open`, `pending` under `approval`, refused when `closed`. An instance admin can create a workspace for any owner and does not apply the default plan. Non-admins get the default plan in the default region when both are set, and may own at most 10 workspaces.

**Why.** Sign-up creating a workspace made the web app guess a name and hid the choice from the customer. Separating the two lets onboarding ask for the workspace, and gives closed instances a way to let someone in without an operator creating the account by hand.

**Other options.** Keep the workspace in the sign-up form and create it in one call. Fewer requests, but it couples account creation to workspace creation and leaves a half-created account on failure.

**Caveats.** A new account has nowhere to go until it creates a workspace, so the web app sends it to onboarding. Setup links, added later, consume the link when the workspace is created.

---

*Later decisions are appended below as they are made.*

## D33. Setup links (your call, 2026-11-12)

**Chosen.** An instance admin creates a single-use link for one plan in one region. Sign-up with its token works even when sign-up is `closed` and claims the link; creating a workspace with the same token consumes it in one conditional `UPDATE … RETURNING`, gives that workspace the link's plan in its region, and makes it active in any mode. Only a hex SHA-256 of the `lbs_` token is stored, and the token is shown once at creation.

**Why.** Closed instances had no way to let a customer in without the operator creating the account and workspace by hand. A link carries the plan and region, so the customer's onboarding needs no operator step, and the two-stage claim/consume records who joined without letting one link create several workspaces.

**Other options.** A reusable invite code, or a public sign-up flow with operator approval. A reusable code loses the plan/region binding and the per-customer record; approval adds a manual step the link is meant to remove.

**Caveats.** The link is bound to one plan and region; moving the workspace later is still an operator action. A claimed link whose account never creates a workspace stays `claimed` until it expires, and the token cannot be handed to someone else.

## D34. DNS profiles: saved Cloudflare credentials, chosen per cluster (your call, 2026-11-13)

**Chosen.** Instance admins save Cloudflare credentials once as a **DNS profile** (name, zone, API token, optional ACME email), sealed with the keyring and tested before saving. A cluster in domain mode names its profile. `lewbox install --dns-profile NAME` fetches the token from the orchestrator and passes it to AerolVM's installer for DNS-01. Lewbox also keeps the cluster's `<domain>` and `*.<domain>` A/AAAA records pointing at its live ingress nodes, unless the cluster's `dns_records` is `off`.

**Why.** The token was typed on every install and never stored, and the operator made the DNS records by hand. Cloudflare only, because AerolVM v0.7.0's installer and Caddy build support only Cloudflare; other providers would need AerolVM changes.

**Other options.** Per-workspace profiles (customers don't run nodes, so no). Keeping the token off nodes by relaying DNS-01 through Lewbox, as the old design planned (needs an AerolVM change).

**Caveats.** The token still ends up on every node, in the installer's Caddy env file, and can edit the whole zone. Use a token scoped to the one zone. A new token reaches existing nodes only when `lewbox node configure` runs on each (they show as outdated until then).

## D35. Storage profiles, and a registry on each node (your call, 2026-11-13)

**Chosen.** Instance admins save S3-compatible storage (AWS S3, Cloudflare R2, MinIO, …) as a **storage profile**. A cluster with one runs CNCF Distribution on every node at `127.0.0.1:5000`, backed by the bucket under `clusters/<cluster id>/`, and AerolVM pushes every snapshot there (`SB_SNAPSHOT_PUSH_ENABLED`, `SB_MIRROR_PUSH_HOST`). Any node can then start a pushed snapshot. `node add` and `node configure` install and configure it; each node reports the profile versions it applied, so out-of-date nodes are flagged.

**Why.** AerolVM pushes to an OCI registry, not to a bucket. A registry per node on a shared bucket has no single point of failure, and the legacy app ran exactly this on real AerolVM. The registry is loopback-only without auth, so the PAT AerolVM sends to it is a random placeholder, not the real one.

**Other options.** One shared registry service (a single point of failure, and it needs TLS and auth). AerolVM's hosted registry (AOCR), which isn't self-hosted.

**Caveats.** The bucket keys are on every node of the cluster; use a key scoped to the bucket. Nothing in AerolVM or Distribution garbage-collects the bucket; deleting an archive retires its manifest but blobs stay until a sweep, which isn't built yet. This reverses D23's "Other options" note that a registry on every cluster was too much to ask.

## D36. Archive on stop, with a new ID on restore (your call, 2026-11-13)

**Chosen.** A cluster's `stop_policy` is `keep` (default) or `archive`. On an archive cluster, Lewbox's sync loop notices every stopped customer sandbox, snapshots it with a short-lived team token, waits for the push to the bucket, and deletes the sandbox. The archive (snapshot name, registry ref, and the sandbox's name, tags, image and size) is recorded and kept until deleted. **Restore** creates a **new** sandbox from the snapshot, on any node, with the old name and tags plus `lewbox.restored_from=<old id>`. On such a cluster, stopped sandboxes don't persist.

**Why.** Nodes have 70 GB of disk, and a stopped sandbox's disk stays on its node; only that node could start it. AerolVM v0.7.0 has no way to move a stopped sandbox and keep its ID (failover keeps the ID but loses the disk; snapshots move but start a new sandbox). Lewbox isn't in the request path, so it can't hide the new ID.

**Other options.** Keep stopped sandboxes and delete them after N days (no restore). Ask AerolVM for archive-on-stop that keeps the ID ([aerolvm-push-on-stop-request.md](aerolvm-push-on-stop-request.md)); when that lands, this setting maps onto it.

**Caveats.** E2B `pause` then `connect(id)` gets 404 on an archive cluster. Snapshots keep no `env` values, so a restore needs them passed again. Running programs end on stop, as before. Each archive and restore adds an image layer, so a sandbox cycled about 100 times reaches overlayfs's layer limit. A start that races the archive is caught by a re-check before the delete, but a write between that check and the delete can be lost.

## D37. Local copy retention and archive snapshot names, per cluster (your call, 2026-11-13)

**Chosen.** Two cluster settings beside `stop_policy`. `archive_local_retention` (`instant` default, `1h`, `1d`, `7d`, `never`) sets how long nodes keep their local copies of archived sandboxes before the cleanup timer removes them; `never` installs no timer. Each node reports the retention it applied, so a change flags nodes outdated until `node configure`. `archive_snapshot_name` (`id` default, `name`) names archive snapshots `archive-<id>` or `archive-<name>-<id>`.

**Why.** Cleanup was forced to "at once". A kept local copy makes a restore on that node faster at the cost of disk, and that trade-off is the operator's. Names make AerolVM's own snapshot lists readable.

**Other options.** A free-form name template: refused, because the `archive-` prefix (node cleanup, bucket delete guard, hiding archives from customers' snapshot lists) and the sandbox ID (names repeat; a restored sandbox keeps its name, and a name collision would make the archiver take a 409 for a finished snapshot) must both stay. A per-workspace retention: the copies are on the operator's disks.

**Caveats.** A snapshot's copy is aged from the snapshot; a pulled copy from the pull (Docker's last tag time), falling back to the image's creation. A copy whose age can't be read is kept. `never` lets disks fill. A name change applies to sandboxes archived afterwards.
