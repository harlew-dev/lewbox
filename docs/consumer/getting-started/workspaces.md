---
title: Workspaces and members
description: Switch workspaces, understand the four roles, and change workspace settings.
---

A workspace is shared by its members and has exactly one region. Everyone in it sees the same sandboxes, snapshots, keys and usage.

## Switch workspaces

The menu at the top of the sidebar lists every workspace you belong to, with a check on the current one. Pick another to switch; the portal remembers your choice in this browser.

The same menu has **New workspace…** and, for instance admins, **Operator console**. A signed-in account with no workspace is sent to onboarding to create one.

## Roles

Four roles decide what a member can do. Your role is per workspace, so you can be an owner of one and a viewer of another.

| Role | Can |
|---|---|
| **Owner** | Everything an admin can, plus make and unmake owners. Every workspace keeps at least one |
| **Admin** | Change the workspace, add and remove members, create management API keys, and do everything a member can |
| **Member** | Create and revoke sandbox API keys, stop and delete sandboxes, delete snapshots |
| **Viewer** | Read the workspace, its members, sandboxes and snapshots. Cannot create keys or change sandboxes |

Owners and admins can change things only while the workspace is active. Only an owner can grant or take away the owner role. Members can remove themselves from a workspace; owners and admins can remove anyone.

## Workspace settings

Open **Workspace settings** from the sidebar header. It has three tabs:

- **General**: the workspace's name, its region and plan, and its ID.
- **Members**: the people in the workspace and their roles. Add a member by the email of their account; remove someone with the row menu.
- **Management API keys** (owners and admins): `lba_` keys for your own backend to call the [management API](../reference/management-api.md). These are not sandbox keys; those are under **API keys** in the sidebar.

Your provider decides the workspace's region: the one in your setup link, or the provider's default. There is no region picker in the portal, and every sandbox key works in that region.

## Pending and suspended workspaces

A workspace is `active`, `pending` or `suspended`.

- **Pending** means your provider has not approved it yet. Members can sign in and read it, but cannot create sandbox keys.
- **Suspended** means it is blocked. Members can still read the workspace, but cannot create keys or change anything, and its running sandboxes are stopped on the next sync pass. Ask your provider to restore it.

Stopped sandboxes keep counting against your limits until you delete them.
