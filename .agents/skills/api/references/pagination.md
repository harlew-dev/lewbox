# Lists: pagination, search, sort

Every list a user can grow is paged, filtered and sorted **in the API**. Never ship a whole table to the browser to filter there: it stops working at a few thousand rows, and it sends data the user didn't ask to see.

Two shapes exist. Use numbered pages for everything a user browses (projects, members, tenants). Use cursor pages only for append-only log tables (audit events, deliveries, comments streams) that grow to millions of rows.

## Numbered pages

### Response

```json
{
  "data": [{"id": "prj_j292s2S2j7Qx4B", "name": "Website redesign"}],
  "pages": {"current": 2, "total": 7, "perPage": 20, "items": 131}
}
```

| Key | Rule |
| - | - |
| `data` | Always an array; `[]` when empty, **never** `null` (serialise an empty slice/list, not a nil one) |
| `pages.current` | The page served, echoed after defaults |
| `pages.total` | `max(1, ceil(items / perPage))`: an empty list is "page 1 of 1", so the UI never renders "page 1 of 0" |
| `pages.perPage` | Echoed after defaults |
| `pages.items` | Every row matching the scope **and** the filters |

- **A page past the end** (`?page=9` of 7) is `200` with `data: []` and the real `pages`, so the client can say "this page is empty" and offer page 7. It is not 404: the list exists, and rows may have been deleted between two clicks.
- Every row in `data` has the same shape as `show` returns (or a documented subset), including per-row capabilities where the UI needs them (a tenant list carries `permissions` for each tenant).

### Parameters

| Param | Rule | Default |
| - | - | - |
| `page` | Plain digits, 1-10 000 | 1 |
| `perPage` | Plain digits, 1-100 | 20 |
| `q` | Trimmed; at most 100 characters; valid UTF-8; no control characters; matches the documented columns, case-insensitive | none |
| filters | Enum values (`status=Planned`), public IDs, or exact tokens | none (all) |
| `sort` | A key from the route's allowlist; `-` prefix for descending (`name`, `-updated`) | per route, e.g. `-updated` |

