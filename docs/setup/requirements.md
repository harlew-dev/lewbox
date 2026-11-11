---
title: Requirements
description: What the orchestrator and each AerolVM node need — operating system, privileges, ports and network.
---

## The orchestrator

Any machine that can run a Go binary: Linux or macOS, amd64 or arm64. It needs:

- a port for its API (8080 by default);
- network access to each cluster's admin URL;
- a disk for its data folder (SQLite, small).

It doesn't need root.

## Each AerolVM node

| | Needed |
|---|---|
| OS | Linux with systemd. Ubuntu 22.04 or later is what AerolVM tests; other apt-based distros usually work. macOS isn't supported by `lewbox install` yet |
| Privileges | A normal user with `sudo`. Lewbox refuses to run as root, and runs each privileged step through `sudo` |
| Container engine | Docker. AerolVM's installer installs it if it's missing |
| Disk | Room for sandbox images and disks under `/var/lib/sandboxd`. AerolVM's own preflight checks the free space before it starts or joins a cluster |
| Firecracker (optional) | `/dev/kvm`: bare metal or a VM with nested virtualization, x86_64 or aarch64 |
| AerolVM | v0.6.13 or later. Lewbox installs v0.7.0 by default |

`sudo` must work without a password prompt when the CLI isn't run from a terminal (for example in scripts). On a terminal, Lewbox lets `sudo` ask for your password once, before it changes anything.

## Ports

| Port | Who reaches it | Used for |
|---|---|---|
| 8080/TCP | You, and the nodes when they run `lewbox` commands | The orchestrator's API (configurable) |
| 21212/TCP | The orchestrator; your customers in public-host mode | AerolVM's API |
| 443/TCP | Your customers | AerolVM's API and sandbox URLs in domain mode |
| 80/TCP | Your customers | Sandbox URLs in public-host mode |
| 2220/TCP | Your customers | AerolVM's SSH gateway |
| 22000–23000/TCP | Your customers (optional) | Ports that sandboxes expose |
| 7000–7003 | **Cluster nodes only** | Raft, gossip, internal RPC and joins |

> [!WARNING]
> Never open ports 7000–7003 to the internet. Nodes in one cluster need a private network between them (a VPC, a WireGuard mesh, or a dedicated subnet), and every node must reach every other node on these ports. AerolVM checks this when a node joins.

## Next

- [Install the CLI](install-the-cli.md)
- [Run the orchestrator](run-the-orchestrator.md)
- [Start a cluster](../clusters/start-a-cluster.md)
