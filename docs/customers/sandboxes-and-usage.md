---
title: Sandboxes and usage
description: See the sandboxes running in your regions, stop or delete them, and read your hourly usage.
---

Lewbox mirrors every sandbox on every cluster. The portal and the API show the sandboxes that belong to your workspace, in every region, with their state and size. The cluster is still the source of truth: Lewbox polls it every 15 seconds.

## List sandboxes

```bash
lewbox sandbox list
lewbox sandbox list --region eu-west --status started
```

The list includes sandboxes that are creating, started, stopped or in error. A sandbox that has been deleted is gone from the list. Stopped sandboxes still count against your plan, exactly as AerolVM counts them.

Owners and admins see every member's sandboxes in the workspace; members and viewers see only their own. An owner or admin can filter with `--member dev@acme.dev`. For an instance admin, `lewbox sandbox list --all` shows the whole fleet and each sandbox's organization.

## Stop and delete

```bash
lewbox sandbox stop sb-0f3c…
lewbox sandbox delete sb-0f3c… --yes
```

Stopping frees the compute but keeps the sandbox and its disk. Deleting removes it. Both run on the cluster first and then update Lewbox; if the cluster cannot be reached, nothing is claimed to have happened. Add `--region eu-west` when the same sandbox ID exists in more than one region.

A sandbox belongs to one workspace member: anyone outside that workspace, and another member, gets "not found", never a peek at it. Owners and admins can stop any sandbox in their workspace.

## Usage

```bash
lewbox usage
lewbox usage --from 2026-11-01 --to 2026-11-30
lewbox usage --from 2026-11-01 --hourly
lewbox usage --member dev@acme.dev     # owners and admins
```

The default range is the last 30 days, in one bucket per day. Every bucket is present, with zeros where nothing ran, so charts have a continuous axis. The totals add up sandbox-hours, vCPU-hours and GiB-hours.

Members see only their own usage; owners and admins see the workspace total and a per-member breakdown in each bucket. Lewbox records usage in hourly buckets per member and region. It samples every 15 seconds and charges each running sandbox for the time since the previous sample, so accuracy is within one sample. Time while Lewbox itself was down is not charged.

## Limits

```bash
lewbox limits
```

This shows each region your workspace is subscribed to: the plan and its limits (with any override), the workspace's live use, and a row per member with their share, live use and whether they are blocked. Limits are per region and split between members; there is no shared pool, and a member cannot use another member's share. A member sees only their own row. The same table is on the workspace's Team page in the portal.
