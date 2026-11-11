---
title: Remove nodes and clusters
description: Take a node out of its cluster, or stop managing a cluster, safely.
---

## Remove a node

```bash
lewbox node remove node-2
```

It asks you to confirm (`--yes` skips the question). Then Lewbox:

1. checks the node is still a member of its cluster;
2. drains it, so no new sandboxes land there;
3. removes it from the cluster in AerolVM;
4. removes it from Lewbox.

If the node still owns sandboxes, AerolVM refuses. Stop them first, or pass `--force` to remove the node anyway: its sandboxes are then lost.

Lewbox refuses two removals:

- **The cluster's last node.** Remove the whole cluster instead (below).
- **The node behind the cluster's admin URL.** The orchestrator reaches the cluster through it. Register the cluster through another node first.

AerolVM stays installed on the removed machine. To use it again, reset it there, then add it like a new machine:

```bash
sudo sandboxd reset --yes
lewbox node add node-2 --cluster eu-west
```

> [!NOTE]
> AerolVM denies a removed node's old identity, so the reset is required. Lewbox's end-to-end tests don't cover re-adding a removed machine yet.

## Remove a cluster

```bash
lewbox cluster remove eu-west
```

This only stops Lewbox managing the cluster. It revokes Lewbox's own admin token on the cluster (if the cluster is reachable) and forgets the cluster and its nodes. **The cluster itself keeps running**, and its sandboxes aren't touched. Register it again any time with [`lewbox cluster add`](register-an-existing-cluster.md).

## Next

- [Add nodes](add-nodes.md)
- [CLI reference](../reference/cli.md#lewbox-node-remove-idname)