- Each route allow-lists its parameters ([input-validation.md](input-validation.md#query-parameters)): unknown, repeated or malformed is 400. Signs, spaces and exponents (`+2`, ` 2`, `1e3`) are rejected, not coerced.
- A route that doesn't support search or sort doesn't accept `q` or `sort` (400), instead of ignoring them. A list with a fixed order (members: role, then name) documents it.
- The 10 000 page cap bounds `OFFSET` (100 x 10 000 = 1M skipped rows at most), and the request deadline bounds the rest.

### How it is safe

```sql
-- one base query: scope first, then filters
FROM projects p
WHERE p.tenant_id = $1                                -- from the tenant middleware, never from input
  AND ($2::text IS NULL OR p.status = $2)
  AND ($3::text IS NULL OR p.name ILIKE $3 ESCAPE '\' OR p.description ILIKE $3 ESCAPE '\')

-- count:  SELECT count(*) <base>
-- rows:   SELECT p.* <base> ORDER BY <fixed SQL from the sort map> LIMIT $4 OFFSET $5
```

1. **Scope before count.** The tenant `WHERE` is part of the base query that both the count and the rows use. A count computed on a different query than the rows leaks other tenants' totals or shows "131 items" over a page of 3.
2. **The pager adds only `LIMIT`/`OFFSET` (and runs the count).** It never adds a `WHERE`, so scoping can't depend on remembering to pass a flag to the pager.
3. **Escape search wildcards.** Before binding, replace `\` with `\\`, `%` with `\%`, `_` with `\_`, then wrap in `%...%`. Without it, `q=%` matches everything and `q=50%` doesn't mean "50%", and `_` and `%` patterns can force expensive scans. Bind the pattern as a parameter; never concatenate it.
4. **Sort from a fixed map.** The client sends a key; the server looks up SQL it wrote itself. Unknown key: 400. No column name, direction or expression is ever built from input.

   ```
   projectSorts = {
     "name":     "lower(p.name) ASC, p.id ASC",
     "-name":    "lower(p.name) DESC, p.id DESC",
     "updated":  "p.updated_at ASC, p.id ASC",
     "-updated": "p.updated_at DESC, p.id DESC",
     "status":   "array_position($statuses::text[], p.status) ASC, p.id ASC",   -- workflow order, list bound as a parameter
     ...
   }
   ```

5. **Every order ends with the internal ID.** Two rows with the same `updated_at` have no defined order; between page 1 and page 2 the database may swap them, so one row shows twice and another never. The unique ID makes the order total.
6. **Add indexes only after `EXPLAIN ANALYZE` on seeded volume shows they're needed.** An existing `(tenant_id, updated_at DESC)` index usually already serves the default page, the count and the per-status summary; adding `id DESC` only drops a cheap tiebreak sort. Every index costs a write on each insert and on each update of an indexed column, and `updated_at` changes on every edit, so a speculative index doubles write cost for nothing. Know what an index can't do: a b-tree can't serve a leading-wildcard `ILIKE '%term%'` (that needs a trigram GIN index, e.g. Postgres `pg_trgm`), and a low-volume tenant log (tens of rows a day) needs no index on name, project or `q` beyond its `(tenant_id, created_at DESC)` scope. Seed realistic volume, read the plan, and add the index in its own migration only for a query that measurably needs it.

### A new paginated resource (pseudocode)

```
// handler
params = allowQuery(request, "page", "perPage", "q", "status", "sort")   // 400 otherwise
page   = parsePage(params)                    // page, perPage with defaults and bounds
filter = TaskFilter{ q: parseSearch(params.q), status: parseEnum(params.status, TaskStatuses), sort: parseSort(params.sort, taskSorts, "-updated") }
result = data.listTasks(ctx, tenant.internalID, filter, page)
return 200 result                             // {data, pages}

// data layer
base = select(tasks t).where("t.tenant_id = ?", tenantID)
if filter.status: base = base.where("t.status = ?", filter.status)
if filter.q:      base = base.where("t.title ILIKE ? ESCAPE '\\'", containsPattern(filter.q))
return paginate(base.orderBy(taskSorts[filter.sort]), page)   // count + LIMIT/OFFSET only
```

If the UI sorts or filters the list, mirror the sort keys and filter enums in the client's types (see `frontend`).

## Cursor pages (log tables only)

```json
{"data": [{"id": "evt_...", "name": "project.updated", "createdAt": "..."}], "next": "evt_..."}
```

Why not numbers: counting millions of rows for "page 3 of 4,812" is the slowest part of the request, `OFFSET 500000` reads and discards half a million rows, and nobody pages to the middle of an audit log; they filter by time, actor or type.

| Rule | Detail |
| - | - |
| Cursor | `before=<public ID of the last row seen>`. The server looks up that row's `(created_at, id)` **within the same scope** |
| Comparison | A row value: `WHERE (created_at, id) < ($t, $id) ORDER BY created_at DESC, id DESC`. Rows sharing a timestamp never repeat or go missing |
| Next | Fetch `perPage + 1` rows; if the extra row exists, `next` is the ID of the last row returned; otherwise `next: null` |
| No totals | Nothing is counted |
| A cursor from another scope | 400, exactly like a malformed one: it must be one of this list's own rows, so a cursor can't be used to test whether another tenant's event exists |
| A cursor whose row is gone (pruned) | 400 `"That page is no longer available."`; the client restarts from the first page |
| Filters | Same rules as numbered lists; a time window (`window` of `24h`, `7d`, `30d` or `all`, or `from`/`to` in RFC 3339 with a maximum span) keeps queries bounded |
| Index | `(tenant_id, created_at DESC, id DESC)` |

Extras such as a summary for charts belong only on the first page (no `before`), only when explicitly asked for (`summary=include`; 400 with `before`), and bounded by their own deadline.

## Never

- `null` instead of `[]`, anywhere in a list response.
- Client-side paging of a server list, or a "fetch all" endpoint for a growing table.
- `ORDER BY` built from a request value, or a sort that doesn't end in a unique column.
- An unescaped `LIKE` pattern.
- A count query that isn't the row query's base.
- `OFFSET` pagination on an append-only log of unbounded size.
