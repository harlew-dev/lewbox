# Files

> Status: Draft · Milestone: Alpha (M2); conditional writes and `Range` with fork F51 and F7 (P1); watching Beta (F9) · Updated: 2026-10-29

Read and write files in a running sandbox, move and remove them, check what's at a path, list and make directories. Bodies are the files themselves, not JSON, streamed both ways. Everything in a sandbox runs as root, so any path can be reached and written files belong to root.

## Today

- **Handlers:** `lewbox-gateway-legacy/internal/server/files.go` (297 lines): `readFile`, `writeFile`, `moveFile`, `removeFile`, `showEntry`, `listDirectory`, `makeDirectory`, `pathQuery`.
- **Toolbox:**
  - envd's `GET`/`POST toolbox/envd/files?path=` stream an upload to disk with `io.Copy`, making directories.
  - `filesystem.Filesystem/{Stat,ListDir,MakeDir,Move,Remove}` take and return JSON.
  - The older multipart upload route caps forms at 256 MB (`SB_UPLOAD_MAX_BYTES`), which is why the gateway uses envd.
- **The limit:** both download routes read the whole file into the toolbox's memory (`os.ReadFile`). The docs warn: "a very large download is limited by the sandbox's memory".
- **Docs:** [files](https://lewbox.com/docs/inside-a-sandbox/files), [directories](https://lewbox.com/docs/inside-a-sandbox/directories).
- **Activity:** `file.written`, `file.moved`, `file.removed`, `directory.made`. Reads aren't logged.

**Who uses it**, in Lewagent (`lewagent-legacy`):
- **Uploads, a directory picker** (`<ComputerBrowser>`), **and the planned Monaco editor** (`docs/editor.md`). All of these run in the browser with a sandbox key.
- **Setup** writes `.env` files and scripts (`RunSetupStep`).
- **Artifacts** read `/root/artifacts/.manifest.jsonl` and download files up to 1 GB (`SyncComputerArtifacts`, `ImportArtifact`).

**What Lewagent is waiting on** (`docs/editor.md` § *What Lewbox needs*, `docs/todo.md` § *Waiting on Lewbox*):
1. **Conditional writes.** "`If-Unmodified-Since` or an ETag on `PUT /sandboxes/{sbx}/files`, answering `412` when the file changed, so a save can't overwrite the agent's edit unseen." Today the editor compares `modified_at` and then writes, and loses the race.
2. **File-change events.** "Nothing polls beyond that, as Lewbox has no file watching." The editor rechecks open files when a tool call ends, a turn ends, or a panel is focused.

## Goals

- Keep the documented API: paths in the query, raw bodies, the entry object, `depth`, the error codes.
- Downloads of any size, streamed, with `Range`, once the fork streams them.
- Conditional writes with ETags (`If-Match`, `If-None-Match`), answering `412`.
- Watching a directory for changes, as an event stream.
- Never hold a whole file in Lewbox's memory either way.

### Non-goals

- A sync protocol (rsync-like) in the alpha. Use `rsync` over [SSH](ssh.md).
- Changing ownership or permissions through the API. Run `chown`/`chmod` as a command.

## Design

### Endpoints

| Method and path | What | Answers |
|---|---|---|
| `GET /v0/sandboxes/{sbx}/files?path=` | Download | `200`, `application/octet-stream` |
| `PUT /v0/sandboxes/{sbx}/files?path=` | Upload, replacing; makes directories | `200` with the entry (`201` when it made the file, new) |
| `PATCH /v0/sandboxes/{sbx}/files?path=` | Move or rename, body `{"path": "<new>"}`; makes directories | `200` with the entry at its new path |
| `DELETE /v0/sandboxes/{sbx}/files?path=` | Remove; a directory with everything in it | `204` |
| `GET /v0/sandboxes/{sbx}/entries?path=` | The entry at a path: says whether it exists | `200` or `404` |
| `GET /v0/sandboxes/{sbx}/directories?path=&depth=` | List, `depth` 1–10, default 1; every level in one list, no paging; the directory itself left out | `200` with entries |
| `POST /v0/sandboxes/{sbx}/directories` | Make, body `{"path"}`, as `mkdir -p` | `201` with the entry, `409 already_exists` |
| `GET /v0/sandboxes/{sbx}/files/watch?path=&recursive=` | **New.** Watch for changes (SSE) | Events, below |

### The entry object

As documented: `name`, `path`, `type` (`file` or `dir`), `size`, `mode`, `permissions` (octal string), `owner`, `group`, `modified_at`, `symlink_target`. Adds:

| Field | |
|---|---|
| `etag` | **New.** An opaque version of a file's contents, for conditional writes. `null` for directories |

**How the ETag is made.**
- **The fork computes it** (F51), as `"<size>-<mtime ns>-<inode>"`, cheap, from `stat`. A content hash would mean reading the whole file on every stat. Lewbox doesn't make its own: until F51 is in the minimum fork release, entries have no `etag` and conditional writes aren't offered ([D15](../README.md#decisions)).

The docs say an ETag changes whenever the file is written; it may also change when the file is touched without being changed.

### Downloads

- **Streamed** from the toolbox to the client, never held in Lewbox: `io.Copy` with a 32 KB buffer, counting bytes as [activity](toolbox.md#activity).
- **Headers:** `Content-Type: application/octet-stream`, `Content-Length` when the toolbox gives it, `Content-Disposition: attachment; filename="<name>"` (RFC 6266 encoded), `ETag`, `Last-Modified`.
- **Conditional reads:** `If-None-Match` answers `304` when unchanged, so an editor can recheck a file cheaply.
- **Large files.** The fork streams downloads (F7, P1). Until F7 is in the minimum fork release, a download has a **documented size limit**, `LEWBOX_FILE_MAX_DOWNLOAD` (512 MiB), refused with `422 sandbox_refused` saying to compress or split, because today's toolbox reads the whole file into memory. It's a stated limit, not a second code path: the limit and its setting go when F7 is the minimum.
- **`Range`** (with F7): `Range: bytes=…` answers `206`, so large downloads can resume and editors can read the head of a file.
- **A directory** is `422 sandbox_refused` ("is a directory"). `404` when nothing is there.

### Uploads

- **Streamed** to the toolbox's envd route, so a large upload never has to fit in memory on either side.
- **Wait for the toolbox first.** A request body can't be sent twice, so the toolbox is checked with `health` (retried as usual) before the body is streamed; a failure after that isn't retried.
- **Conditional writes** (`If-Match`, `If-None-Match`):
  - `If-Match: "<etag>"` writes only if the file's current ETag matches, else `412 precondition_failed` with the current entry in the body, so the client can show a diff.
  - `If-None-Match: *` writes only if nothing is at the path, else `412`.
  - **The fork checks and writes atomically** (F51, P1): it writes to a temporary file beside the target, then checks the precondition, then `rename(2)`. Lewbox has no compare-then-write of its own; until F51 is in the minimum release, `If-Match` and `If-None-Match` on a write are `422 validation_failed` ("conditional writes aren't available on this instance yet"), so a client never believes it has a guarantee it doesn't.
- **Atomic replace** comes with F51 too: every upload writes beside the target and renames, so a reader never sees half a file. Until then envd writes in place, and the docs say so.
- **Size limits:** none from Lewbox beyond the sandbox's disk. A full disk is `507`-like: `422 sandbox_refused` with the toolbox's "no space left on device".
- **Activity:** `file.written` with `path`, `size`, and `conditional: true` when it was.

### Move, remove, make

- **Move:** `Move {source, destination}`; makes the destination's directories. Something already at the destination is replaced for files, and refused with `409 already_exists` for directories, matching `rename(2)`. Writes `file.moved`.
- **Remove:** `rm -r`. Answers `204`, or `404` when nothing was there, as documented, so a client can tell a typo from a removal ([open question 2](#open-questions)). Writes `file.removed`.
- **Make directory:** `MakeDir`, with parents; `409 already_exists` when anything is at the path. Writes `directory.made`.

### Watching

`GET /v0/sandboxes/{sbx}/files/watch?path=/root/workspace&recursive=true`: an SSE stream of changes under a directory. Beta: it ships when the fork's watching (F9) is in the minimum release, not before.

| Event | Data |
|---|---|
| `created` | `{"path", "type"}` |
| `written` | `{"path", "etag"}` |
| `removed` | `{"path"}` |
| `renamed` | `{"path", "from"}` |
| `overflow` | `{}`: too many changes to report one by one; re-list what you care about |

- **Backed by envd's `Filesystem/WatchDir`**, which AerolVM's toolbox already has (unused today), using inotify. The fork's work is to expose it as SSE or a WebSocket on the node API, and to coalesce events (100 ms per path).
- **Ignored by default:** `.git/objects`, `node_modules`, `.worktrees/*/node_modules`, so a `git gc` or `npm install` doesn't flood clients. `ignore=` adds patterns, `ignore=none` turns the defaults off.
- **Limits:** inotify watches are per sandbox (`fs.inotify.max_user_watches`). A recursive watch over a huge tree may fail, with `422 sandbox_refused` and the reason. At most 8 watches per sandbox key.
- **Keepalives** every 15 s. Watching doesn't count as activity: an open editor shouldn't keep a sandbox awake.
- **Webhooks:** none. File changes are too frequent; this is for live clients.

### Paths

- **Absolute paths only.** `~` and `~/…` are resolved to `/root` as for commands. A relative path is `422 validation_failed` on `path`. (Legacy passes relative paths to the toolbox; we refuse them, since there's no working directory they'd mean.)
- **Normalised** with `path.Clean`. `..` can't escape `/`, and there's nowhere to escape to anyway.
- **In the query,** percent-encoded. SDKs and the CLI always encode it.
- **`path_forbidden` (`403`):** paths the toolbox refuses. With the fork these are documented:
  - `/proc`, `/sys` and `/dev` writes;
  - `/usr/local/bin/toolboxd`;
  - the helpers' mount `/usr/local/lib/lewbox` (read-only);
  - `/root/.lewbox/agents/*/` token files, which hold agentd's upload token.
- **Symlinks** are followed for reads and writes, as root would; `symlink_target` shows where one points.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.exec` |
| `403` | `path_forbidden` | The toolbox refused the path |
| `404` | `not_found` | Nothing at the path, or no such sandbox |
| `409` | `sandbox_not_running` | Files are kept while it's stopped, but can't be reached |
| `409` | `already_exists` | Making a directory, or moving a directory onto something |
| `412` | `precondition_failed` | **New.** `If-Match` or `If-None-Match` didn't hold. Carries the current `entry` |
| `422` | `validation_failed` | No `path`, a relative one, `depth` out of 1–10 |
| `422` | `sandbox_refused` | The toolbox refused: listing a file as a directory, a disk full, a download too large without streaming, watching unsupported |
| `502` | `sandbox_error` | The toolbox failed, e.g. downloading a directory |
| `503` | `toolbox_unavailable` | |

## Security

- **Root reaches everything** in its own sandbox. Nothing outside the sandbox is reachable through paths: the toolbox's root is the container's.
- **Agent tokens** live under `/root/.lewbox/agents`. They're readable by root in the sandbox by design: agentd needs them, and they only reach that conversation's uploads ([agents/agentd](../agents/agentd.md)). The files API refuses them anyway, so a browser with a sandbox key can't lift one in a single request.
- **`Content-Disposition: attachment`** on every download, and `X-Content-Type-Options: nosniff`, so a browser opening a download URL directly never renders it in Lewbox's origin.
- **Reads aren't logged.** That's an open question for secrets ([overview](../inside-a-sandbox.md#open-questions)).

## Observability

- **Metrics:** `lewbox_file_bytes_total{direction}`, `lewbox_file_requests_total{op, outcome}`, `lewbox_file_precondition_failed_total`, `lewbox_file_watches_open`.
- **Activity:** `file.written`, `file.moved`, `file.removed`, `directory.made`, each with `path` and the label.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_FILE_MAX_DOWNLOAD` | `512MiB` | The documented download limit until the fork streams downloads (F7); removed then |
| `LEWBOX_FILE_LIST_MAX_ENTRIES` | `50000` | Cap on a `depth=10` listing; past it, `422` saying to list less deep |
| `LEWBOX_FILE_WATCHES_PER_SANDBOX` | `8` | |

## Testing

- **Fake toolbox files with modes:** upload and download round trips of 0 B, 1 B and 100 MB (streamed, memory-checked); directories made on upload and move; replace; remove a tree.
- **Entries:** for files, directories and symlinks.
- **Conditional writes** (with F51 in the fake): a match writes, a mismatch is `412` with the entry, `If-None-Match: *` both ways; before F51, the headers refused.
- **Downloads:** `If-None-Match` answers `304`; the size limit before F7; `Range` with it.
- **Watch:** event mapping, coalescing, ignores, `overflow`, unsupported nodes.
- **Paths:** `~`, relative refused, encoded characters, forbidden paths.
- **Upload waits for the toolbox** before sending the body, and isn't retried after.

## Build plan

1. Port `files.go`; add path rules and entry `etag`.
2. Download headers, `If-None-Match`, the documented size limit.
3. Fork: F7 streamed downloads (+`Range`) and F51 conditional writes (atomic rename, precondition), both P1; then conditional uploads and `Range` in Lewbox.
4. Beta: fork F9 watching; the watch endpoint; SDK `files.watch()`.

## Open questions

1. **ETag strength.** Is `size-mtime-inode` good enough, or do editors need a content hash? `mtime` has nanosecond precision on ext4 and overlayfs, but gVisor's may differ; check live.
2. **Should a failed remove of a missing path be `204`** (idempotent, like killing an ended session) rather than `404`? The docs say `404` today.
3. **Archive endpoints.** `GET …/archive?path=` (tar.gz a directory, streamed) and `PUT …/archive?path=` (extract) would replace "compress it with a command first" for artifacts and uploads of whole projects. Worth it for the alpha?
4. **Watch over SSE or WebSocket?** SSE is simpler and resumable; a WebSocket would let one connection change what it watches.
