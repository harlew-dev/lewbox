---
title: What Lewbox changes on a machine
description: Every file, service and privileged command that lewbox install, node add, update and firewall enable touch on a node.
---

Lewbox runs privileged steps one at a time through `sudo`, and prints each one before running it. Secrets in those lines and in the installer's output are replaced with `******`. `lewbox install --dry-run` prints the steps without running them.

## `lewbox install`

1. **Checks**, changing nothing: Linux, `sudo` works, the ports are free, `/dev/kvm` for Firecracker, and a DNS token for domain mode (from `--dns-profile` or stdin).
2. **Gets AerolVM's installer** for the chosen version from `github.com/aerol-ai/microvm/releases` (or `--from`). It checks the SHA-256 against the value built into Lewbox (for v0.7.0) or the release's `checksums.txt`, saves the verified copy to `~/.cache/lewbox/aerolvm/<tag>/install.sh`, and runs **that copy**. Lewbox never pipes a download into `sudo bash`.
3. **Runs it**: `sudo bash install.sh --version <tag> --pat-scope host --node-name <name> <mode flags> --no-start`. Local mode passes `--local` instead of `--pat-scope host` and `--no-start`: AerolVM starts right away, listening on `127.0.0.1` only. The other modes leave it stopped for `node add`.
4. **Saves** `~/.config/lewbox/node.json`: the version, mode, runtime, name, and the name and version of the DNS profile it used. No secrets.

AerolVM's installer owns what it installs:

- binaries `sandboxd`, `toolboxd` and `aerolvm` in `/usr/local/bin`;
- `sandboxd.service`, `sandboxd-healthcheck.timer`, the egress gateway, and Caddy;
- settings in `/etc/sandboxd/` (`sandboxd.env` holds secrets and is root-only; `node.json` holds the rest);
- logs: `journalctl -u sandboxd`.

Running the installer again is safe: it keeps its saved settings and the node's secrets.

## `lewbox node add`

- **New cluster:** `sudo bash install.sh … init --role <role> --node-id <name> [--advertise <ip>]` starts AerolVM as the first node of a cluster.
- **Join:** `sudo bash install.sh … join <url> --token ****** --ca-cert-hash <hash> --node-id <name> --role <role>` joins, after Lewbox checks that port 7003 on the cluster is reachable.
- Then Lewbox applies [its settings](#lewboxs-settings), and the cluster's [node settings](#the-snapshot-registry): the snapshot registry, its cleanup timer, and a changed DNS profile.
- **New cluster only:** Lewbox reads the node's operator token (`SB_PAT_TOKEN`) with `sudo cat /etc/sandboxd/sandboxd.env`. It uses that token once, over `127.0.0.1`, to create an admin token named `lewbox-<slug>` for the orchestrator. The operator token never leaves the machine and is never stored by Lewbox.

`lewbox node configure` runs the same node settings on a node that is already in a cluster.

## Lewbox's settings

Lewbox needs two AerolVM settings on every node: identity mode, so tokens work, and long key lifetimes. It writes them to **its own files**, never to the installer's, so re-running or upgrading AerolVM can't undo them:

| File | Contents |
|---|---|
| `/etc/sandboxd/lewbox.env` (root, `0600`) | `SB_IDENTITY_ENABLED=true`<br>`SB_TOKEN_MAX_TTL=876000h`<br>On a cluster with storage: `SB_SNAPSHOT_PUSH_ENABLED=true`, `SB_MIRROR_PUSH_HOST=127.0.0.1:5000`, `SB_IMAGE_DISTRIBUTION_AOCR_HOST=127.0.0.1:5000`, `SB_AUTO_IMPORT_CLUSTER_ID=<cluster id>`, `SB_AUTO_IMPORT_CLUSTER_PAT_PATH=/etc/sandboxd/lewbox-registry.token`, `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s` |
| `/etc/systemd/system/sandboxd.service.d/lewbox.conf` | `[Service]`<br>`EnvironmentFile=/etc/sandboxd/lewbox.env` |

Then `sudo systemctl daemon-reload` and `sudo systemctl restart sandboxd`, but only if something changed. Then Lewbox waits for `/health`.

## The snapshot registry

On a cluster with a storage profile, `lewbox node add` and `lewbox node configure` also install a registry that AerolVM pushes snapshots to, and a timer that removes the local copies later. Every file is Lewbox's own:

| File | Contents |
|---|---|
| `/usr/local/bin/lewbox-registry` | CNCF Distribution, pinned version, SHA-256 checked before it is installed |
| `/etc/lewbox-registry/config.yml` (root, `0640`, group `lewbox-registry`) | The bucket, endpoint, region, path style, access key and the cluster's root directory `<prefix>/clusters/<cluster id>/` |
| `/etc/systemd/system/lewbox-registry.service` | Runs the registry as the `lewbox-registry` user, listening on `127.0.0.1:5000` only |
| `/etc/sandboxd/lewbox-registry.token` (root, `0600`) | Random bytes, never the operator token: the registry has no auth, and AerolVM needs a password to send |
| `/usr/local/lib/lewbox/lewbox` (root, `0755`) | A root-owned copy of the running binary, so the timer never runs a user's file |
| `/etc/systemd/system/lewbox-archive-cleanup.service` | Runs `lewbox node cleanup --json` as root, with `--older-than` when the cluster's `archive_local_retention` is `1h`, `1d` or `7d` |
| `/etc/systemd/system/lewbox-archive-cleanup.timer` | Runs that service every five minutes |

`lewbox-registry` is enabled and restarted when anything changed, then Lewbox waits for `http://127.0.0.1:5000/v2/` to answer. The cleanup timer is enabled with `sudo systemctl enable --now lewbox-archive-cleanup.timer`. When the cluster's `archive_local_retention` is `never`, the registry stays but the cleanup service, timer and binary copy are removed instead.

When the cluster no longer has a storage profile, the same commands stop and remove the registry, its config and token, and the cleanup service, timer and binary copy. The push lines leave `/etc/sandboxd/lewbox.env`, and sandboxd restarts.

A changed DNS profile re-runs the verified installer with `sudo bash install.sh --version <installed> --dns-provider cloudflare --dns-api-token ******` and no `init` or `join`; the installer keeps its other saved options.

## `lewbox update --aerolvm`

Re-runs the verified installer for the new version, with no `init` or `join`: `sudo bash install.sh --version <tag>`. AerolVM swaps its binaries in place, keeping the old ones as `*.prev` and a database backup. Lewbox re-applies its settings and checks `/health` for the new version.

## `lewbox firewall enable`

Reads, changing nothing: `sudo ufw version`, `sudo sshd -T` (the SSH port), `sudo iptables-save -t filter` (AerolVM's sandbox rules), `sudo cat /etc/ufw/user.rules` and `user6.rules` (the current rules), and the cluster's members over `127.0.0.1` with the operator token. Then:

1. `sudo ufw --force reset`, only if you chose to reset;
2. `sudo ufw default deny incoming`, and after a reset `sudo ufw default allow outgoing`;
3. one `sudo ufw allow proto <tcp|udp> from <any|network> to any port <port> comment lewbox:<purpose>` per rule;
4. `sudo ufw --force enable`.

See [Turn on the firewall](../operations/firewall.md) for the rules themselves.

## Removing AerolVM

`lewbox node remove` takes a node out of its cluster but leaves AerolVM installed. AerolVM's releases include an `uninstall.sh` for removing it from the machine.
