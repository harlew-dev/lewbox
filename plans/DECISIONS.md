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

**Chosen.** Plans only set limits. Money is purely usage-based (vCPU-hours and GiB-hours from credits, D12). The "monthly credits per plan" idea was dropped.

**Other options.** A monthly price per plan, charged from credits per region subscription. A fixed monthly charge per subscription would slot into the same ledger.

## D22. Watch node certificate expiry

**Chosen.** AerolVM v0.7.0 node certificates last 90 days and renew themselves at two-thirds of that (`internal/cluster/cert_renew.go`). Lewbox's health checks read each node's `aerolvm_cluster_mtls_cert_not_after_unix` metric and warn when one is within 14 days of expiry, so a silently failing renewal shows up before the cluster splits.

**Why.** AerolVM PR #986 (an open plan) says older join paths never renew. v0.7.0's built-in join, which Lewbox uses, does. Watching costs one metrics call per node per check.

---

*Later decisions are appended below as they are made.*
