---
title: Usage and limits
description: The usage Lewbox meters, your plan's limits, and what happens at a limit.
---

Your workspace runs in one region with one plan. Usage is metered for the workspace; the plan sets what it may use there.

## Usage

Open **Usage** in the sidebar, or read it from the API, to see the last 7, 30 or 90 days, one bucket per day. Every bucket is present, with zeros where nothing ran, so the chart has a continuous axis. The totals add up:

- **Sandbox-hours**: one sandbox running for one hour.
- **vCPU-hours**: the vCPUs a sandbox reserves, multiplied by its running time.
- **GiB-hours**: its memory in GiB, multiplied by its running time.

Lewbox records usage in hourly buckets and samples every 15 seconds. It charges each running sandbox for the time since the previous sample, so accuracy is within one sample. Time while Lewbox itself was down is not charged.

Stopped sandboxes still count against your plan's sandbox limit, as the region counts them.

## Your plan and its limits

**Plan & Billing** shows the workspace's region, its plan and the plan's limits:

- maximum sandboxes,
- maximum vCPUs,
- maximum memory.

A limit may be unlimited. Limits are per workspace and per region, with no shared pool between workspaces. The page is informational: it does not take payment. Your provider sets the plan and any change to it.

**Home** shows live use against each limit, so you can see how close a workspace is before creating another sandbox.

## At a limit

When a sandbox would exceed a limit, the region refuses the create request with `429`. Delete or stop sandboxes to free a sandbox slot; stopping keeps the disk but frees nothing against the sandbox limit until you delete the sandbox.

If the workspace is pending or suspended, creating sandboxes is refused as well. Ask your provider to approve or restore it.

## Also see

- [Sandboxes and snapshots](../sandboxes/sandboxes-and-snapshots.md): free capacity by deleting what you no longer use.
- [Management API](../reference/management-api.md): read usage and limits over HTTP.
