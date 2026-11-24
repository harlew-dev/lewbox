# Data loading

Display data comes through one cached resource hook (React hook, Vue composable, Svelte store, Solid resource). Clicks and submits go through the action hook (`mutations.md`). Build the resource hook on a data-cache library with stale-while-revalidate semantics, or write the same contract yourself.

## The contract

```
r = useResource(path, fallback?, { refreshInterval? })

r.data        // last loaded data; KEPT while a new path loads, KEPT if the new request fails
r.loading     // true only when there is no data for this path yet
r.error       // the failure's message (and request ID), or null
r.missing     // the API answered 404
r.refreshing  // revalidating in the background
r.refresh()   // reload now: Retry, or after a mutation
```

| State | Render |
| --- | --- |
| `loading` and no `data` | a skeleton shaped like the content, with an accessible label ("Loading projects") |
| `data`, `refreshing` | the data, nothing else (no spinner for a background refresh) |
| `data` from the previous key while the new one loads | the old data **dimmed** (`opacity`, `aria-busy="true"`) |
| `error` with `data` | the data (dimmed if it is the previous key's) plus an error notice with **Retry** |
| `error`, no `data` | the error notice with Retry, in place of the content |
| `missing` | "not found" inside the layout |
| `data` empty | an empty state (title, description, action) |

React and Vue look the same:

```tsx
// React
const list = useResource(paths.projects(tenant.id).index(query));
if (!list.data) return list.loading ? <ListSkeleton label="Loading projects" /> : null;
```

```vue
<!-- Vue -->
<script setup>
const list = useResource(() => paths.projects(tenant.value.id).index(query.value));
</script>
<ListSkeleton v-if="!list.data && list.loading" label="Loading projects" />
```

## Keys

- **Key = `[path, userId]`.** The user ID (from the session context) makes one account's cache unreadable to the next account signed in on the same tab. The path includes the query string, which is canonical (`lists-and-pagination.md`), so one state = one key.
- **A `null` path fetches nothing**: use it when an ID failed validation or the user lacks the permission ("anyone else sees a notice and nothing is fetched").
- **`fallback`** shows data you already have (the row from a loaded list) until the record arrives, so opening a row is instant.
- No module-level caches of user data outside the cache library.
- If a read has a side effect on the server (an audited admin read), turn off focus/reconnect revalidation for that hook; every load is then deliberate.

## Keep previous data

Changing the key (next page, new search, another record) keeps the previous `data` while the new key loads, and keeps it if the new request fails. Show it dimmed, with `aria-busy`, and the error notice with Retry if it failed.

Why: blanking the table on every page change makes the layout jump, loses the user's place, and turns a transient network error into an empty screen. The dimming tells sighted users the content is stale; `aria-busy` tells assistive tech.

## No Refresh buttons

Data refreshes itself:

| Trigger | Mechanism |
| --- | --- |
| Window regains focus / network reconnects | the cache library's focus revalidation |
| After a mutation | the action revalidates the affected keys (below) |
| Something on the page is pending | polling, **only while it's pending**: `refreshInterval: data => data.some(isSending) ? 3000 : 0` |
| Anything else | a page reload |

The only reload button is **Retry** on a failed load's notice. Why: a Refresh button admits the UI might be stale and puts the job on the user; constant polling wastes requests and battery and hits rate limits.

## Retries and auth answers

- **Retry automatically (twice, with backoff) only network failures, timeouts and 5xx.** NEVER a 4xx: 403/404 won't change; retrying a 429 spends more of the rate limit and extends the lockout. The API's message ("Too many requests. Try again in 12 minutes.") goes in the error notice; Retry asks again.
- **401** → navigate to `/auth/login?next=<this page>`.
- **403 that means "verify your email first"** (or any similar gate the API signals with a code) → the gate page with `?next=`.
- Each request has a timeout (≈ 12 s); a timeout is a network failure.

## NEVER fetch in a lifecycle effect

```
// WRONG
onMount(() => { api.projects(t).index().then(setProjects) })
```

- **Race:** navigate A → B quickly; A's slow response arrives after B's and overwrites it. The cache library orders by key.
- **Duplicates:** dev double-mount (`dev-double-render.md`), HMR and keep-alive run the effect again; you send two requests and maybe two analytics events.
- **No cache, no dedupe, no focus refresh, no retry policy, no `missing`.** Every page reimplements loading/error state, inconsistently.
- **Leaks:** a response setting state after unmount.

Mutations happen in event handlers, never in effects either.

## Revalidation after changes

| After | Do | Why |
| --- | --- | --- |
| Create / update a record | revalidate **every** cached key the resource's `matches(key)` recognises: its lists, counts, filtered views, and the record | one change can move a record between pages, filters and counts; revalidating only "the list" leaves the dashboard count wrong |
| Delete a record | **drop** its `show` key without refetching, then revalidate every matching key | refetching the deleted record 404s noisily; leaving it cached means Back shows a ghost. Dropped, Back shows "not found" |
| Create / rename a tenant | refresh the tenant list (all pages of the infinite list) and that tenant's record | the switcher and the header show the name |
| Sign out / delete account | **navigate away first, then clear the whole cache**, infinite lists included | clearing first re-renders the signed-in page with no data (flashes, refetches with a dead session); not clearing leaves the account's data in memory for the next user of the tab |
| A session rotation (password or email change) | replace the session in context so later requests send the new CSRF token; revalidate the user's records without clearing what's shown | a refresh failure must not turn a committed change into a failed one |

Give each resource's path helper a `matches(key)` so "every key of this resource" is one call.

## Infinite pages

Lists in menus and pickers load page by page through an infinite variant of the same hook (keys per page, same `[path, userId]` shape). See `lists-and-pagination.md#infinite-lists`.
