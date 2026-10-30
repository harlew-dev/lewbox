# Templates

> Status: Draft · Milestone: Alpha (M1) for project and built-in templates; profile templates M4 if time allows, else Beta · Core · Updated: 2026-10-30

A template is a container image built from a Dockerfile, so sandboxes start with their tools installed: Claude Code, a desktop, a language toolchain. A template is built separately on each cluster it's for, into that cluster's registry, and a sandbox created from it runs its **latest ready build** there, keeping that build for life. This plan covers the kinds of template, their fields, which a profile can use, and how they're managed. The parts cover builds, the Dockerfile rules, and the templates Lewbox defines itself.

## Parts

| Plan | What it covers | Milestone |
|---|---|---|
| [builds](templates/builds.md) | Builds per cluster, the `build_template` operation, build contexts, the registry path, size, logs, failures, which build a sandbox uses, rebuilds, new clusters, Firecracker conversion for `microvm` | M1 |
| [dockerfile-rules](templates/dockerfile-rules.md) | What's checked before a build, the classic builder vs BuildKit, build contexts, limits | M1 |
| [provided-templates](templates/provided-templates.md) | Templates defined in Lewbox's code: the internal `agents` template for sign-ins, and a catalog projects can add from | M1 (`agents`), M3 (catalog) |

## Today

- **Tenant templates** are made by the tenant in the dashboard or with the Admin API (`lewbox-legacy/app/Actions/Templates/SaveTemplate.php`, `app/Models/Template.php`). They're usable by every profile, and built on every cluster with cloud storage (`cluster_access: all`) or on the chosen ones.
  - Fields: `id` (slug), `name`, `dockerfile`, `ports`, default `cpu`/`memory_mb`/`disk_gb`, `env` (encrypted), `cluster_access`, `clusters`.
  - The dashboard can import and export a template as JSON.
- **Global templates** are defined in code (`app/Sandboxes/Templates/AgentsTemplate.php`) and written to `templates` with no tenant by `templates:sync`, which runs on deploy and hourly. They're built on each tenant's *sign-in cluster*, and not listed or usable in creates (`lewbox-legacy/docs/api.md` § Templates).
- **Profile templates** (made through the Consumer API, used only by their profile) are documented on lewbox.com and planned, but not built; the `templates` and `template_storage_mb` limits are enforced nowhere.
- **Builds** are a `build_template` operation per cluster. The gateway calls AerolVM's `POST /v1/images/build`: one blocking call, no logs, pushed to `127.0.0.1:5000/cluster/<clu>/templates/<tpl>/<bld>:<bld>` (`lewbox-gateway-legacy/internal/operations/templates.go`).
- **No build context.** `COPY` only `--from` a stage; the classic builder only. Lewagent works around this by inlining every helper file as gzipped base64 into its 521-line Dockerfile, with `{{ file }}` placeholders (`lewagent-legacy/app/Console/Commands/ComputerTemplateCommand.php`, `templates/lewagent-computer/Dockerfile`). Builds take about 20 minutes.

## Goals

