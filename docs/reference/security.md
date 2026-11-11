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

## Signing in

- The orchestrator speaks plain HTTP. Put TLS in front of it ([Run in production](../setup/run-in-production.md#put-tls-in-front)).
- A wrong email and a wrong password give the same error, and take the same time.
- Local mode (on the orchestrator's machine, with direct database access) has no sign-in, because database access already means full control. Protect that machine accordingly.

## Browser sessions

The web console signs in with `POST /v0/auth/session`, which puts the session token in an `HttpOnly` cookie instead of returning it in JSON. The cookie is `SameSite=Strict`, `Path=/`, has no `Domain`, and is `Secure` when the request arrived over TLS (or when `--cookie-secure=always` is set). TLS uses the `__Host-lewbox_session` name; plain HTTP uses `lewbox_session`. Web sessions last 7 days at most and expire after 12 hours without a request.

Cookie-authenticated requests are protected against CSRF: the orchestrator rejects state-changing cross-origin requests (`Sec-Fetch-Site` or `Origin`) with `403`, and requires `Content-Type: application/json` on cookie-authenticated requests with a body. CLI requests use a bearer token and are unaffected.

Sign-in is rate limited in memory: 20 attempts per client IP per minute, and 5 failed attempts per normalized email per minute. The limits are checked before the password hash, so a flood costs little CPU. Exhausting either returns `429` with `Retry-After`.

> [!NOTE]
> The client IP is taken from the connection, never from `X-Forwarded-For`. Behind a reverse proxy every request appears to come from the proxy, so they all share one per-IP budget. If the proxy also rewrites `Host`, add the public origin with `--trusted-origin` (or `LEWBOX_TRUSTED_ORIGINS`) so the CSRF check accepts it. If it terminates TLS without setting `X-Forwarded-Proto: https`, use `--cookie-secure=always`.

## On nodes

- The CLI refuses to run as root, and runs each privileged step through `sudo` after printing it.
- Installers are verified by SHA-256 before they run, and the verified copy is what runs.
- Secrets never go in flag values. Tokens and passwords are read from stdin (`--password-stdin`, `--admin-token-stdin`, `--dns-api-token-stdin`).
- Secrets are masked in everything Lewbox prints, including the installer's own output (which prints the operator token).
- Outside local mode, AerolVM is installed with `--pat-scope host`, so its operator token only works from the node itself and its cluster peers. In local mode AerolVM listens on `127.0.0.1` only.

> [!WARNING]
> AerolVM's installer takes the DNS token and the join token as command-line arguments. While it runs, other users on the same machine could see them with `ps`. Lewbox can't avoid this hand-off. Join tokens are single use and short-lived, which limits the risk. Run nodes on machines without untrusted local users.

## Cluster networks

Ports 7000–7003 carry cluster state and secrets between nodes. Keep them on a private network, never open to the internet. See [Requirements](../setup/requirements.md#ports).
