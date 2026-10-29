# Ports

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

Exposing a port gives a server inside a sandbox an address outside it.
- **`http`:** an `https` URL on the node's domain, with Lewbox (the node's Caddy) handling TLS.
- **`tcp`:** a host and port from the node's pool, for SSH, databases and anything that isn't HTTP.

Any port can be **public**, reached by anyone with its address. An `http` port can be **private** instead: reached only by a browser holding a link Lewbox signed, covered in [private ports](private-ports.md). This plan covers exposing, listing, closing, persisting, template ports, what a start does, and readiness.

## Today

- **Handlers:** `lewbox-gateway-legacy/internal/server/ports.go` (420 lines): `listPorts`, `exposePort`, `exposePrivatePort`, `closeProxy`, `closePort`, `createPortLink`.
- **AerolVM:** `POST`/`DELETE /v1/sandboxes/{id}/ports/{port}` with `protocol` (`http`, `tcp`, or the unused `tls`), answering `public_url`, and for `tcp` `host` and `host_port`.
  - **URLs:** `https://<aerolvm id>-<port>.node-<id>.<domain>`.
  - **TCP pool:** `SB_L4_PORT_RANGE_START`–`END`, 22000–23000, which the install script sets and the node's firewall opens.
  - **Ports belong to the AerolVM sandbox,** so one resumed from a snapshot has none, and its URLs change, as the AerolVM id does.
- **Records:** the app's `sandbox_ports`:
  - columns `port`, `protocol`, `access`, `proxy_port`, `persist`, `public_url`, `host`, `host_port`;
  - `PortController` in the gateway API;
  - `app/Rules/OutsideProxyPorts.php` keeps 61000–61999 for proxies.
