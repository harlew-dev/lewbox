# Dashboard slots

> Status: Draft · Milestone: Alpha (M4) · Updated: 2026-10-29

How plugins appear in the React dashboard and the hosted setup pages. The dashboard ([dashboard](../dashboard.md)) is a shell with named **slots**; a plugin contributes to slots, and a plugin with no React at all still gets a settings form, built from the JSON Schema it registered. Plugins' UI is built with the dashboard (in-tree), shown only where the plugin is on, and loaded lazily so a plugin's code costs nothing on pages that don't use it.

## Today

Legacy's Svelte dashboard hard-codes every agent card, the Git section, the Cloudflare and S3 forms, and the credits pages (`lewbox-legacy/resources/js/pages/**`). Lewagent's React app has the same per-agent components (`lewagent-legacy/resources/js/components/*`).

## Goals

- A plugin's settings need no React: schema in, form out.
- A plugin that needs richer UI (a sign-in flow, a GitHub App connection, a credits ledger) contributes components to slots, typed and lazy.
- Plugins that are off for a project don't appear in it, without a reload when it changes.
- The core's pages are built the same way where it helps, so slots are exercised from the start.

## Design

### How plugins reach the dashboard

Each plugin with UI has `plugins/<name>/web/index.tsx`:

```tsx
// plugins/credits/web/index.tsx
import { definePlugin } from '@lewbox/dashboard-plugin';

export default definePlugin({
  name: 'credits',
  slots: {
    'profile.tabs': [{ id: 'credits', label: 'Credits', route: 'credits', component: () => import('./ProfileCredits') }],
    'project.settings.sections': [{ id: 'credits', label: 'Credit rates', component: () => import('./RatesSettings') }],
    'profile.summary.cards': [{ id: 'balance', component: () => import('./BalanceCard'), order: 20 }],
  },
});
```

