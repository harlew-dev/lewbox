---
title: Turn on the firewall
description: Turn on ufw on a node with exactly the ports Lewbox and AerolVM need, and optionally clear out rules they don't.
---

`lewbox firewall enable` turns on [ufw](https://wiki.ubuntu.com/UncomplicatedFirewall) on a node and opens only the ports that node needs: SSH, the ports for its install mode, the cluster ports from your private network only, and the few host ports AerolVM lets sandboxes reach.

Run it on each node **after** `lewbox node add`. It reads the running AerolVM to work out the cluster's network and its sandbox rules.

```console
$ lewbox firewall enable
You have 8080/tcp, 5432/tcp from 10.0.0.0/8 open that Lewbox doesn't use.
Resetting removes them and keeps only Lewbox's rules (ufw saves a backup in /etc/ufw). Answering no keeps them and adds Lewbox's.
Do you want to reset your firewall? [Y/n]
Firewall (ufw)
  → sudo ufw --force reset
  → sudo ufw default deny incoming
  → sudo ufw default allow outgoing
  → sudo ufw allow proto tcp from any to any port 22 comment lewbox:ssh
  …
  → sudo ufw --force enable
  ✓ ufw enabled with 14 Lewbox rules
```

The question only appears when ufw already has rules that Lewbox wouldn't set. Answering **yes** (the default) resets ufw, so only Lewbox's rules are left. Answering **no** keeps your rules and adds Lewbox's next to them. Either way, incoming traffic is denied unless a rule allows it.

## What it opens

| Port | From | When | Why |
|---|---|---|---|
| Your SSH port (TCP) | anywhere | always | So you aren't locked out. Lewbox reads it from `sshd -T` and from your current SSH session, falling back to 22 |
| 80/TCP | anywhere | public-host mode | Sandbox URLs |
| 443/TCP | anywhere | domain mode | AerolVM's API and sandbox URLs |
| 21212/TCP | anywhere | public-host mode | AerolVM's API |
| 2220/TCP | anywhere | public-host and domain modes | AerolVM's SSH gateway |
| 22000–23000/TCP | anywhere | public-host and domain modes | Ports that sandboxes expose |
| 7000–7003/TCP, 7001/UDP | **your private network only** | public-host and domain modes | Raft, gossip, internal RPC and joins |
| 21212/TCP | your private network only | domain mode | AerolVM's API between nodes |
| AerolVM's sandbox ports | the sandbox network | always | See below |

Local mode listens on `127.0.0.1` only, so it gets SSH and the sandbox rules.

Every rule has a `lewbox:<purpose>` comment, so `sudo ufw status` shows which ones are Lewbox's.

### The cluster network

Ports 7000–7003 must never be open to the internet (see [Requirements](../setup/requirements.md#ports)). Lewbox opens them only to the private network your nodes share. By default that is the network of the interface holding this node's cluster address, for example `10.0.0.0/24`. It must be a private range (RFC 1918, IPv6 ULA, or `100.64.0.0/10` as used by Tailscale).

Lewbox refuses, and changes nothing, when:

- it can't read the cluster's members from the AerolVM on this machine;
- this node's cluster address isn't on a private network;
- a node already in the cluster is outside that network, so the firewall would cut it off.

In each case, pass the network yourself:

```bash
lewbox firewall enable --peers 10.0.0.0/16
lewbox firewall enable --peers 10.0.1.0/24 --peers 10.0.2.0/24
```

A node you add later must also be inside `--peers`, or the existing nodes' firewalls will block its join.

### Sandbox rules

AerolVM keeps its own iptables rules (the "node guard") for what sandboxes may reach on the host: its egress proxy and DNS, and ports 80 and 443. With ufw's default-deny, those connections would be dropped, and sandboxes with egress rules (`network_allow_out`) could no longer reach even the hosts they are allowed. Lewbox copies the node guard's allow rules into ufw, so sandboxes can reach exactly what AerolVM allows and nothing else. If AerolVM's rules aren't loaded (for example, `sandboxd` isn't running), Lewbox refuses.

## Other ports on this machine

To open other ports to anyone, for example the orchestrator running on the same machine:

```bash
lewbox firewall enable --allow 8080/tcp
lewbox firewall enable --allow 8081 --allow 60000:61000/udp
```

Ports given with `--allow` count as Lewbox's own, so they aren't offered for removal. A load balancer health port (`-- --lb-health-port` at install) needs `--allow` too.

## Scripts and dry runs

Without a terminal, Lewbox can't ask. If there are other rules, say what to do with them:

| | |
|---|---|
| `--reset` | Reset without asking |
| `--reset=false` | Keep the other rules without asking |
| `--yes` | Take the default answer: reset |
| `--dry-run` | Read everything and print every command, changing nothing |

Without one of the first three, it stops with exit code 2 and lists the rules.

## Things to know

- ufw only filters traffic to this machine. Ports that Docker publishes on the host skip ufw's rules; AerolVM publishes sandbox ports on `127.0.0.1` only.
- `ufw reset` saves the old rules as `/etc/ufw/*.rules.<timestamp>` before clearing them.
- Running it again is safe. ufw skips rules that already exist.
- Lewbox doesn't turn the firewall off. Use `sudo ufw disable`.
