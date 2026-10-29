# Provided templates

> Status: Draft · Milestone: Alpha (M1 `agents`; M3 catalog) · Core, with contributions from the agent plugins · Updated: 2026-10-29

Some templates are defined in Lewbox's own code rather than by a project. There are two kinds:

- **Built-in templates** that Lewbox needs for its own work, of which there is one so far: `agents`, the image agent sign-ins run in.
- **A catalog** of ready-made templates (Claude Code, Codex, a desktop, Next.js, PHP, a minimal base) that a project can add with one click, as a copy it then owns.

This plan covers how they're defined, versioned, synced and built, and how the catalog works.

## Today

- **Global templates** are PHP classes implementing `GlobalTemplate` (`lewbox-legacy/app/Sandboxes/Templates/`). The only one is `AgentsTemplate`:
  - id `agents`;
  - Ubuntu 24.04, Node 24.22.0 (checksummed per architecture), Claude Code 2.1.302 and Codex 0.163.0;
  - a `VERSION` constant written into the image's labels (`com.lewbox.template.version`).
- **Sync.** `templates:sync` (`app/Actions/Templates/SyncGlobalTemplates.php`) writes each definition to `templates` with no tenant. It queues builds on each tenant's **sign-in cluster** where the current Dockerfile hasn't been built. It runs on deploy and hourly.
- **Visibility.** Global templates aren't listed to tenants, can't be used in creates, and their ids are reserved. Builds on a cluster that's no longer a sign-in cluster are cleaned up like a deleted template's.
- **The Settings page** (Agent sign-in) lets the tenant choose the sign-in cluster, see the `agents` build and rebuild it.
- **Examples.** `lewbox-legacy/docs/examples/templates/` holds Dockerfiles for `claude-code`, `codex`, `desktop` (with `desktop.json`: ports 22/tcp public and 6080/http private, 4 vCPU, 8 GB, `DESKTOP_SIZE`), `minimal`, `nextjs` and `php`. They're copied into the dashboard or the Admin API by hand.
- **The public docs** mention `claude-code` as a "provided" template "made by whoever runs your Lewbox account".

## Goals

- Lewbox's own images build reliably on every project that needs them, from definitions pinned in the binary, rebuilt when the binary's definition changes, and never edited by hand.
- A new project gets useful templates in one click, without copying Dockerfiles from docs.
- Projects own what they use: a catalog template added to a project is the project's to change, rebuild or delete.

**Non-goals:** templates shared live across projects (a project's copy doesn't follow catalog updates unless it asks); a public marketplace.

## Keep, change, drop

| | |
|---|---|
| **Keep** | `agents` defined in code, pinned with checksums, labelled with its version, built on each project's sign-in cluster(s), hidden and reserved. Rebuild from the dashboard |
| **Change** | Definitions are Go code embedded in the binary (Dockerfile plus context files via `embed`). Synced **at startup** under the leader lock, and when a project's sign-in clusters change, not hourly. The examples become a **catalog** in the binary, added to a project as a copy |
| **Drop** | The hourly sync. Copying examples from docs |

## Design

### Built-in templates

```go
// internal/templates/builtin
type Builtin struct {
    ID         string            // "agents"
    Name       string            // "Agents"
    Version    int               // bumped with any change
    Dockerfile string            // from embedded files
    Context    fs.FS             // embedded context, may be empty
    Ports      []Port
    Purpose    string            // "sign_in"
}
```

- `agents` (purpose `sign_in`). A Node base with the agent CLIs whose sign-in Lewbox drives ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)): Claude Code and Codex now, the rest later. Its Dockerfile is **assembled**: core provides the base, and each enabled agent provider plugin contributes a fragment (its CLI and ACP adapter install lines, with pinned versions and checksums) ([plugins/agent-providers](../plugins/agent-providers.md)). The template's `Version` is the hash of the base and the fragments, so enabling or upgrading an agent plugin rebuilds it. It includes `lewbox agentd` and the ACP adapters ([agents/agentd](../agents/agentd.md)), so sign-in helpers and future agent probes start instantly.
- **Versions** are pinned in Go constants next to the Dockerfile, with checksums for downloads. `Version` and the pins are written as image labels (`com.lewbox.template`, `com.lewbox.template.version`, `com.lewbox.build`).
- **Bumping a version** is a pull request that changes the constants. CI builds the Dockerfile for amd64 and arm64 on every change, so a broken pin is caught before release ([foundation/testing](../foundation/testing.md)).

### Sync

At startup, after migrations, the leader runs `templates.SyncBuiltins` (an advisory lock makes concurrent starts safe):

