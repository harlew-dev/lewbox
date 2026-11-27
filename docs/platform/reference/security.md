---
title: Security
description: What Lewbox stores, how secrets are protected, and what to watch for when you run it.
---

## What Lewbox stores

| Secret | Where | How |
|---|---|---|
| Cluster admin tokens (`avm_…`) | Orchestrator database | Encrypted with AES-256-GCM using the keyring. Only the last 4 characters are ever shown |
| The keyring | `keyring.key` next to the database | `0600`. Lewbox refuses a keyring others can read |
| Passwords | Orchestrator database | PBKDF2-HMAC-SHA256, 600,000 iterations, random salt |
| Session tokens (`lbs_…`) | Database: SHA-256 hash only. Your machine: `~/.config/lewbox/credentials.json` (`0600`) | 30 days. Ended by logout, a password change, or disabling the account |
| Organization API keys (`lba_…`) | Database: SHA-256 hash, the first 12 and last 4 characters | Shown once at creation. Expire after 90 days by default. Stop at once when revoked or when the organization is suspended. Can only read their own organization |
| Browser sessions | Cookie `__Host-lewbox_session` (TLS) or `lewbox_session`, and a hashed row in the orchestrator database | 7 days at most, and 12 hours without a request |
| AerolVM operator token (PAT) | Never stored | Read on the node, used once over `127.0.0.1`, then dropped |
| Join tokens | Never stored | Single use, 15 minutes by default |

Anyone with both the database and the keyring controls every registered cluster. Keep them, and their backups, as private as the clusters themselves.

## Audit trail

Every state change the audit trail covers writes one row to the `events` table in the same transaction as the change. A row holds the actor's account ID or key ID plus the email or key name at the time, the action name, the target's public ID or name, the workspace, the changed fields, the client IP and the server's request ID. Passwords, hashes, session and access tokens, API keys, setup-link tokens, profile secrets and join tokens are never recorded. A failed sign-in records the attempt; the email typed for an address with no account is not stored.

Only instance admins can read the trail, through `GET /v0/events`, `lewbox events` or **Audit trail** in the console. It is kept 90 days by default; set `--audit-retention` (or `LEWBOX_AUDIT_RETENTION`) to a duration, or `forever`. IP addresses are not truncated before deletion yet — see [Audit trail](../operations/audit-trail.md#known-gap).

## Signing in

- The orchestrator speaks plain HTTP. Put TLS in front of it ([Run in production](../setup/run-in-production.md#put-tls-in-front)).
- A wrong email and a wrong password give the same error, and take the same time.
- Local mode (on the orchestrator's machine, with direct database access) has no sign-in, because database access already means full control. Protect that machine accordingly.

## Browser sessions

The web console signs in with `POST /v0/auth/session`, which puts the session token in an `HttpOnly` cookie instead of returning it in JSON. The cookie is `SameSite=Strict`, `Path=/`, has no `Domain`, and is `Secure` when the request arrived over TLS (or when `--cookie-secure=always` is set). TLS uses the `__Host-lewbox_session` name; plain HTTP uses `lewbox_session`. Web sessions last 7 days at most and expire after 12 hours without a request.

Cookie-authenticated requests are protected against CSRF: the orchestrator rejects state-changing cross-origin requests (`Sec-Fetch-Site` or `Origin`) with `403`, requires a per-session CSRF token (`X-CSRF-Token`, an HMAC of the session token under a key derived from the keyring) on every cookie-authenticated write, and requires `Content-Type: application/json` on cookie-authenticated requests with a body. CLI requests use a bearer token and are unaffected.

Sign-in is rate limited in memory: 20 attempts per client IP per minute, and 5 failed attempts per normalized email per minute. The limits are checked before the password hash, so a flood costs little CPU. Exhausting either returns `429` with `Retry-After`.

Every authenticated request also draws on a per-caller budget of 1,800 requests per minute. An account's sessions and access tokens share one budget, so swapping credentials does not reset it; each organization API key has its own. Signing out and changing a password are exempt, because a stolen session must not be able to spend the owner's budget and stop them recovering the account. Every window is aligned to a boundary of its length, and a per-client budget counts an IPv6 address by its /64, so neither the current time nor rotating addresses inside one network reveals or resets a budget.

Each creation path also has an aligned hourly budget, checked before anything is written or sent to a region: 5 workspaces per account, 100 sandbox keys per workspace and 100 management keys per workspace. Instance admins and local mode are exempt from the workspace budget only. Change them with `--limit-workspaces-per-hour`, `--limit-sandbox-keys-per-hour` and `--limit-admin-keys-per-hour` (or `LEWBOX_LIMIT_WORKSPACES_PER_HOUR`, `LEWBOX_LIMIT_SANDBOX_KEYS_PER_HOUR`, `LEWBOX_LIMIT_ADMIN_KEYS_PER_HOUR`); `0` turns one off. Exhausting one returns `429` with `Retry-After` and a message such as "too many sandbox keys created".

> [!NOTE]
> By default, Lewbox uses the connection's IP address for per-IP rate limits. To count clients separately behind a reverse proxy, configure the proxy's CIDR with `--trusted-proxy` or `LEWBOX_TRUSTED_PROXIES`. For connections from those CIDRs, Lewbox uses the rightmost valid address in `X-Forwarded-For` outside the trusted CIDRs. It falls back to the connection's address when the header has no such address. If the proxy rewrites `Host`, add the public origin with `--trusted-origin` or `LEWBOX_TRUSTED_ORIGINS` so the CSRF check accepts it. If the proxy terminates TLS without setting `X-Forwarded-Proto: https`, use `--cookie-secure=always`.

## On nodes

- The CLI refuses to run as root, and runs each privileged step through `sudo` after printing it.
- Installers are verified by SHA-256 before they run, and the verified copy is what runs.
- Secrets never go in flag values. Tokens and passwords are read from stdin (`--password-stdin`, `--admin-token-stdin`, `--dns-api-token-stdin`).
- Secrets are masked in everything Lewbox prints, including the installer's own output (which prints the operator token).
- Outside local mode, AerolVM is installed with `--pat-scope host`, so its operator token only works from the node itself and its cluster peers. In local mode AerolVM listens on `127.0.0.1` only.
- Every node runs AerolVM with a create policy that allows only its cluster's runtime. A Docker cluster therefore refuses a Firecracker create, and a Firecracker cluster refuses a Docker container on the host kernel, even though the customers' SDKs call the cluster directly. The policy is reloaded by AerolVM within 10 seconds of a change.

> [!WARNING]
> AerolVM's installer takes the DNS token and the join token as command-line arguments. While it runs, other users on the same machine could see them with `ps`. Lewbox can't avoid this hand-off. Join tokens are single use and short-lived, which limits the risk. Run nodes on machines without untrusted local users.

## Cluster networks

Ports 7000–7003 carry cluster state and secrets between nodes. Keep them on a private network, never open to the internet. See [Requirements](../setup/requirements.md#ports).
