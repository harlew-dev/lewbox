# Deployment

## One image, several commands

Ship **one immutable image** per release, built in CI from a tagged commit, holding every entrypoint:

| Command | Does | Run |
| --- | --- | --- |
| `api` (default) | HTTP server for the API and the built SPA (and the admin build behind its key), the background worker, the periodic cleanup, metrics flush/rollup | long-running, N replicas |
| `migrate` | applies migrations, each in a transaction, under an advisory lock (safe to run from several machines at once) | once per release, **before** the new `api` |
| `prune` | retention targets (see `privacy-and-compliance.md`) | cron, daily, one job per target |
| `admin` | operator CLI: grant/revoke admins, suspend, erase, export, revoke all sessions, generate keys | by hand |
| `seed` | demo data | never in production (it refuses) |

- **The server does not migrate on startup.** With N replicas starting at once, a migration on boot races, and a failed migration would crash-loop the whole fleet.
- **Multi-stage build:** build stage, then a slim runtime image (distroless or alpine/slim). Run as a **non-root user** with a read-only root filesystem where possible. Pin base images by digest, and scan the image in CI.
- **Bake non-secret build values** (app name, release/version) as build args. **Never bake secrets.**
- **Prefer one origin:** the same server serves pages and API, so there's no CORS and no separate frontend host. If you split them, CORS becomes an explicit allowlist (see `security`).
- **Build-time guards:** the build fails if admin code reached the public bundle, and if policy documents contain disallowed markup. Embedding, cache headers and the guards: `web-delivery.md`.

## Release order: migrations before code, expand/contract

```
1. build image vN+1
2. run `migrate` with vN+1            ← old code (vN) is still serving
3. roll the servers to vN+1 (rolling, readiness-gated)
4. later release: drop what vN+1 no longer uses
```

- Because migrations run **before** the new code, **every migration must work with the old code still running**. That's the expand/contract (parallel change) pattern:

| Change | Release 1 (expand) | Release 2 | Release 3 (contract) |
| --- | --- | --- | --- |
| Rename column `a` → `b` | add `b`; write both; backfill | read `b` | drop `a` |
| Add NOT NULL column | add nullable; write it; backfill in batches | Postgres: `CHECK (col IS NOT NULL) NOT VALID`, `VALIDATE`, then `SET NOT NULL` (no long lock) | drop the CHECK |
| Drop a table | stop using it | drop it | |
| New index on a big table | `CREATE INDEX CONCURRENTLY` (outside a transaction) | | |

- **NEVER rename or drop in the same release that stops using something.** The old pods still running would break mid-rollout.
- **Fix forward.** Down migrations destroy data. In production, roll the **code** back to the previous image (which works because the schema change was additive) and write a new forward migration. `migrate rollback` should preview only, unless `--confirm` is given.
- **Freeze migrations before the first real data.** While prototyping you may squash and edit create-migrations. From the first production data on, migrations are **append-only** and an applied migration is never edited. The tooling can't always detect an edit, so this is a team rule. Write it in the deploy doc.
- **Lock timeouts:** set `lock_timeout` (e.g. 5 s) in migrations, so a migration waiting behind a long transaction fails instead of queueing every query behind it.
- **Detect mismatches:** each server records the number of migrations it expects versus those applied. An integrity check flags servers out of step after a 30-minute release grace.

## Two database roles

```sql
CREATE ROLE app_owner LOGIN PASSWORD '…';          -- migrations only
CREATE ROLE app_runtime LOGIN PASSWORD '…';        -- servers, prune, admin CLI
CREATE DATABASE app OWNER app_owner;
\c app
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO app_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_runtime;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE app_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO app_runtime;
```

- **Runtime role:** no DDL, no `TRUNCATE`, not the owner, not a superuser. A compromised server can't drop the audit table or change the schema.
- **`migrate` uses a separate env file** naming the owner role, used only during releases.
- **The database is never reachable from the internet**, and connections use verified TLS.

## Multiple instances, no sticky sessions

