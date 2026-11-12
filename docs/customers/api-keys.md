---
title: Sandbox API keys
description: Create a key for a region and point the AerolVM or E2B SDK at your sandboxes.
---

A sandbox API key is a token (`avm_…`) for one region. It belongs to one workspace member and works in that member's AerolVM team on the cluster: it can create and manage that member's sandboxes, and nothing else. A member's key cannot see another member's sandboxes.

## Create a key

```bash
lewbox region list
lewbox key create laptop --region eu-west
```

`--region` is optional when your workspace is subscribed to exactly one ready region. The regions you can use come from your provider: a key is refused in a region where your workspace has no plan. The key lasts 90 days unless you pass `--expires-in 30d` or `--never-expires`.

An instance admin or a workspace API key must name the member the key belongs to:

```bash
lewbox key create build --region eu-west --member dev@acme.dev --org acme
```

Otherwise a member creates a key for themselves. A key is refused (`412`) in a region where that member's share of the plan is 0; ask the workspace owner for a share. A `viewer` never has a share and cannot create keys.

`create` prints the token once, with the settings to paste:

```bash
SB_API_URL=https://eu-west.api.acme.dev
SB_API_TOKEN=avm_…
```

In a `docker` region it also prints the E2B settings:

```bash
E2B_API_URL=https://eu-west.api.acme.dev/e2b
E2B_SANDBOX_URL=https://eu-west.api.acme.dev/e2b/runtime
E2B_API_KEY=avm_…
```

Over HTTP, send `POST /v0/sandbox-keys` with `{"name": "laptop", "region": "eu-west"}` (and `"member"` when an instance admin or API key creates it). Lewbox keeps only the last four characters of the token, so save it when it is shown.

## List and revoke keys

```bash
lewbox key list
lewbox key revoke laptop
```

`list` shows live keys first, then revoked ones, with the member each belongs to. Revoking stops the token at once on servers and within a few seconds on workers. A member can revoke only the keys they created; owners and admins can revoke any key in the workspace.

Keys are refused while your workspace is waiting for approval or suspended, and in a region where it has no plan (or that is not ready). Ask your provider to assign a plan in the region you want.
