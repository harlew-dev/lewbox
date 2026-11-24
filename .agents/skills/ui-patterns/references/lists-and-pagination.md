# Lists and pagination

The API pages, searches, filters and sorts (see `api-design`). The UI keeps that state in the URL and renders it with one paginated-table component.

## The URL is the state

```
query = projectQuery(searchParams)        // parse; bad values -> defaults
list  = useResource(paths.projects(tenant.id).index(indexQuery(query)))
navigate(projectsUrl(tenant.id, { ...query, page: 1, status }), { preventScrollReset: true })
```

- Changing the URL re-renders the page with the new query and the cache fetches the new key. Nothing else runs (pages have no loaders). Opening the URL directly, Back, Forward, reload and sharing a link all render the same state, for free.
- **NEVER keep list state only in component state.** It is lost on reload, can't be shared, and Back leaves the list.

### One canonical URL per state

The URL builder (`url(path, params)`):

- leaves out empty values and defaults (`status=all`, the default sort);
- leaves out `page=1`;
- writes parameters in a fixed order.

So `/projects`, `/projects?page=1` and `/projects?sort=-updated` can't all exist for one state: each state has one URL and **one cache key**. Otherwise the same data is fetched and cached several times and "is this the current page?" comparisons fail.

The typed API query object does the same for API paths: defaults omitted, unknown keys and values a type error.

### Parse forgivingly, normalise like the API

| Helper | Rule |
| --- | --- |
| `page(query)` | positive integer, anything else → 1 |
| `param(query, key)` | one value; a key repeated (`?q=a&q=b`) counts as absent |
| `sort(query, allowed, fallback)` | in the allowlist, else the fallback |
| `cleanSearch(q)` | exactly the API's validation: trim, strip control characters, cap at the API's max length (e.g. 100) |

Why: a hand-edited or stale URL must never produce a request the API rejects (a 422 rendered as an error page). Mirror the API's rules exactly, not approximately.

**Sort fields** live in the domain types (`sortable = ["name", "status", "updated", "created"]`, `defaultSort = "-updated"`, `-` = descending) and mirror the API's allowlist; a test compares the two lists (see `testing`).

## Changing the state

- **Search, filter and sort changes reset to page 1.** Page 4 of the old result set is meaningless in the new one.
- **Search** waits **300 ms** after typing stops, then navigates with **`replace`**, so typing "acme" doesn't leave five history entries.
- **Filters and sort** push a history entry: Back walks back through them.
- **Sort and page links keep the scroll position** (`preventScrollReset`), so the table doesn't jump to the top.
- A search box is one shared component (`SearchField label value onSearch`) used by every list.
- **After creating from a list, go to page 1 with the search, filters and sort kept.** If the new record doesn't match them, the toast says so: "Project created. It's hidden by your current filter." Why: clearing the user's filters to show the new row throws away their context; silently not showing it reads as "the create failed".
- **After an update, stay on the current page** and revalidate it.

## Sorting

```
toggleSort(current, field, first):
  if current == field:       return "-" + field     // ascending -> descending
  if current == "-" + field: return field           // descending -> ascending
  return first == "descending" ? "-" + field : field
```

- Clicking the current column reverses it; another column starts in its natural direction (text ascending; dates and counts descending).
- A sortable header is a **link** to the next sort's URL (with `page` reset), showing a chevron.
- **`aria-sort="ascending" | "descending"` on the sorted `<th>`, absent on the others** (not `"none"` everywhere). Screen readers announce the column's state.

## The paginated table

```
<PaginatedTable
  page={list.data} loading={list.loading} label="Projects"
  head={<ProjectTableHead sort={query.sort} sortHref={sort => url({...query, page: 1, sort})} />}
  row={project => <ProjectRow project={project} />}
  rowKey={project => project.id}
  href={page => url({...query, page})}
  empty={<ProjectEmptyState filtered={isFiltered(query)} />}
/>
```

| State | Render |
| --- | --- |
| No data yet | a skeleton (rows of grey bars), labelled for screen readers |
| Another page / search loading | the previous rows, **dimmed**, `aria-busy="true"` |
| Past the end (`page > total`) | "There’s nothing on page 7." and a **Go to last page** link; no footer count |
| No records at all | **empty state**: what this list is for, and the Create action (if permitted) |
| Records exist, none match | **no matches**: "No projects match your filters." with **Clear filters** |
| Load error | the page's error notice with Retry **above the table**, the kept rows below; never an error row inside the table |

Why empty ≠ no matches: telling someone with 40 projects and a typo in the search to "Create your first project" is wrong and alarming.

### Footer

