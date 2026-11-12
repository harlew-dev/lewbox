---
title: Sandboxes and snapshots
description: See the sandboxes and snapshots in your workspace, and stop or delete them.
---

Lewbox mirrors every sandbox in your region. The portal shows the sandboxes that belong to your workspace, with their state and size. The region is still the source of truth: Lewbox polls it every 15 seconds.

## Sandboxes

Open **Sandboxes** in the sidebar. The list includes sandboxes that are creating, started, stopped or in error. A sandbox you delete is gone from the list.

Use the row menu to **Stop** or **Delete**:

- **Stop** frees the compute but keeps the sandbox and its disk.
- **Delete** removes the sandbox.

Both run on the region first and then update Lewbox; if the region cannot be reached, nothing is claimed to have happened.

A stopped sandbox still counts against your plan's sandbox limit and keeps its disk, exactly as the region counts it. Delete it when you no longer need it.

A sandbox belongs to one workspace: a member of another workspace gets "not found", never a peek at it.

## Snapshots

Snapshots you take with the SDK show up under **Sandboxes → Snapshots**:

```ts
const snapshot = await sb.snapshot()
```

The portal lists each snapshot's name, source sandbox, image, size and time. A snapshot belongs to your workspace, so every member sees it. Owners, admins and members can delete one; viewers can only look.

The region stores snapshots on more than one machine, so a snapshot may be listed as partial while part of the region does not answer. The rest are still shown. AerolVM reports no snapshot size, so snapshots are not charged.

## Related

- [Sandbox API keys](api-keys.md): what the SDK authenticates with.
- [Usage and limits](../usage/usage-and-limits.md): how sandboxes count.
