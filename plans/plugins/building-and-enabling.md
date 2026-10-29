# Building and enabling

> Status: Draft · Milestone: Alpha (M0); custom builds documented for third parties in Beta · Updated: 2026-10-29

Which plugins a binary contains is decided when it's built; which of those are on is decided by the instance's configuration and then by each project. This plan covers `plugins/config.json`, the generator that turns it into code, custom builds with `lewbox build --with`, the configuration for each plugin, and the API projects use to turn plugins on and off.

## Goals

- One file says what a build contains, in what order.
- The default build is what we release; anyone can build their own with more or fewer plugins, with one command.
- Operators turn plugins on and off in configuration, reviewable like the rest of it.
- Projects turn on what the operator allows, in the dashboard or the API.

## Design

### `plugins/config.json`

```json
{
  "$schema": "./config.schema.json",
  "plugin_api": 1,
  "plugins": [
    { "name": "limits",             "package": "./plugins/limits",             "enabled": true,  "projects": "all" },
    { "name": "storage/s3",         "package": "./plugins/storage/s3",         "enabled": true,  "projects": "all" },
    { "name": "dns/libdns",         "package": "./plugins/dns/libdns",         "enabled": true,  "projects": "all",
      "options": { "providers": ["cloudflare", "route53", "digitalocean", "hetzner", "googleclouddns", "azure", "gandi", "porkbun", "desec", "rfc2136"] } },
    { "name": "agents/claude-code", "package": "./plugins/agents/claude-code", "enabled": true,  "projects": "opt_out" },
    { "name": "agents/codex",       "package": "./plugins/agents/codex",       "enabled": true,  "projects": "opt_out" },
    { "name": "git/github",         "package": "./plugins/git/github",         "enabled": true,  "projects": "opt_out" },
    { "name": "telemetry",          "package": "./plugins/telemetry",          "enabled": true,  "projects": "opt_out" },
    { "name": "credits",            "package": "./plugins/credits",            "enabled": true,  "projects": "opt_in" },
    { "name": "agents/opencode",    "package": "./plugins/agents/opencode",    "enabled": false, "projects": "opt_in", "build": false }
  ]
}
```

