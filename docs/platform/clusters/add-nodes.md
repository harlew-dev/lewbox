---
title: Add nodes
description: Join another machine to an existing cluster with lewbox install and one lewbox node add.
---

Adding a node is the same two commands as [starting a cluster](start-a-cluster.md), except `node add` gets `--cluster` instead of `--new-cluster`.

## Join a machine

On the new machine, signed in as an instance admin:

```bash
lewbox login https://lewbox.example.com
lewbox install --public-host 10.0.0.6
lewbox node add node-2 --cluster eu-west
```

Install it the **same way** as the cluster's other nodes: the same mode and runtime. `--cluster` takes the cluster's slug, or the name of any node already in it, so `--cluster node-1` works too.

```text
  ✓ join token from the orchestrator (single use, expires in 15m)
  ✓ reached 10.0.0.5:7003
Joining cluster "eu-west"
  → sudo bash …/install.sh join https://10.0.0.5:7003 --token ****** --ca-cert-hash sha256:0d1d8c… --node-id node-2 --role mixed
[join] node-2 joined the cluster as a mixed node, with a server token
Lewbox settings
  ✓ wrote /etc/sandboxd/lewbox.env and /etc/systemd/system/sandboxd.service.d/lewbox.conf, restarted sandboxd
  ✓ sandboxd healthy at http://127.0.0.1:21212/health
  → waiting for "node-2" to appear in cluster "eu-west"
  ✓ node "node-2" joined cluster "eu-west"
```

Lewbox asks the orchestrator for a single-use join token that lasts 15 minutes. It checks that this machine can reach the cluster on port 7003, and then runs AerolVM's join. The new node installs the same AerolVM version as the cluster, gets Lewbox's settings, and shows up in `lewbox node list` once the cluster reports it alive. A join usually takes 10–20 seconds.

Running it again on a node that's already a member does nothing and exits successfully.

## Roles

Each node runs as `mixed` by default: it stores cluster state, runs sandboxes, and takes traffic. Pass `--role` for bigger clusters:

| Role | Does |
|---|---|
| `mixed` | Everything. Right for clusters of up to about 10 nodes |
| `server` | Cluster state (Raft) only. Keep 3, 5 or 7 of them |
| `worker` | Runs sandboxes only |
| `ingress` | Takes customer traffic only |

> [!TIP]
> Keep an **odd** number of `mixed` or `server` nodes: 1, 3 or 5. Two nodes are no more fault-tolerant than one, because losing either one stops writes.

## When it can't join

| Message | What to do |
|---|---|
| `a local-mode install can't join a cluster` | Reinstall with `--public-host` or `--domain` |
| `cluster "eu-west" runs firecracker, but this install uses docker` | Reinstall with the cluster's `--runtime` |
| `cannot reach 10.0.0.5:7003` | Open ports 7000–7003 between the nodes, on a private network |
| AerolVM's `dial-back` check fails | The cluster can't reach *this* machine on 7000–7002. Same fix, the other way round |

## A machine without the Lewbox CLI

To join a machine yourself with AerolVM's installer, print a ready-made command:

```bash
lewbox cluster join-command eu-west
```

It prints a line that downloads the cluster's AerolVM version, checks it against the release's checksums, and joins with a 15-minute single-use token. After it runs, apply [Lewbox's settings](../reference/what-lewbox-changes.md#lewboxs-settings) on that machine yourself, or that node answers `401` to the orchestrator.

## Next

- [Health and checks](health-and-checks.md)
- [Remove nodes and clusters](remove-nodes-and-clusters.md)
