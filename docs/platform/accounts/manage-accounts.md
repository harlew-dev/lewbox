---
title: Manage accounts
description: Create accounts, choose who can change things, and disable or remove people.
---

Every person who uses the orchestrator has an account: an email and a password. There are two kinds:

| | Can |
|---|---|
| **Instance admin** | Everything: clusters, nodes, accounts |
| **Regular account** | List and view clusters and nodes; view and change their own account |

Organizations and finer roles come later.

## Create an account

As an instance admin:

```bash
lewbox user create alice@example.com                    # regular account
lewbox user create bob@example.com --instance-admin     # admin
```

It asks for the new password twice (12–256 characters). In scripts, pass it on stdin with `--password-stdin`. The first admin is created [on the orchestrator's machine](../setup/run-the-orchestrator.md#create-the-first-admin).

## See accounts

```bash
lewbox user list
lewbox user get alice@example.com
```

```text
EMAIL              ADMIN  DISABLED  CREATED
alice@example.com  no     no        2h ago
bob@example.com    yes    no        5m ago
```

Commands that take an account accept its email or its ID.

## Change a password

```bash
lewbox user set-password alice@example.com     # admins, for anyone
lewbox user set-password you@example.com       # anyone, for themselves
```

This ends the account's other sessions, so a leaked password stops working everywhere at once. Email-based resets come with email support.

## Disable or remove

```bash
lewbox user disable alice@example.com     # blocks sign-in and ends every session
lewbox user enable alice@example.com
lewbox user remove alice@example.com      # deletes the account and its sessions
```

`disable` and `remove` ask you to confirm (`--yes` skips the question). Lewbox never lets you disable or remove the last enabled instance admin, so you can't lock yourself out.

## Next

- [Sign in](sign-in.md)
- [Security](../reference/security.md): how passwords and sessions are stored.
