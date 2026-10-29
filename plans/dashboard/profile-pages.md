# Dashboard: profile pages

> Status: Draft · Milestone: Alpha (M1, M3); credits Beta · Updated: 2026-10-29

Pages for a project's profiles: listing and creating them, a profile's keys and limits, its setup (agents, sign-ins, Git, SSH keys, telemetry) as the project sees it, setup links, its activity, and its credits once the `credits` plugin ships (Beta). The rules behind them belong to [profiles](../profiles.md) and [credits](../credits.md). A profile's own owner sees the [setup pages](setup-pages.md) instead.

## Today

| Legacy page (`lewbox-legacy/resources/js/pages/tenants/`) | What it does | Lines |
|---|---|---|
| `Profiles.svelte` | List: name, external id, status | 56 |
| `profiles/Create.svelte` | Name, external id, default cluster | 91 |
| `profiles/Edit.svelte` | Name, external id, default cluster ("where its sandboxes go when they don't name a cluster") | 93 |
| `profiles/Show.svelte` | Sections; create a setup link with a return URL and sections, shown once with "send it only to them" and its expiry; open the setup page; suspend | 312 |
| `profiles/ApiKeys.svelte` | Keys: name, cluster access (all or some), permissions ("Every key can read its profile and its sandboxes. Choose what else it may do."), expiry; shown once; revoke | 363 |
| `profiles/Limits.svelte` | Limits by kind and period; "No limits. Sandboxes can use whatever the clusters have." | 178 |
| `profiles/Credits.svelte`, `CreditBuckets.svelte`, `CreditTransactions.svelte` | Balance, buckets (subscription with interval, renews manually, one-time), transactions with live usage polled every 5 s | 103, 400, 232 |

Components: `ProfileStatus`, `UsageChart`, `UsageTiles`, and the setup sections shared with the hosted page: `profile/AgentList`, `profile/GitSettings`, `profile/SshKeyList`, `profile/TelemetrySettings`.

Problems: a profile's setup state (which agents are signed in, Git connected, telemetry on) isn't visible to the project without opening the hosted page; sign-ins in progress aren't visible at all; no per-profile activity view.

## Goals

- From a profile's page, see at once whether its sandboxes will work: agents ready, Git connected, limits sane, not suspended.
- Make keys hard to over-grant: permissions chosen deliberately, with presets for common jobs.
- Share the setup sections with the hosted pages, so they look and behave the same.

## Design

### List

`/p/{project}/profiles`: name, external id, status (active, suspended), running sandboxes, agents ready (icons for Claude Code and Codex, dimmed when not set up), Git (connected or not), default cluster, created. Search by name or external id; filter by status. Live.

Empty state: what a profile is (who sandboxes belong to, usually one of your customers), that a project's own backend usually creates them through the Admin API, with the snippet, and "Create profile".

### Creating a profile