1. Upsert each built-in into `templates` (`kind = 'builtin'`, `project_id` null, `definition_version`), storing its Dockerfile and its context (as a `template_contexts` row, though built-ins aren't project-scoped; open question).
2. For each project's **sign-in clusters** that are ready for builds, queue a build of each `sign_in` built-in where no build of the current definition exists (pending, building or ready). A failed build of the current definition isn't retried automatically, as legacy did; an instance or project admin rebuilds it.
3. Builds of a built-in on clusters that are no longer sign-in clusters are marked for clean-up.

Also run when a project changes its sign-in clusters, and when a sign-in cluster becomes ready for builds ([builds](builds.md#when-builds-happen)).

Built-in builds belong to the cluster's project for scoping (`template_builds.project_id`), so the project sees them on its Settings page, though the template itself is global.

### The catalog

Ready-made templates, embedded in the binary (`internal/templates/catalog/<id>/`, each a `template.json`, `Dockerfile` and any context files), ported from `lewbox-legacy/docs/examples/templates/`. Plugins may add catalog entries too: `agents/claude-code` owns `claude-code` and `agents/codex` owns `codex` (`plugins/agents/<name>/templates/<id>/`), shown only while their plugin is enabled for the project; `agents-base` is assembled from the enabled agent plugins' fragments like `agents`:

| Id | What | Ports | Size |
|---|---|---|---|
| `minimal` | Ubuntu 24.04 with git, curl, the basics | | default |
| `claude-code` | `minimal` + Node, gh, ripgrep and Claude Code | | default |
| `codex` | The same with Codex | | default |
| `agents-base` | **New.** Node, gh, Claude Code, Codex, `lewbox agentd` and the ACP adapters preinstalled, so the first prompt doesn't install anything | | default |
| `desktop` | XFCE desktop on `:0`, Selkies streaming on 6080, Chromium, sshd, Node, Claude Code, Codex | 6080/http private, 22/tcp public | 4 vCPU, 8 GB |
| `nextjs` | Node and a Next.js toolchain | 3000/http public | default |
| `php` | PHP 8.4, Composer, Node | 8000/http public | default |

**Adding one:** the dashboard's Templates → New → From the catalog, or `POST /v0/templates` with `{"from_catalog": "desktop", "id": "desktop"}` (admin keys and project members only). Lewbox copies the catalog entry into a **project template**, recording `catalog_id` and `catalog_version`. From then on it's an ordinary project template, editable, rebuildable and deletable.

**Updates:** when the binary's catalog version for an entry is newer than a project's copy, the template's page shows "A newer version is available", with a diff of the Dockerfile and an **Update** button that replaces the definition and rebuilds. It's never automatic. The API is `GET /v0/templates/{tpl}` → `catalog: {id, version, latest_version}`, and `POST /v0/templates/{tpl}/builds` with `{"from_catalog": true}`.

**Listing:** `GET /v0/template-catalog` lists entries (id, name, description, version, ports, size, Dockerfile), for any key, so docs and dashboards can show them.

### The hosted instance

On lewbox.com, "provided" templates in the public docs' sense (made by whoever runs the account) are project templates of the customer's project. Lewbox's own catalog is the same on every instance. The public docs change to describe project and profile templates and the catalog.

## Data model

On `templates`:
- `definition_version integer`, for built-ins;
- `catalog_id text`, `catalog_version integer`, for project templates added from the catalog.

Sign-in clusters are a project setting ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)).

## API

| Endpoint | Does |
|---|---|
| `GET /v0/template-catalog` | List the catalog |
| `GET /v0/template-catalog/{id}` | One entry, with its Dockerfile and context file list |
| `POST /v0/templates` with `from_catalog` | Add one to the project |
| `POST /v0/templates/{tpl}/builds` with `from_catalog: true` | Update a copy to the latest catalog version and rebuild |
| `POST /v0/projects/{prj}/builtin-builds` | Rebuild the built-ins on the project's sign-in clusters (project admins; the dashboard's Settings → Agent sign-in) |

## Security

- Every download in a provided Dockerfile is pinned by version and checksum, or by a signed apt repository with a pinned key fingerprint, as legacy's are.
- Built-in images run sign-in helpers that briefly hold subscription logins ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)), so they contain nothing but what sign-in needs.
- The `desktop` entry binds Selkies to `127.0.0.1` with no auth, and must only be exposed as a private port. Its `template.json` sets that, and the docs say so.

## Observability

- The startup sync logs what it upserted and queued.
- Metric: `lewbox_builtin_builds{template,status}`.

## Testing

- **CI** builds every built-in and catalog Dockerfile for amd64 and arm64 with the build mode the fork uses (classic until B3; [builds](builds.md#fork-changes)), on pull requests that touch them. Nightly for all.
- **Unit:**
  - sync is idempotent;
  - a changed definition queues builds only where needed;
  - sign-in cluster changes;
  - catalog copy and update.
- **Dockerfile rule tests:** every embedded Dockerfile passes [dockerfile-rules](dockerfile-rules.md).

## Build plan

1. `internal/templates/builtin` with `agents`, ported from `AgentsTemplate.php`; `SyncBuiltins` at startup (M1).
2. Built-in rebuilds from the dashboard (M1/M3, with sign-ins).
3. The catalog: port the six examples plus `agents-base`; catalog endpoints; add and update (M3).
4. CI workflow building the images.

## Open questions

1. **Contexts for built-ins.** Built-ins aren't in a project, but `template_contexts` rows are project-scoped. Store built-in contexts as a separate table, or pass them to the build straight from the binary's embedded files? Proposed: straight from the binary, no row.
2. **Prebuilt images.** Instead of every project building `agents` and the catalog on its own nodes, publish them to a public registry (`ghcr.io/harlew-dev/lewbox-…`), multi-arch and signed, and let templates `FROM` them, or use them directly as `image`. Much faster first runs. Projects that want to build everything themselves still can. Proposed for beta.
3. **More agents.** Cursor, Grok, OpenCode, pi and Antigravity in `agents` when their sign-ins are ported. One image, or one per agent?
