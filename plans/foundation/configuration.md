# Configuration

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

How `lewbox` is configured: environment variables and an optional file, with every setting, its default, and what it was in legacy. It also says which settings are *instance* configuration (for whoever runs the binary) and which are *project settings* (stored in the database, changed in the dashboard or the API).

## Today

- **The app**: `.env` read by `config/*.php`. `config/lewbox.php` holds ~50 `LEWBOX_*` settings with long explanatory comments; `config/aerolvm.php` holds `AEROLVM_*` health settings. Many settings are copied onto nodes by `install.sh` when they're installed, so "changing this only affects nodes added afterwards" appears throughout.
- **The gateway**: env only (`internal/config`, `docs/configuration.md`), Go durations, with every problem reported at once. Some settings must agree with the app's (`GATEWAY_OPERATION_RENEW_INTERVAL` well under `LEWBOX_OPERATION_LEASE_SECONDS`, `GATEWAY_EVENT_BATCH_SIZE` at most `LEWBOX_ACTIVITY_BATCH_SIZE`, `GATEWAY_AEROLVM_BUILD_TIMEOUT` vs `LEWBOX_AEROLVM_IMAGE_BUILD_TIMEOUT_MINUTES`), and nothing checks they do.
- **Tenant settings** live in `tenants` columns: credit rates, free disk, activity retention, `record_command_stderr`, `git_mode`, sign-in cluster and concurrency.

## Goals

- One namespace, `LEWBOX_*`, and one table of settings in code, which generates the documentation.
- Validate everything at start, including settings that must agree, and report every problem.
- Durations as Go durations (`30s`, `5m`) everywhere. No more `_SECONDS` / `_MINUTES` / `_HOURS` suffixes.
- Settings that affect nodes are pushed to nodes by the control plane, so changing them reaches every node.
- Settings that a project should choose for itself are project settings, not env vars.

## Design

### Sources and precedence

Highest wins:

1. Command-line flags (only a few: `--addr`, `--role`, `--config`, `--migrate`, `--dev`).
2. Environment variables, `LEWBOX_*`.
3. The configuration file, if given (`--config` or `LEWBOX_CONFIG`).
4. Defaults in code.

Secrets may also be given as files: any setting `LEWBOX_X` can be `LEWBOX_X_FILE=/run/secrets/x`, read once at start (for Docker and Kubernetes secrets). Giving both is an error.

`lewbox config print` shows each effective setting, where it came from (`flag`, `env`, `file`, `default`), and secrets as `***` with their last four characters.

### File

A file mirrors the env names in nested form: `LEWBOX_DATABASE_URL` ↔ `database.url`, `LEWBOX_JOBS_CONCURRENCY` ↔ `jobs.concurrency`.

```toml
# lewbox.toml
url = "https://lewbox.example.com"

[database]
url = "postgres://lewbox@db/lewbox"

[email]
smtp_url = "smtp://user:pass@smtp.example.com:587"
from = "Lewbox <lewbox@example.com>"
```

Format: **TOML** recommended (comments, no indentation traps, `BurntSushi/toml` or `pelletier/go-toml/v2`). YAML is the alternative. Unknown keys are errors, so typos aren't silently ignored.

### Implementation

`internal/config` defines settings as data:

```go
var Settings = []Setting{
    {Name: "LEWBOX_URL", Kind: URL, Required: true, Doc: "The instance's public URL…"},
    {Name: "LEWBOX_OPERATION_LEASE", Kind: Duration, Default: "60s", Min: "15s", Doc: "…"},
    …
}
```

and loads them into a typed `Config` struct. Cross-setting checks are functions run after loading (`renew < lease/2`, `build timeout < operation timeout`, …). The same table generates `docs/self-hosting/configuration.md`, and a test fails if the doc is out of date.

### Instance configuration or project setting

| Kind | Where | Who changes it | Examples |
|---|---|---|---|
| **Instance configuration** | Env / file | Whoever runs the binary | Database, URL, email, keyring, timeouts, intervals, limits on what any project may choose |
| **Project setting** | `projects.settings` (JSONB, validated) | Project admins, in the dashboard or the API | Activity retention, recording command stderr, sign-in cluster, credit rates (Beta) |
| **Node setting** | Computed by the control plane, sent to node agents | Instance configuration, applied to every node within a minute | TCP port range, snapshot push retry, image build timeout, certificate renewal window, snapshot cleanup |

Instance configuration bounds project settings: `LEWBOX_ACTIVITY_MAX_RETENTION` caps what a project picks.

## Settings

Every setting, grouped. *Legacy* names the setting it replaces. Durations are Go durations.

