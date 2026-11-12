---
title: Lewbox documentation
description: Install AerolVM on your machines, join them into clusters, and manage them all from one server.
---

Lewbox runs [AerolVM](https://github.com/aerol-ai/microvm) sandbox clusters for you. One CLI installs AerolVM on a machine, starts or joins a cluster, and registers it with the Lewbox orchestrator, which keeps checking it from then on.

```bash
lewbox serve                                     # the orchestrator
lewbox install --public-host 10.0.0.5            # on each machine
lewbox node add node-1 --new-cluster eu-west     # the first one
lewbox node add node-2 --cluster eu-west         # every one after it
```

New here? Start with the **[Quickstart](getting-started/quickstart.md)**: one machine, about five minutes. If you are setting up workspaces for other people, see **[what your customers see](../consumer/README.md)** in the customer book.

## Get started

| | |
|---|---|
| [Quickstart](getting-started/quickstart.md) | The orchestrator and a working AerolVM node on one Linux machine |
| [Use the web console](getting-started/web-console.md) | Sign in from a browser, and what each page lets you do |
| [Concepts](getting-started/concepts.md) | Orchestrator, cluster, node, install modes, accounts, local and remote mode |
| [Try it with Docker](getting-started/try-with-docker.md) | A real multi-node cluster on one host, each node in its own container |

## Set up

| | |
|---|---|
| [Requirements](setup/requirements.md) | What the orchestrator and each node need, and which ports to open |
| [Install the CLI](setup/install-the-cli.md) | Install `lewbox` on each machine |
| [Run the orchestrator](setup/run-the-orchestrator.md) | `lewbox serve`, the first admin, where data lives |
| [Run with Docker](setup/run-with-docker.md) | The orchestrator, console and docs in one container, with a volume |
| [Run in production](setup/run-in-production.md) | A systemd service, TLS in front, backups |

## Clusters

| | |
|---|---|
| [Start a cluster](clusters/start-a-cluster.md) | Turn a machine into a cluster's first node |
| [Add nodes](clusters/add-nodes.md) | Join more machines, one command each |
| [Register an existing cluster](clusters/register-an-existing-cluster.md) | Manage a cluster you set up without Lewbox |
| [Health and checks](clusters/health-and-checks.md) | What Lewbox checks, statuses, and fixes |
| [Remove nodes and clusters](clusters/remove-nodes-and-clusters.md) | Take a node out, or stop managing a cluster |

## Accounts

| | |
|---|---|
| [Sign in](accounts/sign-in.md) | `lewbox login`, contexts, scripts and CI |
| [Manage accounts](accounts/manage-accounts.md) | Instance admins and regular accounts, passwords, disabling |
| [Organizations and API keys](accounts/organizations-and-api-keys.md) | Organizations, member roles, and `lba_` API keys for your backend |
| [Customers, plans and limits](accounts/customers-and-plans.md) | Sign-up, setup links, plans, a workspace's region, approvals, suspension |

## Operations

| | |
|---|---|
| [Update](operations/update.md) | Update the CLI and AerolVM on a machine |
| [Turn on the firewall](operations/firewall.md) | ufw with exactly the ports a node needs |
| [Troubleshooting](operations/troubleshooting.md) | Common errors and how to fix them |

## Reference

| | |
|---|---|
| [CLI reference](reference/cli.md) | Every command and flag |
| [Configuration](reference/configuration.md) | Environment variables, files, defaults, the pinned AerolVM version |
| [Output and errors](reference/output-and-errors.md) | Text and JSON, error codes, exit codes |
| [HTTP API](reference/http-api.md) | The orchestrator's `/v0` routes and objects |
| [What Lewbox changes on a machine](reference/what-lewbox-changes.md) | Every file and privileged command on a node |
| [Security](reference/security.md) | What's stored, how secrets are protected |

## For contributors

| | |
|---|---|
| [AGENTS.md](../../AGENTS.md) | How the code is laid out, how to build and test, project rules |
| [Web console development](../../web/README.md) | The React app, how it is bundled and served, and how to add a page |
| [Dev cluster](../../deploy/dev-cluster/README.md) | The Docker test bed and the end-to-end test |
| [MVP specification](../../MVP.md) | The original, larger product plan; partly superseded by [plans/](../../plans/README.md) |
| [Feasibility review](../../REVIEW.md) | Why Lewbox builds on stock AerolVM clusters |

The design notes are in [docs/design](../design/): [CLI commands](../design/cli-commands.md), [CLI conventions](../design/cli-conventions.md) and the [AerolVM integration](../design/aerolvm-integration.md). They are for contributors and are not part of either book.

## What's built

Built and tested end to end: the orchestrator, the admin web console, accounts and sign-in, organizations with members and API keys, customer sign-up and setup links, plans (one region per workspace), sandbox keys, sandboxes and usage, snapshots, the customer portal, installing AerolVM, starting and joining clusters, registering existing clusters, health checks, removing nodes and clusters, and updating. Planned (see [MVP.md](../../MVP.md)): webhooks and cluster-wide rolling upgrades.