**Everything that must be shared lives in the database (or a shared store).** Then any server can serve any request:

| State | Where | Across servers |
| --- | --- | --- |
| sessions, CSRF tokens | database | any server serves any request |
| security rate-limit counters | database / Redis | exact |
| background jobs | database queue claimed with `FOR UPDATE SKIP LOCKED` and a lease (e.g. 5 min) | each job runs once; a crashed worker's lease expires and another takes over |
| reset links, invitations | digests in tables | any server checks them |
| IP blocks | table, reloaded into memory every 30 s | applies everywhere within 30 s |
| HMAC-signed cookies, sealed values, code digests | keyed by the master key | **every server needs the same keys** |
| once-per-fleet jobs (alert evaluator, rollup) | advisory-lock leader | one at a time, failover in about a minute |

Per-process state is acceptable **only for load protection**: the password-hashing slots, in-memory read budgets, dedup sets. With N servers these allow N× as much. Write that down.

**NEVER** keep sessions, uploads, security counters or job state on local disk or in process memory.

## Proxies and CDN

- **Something terminates HTTPS in front.** The app speaks plain HTTP on a **private** interface, and production refuses an `http://` origin. Bind the app port so that only the proxy can reach it (private interface, firewall, or a tunnel with no inbound port).
- **Configure the client IP** (`CLIENT_IP_HEADER` + `TRUSTED_PROXIES`, see `configuration-and-secrets.md`). Without it, every per-IP limit is shared by the whole internet.
- **Proxy timeouts must be at least the server's own** (e.g. read 15 s, write 20 s). Don't let the proxy strip or override the app's security headers (CSP, `X-Frame-Options`, HSTS, `no-store`).
- **Keep secret paths out of proxy access logs.** The admin key is in `/admin/<key>`. Redact the path or exclude it.
- **Behind a CDN or WAF** (e.g. Cloudflare):
  - **lock the origin to the CDN:** firewall to its ranges, or use a tunnel. Otherwise attackers bypass every edge rule;
  - **never shared-cache** the app shell, API answers or admin assets. Hashed public assets can be cached for a year;
  - bot rules and managed challenges are fine on the SPA's own same-origin API, but **exclude the machine API** (`/api/v1/*`) from challenges. Protect it with block-only edge limits and keys;
  - put page and anonymous floods at the edge.
- **Never route `/metrics` or the admin console through public caching rules.** Put `/metrics` on its own private listener.

## Health checks

| Probe | Endpoint | Note |
| --- | --- | --- |
| load balancer / readiness | `GET /api/health` (database-checked; 503 while draining) | removes a server that can't reach the database, or is stopping |
| orchestrator liveness | `GET /api/health/live` (no database) | **never** restarts every server because the database blipped |
| external uptime | `/api/health` and `/` from 2+ regions every minute; page after 2 failures; plus TLS expiry | the only thing that can say "the app is down" |
| heartbeat | the alert evaluator pings a dead man's switch after each successful run | catches the alert loop itself being dead |

## Graceful shutdown

- **On SIGTERM:** readiness goes to 503, keep serving for `SHUTDOWN_DELAY` (5 s), drain for 10 s, then 5 s of final writes (see `observability.md`).
- **Stop timeout = `SHUTDOWN_DELAY` + 20 s.** Docker's default 10 s is too short (`--stop-timeout 25`). With a platform pre-stop hook (Kubernetes `preStop` sleep), set `SHUTDOWN_DELAY=0`.
- Background work interrupted mid-job is retried when its lease expires, so every job must be idempotent.

## Non-blocking logs

- Log JSON to stdout, and let the platform ship it (Loki, CloudWatch, Datadog). Nothing in the app sends logs anywhere.
- **Use a non-blocking log driver** with a bounded buffer: Docker `--log-opt mode=non-blocking --log-opt max-buffer-size=4m`. A synchronous driver that falls behind makes every request wait.
- **Ship logs off the host** to storage the servers can't change. Keep them 30 days, and route audit lines to a 1-year immutable store. Restrict who can read them, since logs hold IPs and user IDs.

