# Builds

> Status: Draft · Milestone: Alpha (M1, Dockerfile builds as legacy); build contexts and BuildKit after the alpha (fork F30, F29); Firecracker templates later · Updated: 2026-10-29

A build is one template built on one cluster: its Dockerfile, with its build context, built by AerolVM on one of the cluster's nodes and pushed to the cluster's registry. Each template has a build per cluster it's for, and a new one there each time it's rebuilt. This plan covers when builds happen, the `build_template` operation, build contexts, the registry path, sizes, logs, failures, which build a sandbox uses, and the Firecracker conversion for clusters offering `microvm`.

## Today

- **Queueing.** `BuildTemplate` (`lewbox-legacy/app/Actions/Templates/BuildTemplate.php`) queues a build per target cluster that's ready (cloud storage and an online node), each with a `build_template` operation, holding the template row so two saves don't both queue.
  - Without `rebuild`, it only builds where there's no build of the current Dockerfile, so a failed build stays failed.
  - A rebuild builds again on every cluster without one in progress.
  - `onCluster` queues every all-clusters template, plus the globals on a sign-in cluster, when a cluster first becomes ready.
- **The operation** (`lewbox-gateway-legacy/internal/operations/templates.go`; `docs/operations.md`):
  1. It does nothing if the build is already `ready`.
  2. It fails without cloud storage.
  3. It reports `building`, then runs one blocking `POST /v1/images/build` on any node. AerolVM places it on a node with the `docker` runtime and pushes to `repository:tag` with the lowercased cluster id and the PAT as credentials.
  4. The call is bounded by `GATEWAY_AEROLVM_BUILD_TIMEOUT` (25 min).
  5. It reports `ready`. No steps are recorded.

  A Dockerfile that fails is `build_failed`, recognised by `build image:` in the message. A failed build is failed by the app.
- **Image reference:** `127.0.0.1:5000/cluster/<clu, lowercased>/templates/<template id>/<bld, lowercased>:<bld>`, one repository per build, so registry clean-up can delete a build with its repository (`app/Models/TemplateBuild.php`, `lewbox-legacy/docs/clusters.md` § Templates and image distribution).
- **Size.** `MeasureTemplateBuild` reads the pushed manifest from the cluster's bucket and sums the config and layers (`size_mb`).
- **No build logs**: AerolVM's build call answers once. A failed build reports "the last thing Docker said".
- **Cache.** AerolVM reuses a build of the same Dockerfile on the same node for a day (tagged by a hash of the Dockerfile and node), so an unchanged rebuild within a day pushes the same image. No option skips it.
- **Placement.** Builds go only to nodes listing the `docker` runtime, so gVisor nodes must list `docker,gvisor` (`SB_HOST_RUNTIMES`; legacy fix script `docs/examples/fix-gvisor-host-runtimes.sh`).
- **No context.** Dockerfile only, and the classic builder only ([dockerfile-rules](dockerfile-rules.md)).
- **Lewagent's builds take ~20 minutes**, and existing computers never get template updates (`lewagent-legacy/docs/todo.md`).

## Goals

- Every template build on every cluster it's meant for, as soon as the cluster can build.
- Contexts: files beside the Dockerfile.
- Logs you can follow live, and read after.
- Fast rebuilds: Docker's layer cache works, and can be skipped on request.
- Sizes known for every ready build.
- Clear failures: the failing step and its output.

## Keep, change, drop

| | |
|---|---|
| **Keep** | A build per template per cluster. One registry repository per build. Latest ready wins. Failed builds aren't retried on their own. New clusters get builds. The operation's failure codes |
| **Change** | Build contexts. BuildKit where the fork supports it. Logs streamed from the fork and kept. Size returned by the fork's build result (F33; until that's in the minimum release, measured from the bucket as legacy does, and that code goes when it is). Steps recorded. `no_cache` option. Builds record exactly what they built (Dockerfile, context digest, build args) |
| **Drop** | Inferring `build_failed` from message text (the fork returns a code) |

