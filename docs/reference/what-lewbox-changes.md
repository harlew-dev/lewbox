---
title: What Lewbox changes on a machine
description: Every file, service and privileged command that lewbox install, node add and update touch on a node.
---

Lewbox runs privileged steps one at a time through `sudo`, and prints each one before running it. Secrets in those lines and in the installer's output are replaced with `******`. `lewbox install --dry-run` prints the steps without running them.

## `lewbox install`

1. **Checks**, changing nothing: Linux, `sudo` works, the ports are free, `/dev/kvm` for Firecracker, a DNS token for domain mode.
2. **Gets AerolVM's installer** for the chosen version from `github.com/aerol-ai/microvm/releases` (or `--from`). It checks the SHA-256 against the value built into Lewbox (for v0.7.0) or the release's `checksums.txt`, saves the verified copy to `~/.cache/lewbox/aerolvm/<tag>/install.sh`, and runs **that copy**. Lewbox never pipes a download into `sudo bash`.
3. **Runs it**: `sudo bash install.sh --version <tag> --pat-scope host --node-name <name> <mode flags> --no-start`. Local mode passes `--local` instead of `--pat-scope host` and `--no-start`: AerolVM starts right away, listening on `127.0.0.1` only. The other modes leave it stopped for `node add`.
4. **Saves** `~/.config/lewbox/node.json`: the version, mode, runtime and name. No secrets.

AerolVM's installer owns what it installs:

- binaries `sandboxd`, `toolboxd` and `aerolvm` in `/usr/local/bin`;
- `sandboxd.service`, `sandboxd-healthcheck.timer`, the egress gateway, and Caddy;
- settings in `/etc/sandboxd/` (`sandboxd.env` holds secrets and is root-only; `node.json` holds the rest);
- logs: `journalctl -u sandboxd`.

Running the installer again is safe: it keeps its saved settings and the node's secrets.

## `lewbox node add`

- **New cluster:** `sudo bash install.sh … init --role <role> --node-id <name> [--advertise <ip>]` starts AerolVM as the first node of a cluster.
- **Join:** `sudo bash install.sh … join <url> --token ****** --ca-cert-hash <hash> --node-id <name> --role <role>` joins, after Lewbox checks that port 7003 on the cluster is reachable.
- Then Lewbox applies [its settings](#lewboxs-settings).
- **New cluster only:** Lewbox reads the node's operator token (`SB_PAT_TOKEN`) with `sudo cat /etc/sandboxd/sandboxd.env`. It uses that token once, over `127.0.0.1`, to create an admin token named `lewbox-<slug>` for the orchestrator. The operator token never leaves the machine and is never stored by Lewbox.

## Lewbox's settings

Lewbox needs two AerolVM settings on every node: identity mode, so tokens work, and long key lifetimes. It writes them to **its own files**, never to the installer's, so re-running or upgrading AerolVM can't undo them:

| File | Contents |
|---|---|
| `/etc/sandboxd/lewbox.env` (root, `0600`) | `SB_IDENTITY_ENABLED=true`<br>`SB_TOKEN_MAX_TTL=876000h` |
| `/etc/systemd/system/sandboxd.service.d/lewbox.conf` | `[Service]`<br>`EnvironmentFile=/etc/sandboxd/lewbox.env` |

Then `sudo systemctl daemon-reload` and `sudo systemctl restart sandboxd`, but only if something changed. Then Lewbox waits for `/health`.

## `lewbox update --aerolvm`

Re-runs the verified installer for the new version, with no `init` or `join`: `sudo bash install.sh --version <tag>`. AerolVM swaps its binaries in place, keeping the old ones as `*.prev` and a database backup. Lewbox re-applies its settings and checks `/health` for the new version.

## Removing AerolVM

`lewbox node remove` takes a node out of its cluster but leaves AerolVM installed. AerolVM's releases include an `uninstall.sh` for removing it from the machine.
