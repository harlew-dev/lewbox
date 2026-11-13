---
title: DNS profiles
description: Save a Cloudflare API token once and test it, so a cluster in domain mode can get a certificate without the token being typed on every machine.
---

A **DNS profile** is a saved Cloudflare API token, its zone and an optional ACME account email. Lewbox tests it when you save it, seals the token with the instance keyring, and shows only its last four characters afterwards. `lewbox install --dns-profile` fetches it back to give a domain-mode node its certificate, and a cluster uses it to keep its domain's DNS records pointed at its nodes.

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

A profile a cluster uses, or one that still has DNS records from a cluster, cannot be deleted:

```text
DNS profile "cf-eu" is used by 1 cluster(s); stop using it in those clusters first
```

Point the cluster at another profile first, then delete this one. If the cluster no longer names the profile but its records are still there, run `lewbox cluster check` once (it removes them) and delete the profile again.

## The records Lewbox keeps

When a cluster has a domain and a DNS profile, registering it creates the records, and each check keeps the domain pointed at the cluster:

- For every **live `mixed` or `ingress` member with a public IP**, an `A` (IPv4) or `AAAA` (IPv6) record for `<domain>` and for `*.<domain>`, TTL 60, DNS only (never proxied), with the comment `lewbox <cluster> <node>`.
- A member's IP is its `public_host` when that is an IP (IP mode), otherwise its `data_plane_host`. In domain mode AerolVM reports the domain as `public_host` and the machine's IP as `data_plane_host` (verified on v0.7.0).
- A member whose IP is private, loopback or otherwise unusable gets no record, and the check names it (`node-1: 10.0.0.5 is a private address, no record`). A `worker` or `server` member gets none either.
- `data_plane_host` is the address the node advertises to the cluster: `node add --advertise` when given, otherwise the one AerolVM picks, usually the machine's first IP. On most VPSs that is the public IP. Behind a cloud provider's NAT (AWS, GCP) it is private, so the node gets no record: set `dns_records` to `off` and point the domain at the machine or your load balancer yourself.
- A new domain has no records until the cluster is registered, so `lewbox node add … --new-cluster` sends the node's IP along, and the orchestrator connects to that IP for the registration checks when the domain doesn't resolve yet (still verifying the certificate for the domain). Nothing has looked the domain up before its records exist, so no resolver caches a "no such host" answer.
- Lewbox records each Cloudflare record's ID. When a member leaves, stops being ingress or changes IP, its records are deleted; records that are gone from Cloudflare already count as deleted. **Lewbox never touches another record in the zone.**
- An identical record that already exists is adopted rather than duplicated.

The cluster's `dns_records` setting controls this:

| Value | |
|---|---|
| `auto` | The default. Keep the records as above |
| `off` | Keep none, and delete the ones Lewbox made. Use it when the domain points at a load balancer in front of the nodes |

Set it with `lewbox cluster set <cluster> --dns-records off` (and `auto` to turn it back on). The check runs the reconciliation, so a Cloudflare failure shows as a failed `dns records` check and a degraded cluster.

Removing the cluster deletes the records first, best effort: Cloudflare never blocks a removal.

## Next

- [Start a cluster](start-a-cluster.md): `lewbox install --dns-profile`.
- [Health and checks](health-and-checks.md): what the `dns records` check means.
