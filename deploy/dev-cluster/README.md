# Lewbox dev cluster

A Lewbox orchestrator and two (or three) machines that each run a real AerolVM node, all in Docker on one host. Use it to try the CLI end to end: sign in, install AerolVM, start a cluster, join nodes, remove them. The user-facing walkthrough is [Try it with Docker](../../docs/getting-started/try-with-docker.md); this page has the details.

Each node container is a small "machine": Ubuntu 24.04 with systemd as PID 1 and **its own Docker engine**. Its sandboxes stay inside it, apart from the host's Docker and from the other nodes. Nothing AerolVM is baked into the image. `lewbox install` runs AerolVM's real installer inside the container, as it would on a server.

```
 host (127.0.0.1)
   :8080 ──────► orchestrator   172.30.0.10   lewbox serve (SQLite in a volume)
   :21201 ─────► node-1         172.30.0.11   systemd + dockerd + sandboxd + caddy
   :21202 ─────► node-2         172.30.0.12   systemd + dockerd + sandboxd + caddy
   :21203 ─────► node-3         172.30.0.13   (optional: ./dev.sh up --three)
                 └── network lewbox-dev_lewbox, 172.30.0.0/24
```

## Needs

- Linux with Docker 25+ and the compose plugin. Tested on Docker 29.1 and kernel 7.0 with cgroup v2.
- The node containers run **privileged**: systemd and Docker inside them need it. Only run this on a machine you control.
- About 2 GB of RAM and 4 GB of disk for two nodes. The AerolVM release files (about 200 MB per version) are downloaded once into `.cache/` and shared by every node.

## Use

```bash
cd deploy/dev-cluster
./dev.sh up          # fetch AerolVM v0.7.0, build images, start everything (about 1 min)
./dev.sh cluster     # sign in on each node, install AerolVM on both, start "lab" on node-1, join node-2 (about 2 min)
```

`./dev.sh cluster` runs the same commands you would type on each machine:

```bash
# on node-1
lewbox login http://orchestrator:8080 --email admin@lewbox.dev
lewbox install --public-host 172.30.0.11
lewbox node add node-1 --new-cluster lab

# on node-2
lewbox login http://orchestrator:8080 --email admin@lewbox.dev
lewbox install --public-host 172.30.0.12
lewbox node add node-2 --cluster node-1      # the cluster that node-1 is in
```

To do it by hand instead, open a shell on a node with `./dev.sh shell node-1`. The admin account is `admin@lewbox.dev` / `dev-password-123`; `LEWBOX_DEV_EMAIL` and `LEWBOX_DEV_PASSWORD` change it before `up`. `LEWBOX_AEROLVM_FROM=/opt/aerolvm` is set on every node, so `install` and `node add` use the shared release files instead of GitHub.

Other commands:

| | |
|---|---|
| `./dev.sh lewbox node-1 cluster get lab` | Run any `lewbox` command on a node |
| `./dev.sh shell node-2` | A shell on a node, as the `lewbox` user (it has passwordless sudo) |
| `./dev.sh logs` / `./dev.sh logs node-1` | Follow the orchestrator's log, or `sandboxd`'s on a node |
| `./dev.sh up --three` | Also start node-3, for a three-voter cluster |
| `./dev.sh cluster --runtime firecracker` | Install the nodes for Firecracker instead of Docker (below) |
| `./dev.sh down` | Stop everything and delete the dev cluster's volumes |
| `./archives.sh` | Storage profiles and archived sandboxes against a running cluster (the `storage` service is an S3-compatible bucket, RustFS, at `172.30.0.20:9000`) |
| `./e2e.sh` | The end-to-end test (below) |

From the host you can also use the orchestrator directly: `lewbox login http://127.0.0.1:8080`. Each node's AerolVM API is published on `127.0.0.1:2120N`.

## End-to-end test

`./e2e.sh` builds a fresh dev cluster and checks the whole flow with real AerolVM:

