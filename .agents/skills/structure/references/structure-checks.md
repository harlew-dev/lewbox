# Structure checks

Read when setting up a new project, adding an area/layer, or when one of these checks fails. Every structural rule in this skill should be a test or lint rule, written in the shape `conventions` describes (pure function, table of good/bad cases, run on the real tree, exception list with reasons, row in `docs/checks.md`). Fix the code, not the check.

## Checks to have

| Check | Rule it enforces | How to implement |
| --- | --- | --- |
| HTTP layout | Controllers only in `endpoints/<area>`; one controller per file, named after the resource; an area exports only controllers; areas never import each other; only the router imports areas; the core imports no area | Parse the source tree (Go `go/parser`, TS compiler API / `ts-morph`, PHP-Parser, Python `ast`) and read each file's imports and exported symbols |
| Route registry | Every route is registered in the registry as a controller action (no inline/bare handler), with an allowed method (no PUT) | Introspect the router after building it, or parse the registry file |
| Entrypoint imports | The production server entrypoint's transitive imports exclude `factory`, `seed`, test helpers | Walk the import graph (`go list -deps`, `madge`, `deptrac`, `import-linter`) |
| Data-layer only SQL | No SQL/driver calls outside the data layer, migrations and the DB package's listed statements | Lint rule banning the driver's raw-query functions outside allowed paths, plus a grep test for SQL keywords in string literals |
| No stray HTTP | `fetch`/HTTP library and `/api/` strings only in `lib/api/` | `no-restricted-globals` / `no-restricted-imports` / `no-restricted-syntax` for string literals starting `/api/` |
| Library wrappers | Each third-party library imported only from its wrapper module(s) | `no-restricted-imports` with an allow-list per library; a test that the allow-list names files that still exist |
| Admin isolation | Admin modules imported only from admin paths; app bundle contains no admin module or admin string | Lint rule + bundler plugin + post-build grep script |
| Page folders | Every page file is `index`, `create`, `[param]`, `edit` or `layout` (per `naming`); anything else is a folder with `index`; a folder with pages has its own `index` or `layout` | Walk `pages/` (not the route list, so unregistered files are caught too) |
| Kebab-case files | Files in `components/`, `lib/`, `types/`, `test/` are kebab-case (allowing build markers like `.server`) | Walk the folders, regex `^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z]+)*$` |
| Test-only imports | Test helper modules are imported only by test files | Lint rule on the helper path |
| Build-time modules | Build-only modules never reach the client bundle | Grep the built client assets for their symbols |

## Shape of a layout test (pseudocode)

```
// pure: takes facts about files, returns violations with a fix
function layoutViolations(files: {path, package, imports, exports}[]): string[] {
  for f in files under endpoints/<area>/:
    for imp in f.imports:
      if imp is endpoints/<other area>:
        yield `${f.path}: areas never import each other (${imp}); move the shared code to the core or a leaf package — docs/naming.md#controllers`
    if f exports more than one controller:
      yield `${f.path}: one controller per file — split ${names}`
  for f in core files:
    if f imports any endpoints/*: yield `${f.path}: the core imports no area`
}

test "layout rules refuse bad trees"     // table: good trees → [], bad trees → expected messages
test "the real tree follows the layout"  // parse the repo, assert no violations,
                                         // and assert it found ≥ N controllers so a moved folder can't pass by finding nothing
```

Keep a `supportPackages` (or similar) exception list in the test for folders under `endpoints/` that aren't areas, each with its reason, and fail if an entry no longer matches anything.

## Reading a failure

| Message | Usual fix |
| --- | --- |
| `endpoints/admin/users: imports endpoints/workspace` | Move the shared helper into the core or a leaf package |
| `route GET /api/things: register a controller's action` | Add `Things.index` to the area's controller and register that |
| `cmd/server imports internal/factory` | Remove the import; factories are for seed/tests only |
| `components/ui/ProjectTable.<ext>: files are kebab-case` | `git mv` to `project-table.<ext>` |
| `pages/app/settings.<ext>: move to settings/index.<ext>` | `git mv` the page into a folder |
| `app bundle mentions /api/admin` | Find the import chain (`--why`/bundle analyser) and cut it |
| `<charting library> imported from pages/...` | Import the wrapper (`components/ui/chart`) instead |
