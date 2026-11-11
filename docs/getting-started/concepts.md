---
title: Concepts
description: The few ideas Lewbox is built on — orchestrator, cluster, node, account, context — and how they fit together.
---

Lewbox sits **next to** your AerolVM clusters, not in front of them. Your customers' apps talk to the clusters directly. Lewbox installs and registers the clusters, checks them, and (later) handles accounts, keys, limits and billing around them.

```
  you (lewbox CLI) ──────────────►  orchestrator  ──── admin token ────►  cluster "eu-west"
        │                          (lewbox serve)                         ├── node eu-west-1
        │                                                                 └── node eu-west-2
        └── on each machine: lewbox install, lewbox node add
                                                   customers' apps ──────►  cluster endpoint
```

## Orchestrator

The Lewbox server: one process, `lewbox serve`. It holds the database (accounts, clusters, nodes), serves the HTTP API under `/v0`, and checks every cluster once a minute. You run one per Lewbox installation. See [Run the orchestrator](../setup/run-the-orchestrator.md).

## Cluster

One AerolVM cluster, registered with Lewbox. A cluster has:

- a **slug**, such as `eu-west`, that you use in commands;
- an **admin URL**, where Lewbox reaches it with an admin token;
- an **endpoint**, the URL your customers' SDKs use (often the same as the admin URL);
- one **runtime**: `docker` or `firecracker`.

A single machine is a one-node cluster. Lewbox stores the cluster's admin token encrypted and never shows it again.

## Node

One machine running AerolVM (`sandboxd`). Nodes belong to a cluster. Lewbox learns a cluster's nodes from AerolVM itself, so `lewbox node list` always reflects what the cluster reports.

You make a machine into a node in two steps, run on that machine:

1. `lewbox install` installs AerolVM.
2. `lewbox node add` either **starts a new cluster** there or **joins an existing one**.

[Start a cluster](../clusters/start-a-cluster.md) and [Add nodes](../clusters/add-nodes.md) walk through both.

## Install modes

How a node is reached decides how you install it:

| Mode | Flag | Use it for |
|---|---|---|
| Local | `--mode local` (the default) | Trying things on one machine. Listens on `127.0.0.1` only. Can't join a cluster |
| Public host | `--public-host <ip-or-host>` | Labs and private networks. Plain HTTP on port 21212 |
| Domain | `--domain <domain>` | Production. Wildcard TLS through Cloudflare DNS |

## Accounts

People sign in to the orchestrator with an email and password. An account is either an **instance admin**, who can do everything, or a regular account, who can view clusters and nodes and change only their own password. See [Manage accounts](../accounts/manage-accounts.md).

## Organizations

An **organization** is a business using Lewbox. Accounts are its **members**, each as an owner, admin, member or viewer, and its backend calls the Lewbox API with an organization **API key** (`lba_…`). See [Organizations and API keys](../accounts/organizations-and-api-keys.md).

## Local and remote mode

The CLI works two ways:

- **Local mode.** On the orchestrator's own machine, with no sign-in, the CLI opens the database directly. Whoever can read that database owns the installation, so there's nothing to sign in to.
- **Remote mode.** Anywhere else, `lewbox login https://lewbox.example.com` signs you in and saves a **context**. After that, every command goes over the HTTP API.

Both give the same results: CLI output in JSON is byte-for-byte what the API returns. See [Sign in](../accounts/sign-in.md).

## Next

- [Quickstart](quickstart.md): everything on one machine.
- [Try it with Docker](try-with-docker.md): a multi-node cluster on your laptop.
- [Requirements](../setup/requirements.md): what each machine needs.
