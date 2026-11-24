# Configuration and secrets

Production misconfiguration is a security bug that no test catches unless the server itself refuses it. **The server refuses to start rather than start insecurely**, and everything it says about its configuration is safe to paste into a ticket.

## Refuse to start when insecure

Load and validate **all** configuration at startup, in one place (e.g. `config.<ext>`), into a typed struct. Validate every rule before opening a listener, and on failure exit non-zero with a message naming the **setting**, never its value.

Typical production refusals:

| Rule | Why |
| --- | --- |
| `APP_ENV=production` must be explicit; without it the app runs in development mode | `Secure`/`__Host-` cookies, HSTS and the strict checks below hang off it |
| `APP_ORIGIN` is an exact `https://` origin with no trailing slash | Origin checks for CSRF and every emailed link are built from it, **never from the request's Host header** |
| Database TLS must verify the server (`sslmode=verify-full`, plus a CA file for providers with their own CA) | otherwise anyone on the path can impersonate the database |
| The master key (`APP_KEY`) is set, decodes to 32 bytes, and isn't the example file's | it seals MFA secrets and derives other keys |
| No dev-only switches: `RATE_LIMITS=off`, a file mail driver, `SMTP_TLS=none`, CAPTCHA test keys, demo passwords, `migrate fresh`, `seed` | each is a working hole when left on |
| Features that matter must be **explicitly** on or off: `CAPTCHA_PROVIDER=turnstile` or `off`, `MAIL_DRIVER=smtp` or `off` | "unset" must never silently mean "insecure" |
| Contradictory pairs are refused: `PER_IP_LIMITS=off` with `CLIENT_IP_HEADER` set; `MAIL_DRIVER=off` with email verification on | one of the two is a mistake |
| `/metrics` on a non-private address without a token | it would be public |
| An admin key set but no admin build; an admin key that's too short or is the example key | |
| Published policy documents still marked as templates while `POLICIES=on` | you'd publish boilerplate as your terms |
| Unknown values for enumerated settings (`ACCESS_LOG=sometimes`) | a typo must not fall back to a default |
| Renamed settings | refuse the old name with a message pointing at the new one (`MAIL_DRIVER=file` → "now called `log`") |

- **Outside production**, the same rules warn instead where that's convenient (CAPTCHA test keys, `RATE_LIMITS=off` for e2e). Log each warning at startup so it's visible.
- **Warnings that must never be hidden** (audit lines off because `LOG_LEVEL` is above `info`) are logged at `error`, so no log level suppresses them.
- **Example env file:** commit `.env.example` with throwaway values, and refuse those exact values in production. Tests use a committed `.env.testing`, never production settings.

## Secret stores

- **Secrets never live in the image, the repository or the frontend bundle.** Pass them as environment variables injected by the platform's secret store (Kubernetes Secrets backed by a KMS, AWS Secrets Manager or SSM, GCP Secret Manager, Vault, Doppler, 1Password, Fly/Render/Railway secrets), or as files mounted from it.
- **Know which settings are secrets:** master key, admin route keys (current and previous), database passwords, SMTP password, CAPTCHA secret key, metrics token, provider API tokens. Anything that grants access is a secret even if it looks like config, and the admin path key counts. The CAPTCHA **site** key is public.
- **Generate secrets with a CLI the image ships** (`admin app-key generate`, `admin route-key generate`, or `openssl rand -base64 32`) so nobody invents a weak one.
- **Never echo a secret:**
  - in logs, error messages or panics;
  - in config pages, the version endpoint, the instance table or support bundles;
  - in exceptions from parsing (`invalid DATABASE_URL: postgres://user:hunter2@…` must not happen).
  - Parse URLs that contain credentials and drop the userinfo before logging the host.
- **Secrets at rest in the database** (third-party webhook URLs, signing keys, MFA secrets):
  - seal them with AEAD (AES-GCM, XChaCha20-Poly1305) under a key **derived** from the master key: `HKDF(master, info = "<app> <purpose> v1")`, one label per purpose;
  - bind the ciphertext to its row and field as associated data, so a sealed value can't be copied to another row;
  - make them write-only in the UI.

## "Set / not set" summaries

Every server records a configuration summary (in the instance table, and on the admin config page):

```json
{ "production": true, "mailDriver": "smtp", "smtpHost": "smtp.resend.com", "smtpPort": 587, "smtpTls": "starttls",
  "captchaProvider": "turnstile", "captchaSiteKey": true, "captchaSecretKey": true, "captchaTestKeys": false,
  "clientIpHeader": "CF-Connecting-IP", "trustedProxies": 2, "adminAllowedIps": 1,
  "adminRouteKey": true, "adminRouteKeyPrevious": false, "metricsAddr": true, "metricsToken": true,
  "logFormat": "json", "logLevel": "info", "migrationsApplied": 42, "migrationsBuiltIn": 42 }
```

- **Secrets appear only as booleans:** never the value, a prefix, a length or a digest. Even a SHA-256 of a low-entropy secret can be brute-forced.
- **Build it field by field, never by reflecting over the config struct.** Otherwise a newly added secret leaks the day it's added.
- **Test it:** build the summary from a config full of known secrets and assert no 8-character run of any of them appears, raw, base64 or as a SHA-256 in hex or base64.
- **Flag drift:** settings that differ between servers, more than one version live, pending migrations. These turn into dashboard warnings, each with the fix.