## Design

### When builds happen

| Trigger | Builds |
|---|---|
| A template is created | On each of its clusters that's ready |
| A template is rebuilt (`POST …/builds`), optionally with a new Dockerfile, context or build args | On each of its clusters (or the ones named), except where one is already `pending` or `building` |
| A template's `clusters` gains one | On that cluster |
| A cluster becomes ready (first node online with cloud storage) | Every template meant for it that has no build of its current definition there |
| A built-in template's definition changes (on startup) | Where it's needed ([provided-templates](provided-templates.md)) |

A cluster is **ready for builds** when it has cloud storage, at least one online node with the `docker` runtime, and isn't suspended.

`templates.QueueBuilds(template, clusters, rebuild)` runs in a transaction holding the template row. It inserts a `template_builds` row (`pending`) with the definition snapshot (Dockerfile, context digest, build args), plus its `build_template` operation and job ([sandboxes/operations](../sandboxes/operations.md)).

### Build contexts

**New.** A template can have a context: a gzipped tarball of the files its Dockerfile `COPY`s.

1. **Upload:** `POST /v0/template-contexts`, with the body a `.tar.gz` (`Content-Type: application/gzip`), up to `LEWBOX_TEMPLATE_CONTEXT_MAX` (64 MiB compressed, 256 MiB uncompressed). The server:
   1. streams it, hashing as it goes, and checks it's a valid tar.gz;
   2. checks that no entry has an absolute path, `..`, a device, or a link out of the tree;
   3. stores it content-addressed.

   It answers `{digest: "sha256:…", size_bytes}`. The same content uploaded twice is stored once. Profile keys need `templates.create`.
