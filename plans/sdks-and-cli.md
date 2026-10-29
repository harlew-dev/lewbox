# SDKs and CLI

> Status: Draft · Milestone: Alpha (M4) for the CLI and TypeScript SDK; Beta for PHP; Go with the CLI · Updated: 2026-10-29

How people and programs use Lewbox besides the dashboard: the `lewbox` CLI, the TypeScript SDK (`@lewbox/sdk`), the PHP SDK (`lewbox/lewbox-php`, which Lewagent depends on), and a Go client package, which the CLI is built on. Also where the public docs live, since every docs page gives each example in cURL, the CLI, TypeScript and PHP.

## Today

- **PHP SDK:** the only real SDK. `lewbox-legacy/packages/lewbox-php-sdk`, about 9,600 lines with tests: `Lewbox` (profile or sandbox key, Consumer API) and `LewboxAdmin` (tenant key, Admin API), a Guzzle transport with retries and idempotency keys (`src/Http/Transport.php`), SSE parsing (`src/Http/EventStream.php`), typed data classes, exceptions by error code, Standard Webhooks verification, a Laravel service provider and facades. No WebSockets, no Consumer API templates. Lewagent uses an identical copy (`lewagent-legacy/packages/lewbox-php`), synced by a Composer hook from the sibling repo (`app/Console/Commands/SyncLewboxSdkCommand.php`).
- **The Laravel app uses the PHP SDK on itself**, with sandbox keys it mints, to drive sign-ins, token refresh and pushes through the gateway. In one binary that goes away ([README](README.md#goals)).
- **TypeScript SDK:** planned only (`lewbox-legacy/docs/sdks.md`), but the public docs already show TypeScript for every example, and both dashboards have their own partial clients (`lewbox-legacy/resources/js/lib/gateway.ts`, `lewagent-legacy/resources/js/lib/agents.ts`).
- **CLI:** planned only, but the docs show it for every example: `npm install --global @lewbox/cli`, `lewbox login`, `lewbox sandboxes create …`, `lewbox shell`, and about 60 subcommands ([cli](sdks-and-cli/cli.md#command-tree)).
- **Contract:** the gateway's hand-written `api/openapi.yaml` (3,870 lines), missing routes the server has (tasks, agent uploads).
- **Docs:** three sets served by the Laravel app (`/docs`, `/api/docs`, `/api/private/docs`), from `lewbox-legacy/docs/{public,admin,private}`, plus `.md` variants and `/llms.txt`. lewbox.com/docs is the public set.

## Goals

- **One contract.** Every SDK's types come from `api/openapi.yaml`, which the server is checked against ([api/openapi-and-codegen](api/openapi-and-codegen.md)).
- **The same concepts in every language,** shaped like E2B's where it fits (`commands.run`, `files.write`, `getUrl`, `kill`), so moving from E2B is mostly renaming, as the [guide](https://lewbox.com/docs/guides/moving-from-e2b) promises.
- **The hard parts once per language, done well:** retries with idempotency, waiting for operations, cursor pages, SSE with resume, WebSockets, streamed files, webhook verification.
- **A CLI that's the binary.** `lewbox` the CLI and `lewbox` the server are the same Go program ([D1](README.md#decisions)), so a self-hoster has one download.
- **Released together.** An API change, its SDKs and its docs land in one pull request in this repository, so they never drift. (Legacy's cross-repo copy hook is what we're avoiding.)

### Non-goals (for now)

- A Python SDK. Planned after the PHP SDK (the docs say "No Python SDK yet").
- A browser build of the TypeScript SDK for profile keys: keys belong on servers. The SDK does run in browsers with a **sandbox key** ([typescript](sdks-and-cli/typescript.md#browsers)).
- An MCP server. Worth doing (legacy `docs/sdks.md` open decision 5), after the alpha, as `lewbox mcp` in the CLI.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | E2B-shaped names; a `Sandbox` handle; waiting for operations by default; retries only where safe; `Idempotency-Key` on every create; errors as classes by code with `requestId`; durations in ms in TypeScript and seconds elsewhere; `LEWBOX_API_KEY`, `LEWBOX_API_URL`, `LEWBOX_CLUSTER`; Standard Webhooks verification; the Laravel integration |
| **Change** | Two clients: `Lewbox` (profile or sandbox key) and `LewboxAdmin` (admin key, `lba_`, was tenant key `lbt_`). Both on one base URL, since the APIs are one server ([api](api.md)). Paths follow the new API. The PHP SDK moves to its own repository, published from this one. Conversations' event merging moves into the SDKs, from the dashboards |
| **Drop** | Lewagent's copy of the PHP SDK and its sync hook; the app's use of its own public API; per-app hand-written TypeScript clients |
| **Add** | The CLI; the TypeScript SDK; a public Go client; WebSockets in every SDK (PHP optional); conversation helpers (follow a turn, merge events into chunks, reply); `LEWBOX_ADMIN_KEY` |

## Design

| Plan | What it covers |
|---|---|
| [cli](sdks-and-cli/cli.md) | The `lewbox` CLI: commands, config, output formats, `login`, `shell`, waiting, exit codes, distribution (binaries, npm, Homebrew, install script) |
| [typescript](sdks-and-cli/typescript.md) | `@lewbox/sdk`: generated types, hand-written clients, transport, waiters, pagination, streams, WebSockets, conversations, webhooks, browsers |
| [php](sdks-and-cli/php.md) | `lewbox/lewbox-php`: moving the legacy SDK to the new API, its own repository, WebSockets, Laravel, Lewagent's move |
| [go](sdks-and-cli/go.md) | `github.com/harlew-dev/lewbox/client`: the public Go client the CLI uses, and Go programs can too |

### Shared shape

Every SDK has the same surface, in its language's style:

| Area | TypeScript | PHP | Go | CLI |
|---|---|---|---|---|
| Client | `new Lewbox({ apiKey, apiUrl, cluster })` | `new Lewbox(apiKey: …)` | `lewbox.New(lewbox.WithAPIKey(…))` | `lewbox login`, `--api-key` |
| Create | `lewbox.sandboxes.create({ template, … })` | `$lewbox->sandboxes()->create(template: …)` | `c.Sandboxes.Create(ctx, lewbox.SandboxCreate{…})` | `lewbox sandboxes create --template …` |
| Commands | `sandbox.commands.run(cmd, { onStdout })` | `$sandbox->commands()->run($cmd, onStdout: …)` | `sbx.Commands.Run(ctx, cmd, opts)` | `lewbox commands run $SBX -- cmd` |
| Files | `sandbox.files.write(path, data)` | `$sandbox->files()->write(…)` | `sbx.Files.Write(ctx, path, r)` | `lewbox files write $SBX path < file` |
| Agents | `sandbox.prompt(text, { agent })` | `$sandbox->prompt($text, agent: …)` | `sbx.Conversations.Start(ctx, …)` | `lewbox conversations start $SBX …` |
| Admin | `new LewboxAdmin({ adminKey })` | `new LewboxAdmin(adminKey: …)` | `lewbox.NewAdmin(…)` | `lewbox admin …` with an admin key |
| Webhooks | `Webhook.verify(body, headers, secret)` | `Webhook::verify(…)` | `webhook.Verify(…)` | `lewbox webhooks verify` (for testing) |

### Plugins

Plugins ([plugins](plugins.md)) add API operations (credits' buckets, a Git host's tokens). The SDKs are generated from the **default build's** document, so they cover every first-party plugin:

- **Grouped by plugin.** A plugin's operations live under its name: `admin.credits.buckets(prf).list()` in TypeScript, `$admin->credits()->buckets($prf)` in PHP, `admin.Credits.Buckets(prf).List(ctx)` in Go, `lewbox credits buckets list` in the CLI. Operations of the frameworks (agents, Git) stay where they are, with the provider as an argument (`lewbox.git('github').tokens.create(…)`, or the `github` shorthand).
- **`PluginDisabledError`** (`PluginDisabledException` in PHP, `lewbox.ErrPluginDisabled` in Go) for `409 plugin_disabled`, carrying `plugin.name` and `plugin.scope`, so a caller can tell "turned off" from any other refusal ([api/errors](api/errors.md)).
- **`client.plugins()`** reads `/v0/meta`'s list, for code that adapts to what an instance has.
- **Third-party plugins** aren't in our SDKs. Their operations are reachable with each SDK's raw `request(method, path, body)`, which keeps auth, retries, idempotency and errors; a plugin's author may publish a small package on top.

### Configuration

Every SDK and the CLI read the same environment, in this order: an explicit option, the environment, then (CLI only) `~/.config/lewbox/config.json`.

| Variable | Setting | Default |
|---|---|---|
| `LEWBOX_API_KEY` | A profile key (`lbp_`) or sandbox key (`lbs_`), for `Lewbox` | Required for `Lewbox` |
| `LEWBOX_ADMIN_KEY` | An admin key (`lba_`), for `LewboxAdmin` | Required for `LewboxAdmin` |
| `LEWBOX_API_URL` | The API's URL | `https://api.lewbox.com/v0` |
| `LEWBOX_CLUSTER` | Default cluster for creates | The profile's default |

Each client checks its key's prefix when it's built and refuses the wrong kind, rather than letting every call fail with `wrong_key_kind`.

### Versioning and release

- SDKs use semver, independently of the API (`/v0`, which only grows). Before 1.0, minor versions may break.
- Each SDK's version is in its own manifest; the CLI's is the binary's.
- **Release train:** a tag `sdk-typescript/v0.3.0` publishes to npm; `sdk-php/v0.3.0` pushes `sdks/php` to the `harlew-dev/lewbox-php` repository with that tag, which Packagist picks up; `v0.3.0` releases the binary and the CLI's npm package ([foundation/build-and-release](foundation/build-and-release.md)).
- CI regenerates each SDK's types from the spec on every change and fails if they're stale.

### Testing, shared

- **Recorded fixtures** of real API exchanges (requests, responses, SSE streams, WebSocket frames), made by running the server's end-to-end tests with recording on, and replayed by every SDK's unit tests. One set of fixtures, so every SDK handles the same edge cases.
- **Contract tests** against `lewbox serve --fake-cluster` in CI, for each SDK: create, run, files, ports, a conversation with the fake ACP adapter, stop, start, delete.
- **Live tests** nightly against a staging project with a real cluster, tagging every sandbox with the run in `metadata` and sweeping leftovers.
- **Docs examples are tests:** each code block in the docs is extracted and run, per language, against the fake cluster ([Docs](#docs)).

### Docs

- The public docs (lewbox.com/docs) move out of the Laravel app into **`docs/` in this repository**, as markdown with one example block per language (`curl`, `cli`, `typescript`, `php`, and later `go`), as `/tmp/lewbox-docs/*.md` already are.
- A static site generator (Astro Starlight, or similar) builds them; every page is also served as `.md`, with `/llms.txt` as an index, as today.
- The API reference is rendered from `api/openapi.yaml`.
- The admin docs (`docs/admin`) and the self-hosting docs (installing `lewbox serve`, adding nodes, upgrading) are new sections of the same site. Legacy's private docs set (`docs/private`) becomes these plans and the self-hosting section.
- The dashboard links to the docs; it doesn't serve them.

## Security

- Keys are read from the environment or explicit options; the CLI stores its key in `~/.config/lewbox/config.json` with mode `0600`, or in the OS keychain where available ([cli](sdks-and-cli/cli.md#config)).
- SDKs never log keys; request logging, where offered, redacts `Authorization` and `Sec-WebSocket-Protocol`.
- Webhook verification checks the signature and a timestamp within 5 minutes, in constant time.

## Build plan

1. M0: the Go client's transport and generated types; CI checks of generated types.
2. M2: the CLI for sandboxes, commands, files, sessions, ports, SSH keys, `login`, `shell`.
3. M3: CLI conversations; TypeScript SDK's transport, sandboxes, commands, files, sessions, ports.
4. M4: TypeScript conversations and webhooks; CLI templates, profile, admin; distribution (npm, Homebrew, install script); docs site with every example tested.
5. Beta: PHP SDK on the new API, its own repository, Lewagent moved to it ([migration/lewagent](migration/lewagent.md)).
6. Later: Python; `lewbox mcp`.

## Open questions

1. **npm package for the CLI.** The docs say `npm install --global @lewbox/cli`. Do we ship the Go binary through npm (per-platform optional dependencies, as esbuild does)? (Proposed: yes, [cli](sdks-and-cli/cli.md#distribution).)
2. **Does the TypeScript SDK include `LewboxAdmin`** in the same package, or a separate `@lewbox/admin`? (Proposed: same package, as legacy's design and the PHP SDK do.)
3. **PHP SDK repository:** split from this monorepo on release (subtree split), or developed in its own repository with the spec vendored? (Proposed: developed here in `sdks/php`, split on release, so API and SDK change together.)
4. **Docs site generator,** and do docs live here or in their own repository? (Proposed: here, in `docs/`.)
5. **Python SDK timing.** Many agent builders use Python; is it worth moving ahead of PHP for the open-source launch?
