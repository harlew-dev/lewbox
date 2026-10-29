# Dockerfile rules

> Status: Draft · Milestone: Alpha (M1, classic builder, no context); BuildKit and contexts after the alpha · Updated: 2026-10-29

A template's Dockerfile is checked when it's saved, so a mistake shows on the form or in the API's `422` at once, naming the line, instead of as a failed build minutes later. This plan covers what's checked, how the rules depend on whether the cluster builds with the classic builder or BuildKit, how build contexts change the rules, and the limits on size.

## Today

`App\Rules\BuildableDockerfile` (`lewbox-legacy/app/Rules/BuildableDockerfile.php`, 341 lines) parses the Dockerfile as Docker does:
- parser directives (`escape`);
- comments and blank lines between continuations;
- line continuations with the escape character.

It reports the **first** problem, with its line. It checks:

- **The start.** The file starts with `FROM`; only `ARG` may come before it.
- **Instructions.** Every instruction is one of Docker's, with arguments.
- **Image and stage names.** `FROM` image references follow Docker's reference grammar (registry host and port, lowercase path, tag, digest), and stage names (`AS name`) are valid. Later `FROM`s may name earlier stages.
- **No build context.** `COPY` can only copy `--from` another stage, and `ADD` only from a URL.
- **The classic builder.** Heredocs (`RUN <<EOF`) are refused, as are BuildKit-only flags (`RUN --mount`, `--network`, `--security`, `COPY --chmod`, `--link`, `--parents`, `--exclude`, `ADD --checksum`, `--keep-git-dir`). The classic builder's own flags are allowed:
  - `FROM --platform`;
  - `COPY --from` and `--chown`;
  - `ADD --chown`.
- **Length.** At most 65 535 characters.

What can't be known without building isn't checked: commands that fail, and base images that don't exist or lack the cluster's architecture. The public docs suggest trying a build locally the way Lewbox does: `DOCKER_BUILDKIT=0 docker build - < Dockerfile`.

## Goals

- Catch everything knowable without building, with the line and a fix in the message.
- Rules that follow what the target clusters can actually build: classic or BuildKit, with or without a context.
- A parser in Go that matches Docker's own, ideally by using it.

## Keep, change, drop

