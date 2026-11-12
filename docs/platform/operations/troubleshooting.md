---
title: Troubleshooting
description: Common errors from lewbox install, node add and cluster checks, and how to fix them.
---

Start here:

```bash
lewbox status                   # which orchestrator, healthy, signed in
lewbox cluster check <slug>     # every check, and how to fix each failure
sudo journalctl -u sandboxd -e  # AerolVM's own log, on a node
```

Every error Lewbox prints says what failed. Add `--json` to get the machine-readable code ([Output and errors](../reference/output-and-errors.md)).

## Signing in

**`sign in first: lewbox login`**: you're in remote mode without a session. Run `lewbox login <url>`, or `lewbox context use local` on the orchestrator's own machine.

**`session expired or invalid`**: the session ran out (30 days), was ended by a password change, or the account was disabled. Sign in again.

**`only an instance admin can do this`**: your account can view but not change things. Ask an admin, or see [Manage accounts](../accounts/manage-accounts.md).

## Installing

**`running as root is not supported`**: run `lewbox` as your own user. It uses `sudo` for the steps that need root.

**``sudo -n true` failed``**: outside a terminal, `sudo` can't prompt for a password. Run `sudo -v` in a terminal first, or allow passwordless `sudo` for this user.

**`ports … are in use`**: something else listens on 21212, 443 or 7000–7003. Ports held by this machine's own `sandboxd` (on a re-run) are fine.

**`macOS isn't supported by lewbox install yet`**: use a Linux machine or VM for nodes. Only nodes need Linux and systemd.

**`install.sh … sha256 is …, want …`**: the downloaded installer doesn't match the hash this Lewbox release expects. Don't work around it: check your network, proxy or mirror.

## Joining

**`a local-mode install can't join a cluster`**, **runtime mismatch**, **`cannot reach …:7003`**: see [Add nodes](../clusters/add-nodes.md#when-it-cant-join).

**The node joined but stays not alive**: on the new machine, check `systemctl status sandboxd` and that UDP 7001 is open between the nodes (gossip uses it).

## Cluster checks

**`admin_token` fails with 401**: identity mode is off on at least one node. Every node needs `SB_IDENTITY_ENABLED=true` ([Register an existing cluster](../clusters/register-an-existing-cluster.md#1-prepare-the-cluster)).

**`unreachable`**: the orchestrator can't reach the admin URL. Check `sandboxd` on that node, the URL, and the firewall between them.

**`token_ttl` fails**: set `SB_TOKEN_MAX_TTL=876000h` on every node, or keys you create will expire.

## Removing

**`node "x" serves cluster "y"'s admin URL`**: the orchestrator reaches the cluster through that node. Register the cluster through another node first, then remove it.

**AerolVM refuses to delete the node**: it still owns sandboxes. Stop them, or pass `--force`.

## Could not resolve 'archive.ubuntu.com' in the dev cluster

AerolVM's installer runs `apt-get`. The dev cluster's containers resolve outside names through your host's resolver, and some home routers fail on some names. Send their lookups elsewhere, then start again:

```bash
export LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8"
cd deploy/dev-cluster && ./dev.sh down && ./dev.sh up && ./dev.sh cluster
```

## Next

- [Health and checks](../clusters/health-and-checks.md)
- [Output and errors](../reference/output-and-errors.md)
