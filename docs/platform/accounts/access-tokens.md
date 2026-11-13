---
title: Access tokens
description: Long-lived tokens that act as your account, for scripts, CI and automating clusters and nodes.
---

An access token (`lbp_…`) lets a script or CI job use the Lewbox CLI and API as you, without your password. It has your account's rights: an instance admin's token can add clusters and nodes, manage accounts and run every other operator command.

This differs from an organization's [API key](organizations-and-api-keys.md) (`lba_…`). An API key acts for one workspace and can never reach clusters, nodes or accounts.

## Create a token

Sign in with your password first, then:

```bash
lewbox token create ci --expires-in 90d
lewbox token create deploy --never-expires
```

`create` prints the token once. Lewbox keeps only its SHA-256 hash and its first and last characters (`lbp_3c34b3e9…xFCz`), so copy it straight away. Tokens expire after 90 days unless you choose otherwise. `--expires-in` takes `30d`, `720h` and so on, from 1 hour to 3650 days.

In the web console, open **Account settings** from the account menu. Then choose **Create token** under **Access tokens**.

A token can't create another token, so a leaked token can't outlive its own expiry. Create tokens with a password sign-in, or in the console.

## Use a token

Sign the CLI in with it. On its own, `--token` asks for the token without echo, or reads it from stdin:

```bash
lewbox login https://lewbox.example.com --token
printf '%s\n' "$LEWBOX_ACCESS_TOKEN" | lewbox login https://lewbox.example.com --token
```

This saves a context, like a password sign-in. Every later command uses it, including `lewbox node add`, `lewbox install` and `lewbox cluster list`. `--token=lbp_…` also works, but the token stays in your shell history, so the CLI warns you. `--token lbp_…` with a space doesn't work: the CLI refuses, because the token would be read as the URL.

For a single CI job, skip `login` and set two variables:

```bash
export LEWBOX_API_URL=https://lewbox.example.com
export LEWBOX_TOKEN=lbp_…
lewbox cluster list
```

Or call the API with it as a bearer token:

```bash
curl -H "Authorization: Bearer $LEWBOX_TOKEN" https://lewbox.example.com/v0/clusters
```

`lewbox whoami` shows `via: access_token` and the token's name. `lewbox logout` forgets the saved token on this machine. The token keeps working until it expires or you revoke it.

## List and revoke

```bash
lewbox token list
lewbox token revoke ci
```

`list` shows every token, revoked ones included, with when it was last used. `revoke` asks to confirm, and the token stops working at once. A token may revoke itself.

## Other accounts' tokens

Instance admins can manage any account's tokens with `--user`:

```bash
lewbox token list --user alice@example.com
lewbox token revoke ci --user alice@example.com
```

In local mode, on the machine that runs `lewbox serve`, there is no signed-in account, so `--user` is required. That's how you mint the first token for an automation account without signing in:

```bash
lewbox --context local token create ci --user ops-bot@example.com
```

## What ends a token

- It expires, or someone revokes it.
- Its account is disabled. Disabling revokes every token for good, so enabling the account again doesn't bring them back.
- Its account is removed.
- Its account stops being an instance admin. The token then has only a regular account's rights, from the next request.

Changing the account's password doesn't revoke its tokens. Revoke them yourself if you think one has leaked.

## A dedicated account for automation

For CI that manages clusters, create an account just for it rather than using your own:

```bash
lewbox user create ops-bot@example.com --instance-admin
lewbox --context local token create ci --user ops-bot@example.com --expires-in 90d
```

Its tokens are then easy to find and revoke. Disabling the account stops everything it can do at once.

## Next

- [Sign in](sign-in.md)
- [Manage accounts](manage-accounts.md)
- [CLI reference](../reference/cli.md)
