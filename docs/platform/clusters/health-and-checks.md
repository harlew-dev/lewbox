---
title: Health and checks
description: The checks Lewbox runs on every cluster, what each status means, and how to fix a failure.
---

Lewbox checks each cluster when you register it, whenever you run `lewbox cluster check`, and every minute in the background. Each run updates the cluster's status, its version, and its node list.

```bash
lewbox cluster check eu-west      # run the checks now
lewbox cluster get eu-west        # the last results, without contacting the cluster
lewbox cluster list               # every cluster's status at a glance
```

## Statuses

| Status | Means |
|---|---|
| `ready` | Every check passed |
| `degraded` | Reachable, but a check failed. `status_detail` says which one and how to fix it |
| `unreachable` | The cluster's `/health` didn't answer. Its nodes are shown as not alive until it's back |

## The checks

They run in this order. The first three are **required**: when you register a cluster, a failure there stops the registration and nothing is stored.

| Check | Passes when | If it fails |
|---|---|---|
| `health` | `GET /health` on the admin URL answers | Check the admin URL, and that `sandboxd` is running |
| `version` | AerolVM is v0.6.13 or later | Upgrade AerolVM on every node |
| `admin_token` | The token is an admin token for every team (`*`) | Check `SB_IDENTITY_ENABLED=true` on every node, and the token's role |
| `members` | Every node the cluster lists is alive | Check `sandboxd` on the nodes it names |
| `runtime` | Every node offers the cluster's runtime | Install the runtime there, or register the cluster with the other runtime |
| `node certificates` | Every node that reports a certificate expiry has more than 14 days left | Renewal has failed; check `sandboxd`'s log on the node it names |
| `token_ttl` | A 100-year key can be created | Set `SB_TOKEN_MAX_TTL=876000h` on every node, or keys will expire |
| `endpoint` | The customer endpoint answers `/health` | Check the endpoint URL and its DNS |

AerolVM node certificates last 90 days and renew at two-thirds of their life, so a certificate with 14 days left means renewal has failed and the node will leave the cluster at day 90. `node certificates` reads each member's `aerolvm_cluster_mtls_cert_not_after_unix` metric from `GET /v1/metrics`. A node that does not answer is skipped, and the check stays ok when no node reports an expiry.

A node that isn't in cluster mode (a local-mode install) counts as a one-node cluster.

## Nodes follow the cluster

Every check also syncs the node list from AerolVM. New members appear, members that left disappear, and each node's role, version and alive state is updated. You don't add or remove node records by hand.

## History and uptime

Every check keeps a sample of the cluster's status and each node's capacity, and Lewbox deletes samples older than 31 days. The console and the CLI show a 30-day bar from them. A day with no samples is "no data", not downtime: a stopped orchestrator says nothing about the cluster.

```bash
lewbox cluster health eu-west --days 30    # a bar per day, then the percentage
lewbox cluster metrics eu-west --range 24h # sandboxes, reserved vCPUs and memory
lewbox overview                            # customers, capacity, clusters, top usage
```

`--days` is 1–31. `--range` is `24h`, `7d` or `30d`; metrics are averaged into 15-minute, 1-hour or 6-hour buckets. The bars use `█` for a full day, `▒` for a partial one and `·` when there is no data.

## Next

- [Remove nodes and clusters](remove-nodes-and-clusters.md)
- [Troubleshooting](../operations/troubleshooting.md)
