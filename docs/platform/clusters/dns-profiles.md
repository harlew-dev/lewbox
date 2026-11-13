---
title: DNS profiles
description: Save a Cloudflare API token once and test it, so a cluster in domain mode can get a certificate without the token being typed on every machine.
---

A **DNS profile** is a saved Cloudflare API token, its zone and an optional ACME account email. Lewbox tests it when you save it, seals the token with the instance keyring, and shows only its last four characters afterwards. A cluster in domain mode will name a profile when it starts, instead of the token being passed to every install.

Instance admins only. The token is fetched back only by `lewbox dns-profile credentials`, which exists to configure a node.

## The Cloudflare token

Create an API token in the Cloudflare dashboard with:

- **Zone → Zone → Read**, on the one zone the cluster uses;
- **Zone → DNS → Edit**, on that same zone.

Copy the token once. Do not use the global API key, and do not give the token access to every zone in the account: it ends up on the cluster's nodes.

## Commands

```bash
printf '%s' "$CF_TOKEN" | lewbox dns-profile create cf-eu \
  --zone example.com --api-token-stdin --acme-email ops@example.com

lewbox dns-profile list
lewbox dns-profile show cf-eu
lewbox dns-profile test cf-eu
printf '%s' "$NEW_TOKEN" | lewbox dns-profile update cf-eu --api-token-stdin
lewbox dns-profile delete cf-eu --yes
```

`create` and `update` read the token from stdin, never from a flag. `update` changes only the flags you pass; a new zone or token is tested before it is saved and bumps the profile's `version`.

The `--acme-email` is optional. It is the address Let's Encrypt uses for expiry notices.

## The test

Saving a profile, changing its zone or token, and `lewbox dns-profile test` all run the same test with a 30-second timeout:

1. find the zone with the token;
2. create the TXT record `_lewbox-test.<zone>`, TTL 60;
3. delete that record.

The record is removed again, so a test leaves nothing behind. The step that failed comes back as `details.step`:

| Step | The message means | Fix |
|---|---|---|
| `zone` | `the token was refused` | Create a token with **Zone:Read**, and check you copied all of it |
| `zone` | `zone <zone> not found with this token` | Check the zone's spelling, and that the token is scoped to it |
| `write` | `the token can read the zone but not edit its records` | Add **DNS:Edit** on the same zone |

A failing test on `create` or a changed `update` **saves nothing**: the command exits with a precondition error. A failing `dns-profile test` on a saved profile records the message in `test_error` and is not an error response.

`list` shows when each profile last passed: `ok`, `failed: <message>`, or `never`.

## Deleting

A profile a cluster uses cannot be deleted:

```text
DNS profile "cf-eu" is used by 1 cluster(s); stop using it in those clusters first
```

Point the cluster at another profile first, then delete this one.

## Next

Domain-mode clusters choose a profile when they start. Until that wiring is in place, a profile is only stored and tested.