## Scheduled jobs

- One cron line per prune target, daily, staggered (e.g. 03:15, 03:20, …), each a fresh container with the production env.
- **Alert on failure:** a job exits non-zero on failure, so have cron mail or page on it. Monitoring also warns if a target hasn't run in two days.
- During an incident, **pause destructive jobs** (prune, pseudonymise) to preserve evidence (see `incident-response.md`).

## Destructive-command guards

- **Every command that changes or deletes data previews by default** and acts only with `--confirm` (`prune`, `admin suspend`, `admin erase`, `admin sessions revoke-all`, `migrate rollback`). The preview says exactly what would happen ("would delete 1,204 events older than 2025-08-19").
- **Refuse in production:** `migrate fresh` / drop-all, `seed`, demo-account grants, `RATE_LIMITS=off`, the file mail driver.
- **Ask for reasons on the CLI too:** `--reason` with the same validator as the console. Record CLI actions as audit events with `by: cli`.
- **Verify identity out of band** before `reset-mfa`, `erase` and `export`.
- **Prefer** narrow commands (one target, one email) over broad ones. The broad break-glass command (`sessions revoke-all`) exists, but only on the CLI.

## Email, CAPTCHA and outbound

- Allow outbound SMTP (587/465) to your provider. Publish SPF, DKIM and DMARC (see `email-delivery.md`).
- Allow outbound HTTPS to the CAPTCHA verify endpoint. It's used only while a limit is in challenge mode.
- If outbound traffic is restricted by allowlist, add webhook and heartbeat receivers to it. The SSRF guard still applies.

## Security checklist

- [ ] `APP_ENV=production`, an HTTPS `APP_ORIGIN`, and verified database TLS (`sslmode=verify-full`).
- [ ] Two database roles; the runtime role has no DDL; the database isn't reachable from the internet.
- [ ] The app port is reachable only by the proxy (private interface, firewall or tunnel); behind a CDN, the origin is locked to it.
- [ ] Migrations are append-only (frozen) before the first real data; the release runs `migrate` before the new code; expand/contract followed.
- [ ] Secrets come only from the secret store (none in the image or repo); the master key is generated for this deployment, identical on every server, and **backed up with the database**.
- [ ] `CLIENT_IP_HEADER` + `TRUSTED_PROXIES` list only your proxies (or the CDN's current ranges); single-value headers are overwritten by the proxy.
- [ ] The admin key is generated, the previous slot unset outside rotations, `/admin/` out of proxy logs, no shared cache over admin assets, the allowlist or a zero-trust proxy in front, and admins granted as viewers unless they must act, reviewed quarterly.
- [ ] CAPTCHA set with your own keys (secret in the store), or `off` deliberately.
- [ ] Mail: SMTP over TLS, a verified domain with SPF, DKIM and DMARC, the password in the store; or `off` deliberately with dependent features off.
- [ ] `/metrics` unset, or on a private listener with a token, never via the public proxy.
- [ ] Logs are JSON at `info`, shipped off-host to immutable storage (30 days; audit lines 1 year), on a non-blocking driver, with access restricted.
- [ ] Readiness and liveness wired to the right probes; stop timeout = delay + 20 s.
- [ ] External uptime checks (2+ regions) and a TLS expiry check; a heartbeat to a dead man's switch; at least one admin subscribed to critical alerts; webhook receivers verify signatures; private targets disallowed.
- [ ] Every prune target scheduled daily, failures alerting, `prune.completed` visible.
- [ ] `seed` never run; dev switches refused.
- [ ] Security headers (CSP with hashes, `X-Frame-Options: DENY`, HSTS, `nosniff`, `no-store` on shell and API) survive the proxy.
- [ ] With public sign-up: terms and privacy policy written, reviewed and dated, no template markers; a privacy notice naming processors and retention.
- [ ] A breach register exists, and the incident runbook is known to whoever is on call.
