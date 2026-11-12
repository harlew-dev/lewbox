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

> [!WARNING]
> `update --aerolvm` refuses on a machine that is one of **several** nodes in a cluster. Upgrading one Raft member alone leaves the cluster on mixed versions. Cluster-wide rolling upgrades (`lewbox cluster upgrade`) are planned. Until then, follow AerolVM's own `aerolvm upgrade` procedure for multi-node clusters. Lewbox also refuses if it can't tell how many nodes there are.

## Next

- [Troubleshooting](troubleshooting.md)
- [What Lewbox changes on a machine](../reference/what-lewbox-changes.md)
