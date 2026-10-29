# Inside a sandbox

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

Everything a client does *inside* a running sandbox: run commands, keep processes running as sessions, read and write files, expose ports publicly or privately, and let people in over SSH. All of it goes from the control plane to AerolVM's daemon inside the sandbox, the **toolbox** (`toolboxd`), through the node the sandbox runs on. This plan is the overview; each part has its own plan.

## Today

In legacy, all of this lives in the **gateway** (`lewbox-gateway-legacy`), which asks the Laravel app for everything it decides:

- `internal/server/inside.go` loads the sandbox from the app (`GET sandboxes/{sbx}?key=`), checks it's running, finds its node from the cluster cache, and calls the toolbox with retries while it warms up.
- `internal/server/{commands,sessions,files,ports,sshkeys}.go` serve the routes; `websocket.go` and `sse.go` relay streams.
- `internal/aerolvm/toolbox.go` is the toolbox client, with the gVisor exit-status traps (`febb176`, `cf9813a`, `590bbc2`).
- `internal/privateports`, `internal/portd` and `internal/portlink` are private ports: a proxy uploaded into the sandbox, and Ed25519-signed links (`201cf76`, `5f3d19e`).
- The app records sessions, ports and SSH keys, chooses each private port's proxy port (`app/Actions/Sandboxes/ChooseProxyPort.php`), writes `authorized_keys` through a command it hands the gateway (`app/Sandboxes/Setup/SshSetup.php`), and stores the activity events the gateway batches to it.

