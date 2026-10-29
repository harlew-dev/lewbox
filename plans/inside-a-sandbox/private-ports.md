# Private ports

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

A private port is an `http` port that only a browser holding a link Lewbox signed can reach. It's how a project shows its users something running in a sandbox without giving that thing a login of its own: a desktop (Lewagent's Selkies on 6080), a code editor, a dev server's preview. A small proxy, `lewbox portd`, runs inside the sandbox in front of the port. Only the proxy is exposed; it checks a signed link, starts a cookie session for the browser, and forwards to the service on the sandbox's loopback.

## Today

- **Proxy:** `lewbox-gateway-legacy/internal/portd/portd.go` (313 lines), built as `cmd/lewbox-portd` for `linux/amd64` and `linux/arm64`.
- **Links:** `internal/portlink/portlink.go`. A token is `base64url(claims JSON) "." base64url(Ed25519 signature)`, with claims `{sbx, port, path, exp, sess}`. A link goes to `<port url>/__lewbox/auth?token=…`.
- **Running proxies:** `internal/privateports/privateports.go`.
  - Uploads the build to `/usr/local/lib/lewbox/portd-<sha12>`, writing a part file, then `chmod` (checked by its mode, `4f0da62`), then a move.
  - Starts it as toolbox session `lewbox-portd-<port>`, with `LEWBOX_PORTD_LISTEN=:<proxy port>`, `LEWBOX_PORTD_SANDBOX`, `LEWBOX_PORTD_PORT` and `LEWBOX_PORTD_KEYS` (the gateway's public keys).
  - Waits `GATEWAY_PORTD_READY_WAIT` (500 ms) and checks it's still running.
  - Refuses clusters without HTTPS (`CheckURL`).
- **Proxy ports:** the app chooses the lowest free port in 61000–61999 for the sandbox (`app/Actions/Sandboxes/ChooseProxyPort.php`), and refuses those ports to consumers (`app/Rules/OutsideProxyPorts.php`).
- **Key:** `GATEWAY_PORT_LINK_KEY`, a base64 32-byte Ed25519 seed, which also signs agentd tokens. Without it, private ports answer `503 private_ports_unavailable`.
- **Commits:** `201cf76` (first version), `4f0da62` (gVisor chmod), `7149a8c` (`X-Forwarded-Proto: https`), `5f3d19e` (never a second proxy).
- **Docs:** [ports § private ports](https://lewbox.com/docs/inside-a-sandbox/ports#private-ports).

**How Lewagent uses it** (`lewagent-legacy/docs/displays.md`, `ScreenController`, `PreviewController`):
- Its backend makes a link (`POST /v1/sandboxes/{sbx}/ports/{port}/links`, optional `path`) and the Screen panel loads it in an iframe.
- The cookie is third-party there (the node's host, not Lewagent's), so it relies on `SameSite=None; Partitioned`. Safari is unverified.
- On `lewbox:port-unauthorized` it offers "Open in new window".
- It sets `SELKIES_FRAME_ANCESTORS` in the sandbox's env so Selkies allows framing.
- It curls the port first (`CheckPort`), since a link to a port nothing listens on lands on the node's error page.
- Links last a minute, so Lewagent's shareable "Copy link" is its own route that mints a fresh one.

**What's wrong:**
1. **The double-proxy bug.** The toolbox's session names aren't unique, so a second start of a port's proxy made a second session. It couldn't bind, and the failed expose then stopped the first (`5f3d19e`, seen live). The fix lists sessions before starting, which is racy without the fork.
2. **Uploading into every sandbox** costs a files call per start and needs the gVisor `chmod` workaround.
3. **Links can be replayed** within their minute by anyone who sees them; the docs say "by anyone who has it".
4. **Nothing to show while the service starts**: the browser gets an error.
5. **One key** signs port links and agent tokens, and there's no rotation story.

## Goals

- Keep the model: signed links, a cookie session per browser, a proxy that knows only public keys, so nothing in the sandbox can mint a link.
- Exactly one proxy per private port, always, by construction.
- Links that work once, in an iframe, in every major browser, or say clearly why not.
- A waiting page instead of an error while the service starts.
- Rotatable signing keys.
- Later, checking at the node's edge instead of inside the sandbox.

## Design

### Pieces

```
browser ──https──▶ node edge (Caddy, TLS) ──http──▶ sandbox:61000  lewbox portd ──http──▶ 127.0.0.1:6080  service
                    <aerolvm id>-61000.node-…        checks link/cookie                    (e.g. Selkies)
```

- **`lewbox portd`** is a subcommand of the slim helper binary ([toolbox § helpers](toolbox.md#lewboxs-helpers-in-the-sandbox)). It's mounted read-only by the fork (`helpers_mount`), or uploaded as legacy does on older nodes.
- **One proxy per private port**, run as a toolbox session named `lewbox.portd.<port>` ([sessions](sessions.md#start)), internal and hidden from consumers.
- **AerolVM exposes the proxy's port**, never the private port itself.

### Proxy ports

- Chosen in the transaction that records the port as private: the lowest port in 61000–61999 not used by another of the sandbox's ports (unique index on `(sandbox_id, proxy_port)`).
- A port keeps its proxy port for its life, across starts, and frees it when it's closed or made public.
- Consumers can never expose 61000–61999 ([ports](ports.md#expose)). Templates and docs say to stay clear of them (Lewagent's displays use 6080–6095).

### Starting a proxy

`internal/ports.StartProxy`, used by expose and by every start's `ports_exposed` step:

1. **Install** the helper, if the node doesn't mount it: written to a part file, then `chmod`, checked by `Stat`, then moved to `/usr/local/lib/lewbox/helper-<version>`.
2. **Start** session `lewbox.portd.<port>` with `if_absent` semantics:
   - **With `unique_session_names`** (fork): the toolbox answers the running session of that name instead of starting another. Two racing starts get one proxy.
   - **Without it:** list the toolbox's sessions by name. If one is running, use it. Otherwise start one, wait the ready time, and check it's running. If it exited because the port was taken, another start won, so use that one. This is legacy's `5f3d19e` logic.
3. **Environment** for the proxy:

   | Variable | |
   |---|---|
   | `LEWBOX_PORTD_LISTEN` | `:<proxy port>` |
   | `LEWBOX_PORTD_SANDBOX` | `sbx_…` |
   | `LEWBOX_PORTD_PORT` | The private port |
   | `LEWBOX_PORTD_KEYS` | Comma-separated base64 Ed25519 public keys: every active key in the keyring |
   | `LEWBOX_PORTD_FRAME_ANCESTORS` | Optional, see [embedding](#embedding) |

4. **Expose the proxy port** in AerolVM as `http`, and record its URL as the port's `url`.
5. **Check HTTPS.** A URL that isn't `https` (a cluster without a domain) fails with `private_ports_unavailable`, since the cookie needs a secure host of its own.

Stopping a proxy deletes its session, which ends every browser session it started, and stops exposing its port.

### Changing access

- **Public → private:** stop exposing the port itself, choose a proxy port, start the proxy, expose the proxy.
- **Private → public:** stop the proxy and free its proxy port, then expose the port itself.
- **Private → private** (exposed again, say after a start restored it): nothing on the node. This is the case that used to start a second proxy.

### Links

`POST /v0/sandboxes/{sbx}/ports/{port}/links`, needs `sandboxes.ports.view`, which a [sandbox key](../sandboxes/sandbox-keys.md) can carry, so a project's web app can make links without its profile key.

| Field | Default | |
|---|---|---|
| `path` | `/` | Where the browser goes after signing in, with its query, e.g. `/vnc.html?autoconnect=1`. Must start with `/`, at most 2048 characters |
| `session_ttl` | 12 h | **New.** Seconds the browser session lasts, 60 to 12 h |
| `wait` | `0` | **New.** Seconds to wait for the service to listen first ([readiness](ports.md#readiness)) |

Answers `201`:

```json
{ "data": { "url": "https://…-61000.node-….example.com/__lewbox/auth?token=…", "expires_at": "…", "session_expires_at": "…" } }
```

**Claims**, signed with the keyring's current port-link key:

| Claim | |
|---|---|
| `sbx`, `port` | The sandbox and private port; the proxy checks both match its own |
| `path` | Where to go after |
| `exp` | When the link stops working: 60 s after it's made |
| `sess` | When the browser session ends |
| `jti` | **New.** A random id. The proxy keeps the ids it has used until they expire, so **a link works once** |
| `kid` | **New.** Which key signed it |
| `act` | **New.** Who made it, for the proxy's log only |

- **Errors:** `409 port_not_private` for a public port, which needs no link; `409 port_unreachable` while the sandbox starts or the proxy isn't up; `404` for a port that isn't exposed.
- **Activity:** each link writes `port.link_created`, with who made it and its `path`, never its token.

### In the browser

When the proxy gets a request:

1. **`GET /__lewbox/auth?token=…`:**
   - verify the signature against its keys, then `sbx`, `port`, `exp`, `sess` and that `jti` is unused;
   - set the cookie and answer `303` to `path`;
   - the answer has `Cache-Control: no-store` and `Referrer-Policy: no-referrer`, since the token is in the URL;
   - anything but `GET`/`HEAD` is `405`.
2. **The cookie** is `__Host-lewbox-port`, with `Secure; HttpOnly; Path=/; SameSite=None; Partitioned`.
   - Its value is the session's end and an HMAC of it, keyed by a secret the proxy makes when it starts, so restarting the proxy ends every session.
   - `Partitioned` (CHIPS) is what makes it work as a third-party cookie in an iframe in Chrome and Firefox.
3. **Every other request** needs a valid, unexpired cookie, which is removed before the request is forwarded. The service never sees it.
4. **The session ends at `sess`.** Each forwarded request's context has that deadline, so open WebSockets (a desktop's stream) close with it.
5. **Forwarding** goes to `127.0.0.1:<port>`:
   - streams unbuffered (`FlushInterval: -1`);
   - sets `X-Forwarded-For`, `X-Forwarded-Host`, and `X-Forwarded-Proto: https` (`7149a8c`), so a framework makes `https://` links;
   - keeps the request's host and path as sent.
6. **Without a session**, the proxy answers `401`:
   - **A browser's page load** gets a small page saying why. It posts `{type: "lewbox:port-unauthorized", reason}` to `window.parent`, so the embedding app can make a new link or offer a new tab.
   - **Anything else** (a WebSocket, a script's request) gets plain text.

### When the service isn't up

When forwarding fails because nothing listens (connection refused), the proxy answers a page load with `503`. The page:
- says "Starting…";
- retries itself with backoff;
- posts `{type: "lewbox:port-unavailable"}` to the parent.

Other requests get plain `503`. So a desktop link made a moment early shows a waiting page and then the desktop, without the embedding app checking first.

### Embedding

- **Partitioned cookies** work in Chrome and Firefox. Safari blocks third-party cookies in iframes unless the user has interacted with the site, so the docs recommend a cluster domain under the project's own (`*.computers.lewagent.com`), which makes the cookie first-party.
- **`frame_ancestors`** (new, on expose): a list of origins allowed to frame the port. The proxy sets `Content-Security-Policy: frame-ancestors <origins>` on page responses, replacing any `frame-ancestors` and `X-Frame-Options` the service sent. This replaces Lewagent having to configure Selkies through `SELKIES_FRAME_ANCESTORS`. Left out, the service's own headers stand.

### Keys

- **Port links are signed with an Ed25519 key** held in the [keyring](../foundation/secrets-and-encryption.md) under the purpose `port-link`, separate from agentd's tokens (`agent-upload`).
- **Rotation.** A proxy knows only the public keys it was started with, and can't fetch more, since a sandbox may not reach the control plane.
  - Each port's row records the key ids its proxy was started with (`proxy_key_ids`).
  - A link is signed with the newest key its proxy knows.
  - Rotating adds a new key, which proxies started from then on get. Running proxies keep working with their keys.
  - An old key is retired once no running proxy lists it: proxies are restarted at their sandbox's next start, or at once with `lewbox keys rotate port-link --restart-proxies`. A restart ends that proxy's browser sessions.
  - A key that leaked is revoked at once, with every proxy that knows it restarted.
- **Without a key configured**, private ports answer `503 private_ports_unavailable`. `lewbox serve` makes one on first boot, so this only happens if it's removed.

### Later: checking at the node's edge

Instead of a `portd` per port inside the sandbox, the fork's node edge (Caddy, or AerolVM's own proxy) could check links and cookies before forwarding to the port.
- **For:** no helper in the sandbox; no proxy ports; no session per port; works on Firecracker without a mount; nothing to restart in a sandbox.
- **Against:** the node, not the sandbox, holds session state; more code in the fork; every port request passes a Lewbox-aware check.
- **What carries over unchanged:** the link format and the cookie semantics, so clients don't change.

This is [open question 2](../inside-a-sandbox.md#open-questions); the alpha ships `portd`.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.ports` (expose) or `sandboxes.ports.view` (link) |
| `404` | `not_found` | No such sandbox, or the port isn't exposed |
| `409` | `port_not_private` | A link to a public port |
| `409` | `port_unreachable` | A link while the sandbox starts or the proxy isn't up |
| `422` | `validation_failed` | A `path` not starting with `/`, too long, or a `session_ttl` out of range |
| `422` | `private_ports_unavailable` | A cluster without a domain, or a proxy that couldn't start, with why |
| `503` | `private_ports_unavailable` | No signing key |

## Security

- **Nothing in the sandbox can make a link.** The proxy holds only public keys. A sandbox that replaces the proxy can't sign for any other sandbox or port, since every link names its `sbx` and `port`.
- **The proxy is replaceable from inside the sandbox** (root can kill it and listen on the proxy port itself). That's acceptable: whoever controls the sandbox controls the service anyway. The private port protects the *service* from the internet, not the internet from the sandbox. The docs say so.
- **Open redirects:** `path` is refused unless it starts with a single `/` and has no backslash (legacy's `safePath`).
- **Single-use links** (`jti`) stop a link seen in a log or a referrer from being replayed within its minute.
- **Cookies are `__Host-` prefixed**, so they're bound to the port's exact host, over HTTPS.
- **`port.link_created` in the activity log** for every link, so a project can audit who opened what.

## Observability

- **Proxy:** logs each sign-in and refusal (reason, `act`, never the token) to its session's output, readable with the session log by an admin key.
- **Metrics:** `lewbox_port_links_total`, `lewbox_port_proxies_started_total{outcome}`, `lewbox_port_proxy_reused_total` (the double-start case, caught).
- **Activity:** `port.link_created`.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_PORT_LINK_TTL` | `60s` | How long a link can be opened |
| `LEWBOX_PORT_SESSION_TTL` | `12h` | Default and maximum browser session |
| `LEWBOX_PORTD_READY_WAIT` | `500ms` | Before checking a started proxy is still running, on nodes without `unique_session_names` |

## Testing

- **portd unit tests** (port legacy's 328 lines): a valid link sets the cookie and redirects; expired, wrong sandbox, wrong port and wrong key are refused; a used `jti` is refused; the cookie is stripped before forwarding; the session deadline closes a WebSocket; `401` page and postMessage; `503` waiting page when nothing listens; `frame-ancestors` replaced; `X-Forwarded-*` set; `safePath`.
- **Starting proxies** against the fake toolbox, which (like toolboxd) starts a new session for every create: two concurrent starts end with one proxy; a restart after a stop restores it; private → public → private.
- **Links:** signing and verification across a key rotation.
- **Live check:** a desktop in an iframe from another origin, in Chrome, Firefox and Safari, before release.

## Build plan

1. Port `portlink` (add `jti`, `kid`, `act`) and `portd` (used-token set, waiting page, frame ancestors).
2. Keyring purpose `port-link`, rotation.
3. `internal/ports.StartProxy` with both strategies; private expose and access changes.
4. Links endpoint with `wait` and `session_ttl`.
5. Helper mount in the fork, and proxies started from it.

## Open questions

1. **Single-use links.** Lewagent opens the same link in an iframe and might reload it. Is single use going to bite, or is "make a new link" (which Lewagent already does on reconnect) fine?
2. **Session length per link.** Should a project be able to ask for longer than 12 hours, e.g. for a kiosk-style desktop?
3. **Safari.** Do we need a fallback for Safari's iframe cookies beyond "use a cluster domain under your own", such as the Storage Access API on the 401 page?
4. **Edge checking.** Should we plan the move to the node's edge for Beta, since it also removes the helper from Firecracker sandboxes?
