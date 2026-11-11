---
title: Try it with Docker
description: Run an orchestrator and a real multi-node AerolVM cluster on one Linux host, with each node in its own container.
---

No spare servers? The dev cluster gives you an orchestrator and two (or three) "machines" in Docker. Each machine is a container with systemd and its **own** Docker engine, so `lewbox install` runs AerolVM's real installer inside it, exactly as on a server, and sandboxes stay inside their node.

You need Linux with Docker 25+ and the compose plugin, about 2 GB of free RAM, and 4 GB of disk. The node containers run **privileged**, so only do this on a machine you control.

## 1. Start everything

```bash
cd deploy/dev-cluster
./dev.sh up
```

This downloads AerolVM v0.7.0 once (about 200 MB, checked against the release's checksums), builds the images, and starts:

| | Address on your host |
|---|---|
| Orchestrator | `http://127.0.0.1:8080` |
| node-1 | AerolVM API on `127.0.0.1:21201` |
| node-2 | AerolVM API on `127.0.0.1:21202` |

The admin account is `admin@lewbox.dev` with password `dev-password-123`.

## 2. Make a cluster

```bash
./dev.sh cluster
```

That runs, on each node, the same commands you'd type on real machines:

```bash
# node-1: start cluster "lab"
lewbox login http://orchestrator:8080 --email admin@lewbox.dev
lewbox install --public-host 172.30.0.11
lewbox node add node-1 --new-cluster lab

# node-2: join it
lewbox login http://orchestrator:8080 --email admin@lewbox.dev
lewbox install --public-host 172.30.0.12
lewbox node add node-2 --cluster node-1
```

It takes about two minutes and ends with the cluster's nodes:

```text
NAME    CLUSTER  ROLE   VERSION  ALIVE  URL
node-1  lab      mixed  v0.7.0   yes    http://172.30.0.11:21212
node-2  lab      mixed  v0.7.0   yes    http://172.30.0.12:21212
```

## 3. Explore

```bash
./dev.sh shell node-2                       # a shell on node-2, as the lewbox user
lewbox cluster get lab                      # checks and nodes
./dev.sh lewbox node-1 cluster check lab    # or run one command from the host
./dev.sh logs node-1                        # follow sandboxd on node-1
```

You can also use the CLI on your host against the orchestrator: `lewbox login http://127.0.0.1:8080`.

> [!TIP]
> `./dev.sh up --three` adds node-3, and `./dev.sh cluster` then joins it too, for a three-voter cluster.

## 4. Clean up

```bash
./dev.sh down
```

This deletes the dev cluster's containers and volumes, and nothing else.

> [!NOTE]
> If installs fail with `Could not resolve 'archive.ubuntu.com'`, your network's DNS resolver is failing on that name. Run `export LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8"`, then `./dev.sh down && ./dev.sh up`. See [Troubleshooting](../operations/troubleshooting.md#could-not-resolve-archiveubuntucom-in-the-dev-cluster).

## Next

- [Start a cluster](../clusters/start-a-cluster.md): the same flow on real machines.
- [Remove nodes and clusters](../clusters/remove-nodes-and-clusters.md): try `lewbox node remove node-2`.
- [Dev cluster details](../../deploy/dev-cluster/README.md): the end-to-end test, ports, and how the containers differ from real servers.