The public docs at [lewbox.com/docs/inside-a-sandbox](https://lewbox.com/docs/inside-a-sandbox/commands) describe this API, and are the starting point for ours.

What's wrong with it:

1. **Two hops for every request.** Each call inside a sandbox first asks the app about the key (cached 60 s) and the sandbox, over HTTP, then calls the node. Sessions, ports and SSH keys are written back to the app over HTTP too.
2. **Workarounds for the toolbox live in Lewbox.** Exit statuses lost under gVisor, session names that aren't unique, activity counted only when a request starts, downloads read whole into memory, dropped streams that leave commands running. Each is patched around in the gateway, and some aren't patched at all (exec streams still lose exit statuses).
3. **Polling.** Watched sessions are polled every 5 s per replica; port readiness is checked by Lewagent shelling out `curl` (`lewagent-legacy/app/Actions/Computers/CheckPort.php`).
4. **Private ports are fragile.** A proxy binary is uploaded into every sandbox and run as a toolbox session. Exposing a private port again once started a second proxy that killed the first (`5f3d19e`, seen live on Lewagent).

## Goals

- Keep the API that works: the shapes, protocols and error codes in the public docs, which Lewagent and the PHP SDK are built on.
- One hop: the control plane checks the key and the sandbox in its own database, then calls the node.
- Fix toolbox problems in our [AerolVM fork](clusters/aerolvm-fork.md), and keep the Lewbox-side workaround only until every node runs a fork version that has the fix ([toolbox](inside-a-sandbox/toolbox.md#capabilities)).
- Push instead of poll: session ends, port readiness and (later) file changes come to Lewbox as events.
- Close the gaps Lewagent is waiting on: conditional file writes, file watching, port readiness, stable private-port proxies, exit codes on streams.

### Non-goals

- Running commands as users other than root. Everything in a sandbox runs as root, as today.
- Keeping commands' output or env. Lewbox keeps how a command ended, never what it printed ([activity log](activity-and-webhooks/activity-log.md)).
- An SSH server of our own in the alpha ([ssh](inside-a-sandbox/ssh.md#later-an-ssh-gateway)).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | `bash -lc` login shells as root; `~` directories; command results with `exit_code`, `stdout`, `stderr`, `duration_ms`; SSE streaming with `stdout`, `stderr`, `exit`, `error`; the WebSocket protocol (`0x01`/`0x02` framing, `resize`, `signal`, `close`, `exit`, close codes `1000`, `1001`, `1008`, `1011`); `lewbox.bearer` subprotocol auth; sessions with `watch`, log `tail`, attach replay; streamed file uploads and downloads; the entry object; public `http`/`tcp` ports, `persist`, template ports; private ports behind signed links; SSH keys written between `# lewbox begin` and `# lewbox end` |
| **Change** | Key and sandbox checks are database reads, not HTTP calls. Session ends come from the toolbox as events, not a 5 s poll. Toolbox fixes live in the fork, gated by capability. Exit codes are right on every path, streams included. Downloads stream and take `Range`. Writes can be conditional (`If-Match`). Ports can be waited on until something listens. Private-port proxies are started once, by name, and never twice. Proxy ports are chosen in the same transaction that records the port |
| **Drop** | The gateway→app internal API (`/api/gateway/v1`) and its 60 s key cache. Lewagent's `curl` port checks (replaced by [readiness](inside-a-sandbox/ports.md#readiness)). Uploading the private-port proxy into each sandbox, once the fork mounts Lewbox's helpers ([toolbox](inside-a-sandbox/toolbox.md#lewboxs-helpers-in-the-sandbox)) |
| **Add** | File watching ([files](inside-a-sandbox/files.md#watching)); conditional writes; readiness on ports; a `listening` field on ports; single-use port links; `frame_ancestors` on private ports; stable port URLs as a later goal |

## Design

### The path of a request

Every endpoint in these plans follows the same steps, in `internal/httpapi` and the [toolbox](inside-a-sandbox/toolbox.md) client:

1. **Authenticate** the key ([api/authentication](api/authentication.md)): a profile key, a sandbox key for this sandbox, or an admin key for the sandbox's project. A sandbox key for another sandbox answers `404`.
2. **Authorize**: the endpoint's permission (below).
3. **Load the sandbox** from Postgres, scoped to what the key reaches. A sandbox on a cluster the key doesn't reach answers `404`.
4. **Require it running**, except for reads that work whatever its status (listing sessions, ports and SSH keys). Otherwise `409 sandbox_not_running` with its `status`.
5. **Find its node** from the sandbox's recorded node and the cluster's connection ([clusters](clusters.md)). No node online answers `503 cluster_unavailable`.
6. **Call the toolbox**, retrying while it warms up after a start, for up to 30 s, then `503 toolbox_unavailable`.
7. **Answer**, relay the stream, or map the toolbox's error to ours ([toolbox](inside-a-sandbox/toolbox.md#errors)).
8. **Record activity**: an event in the sandbox's [activity log](activity-and-webhooks/activity-log.md), written once the request ends.

### Permissions

| Permission | Covers |
|---|---|
| `sandboxes.exec` | Commands, interactive commands, sessions (including listing and reading them, as their commands and logs can hold secrets), files, directories, watching, SSH keys |
| `sandboxes.ports` | Exposing and closing ports, public or private |
| `sandboxes.ports.view` | Making links to private ports |
| none | Listing a sandbox's ports and SSH keys |

Sandbox keys may carry `sandboxes.exec`, `sandboxes.ports` and `sandboxes.ports.view` ([sandbox keys](sandboxes/sandbox-keys.md)).

### Activity

Every request inside a sandbox can be labelled, with a `label` field where the body has one, or the `X-Lewbox-Label` header anywhere: 1 to 64 of `[a-z0-9._-]`, starting with a letter or digit. The field wins over the header. The events these plans write:

| Kind | Written by | When |
|---|---|---|
| `command.ran` | [commands](inside-a-sandbox/commands.md) | A command, streamed or interactive, ends: outcome `succeeded`, `failed`, `timed_out` or `killed`, `exit_code`, `duration_ms`, and when it didn't succeed the last 2 KB of its stderr, redacted |
| `terminal.opened`, `terminal.closed` | [commands](inside-a-sandbox/commands.md#interactive) | An interactive command with `pty` opens and closes |
| `session.started`, `session.ended`, `session.killed`, `session.signalled` | [sessions](inside-a-sandbox/sessions.md) | |
| `file.written`, `file.moved`, `file.removed`, `directory.made` | [files](inside-a-sandbox/files.md) | |
| `port.exposed`, `port.closed`, `port.link_created` | [ports](inside-a-sandbox/ports.md), [private ports](inside-a-sandbox/private-ports.md) | |
| `ssh_key.added`, `ssh_key.removed` | [ssh](inside-a-sandbox/ssh.md) | |
| `request.refused` | `internal/httpapi` middleware | Any request about a sandbox answered with an error |

Reads (downloads, listings, stats, logs) aren't logged, as legacy doesn't log them: they'd fill the log.

### Activity for the idle timeout

A sandbox's [idle timeout](sandboxes/timers-and-activity.md) counts from its last activity. Every request inside it counts when it starts, and data moving on a stream counts about once a minute while it moves; a quiet stream doesn't count, so an abandoned terminal doesn't keep a sandbox running. Today the gateway "touches" the sandbox through `toolbox/health` while data moves, because AerolVM only counts a request when it starts. The fork counts bytes on its own streams instead ([toolbox](inside-a-sandbox/toolbox.md#activity)).

### Errors

Common to every endpoint here. Each part lists its own.

| Status | Code | Means |
|---|---|---|
| `403` | `permission_denied` | The key lacks the permission. The message names it |
| `403` | `wrong_key_kind` | A sandbox key for something outside its sandbox |
| `404` | `not_found` | No such sandbox the key can see, or nothing at the path, session or port |
| `409` | `sandbox_not_running` | The sandbox isn't running. Carries `status` |
| `422` | `validation_failed` | A field is invalid. `errors` says which |
| `422` | `sandbox_refused` | The toolbox refused. The message is its own |
| `426` | `upgrade_required` | A WebSocket endpoint opened as plain HTTP |
| `502` | `sandbox_error` | The toolbox failed to do it |
| `503` | `toolbox_unavailable` | The toolbox didn't answer within 30 s, as briefly after a start |
| `503` | `cluster_unavailable` | The sandbox's node can't be reached |

## Index

| Plan | Covers |
|---|---|
| [toolbox](inside-a-sandbox/toolbox.md) | The client for `toolboxd` through AerolVM: routes, retries, login shells, `~`, exit codes under gVisor, activity, errors, capabilities by fork version, which fixes move into the fork, Lewbox's helpers in the sandbox |
| [commands](inside-a-sandbox/commands.md) | Running to completion, streaming as SSE, interactive over WebSocket, timeouts, env, labels, stderr redaction |
| [sessions](inside-a-sandbox/sessions.md) | Long-running processes: create, list, get, log and tail, attach, resize, signal, kill, `watch`, statuses, what a stop does |
| [files](inside-a-sandbox/files.md) | Download, upload, move, remove, entries, directories, paths, large files, conditional writes, watching |
| [ports](inside-a-sandbox/ports.md) | Exposing public `http` and `tcp` ports, `persist`, template ports, URLs after a start, readiness, closing |
| [private ports](inside-a-sandbox/private-ports.md) | `lewbox portd`, signed links, browser sessions, iframes, proxy ports, one proxy per port |
| [ssh](inside-a-sandbox/ssh.md) | Sandbox SSH keys, `authorized_keys`, SSH access through a `tcp` port, an SSH gateway later |

Related plans: [sandboxes](sandboxes.md) (a sandbox's life, and what a stop does to sessions and ports), [templates](templates.md) (template ports), [agents](agents.md) (agentd runs as a session, and uses the toolbox), [profiles](profiles.md) (profile SSH keys), [api/streaming](api/streaming.md) (SSE and WebSocket conventions), [clusters/aerolvm-fork](clusters/aerolvm-fork.md) (where toolbox fixes go), [lessons](lessons.md).

## Build plan

Go packages:

| Package | What |
|---|---|
| `internal/aerolvm` | The AerolVM client, including the toolbox: execute, exec stream, sessions, files, filesystem, ports, health, capabilities |
| `internal/httpapi` | Handlers: `inside_commands.go`, `inside_sessions.go`, `inside_files.go`, `inside_ports.go`, `inside_ssh.go`; the shared `runningSandbox` and `withToolbox` steps; the SSE writer and WebSocket relay |
| `internal/sessions` | Session records, the end-of-session watcher, exit-status recovery |
| `internal/ports` | Port records, proxy port allocation, re-exposing after a start, readiness |
| `internal/portd` | `lewbox portd`, the private-port proxy |
| `internal/portlink` | Signing and checking links |
| `internal/sshkeys` | Key parsing and normalizing, `authorized_keys` blocks |

Order of work, each with tests against the [fake toolbox](foundation/testing.md):

1. Toolbox client and the shared request path (`runningSandbox`, `withToolbox`, error mapping).
2. Commands: whole, then SSE, then interactive.
3. Files and directories.
4. Sessions, attach, then the watcher.
5. Public ports, then private ports with portd and links.
6. SSH keys.
7. Fork fixes, each with its capability, removing Lewbox's workaround behind it.

## Open questions

1. **Lewbox's helpers in the sandbox.** Keep uploading `portd` and `agentd` into each sandbox, or have the fork mount a read-only directory of Lewbox helpers that `lewbox node` keeps up to date, as it does `toolboxd`? ([toolbox](inside-a-sandbox/toolbox.md#lewboxs-helpers-in-the-sandbox))
2. **Private ports at the node's edge.** Should the fork check links and cookies in its own proxy in front of every port, instead of a `portd` per port inside the sandbox? ([private ports](inside-a-sandbox/private-ports.md#later-checking-at-the-nodes-edge))
3. **Stable port URLs.** Should a port's URL stay the same across stops and starts, routed to whichever node runs the sandbox? It would remove "never store a port's URL" from every client ([ports](inside-a-sandbox/ports.md#later-stable-urls)).
4. **AerolVM's own SSH.** AerolVM counts "SSH activity" and reuses sessions for "SSH attach". Is there an SSH gateway in AerolVM we can use instead of `sshd` in the image and a `tcp` port? ([ssh](inside-a-sandbox/ssh.md#later-an-ssh-gateway))
5. **Reads in the activity log.** Should downloads of large files, or of files under `/root/.ssh` or `.env`, be logged?