- Projects, and later profiles, can build any image a normal Dockerfile builds, **with a build context** (after the alpha, once the fork's build API takes one, F30), so files sit beside the Dockerfile rather than inlined. The alpha builds as legacy does: a Dockerfile alone, the classic builder ([D16](README.md#decisions)).
- Build logs, live and kept.
- A template is usable on a cluster the moment one build there is ready, and a rebuild never interrupts new sandboxes.
- One definition format (JSON plus Dockerfile plus context directory), used by the dashboard, the API and the CLI (`lewbox templates push ./dir`).

**Non-goals:** templates shared across projects (beyond the code-defined catalog); updating existing sandboxes to a new build (see [builds](templates/builds.md#open-questions)); private base images with registry credentials (later).

## Keep, change, drop

| | |
|---|---|
| **Keep** | Ids as slugs, fixed. Fields. Per-cluster builds into the cluster's registry. Latest ready build wins. Sandboxes keep their build. Ports and defaults change without a build and affect new sandboxes only. `template_in_use` on delete. Template limits for profile templates. JSON import and export |
| **Change** | "Tenant templates" become **project templates**. "Global templates" become **built-in templates**, defined in the binary and synced at startup (not hourly). **Build contexts** (a tarball, content-addressed). **BuildKit** where the fork supports it. **Build logs.** One `/v0/templates` API for admin and profile keys ([API](#api)). Kinds named `project`, `profile`, `builtin` |
| **Drop** | Inlining files into Dockerfiles. Hourly `templates:sync`. Separate Admin and Consumer template APIs |

## Kinds

| Kind | Made by | Usable by | Dockerfile shown to | Counts against |
|---|---|---|---|---|
| `builtin` | Lewbox, in code | Lewbox only (`agents`), or nobody until copied (catalog; see [provided-templates](templates/provided-templates.md)) | Project admins | Nothing |
| `project` | A project: the dashboard or an admin key | Every profile in the project | Admin keys and project members; `null` to profile keys | Nothing |
| `profile` | A profile key with `templates.create` | That profile only | That profile, and the project | The profile's `templates` and `template_storage_mb` limits |

### Which templates a profile can use

The project's templates plus its own, on the clusters the key reaches. A create naming any other id is `validation_failed` on `template`. Built-in ids (`agents`) are reserved: no project or profile template can take them, and they can't be used in creates.

### Ids

- Lowercase words joined by single hyphens: `^[a-z0-9]+(-[a-z0-9]+)*$`, at most 64 characters.
- `create` and every built-in id are reserved.
- Unique within a project, across its project templates and all its profiles' templates, so an id always means one template in a project. Once a template is deleted, its id is free again.
- Fixed once created. `name` is for display.

## The template object

| Field | Type | Notes |
|---|---|---|
| `id` | string | The slug |
| `name` | string | Up to 255 |
| `kind` | string | `project` or `profile` (built-ins aren't listed) |
| `profile` | string or null | For `profile` templates, the `prf_` id (shown to admin keys) |
| `dockerfile` | string or null | Null to profile keys for project templates |
| `context` | object or null | **New.** `{digest, size_bytes}` of the build context, or null |
| `cached` | boolean | Default `false`; admin-controlled local storage for the template artifact ([below](#local-template-storage)) |
| `build_args` | object | **New.** `ARG` values passed to builds (not secret; shown) |
| `ports` | array | `{port, protocol, access}`, up to 20; none from 61000 to 61999 |
| `cpu`, `memory_mb`, `disk_gb` | number or null | Defaults for its sandboxes; null for Lewbox's |
| `env` | object | Write-only: names shown, values never. Merged under a create's `env` |
| `hooks` | object | **New.** `{on_create, on_start, on_stop}` its sandboxes run before their own; hook `env` write-only ([sandboxes/hooks](sandboxes/hooks.md#templates)). Changing them needs no build and affects only sandboxes created afterwards, as with ports |
| `stop_grace` | integer or null | **New.** Default `stop_grace` for its sandboxes |
| `scrub` | array | **New.** Paths every sandbox from it removes before a snapshot (a desktop template's browser cookies, say) ([sandboxes/hooks](sandboxes/hooks.md#scrub)) |
| `clusters` | object | `{access: "all" \| "restricted", list: [{id, name}]}` |
| `builds` | array | Its builds on the clusters the key reaches, newest first per cluster ([builds](templates/builds.md#the-build-object)) |
| `created_at`, `updated_at` | string | |

## Local template storage

- **`cached: false` (default):** push builds to the cluster's S3/R2-backed registry; nodes pull them when needed.
- **`cached: true`:** store the built template image or Firecracker template snapshot on node disk instead of offloading it to object storage. Pin the complete artifact against image cleanup. These builds do not require a bucket. This setting covers template artifacts; snapshots of sandboxes created from them still follow the sandbox's persistence policy.
- **Control:** project admins in the dashboard and admin keys can set it for project and profile templates. Built-ins remain instance-admin controlled. Include it in template responses and JSON export/import, enforcing the same write permissions on import.
- **Placement:** record healthy holder nodes per build and place new sandboxes only on nodes with the selected build's complete artifact. Additional nodes need a build or explicit artifact replication before becoming eligible. If no holder is available, return `template_unavailable` with `reason: cache_unavailable`; do not silently fall back to S3/R2 or a different build.
- **Changes and retention:** changing the setting queues replacement builds that capture the new storage mode. Keep the previous ready build usable while its replacement builds. Existing sandboxes keep their original build. Unpin and delete local artifacts only when the normal live-build and grace-period rules permit it. Disk pressure refuses new cache work rather than evicting artifacts still needed. Show retained bytes, holder nodes and failures on the template page.
- **Durability:** retained artifacts survive service restarts. Losing a disk can lose its template artifacts; mark those copies unavailable and rebuild from the definition. Sandboxes relying on lost artifacts may be unable to resume. Explain this tradeoff beside the dashboard setting.
- **Delivery:** depends on fork support for local builds, artifact inventory, pinning and placement (B6 / F56). Until it is in the minimum supported fork release, reject `cached: true` with `422 validation_failed`. This is separate from Docker's build cache and the rebuild option `no_cache`.

## Data model

```sql
CREATE TYPE template_kind AS ENUM ('builtin','project','profile');

CREATE TABLE templates (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug            text NOT NULL,                    -- the public id
  kind            template_kind NOT NULL,
  project_id      bigint REFERENCES projects,       -- null for builtin
  profile_id      bigint REFERENCES profiles,       -- set for profile
  name            text NOT NULL,
  dockerfile      text NOT NULL,
  context_digest  text REFERENCES template_contexts (digest),
  cached          boolean NOT NULL DEFAULT false,
  build_args      jsonb NOT NULL DEFAULT '{}',
  ports           jsonb NOT NULL DEFAULT '[]',
  cpu_millis      integer,
  memory_mb       integer,
  disk_gb         integer,
  env             bytea,                            -- encrypted JSON
  hooks           jsonb NOT NULL DEFAULT '{}',      -- {on_create, on_start, on_stop}; hook env encrypted inside
  stop_grace_s    integer,
  scrub_paths     text[] NOT NULL DEFAULT '{}',
  cluster_access  text NOT NULL DEFAULT 'all',      -- all | restricted
  definition_version integer,                       -- builtin only
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  CHECK ((kind = 'builtin') = (project_id IS NULL)),
  CHECK ((kind = 'profile') = (profile_id IS NOT NULL))
);
CREATE UNIQUE INDEX ON templates (project_id, slug) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX ON templates (slug) WHERE kind = 'builtin';

CREATE TABLE template_clusters (template_id bigint REFERENCES templates, cluster_id bigint REFERENCES clusters,
                                PRIMARY KEY (template_id, cluster_id));
```

`template_contexts` and `template_builds` are in [builds](templates/builds.md#data-model).

## API

One API for both kinds of key. What a key sees and makes depends on its kind:

| Endpoint | Profile key | Admin key and dashboard |
|---|---|---|
| `GET /v0/templates` | The project's templates and its own. Filter `cluster` | All the project's templates and every profile's. Filters: `kind`, `profile`, `cluster` |
| `GET /v0/templates/{tpl}` | One it can use | Any in the project |
| `POST /v0/templates` | Makes a `profile` template (`templates.create`) | Makes a `project` template, or a `profile` one by naming `profile` |
| `PATCH /v0/templates/{tpl}` | Its own: `name`, `ports`, defaults, `env`, `hooks`, `stop_grace`, `scrub` | Any: also `cached` and `clusters` (adding a cluster builds there) |
| `DELETE /v0/templates/{tpl}` | Its own (`templates.delete`) | Any |
| `POST /v0/templates/{tpl}/builds` | Rebuild its own (`templates.create`) | Rebuild any |
| `GET /v0/templates/{tpl}/builds/{bld}` | | |
| `GET /v0/templates/{tpl}/builds/{bld}/log` | Text, or SSE with `Accept: text/event-stream` to follow ([builds](templates/builds.md#logs)) | |
| `POST /v0/template-contexts` | Upload a context tarball; answers its digest ([builds](templates/builds.md#build-contexts)) | |

`POST /v0/templates` fields: `id`, `name`, `dockerfile`, `context` (a digest), `build_args`, `ports`, `cpu`, `memory_mb`, `disk_gb`, `env`, `hooks`, `stop_grace`, `scrub`, `clusters` (`"all"` or a list). Setting `hooks` or `scrub` needs `sandboxes.exec` beside `templates.create`, as on a sandbox. Admin creates also accept `cached`. Profile keys cannot set or change it, even on their own templates (`403 permission_denied`). It also accepts `Idempotency-Key`. It answers `202` with the template and its `pending` builds.

Errors:
- `422 validation_failed`: the id is taken or reserved, Dockerfile problems (with the line), a cluster the key doesn't reach or without storage.
- `429 limit_exceeded` (`templates`, `template_storage_mb`), for profile templates.
- `409 template_in_use` on delete.
- `403 permission_denied`, including a profile key on a project template.

## Who can do what

- **Project members** manage project templates in the dashboard by role ([projects/members-and-roles](projects/members-and-roles.md)).
- **Admin keys** manage every template in the project.
- **Profile keys** manage their profile's own, with `templates.create` and `templates.delete`, and read the project's.
- **Built-in templates** are managed by Lewbox: an instance admin can force a rebuild, and nobody can edit or delete them.

## Deleting

- Refused with `409 template_in_use` while any sandbox created from it isn't deleted, as its snapshots build on the template's layers.
- Otherwise the template is soft-deleted: gone from lists, its id free, its builds no longer usable.
- Its builds' registry repositories are deleted by the registry clean-up after the grace period ([clusters/storage-and-registry](clusters/storage-and-registry.md)).

## Security

- A Dockerfile runs arbitrary commands at build time on a node, as root, under Docker. Builds are placed on nodes with the `docker` runtime (AerolVM requires it), whatever isolation levels the cluster offers its sandboxes ([clusters/runtimes](clusters/runtimes.md)). A profile template's build is as trusted as a sandbox: it isn't isolated by gVisor, which is a real difference.
  - On clusters that don't offer `container` (only `gvisor` or `microvm`), profile templates are **off by default**. A project can enable them per cluster, knowing builds run under plain Docker ([builds](templates/builds.md#security)).
  - Project templates are trusted, as the project owns the nodes.
- `env` is encrypted and write-only.
- `build_args` are shown, so the docs say never to put secrets in them. BuildKit secrets come later (open question).

## Observability

Covered in [builds](templates/builds.md#observability). Webhooks: `template.build.succeeded` and `template.build.failed`.

## Testing

Per part. Plus: admin-only `cached` writes (including import), default `false`, mode changes preserving live builds, and the visibility matrix (which keys see and use which kinds), id uniqueness across project and profile templates, and reserved ids.

## Build plan

1. Migrations: `templates`, `template_clusters`, `template_contexts`, `template_builds`.
2. `internal/templates`: CRUD and visibility; resolution at sandbox create ([sandboxes/lifecycle](sandboxes/lifecycle.md#choosing-the-image)).
3. Builds ([builds](templates/builds.md)), Dockerfile checks ([dockerfile-rules](templates/dockerfile-rules.md)), built-ins ([provided-templates](templates/provided-templates.md)), with the agent plugins' fragments and catalog entries ([plugins/agent-providers](plugins/agent-providers.md)).

Templates are core. Plugins contribute to them in two ways only: agent provider plugins add install fragments to the `agents` built-in and own their catalog entries, and any plugin may ship catalog entries ([plugins/extension-points](plugins/extension-points.md)). Templates may also carry [hooks](sandboxes/hooks.md), which are core.
4. The dashboard pages ([dashboard](dashboard.md)) and the CLI's `lewbox templates push` ([sdks-and-cli](sdks-and-cli.md)).

## Open questions

1. ~~**Profile templates in the alpha?**~~ Decided (2026-10-29): M4 if time allows, else Beta; they need build isolation first ([builds](templates/builds.md#open-questions)).
2. **Restricting project templates to some profiles.** Deferred (Later): every profile in the project can use every project template until someone needs per-profile access.
3. **Build secrets.** Deferred (Later): BuildKit's `--mount=type=secret`, which needs the fork's BuildKit support and secret storage on the template.
4. ~~**Template definition files.**~~ Decided (2026-10-29): two calls in the API (upload a context, then create), one command in the CLI.
