# Writing a convention check

Read when adding or changing a check that enforces a naming or structure rule. Checks are ordinary tests (or lint rules / scripts run by `check`), so the normal `test` and `check` commands fail as soon as a name is wrong.

## The five parts

### 1. A pure function

Takes names (or facts about files) and returns violations. No I/O, no globals, so it's trivially table-testable.

Each violation message must:
- name the offending thing exactly (`handler Projects.list`, `route PUT /api/things/{thing}`, `pages/settings.<ext>`);
- state the rule in words;
- link the doc section it comes from (`docs/naming.md#api-routes-and-handlers`);
- say how to fix it, ideally with the corrected name.

```
handler Projects.list: handler actions are index, show, store, update, destroy or a listed named verb — docs/naming.md#api-routes-and-handlers. Try Projects.index.
```

### 2. A table test

Good names that must pass, bad names that must be refused (with the expected message fragment). This proves the function can fail, and documents the rule by example.

### 3. A run on the real tree

Feed the function the actual code: by reflection, by parsing the source (Go `go/parser`, TypeScript compiler API, PHP-Parser, Python `ast`), by importing modules, or by walking the folder tree. Assert:
- no violations;
- **at least N names were found** (a floor near the current count). Otherwise moving a folder or changing a glob makes the check find nothing and pass forever.

Read the source of truth that catches the most: walk the folder tree rather than the route list, so a file that isn't registered yet is still checked.

### 4. An exception list, in the test

```
// namedActions: handler actions beyond index/show/store/update/destroy.
var namedActions = map[string]string{
	"Invitations.accept": "consumes the invitation and creates a membership; not a store of either alone",
	"Sessions.login":     "auth flows act on the session, not a resource (resource-naming: the auth exception)",
	"Deliveries.retry":   "re-queues a failed delivery; there is no retry resource to store",
}
```

- Every entry has its reason. An entry without one is refused in review.
- Where an entry names real code, the check fails for an entry nothing uses any more (stale exceptions are how rules quietly loosen).
- Pre-check violations are listed as `"existing; to fix"` and fixed when that code is next touched.
- Adding an entry needs the user's agreement (SKILL.md §4).

### 5. A row in `docs/checks.md`

| Check | Rule | Lives in | Exceptions |
| --- | --- | --- | --- |
| `TestRouteActions` | Every route's handler method is a CRUD action or a listed named verb; no PUT ([naming.md](…#api-routes-and-handlers)) | `router/naming_test.<ext>` | `namedActions`, each with its reason |

…and a link back from the rule's section in the naming doc ("checked by `TestRouteActions`, see checks.md").

## See it fail first

Before fixing the tree (or before trusting a check you just wrote), run it and watch it fail:
- on the current tree, if you're introducing the rule to existing code (the failures are your to-do list);
- or on a deliberately bad case you add temporarily (rename one file, add one PUT route).

If it doesn't fail, the check is wrong (wrong glob, wrong parser, rule never evaluated). A green check you never saw red proves nothing.

## Same check in two stacks

Go (handler actions, read from the real route registry):

```go
var crudActions = map[string]bool{"index": true, "show": true, "store": true, "update": true, "destroy": true}

// routeActionViolations: routes whose handler method breaks the action rule, with a fix.
func routeActionViolations(routes []Route) []string {
	var out []string
	for _, r := range routes {
		if r.Method == "PUT" {
			out = append(out, fmt.Sprintf("route PUT %s: update is always PATCH — docs/naming.md#api-routes-and-handlers", r.Path))
		}
		if !crudActions[r.Action] && namedActions[r.Handler+"."+r.Action] == "" {
			out = append(out, fmt.Sprintf("handler %s.%s: handler actions are index, show, store, update, destroy or a listed named verb — docs/naming.md#api-routes-and-handlers", r.Handler, r.Action))
		}
	}
	return out
}

func TestRouteActionsRule(t *testing.T) {
	for _, tc := range []struct{ r Route; ok bool }{
		{Route{"GET", "/projects", "Projects", "index"}, true},
		{Route{"POST", "/invitations/{invitation}/accept", "Invitations", "accept"}, true},
		{Route{"GET", "/projects", "Projects", "list"}, false},
		{Route{"PUT", "/projects/{project}", "Projects", "update"}, false},
	} {
		got := routeActionViolations([]Route{tc.r})
		if (len(got) == 0) != tc.ok { t.Errorf("%v: ok=%v, got %v", tc.r, tc.ok, got) }
	}
}

func TestRouteActions(t *testing.T) {
	routes := router.Registered() // the real registry
	if len(routes) < 30 { t.Fatalf("found only %d routes: is the check reading the real registry?", len(routes)) }
	for _, v := range routeActionViolations(routes) { t.Error(v) }
}
```

