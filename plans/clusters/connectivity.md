# Connectivity

> Status: Draft · Milestone: Alpha (M1 direct; M4 tunnel) · Updated: 2026-10-29

How the control plane reaches a cluster's nodes, and how people reach the sandboxes on them. A cluster connects in one of two ways. **Direct**: every node has a public IP and its own subdomain, and is reached on port 443, as in legacy. **Tunnel**: nodes only dial out, through the node agent, so they can sit behind NAT, in a homelab or a private network. The announcement promises both: the hosted control plane "reaches your servers through a secure tunnel or exposed ports".

## Today

- **Direct only.** The gateway calls each node's AerolVM API at `https://node-<id>.<domain>` (Caddy on 443, wildcard certificate by DNS-01), or `http://<ip>:21212` without a domain, which it refuses outside local development, as the PAT would cross the internet in clear (`lewbox-gateway-legacy/docs/aerolvm.md` § *Reaching a node*).
- **Sandbox URLs:**
  - `https://<aerolvm id>-<port>.node-<id>.<domain>` for HTTP ports;
  - `tcp://<node host>:<22000–23000>` for TCP ports;
  - `http://<host>/<id>/` without a domain.
- **One public IP per node.** Enrollment refuses a node whose IP another live node in the cluster has. Every cluster address is the public IP, a router can forward each port to only one node, and sandbox URLs carry no port (`lewbox-legacy/docs/clusters.md` § *3. Install script*: "a homelab behind one router … isn't supported yet").
- **Ports a node must open:**
  - 443 (or 80 and 21212) to the internet;
  - 22000–23000 to the internet;
  - 7000–7002 TCP, 7001 UDP and 21212 to the other nodes;
  - SSH.
- **Laravel Cloud's edge cut plain requests at ~20 s** with a `504`, which broke long commands and create waits. Streams with keepalives were fine (`docs/todo.md` l.76). Not a node problem, but a reminder that every hop in the path has timeouts.

## Goals

- Keep direct connectivity as the fast, simple default for servers with public IPs.
- Add a tunnel, so a node needs **no inbound ports from the internet at all**: homelabs, office servers, private clouds, several nodes behind one public IP.
- Sandbox URLs (HTTP, private ports, TCP) work the same in both modes.
- No extra trust: the tunnel can't see sandbox traffic (TLS passes through it), and it only reaches the local ports it's meant to.

### Non-goals

- Nodes of one cluster in different private networks. A cluster's nodes must reach each other directly for Raft and gossip (same LAN, or a VPN the project runs). A managed WireGuard mesh is a later idea (open question 3).
- Load-balancing a sandbox's port across nodes. A sandbox runs on one node.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Direct mode as legacy built it: a subdomain per node, wildcard TLS on the node, SNI routing by AerolVM's Caddy, TCP ports from a pool, the PAT never in clear (HTTPS only) |
| **Change** | One public IP per node becomes a direct-mode rule only. Tunnel clusters advertise LAN addresses, and need no public IP at all |
| **Add** | Tunnel mode: the node agent's connection carries multiplexed streams; a **relay** (a role of `lewbox serve`) takes sandbox traffic on the instance's domain and passes it to nodes through their tunnels |
| **Drop** | The no-domain mode (`http://<ip>:21212`, `http://<host>/<id>/`) outside development. A cluster without its own domain uses the tunnel with the instance's relay domain instead |

## Design

### The two modes

`clusters.connectivity`: `direct` or `tunnel`, chosen at creation and changeable only while the cluster has no nodes. Mixing modes within one cluster is open question 2.

