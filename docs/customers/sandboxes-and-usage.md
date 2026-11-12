---
title: Sandboxes and usage
description: See the sandboxes running in your regions, stop or delete them, and read your hourly usage.
---

Lewbox mirrors every sandbox on every cluster. The portal and the API show the sandboxes that belong to your organization, in every region, with their state and size. The cluster is still the source of truth: Lewbox polls it every 15 seconds.

## List sandboxes

```bash
lewbox sandbox list
lewbox sandbox list --region eu-west --status started
```

The list includes sandboxes that are creating, started, stopped or in error. A sandbox that has been deleted is gone from the list. Stopped sandboxes still count against your plan, exactly as AerolVM counts them.

For an instance admin, `lewbox sandbox list --all` shows the whole fleet and each sandbox's organization.

## Stop and delete

```bash
lewbox sandbox stop sb-0f3c…
lewbox sandbox delete sb-0f3c… --yes
```

Stopping frees the compute but keeps the sandbox and its disk. Deleting removes it. Both run on the cluster first and then update Lewbox; if the cluster cannot be reached, nothing is claimed to have happened. Add `--region eu-west` when the same sandbox ID exists in more than one region.

A sandbox belongs to one organization: another organization's member gets "not found", never a peek at it.

## Snapshots

Snapshots you take with the SDK (`sandbox.snapshot()`) show up in the portal and in the CLI:

```bash
lewbox snapshot list
lewbox snapshot list --region eu-west
lewbox snapshot delete ckpt-1 --region eu-west --yes
```

A snapshot belongs to your workspace, so every member sees it. Lewbox asks each node of each region, since AerolVM stores snapshots per node; if a node does not answer, its name is listed as partial and the rest are shown. `--region` picks the region when the same name exists more than once. Deleting asks to confirm. AerolVM reports no snapshot size, so snapshots are not charged.

## Usage

```bash
lewbox usage
lewbox usage --from 2026-11-01 --to 2026-11-30
lewbox usage --from 2026-11-01 --hourly
```

The default range is the last 30 days, in one bucket per day. Every bucket is present, with zeros where nothing ran, so charts have a continuous axis. The totals add up sandbox-hours, vCPU-hours and GiB-hours.

Lewbox records usage in hourly buckets per organization and region. It samples every 15 seconds and charges each running sandbox for the time since the previous sample, so accuracy is within one sample. Time while Lewbox itself was down is not charged.

## Limits

```bash
lewbox limits
```

This shows each region your organization is subscribed to: the plan and its limits (with any override), live use, and the quota Lewbox has pushed to that region's AerolVM. Limits are per region; there is no shared pool. Your quota and use are also on the organization's page in the portal.