Some groups belong to plugins: `LEWBOX_GITHUB_*` to `git/github`, the agents' adapter versions to their agent plugins, `LEWBOX_CREDITS_*` to `credits`, the relay's DNS provider settings and the offered providers (`[plugins."dns/libdns".settings] providers`) to `dns/libdns`. They're those plugins' instance settings, read only when the plugin is compiled in ([Plugins](#plugins)); a build without the plugin treats them as unknown keys.

### Core

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_URL` | Required | The instance's public URL, e.g. `https://lewbox.example.com`. Used for links in emails, setup links, the dashboard, OAuth callbacks, and what agentd posts to | `APP_URL`, `GATEWAY_PUBLIC_URL`, `LEWBOX_GATEWAY_URL` |
| `LEWBOX_API_URL` | `LEWBOX_URL` | Where the API is reached, if on another host, e.g. `https://api.lewbox.com` | `LEWBOX_GATEWAY_URL` |
| `LEWBOX_ENV` | `production` | `production` or `development`. Development logs text, allows plain-HTTP clusters and `localhost` webhooks, and shows sign-in codes in the log | `APP_ENV`, `GATEWAY_ENV` |
| `LEWBOX_ROLE` | `all` | `all`, or a comma-separated list of `api`, `worker` and (Beta) `relay` ([binary-and-commands](binary-and-commands.md#process-roles)) | |
| `LEWBOX_REPLICA_ID` | hostname + random | Names this process in leases and logs. Must be unique per process | `GATEWAY_REPLICA_ID` |
| `LEWBOX_CONFIG` | none | Path to a configuration file | |
| `LEWBOX_INSTANCE_ADMINS` | none | Comma-separated emails who are instance admins, in addition to those marked in the database ([projects/instance-administration](../projects/instance-administration.md)) | |
| `LEWBOX_SIGN_UP` | `open` | Who may sign up: `open`, `invite_only` (invited emails only) or `allowlist` (emails in `LEWBOX_SIGN_UP_DOMAINS`). Instance admins can override it in the dashboard ([projects/instance-administration](../projects/instance-administration.md)) | |
| `LEWBOX_SIGN_UP_DOMAINS` | none | Email domains allowed to sign up, for `allowlist` | |
| `LEWBOX_PROJECT_CREATION` | `anyone` | Who may create projects: `anyone` or `admins` (instance admins, and users they allow); overridable like `LEWBOX_SIGN_UP` | |
| `LEWBOX_INSTANCE_NAME` | `Lewbox` | Seeds the instance's display name, shown in emails and the dashboard | |

### HTTP

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_ADDR` | `:8080`, or `:$PORT` | Listen address | `GATEWAY_ADDR` |
| `LEWBOX_TRUSTED_PROXIES` | none | CIDRs whose `X-Forwarded-For` / `X-Forwarded-Proto` are believed | |
| `LEWBOX_TLS_CERT_FILE`, `LEWBOX_TLS_KEY_FILE` | none | Serve TLS directly (a homelab without a proxy) | |
| `LEWBOX_ACME_DOMAINS` | none | Get certificates from Let's Encrypt for these domains, with `autocert` | |
| `LEWBOX_READ_HEADER_TIMEOUT` | `10s` | | gateway `ReadHeaderTimeout` |
| `LEWBOX_IDLE_TIMEOUT` | `120s` | Idle keep-alive connections | gateway `IdleTimeout` |
| `LEWBOX_MAX_BODY` | `10MB` | Largest JSON body; file uploads stream and have their own limit | |
| `LEWBOX_SHUTDOWN_DRAIN` | `5s` | | |
| `LEWBOX_SHUTDOWN_TIMEOUT` | `30s` | | `GATEWAY_SHUTDOWN_TIMEOUT` |
| `LEWBOX_CORS_DASHBOARD_ORIGINS` | `LEWBOX_URL` | Origins allowed to use the session cookie. Keyed requests allow any origin ([api/authentication](../api/authentication.md)) | |

### Database

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_DATABASE_URL` | Required | Postgres connection URL | `DB_*` |
| `LEWBOX_DATABASE_MAX_CONNS` | `25` | Pool size per process. Remember the `NOTIFY` listener's own connection | |
| `LEWBOX_DATABASE_CONNECT_TIMEOUT` | `60s` | How long startup retries the first connection | |
| `LEWBOX_DATABASE_STATEMENT_TIMEOUT` | `30s` | Default `statement_timeout` for request queries | |
| `LEWBOX_MIGRATE_ON_START` | `true` | | |
| `LEWBOX_DATABASE_LISTEN_URL` | `LEWBOX_DATABASE_URL` | A direct connection for the `LISTEN` connection, when `LEWBOX_DATABASE_URL` goes through a pooler in transaction mode ([database](database.md)) | |

### Secrets

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_SECRET_KEYS` | Required | The keyring: `id:base64key` pairs, comma-separated; the first encrypts ([secrets-and-encryption](secrets-and-encryption.md)) | `APP_KEY` |
| `LEWBOX_SIGNING_KEYS` | generated, stored encrypted | Optional override for the Ed25519 keys that sign port links and agent tokens | `GATEWAY_PORT_LINK_KEY` |
| `LEWBOX_DEK_ROTATE_AFTER` | `2160h` | Make a new data-encryption key this often; `0` rotates only by hand ([secrets-and-encryption](secrets-and-encryption.md)) | |
| `LEWBOX_SIGNING_ROTATE_AFTER` | `2160h` | Make a new signing key this often | |
| `LEWBOX_SIGNING_RETIRE_AFTER` | `720h` | How long an old signing key still verifies after rotation | |

### Email

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_EMAIL_SMTP_URL` | none | `smtp://` or `smtps://` with credentials. `lewbox serve` refuses to start in production without it, unless `LEWBOX_EMAIL_LOG` is set ([projects/users-and-login](../projects/users-and-login.md)) | `MAIL_*` |
| `LEWBOX_EMAIL_FROM` | `Lewbox <no-reply@{host}>` | | `MAIL_FROM_*` |
| `LEWBOX_EMAIL_LOG` | `false` (`true` in development) | Write sign-in codes to the log at `warn` instead of sending them | `MAIL_MAILER=log` |

### Sign-in and sessions

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_LOGIN_CODE_TTL` | `10m` | How long an emailed code works | `PASSWORDLESS_*` |
| `LEWBOX_LOGIN_CODE_ATTEMPTS` | `5` | Wrong tries before a code is spent | `PASSWORDLESS_*` |
| `LEWBOX_USER_SESSION_IDLE` | `720h` | A dashboard session ends after this long unused (sliding) | `SESSION_LIFETIME` |
| `LEWBOX_USER_SESSION_MAX` | `2160h` | A dashboard session ends this long after its sign-in, however active | |
| `LEWBOX_REAUTH_WINDOW` | `15m` | Dangerous actions need a sign-in this recent ([api/authentication](../api/authentication.md)) | |
| `LEWBOX_AUTH_CACHE_TTL` | `5s` | Per-replica cache of key and session lookups; revocations invalidate it at once through `NOTIFY` | `GATEWAY_KEY_CACHE_TTL` (60 s) |

### Jobs and operations

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_JOBS_CONCURRENCY` | `64` | Jobs one process runs at once | `GATEWAY_OPERATION_CONCURRENCY` |
| `LEWBOX_JOBS_FALLBACK_POLL` | `5s` | How often workers look for jobs if a `NOTIFY` was missed | `GATEWAY_OPERATION_POLL_INTERVAL` (1 s) |
| `LEWBOX_OPERATION_LEASE` | `60s` | How long a claimed operation is held without renewal | `LEWBOX_OPERATION_LEASE_SECONDS` |
| `LEWBOX_OPERATION_RENEW_INTERVAL` | `20s` | Must be under half the lease | `GATEWAY_OPERATION_RENEW_INTERVAL` |
| `LEWBOX_OPERATION_WAIT_INTERVAL` | `2s` | Polling AerolVM while an operation waits (e.g. a snapshot upload) | `GATEWAY_OPERATION_WAIT_INTERVAL` |
| `LEWBOX_OPERATION_TIMEOUT` | `30m` | An operation fails if not done this long after it was asked for | `GATEWAY_OPERATION_TIMEOUT` |
| `LEWBOX_CAPACITY_WAIT` | `2m` | Retrying a cluster without room before `cluster_capacity_exceeded` | `GATEWAY_CAPACITY_WAIT` |
| `LEWBOX_CREATE_WAIT` | `60s` | How long a create waits before answering 202 | `GATEWAY_CREATE_WAIT` |
| `LEWBOX_CLONE_TIMEOUT` | `5m` | All of a create's repositories together | `GATEWAY_CLONE_TIMEOUT` |
| `LEWBOX_CLONE_CONCURRENCY` | `4` | Repositories cloned at once in one sandbox ([sandboxes/repositories](../sandboxes/repositories.md)) | |
| `LEWBOX_OPERATION_RETRY_MIN`, `LEWBOX_OPERATION_RETRY_MAX` | `1s`, `30s` | Backoff between an operation's retries ([sandboxes/operations](../sandboxes/operations.md)) | gateway constants |
| `LEWBOX_OPERATION_WAIT_MAX` | `60s` | The longest `?wait=` on reading an operation | |
| `LEWBOX_CAPACITY_PARALLEL` | `2` | Operations waiting for capacity retried at once per cluster ([clusters/placement-and-capacity](../clusters/placement-and-capacity.md)) | |
| `LEWBOX_EVENTS_RETENTION` | `24h` | How long the live-change outbox keeps events, so streams can resume ([api/streaming](../api/streaming.md)) | |

### AerolVM

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_AEROLVM_TIMEOUT` | `30s` | One ordinary call to a node | `GATEWAY_AEROLVM_TIMEOUT` |
| `LEWBOX_AEROLVM_LONG_TIMEOUT` | `10m` | Create, start, stop, destroy, snapshot | `GATEWAY_AEROLVM_LONG_TIMEOUT` |
| `LEWBOX_AEROLVM_DEFAULT_VERSION` | from the embedded fork manifest | The fork version new clusters get ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)) | `AEROLVM_SUPPORTED_VERSION` |
| `LEWBOX_CLUSTER_CACHE_TTL` | `5m` | Cluster connection cache; invalidated by `NOTIFY` on change | `GATEWAY_CLUSTER_CACHE_TTL` |
| `LEWBOX_AEROLVM_RELEASES_URL` | `https://github.com/harlew-dev/aerolvm/releases/download` | Where node agents download the fork's assets, so an air-gapped instance can mirror them ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)) | |
| `LEWBOX_ALLOWED_ISOLATION` | `container` (the alpha supports nothing else) | Isolation levels projects may offer on their clusters, among those this release supports ([clusters/runtimes](../clusters/runtimes.md)) | |

### Node settings (sent to node agents)

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_NODE_TCP_PORTS` | `22000-23000` | Node ports for TCP sandbox ports, opened in the firewall | `LEWBOX_AEROLVM_TCP_PORTS_FROM/TO` |
| `LEWBOX_NODE_IMAGE_BUILD_TIMEOUT` | `20m` | AerolVM's own limit on each of a build and its push | `LEWBOX_AEROLVM_IMAGE_BUILD_TIMEOUT_MINUTES` |
| `LEWBOX_NODE_SNAPSHOT_PUSH_RETRY` | `30s` | | `LEWBOX_AEROLVM_SNAPSHOT_PUSH_RETRY_INTERVAL_SECONDS` |
| `LEWBOX_NODE_SNAPSHOT_PUSH_MAX_IN_FLIGHT` | `2` | | `LEWBOX_AEROLVM_SNAPSHOT_PUSH_MAX_IN_FLIGHT` |
| `LEWBOX_NODE_HEARTBEAT_INTERVAL` | `20s` | How often agents report | install.sh timer, 1 m |
| `LEWBOX_NODE_OFFLINE_AFTER` | `3m` | No heartbeat this long: offline | `AEROLVM_OFFLINE_AFTER_MINUTES` |
| `LEWBOX_NODE_METRICS_RAW_RETENTION` | `48h` | Per-minute node metrics ([clusters/health-and-metrics](../clusters/health-and-metrics.md)) | `AEROLVM_METRICS_RETENTION_DAYS` (14 days, one table) |
| `LEWBOX_NODE_METRICS_5M_RETENTION` | `336h` | 5-minute roll-ups | |
| `LEWBOX_NODE_METRICS_1H_RETENTION` | `9600h` | Hourly roll-ups | |
| `LEWBOX_NODE_LOST_AFTER` | `24h` | Offline this long: the node is `lost` and its running sandboxes `failed` ([sandboxes/reconciler](../sandboxes/reconciler.md)) | |
| `LEWBOX_NODE_MIN_MEMORY_MB`, `LEWBOX_NODE_MIN_DISK_GB` | `4096`, `40` | Installation preflight minimums ([clusters/installation](../clusters/installation.md)) | install.sh checks |
| `LEWBOX_NODE_SEED_WAIT` | `30m` | How long joining nodes wait for the cluster's seed | install.sh |
| `LEWBOX_NODE_DISK_RESERVE_GB` | `50` | Disk kept back from placement for images and the build cache ([clusters/placement-and-capacity](../clusters/placement-and-capacity.md)) | |
| `LEWBOX_MEMBERSHIP_SYNC_INTERVAL` | `30s` | How often cluster membership is compared with AerolVM's | |
| `LEWBOX_ORPHAN_DESTROY` | `false` | Destroy AerolVM sandboxes Lewbox has no record of, after the grace; off for the first release, report only ([sandboxes/reconciler](../sandboxes/reconciler.md)) | |
| `LEWBOX_ORPHAN_GRACE` | `15m` | How long an orphan is left before it's destroyed (when on) | |
| `LEWBOX_INSTALL_TOKEN_TTL` | `1h` | How long an install command works | `LEWBOX_INSTALLATION_TOKEN_LIFETIME_MINUTES` |
| `LEWBOX_CERTIFICATE_LIFETIME` | `2160h` | 90 days | `LEWBOX_CERTIFICATE_LIFETIME_DAYS` |
| `LEWBOX_CERTIFICATE_RENEW_WITHIN` | `720h` | 30 days | `LEWBOX_CERTIFICATE_RENEW_WITHIN_DAYS` |
| `LEWBOX_CERTIFICATE_WARNING` | `336h` | 14 days: notify the project | `LEWBOX_CERTIFICATE_WARNING_DAYS` |
| `LEWBOX_REGISTRY_ADDRESS` | `127.0.0.1:5000` | Each node's registry | `lewbox.cluster.registry.address` |
| `LEWBOX_REGISTRY_GC_GRACE` | `6h` | Don't delete bucket objects younger than this ([clusters/storage-and-registry](../clusters/storage-and-registry.md)) | `LEWBOX_REGISTRY_CLEANUP_GRACE_HOURS` |
| `LEWBOX_REGISTRY_GC_INTERVAL` | `1h` | How often the registry's bucket is collected | `clusters:clean-up-registries` (hourly) |
| `LEWBOX_REGISTRY_VERSION` | `3.1.2` | The CNCF Distribution version node agents run | install.sh |

#### DNS (the `dns/libdns` plugin)

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_DNS_PROVIDERS` | `cloudflare` in the alpha; after it, every provider the build has | Providers offered to projects ([plugins/dns-providers](../plugins/dns-providers.md)) | Cloudflare only |

#### Tunnel and relay (Beta)

| Setting | Default | Meaning |
|---|---|---|
| `LEWBOX_RELAY_DOMAIN` | none | The relay's domain, e.g. `lewbox.net`; unset, tunnel clusters aren't offered ([clusters/connectivity](../clusters/connectivity.md)) |
| `LEWBOX_RELAY_ADVERTISE` | `LEWBOX_URL` | The address node agents connect to for this replica's tunnel |
| `LEWBOX_RELAY_TCP_PORTS` | `30000-40000` | Ports the relay offers for TCP sandbox ports |
| `LEWBOX_RELAY_DNS_PROVIDER` | none | The relay domain's DNS provider: any `dns/libdns` provider the build has ([clusters/connectivity](../clusters/connectivity.md#dns-for-the-relay-domain)) |
| `LEWBOX_RELAY_DNS_*` | | That provider's credentials, shaped by its schema |
| `LEWBOX_TUNNEL_WINDOW` | `1MiB` | yamux stream window |

Dashboard chart settings (`AEROLVM_METRICS_CHART_MAX_POINTS`, `AEROLVM_UPTIME_DAYS`, `AEROLVM_UPTIME_BUCKET_HOURS`) become constants in the dashboard's API, not settings.

### Sandboxes

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_SANDBOX_DEFAULT_CPU` | `2` | vCPUs when neither the request nor template says | `LEWBOX_SANDBOX_DEFAULT_VCPUS` |
| `LEWBOX_SANDBOX_DEFAULT_MEMORY` | `4096` | MB | `LEWBOX_SANDBOX_DEFAULT_MEMORY_MB` |
| `LEWBOX_SANDBOX_DEFAULT_DISK` | `20` | GB | `LEWBOX_SANDBOX_DEFAULT_DISK_GB` |
| `LEWBOX_SANDBOX_DEFAULT_IDLE_TIMEOUT` | none | | same |
| `LEWBOX_SANDBOX_DEFAULT_LIFETIME` | none | | same |
| `LEWBOX_IDEMPOTENCY_TTL` | `24h` | How long an `Idempotency-Key` is remembered | `LEWBOX_SANDBOX_IDEMPOTENCY_HOURS` |
| `LEWBOX_SANDBOX_KEY_TTL` | `15m` | | `LEWBOX_SANDBOX_KEY_MINUTES` |
| `LEWBOX_SANDBOX_KEY_MAX_TTL` | `60m` | The longest `ttl` a sandbox key may ask for ([sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md)) | |
| `LEWBOX_SANDBOX_MAX_REPOSITORIES` | `20` | Repositories a create may clone ([sandboxes/repositories](../sandboxes/repositories.md)) | `LEWBOX_GIT_MAX_REPOSITORIES` |
| `LEWBOX_SANDBOX_MAX_TIMER` | `720h` | Upper bound on `idle_timeout` and `lifetime` | AerolVM's `MaxLifecycle` |
| `LEWBOX_SETUP_TIMEOUT` | `120s` | A sandbox's whole profile setup script ([sandboxes/profile-setup](../sandboxes/profile-setup.md)) | |
| `LEWBOX_SETUP_COMMAND_TIMEOUT` | `30s` | Each command in it | |
| `LEWBOX_SNAPSHOT_MAX_LAYERS` | `32` | Layers above the base image before a stop flattens the snapshot ([sandboxes/persistence](../sandboxes/persistence.md)) | |
| `LEWBOX_SNAPSHOT_GRACE` | `6h` | How long a superseded snapshot is kept before it's deleted | |
| `LEWBOX_RECONCILE_INTERVAL` | `60s` | `5m` on clusters whose fork release sends lifecycle events (`LEWBOX_RECONCILE_INTERVAL_WITH_EVENTS`) | `GATEWAY_RECONCILE_INTERVAL` |
| `LEWBOX_IDLE_WARN_BEFORE` | `5m` | Default `warn_before`: when `sandbox.idle_warning` is sent before an idle stop ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop)) | new |
| `LEWBOX_TIMER_BACKSTOP_GRACE` | `10m` | How long after a timer runs out the node waits for Lewbox before stopping the sandbox itself; at least a sandbox's `stop_grace` + 5 m | new |
| `LEWBOX_HOOKS_DEFAULT_TIMEOUT` | `300s` | A create or start hook's timeout when none is given ([sandboxes/hooks](../sandboxes/hooks.md#configuration)) | new |
| `LEWBOX_HOOKS_MAX_TIMEOUT` | `1h` | The most a create or start hook may ask for | new |
| `LEWBOX_HOOKS_DEFAULT_STOP_GRACE` | `60s` | `stop_grace` when none is given | new |
| `LEWBOX_HOOKS_MAX_STOP_GRACE` | `10m` | The most `stop_grace` may be | new |
| `LEWBOX_HOOKS_FORCED_STOP_GRACE` | `30s` | The most on-stop hooks get on a stop for credits, a budget or a suspension | new |
| `LEWBOX_HOOKS_MAX_PER_LIST` | `20` | Hooks per list, template's and sandbox's together | new |
| `LEWBOX_SCRUB_MAX_PATHS` | `50` | Scrub paths per sandbox, template's included | new |
| `LEWBOX_SESSION_WATCH_INTERVAL` | `5s` | | `GATEWAY_SESSION_WATCH_INTERVAL` |

### Inside a sandbox

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_TOOLBOX_WAIT` | `30s` | How long a request waits for a just-started sandbox's toolbox to be ready (the fork's `toolbox_ready`, F4) | `GATEWAY_TOOLBOX_WAIT` |
| `LEWBOX_COMMAND_DEFAULT_TIMEOUT` | `60s` | | gateway constant |
| `LEWBOX_COMMAND_MAX_TIMEOUT` | `1h` | | `GATEWAY_COMMAND_MAX_TIMEOUT` |
| `LEWBOX_ACTIVITY_TOUCH_INTERVAL` | `60s` | Most often Lewbox touches a sandbox for activity only it sees (agent turns, the touch endpoint), and how recent stream traffic must be for an idle re-check to count it ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md)). Streams aren't touched: the fork counts them (F5) | `GATEWAY_ACTIVITY_INTERVAL` |
| `LEWBOX_COMMAND_MAX_OUTPUT` | `16MiB` | Per stream, for whole commands ([inside-a-sandbox/commands](../inside-a-sandbox/commands.md)) | |
| `LEWBOX_STREAM_START_TIMEOUT` | `10s` | How long `commands/stream` waits for its first message | |
| `LEWBOX_SESSION_START_GRACE` | `60s` | How long a session the toolbox doesn't know yet stays `running` ([inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md)) | |
| `LEWBOX_SESSION_WATCH_CONCURRENCY` | `8` | Sandboxes whose watched sessions are polled at once | gateway constant |
| `LEWBOX_FILE_MAX_DOWNLOAD` | `512MiB` | Largest download until the fork streams them ([inside-a-sandbox/files](../inside-a-sandbox/files.md)) | |
| `LEWBOX_FILE_LIST_MAX_ENTRIES` | `50000` | Cap on a deep directory listing | |
| `LEWBOX_FILE_WATCHES_PER_SANDBOX` | `8` | File watches at once (when file watching ships) | |
| `LEWBOX_SSH_ADDR` | none | Later: where the SSH gateway listens ([inside-a-sandbox/ssh](../inside-a-sandbox/ssh.md)) | |
| `LEWBOX_WEBSOCKET_PING_INTERVAL` | `30s` | | `GATEWAY_WEBSOCKET_PING_INTERVAL` |
| `LEWBOX_SSE_KEEPALIVE` | `15s` | | `GATEWAY_SSE_KEEPALIVE` |
| `LEWBOX_FILE_MAX_UPLOAD` | `5GB` | | |
| `LEWBOX_HELPER_INSTALL_DIR` | `/usr/local/lib/lewbox` | | hard-coded |
| `LEWBOX_SSH_KEYS_PER_PROFILE` | `20` | | `LEWBOX_SSH_KEYS_PER_PROFILE` |
| `LEWBOX_SSH_KEYS_PER_SANDBOX` | `20` | | `LEWBOX_SSH_KEYS_PER_SANDBOX` |

### Ports

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_PORT_PROXY_PORTS` | `61000-61999` | Ports inside a sandbox kept for portd | `LEWBOX_SANDBOX_PROXY_PORTS_FROM/TO` |
| `LEWBOX_PORT_LINK_TTL` | `60s` | How long a link can be opened | `GATEWAY_PORT_LINK_TTL` |
| `LEWBOX_PORT_SESSION_TTL` | `12h` | The browser session a link starts | `GATEWAY_PORT_SESSION_TTL` |
| `LEWBOX_PORTD_READY_WAIT` | `500ms` | | `GATEWAY_PORTD_READY_WAIT` |
| `LEWBOX_PORT_WAIT_MAX` | `120s` | The longest a port readiness wait may ask for ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md)) | |

Private ports are always on: the signing key always exists ([secrets-and-encryption](secrets-and-encryption.md#signing-keys)) and portd is embedded. They need a cluster with a domain.

### Agents

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_AGENTS_REPLY_TIMEOUT` | `10m` | How long an approval or question waits | `LEWBOX_AGENTS_REPLY_TIMEOUT_SECONDS` |
| `LEWBOX_AGENTS_DEFAULT_DIRECTORY` | `/root/workspace` | | same |
| `LEWBOX_AGENTS_CHUNK_MAX_BYTES` | `65536` | | same |
| `LEWBOX_AGENTS_QUEUE_MAX` | `20` | | same |
| `LEWBOX_AGENTS_OUTPUT_SCHEMA_MAX_BYTES` | `65536` | | same |
| `LEWBOX_AGENTS_RETENTION` | none | Prune conversations idle this long; none keeps them | `LEWBOX_AGENTS_RETENTION_DAYS` |
| `LEWBOX_AGENTD_START_WAIT` | `10s` | | `GATEWAY_AGENTD_START_WAIT` |
| `LEWBOX_AGENT_TOKEN_TTL` | `24h` | agentd's channel token | `GATEWAY_AGENT_TOKEN_TTL` |
| `LEWBOX_AGENTS_CLAUDE_ADAPTER` | pinned version | e.g. `@agentclientprotocol/claude-agent-acp@0.94.0` | agentd env |
| `LEWBOX_AGENTS_CODEX_ADAPTER` | pinned version | e.g. `@agentclientprotocol/codex-acp@2.2.2` | agentd env |

`LEWBOX_AGENTS_BATCH_SIZE` is dropped: chunks are written directly.

### Agent sign-ins

| Setting | Default | Legacy |
|---|---|---|
| `LEWBOX_SIGN_IN_HELPER_CPU` | `1` | `LEWBOX_SIGN_IN_HELPER_MILLICORES` (1000) |
| `LEWBOX_SIGN_IN_HELPER_MEMORY` | `2048` | `LEWBOX_SIGN_IN_HELPER_MEMORY_MB` |
| `LEWBOX_SIGN_IN_HELPER_DISK` | `10` | `LEWBOX_SIGN_IN_HELPER_DISK_GB` |
| `LEWBOX_SIGN_IN_HELPER_LIFETIME` | `20m` | `LEWBOX_SIGN_IN_HELPER_LIFETIME_MINUTES` |
| `LEWBOX_SIGN_IN_TTL` | `15m` | `LEWBOX_SIGN_IN_EXPIRES_AFTER_MINUTES` |
| `LEWBOX_SIGN_IN_START_TIMEOUT` | `3m` | `LEWBOX_SIGN_IN_START_TIMEOUT_SECONDS` |
| `LEWBOX_SIGN_IN_QUEUE_TTL` | `10m` | `LEWBOX_SIGN_IN_QUEUE_MINUTES` |
| `LEWBOX_SIGN_IN_CHATGPT_REFRESH_AFTER` | `72h` | `LEWBOX_SIGN_IN_REFRESH_AFTER_DAYS` |
| `LEWBOX_SIGN_IN_LINK_WAIT` | `60s` | new: the agent's CLI must print its sign-in link within this |
| `LEWBOX_SIGN_IN_PROVIDERS` | `anthropic_subscription,chatgpt_subscription` | new: subscription sign-ins the instance allows ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md#legal)) |

### Setup links

| Setting | Default | Legacy |
|---|---|---|
| `LEWBOX_SETUP_LINK_TTL` | `168h` | `LEWBOX_SETUP_LINK_EXPIRES_AFTER_MINUTES` |
| `LEWBOX_SETUP_LINK_SESSION_TTL` | `1h` | `LEWBOX_SETUP_LINK_SESSION_MINUTES` |

### Templates

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_TEMPLATE_BUILD_TIMEOUT` | `45m` | One template build and its push; builds' operations get a 60 m deadline ([templates/builds](../templates/builds.md)) | `GATEWAY_AEROLVM_BUILD_TIMEOUT` (25 m) |
| `LEWBOX_TEMPLATE_BUILD_GRACE` | `6h` | How long superseded builds' images are kept before they're deleted | |
| `LEWBOX_TEMPLATE_CONTEXT_MAX` | `64MiB` compressed | Largest build context (after the alpha, with the fork's build contexts) | |
| `LEWBOX_TEMPLATE_LOG_MAX` | `4MiB` | Build log kept per build, once the fork streams build logs | |

### Activity

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_ACTIVITY_DEFAULT_RETENTION` | `720h` | New projects' retention | `LEWBOX_ACTIVITY_RETENTION_DAYS` |
| `LEWBOX_ACTIVITY_DEFAULT_SUCCEEDED_COMMAND_RETENTION` | `72h` | | `LEWBOX_ACTIVITY_SUCCEEDED_COMMAND_RETENTION_DAYS` |
| `LEWBOX_ACTIVITY_MAX_RETENTION` | `2160h` | Cap on what a project may choose | `LEWBOX_ACTIVITY_MAX_RETENTION_DAYS` |
| `LEWBOX_ACTIVITY_CLIENT_EVENTS_PER_MINUTE` | `60` | Per key | same |
| `LEWBOX_ACTIVITY_QUEUE` | `50000` | The in-process activity writer's queue; past 80%, successful commands are shed first ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)) | `GATEWAY_EVENT_QUEUE_SIZE` |
| `LEWBOX_AUDIT_DEFAULT_RETENTION` | `8760h` | New projects' audit log retention ([foundation/observability](observability.md)) | |
| `LEWBOX_AUDIT_MAX_RETENTION` | `17520h` | Cap on what a project may choose | |

`LEWBOX_ACTIVITY_BATCH_SIZE`, `GATEWAY_EVENT_FLUSH_INTERVAL`, `GATEWAY_EVENT_QUEUE_SIZE` and `GATEWAY_EVENT_BATCH_SIZE` are dropped.

### Webhooks

| Setting | Default | Legacy |
|---|---|---|
| `LEWBOX_WEBHOOKS_TIMEOUT` | `10s` | `lewbox.webhooks.timeout_seconds` |
| `LEWBOX_WEBHOOKS_RETRY_DELAYS` | `1m,5m,30m,2h,6h,12h,24h` | `retry_delays_seconds` |
| `LEWBOX_WEBHOOKS_DISABLE_AFTER` | `72h` | `disable_after_hours` |
| `LEWBOX_WEBHOOKS_PREVIOUS_SECRET_TTL` | `24h` | `previous_secret_hours` |
| `LEWBOX_WEBHOOKS_ALLOWED_PRIVATE_CIDRS` | none (any private range in development) | Private ranges endpoints may resolve to, for a homelab receiver on the LAN; the SSRF guard refuses every other private address ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)) |
| `LEWBOX_WEBHOOKS_ALLOW_HTTP` | `false` | Allow `http` endpoint URLs |
| `LEWBOX_WEBHOOKS_MAX_ENDPOINTS` | `20` | Endpoints per project, unless an instance quota says less |
| `LEWBOX_WEBHOOKS_RESPONSE_BODY_BYTES` | `1024` | How much of a receiver's response body is kept |

