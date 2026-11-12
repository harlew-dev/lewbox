# The product

## Who uses it

| Who | What they are in Lewbox | Where they work |
|---|---|---|
| **Operator** | An *instance admin*. Owns the servers, runs `lewbox serve`, installs AerolVM on machines with `lewbox install` / `lewbox node add`. | The **console** (admin side of the web app) and the CLI |
| **Customer** | A normal account (not an instance admin) that signed up, plus the **organization** it belongs to. An organization is the customer's account: it holds a plan per region (assigned by the operator), keys, sandboxes and usage. Several people can share one organization (members with roles). | The **portal** (customer side of the same web app), the SDKs, and optionally the Lewbox API with an organization API key |

One Lewbox instance belongs to **one operator**. Lewbox is not a platform for many operators (the MVP's "organization = a business that resells" layer is dropped; see DECISIONS D1).

## What a customer does

1. Gets an account from the operator (**New customer** in the console: email, password, organization name and a plan per region), or signs up themselves if the operator has turned sign-up on (it's off by default).
2. Lands on **Get started**: pick a region, create an **API key**, copy the env vars and a 6-line SDK example.
3. Runs sandboxes with the **AerolVM SDK** (or the E2B SDK through AerolVM's `/e2b` facade) straight against the region's endpoint. Lewbox is never in the sandbox request path.
4. Sees their **Sandboxes** (every region), **Usage** (charts: vCPU-hours, memory GiB-hours, sandbox-hours), **Plan & limits per region**, **Team** (members) and **API keys**.
5. Can stop or delete their sandboxes from the portal.

The customer never sees servers, nodes, clusters, admin tokens or other customers. Clusters are shown to them as **regions** (name + endpoint).

## What the operator does

1. Installs Lewbox (`lewbox serve`, Docker or systemd) and AerolVM on servers (`lewbox install`, `lewbox node add`). Already built.
2. In **Settings**: instance name, sign-up mode (closed by default; open or approval), default plan and regions for sign-ups.
3. In **Plans**: named limit sets (sandboxes, vCPUs, memory). There is no billing (D24).
4. In **Customers**: create customers; see every organization, its plan in each region, live usage vs limits, status; approve, suspend, assign or change a region's plan, override its limits.
5. In **Overview** and **Servers**: fleet health and 30-day uptime per cluster and node, sandboxes running, capacity, usage charts.

## Words

| Customer sees | Operator sees | Code / API |
|---|---|---|
| Region | Cluster | `cluster` |
| — | Server / node | `node` |
| Sandbox | Sandbox | `sandbox` (mirror of AerolVM's) |
| API key (per region) | Sandbox key | `sandbox_key` (`avm_` token on one cluster) |
| Organization | Customer | `organization` |
| Management API key | Organization API key | `admin_key` (`lba_`) |
| Plan, limits | Plan | `plan`, limits |
| Usage | Usage | `usage` |

## Architecture (unchanged in spirit)

```
customers' apps ──(AerolVM / E2B SDK, avm_ key)──► region endpoint (AerolVM cluster)
                                                         ▲
browser (portal / console) ──► lewbox serve ──(admin avm_ token: tokens, quotas,
                                  │             sandboxes list/stop/delete, members)
                                  └── SQLite (one file)
```

`lewbox serve` runs the API, the web app and these background loops:
- **check** (60 s): cluster checks and node sync (exists), plus a health sample per cluster and node (Phase 4);
- **sync** (15 s): mirror every cluster's sandboxes, meter usage, push each organization's plan in each region to that cluster as a team quota (Phases 3, 3b);
- **prune** (daily): drop samples older than their retention.
