---
title: Customer documentation
description: Run sandboxes in your workspace, with an API key and the AerolVM or E2B SDK.
---

A **workspace** is your team's place in Lewbox. It owns your sandboxes, snapshots, usage and API keys, and it runs in exactly one **region**, the location your provider picked for you. Everyone in the workspace shares that region and those sandboxes. Your provider runs the region and sets your plan; you never manage its machines.

Your apps talk to the region's API directly with a sandbox API key. Lewbox itself is not in the path of a sandbox request.

New here? Start with the **[Quickstart](getting-started/quickstart.md)**: create a workspace, get a key, and run your first sandbox.

## Get started

| | |
|---|---|
| [Quickstart](getting-started/quickstart.md) | Sign up or open a setup link, create a workspace, get a key, run a sandbox |
| [Workspaces and members](getting-started/workspaces.md) | Switching workspaces, roles, settings, and what a pending or suspended workspace means |

## Sandboxes

| | |
|---|---|
| [Sandbox API keys](sandboxes/api-keys.md) | Create a key for your workspace's region and revoke it |
| [AerolVM SDK](sandboxes/aerolvm-sdk.md) | Create, use and destroy sandboxes from TypeScript or Python |
| [E2B SDK](sandboxes/e2b.md) | Use the E2B SDK against the compatible endpoint |
| [Sandboxes and snapshots](sandboxes/sandboxes-and-snapshots.md) | See, stop and delete sandboxes, and manage snapshots |
| [Archived sandboxes](sandboxes/archived-sandboxes.md) | Restore a sandbox that stopped on an archive region |

## Usage

| | |
|---|---|
| [Usage and limits](usage/usage-and-limits.md) | The numbers Lewbox meters, your plan's limits, and what happens at a limit |

## Reference

| | |
|---|---|
| [Management API](reference/management-api.md) | `lba_` keys and the customer-facing `/v0` routes |

Running Lewbox for other people? That is the [platform book](../platform/README.md).