- Left: **"Showing 21–40 of 57"** (rows on this page out of every match; en dash). Omitted past the end.
- Right: the page selector, **only when there is more than one page**: Previous, page numbers with gaps (`1 … 4 5 6 … 12`), Next, grouped at the right. Previous/Next disabled at the ends. Page numbers hidden on small screens (Previous/Next remain).
- Every page control is a **real link** (middle-click, open in new tab).
- The page header does **not** repeat the count.
- **Counts elsewhere (dashboard cards) reuse the list endpoint**: `?status=active&perPage=1` and read the total (`pages.items`); recent items are `?perPage=5`. Don't add a summary endpoint until measurement shows the list query can't serve it: each new endpoint needs its own scoping, tests and cache key, and drifts from the list's filter rules.

### Rows

- A row that opens a record is a link across the whole row: each cell gets a link overlay, **only the first cell is in the tab order** (one tab stop per row, not one per cell). The row's `title`/accessible name is the record's name.
- Row actions live in a **⋯ menu in the last column** (right-aligned, minimal width; hidden column header text for screen readers: "Actions"). Never loose buttons in a row.
- The header row is part of the table's horizontal scroll container on phones; the table scrolls, the page doesn't.

## Cursor lists (logs, activity)

Append-only logs page by cursor: no totals or page numbers.

- A plain table with **Older / Newer** below it.
- The URL holds only `before=<cursor>` (shareable). **Older** pushes the current cursor onto a stack in **navigation state**; **Newer** pops it, or opens the newest page when the stack is empty (arrived from a shared link).
- Summaries that only come with the newest page (charts) are kept in page state keyed by the filters without the cursor; on an older page opened directly, say "Charts show with the newest events." with Show newest.

### A tenant's change log

- **Each row reads as a sentence:** "**Ada** renamed the project *Website* to *Marketing site*". Labels live in the web app in **one table keyed by the stable event name**; the API sends names and an allow-listed projection, never prose or the raw payload.
- **A row expands in place** to a description list (field, from → to), never JSON. A ⋯ menu offers **View event**, a page with the sentence, the details, the copyable **Request ID** (so a user can quote it to support) and **Other changes in this request** (same request ID, same tenant, allow-listed names, at most 20), each linking to its own page. The event's own public ID is the page's ID: one request can record several events.
- **Name it for what it holds.** If reads and sign-ins aren't recorded, the tab says "Changes in this workspace", not "All activity": a log that claims completeness it lacks misleads the owner investigating an incident.
- **Seed its history through the real write paths** (the same store/service calls the app uses), so the page isn't empty in development and the seeded rows have exactly the shape production writes; never hard-code UI data.

## Infinite lists

For lists inside a menu, a picker or a combobox (the tenant switcher), load a page at a time as the user scrolls instead of a selector.

```
list = useResourcePages(page => paths.tenants.index({ page }), initialPage1)
list.items        // all loaded pages, de-duplicated by id
list.hasMore      // the last loaded page isn't the last page
list.loadMore()   // ignored while loading, at the end, or after an error
list.loadingMore  // -> one skeleton row
list.error        // loaded items stay; show "Couldn’t load more · Retry" -> list.retry()
```

- **De-duplicate by ID:** offset pages shift when records are added, so page 2 can repeat page 1's last item.
- An `IntersectionObserver` on a sentinel after the last item, **rooted in the scrolling list** (not the viewport), calls `loadMore()`. Mount it inside the popup (portal) so it attaches once the popup exists.
- A focusable **Show more** item does the same for keyboards and browsers without the observer. Nothing extra renders when everything fits on page 1.
- Page 1 revalidates with the list (focus, after mutations) so new records appear. A "refresh all pages" helper exists for after create/rename.
- **Selections live separately from the visible results** (IDs and labels), so searching or loading another page never drops a chosen chip or shows an old choice as a match.
- Server search in a combobox: the combobox's own input, 300 ms debounce, `cleanSearch`.

## Filters dialog and badges

When a list has more filters than fit beside its heading:

- A **Filters** button (outline, funnel icon, the word, and a badge counting applied filters) in the page header, beside the one filter changed most often (a time range, applied as soon as it's chosen).
- It opens a dialog of the fields with **Clear all** (plain: empties the fields, applies nothing) · **Cancel** · **Apply filters** (primary, last). A field's problem shows under it and nothing is applied.
- Applied filters show as **removable badges** under the heading. Removing one applies at once; clicking its text reopens the dialog. After removing, **focus moves to the next badge** (or the Filters button), never lost to `<body>`.
- **Clear all** (a text button) appears once two or more badges are shown.
- Every filter is in the URL; each change is a navigation, so Back undoes it.
