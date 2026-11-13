---
title: Archived sandboxes
description: On some regions a stopped sandbox is archived to storage; restore it as a new sandbox when you need it again.
---

Some regions are set to **archive on stop**. There, a stopped sandbox is snapshotted to the region's storage and removed from the server, so it no longer takes up disk on the machine that ran it. This makes stopped sandboxes cheap to keep without filling the region's hosts.

A region's [region list](../reference/management-api.md) shows its `stop_policy`: `keep` means stopped sandboxes stay where they are, and `archive` means they are archived when they stop.

## Where archived sandboxes appear

An archived sandbox shows under **Sandboxes → Archived** in the portal, and with the CLI:

```bash
lewbox archive list
```

The list shows the original sandbox name, its region, when it was archived, its size and whether it has been restored before. Archived sandboxes no longer count as stopped sandboxes on the region and do not take up a sandbox slot there.

## Restoring

Choose **Restore** on an archived sandbox, or run:

```bash
lewbox archive restore <id>
```

Restoring creates a **new** sandbox:

- its ID is new; the old ID is gone;
- the name, tags, runtime, vCPU, memory and disk size are the archived sandbox's;
- the tag `lewbox.restored_from` is set to the old sandbox ID, so you can link the two;
- if the old name is taken, the new sandbox gets a short suffix.

The new sandbox starts as usual and runs in the same region. The archive is kept, so you can restore it again later.

Environment variables are **not** kept: a snapshot records the disk, not what you passed at start. Pass them again when you create the sandbox. Running programs end when a sandbox stops, archived or not; they are not running in the archive.

## Restoring with the SDK

The portal shows the archive's **registry reference**; `lewbox archive show <id>` prints it too. You can restore with either SDK by creating a sandbox whose image is that reference:

```ts
const sandbox = await sb.create({ image: "<registry ref>" })
```

Add the tags and size you need, and pass your environment variables again. The region keeps the reference scoped to your workspace, so only your team can restore it.

## E2B pause and connect

E2B's `pause()` followed by `connect(id)` does not work on an archive region: restoring creates a new sandbox with a new ID, so the old ID no longer exists. Use the archive list (or the registry reference) to bring the sandbox back instead.

## Keeping and deleting archives

Archives are kept until you delete them; there is no automatic retention. Delete one when you no longer need it:

```bash
lewbox archive delete <id>
```

Deleting an archive removes its snapshot from the region and its data from the bucket. It does not touch sandboxes you restored from it: those are separate sandboxes with their own disks.

Owners, admins and members can restore and delete archived sandboxes; viewers can only list and show them.

## Related

- [Sandboxes and snapshots](sandboxes-and-snapshots.md): stopping, deleting and snapshots on a region that keeps stopped sandboxes.
- [Management API](../reference/management-api.md): the `archive` routes and the `stop_policy` field.