2. **Use:** create or rebuild with `context: "sha256:…"`. The digest must be one the project uploaded (contexts are scoped to the project).
3. **Storage:** in Postgres (`template_contexts`, `bytea`), as contexts are small (Lewagent's is under 1 MiB). It's self-host friendly, with no object storage needed. A context no template or build references for 7 days is deleted. If contexts grow, move the bytes to the instance's object storage when one is configured (open question).
4. **Build:** the operation streams the context to the fork's build API (fork change B1).
5. **`.dockerignore`** in the context is honoured by Docker.

The CLI tars a directory for you: `lewbox templates push ./templates/lewagent-computer` reads `template.json` and `Dockerfile`, tars the rest, uploads it and creates or rebuilds ([sdks-and-cli](../sdks-and-cli.md)). That replaces Lewagent's `ComputerTemplateCommand` and its base64 inlining.

### The build operation

`build_template` ([sandboxes/operations](../sandboxes/operations.md#build_template)), steps `built` → `measured`:

1. If the build is already `ready`, finish. If the cluster has no cloud storage, fail (`operation_failed`, "The cluster has no cloud storage to keep templates in.").
2. Mark the build `building`, with `started_at` and the node chosen (from the fork's response or its log header).
3. Call the fork's build API (B1–B3):

   ```
   POST /v1/images/build   (multipart: dockerfile, context.tar.gz, JSON options)
   options: { tags: ["127.0.0.1:5000/cluster/<clu>/templates/<tpl>/<bld>:<bld>"],
              push: true, push_auth: {username: <clu>, password: <PAT>},
              buildkit: true|false, build_args: {...}, no_cache: bool,
              platform: "linux/<arch>", labels: {com.lewbox.template, com.lewbox.build} }
   → streams log lines (NDJSON); final line {status, image, digest, size_bytes, error, error_code}
   ```

4. Log lines are appended to the build's log as they come (see [Logs](#logs)). The job heartbeats while the stream is open.
5. On success, record `image`, `digest` and `size_bytes` → `built`. Until F33 is in the minimum fork release, the size is measured from the bucket as legacy does → `measured`; one way at a time, switched when F33 is the minimum. Then mark the build `ready`, with `finished_at`.
6. Timeouts: `LEWBOX_TEMPLATE_BUILD_TIMEOUT` (45 min, up from 25, since Lewagent's take 20) within the operation's deadline (raised to 60 min for builds).
7. Failures:
   - `error_code: build_failed` from the fork (coded errors, F6): fail at once with the failing step's output (the last 4 KiB of the log) in `error`;
   - capacity (no docker node with room): retried for the capacity wait, then `cluster_capacity_exceeded`;
   - an unreachable node or a failed push: transient, retried.

   A failed operation marks the build `failed` with its error.

A resumed build (after a crash) **starts again**. The cache makes that cheap when it lands on the same node, and pushing the same tag again is harmless. The fork may later let us reattach to a running build (open question).

### The build object

| Field | Type | |
|---|---|---|
| `id` | string | `bld_…` |
| `template` | string | Template id |
| `cluster` | object | `{id, name}` |
| `status` | string | `pending`, `building`, `ready`, `failed` |
| `dockerfile_digest` | string | **New.** sha256 of the Dockerfile it built |
| `context` | string or null | **New.** The context digest |
| `size_bytes` | integer or null | Compressed size in the registry (was `size_mb`) |
| `error` | string or null | |
| `error_code` | string or null | `build_failed`, `cluster_capacity_exceeded`, `cluster_unavailable`, `operation_failed` |
| `operation` | string | `op_…` |
| `created_at`, `started_at`, `finished_at` | string or null | |

### Logs

**New** (fork change B2: stream the build's output).

- Lines are stored in `template_build_logs`, in chunks of up to 64 KiB, capped at 4 MiB per build, keeping the start and the end when it's longer, with a marker between.
- `GET /v0/templates/{tpl}/builds/{bld}/log` returns `text/plain`. With `Accept: text/event-stream`, it replays and then follows live as SSE (`line` events, then `end` with the status), across replicas via `NOTIFY build_log` ([api/streaming](../api/streaming.md)).
- Secrets: the PAT is never in the log (it's the fork's push credential, not a build arg). Build args are shown anyway.
- Logs are kept with the build, and deleted when the build is cleaned up.

### Which build a sandbox uses

- At create, the template's **latest `ready` build on the sandbox's cluster** (by `finished_at`). It's recorded on the sandbox (`template_build_id`), which keeps it for life, through every stop and start, since its snapshots build on it.
- A rebuild in progress or failed doesn't affect new sandboxes; they keep using the latest ready one.
- No ready build: `422 template_unavailable` with a `reason`:
  - `no_cloud_storage`;
  - `not_on_cluster` (not one of the template's clusters);
  - `building` (the first build there isn't done);
  - `failed` (failed with no earlier ready build);
  - `not_built` (meant for the cluster but no build yet, e.g. it became ready since).

### Old builds

Builds superseded by a newer ready one, failed builds, and all of a deleted template's builds are deleted from the registry after the grace period. **But a build that a sandbox (not deleted) was created from is kept**, since starting it needs the base layers. (Legacy deleted superseded builds' repositories, relying on snapshots linking the layers; that holds for a stopped sandbox's snapshot, but not for a running sandbox that's never been snapshotted and might be resumed on another node later. So we keep the repository.) The registry sweep is [clusters/storage-and-registry](../clusters/storage-and-registry.md)'s; this plan supplies which builds are live.

### Firecracker clusters

**Later.** A build is per cluster, not per isolation level: `container` and (from the beta) `gvisor` sandboxes run the same OCI image, so one build serves both. Only `microvm` needs more, once it's offered.

AerolVM's Firecracker sandboxes run from AerolVM *templates* (`rootfs.ext4` plus a memory snapshot), built from a container image (`POST /v1/templates`, operator only). On a cluster that offers `microvm` ([clusters/runtimes](../clusters/runtimes.md#what-a-cluster-offers)), a Lewbox build has a third step, `converted`: after pushing the image, call the fork's template create from it, which pushes to `cluster/<clu>/templates/<tid>` and lets peers pull lazily. Creates use that template id. Memory snapshots restore only on the same CPU model, kernel and Firecracker version, so the fork pins them per cluster ([clusters/runtimes](../clusters/runtimes.md)). The build records both artifacts (`image` and `firecracker_template`), and is `ready` for a level once its artifact for that level is. Adding `microvm` to a cluster later converts each template's latest ready build there (an operation per build, no rebuild). A `microvm` create from a template whose conversion isn't ready is `422 template_unavailable` with `reason: building` (or `failed`), while `container` and `gvisor` creates use the image already. Built as soon as the fork supports Firecracker templates; until then a cluster can't offer `microvm`.

### Existing sandboxes and new builds

A sandbox keeps its build. Updating a running or stopped sandbox in place isn't possible: its snapshots are layers on top of the old image. For Lewagent's computers, the options are in [open questions](#open-questions).

## Data model

```sql
CREATE TABLE template_contexts (
  digest      text PRIMARY KEY,                 -- sha256:…
  project_id  bigint NOT NULL REFERENCES projects,
  size_bytes  bigint NOT NULL,
  bytes       bytea NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE build_status AS ENUM ('pending','building','ready','failed');

CREATE TABLE template_builds (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id         text NOT NULL UNIQUE,         -- bld_…
  project_id        bigint REFERENCES projects,   -- null for builtin
  template_id       bigint NOT NULL REFERENCES templates,
  cluster_id        bigint NOT NULL REFERENCES clusters,
  node_id           bigint REFERENCES nodes,
  status            build_status NOT NULL DEFAULT 'pending',
  dockerfile        text NOT NULL,
  dockerfile_digest text NOT NULL,
  context_digest    text REFERENCES template_contexts (digest),
  build_args        jsonb NOT NULL DEFAULT '{}',
  buildkit          boolean NOT NULL,
  image             text,                          -- registry reference
  image_digest      text,
  size_bytes        bigint,
  error             text,
  error_code        text,
  started_at        timestamptz,
  finished_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON template_builds (template_id, cluster_id, status, finished_at DESC);

CREATE TABLE template_build_logs (
  build_id  bigint NOT NULL REFERENCES template_builds ON DELETE CASCADE,
  seq       integer NOT NULL,
  chunk     text NOT NULL,
  PRIMARY KEY (build_id, seq)
);
```

`template_contexts` are scoped by project, so a digest from another project is never usable even if the content matches (no cross-project existence oracle).

## API

| Endpoint | Does |
|---|---|
| `POST /v0/template-contexts` | Upload a context |
| `POST /v0/templates/{tpl}/builds` | Rebuild: `dockerfile?`, `context?`, `build_args?`, `clusters?`, `no_cache?`. Answers `202` with the template, new builds first |
| `GET /v0/templates/{tpl}/builds` | **New.** List builds, newest first, filter `cluster`, `status` |
| `GET /v0/templates/{tpl}/builds/{bld}` | One build |
| `GET /v0/templates/{tpl}/builds/{bld}/log` | Log; SSE to follow |

Rebuilding a built-in template is for instance admins only (`403` otherwise).

## Security

- Builds run arbitrary Dockerfile commands as root under **Docker** on a node, outside gVisor or Firecracker isolation. Project templates are trusted (the project owns the nodes). Profile templates are a profile running code on the project's node *outside the sandbox boundary*. So:
  - they're off by default on clusters that don't offer `container` (a cluster offering only `gvisor` or `microvm` promises more isolation than a plain-Docker build gives);
  - the project can enable them per cluster;
  - the fork's builder should run builds in a rootless BuildKit or a gVisor-wrapped builder (fork change B4; the open question below).
- Contexts are checked for path traversal and links, and are scoped to their project.
- Push credentials (the PAT) go to the fork in the request, never into the build or its log.

## Observability

- **Metrics:**
  - `lewbox_template_builds_total{outcome,error_code}`;
  - `lewbox_template_build_duration_seconds`;
  - `lewbox_template_build_queue_seconds`;
  - `lewbox_template_image_bytes` (histogram);
  - `lewbox_template_context_bytes`.
- **Webhooks:** `template.build.succeeded`, `template.build.failed` (with error and error code).
- **Activity:** builds are template events, not sandbox ones. The dashboard shows them on the template's page.

## Configuration

| Variable | Default |
|---|---|
| `LEWBOX_TEMPLATE_BUILD_TIMEOUT` | `45m` |
| `LEWBOX_TEMPLATE_CONTEXT_MAX` | `64MiB` (compressed) |
| `LEWBOX_TEMPLATE_LOG_MAX` | `4MiB` |
| `LEWBOX_TEMPLATE_BUILD_GRACE` | `6h` (before deleting superseded builds' repositories) |

## Fork changes

| # | Change | Why |
|---|---|---|
| B1 | Build API takes a context tarball (multipart, or a URL the node fetches) | Build contexts |
| B2 | Build API streams logs (NDJSON), ending with a result object with `error_code` (`build_failed`, `push_failed`, `capacity_exceeded`), `image_digest` and `size_bytes` | Logs; codes instead of phrases; sizes without reading the bucket |
| B3 | `buildkit: true` (Docker's `/build?version=2`, or a BuildKit daemon) and `no_cache` | Modern Dockerfiles; skipping the cache |
| B4 | Builds isolated from the host: rootless BuildKit, or builds inside a gVisor sandbox | Profile templates on shared nodes |
| B5 | Firecracker template from a pushed image, per cluster | Templates on Firecracker clusters |

These map to the fork backlog: B1 is F30 and B3 is F29 (both P2), B2 is F32/F33 (P1), B5 is F26/F27 (Firecracker, later). **Contexts and BuildKit come after the alpha:** until B1 and B3 are in Lewbox's minimum fork release, they're refused for the whole instance (`validation_failed`: "Build contexts aren't available on this instance yet."), and builds work as legacy's do: a Dockerfile alone, the classic builder. There's no per-cluster detection ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)).

## Testing

- **Fake AerolVM builder:** streams scripted log lines and ends ready, failed or capacity-refused, with or without size.
- **Queueing:** creates, rebuilds (skipping in-progress clusters), new clusters, cluster access changes, and concurrency (two saves).
- **Contexts:** upload limits, traversal, dedupe, scoping, garbage collection.
- **Logs:** replay plus follow across two replicas; truncation keeping start and end.
- **Resolution:** latest ready wins, and each `template_unavailable` reason.
- **Live:** build Lewagent's `lewagent-computer` with its directory as context (no inlining) on a gVisor cluster, and time it.

## Build plan

1. Migrations; `QueueBuilds`; the `build_template` handler against legacy's fork API (no context, no logs) so builds work in M1.
2. Context upload and storage.
3. Fork B1–B3, then the handler's streaming path, logs table and log endpoint.
4. Webhooks; the dashboard's build pages; the CLI's `templates push`.
5. B4 and B5 later.

## Open questions

1. ~~**Updating Lewagent's computers to a new build.**~~ Decided (2026-10-29): in the alpha, Lewagent makes a new computer and clones; an `upgrade` operation (a fresh sandbox from the new build, with `/root/workspace` and chosen home directories copied over) is planned for the Beta.
2. **Where contexts live** when they're large. Deferred (with contexts, after the alpha): Postgres up to tens of MiB; optional instance object storage when it's set.
3. **Reattaching to a running build** after a control-plane crash. Deferred (Later): ask the fork once builds are asynchronous.
4. **Build isolation (B4).** Spike (before profile templates, M4 or Beta): try rootless BuildKit and a gVisor builder on AerolVM's build path; whichever runs a hostile Dockerfile without reaching the node decides.