| | |
|---|---|
| **Keep** | First problem only, with its line. `FROM` first (after `ARG`). Known instructions with arguments. Image reference and stage name checks. Length limit |
| **Change** | Use **`github.com/moby/buildkit/frontend/dockerfile/parser`** (Docker's own parser, which the classic builder shares) instead of a hand-written one, so directives, escapes, heredocs and continuations match Docker exactly. Rules depend on the template's **build mode** (classic or BuildKit) and whether it has a **context** |
| **Drop** | The hand-written parser |

## Design

### Build mode

Each cluster's fork version tells us whether it can build with BuildKit ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox), fork change B3). A template's **build mode** is:

- `buildkit` if every one of its clusters supports BuildKit;
- otherwise `classic`.

Adding a classic-only cluster to a BuildKit-using template is refused (`validation_failed` on `clusters`, naming the feature that cluster can't build). The template object shows `build_mode`.

### Checks

`templates.CheckDockerfile(src string, mode BuildMode, hasContext bool) *Problem`:

| # | Check | Mode | Message (example) |
|---|---|---|---|
| 1 | Parses (Docker's parser) | both | `Line 12: unterminated heredoc.` |
| 2 | At most 65 535 bytes (raised to 256 KiB with BuildKit, since contexts make huge Dockerfiles unnecessary; open question) | both | `The Dockerfile is longer than 65,535 characters.` |
| 3 | The first instruction is `FROM`; only `ARG` and parser directives before it | both | `Line 1: a Dockerfile starts with FROM. Only ARG can come before it.` |
| 4 | Every instruction is known and has arguments | both | `Line 7: COPPY isn't a Dockerfile instruction.` |
| 5 | `FROM` names a valid image reference, `scratch`, an earlier stage, or uses `${ARG}`; `AS` names are valid and unique | both | `Line 3: "Ubuntu:24.04" isn't a valid image name: names are lowercase.` |
| 6 | `COPY` without `--from` (and `ADD` of a local path) needs a context | both | `Line 9: COPY needs files beside the Dockerfile. Upload a build context, or copy --from another stage.` |
| 7 | `COPY`/`ADD` sources stay within the context (no absolute paths, no `..` escaping) | both | `Line 9: COPY can't copy from outside the build context.` |
| 8 | Heredocs | classic: refused | `Line 15: heredocs need BuildKit, which this template's clusters don't build with. Join commands with &&.` |
| 9 | BuildKit flags (`RUN --mount/--network/--security`, `COPY --chmod/--link/--parents/--exclude`, `ADD --checksum/--keep-git-dir`) | classic: refused | `Line 20: COPY --chmod needs BuildKit…` |
| 10 | `RUN --mount=type=secret` and `type=ssh` | buildkit: refused until build secrets exist (open question in [templates](../templates.md#open-questions)) | `Line 20: build secrets aren't supported yet.` |
| 11 | `RUN --network=host` and `--security=insecure` | buildkit: refused | `Line 21: --network=host isn't allowed.` |
| 12 | `ONBUILD` with an instruction that breaks these rules | both | as the inner rule |
| 13 | `# syntax=` directive | buildkit: only the default frontend, `docker/dockerfile:1` or a pinned version of it; classic: ignored with a note | `Line 1: only docker/dockerfile frontends are allowed.` |

Checks 6 and 7 use the context's file list when a context is given, so `COPY missing.txt /` is caught too: `Line 9: missing.txt isn't in the build context.`. It's a best-effort glob match, with `.dockerignore` applied.

Not checked, since they're only known by building:
- base images that don't exist, or that lack the cluster's architecture;
- `RUN` commands that fail;
- network fetches.

The template docs keep the "build it locally the way Lewbox does" tip: with BuildKit, `docker build -f Dockerfile ./context`; classic, `DOCKER_BUILDKIT=0 docker build ./context`.

### Where it's checked

- `POST /v0/templates` and `POST /v0/templates/{tpl}/builds` with a new Dockerfile: `422 validation_failed` on `dockerfile`, with `errors.dockerfile = ["Line N: …"]`.
- **New:** `POST /v0/templates/dockerfile-checks` with `{dockerfile, context?, clusters?}` answers `200 {problem: null | {line, message}}`, for the dashboard's editor to check as you type, and for the CLI's `templates push --check`.
- Built-in templates are checked in a unit test, not at runtime.

### Limits

| | Default | Notes |
|---|---|---|
| Dockerfile size | 65 535 bytes (classic), 256 KiB (BuildKit) | |
| Context | 64 MiB compressed, 256 MiB uncompressed, 10 000 files | [builds](builds.md#build-contexts) |
| Build args | 50, names `[A-Za-z_][A-Za-z0-9_]*`, values ≤ 4 KiB | |
| Build time | 45 minutes | [builds](builds.md) |

## Testing

- Port legacy's `BuildableDockerfile` tests (`lewbox-legacy/tests/…`) as a table, adding BuildKit-mode cases and context cases.
- Fuzz the checker with Docker's parser test corpus: it must never panic, and must agree with the parser about what parses.
- Golden messages.

## Build plan

1. `internal/templates/dockerfile.go` on top of the BuildKit parser package (vendored version pinned).
2. The check endpoint.
3. Build mode from cluster capabilities.

## Open questions

1. **Dockerfile size with BuildKit.** Deferred (with BuildKit, after the alpha): keep 64 KiB; contexts remove the inlining that made Lewagent's Dockerfile large.
2. **Linting.** Deferred (with BuildKit, after the alpha): BuildKit's linter findings as a `warnings` array on the check endpoint, never errors.
3. **Private base images.** Deferred (Later): needs registry credentials on the node during builds, with private images for sandboxes ([sandboxes/lifecycle](../sandboxes/lifecycle.md#open-questions)).
