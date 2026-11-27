---
title: Update
description: Update the lewbox CLI, and the AerolVM version on a machine, without surprises.
---

Lewbox never updates itself silently: it installs system software with `sudo`, so you decide when.

## See what's available

```bash
lewbox update --check
```

It shows your version, the latest release, the AerolVM version on this machine, and the version that release would move it to. Nothing changes.

## Update the CLI

```bash
lewbox update                     # the latest release
lewbox update --version v0.2.0    # a specific one
```

Lewbox downloads the binary for this OS and architecture, checks it against the release's `checksums.txt`, and replaces itself in place. If its folder isn't writable by you (for example `/usr/local/bin`), it stops and prints the exact `sudo install …` command to run instead.

> [!NOTE]
> Releases are read from `LEWBOX_RELEASE_URL`, by default the [GitHub releases](https://github.com/harlew-dev/lewbox/releases) of `harlew-dev/lewbox`. To test with your own build, point `--release-from` at a folder holding `release.json`, `checksums.txt` and `lewbox_<os>_<arch>`.

## Update AerolVM on this machine

```bash
lewbox update --aerolvm
```

This updates the CLI first, then runs the **new** binary to update AerolVM. That way the AerolVM version and installer checksum come from the release that was tested with them. AerolVM moves to the version that Lewbox release was tested with, not simply AerolVM's newest.

Lewbox re-runs AerolVM's pinned installer on this machine. AerolVM keeps the old binaries and a database backup, and puts them back if the new version doesn't come up. Lewbox's own settings live in their own files, so they survive the upgrade. Lewbox then checks that `/health` reports the new version.

| Flag | |
|---|---|
| `--aerolvm-version v0.7.3` | Move to a specific AerolVM version. Lewbox warns that it's untested |
| `--aerolvm-version latest` | AerolVM's newest release, with the same warning |
| `--from <dir>` | Take AerolVM's files from a local mirror instead of GitHub |

> [!NOTE]
> `update --aerolvm` refuses on a machine that is one of **several** nodes in a cluster. Upgrading one Raft member alone leaves the cluster on mixed versions. Use [`lewbox cluster upgrade`](#update-every-node-of-a-cluster) instead. Lewbox also refuses if it can't tell how many nodes there are.

## Update every node of a cluster

```bash
lewbox cluster upgrade eu-west --check
lewbox cluster upgrade eu-west --version v0.7.1 --allow-downtime --yes
```

`cluster upgrade` uses AerolVM's own rolling upgrade: it fetches the plan, shows it, and after you confirm it starts one node at a time. Running sandboxes keep running; each node's API is down for a few seconds. Lewbox's files (`/etc/sandboxd/lewbox.env`, the create policy and the systemd drop-in) are left in place.

The default version is the AerolVM release this Lewbox release was tested with. Another release tag works, with an "untested" warning. `latest` is not accepted. Before sending anything to the cluster, Lewbox reads the release's `checksums.txt` and `release.json` and checks `release.json` against its checksum; the nodes then check every file against those hashes before installing it. Releases are read from `LEWBOX_AEROLVM_RELEASE_URL` (default: AerolVM's GitHub releases); the tested version's `checksums.txt` is pinned in the binary, so a mirror cannot substitute its own hashes for it. Any other version is trusted because it comes over HTTPS, so a mirror on another machine must use `https://`.

> [!NOTE]
> AerolVM v0.7.1–v0.7.7 has a bug in the upgrade host step on a node installed in IP mode: its `node.json` keeps `"ingress_proxy_routing": "auto"`, and the host step passes `auto` to `install.sh` as a value, which `install.sh` refuses. The upgrade fails and that node goes back to its old release. Install an IP-mode node with `--no-ingress-proxy-routing` (routing is off without a domain anyway), or change the value to `false` in `/etc/sandboxd/node.json` before upgrading. This is an AerolVM bug, not a Lewbox one.

| Flag | |
|---|---|
| `--version <tag>` | The AerolVM release to move to (default: the tested one) |
| `--batch <n\|n%>` | How many worker nodes restart at a time (default 10%) |
| `--pause-after <n>` | Pause after this many workers, to test the release first |
| `--hold` | Stop at `done`, before finalize, so a rollback stays possible |
| `--allow-downtime` | Accept that the API is down while one of one or two Raft voters restarts |
| `--check` | Show the plan and change nothing |
| `--yes` | No prompt outside a terminal |

The plan shows the node order, the version skew, one-way migrations and warnings. Lewbox refuses when the jump is not allowed, when a node is newer than the target, or when the plan cannot be read. With one or two Raft voters the cluster has no majority while a server restarts, so AerolVM asks for `--allow-downtime`; without it the error says so.

Follow and control it with:

```bash
lewbox cluster upgrade status eu-west      # nodes, roles, from/to versions, state
lewbox cluster upgrade stop eu-west        # finish the node in progress, start no more
lewbox cluster upgrade resume eu-west      # go on after a pause
lewbox cluster upgrade rollback eu-west    # back to the previous release
lewbox cluster upgrade finalize eu-west    # turn the new release on for good
```

`rollback` works while the rollout is `paused`, `stopped` or `failed`, and at `done` when it was started with `--hold`. Once a rollout is finalized, rollback is no longer possible.

## Next

- [Troubleshooting](troubleshooting.md)
- [What Lewbox changes on a machine](../reference/what-lewbox-changes.md)