- the CLI refuses to run as root, and `install --dry-run` changes nothing;
- sign-in, install on two nodes, cluster start, join by node name;
- the cluster is `ready`, every check passes, and both nodes are alive;
- the API agrees, needs a token, and never returns the cluster's admin token;
- Lewbox's settings are on each node, in Lewbox's own files;
- the installer's PAT never appears in the CLI's output;
- sandboxes start through node-2 and spread across both nodes;
- `firewall enable` turns on ufw on both nodes (keeping or resetting other rules), and the cluster, sandboxes and egress allowlists still work behind it;
- a storage profile on the dev bucket (`archives.sh`): `node configure` installs the registry and the cleanup timer on both nodes, a customer's stopped sandbox is archived to the bucket as `archive-<name>-<id>`, its local copy kept under a 1d retention and removed under `instant`, restored on any node with its files, and deleted from the bucket;
- `node add` is safe to run again;
- a stopped cluster is reported `unreachable`, then `ready` again;
- `node remove` needs `--yes`, removes the node from AerolVM too, and refuses the last node;
- `update --aerolvm` upgrades a single-machine setup (node-3: local mode, orchestrator on the same machine) from v0.6.13 to v0.7.0.

`--keep` leaves the cluster running afterwards. `--reuse` runs the checks against a cluster that is already up. `--runtime firecracker` runs it all on Firecracker nodes.

## Firecracker

`./dev.sh cluster --runtime firecracker` (or `./e2e.sh --runtime firecracker`, or `LEWBOX_DEV_RUNTIME=firecracker`) installs every node with `lewbox install --runtime firecracker`, so cluster `lab` runs microVMs instead of containers. `customers.sh` and `archives.sh` read `LEWBOX_DEV_RUNTIME` and create their sandboxes with that runtime.

- The host needs `/dev/kvm`: bare metal, or a VM with nested virtualization. The node containers are privileged, so they see the host's `/dev/kvm`; nothing else is passed through.
- The installer downloads Firecracker, the jailer and a guest kernel from GitHub and S3 (pinned hashes) on each node, so the first install takes a little longer.
- A cluster's runtime is fixed when it starts. To switch, `./dev.sh down` and start again.

## Troubleshooting

- **`Could not resolve 'archive.ubuntu.com'` during install.** The AerolVM installer runs `apt-get`, and the nodes resolve outside names through the host's resolver. Some home routers return SERVFAIL for some names (this happened on the machine this was built on). Send the nodes' outside lookups elsewhere, then start again:

  ```bash
  export LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8"
  ./dev.sh down && ./dev.sh up && ./dev.sh cluster
  ```

  Check the host with `getent hosts archive.ubuntu.com`.
- **An install log.** `./dev.sh cluster` keeps each node's install output in `.cache/install-node-N.log` (secrets masked).
- **sandboxd on a node.** `./dev.sh logs node-1`, or `./dev.sh shell node-1` then `sudo journalctl -u sandboxd`.
- **Start over.** `./dev.sh down` deletes the dev cluster's containers and volumes (only this project's).

## Things that differ from a real server

- **IP mode, no TLS.** Nodes use `--public-host <container IP>`. Domain mode needs a real DNS zone and Cloudflare token.
- **No firewall until the e2e test turns one on.** The node image includes `ufw` (as Ubuntu servers do), off by default. `e2e.sh` runs `lewbox firewall enable` on node-1 and node-2.
- The image removes Ubuntu's container-only `policy-rc.d` (it stops services from starting), adds `file` (the installer uses it to check binaries), and masks units that need real hardware.
- `/var/lib/docker`, `/var/lib/containerd` and `/var/lib/sandboxd` are volumes, because Docker inside Docker can't use overlayfs on top of overlayfs.
- `/lib/modules` is mounted read-only, so `sandboxd` can load `br_netfilter` for sandbox network isolation.