| Field | Means |
|---|---|
| `name` | The plugin's name; must equal its `Info().Name` |
| `package` | A local path in this module, or a module path with a version (`github.com/acme/lewbox-gitlab@v1.2.0`) for a custom build |
| `enabled` | The default for the instance, when configuration doesn't say. The plugin's own `Info` can't override it |
| `projects` | The default projects mode: `all` (on for every project, projects can't turn it off), `opt_out` (on unless a project turns it off), `opt_in` (off unless a project turns it on) |
| `build` | `false` to keep an entry without compiling it in (a plugin not ready yet). Default `true` |
| `options` | Build-time options the generator passes to the plugin. `dns/libdns` takes `providers`: the libdns providers to compile in, each one a blank import in its generated `providers_gen.go`, so an unlisted provider and its dependencies aren't in the binary ([dns-providers](dns-providers.md#choosing-providers)) |

The order of the list is the tie-breaker after dependencies ([registry-and-lifecycle](registry-and-lifecycle.md#the-registry)). A JSON Schema checks the file in CI.

### The generator

`go generate ./plugins` runs `internal/cmd/genplugins`, which reads `config.json` and writes, all committed and checked in CI to be current:

| File | Build | Contains |
|---|---|---|
| `cmd/lewbox/plugins_gen.go` | default (`!sandbox && !cli`) | A blank import of each plugin package, and the list with its order and defaults |
| `cmd/lewbox/plugins_sandbox_gen.go` | `sandbox` | Blank imports of each agent plugin's `agentd` sub-package ([agent-providers](agent-providers.md#the-agentd-driver)) |
| `cmd/lewbox/plugins_cli_gen.go` | `cli` | Blank imports of each plugin's `cli` sub-package |
| `web/src/plugins.gen.ts` | dashboard | A lazy `import()` per plugin with a `web/index.tsx` ([dashboard-slots](dashboard-slots.md#how-plugins-reach-the-dashboard)) |
| `api/openapi.yaml` | all | The core's document merged with each plugin's fragment ([api/openapi-and-codegen](../api/openapi-and-codegen.md#plugin-fragments)) |
| `docs/plugins.md` | docs | The list of plugins with their descriptions and settings, from their schemas |

Each generated Go file carries `!noplugins` in its build constraint, so `go build -tags noplugins` gives the **core-only** build CI tests ([foundation/build-and-release](../foundation/build-and-release.md#ci)).

So the build is ordinary `go build`: no plugin is found at run time, and `go list -deps` shows exactly what's in a binary.

### Custom builds

For an operator who wants a plugin we don't ship (a third party's GitLab provider), or fewer plugins:

```
lewbox build \
  --with github.com/acme/lewbox-gitlab@v1.2.0 \
  --without agents/codex \
  --output ./lewbox
```

What it does (the `xcaddy` approach):

1. Make a temporary module that requires `github.com/harlew-dev/lewbox` at the running binary's version, and each `--with` module at its version.
2. Write a `config.json` from the default one, with `--with` added and `--without` removed (marked `build: false`), and run the generator.
3. Check every plugin's `plugin_api` against the core's; a mismatch fails here with the plugin and the versions.
4. Build both targets: `lewbox` and `lewbox-sandbox` (for each architecture), since an agent plugin may have an `agentd` part, and embed the sandbox builds as the release does ([foundation/build-and-release](../foundation/build-and-release.md#custom-builds)).
5. The dashboard: if every plugin's UI is already built into the release's dashboard, reuse it. If a `--with` plugin has a `web/` folder, rebuild the dashboard, which needs Node 22 and `pnpm` on the build machine; without them the build fails, saying so (`--skip-ui` builds without the plugin's pages; its settings still render from its schema).
6. Print the result: version, plugins, hashes. `lewbox version` on the result lists the plugins and marks the build `custom`.

Needs Go 1.27+ on the build machine. A container image (`ghcr.io/harlew-dev/lewbox-builder`) has Go, Node and pnpm for those who don't want them installed.

### Configuration

Each plugin has a section, in the file or the environment ([foundation/configuration](../foundation/configuration.md#plugins)):

```toml
[plugins.credits]
enabled  = true          # on for the instance (default from config.json)
projects = "opt_in"      # all | opt_in | opt_out

[plugins.credits.settings]          # instance settings, validated by the plugin's schema
default_rates = { vcpu_hour = 1, memory_gib_hour = 0.25, disk_gb_hour = 0.005 }
free_disk_gb  = 20

[plugins."git/github"]
enabled = true

[plugins."agents/opencode"]
enabled = false
```

Environment: `LEWBOX_PLUGINS_<NAME>_ENABLED`, `…_PROJECTS`, `…_SETTINGS_<KEY>`, with the name upper-cased and `/` and `-` as `_` (`LEWBOX_PLUGINS_GIT_GITHUB_ENABLED=false`, `LEWBOX_PLUGINS_AGENTS_CLAUDE_CODE_ENABLED`). Two shortcuts: `LEWBOX_PLUGINS_DISABLE=agents/codex,credits` and `LEWBOX_PLUGINS_ENABLE=…`.

- **Unknown plugin names** in configuration are errors, like any unknown key, so a typo never silently leaves a plugin on.
- **A plugin in configuration but not in the build** is an error too ("credits is configured but isn't in this build"), unless `enabled = false`.
- **Secrets** in instance settings take `_FILE` like other settings, and are redacted in `lewbox config print`.
- **Configuration is the only way to change the instance's plugins** (decided 2026-10-29): there's no API or dashboard switch, and nothing about instance enablement is stored in the database. Instance settings are read at start-up. Changing them is a restart, rolled one replica at a time ([registry-and-lifecycle](registry-and-lifecycle.md#turning-a-plugin-on-or-off-for-the-instance)).

### Project enablement

`project_plugins(project_id, plugin, enabled boolean, updated_at, updated_by jsonb)`, one row per project and plugin **only where the project has chosen**. The effective state:

| Instance | Projects mode | Project row | Effective for the project | Can the project change it? |
|---|---|---|---|---|
| off | any | any | **off** (scope `instance`) | No |
| on | `all` | ignored | **on** | No |
| on | `opt_out` | none | on | Yes |
| on | `opt_out` | `enabled = false` | off (scope `project`) | Yes |
| on | `opt_in` | none | off (scope `project`) | Yes |
| on | `opt_in` | `enabled = true` | on | Yes |

A row for a plugin whose mode later becomes `all` is kept but ignored, so switching back restores the project's choice.

New projects get no rows: they follow the defaults. The instance can't force a plugin off for one project only; it can suspend a project ([projects](../projects.md)) or set the plugin's mode.

### API

| Method | Path | Who | Permission | What |
|---|---|---|---|---|
| GET | `/v0/meta` | anyone | — | `plugins: [{name, kind, title, version, enabled}]`: what this instance offers. Lets the dashboard and SDKs adapt before sign-in |
| GET | `/v0/instance/plugins` | S (instance admin) | — | Read-only (no write route; instance enablement is configuration only). Every compiled plugin: `info`, `enabled`, `projects`, instance settings (secrets as `***` and last four), `requires`, `required_by`, and counts of projects with it on |
| GET | `/v0/projects/{prj}/settings/plugins` | S, A | `project.read` | For each plugin on for the instance: `enabled`, `scope` (`instance` or `project`: who decided), `changeable`, `version`, `requires`, `blocked_by`, `used_by` ([projects/settings](../projects/settings.md#plugins)) |
| PATCH | `/v0/projects/{prj}/settings/plugins` | S (owner, admin), A | `project.write` | `{"plugins": {"credits": true, "agents/codex": false}}`: every change in one transaction, all or none → the list |
| GET | `/v0/projects/{prj}/settings/{plugin}` | S, A | `project.read` | The plugin's project settings and its schema (`schema` beside `data`) |
| PATCH | `/v0/projects/{prj}/settings/{plugin}` | S (owner, admin), A | `project.write` | Partial update; validated; secret fields write-only |
| GET, PATCH | `/v0/projects/{prj}/profiles/{prf}/settings/{plugin}` | S, A, L | `profiles.credentials` (L: the plugin's setup section) | Profile settings, for plugins that declare them |

The plugin object:

```json
{
  "name": "credits",
  "kind": "feature",
  "title": "Credits",
  "description": "Compute units, balances and budgets for profiles.",
  "version": "0.1.0",
  "enabled": false,
  "scope": "project",
  "changeable": true,
  "requires": ["limits"],
  "used_by": []
}
```

`settings/plugins` is reserved: no plugin may be named `plugins`. A plugin that registers its own settings routes under the same area (`credits`' rates at `…/settings/credits`) replaces the generic ones.

Errors: `404 not_found` for a plugin not in the build; `409 plugin_disabled` (off for the instance) when turning on; `422 plugin_dependency` with `requires` or `required_by` (as [projects/settings](../projects/settings.md#plugins) has it); `409 plugin_in_use` with `used_by`; `403 permission_denied` for a mode of `all` (`changeable: false`, message saying the instance decides) ([api/errors](../api/errors.md)).

## Security

- Building a binary with a plugin is trusting it fully ([plugins](../plugins.md#security)). `lewbox build` prints each module and its checksum from `go.sum`, and refuses `replace` directives pointing outside the build directory unless `--allow-replace`.
- Turning plugins on or off is audited; only owners, admins and admin keys with `project.write` may do it.

## Observability

- `lewbox version --json` lists `plugins: [{name, version, module, sum}]` and `build: release | custom | dev`.
- `/v0/meta` and the dashboard's About page show the same.

## Testing

- The generator: golden files for the default `config.json`; CI fails if generated files are stale.
- `lewbox build`: an integration test builds with the example plugin added and with `--without agents/codex`, and runs the result's `version` and `config check`.
- Enablement table: every row of the table above, through the API.

## Build plan

1. `config.json`, its schema, and `genplugins` for the Go targets (M0).
2. Configuration sections and the enablement logic (M0).
3. `project_plugins` and the routes (M0).
4. `genplugins` for the dashboard and OpenAPI (M4, with [dashboard-slots](dashboard-slots.md)).
5. `lewbox build` (Beta; usable by us from M4 for tests).

## Open questions

1. **Distributing third-party plugins.** A list on lewbox.com of known plugins with their module paths? Later.
2. **Pre-built custom images.** Should `lewbox build` also produce a container image? Proposed: document a two-line Dockerfile instead.
