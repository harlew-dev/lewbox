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
2. if the node is the cluster's Raft leader, hands leadership to another voter and waits until that node reports it is the leader;
3. drains it, so no new sandboxes land there;
4. removes it from the cluster in AerolVM;
5. removes it from Lewbox.

If the node still owns sandboxes, AerolVM refuses. Stop them first, or pass `--force` to remove the node anyway: its sandboxes are then lost.

Lewbox refuses these removals, before changing anything:

- **The cluster's last node.** Remove the whole cluster instead (below).
- **The node behind the cluster's admin URL.** The orchestrator reaches the cluster through it. Register the cluster through another node first.
- **The cluster's Raft leader, when no other voter is alive to take over.** Add a server node (or bring one back) first. The same refusal appears if the cluster reports no leader at all, or if the leadership cannot be moved and confirmed; try again once the cluster is healthy.

AerolVM stays installed on the removed machine. To use it again, reset it there, then add it like a new machine:

```bash
sudo sandboxd reset --yes
lewbox node add node-2 --cluster eu-west
```

> [!NOTE]
> AerolVM denies a removed node's old identity, so the reset is required. Lewbox's end-to-end tests don't cover re-adding a removed machine yet.

## A cluster left without a leader after a removal

AerolVM v0.7.x has a bug: removing the Raft leader of a two-node cluster can
leave the surviving node trying to get a vote from the machine that was just
removed. Every write then answers `503` (`cluster: not raft leader`), and
`sudo journalctl -u sandboxd` repeats `raft: Election timeout reached` without
ever electing a leader. Lewbox avoids this by handing leadership to another
voter before the removal. A cluster removed by an older Lewbox can still be in
this state; repair it on the machine you keep:

1. Stop `sandboxd` on every surviving server node, and on the removed machine
   if it is still running:

   ```bash
   sudo systemctl stop sandboxd
   ```

2. On the one node you keep, give it the whole cluster's vote:

   ```bash
   sudo sandboxd recover-quorum --start
   ```

   It backs the node up first, makes it the only voter and starts `sandboxd`.
   It returns once the node leads the cluster.

3. If other server nodes must stay, rejoin them one at a time, waiting until
   each is `Ready` before the next:

   ```bash
   sudo sandboxd recover-quorum --rejoin --start
   ```

4. The removed machine stays out. Reset it before adding it again (above).

> [!CAUTION]
> `recover-quorum` is a last resort. The node's records become the cluster's
> records, so changes that only the stopped nodes had are lost. AerolVM's
> [lost-quorum runbook](https://github.com/aerol-ai/microvm/blob/main/setup/runbooks/lost-quorum-recovery.md)
> has the full procedure.

## Remove a cluster

```bash
lewbox cluster remove eu-west
```

This only stops Lewbox managing the cluster. It revokes Lewbox's own admin token on the cluster (if the cluster is reachable) and forgets the cluster and its nodes. **The cluster itself keeps running**, and its sandboxes aren't touched. Register it again any time with [`lewbox cluster add`](register-an-existing-cluster.md).

## Next

- [Add nodes](add-nodes.md)
- [CLI reference](../reference/cli.md#lewbox-node-remove-idname)
