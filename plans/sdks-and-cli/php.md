# PHP SDK

> Status: Draft · Milestone: Beta · Updated: 2026-10-29

`lewbox/lewbox-php` is the PHP client for Lewbox, with a Laravel integration. It exists today, built over the legacy APIs, and Lewagent depends on it. This plan moves it to the new API, gives it its own published repository, adds what it's missing (WebSockets, templates, the rest of the Admin API), and moves Lewagent onto it.

## Today

- **Where:** `lewbox-legacy/packages/lewbox-php-sdk`, required by the Laravel app through a Composer path repository with Laravel discovery turned off (`dont-discover`), since the app has its own `config/lewbox.php`. An identical copy lives in `lewagent-legacy/packages/lewbox-php`, copied on `post-autoload-dump` and in a pre-commit hook by `lewagent-legacy/app/Console/Commands/SyncLewboxSdkCommand.php`, which also symlinks `vendor/lewbox/lewbox-php` back to the source when it's there.
- **Size:** about 9,600 lines of PHP with tests (Pest), PHP 8.2+, Guzzle 7 or 8.
- **Clients:**
  - `Lewbox\Lewbox` (profile key `lbp_` or sandbox key `lbs_`, Consumer API): `profile()`, `sandboxes()`, `conversations()`, `activity()`, `operations()`; a `Sandbox` handle with `refresh`, `stop`, `start`, `restart`, `kill` (each with `wait` and `timeout`), `update`, `waitUntilRunning`, `getUrl`, `commands()`, `files()`, `sessions()`, `ports()`, `ssh()`, `keys()`, `prompt()`, `resume()`, `agents()`, `conversations()`, `activity()` (`src/Sandbox.php`).
  - `Lewbox\LewboxAdmin` (tenant key `lbt_`, Admin API): `profiles()` with `ProfileApiKeys`, `ProfileLimits`, `ProfileAgents`, `ProfileGit`, `ProfileTelemetry`, `ProfileSetupLinks`, `ProfileSshKeys`, `ProfileRepositories`; `sandboxes()` with `SandboxPushes`; `templates()`; `clusters()`; `nodes()`; `webhookEndpoints()` (`src/Admin/Resources/*`).