TypeScript (page files, read from the real folder tree):

```ts
const PAGE_FILE = /^(index|create|edit|layout|\[[a-z][a-zA-Z]*\])$/

// pageFileViolations: page files whose name isn't a page action, with a fix.
export function pageFileViolations(paths: string[]): string[] {
  return paths
    .filter((p) => !PAGE_FILE.test(stem(p)))
    .map((p) => `${p}: page files are index, create, [param], edit or layout; anything else is a folder — docs/naming.md#pages. Try ${dirname(p)}/${stem(p)}/index${extname(p)}.`)
}

test.each([
  ['pages/project/index.vue', true],
  ['pages/project/[project].vue', true],
  ['pages/project/[project]/edit.vue', true],
  ['pages/settings.vue', false],
  ['pages/project/[id].vue', false],
  ['pages/project/new.vue', false],
])('%s ok=%s', (p, ok) => expect(pageFileViolations([p]).length === 0).toBe(ok))

test('the real pages tree follows the page-file rule', () => {
  const files = walk('pages').filter(isPageFile) // tests and helpers excluded
  expect(files.length).toBeGreaterThan(10)
  expect(pageFileViolations(files)).toEqual([])
})
```

`[id]` is refused by a second rule (the param must be named after the resource folder: `project/[project]`), left out here for brevity.

## Where checks live, and what they refuse

- **Prefer ordinary tests** (and one script for docs) over custom lint plugins or static-analysis passes. A test needs no new tool or dependency, runs in the commands everyone already runs, and is readable by anyone who can read the code. Reach for a lint rule only where lint is the natural tool (import restrictions).
- **Unreadable is a failure.** When a check parses source (e.g. the route registry), a route whose method or path isn't a string literal fails as "unreadable" rather than being skipped. Otherwise building paths dynamically becomes the way around every rule.
- Encode the obvious invariants while you're there: e.g. page routes (those serving HTML) may only be GET.
- **Defer git hooks and agent `Stop` hooks** until checks are actually being skipped. `test` and `check` are already required before "done"; a hook is a second place to maintain.

## The docs link check

A script run by `check`:
- every relative link in tracked (or staged) markdown points at a file that exists. **Only** tracked or staged files are scanned: another session's untracked drafts would otherwise fail `check` for everyone, forever;
- every `#anchor` matches a heading in the target (using the host's slug rules: lower case, punctuation other than `-`/`_` dropped, spaces as `-`, and `-1`, `-2` suffixes for repeated headings);
- links inside fenced code blocks and inline code are skipped, and absolute URLs are not fetched: the script makes **no network calls and uses no eval**, so it is fast, deterministic and safe to run anywhere;
- a tracked doc linking an untracked file is a **warning**, not a failure (stage it, or remove the link) — a new doc is legitimately untracked while you write it;
- its own exception list (`allowedDeadLinks`) keyed `file → target`, each with a reason; a new entry needs the user's agreement, like any other exception;
- its own `--test` mode with good and bad fixtures.

## Catalogue of checks worth having

Naming (rules owned by `resource-naming`): controller layout and action names; every route is a controller action, no PUT; permission names `<resource>.<action>`; event names and resources; rate-limit names `<feature>:<counted-by>`; API client method names; page route constants; page/layout export names; page folders; kebab-case files.
Structure (rules owned by `project-structure`): area imports; entrypoint imports; SQL only in the data layer; HTTP only in the API client; library wrappers; admin isolation; test-only imports.
Data (owned by `testing`/`production-hardening`): every table has a factory; every table is in the data map.
Docs: the link check.