| | Direct | Tunnel |
|---|---|---|
| Control plane → AerolVM API | HTTPS to `https://node-<id>.<domain>` (443) | Through the node agent's tunnel to `127.0.0.1:21212` on the node |
| Browser → sandbox HTTP port | `https://<aerolvm id>-<port>.node-<id>.<domain>` → the node's 443 | `https://<aerolvm id>-<port>.node-<id>.<relay domain>` → the relay → the tunnel → the node's 443 (TLS passed through) |
| Client → sandbox TCP port | `tcp://node-<id>.<domain>:<22000–23000>` | `tcp://<relay host>:<relay port>` → the tunnel → the node's host port |
| Node ↔ node (Raft, gossip, mTLS) | Public IPs, ports opened to each other only | LAN (or VPN) addresses |
| Node → control plane | The agent's WebSocket (outbound 443) | The same; it also carries the tunnel |
| Node → bucket, releases | Outbound HTTPS | Outbound HTTPS |
| Inbound ports from the internet | 443, the TCP pool, SSH | None (SSH optional) |
| Domain | The project's, with DNS records per node ([pki-and-dns](pki-and-dns.md)) | The instance's relay domain (or the project's, pointed at the relay) |
| Cost | Nothing extra | Every byte of sandbox traffic goes through a relay |
| Latency | Direct | One more hop |

### Direct mode

As legacy, with the domain required outside development:

- **The API:** AerolVM's Caddy (caddy-l4) owns 443 and routes `node-<id>.<domain>` to sandboxd. The control plane verifies the node's certificate against public CAs.
- **Port URLs** are routed by the TLS server name, so records are **DNS only, never proxied** (a proxy like Cloudflare's would terminate TLS and break routing).
- **Enrollment refuses:**
  - a node whose public IP another live node in the cluster has;
  - a node whose IP is private or reserved, suggesting `--public-ip`, or the tunnel.
- **Development:** `LEWBOX_ENV=development` allows `http://<ip>:21212` and IP-mode sandbox URLs, for a local cluster without DNS.

### The tunnel

```
                 control plane replica(s)                              node (behind NAT)
 ┌──────────────────────────────────────────────┐            ┌──────────────────────────────┐
 │  AerolVM client ──dial(node, "api")──┐       │   WSS      │  lewbox node                 │
 │                                      ▼       │◀══════════▶│   yamux ── "api" ──▶ 127.0.0.1:21212 (sandboxd)
 │  relay :443 ──SNI──▶ dial(node,"https")──────│  (agent    │         ── "https" ─▶ 127.0.0.1:443   (Caddy)
 │  relay :tcp pool ──▶ dial(node,"tcp:22017")──│  dials out)│         ── "tcp:N" ─▶ 127.0.0.1:N     (pool only)
 └──────────────────────────────────────────────┘            └──────────────────────────────┘
```

- **The node agent's connection** ([node-agent](node-agent.md#connection)) is upgraded with a stream multiplexer: [yamux](https://github.com/hashicorp/yamux) over the WebSocket's binary frames. The agent dials out; the control plane opens streams.
- **Each stream starts with a header** naming its target, from a fixed list the agent allows:
  - `api` → `127.0.0.1:21212`;
  - `https` → `127.0.0.1:443`;
  - `tcp:<port>` → `127.0.0.1:<port>`, only within the sandbox TCP pool.

  Any other target is refused. The agent never dials beyond the node itself.
- **On the control plane:**
  - The AerolVM client takes a `Dialer` per node. Direct nodes use `net.Dialer`; tunnel nodes use `tunnels.Dial(nodeID, "api")`.
  - Everything above it is unchanged: HTTP, WebSockets for exec and attach, streamed files. The PAT goes inside the tunnel, which is itself TLS.
  - In tunnel mode the API is plain HTTP on the node's loopback, so there is no certificate to manage for it.
- **Keepalives:**
  - yamux pings every 15 s, and the WebSocket pings every 30 s, so idle NAT mappings stay open.
  - A broken tunnel fails open streams at once; the agent reconnects with backoff.
  - Meanwhile the node shows `unreachable` ([health-and-metrics](health-and-metrics.md#node-statuses)), and calls to it answer `503 cluster_unavailable`.
- **Flow control:** yamux's per-stream windows, so one large download can't starve a terminal. Window size 1 MiB, configurable.

#### Several replicas

A node's tunnel lands on one replica, but any replica may need it: for an API call, or for relay traffic arriving at a different relay. Two ways to solve it:

1. **The agent keeps a tunnel to every relay-capable replica** listed in its spec (`tunnel.endpoints`, each replica's own address). Every replica can then reach every tunnel node directly. Simple, and with 2–3 replicas cheap.
2. **Replicas forward to the one that holds the tunnel,** found in `node_connections`, over an internal mTLS connection between replicas. One tunnel per node, but an extra internal hop and an internal CA.

**Proposed: option 1 for the alpha.** An instance with one replica has one tunnel; an instance with many runs a few relay replicas, and agents connect to those.

### The relay

The relay is a role of `lewbox serve` ([foundation/binary-and-commands](../foundation/binary-and-commands.md)): `lewbox serve --roles api,worker,relay`. All roles are on by default; a large instance runs relays on their own hosts, near its nodes.

- **HTTPS:**
  - It listens on `:443` for `*.<relay domain>` and peeks the TLS ClientHello for the server name, without terminating TLS.
  - A name like `<anything>.node-<id>.<relay domain>` maps to node `<id>`; the relay opens a tunnel stream `https` to it and copies bytes both ways.
  - The node's own Caddy terminates TLS and routes to the sandbox as in direct mode. **The relay never sees sandbox traffic in clear.**
- **TCP:**
  - Exposing a TCP port on a tunnel cluster also allocates a port from the relay's pool (`LEWBOX_RELAY_TCP_PORTS`, e.g. 30000–40000), recorded on the port ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md)).
  - The relay listens on it and opens `tcp:<host port>` streams to the node.
  - The port object shows `host` = the relay's host and `host_port` = the relay port.
- **Certificates for tunnel nodes:** each node's Caddy needs a certificate for `node-<id>.<relay domain>` and its wildcard. It gets them by DNS-01 **through the control plane** ([pki-and-dns](pki-and-dns.md#dns-01-through-the-control-plane)), so the relay domain's DNS token never leaves the control plane.
- **DNS:** `*.node-<id>.<relay domain>` (or one wildcard `*.<relay domain>` covering every node, when the relay domain is one level up) points at the relay's addresses.
- **Limits:**
  - per-node bandwidth and connection caps, so one node's sandbox can't take the relay;
  - idle timeouts: 1 hour for HTTPS streams (desktops stream for hours), 24 hours for TCP;
  - metrics per node and per project.
- **Health:** the relay answers a request for a node whose tunnel is down with a TLS alert. With no certificate for the name, it can't send a page, so a clear error page needs open question 4.

### DNS for the relay domain

The relay domain belongs to the instance, not to a project, so it has its own DNS settings. Two jobs need them: pointing names at the relays (`*.node-<id>.<relay domain>`, or one wildcard for the whole domain), and the DNS-01 challenge for each tunnel node's wildcard certificate, at every issue and renewal. The second needs a DNS API, because Let's Encrypt only issues wildcards through DNS-01.

**Decided (2026-10-29): any provider from [`dns/libdns`](../plugins/dns-providers.md), set in configuration.** `LEWBOX_RELAY_DNS_PROVIDER` names the provider (any the build has, Cloudflare on lewbox.com) and `LEWBOX_RELAY_DNS_*` carries its credentials. It is the same code and contract tests that project clusters use; the credentials stay on the control plane.

Options considered:

| Option | Verdict |
|---|---|
| **A. Any `dns/libdns` provider, set in configuration** | **Chosen.** No new code; a self-hoster uses whatever DNS host they already have |
| **B. Cloudflare only** | Rejected: a needless restriction now that libdns covers many hosts |
| **C. Lewbox answers DNS for the relay domain itself.** The owner delegates a subdomain once (`relay.example.com NS <relay hosts>`), and the relay role runs a small authoritative DNS server (e.g. on `miekg/dns`, as [acme-dns](https://github.com/joohoi/acme-dns) does) that answers the relay names and the ACME TXT challenges from Postgres | **Later.** No DNS provider or API token at all, which suits a homelab. It costs a public DNS server on port 53 (UDP and TCP) on every relay, NS records for each, and hardening against abuse (rate limits, no recursion, no zone transfers) |
| **D. The relay terminates TLS** with one wildcard certificate for the relay domain, so nodes need none | **Ruled out.** The relay would decrypt all sandbox traffic, breaking end-to-end TLS between the browser and the node. (A certificate used only for an error page when a tunnel is down is a separate question, open question 4) |

### Choosing a mode

- **Create cluster** (dashboard, Admin API) asks: *Do your servers have public IP addresses?*
  - **Yes:** direct. A domain and a DNS provider are needed ([pki-and-dns](pki-and-dns.md)).
  - **No, or not sure:** tunnel, on the instance's relay domain. Nothing more to configure.
- **An instance without a relay** (`LEWBOX_RELAY_DOMAIN` unset) offers direct only.
- **lewbox.com** offers both. Its relays run on Harlew's infrastructure, and their bandwidth counts towards the project's usage ([credits](../credits.md), Beta).

### Nodes behind one public IP

With the tunnel, several nodes behind one router just work:

- each dials out;
- the cluster advertises LAN addresses (`--raft-advertise`, `--gossip-advertise`, `--internal-advertise`, `--api-advertise-url` with the LAN IP, which the installer sets from the node's outbound interface on a tunnel cluster);
- the firewall opens cluster ports to the other nodes' LAN addresses.

The enrollment rule against a shared IP applies only to direct clusters.

## API

- `POST /v0/projects/{prj}/clusters` takes `connectivity` (`direct` or `tunnel`). `direct` needs a `dns` configuration unless the instance is in development.
- `GET …/clusters/{clu}` shows `connectivity`, and for tunnel clusters the relay domain.
- `GET …/nodes/{node}` shows `connection`: `{mode, connected, connected_at, replica, tunnels: n}`.
- Errors:

| Status | Code | When |
|---|---|---|
| 422 | `relay_unavailable` | `tunnel` on an instance with no relay domain |
| 422 | `dns_required` | `direct` without DNS, outside development |
| 409 | `cluster_has_nodes` | Changing `connectivity` once a node has enrolled |
| 503 | `cluster_unavailable` | The node's tunnel is down (consumers see this on calls into the sandbox) |

## Security

- **The tunnel only reaches the node's own loopback ports** in its allow-list. A compromised control plane could reach sandboxd's API, which the PAT reaches anyway. It couldn't reach anything else on the project's network.
- **The relay is TLS pass-through.** Sandbox HTTPS and private-port cookies are between the browser and the node's Caddy and portd. The relay sees only server names, timing and byte counts. TCP ports are raw, as in direct mode: services authenticate themselves.
- **No inbound ports** in tunnel mode, so the node's SSH can be closed to the internet entirely (the firewall's `public` list is empty if the project wants).
- **The node token authenticates the tunnel.** A stolen token could impersonate the node's tunnel, so a new connection with the same token closes the older one, and that event is logged on the node page.

## Observability

- **Per node:**
  - tunnel up or down;
  - reconnects in the last hour;
  - streams open;
  - bytes in and out through the relay.
- **Per relay:**
  - connections by node and project;
  - TLS names it couldn't route;
  - streams refused by an agent (a target not on the allow-list is worth an alert).
- **Metrics:**
  - `lewbox_relay_bytes_total{project,direction}`;
  - `lewbox_tunnel_connected{node}`;
  - `lewbox_tunnel_stream_open_seconds`.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_ROLES` | `api,worker,relay` | Which roles this replica runs |
| `LEWBOX_RELAY_DOMAIN` | unset | The relay's domain, e.g. `lewbox.net`; unset disables tunnel clusters |
| `LEWBOX_RELAY_ADVERTISE` | the public URL | The address agents connect to for this replica's tunnel |
| `LEWBOX_RELAY_TCP_PORTS` | `30000-40000` | Ports the relay offers for TCP sandbox ports |
| `LEWBOX_RELAY_DNS_PROVIDER` | | The DNS provider for the relay domain: any `dns/libdns` provider the build has ([DNS for the relay domain](#dns-for-the-relay-domain)) |
| `LEWBOX_RELAY_DNS_*` | | That provider's credentials, shaped by its schema |
| `LEWBOX_TUNNEL_WINDOW` | `1MiB` | yamux stream window |

## Testing

- **Unit:**
  - tunnel stream targets (allowed and refused);
  - SNI parsing (valid, missing, malformed ClientHello);
  - the dialer chosen per node.
- **Integration:**
  - a fake node agent with a fake AerolVM behind a tunnel;
  - the full consumer suite (commands, exec stream, attach, a 1 GB file both ways, private ports, TCP SSH) run against both a direct and a tunnel node, with the same results.
- **Chaos:** kill the tunnel mid-stream; reconnect; check commands fail cleanly and terminals re-attach.
- **End to end:** two nodes in one VM network behind NAT, no inbound ports, joined as one cluster over the tunnel; sandbox URLs through a relay with a real certificate.

## Build plan

1. **M1:**
   - the `Dialer` abstraction in the AerolVM client;
   - direct mode as legacy;
   - development-only IP mode.
2. **M4:**
   - yamux over the agent's WebSocket;
   - the target allow-list;
   - `tunnels.Dial`;
   - tunnel-mode installation (LAN advertise addresses, firewall).
3. **M4:**
   - the relay role, with SNI pass-through and TCP;
   - relay DNS records;
   - DNS-01 through the control plane for tunnel nodes.
4. **M4:** dashboard and API for choosing a mode; node connection status.
5. **Beta:**
   - relay bandwidth metering;
   - per-node caps;
   - relays in more than one region.

## Open questions

1. **One tunnel per relay replica (proposed), or forwarding between replicas?** It decides how relays scale on the hosted instance.
2. **Mixed clusters:** may a direct cluster have some tunnel nodes (e.g. one node at home)? It complicates firewall rules and Raft addresses.
3. **Clusters across networks:** should the node agent manage a WireGuard mesh between a cluster's nodes, so one cluster can span sites without the project running a VPN?
4. **A friendly error when a tunnel is down:** the relay can't serve a page without a certificate for the name. Should relays hold a wildcard certificate for the relay domain to answer with an error page (and pass through everything else)?
5. **Relay bandwidth on lewbox.com:** Lewagent streams desktops at 60 fps. Is relay traffic affordable at that rate, or do Lewagent's hosted clusters stay direct?

Later: a built-in DNS server for the relay domain, so an instance needs no DNS API ([option C](#dns-for-the-relay-domain)).