## Both-or-neither settings

Some settings only make sense together. Refuse one without the other:

| Pair | Why |
| --- | --- |
| `CLIENT_IP_HEADER` + `TRUSTED_PROXIES` | a header without trusted proxies would be believed from anyone; proxies without a header do nothing |
| `SMTP_USERNAME` + `SMTP_PASSWORD` | half a credential is always a mistake |
| `CAPTCHA_SITE_KEY` + `CAPTCHA_SECRET_KEY` (with a provider) | the widget would render but every check would fail, or the reverse |
| `ROUTE_KEY_PREVIOUS` requires `ROUTE_KEY` and must differ from it | a previous key alone is meaningless |
| `METRICS_TOKEN` requires `METRICS_ADDR` | |

**Prefer** two variables over one composite one (`MAIL_FROM_NAME` + `MAIL_FROM_ADDRESS`, not `Name <addr>`, whose spaces shells and `make` parse differently).

## Trusted proxies and the client IP

TLS usually ends at a proxy, so every connection appears to come from the proxy. Get the real client IP wrong, and per-IP rate limits become site-wide, audit logs record the proxy, and admin allowlists either fail or let everyone in.

**Rules:**

- **Believe the header only on connections from `TRUSTED_PROXIES`.** Anyone can send `X-Forwarded-For: 1.2.3.4`. Ignore it from everyone else.
- **Read `X-Forwarded-For` from the right**, skipping your own trusted proxies. The first untrusted hop is the client. Entries the client added at the left are never chosen. Read at most 20 entries.
- **Single-value headers** (`X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`) are accepted only when they hold exactly one valid address. The proxy must **overwrite** them, never pass on the client's own.
- **A header that doesn't parse** falls back to the last trusted hop or the connection, never to a client-chosen value.
- **Allow-list the header names** you accept (`X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`). Refuse others. RFC 7239 `Forwarded` needs its own parser if you support it.
- **Refuse at startup:** unparseable ranges, IPv4-mapped IPv6 ranges (write them as IPv4), and `0.0.0.0/0` or `::/0`, which mean "believe anyone".
- **Resolve the IP once per request** in the outermost middleware, and use that value everywhere: limits, logs, events, admin allowlist, IP blocks.
- **IPv6** is counted by /64 in per-IP limits. Events record the full address until minimised.
- **Log the header in use at startup.** Warn (and show on the dashboard) when one IP sends over 80 % of events. That almost always means the client-IP config is missing.

| Setup | Header | Trusted proxies |
| --- | --- | --- |
| nginx or Caddy on the same host | `X-Forwarded-For` (nginx: `$proxy_add_x_forwarded_for`) | `127.0.0.1/32,::1/128` |
| nginx setting `X-Real-IP $remote_addr` | `X-Real-IP` | `127.0.0.1/32,::1/128` |
| Cloud load balancer in a private network | `X-Forwarded-For` | the LB subnet, e.g. `10.0.0.0/16` |
| Cloudflare → origin | `CF-Connecting-IP` | Cloudflare's published ranges (recheck periodically), with the origin firewalled to them |
| Cloudflare Tunnel on the same host | `CF-Connecting-IP` | `127.0.0.1/32,::1/128` |

## Key backup and rotation

| Key | If lost | If leaked | Rotation |
| --- | --- | --- | --- |
| **Master key** (`APP_KEY`: seals MFA secrets, keys code digests and email pseudonyms, signs passes) | every MFA user locked out; pseudonyms unmatchable; sealed receivers unreadable (fail closed) | treat every MFA secret and pseudonym as exposed | **Back it up with the database**, in a separate secret store, and restore them together. Every server on one database needs the same key. Design rotation in from day one: store a **key ID with each ciphertext**, keep old keys decrypt-only, re-encrypt in batches. The previous project lacked this and had to document it as a known gap |
| **Admin route key** | admins can't find the console (generate a new one) | rotate outright, **without** the previous slot | routine: current + previous slot for one session lifetime (see `admin-and-privileged-actions.md`) |
| **Webhook signing keys** | receivers can't verify | rotate | 15-minute dual-signature overlap |
| **Database, SMTP, CAPTCHA, metrics token** | service down | rotate at the provider and in the secret store, then restart | the provider's process; prefer two valid credentials during the switch |

- **Derive, don't reuse.** One master key, many purpose-specific keys via HKDF with distinct labels (`"<app> email-pseudonyms v1"`, `"<app> admin-assets v1"`). A leak of one derived key doesn't expose the others, and the label's version gives you a rotation handle.
- **Every server must share the same secrets.** One server's worker may send a code that another checks, or set a cookie another validates.

## Dev-only conveniences

- **The always-passing CAPTCHA test keys** are the default outside production, with a startup line saying so.
- **The file mail driver** is refused in production.
- **The seeder** refuses `APP_ENV=production`. It never writes rate-limit counters or metrics, because those would refuse real requests or mislead.
- **Demo passwords** come from an env var used only by the seeder.