`in_flight_seconds` and the `webhooks:send-due` backstop are dropped: deliveries are jobs.

### Git

| Setting | Default | Legacy |
|---|---|---|
| `LEWBOX_GIT_LIST_CACHE_TTL` | `60s` | `LEWBOX_GIT_LISTED_*_CACHE_SECONDS` |
| `LEWBOX_GIT_LIST_MAX` | `1000` | `LEWBOX_GIT_LISTED_*_MAX` |
| `LEWBOX_GIT_TOKEN_REFRESH_BEFORE` | `15m` | `LEWBOX_GIT_GITHUB_APP_TOKEN_REFRESH_BEFORE_MINUTES` |
| `LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` | `25` | none (legacy: one person per profile, installations unlimited). Git people and accounts together, per profile, across every Git provider ([git/github-app](../git/github-app.md#the-limit)) |
| `LEWBOX_GIT_MAX_SANDBOX_REPOSITORIES` | `100` | new: repositories one sandbox's Git setting may name ([git/tokens](../git/tokens.md)) |
| `LEWBOX_GIT_TOKENS_PER_MINUTE` | `120` | new: tokens the token API issues per profile per minute |
| `LEWBOX_GIT_ISSUED_TOKENS_RETENTION` | `720h` | new: how long the record of an issued token (never the token) is kept |

`LEWBOX_GIT_LISTED_*_PER_PAGE` become API page sizes. The GitHub App's permissions stay in code. Legacy's push settings (`LEWBOX_GIT_PUSH_*`) are gone with pushes ([git](../git.md)).

#### GitHub (the `git/github` plugin)

| Setting | Default | Meaning |
|---|---|---|
| `LEWBOX_GITHUB_URL` | `https://github.com` | GitHub's web URL ([git/github-app](../git/github-app.md)) |
| `LEWBOX_GITHUB_API_URL` | `https://api.github.com` | GitHub's API, for a fake or, later, GitHub Enterprise |
| `LEWBOX_GITHUB_WEBHOOK_MAX_BODY` | `25MB` | Largest inbound GitHub webhook ([activity-and-webhooks/github-webhooks](../activity-and-webhooks/github-webhooks.md)) |
| `LEWBOX_GITHUB_DELIVERIES_RETENTION` | `168h` | How long inbound deliveries are kept |
| `LEWBOX_GITHUB_FORWARD_MAX_PAYLOAD` | `512KB` | Largest GitHub payload forwarded inside a `github.<event>` webhook; larger ones are fetched by id |

### Rate limits

| Setting | Default | Meaning | Legacy |
|---|---|---|---|
| `LEWBOX_RATE_LIMITS` | the built-in table of classes in [api/rate-limits](../api/rate-limits.md) | Overrides per class, as a small JSON or YAML map: `{"api": {"limit": 2400, "burst": 400}}` | `LEWBOX_ADMIN_API_REQUESTS_PER_MINUTE`, `LEWBOX_NODE_API_*`, the passwordless package's limits |
| `LEWBOX_RATE_LIMITS_DISABLED` | `false` | For tests and private instances | |

How they're counted across replicas: [api/rate-limits](../api/rate-limits.md).

### Plugins

Which compiled-in plugins are on, how projects get them, and each plugin's instance settings ([plugins/building-and-enabling](../plugins/building-and-enabling.md#configuration)). Defaults come from `plugins/config.json`. **This is the only place they're set**: there's no API or dashboard switch for the instance (decided 2026-10-29); changing them is a configuration change and a rolling restart.

| Setting | Default | Meaning |
|---|---|---|
| `LEWBOX_PLUGINS_<NAME>_ENABLED` | from `config.json` (every shipped plugin `true`) | On for the instance. `<NAME>` is the plugin's name upper-cased with `/` and `-` as `_`: `LEWBOX_PLUGINS_GIT_GITHUB_ENABLED` |
| `LEWBOX_PLUGINS_<NAME>_PROJECTS` | from `config.json` | `all` (on for every project; projects can't turn it off), `opt_out`, or `opt_in`. Shipped: `limits`, `dns/libdns`, `storage/s3` `all`; agents, `git/github`, `telemetry` `opt_out`; `credits` `opt_in` |
| `LEWBOX_PLUGINS_<NAME>_SETTINGS_<KEY>` | the plugin's schema | One instance setting of the plugin; a plugin may also name its settings' environment variables itself (`credits`' `LEWBOX_CREDITS_*` below) |
| `LEWBOX_PLUGINS_DISABLE` | none | Comma-separated plugins to turn off, a shortcut: `agents/codex,credits` |
| `LEWBOX_PLUGINS_ENABLE` | none | Comma-separated plugins to turn on |
| `LEWBOX_PLUGINS_ALLOW_MISSING` | none | Plugins configured or in use (by clusters) that this build lacks, acknowledged so start-up continues |

In the file, a section per plugin:

```toml
[plugins.credits]
enabled  = true
projects = "opt_in"

[plugins.credits.settings]
checkpoint_interval = "1h"

[plugins."agents/codex"]
enabled = false
```

A plugin named in configuration that the build doesn't have is an error (unless `enabled = false`); so is an unknown key in its settings. Plugins' settings are validated with the core's, every problem reported together.

### Credits (Beta, the `credits` plugin)

The `credits` plugin's instance settings ([credits](../credits.md)): defaults for new projects, each of which changes its own rates. Also settable as `[plugins.credits.settings]`.

| Setting | Default | Legacy |
|---|---|---|
| `LEWBOX_CREDITS_DEFAULT_VCPU_HOUR` | `0` (credits, decimal; stored as micro-units, [credits/metering](../credits/metering.md)) | `LEWBOX_CREDITS_VCPU_HOUR_RATE` |
| `LEWBOX_CREDITS_DEFAULT_MEMORY_GIB_HOUR` | `0` | `LEWBOX_CREDITS_MEMORY_GIB_HOUR_RATE` |
| `LEWBOX_CREDITS_DEFAULT_DISK_GB_HOUR` | `0` | `LEWBOX_CREDITS_DISK_GB_HOUR_RATE` |
| `LEWBOX_CREDITS_DEFAULT_FREE_DISK` | `20` | `LEWBOX_CREDITS_FREE_DISK_GB` |
| `LEWBOX_CREDITS_CHECKPOINT_INTERVAL` | `1h` | the 5-second meter (`MeterSandboxes`), gone ([credits/balances](../credits/balances.md)) |
| `LEWBOX_CREDITS_SWEEP_INTERVAL` | `1m` | |
| `LEWBOX_CREDITS_BUDGET_WARNING_THRESHOLD` | `0.8` | new: default warning point for new projects' budgets ([credits/budgets-and-usage](../credits/budgets-and-usage.md)) |
| `LEWBOX_CREDITS_USAGE_RETENTION` | `9504h` (13 months) | new: hourly usage roll-ups |

### Observability

| Setting | Default | Meaning |
|---|---|---|
| `LEWBOX_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LEWBOX_LOG_FORMAT` | `json` (`text` in development) | |
| `LEWBOX_METRICS_ADDR` | none | Serve `/metrics` and `/debug/pprof` on a separate address, e.g. `127.0.0.1:9090`. Without it, `/metrics` is on the main address behind `LEWBOX_METRICS_TOKEN` |
| `LEWBOX_METRICS_TOKEN` | none | Bearer token for `/metrics` on the main address. Without it or `LEWBOX_METRICS_ADDR`, `/metrics` is off |
| `LEWBOX_LOG_SAMPLE_SUCCESS` | `1` | Fraction of successful (2xx) access lines logged; errors and slow requests always are ([observability](observability.md)) |
| `LEWBOX_METRICS_CLUSTER_LABEL` | `true` | Label metrics by cluster; turn off on instances with many clusters |
| `LEWBOX_METRICS_PROJECT_LABELS` | `true` | Label node and cluster metrics by project; turn off on large instances ([clusters/health-and-metrics](../clusters/health-and-metrics.md)) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` and other standard `OTEL_*` | none | Traces are exported when set |

### Development

| Setting | Default | Meaning |
|---|---|---|
| `LEWBOX_DEV_WEB_URL` | none | Proxy the dashboard to Vite's dev server instead of serving the embedded build |
| `LEWBOX_DEV_FAKES` | none | `aerolvm,github,smtp`: use in-process fakes instead of real services, for the dashboard's end-to-end tests |
| `LEWBOX_TEST_DATABASE_URL` | none (`make test` starts one) | A Postgres where the test role can `CREATE DATABASE` ([testing](testing.md)) |

### Dropped

`LEWBOX_APP_URL`, `LEWBOX_GATEWAY_TOKEN`, `LEWBOX_GATEWAY_URL`, `GATEWAY_APP_TIMEOUT`, `GATEWAY_KEY_CACHE_TTL`, `GATEWAY_PORTD_DIR`, `GATEWAY_AGENTD_DIR`, `GATEWAY_EVENT_*`, `LEWBOX_ACTIVITY_BATCH_SIZE`, `LEWBOX_AGENTS_BATCH_SIZE`, `LEWBOX_OPERATION_BATCH_SIZE`, `LEWBOX_PERFORMANCE_SEED_SCALE` (moves to the seeder's flags), `LEWBOX_ADMIN_URL` (one API), `LEWBOX_EXPERIMENTAL_FIRECRACKER` and any flag for a half-supported runtime (a level is offered when the fork supports it, [clusters/runtimes](../clusters/runtimes.md)), the AerolVM workarounds' settings (D15), every Laravel setting (`APP_*`, `SESSION_*`, `QUEUE_*`, `CACHE_*`, `REDIS_*`, `BROADCAST_*`, `AWS_*`, `VITE_*`).

### Clients (the CLI and SDKs; not read by `lewbox serve`)

| Variable | Default | Meaning |
|---|---|---|
| `LEWBOX_API_KEY` | none | The key the CLI and SDKs send ([sdks-and-cli](../sdks-and-cli.md)) |
| `LEWBOX_API_URL` | `https://api.lewbox.com/v0` | The API to call, e.g. a self-hosted instance's |
| `LEWBOX_CLUSTER` | none | The client's default cluster for creates |
| `LEWBOX_ADMIN_KEY` | none | An admin key, for the SDKs' `LewboxAdmin` client in a project's own servers |
| `LEWBOX_NO_UPDATE_CHECK` | none | `1` stops the CLI checking for a newer release ([sdks-and-cli/cli](../sdks-and-cli/cli.md)) |
| `LEWBOX_WEBHOOK_SECRET` | none | Convention for an app's copy of an endpoint's signing secret, read by the SDKs' webhook verifiers (Lewagent uses it) |

### Inside sandboxes (set by Lewbox; not instance settings)

Variables Lewbox sets for the helpers and hooks it runs in a sandbox. Listed so names don't collide; an operator never sets them.

| Variable | Set for | Meaning |
|---|---|---|
| `LEWBOX_SANDBOX_ID`, `LEWBOX_HOOK`, `LEWBOX_HOOK_LABEL`, `LEWBOX_OPERATION_ID` | Hooks | Which sandbox, hook (`on_create`, `on_start`, `on_stop`), label and operation ([sandboxes/hooks](../sandboxes/hooks.md)) |
| `LEWBOX_STOP_REASON`, `LEWBOX_STOP_DEADLINE` | `on_stop` hooks | Why the sandbox is stopping, and by when (Unix seconds) |
| `LEWBOX_PORTD_LISTEN`, `LEWBOX_PORTD_SANDBOX`, `LEWBOX_PORTD_PORT`, `LEWBOX_PORTD_KEYS`, `LEWBOX_PORTD_FRAME_ANCESTORS` | `lewbox portd` | Its proxy port, sandbox, private port, the signing keys it verifies with, and who may frame it ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)) |
| `LEWBOX_AGENTD_DIR`, `LEWBOX_AGENTD_SOCKET`, `LEWBOX_AGENTD_WORKTREES_DIR`, `LEWBOX_AGENTD_INSTALL_DIR` | `lewbox agentd` | Where it keeps conversations, its socket, worktrees and adapters ([agents/agentd](../agents/agentd.md)) |
| `LEWBOX_AGENTD_CHANNEL_URL` | `lewbox agentd` | The agent channel it dials |
| `LEWBOX_AGENTD_REPLY_TIMEOUT`, `LEWBOX_AGENTD_ADAPTER_IDLE`, `LEWBOX_AGENTD_BACKGROUND_IDLE`, `LEWBOX_AGENTD_AGENT_TURN_QUIET`, `LEWBOX_AGENTD_MAX_LOADED`, `LEWBOX_AGENTD_UPLOAD_INTERVAL`, `LEWBOX_AGENTD_HEARTBEAT` | `lewbox agentd` | Its timers and limits, overridden by the control plane per prompt where it says |
| `LEWBOX_AGENTD_CLAUDE_CODE_ADAPTER`, `LEWBOX_AGENTD_CODEX_ADAPTER`, `LEWBOX_AGENTD_CLAUDE_CODE_PACKAGE`, `LEWBOX_AGENTD_CODEX_PACKAGE` | `lewbox agentd` | Adapter commands and pinned packages, from the agent plugins' settings |
| `LEWBOX_AGENTD_TELEMETRY_FILE` | `lewbox agentd` | Where the profile's telemetry settings are, or `none` ([profiles/telemetry](../profiles/telemetry.md)) |
| `LEWBOX_AGENTD_CODEX`, `LEWBOX_AGENTD_CODEX_OUTPUT_SCHEMA` | agentd's Codex stand-in | The real `codex` binary, and the output schema to add |

## Project settings

Stored in `projects.settings`, validated by a Go struct, edited at `PATCH /v0/projects/{prj}` ([projects](../projects.md)):

| Setting | Default | Was (`tenants` column) |
|---|---|---|
| `activity.retention` | `LEWBOX_ACTIVITY_DEFAULT_RETENTION` | `activity_retention_days` |
| `activity.succeeded_command_retention` | `LEWBOX_ACTIVITY_DEFAULT_SUCCEEDED_COMMAND_RETENTION` | `succeeded_command_retention_days` |
| `activity.record_command_stderr` | `true` | `record_command_stderr` |
| `git.sandbox_access_max` | none (no cap but the App's permissions; sandboxes default to `full`) | none (legacy fixed sandboxes at read-only in code); `git_mode` is dropped |
| `sign_ins.cluster` | none | `sign_in_cluster_id` |
| `sign_ins.max_concurrent` | `5` | `max_sign_in_sandboxes` |
| `credits.rates`, `credits.free_disk_gb`, `credits.multiple_subscription_buckets` (Beta, the `credits` plugin's project settings) | instance defaults | `vcpu_hour_rate`, …, `allow_multiple_subscription_buckets` |
| Which plugins are on (`project_plugins`) | the instance's projects mode | none ([projects/settings](../projects/settings.md#plugins)) |

## Security

- Secrets (`LEWBOX_DATABASE_URL`'s password, `LEWBOX_SECRET_KEYS`, `LEWBOX_EMAIL_SMTP_URL`'s password, `LEWBOX_METRICS_TOKEN`) are typed `secret.String`, never logged, and redacted in `config print`.
- `_FILE` variants so secrets needn't be in the environment.
- An unknown `LEWBOX_*` variable is a warning (likely a typo, or a legacy name, in which case the warning names the new one).

## Testing

- Every setting parses its default and rejects bad values.
- Cross-setting checks: each rule has a failing and passing case.
- Precedence: flag > env > file > default; `_FILE` variants; both given is an error.
- The generated doc matches the table.
- Legacy names produce a warning naming the replacement.

## Build plan

1. `internal/config`: the `Setting` table, loader, typed `Config`, cross-checks, `secret.String`.
2. `lewbox config check|print`.
3. Doc generator and its test.
4. Project settings struct and validation, with the projects plan.
5. Node settings payload, with the node agent plan.

## Open questions

1. TOML or YAML for the file?
2. Should `LEWBOX_SIGN_UP` default to `invite_only` for self-hosted installs, so a fresh instance on the internet isn't open to sign-ups?
3. Are any of the node settings worth making per-cluster rather than instance-wide (e.g. the TCP port range, for clusters behind different firewalls)?
