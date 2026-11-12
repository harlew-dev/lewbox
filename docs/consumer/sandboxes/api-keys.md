---
title: Sandbox API keys
description: Create a key for your workspace's region and point the AerolVM or E2B SDK at it.
---

A sandbox API key is a token (`avm_…`) for your workspace's region. It can create and manage sandboxes for your workspace there, and nothing else.

## Create a key

In the portal, open **API keys** and choose **Create API key**. Give it a name. The key works in your workspace's region; there is no region to pick.

The key lasts 90 days by default. Choose another lifetime in the dialog, or keep it until you revoke it. Members, admins and owners can create keys; viewers cannot.

Creating a key shows the token once, with the settings to paste:

```bash
export SB_API_URL=https://eu-west.api.example.com
export SB_API_TOKEN=avm_…
```

Lewbox stores only the last four characters, so save the token when it is shown. If you lose it, revoke the key and create another.

In a region that runs the Docker runtime, the key also works with E2B:

```bash
export E2B_API_URL=https://eu-west.api.example.com/e2b
export E2B_SANDBOX_URL=https://eu-west.api.example.com/e2b/runtime
export E2B_API_KEY=avm_…
```

Over HTTP, send `POST /v0/sandbox-keys` with `{"name": "laptop"}`. `region` is optional: it defaults to the workspace's region, and naming a different one is refused.

## List and revoke keys

**API keys** lists live keys first, then revoked ones. Revoking stops the token within a few seconds.

A member can revoke only the keys they created; owners and admins can revoke any key in the workspace. Over HTTP, send `DELETE /v0/sandbox-keys/{id or name}`.

Keys are refused while your workspace is pending or suspended, and for a few seconds after your provider first sets up your region (try again).

## Next

- [AerolVM SDK](aerolvm-sdk.md) and [E2B SDK](e2b.md): use the key.
- [Management API](../reference/management-api.md): the routes behind the portal.
