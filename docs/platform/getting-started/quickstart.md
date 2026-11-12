---
title: Quickstart
description: Run Lewbox and a working AerolVM node on one Linux machine in about five minutes.
---

Lewbox runs AerolVM sandbox clusters for you: it installs AerolVM on your machines, joins them into clusters, and keeps track of them from one server. This page gets you from nothing to a running node on a single Linux machine.

You need a Linux machine with systemd and `sudo` (a VM is fine). See [Requirements](../setup/requirements.md) for the details.

## 1. Install the CLI

Download the latest release, check it against the release's checksums and put it in `~/.local/bin`:

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
URL=https://github.com/harlew-dev/lewbox/releases/latest/download
cd "$(mktemp -d)" &&
  curl -fsSL -O "$URL/lewbox_linux_$ARCH" -O "$URL/checksums.txt" &&
  sha256sum --check --ignore-missing checksums.txt &&
  install -D -m 755 "lewbox_linux_$ARCH" ~/.local/bin/lewbox &&
  cd - >/dev/null
export PATH="$HOME/.local/bin:$PATH"
lewbox version
```

This machine runs the orchestrator, so use the release: it includes the web console. On other nodes, the shorter `go install` route in [Install the CLI](../setup/install-the-cli.md) is enough.

## 2. Start the orchestrator

The orchestrator is the Lewbox server. Start it in its own terminal and leave it running:

```bash
lewbox serve
```

It listens on port 8080 and keeps its data in `~/.local/share/lewbox/`. On this machine the CLI talks to that data directly, so you don't need to sign in.

In a browser, open [http://localhost:8080/](http://localhost:8080/) for the web console; see [Use the web console](web-console.md).

Prefer Docker? See [Run with Docker](../setup/run-with-docker.md).

## 3. Install AerolVM

Back in your first terminal:

```bash
lewbox install
```

Lewbox checks the machine, asks you to confirm, then runs AerolVM's own installer with `sudo`. It prints each privileged command before running it, and masks any secrets. This takes a minute or two.

With no other flags, `install` uses **local mode**: AerolVM listens on `127.0.0.1:21212` only, with no TLS. That's right for trying things out. For machines other people reach, see [Start a cluster](../clusters/start-a-cluster.md).

## 4. Add this machine

```bash
lewbox node add
```

This turns on the AerolVM settings Lewbox needs, creates an admin token for Lewbox, and registers the machine as a one-node cluster named after the host.

## 5. Look at it

```bash
lewbox cluster list
lewbox node list
lewbox cluster check my-host     # use your cluster's slug from cluster list
```

```text
SLUG     RUNTIME  STATUS  NODES  VERSION  ENDPOINT
my-host  docker   ready   1      v0.7.0   http://127.0.0.1:21212
```

`ready` means every check passed. `cluster check` shows each check and how to fix any that fail.

That's it: AerolVM is running, and Lewbox knows about it. Your apps can now create sandboxes against `http://127.0.0.1:21212` with the AerolVM, E2B or Daytona SDKs.

## Next

- **[Concepts](concepts.md)**: the orchestrator, clusters, nodes, and local vs remote mode.
- **[Try it with Docker](try-with-docker.md)**: a three-machine cluster on your laptop, no spare servers needed.
- **[Start a cluster](../clusters/start-a-cluster.md)**: real machines, reachable by others, that can grow.
- **[Add nodes](../clusters/add-nodes.md)**: join more machines with one command each.
- **[Sign in from anywhere](../accounts/sign-in.md)**: run the CLI against a remote orchestrator.
- **[CLI reference](../reference/cli.md)**: every command and flag.
