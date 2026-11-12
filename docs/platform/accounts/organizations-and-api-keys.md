---
title: Organizations and API keys
description: Group accounts into organizations with roles, and give an organization's backend an API key.
---

An **organization** is one business using Lewbox. It has **members** (Lewbox accounts, each with a role) and **API keys** (`lba_…`) that its own backend uses to call the Lewbox API. Instance admins create organizations; their owners and admins run them.

You can do everything here in the web console (customers: **Workspace settings** in the portal; instance admins: the workspace's page in the console) or with the CLI. The web app calls an organization a **workspace**.

## Create an organization

Instance admins only. The owner is an existing account and defaults to you.

```bash
lewbox org create acme --name "Acme Inc" --owner owner@acme.dev
lewbox org list
```

The slug (`acme`) names the organization in URLs and in `--org`. It can't change; the name can (`lewbox org update acme --name "Acme Corp"`).

## Choose the organization a command acts on

Member and API key commands act on one organization. Name it with `--org` (or `LEWBOX_ORG`), or make it the default for the current context:

```bash
lewbox org use acme
lewbox member list            # now means --org acme
```

Over HTTP, send its slug or ID in the `Lewbox-Organization` header. An API key always acts for its own organization and needs neither.

## Members and roles

| Role | Can |
|---|---|
| `owner` | Everything, including making and removing owners |
| `admin` | Add, change and remove members (except owners), create and revoke API keys, rename the organization |
| `member` | See the organization and its members; create sandbox keys and revoke the ones they created |
| `viewer` | The same, read-only |

Instance admins can do everything in every organization, whether or not they're members.

```bash
lewbox member add dev@acme.dev --role admin    # an existing account
lewbox member set-role dev@acme.dev member
lewbox member remove dev@acme.dev
```

Members are existing accounts: an instance admin creates the account first ([Manage accounts](manage-accounts.md)). Anyone can leave an organization. An organization always keeps at least one owner, so the last owner can't be demoted or removed.

Removing a member, changing their role to viewer, disabling their account or deleting their account revokes every sandbox key they created. If a key cannot be revoked, the change is refused with `502` and nothing changes, so you can try again.

## API keys

An API key lets the organization's own backend call the Lewbox API as the organization. Owners and admins create them:

```bash
lewbox admin-key create "Production backend" --expires-in 90d
lewbox admin-key create ci --never-expires
lewbox admin-key list
lewbox admin-key revoke ci
```

`create` prints the token once. Lewbox keeps only its SHA-256 hash and its first and last characters (`lba_3c34b3e9…xFCz`), so copy it straight away. Keys expire after 90 days unless you choose otherwise, and `list` shows when each was last used.

Use it as a bearer token, or sign the CLI in with it:

```bash
curl -H "Authorization: Bearer $LEWBOX_KEY" https://lewbox.example.com/v0/organization
printf '%s\n' "$LEWBOX_KEY" | lewbox login https://lewbox.example.com --key-stdin
```

A key is deliberately narrow. It can read its organization, its members and its keys. It can't change members or admin keys, and it can't reach anything outside its organization, such as clusters, nodes or accounts: those calls get `403`. It can list, create and revoke the organization's [sandbox keys](../../consumer/sandboxes/api-keys.md). Revoking a key, or suspending its organization, stops it at once.

## Suspend an organization

Instance admins can suspend an organization (`lewbox org suspend acme`, or **Suspend** on the customer's page in the console). Its API keys stop working, its members can only look and it cannot mint sandbox keys until it is restored with `lewbox org restore acme`. Nothing is deleted.

## Not yet

Inviting someone by email, and the audit log of member and key changes, come with email support. Until then, create the account and add it as a member.

## Next

- [Manage accounts](manage-accounts.md)
- [HTTP API](../reference/http-api.md#organizations)