- The generator writes `web/src/plugins.gen.ts` with one lazy `import()` per compiled plugin's `index.tsx` ([building-and-enabling](building-and-enabling.md#the-generator)). Vite splits each into its own chunks.
- `@lewbox/dashboard-plugin` is a workspace package in `web/packages/`: `definePlugin`, the slot types, and what a plugin may use from the shell (the API client and query factories, `ui/` components, `Form` and `SchemaForm`, status components, the router's helpers). Plugins import nothing else from the shell, so the shell can change behind it.
- At start, the shell reads `/v0/meta` (instance plugins) and, inside a project, `GET /v0/projects/{prj}/settings/plugins`; it mounts the slots of plugins on for that project. A `project.plugin_enabled` or `project.plugin_disabled` event on the project's live stream ([dashboard/stack-and-structure](../dashboard/stack-and-structure.md#live-updates)) refetches that list, so turning a plugin on shows its pages at once.
- A route belonging to a plugin that's off shows the "Turned off" page: what the plugin does, who can turn it on, a button for owners and admins.

### Slots

| Slot | Where | Contributes | Used by |
|---|---|---|---|
| `project.nav` | The project sidebar, after Templates | A top-level page (`label`, `icon`, `route`, `component`, `badge` query) | A plugin with its own area (none first-party yet) |
| `project.settings.sections` | Settings, as a section with its own route `/p/{project}/settings/{plugin}` | A component, or nothing (the schema form is used) | `credits` (rates), `git/github` (the App), `telemetry` (instance-wide defaults, Later) |
| `project.overview.cards` | Project overview | A card | `credits` (spend this month) |
| `profile.tabs` | A profile's tabs | A tab with a route under the profile | `credits` |
| `profile.summary.cards` | A profile's summary | A card | `credits` (balance), `limits` (usage against limits) |
| `profile.setup.sections` | A profile's setup, in the dashboard **and** on the hosted setup page | A section (agents, Git, telemetry…), with `setupSection` naming the link section it belongs to | `agents/*` (one card per agent), `git/github`, `telemetry` |
| `profile.credentials.cards` | Inside the agents section | A card per agent provider: methods, sign-in buttons, status | `agents/*` (built from the provider registry if the plugin has no component) |
| `cluster.forms.dns` | Create cluster and cluster settings: DNS | The provider's form | `dns/libdns` per provider, or another DNS plugin (schema form by default) |
| `cluster.forms.storage` | Create cluster and cluster settings: storage | The provider's form, with flavour picker | `storage/s3` |
| `sandbox.tabs` | A sandbox's tabs | A tab | Plugins with per-sandbox views (none first-party yet) |
| `sandbox.create.fields` | The create-sandbox dialog's advanced section | Fields for a plugin's [sandbox field](extension-points.md#sandbox-fields) | Plugins owning sandbox fields |
| `sandbox.summary.cards` | A sandbox's overview | A card | `credits` (this run's cost), `git/github` (repositories and their access) |
| `instance.settings.sections` | Instance admin | A section | Plugins with instance settings worth showing (read-only: configuration decides) |
| `activity.renderers` | The activity log | A renderer for the plugin's activity kinds | Every plugin with activity kinds (default: a generic row) |

Each contribution has an `id` (unique within the plugin), an `order` (lower first; the core's own sections use multiples of 100), and an optional `when` predicate over the slot's context (`profile`, `sandbox`, `cluster`) so, for example, a Git card shows only for profiles with a connection.

Every slot gets a typed context: `{ project, profile?, sandbox?, cluster?, can(permission) }`. Components fetch their data through the shared API client; they never get raw tokens.

### Settings forms

The default for every settings scope a plugin registers ([extension-points](extension-points.md#settings)): the shell renders `SchemaForm` from the schema returned with `GET …/settings/{plugin}`.

- Types: string, number, integer, boolean, enum (select or radio by size), array of strings (tag input), object (fieldset), `oneOf` (a picker that swaps the fields, used for storage flavours and telemetry presets).
- UI hints: `x-lewbox-secret` (password field, write-only, "set" badge, *Replace* button; blank on update keeps the value), `x-lewbox-help` (Markdown under the field), `x-lewbox-order`, `x-lewbox-group` (fieldsets), `x-lewbox-depends-on` (shown when another field has a value), `x-lewbox-placeholder`, `x-lewbox-link` ("Create a token" with a URL).
- Validation: client-side from the schema; the server's `422` field errors mapped onto fields by path, as every form does ([dashboard/stack-and-structure](../dashboard/stack-and-structure.md#forms)).
- Test buttons: a schema can name a test operation (`x-lewbox-test: "POST …/dns/tests"`), shown as *Test connection* with the step-by-step result (DNS and storage use it).

So a DNS provider plugin, which is a schema and a Go adapter, gets its form on Create cluster without any TypeScript.

### The setup pages

The hosted setup page ([dashboard/setup-pages](../dashboard/setup-pages.md)) is the same shell in a minimal, project-branded layout, with only `profile.setup.sections` mounted, filtered to the sections the link was made for. Agent cards come from `profile.credentials.cards`: a plugin's own component if it has one (Codex's device sign-in), else a card built from the provider's credential methods ([agent-providers](agent-providers.md#credentials)). The setup session's principal can only reach what those sections need ([api/permissions](../api/permissions.md#setup-sessions)).

### Third-party plugins' UI

A plugin added with `lewbox build --with` that has a `web/` folder is compiled into the dashboard by that build ([building-and-enabling](building-and-enabling.md#custom-builds)), with the same `@lewbox/dashboard-plugin` API and versions as first-party ones. A plugin without a `web/` folder uses schema forms only.

### Later: runtime UI plugins

Loading a plugin's pre-built ES module at run time, without rebuilding the dashboard, would need an import map sharing React, the router and query client with the shell, a stable `@lewbox/dashboard-plugin` API, and a CSP allowing the plugin's assets. It's possible, and the slot API is shaped so it could be added, but not before the plugin API is stable (after `/v1`).

## Security

- Plugins' components run with the dashboard's origin and the user's session: they're trusted like the shell, which is why only built-in code reaches it.
- The CSP is unchanged: no inline script, no remote scripts. Plugin chunks are served from `/assets/` like the rest.
- Secret fields are never sent to the browser: the API returns `{"set": true, "last_four": "…"}`.

## Testing

- Each first-party plugin's UI has component tests (Vitest, React Testing Library) and one Playwright flow in `web/e2e`.
- `SchemaForm`: golden tests over a schema covering every type and hint, and the DNS and storage provider schemas.
- Turning a plugin off: its nav, tabs, cards and settings disappear without a reload; its routes show the "Turned off" page.
- The core-only build's dashboard: no plugin slots filled, every core page works.

## Build plan

1. `@lewbox/dashboard-plugin` package, slot registry in the shell, `plugins.gen.ts` from the generator (M4).
2. `SchemaForm` and the settings section route (M4; DNS and storage forms use it from M1 in the API, and in the dashboard when it lands).
3. Move first-party plugin UI into `plugins/*/web/` as each page is built (M4, Beta for credits).
4. The "Turned off" page and live enable and disable.

## Open questions

1. ~~**`definePlugin` in the plugin folder or in `web/`.**~~ Decided (2026-10-29): beside the plugin's Go code (`plugins/*/web`); the dashboard's `tsconfig` paths and linting cover it.
2. **Slots for Lewagent** (sharing the slot model with `@lewbox/react`). Deferred (Later).