- **Persisted ports** are exposed again by every start's `ports_exposed` step. Template ports are recorded at reserve time (`ReserveSandbox.php`) and exposed by the create (`internal/operations/sandboxes.go`).
- **Docs:** [ports](https://lewbox.com/docs/inside-a-sandbox/ports).

**What's wrong:**
1. **Order of writes.** A public port is exposed in AerolVM first, then recorded; if the app refuses (say, a port in the proxy range), the gateway has to close it again. Validation that could have come first came last.
2. **Exposing again was fragile.** Exposing a port that was already private, after a start had restored its proxy, started a second proxy, which failed, and the failed expose then stopped the working proxy and forgot the port (`5f3d19e`). Lewagent stopped exposing a port twice because of it (`lewagent-legacy/docs/todo.md`).
3. **No readiness.** A link to a port nothing listens on opens the node's error page, so Lewagent runs `curl` inside the sandbox before every link (`lewagent-legacy/app/Actions/Computers/CheckPort.php`), reading the status from stdout because gVisor loses exit codes. That was Lewagent's workaround for a gap; the fork closes the gap (F53).
4. **URLs change on every start**, so every client must re-read them and never store them.

## Goals

- Keep the documented API and port object.
- Validate everything before touching the node; record and expose in an order that can't leave a port half-made.
- Exposing a port again is always safe and idempotent.
- Readiness: tell clients whether something listens, and let them wait for it.
- Later, URLs that don't change across starts.

## Design

### Data model

`sandbox_ports`:

| Column | Type | |
|---|---|---|
| `sandbox_id` | fk | Primary key with `port` |
| `port` | `int` | 1–65535, never 61000–61999 |
| `protocol` | `text` | `http`, `tcp` |
| `access` | `text` | `public`, `private` |
| `proxy_port` | `int` null | For private ports. Unique per sandbox (partial unique index); see [private ports](private-ports.md#proxy-ports) |
| `proxy_key_ids` | `text[]` | For private ports: the link keys its running proxy was started with ([private ports § keys](private-ports.md#keys)) |
| `frame_ancestors` | `text[]` | For private ports ([private ports § embedding](private-ports.md#embedding)) |
| `persist` | `bool` | Exposed again after each start |
| `source` | `text` | `api` or `template` |
| `status` | `text` | `exposing`, `exposed`, `unreachable` (sandbox not running), `failed` |
| `url`, `host`, `host_port` | null | Where it's reached now; null unless `exposed` |
| `error` | `text` null | Why the last expose failed |
| `exposed_by` | `jsonb` | Actor |
| `created_at`, `updated_at` | | |

### Expose

`PUT /v0/sandboxes/{sbx}/ports/{port}`, body optional:

| Field | Default | |
|---|---|---|
| `protocol` | `http` | `http` or `tcp` |
| `access` | `public` | `public` or `private`; private needs `http` |
| `persist` | `false` | Expose it again after each start |
| `frame_ancestors` | | Private ports only, see [private ports](private-ports.md#embedding) |

Steps:

1. **Validate first:** port 1–65535, not 61000–61999, protocol and access valid, private only for `http`, and private only on a cluster with a domain (`422 private_ports_unavailable`). Requires `sandboxes.ports`.
2. **Load the sandbox,** which must be running (`409 sandbox_not_running`).
3. **Lock the port's row** (`SELECT … FOR UPDATE`, inserting it as `exposing` if it's new) in a short transaction. Two exposes of the same port queue behind each other rather than race.
4. **Already exposed, same protocol and access:** update `persist` (and `frame_ancestors`), answer the port. Nothing is done on the node. This is the case that used to break.
5. **Other protocol:** `409 port_protocol_conflict`. Close it first.
6. **Public → private or private → public:** see [private ports](private-ports.md#changing-access). Making a private port public stops its proxy first.
7. **Public:** AerolVM `POST …/ports/{port}` with the protocol, then record `url`/`host`/`host_port` and `exposed`.
8. **Private:** choose a proxy port in the same transaction, start or reuse the proxy, expose the proxy's port, and record ([private ports](private-ports.md)).
9. **If the node refuses or fails:** a new port's row is deleted; an existing one keeps its old state. Then the error is answered.

Answers `200` with the port object. Writes `port.exposed` (or nothing for a no-op, step 4).

### The port object

As documented: `port`, `protocol`, `access`, `persist`, `url`, `host`, `host_port`. Adds:

| Field | |
|---|---|
| `status` | `exposed`, `unreachable` (sandbox not running), `failed` |
| `listening` | **New.** `true`/`false` from the last [readiness check](#readiness), or `null` when not checked |
| `checked_at` | When `listening` was checked |

### List

`GET /v0/sandboxes/{sbx}/ports`: every recorded port, whatever the sandbox's status, ordered by port. Needs no permission. A sandbox's own object carries `ports` too. While it isn't running, `url`, `host` and `host_port` are `null`, and `status` is `unreachable`.

### Close

`DELETE /v0/sandboxes/{sbx}/ports/{port}`: stops exposing it, stops a private port's proxy (ending its browser sessions), and forgets it, persisted or not.
- Works whatever the sandbox's status; on a stopped sandbox it only forgets.
- `204`, including for a port that wasn't exposed.
- Writes `port.closed`.

### Template ports

A template's `ports` (`{port, protocol, access}`, up to 20, [templates](../templates.md)) are recorded on the sandbox at create with `source: template` and `persist: true`, and exposed by the create's `ports_exposed` step. After that they're ordinary ports: changed or closed the same way. A template's ports changing later only affects sandboxes created afterwards.

### Starts and stops

- **On a stop** (any kind), in the transaction that records it: persisted ports go `unreachable` with `url`, `host`, `host_port` cleared; unpersisted ones are deleted. Private ports' browser sessions end with their proxies.
- **On a start**, the `ports_exposed` step exposes every persisted port again, private ones with their proxies started first. They get new URLs, since the AerolVM sandbox is new and may be on another node.
  - A port that fails to expose is recorded `failed` with its `error`, and doesn't fail the start.
  - The step is idempotent, so a retried start doesn't double anything ([sandboxes/operations](../sandboxes/operations.md)).
- **The server behind a port isn't started again**, as processes don't survive a stop. A template's own command, which AerolVM starts on every start, is how a template's service comes back ([sandboxes/persistence](../sandboxes/persistence.md)).
- **Webhook:** `sandbox.running` carries `ports`, so a client learns the new URLs without reading the sandbox again.

### Readiness

What Lewagent does with `curl` today, done once in Lewbox:

- **Check:** `GET /v0/sandboxes/{sbx}/ports/{port}?check=true` checks whether something listens on the port inside the sandbox, records `listening` and `checked_at`, and answers the port.
- **Wait:** `POST /v0/sandboxes/{sbx}/ports/{port}/wait` with `{"timeout": 30, "http": true}` answers once something listens, or after the timeout with the port and `listening: false`. With `http`, "listening" means an HTTP request to `127.0.0.1:<port>/` gets any response; otherwise a TCP connection is enough.
- **Links wait too.** `POST …/ports/{port}/links` takes `wait` (seconds, default 0), so making a desktop link can wait for the desktop in one call ([private ports](private-ports.md#links)).
- **How it checks:** a toolbox endpoint in the fork, `GET toolbox/ports/{port}/status` ([F53](../clusters/aerolvm-fork.md#process-and-exec-toolboxd), P1), dials `127.0.0.1:<port>` (and optionally sends `HEAD /`) from inside the sandbox, and doesn't count as activity, so polling readiness doesn't keep a sandbox awake. Lewbox doesn't probe with a command of its own: until F53 is in the minimum fork release, `check`, `wait` and a link's `wait` aren't offered (`422 validation_failed`, "port readiness isn't available on this instance yet"), and the docs say so ([D15](../README.md#decisions)).
- **portd shows a waiting page.** A private port whose service doesn't answer yet answers its browser with a small "Starting…" page that retries, and posts `lewbox:port-unavailable` to the embedding page, rather than an error ([private ports](private-ports.md#when-the-service-isnt-up)).

### URLs

- **`http`:** `https://<aerolvm id>-<port>.node-<id>.<domain>` today; whatever the fork's Caddy serves. The node's domain comes from the cluster ([clusters/pki-and-dns](../clusters/pki-and-dns.md)).
- **`tcp`:** `tcp://<node host>:<host_port>`, from the node's pool 22000–23000.
- **Clusters without a domain** expose `http` ports as `http://<node ip>/<id>/`, which is only allowed in development, and can't be private.

### Later: stable URLs

A port's URL could stay the same for the sandbox's whole life: `https://<port>-<sbx>.<cluster domain>`.
- A wildcard DNS record for the cluster points at every node (or a load balancer).
- Each node's edge (the fork's Caddy) routes a hostname to the node that runs the sandbox now, as AerolVM already forwards API calls about a sandbox to its owner (`clusterForwardWrap`).
- **For:** clients stop re-reading URLs after every start; private-port cookies survive a start (same host); Lewagent's "Reconnect" after a restart becomes a reload.
- **Against:** an extra hop when the request lands on the wrong node; the fork must carry the routing.
- `tcp` ports stay per node.

This is [open question 3](../inside-a-sandbox.md#open-questions).

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.ports` to expose or close |
| `404` | `not_found` | No such sandbox |
| `409` | `sandbox_not_running` | Exposing on a sandbox that isn't running |
| `409` | `port_protocol_conflict` | Exposed with the other protocol |
| `422` | `validation_failed` | Bad protocol or access, private `tcp`, a port out of range or in 61000–61999 |
| `422` | `private_ports_unavailable` | Private on a cluster without a domain, or a proxy that couldn't start, with why |
| `502` | `sandbox_error` | AerolVM failed to expose it |
| `503` | `toolbox_unavailable`, `cluster_unavailable` | |

## Security

- **Public means public.** Lewbox checks nothing on the way in to a public port. The docs say to authenticate in the service, or make it private. That's why exposing needs its own permission, `sandboxes.ports`, apart from `sandboxes.exec`.
- **TCP pool ports** are opened by the node's firewall for the whole range ([clusters/node-agent](../clusters/node-agent.md)); anything listening on an exposed port is on the internet.
- **Ports 61000–61999** can never be exposed directly, so a private port's service can't be reached around its proxy.

## Observability

- **Metrics:** `lewbox_ports_exposed{protocol, access}`, `lewbox_port_expose_total{outcome}`, `lewbox_port_reexpose_failed_total`, `lewbox_port_readiness_checks_total{result}`.
- **Activity:** `port.exposed`, `port.closed`.
- **Webhooks:** `activity.port.exposed`, `activity.port.closed`; ports on `sandbox.running`.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_PORT_WAIT_MAX` | `120s` | Longest a wait may be asked for |
| Proxy range | `61000–61999` | Fixed, documented; not configurable, as clients and templates avoid it |

TCP pool ranges are the node's, set by the [node agent](../clusters/node-agent.md).

## Testing

- **Expose:** public `http` and `tcp`; again with the same settings (no node call); `persist` changed; other protocol refused; private on a cluster without a domain refused; the proxy range refused before any node call.
- **Concurrency:** two concurrent exposes of one port end in one exposure.
- **Failure:** a node refusal on a new port leaves no row.
- **Close:** on running and stopped sandboxes; idempotent.
- **Stop and start:** persisted ports re-exposed with new URLs; unpersisted forgotten; a failed re-expose marks `failed` without failing the start.
- **Template ports:** recorded and exposed at create.
- **Readiness** (with F53 in the fake): listening and not, `http` probe, wait timing out, a link with `wait`; refused before F53.

## Build plan

1. Migration, `internal/ports` (store, row lock, validation).
2. Public expose, list and close handlers; the `ports_exposed` step shared with [operations](../sandboxes/operations.md).
3. Readiness, once the fork's endpoint (F53) is in the minimum release.
4. Private ports ([private ports](private-ports.md)).
5. Later: stable URLs in the fork's edge.

## Open questions

1. **Readiness on the port object.** Should a sandbox's `ports` carry `listening` always, kept current by a check after every start, or only when asked?
2. **A port that fails to re-expose after a start:** should the start's operation report it (a `ports_failed` step), or only the port's `status`?
3. **`tls` protocol.** AerolVM has `tls` (TLS passthrough). Expose it as a third protocol, e.g. for services that terminate their own TLS?