- **Transport** (`src/Http/Transport.php`): JSON requests, `pages()` generator over cursors, retries on network errors and `429`, `502`, `503`, `504` with backoff and `Retry-After`, only for `GET`, `PUT`, `DELETE`, or `POST` with an `Idempotency-Key`; final codes (`limit_exceeded`) never retried; a `User-Agent`; exceptions by status and code. `src/Http/EventStream.php` parses SSE fed in chunks.
- **Data classes** written by hand with `fromArray()` (`Sandbox`, `Operation`, `Port`, `Entry`, `Session`, `Conversation`, `Message`, `Chunk`, `AgentEvent`, `Push`, `PullRequest`, …).
- **Exceptions** in `Lewbox\Exceptions`: `AuthenticationException`, `PermissionDeniedException`, `NotFoundException`, `SandboxNotFoundException`, `ConflictException`, `OperationInProgressException`, `ValidationException`, `LimitExceededException`, `RateLimitException`, `ClusterUnavailableException`, `ServerException`, `ConnectionException`, `TimeoutException`, `OperationFailedException`, `CommandExitException`, `WebhookVerificationException`, all extending `LewboxException` (with `errorCode`, since PHP's `code` is an int).
- **Webhooks:** `Lewbox\Webhooks\Webhook::verify()` and `WebhookEvent`, Standard Webhooks.
- **Laravel:** `Lewbox\Laravel\LewboxServiceProvider` binds both clients as singletons from `config/lewbox.php`; facades `Lewbox` and `LewboxAdmin`.
- **Missing:** WebSockets (interactive commands, session attach), Consumer API templates, `profile()->update()`, the Admin API's credits, most admin sandbox and conversation operations, installations.
- **Tests:** 20 Pest files with mocked HTTP (`tests/*Test.php`).
- **Users:** Lewagent, in 46 PHP files (actions, controllers, jobs, models; listed in [migration/lewagent](../migration/lewagent.md)); and the legacy app itself, for sign-ins, token refresh and pushes, which goes away.

## Goals

- The same well-liked shape (`$sandbox->commands()->run(…)`), on the new API.
- Published properly: its own repository and Packagist, semver, no copy hooks.
- Feature parity with the TypeScript SDK where PHP can, including WebSockets as an optional dependency.
- Lewagent moves onto it with as few changes to its own code as the new API allows.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Namespaces and class names (`Lewbox\Lewbox`, `Lewbox\LewboxAdmin`, `Lewbox\Sandbox`, `Lewbox\Exceptions\*`); hand-written data classes with `fromArray()`; named arguments; seconds for durations; the transport's retry rules; `pages()` generators; webhook verification; the Laravel provider and facades; Pest tests |
| **Change** | Paths and fields to the new API; `LewboxAdmin` takes an admin key (`lba_`, from `LEWBOX_ADMIN_KEY`) on the same base URL as `Lewbox`; tenant → project wherever it shows; exceptions for new codes; development in this repository under `sdks/php`, published by splitting |
| **Drop** | `SyncLewboxSdkCommand` and Lewagent's copy; pushes, pull requests and repository creation on `LewboxAdmin` (Lewbox no longer has those APIs; Lewagent ports legacy's code, [git/integrating](../git/integrating.md#lewagent)) |
| **Add** | GitHub tokens (`profile()->github()->token(...)` for installation tokens; `profiles($prf)->github()->token(...)` and `->userToken($login)` on `LewboxAdmin`, the only place user tokens are), the profile's GitHub people and accounts (`profiles($prf)->github()->people()`, `->accounts()`) and the sandbox's Git setting (`$sandbox->git()->update(...)`); WebSockets (`commands()->stream()`, `sessions()->attach()`) with an optional client; templates on `Lewbox`; `profile()->update()`; `LewboxAdmin` credits (Beta), installations, nodes' drain and update, admin sandboxes and conversations; contract tests against the fake server |

## Design

### Repository and releases

- **Developed in `sdks/php/` in this repository,** so an API change and its SDK change land together and are tested together in CI. Its `composer.json` is at `sdks/php/composer.json`.
- **Published by subtree split** to `github.com/harlew-dev/lewbox-php` on a `sdk-php/v*` tag, with the tag carried over; Packagist watches that repository. The split repository's README says issues and pull requests go to the main repository.
- `lewbox/lewbox-php` on Packagist (the name the docs use). The 0.x line starts again on the new API; the legacy SDK was never published, so nobody outside Harlew depends on its old shape.

### Clients and configuration

```php
use Lewbox\Lewbox;
use Lewbox\LewboxAdmin;

$lewbox = new Lewbox(
    apiKey: getenv('LEWBOX_API_KEY'),      // lbp_… or lbs_…
    apiUrl: getenv('LEWBOX_API_URL') ?: 'https://api.lewbox.com/v0',
    cluster: getenv('LEWBOX_CLUSTER') ?: null,
    timeout: 30,
    waitTimeout: 600,
    maxRetries: 3,
);

$admin = new LewboxAdmin(adminKey: getenv('LEWBOX_ADMIN_KEY'));
```

Both check their key's prefix. `LewboxAdmin` defaults to the same `LEWBOX_API_URL`; legacy's separate `LEWBOX_ADMIN_URL` is gone, as the APIs are one server.

### Surface

The surface follows the [TypeScript SDK](typescript.md#surface) in PHP style, extending the legacy one:

- `Lewbox`: `profile()` (`get`, `update`), `sandboxes()` (`create`, `get`, `list`), `conversations()`, `activity()`, `templates()` (new), `repositories()` (moved from profile), `operations()`.
- `Sandbox`: as today, plus `commands()->stream()` and `sessions()->attach()` (WebSockets), `files()->readStream()` and `writeStream()` with PHP streams, `ports()->link()`.
- `Conversation`: `prompt`, `events(types: …)` (a generator of `AgentEvent`s, resuming with `Last-Event-ID`), `follow()` (the TypeScript `follow`'s rules: reconnect on drop and after 20 s of quiet, finish from stored messages after 3 empty reconnects), `output()`, `reply()`, `cancel()`, `steer()`, `queued()`, `messages()`, `tasks()`, `stopTask()`, `delete(force: …, keepWorktree: …)`, `->worktree`.
- `LewboxAdmin`: `profiles()` (and its sub-resources), `clusters()` (with `installations()`), `nodes()`, `sandboxes()`, `templates()`, `webhookEndpoints()`, `activity()`; `credits()` in Beta when the `credits` plugin's API ships ([credits](../credits.md)); `plugins()` to list and turn the project's plugins on and off. `PluginDisabledException` for `409 plugin_disabled` ([sdks-and-cli](../sdks-and-cli.md#plugins)). GitHub: `profiles($prf)->github()` (connection, installations, install links, repositories, `token(type:, repositories:, permissions:)`) ([git](../git.md)).

### Transport

Kept from `src/Http/Transport.php`, which already does the right things: retries for safe requests only, `Idempotency-Key` from `Transport::idempotencyKey()` on creates, `Retry-After`, final codes, a `User-Agent` (`lewbox-php/<v> (php 8.4; linux)`), a swappable sleeper for tests. Changes:

- Paths and envelope per the new API; `Lewbox-Request-Id` read from every response into exceptions as `requestId`.
- **Streaming bodies** through Guzzle's `sink` and `body` streams for files; SSE read incrementally with `stream: true`, through `EventStream::feed()`.
- **Operation waits** poll from 1 s to 5 s, as today, until `waitTimeout`.

### WebSockets

Interactive commands and session attach need a WebSocket client, which PHP lacks. As the docs already say, it's an **optional dependency**: the rest of the SDK stays light.

- **Choice:** `phrity/websocket` (synchronous, pure PHP, maintained, supports subprotocols and binary frames), suggested in `composer.json`. Calls that need it throw a clear `LewboxException` naming the package when it isn't installed.
- **Auth:** subprotocols `lewbox.bearer` and the key, as every SDK.
- **Shape:** blocking, with callbacks: `$handle = $sandbox->commands()->stream('python3 -i', pty: true, onData: fn (string $bytes) => …); $handle->sendStdin("print(1)\n"); $handle->wait();`. Long-running PHP workers (Octane, queue workers) can use it; request-response PHP mostly won't, and the docs point those at sessions with logs instead.
- Async (`amphp/websocket-client`) is a possible second adapter later.

### Laravel

- `Lewbox\Laravel\LewboxServiceProvider`, discovered automatically; `config/lewbox.php` published with `php artisan vendor:publish --tag=lewbox-config`, keys `api_key`, `admin_key`, `api_url`, `cluster`, `timeout`, `wait_timeout`, `max_retries`, `webhook_secret`.
- Facades `Lewbox` and `LewboxAdmin`; each client is made when first used, so an app without an admin key can still use `Lewbox`.
- **Webhook route helper:** `Route::lewboxWebhooks('/webhooks/lewbox', HandleLewbox::class)` verifies the signature with `lewbox.webhook_secret`, rejects replays by event id (cache, 3 days, as Lewagent does), and dispatches a typed event to the handler. This is the pattern Lewagent's `LewboxWebhookController` and `HandleLewboxWebhook` use today.
- `illuminate/support` stays a suggestion, not a requirement.

### Errors

The legacy classes stay, plus new ones for new codes where a class helps (`CsrfFailed` doesn't apply to keys; most new codes are a `ConflictException` or `ValidationException` with `errorCode`). The mapping table is the TypeScript SDK's ([typescript](typescript.md#errors)) with `Exception` for `Error`. `RateLimitException::$retryAfter` is in seconds.

## Testing

- Keep and port the 20 Pest files (`tests/*Test.php`) to the new paths, with mocked HTTP (Guzzle's `MockHandler`).
- Shared recorded fixtures ([sdks-and-cli](../sdks-and-cli.md#testing-shared)) replayed through `MockHandler`, including SSE streams split at awkward byte boundaries for `EventStream`.
- Contract tests against `lewbox serve --fake-cluster` in CI, on PHP 8.2, 8.3 and 8.4, with and without `phrity/websocket`.
- Larastan level 8 (Lewagent uses Larastan); Pint.
- Lewagent's own test suite, run against the new SDK and the fake server, is the real acceptance test ([migration/lewagent](../migration/lewagent.md)).

## Build plan

1. Move `lewbox-legacy/packages/lewbox-php-sdk` into `sdks/php/` with its history (`git subtree` or `filter-repo`), CI for it. (Beta, early)
2. Change paths, envelope and fields to the new API; rename tenant to admin; drop pushes, pull requests and repository creation; add GitHub tokens; update tests. (Beta)
3. Add templates, `profile()->update()`, admin installations, nodes, sandboxes, conversations. (Beta)
4. WebSockets with `phrity/websocket`. (Beta)
5. Laravel webhook route helper. (Beta)
6. Subtree split release to `harlew-dev/lewbox-php`, Packagist. (Beta)
7. Credits, as its API ships. (Beta)
8. Lewagent moves ([migration/lewagent](../migration/lewagent.md)).

## Open questions

1. **Subtree split or separate repository** with a vendored spec? (Proposed: split, as above.)
2. **WebSocket library:** `phrity/websocket` (sync) or `amphp/websocket-client` (async, Revolt)? (Proposed: phrity, optional.)
3. **Generated data classes?** Legacy decided hand-written; with the spec as the source of truth, should a generator at least check the hand-written classes against it in CI? (Proposed: yes, a check, not a generator.)
4. **Should the PHP SDK ship in the alpha** after all, since Lewagent is the first big user and moves in Beta? Lewagent could move earlier if it's ready.
