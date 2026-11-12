---
title: Start a cluster
description: Turn a Linux machine into the first node of a new AerolVM cluster and register it with Lewbox, in two commands.
---

On the machine that will be the cluster's first node, you run `lewbox install` and then `lewbox node add`. Later machines [join it](add-nodes.md) with one command each.

## Before you start

- The orchestrator is running and you have an admin account. See [Run the orchestrator](../setup/run-the-orchestrator.md).
- The machine meets the [requirements](../setup/requirements.md), and `lewbox` is installed on it ([Install the CLI](../setup/install-the-cli.md)).
- You're logged in as a normal user that can use `sudo`, not as root. If the server only has `root`, [create a user first](../setup/install-the-cli.md#first-a-normal-user-with-sudo).

## 1. Sign in

On the new machine:

```bash
lewbox login https://lewbox.example.com
```

It asks for your email and password. See [Sign in](../accounts/sign-in.md).

## 2. Install AerolVM

Choose how customers will reach the node.

**On a private network or in a lab**, use the machine's IP:

```bash
lewbox install --public-host 10.0.0.5
```

**In production**, use a domain with wildcard TLS. You need a Cloudflare API token with Zone:Read and DNS:Edit on that zone, passed on stdin:

```bash
lewbox install --domain eu-west.api.example.com --acme-email ops@example.com \
  --dns-api-token-stdin < cloudflare-token
```

Lewbox checks the machine first. Nothing changes unless every check passes:

```text
Checking this machine
  ✓ os: Linux
  ✓ sudo: sudo works without a password
  ✓ ports: ports 21212, 7000, 7001, 7002, 7003 are free
AerolVM v0.7.0
  ✓ install.sh verified (sha256 checked)
  → sudo bash ~/.cache/lewbox/aerolvm/v0.7.0/install.sh --version v0.7.0 --pat-scope host --node-name node-1 --public-host 10.0.0.5 --no-start
  …
  ✓ installed; sandboxd is stopped until this node starts or joins a cluster
```

AerolVM is installed but **not started yet**. That's on purpose: a node has to start as part of a cluster, or it can't join one later.

> [!TIP]
> Add `--dry-run` to see every check and command without changing anything. Add `--runtime firecracker` for microVM sandboxes (needs `/dev/kvm`). [What Lewbox changes on a machine](../reference/what-lewbox-changes.md) lists everything `install` does.

## 3. Start the cluster

```bash
lewbox node add node-1 --new-cluster eu-west
```

```text
Starting a cluster
  → sudo bash …/install.sh --version v0.7.0 --pat-scope host init --role mixed --node-id node-1 --advertise 10.0.0.5
The cluster is up. This node, node-1, is its first server and its leader.
Lewbox settings
  ✓ wrote /etc/sandboxd/lewbox.env and /etc/systemd/system/sandboxd.service.d/lewbox.conf, restarted sandboxd
  ✓ sandboxd healthy at http://127.0.0.1:21212/health
  ✓ admin token "lewbox-eu-west" minted over http://127.0.0.1:21212
  ✓ cluster "eu-west" registered, node "node-1" added
```

In order, Lewbox:

1. starts AerolVM as the first node of a new cluster;
2. turns on the AerolVM settings Lewbox needs (identity and long-lived keys);
3. creates an admin token for Lewbox on the machine itself, over `127.0.0.1`;
4. registers the cluster with the orchestrator, which checks it and stores the token encrypted.

The node name defaults to the machine's short hostname, and the cluster slug defaults to the node name.

## 4. Check it

```bash
lewbox cluster get eu-west
```

```text
admin_token_last4: 9f3a
admin_url        : http://10.0.0.5:21212
checked_at       : just now
created_at       : just now
endpoint_url     : http://10.0.0.5:21212
id               : 01a1cb34-29a5-74c3-9691-5394d021b0f2
name             : eu-west
nodes            : 1
runtime          : docker
slug             : eu-west
status           : ready
version          : v0.7.0

CHECK        OK   DETAIL
health       yes  v0.7.0, 1 node(s)
version      yes  v0.7.0
admin_token  yes  admin on team *
members      yes  1 member(s)
runtime      yes  members support docker
token_ttl    yes  a 36500d runner token was created and revoked
endpoint     yes  same as admin_url

NAME    CLUSTER  ROLE   VERSION  ALIVE  URL
node-1  eu-west  mixed  v0.7.0   yes    http://10.0.0.5:21212
```

Running `node add` again is safe: AerolVM's `init` doesn't start a second cluster, and Lewbox checks the registered cluster instead of creating another admin token.

## Different addresses for Lewbox and customers

By default, Lewbox and your customers use the same URL. If customers should use another (for example a load balancer in front of several nodes), set it when you start the cluster:

```bash
lewbox node add node-1 --new-cluster eu-west --endpoint-url https://eu-west.api.example.com
```

`--admin-url` changes the address the orchestrator uses, and `--advertise` changes the IP that other nodes use to reach this one.

## Next

- [Add nodes](add-nodes.md): grow the cluster to three nodes.
- [Health and checks](health-and-checks.md): what each check means.
- [Register an existing cluster](register-an-existing-cluster.md): for clusters you set up without Lewbox.