`/p/{project}/profiles/new`: name; external id (your id for them, e.g. a customer id; unique in the project); default cluster; isolation (which levels its sandboxes may use, and the default, from the project's defaults; [profiles § Isolation](../profiles.md#isolation)); Git identity (name and email commits are made as). Then the new profile's page, with a callout to create a key or send a setup link.

### A profile

`/p/{project}/profiles/{prf}`, the overview:

- **Header:** name, id, external id, status. Actions: Create key, Send setup link, Suspend or Resume, `⋯` (Edit, Delete).
- **Readiness:** a checklist of what its sandboxes get from it ([profiles](../profiles.md), [sandboxes/profile-setup](../sandboxes/profile-setup.md)):
  - Claude Code: signed in with a subscription / an API key / not set up, and when it was last verified;
  - Codex: the same;
  - Git: N GitHub people and M accounts (of the limit) / not connected;
  - SSH keys: N keys;
  - Telemetry: exporting to Langfuse / off;
  - Isolation: which levels it may use and its default, with a warning when none of its clusters offers an allowed level.
  Each line links to where it's changed.
- **Sandboxes:** running now, and the list filtered by this profile.
- **Usage:** sandboxes running, vCPU and memory in use against its limits, as bars.
- **Recent activity** across its sandboxes, problems first.

Suspending explains its effect before confirming: every key is refused, running sandboxes stop (persisted ones keep files, others are deleted). Deleting is refused while it has sandboxes (`profile_has_sandboxes`), and says how many.

### Keys

`/p/{project}/profiles/{prf}/keys` ([profiles/profile-keys](../profiles/profile-keys.md)):

- **List:** name, last four, cluster access, permissions (as chips, or "Full access"), created by, created, last used, expires. Revoke.
- **Create:**
  - name;
  - **clusters:** all (including later ones) or chosen ones;
  - **permissions:** "Full access (including permissions added later)" or chosen ones, grouped: *Sandboxes* (create, update, start, stop, delete), *Inside sandboxes* (exec, ports, ports.view, agents), *Activity* (activity.view, activity), *Templates* (create, delete). Presets fill them in: **Read only**, **Worker** (create, exec, delete), **Agent worker** (create, exec, delete, agents), **Full access**. Each permission has a one-line description from the [api/permissions](../api/permissions.md) table;
  - expiry: never, 7, 30, 90 days, or a date.
- The token (`lbp_…`) is shown once, with `LEWBOX_API_KEY` snippets.
- Sandbox keys (`lbs_`) aren't listed here: they last 15 minutes and are made by the profile's own backend. The sandbox's activity shows when one was made.

### Limits

`/p/{project}/profiles/{prf}/limits` ([profiles/limits](../limits.md)): a form with one row per limit kind, each blank for unlimited, grouped:

| Group | Kinds |
|---|---|
| Running at once | `running_sandboxes`, `vcpus`, `memory_mb` |
| Storage | `storage_gb` (persisted sandboxes' disks) |
| Per sandbox | `sandbox_vcpus`, `sandbox_memory_mb`, `sandbox_disk_gb`, `sandbox_idle_seconds`, `sandbox_lifetime_seconds` |
| Templates | `templates`, `template_storage_mb` |
| Budgets (Beta) | `compute_units` per day or month |

Each row shows current use beside the field ("12 of 20"), and lowering a limit below current use warns what happens: nothing stops, but new creates are refused. Units are shown human-friendly (vCPUs, GB, minutes) while the API keeps its own units.

### Setup

`/p/{project}/profiles/{prf}/setup`, the project's view of what the hosted setup page shows the profile's owner ([setup-pages](setup-pages.md)). The same React components, with project-only additions:

- **Setup links:** create one with a return URL and the sections to show (agents, Git, SSH keys, telemetry); it's shown once, with its expiry and "Send it only to them"; list links with their state (unused, in use until …, used, expired) and Revoke. "Open setup page" opens it in a new tab as the project, for helping a customer.
- **Agents:** each agent's credential kind, provider and verified time; set or replace an API key directly (for a project that manages keys for its customers); remove a credential. Subscription sign-ins must be done by the owner on the setup page, or through a link.
- **Git:** identity (name, email); the profile's GitHub people (default first, each with whether their authorization works, Make default, Remove) and accounts (each with its repository selection, the person it's reached through, any error, Remove), available accounts with Link, the count against the limit, and an install link to add more; Disconnect all.
- **SSH keys:** the profile's keys, which every sandbox gets; add, remove.
- **Telemetry:** the provider and endpoint, signals, whether content is included; edit or turn off ([profiles/telemetry](../profiles/telemetry.md)).

### Sign-ins

`/p/{project}/profiles/{prf}/sign-ins`: subscription sign-ins for this profile, in progress and past ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)). Each shows its agent, status (`queued`, `starting`, `awaiting_code`, `awaiting_approval`, `submitting_code`, `completed`, `failed`, `expired`, `cancelled`), started by, times, the helper sandbox, and the error if it failed. Live, replacing legacy's 2 s Inertia poll. A project member can cancel one.

### Activity

`/p/{project}/profiles/{prf}/activity`: the activity log across the profile's sandboxes, with the same filters as a sandbox's ([sandbox-pages](sandbox-pages.md#activity)) plus a sandbox filter.

### Settings

`/p/{project}/profiles/{prf}/settings`: name, external id, default cluster, Git identity; Suspend; Delete.

### Credits (Beta)

`/p/{project}/profiles/{prf}/credits` and its tabs, the `credits` plugin's `profile.tabs` slot, shown when it's on for the project ([credits/balances](../credits/balances.md)):

- **Balance:** total, by bucket in spending order, renewals.
- **Buckets:** add a subscription bucket (allowance, interval, renews manually or not, next renewal) or a one-time top-up; edit; renew now.
- **Transactions:** every grant, spend and adjustment, with the live spend of running sandboxes updating in place (from the event stream, replacing the 5 s poll), filters by kind and date, and "Add adjustment" with a reference.
- **Usage:** compute units by day, by sandbox size.

## API

From [profiles](../profiles.md) and its parts: profile CRUD, suspend, readiness summary; keys; limits with current use; setup links; agent credentials; sign-ins; Git; SSH keys; telemetry. From [credits](../credits.md) in Beta: buckets, transactions, usage.

## Testing

Playwright: create a profile and a key with the Worker preset, use the key with the API to create a sandbox, see it on the profile page live; set a limit below use and see the warning; create a setup link, open it, sign in an agent with the fake sign-in flow, and watch readiness turn green on the project's page; suspend and watch the profile's sandboxes stop.

## Build plan

1. List, create, a profile's overview (without readiness), settings, suspend, delete. (M1)
2. Keys with presets. (M1)
3. Limits. (M1)
4. Setup tab with setup links, agents, Git, SSH keys, telemetry; readiness checklist. (M3)
5. Sign-ins. (M3)
6. Activity. (M4)
7. Credits. (Beta)

## Open questions

1. **Should projects be able to sign in a subscription on a profile's behalf** from the dashboard, or only the owner through a setup link? Legacy only allowed the latter, and [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) has the legal question.
2. **Key permission presets:** are these four right? They're the combinations Lewagent and the docs examples use.
3. **Profile-level SSH keys vs sandbox-level** in one view, or separate? (Proposed: profile keys here, a sandbox's own keys on the sandbox.)
