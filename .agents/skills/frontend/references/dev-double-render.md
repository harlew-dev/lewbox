# Dev double-render discipline

Components mount, unmount and mount again far more often than you think: React's StrictMode does it on purpose in development, every framework's hot module reload does it on save, keep-alive caches (`<KeepAlive>`, `<Activity>`) deactivate and reactivate, route transitions overlap two copies of a page, and a list re-keys. Code that assumes "this runs once" breaks in all of them. Write every component as if it will be rendered twice and mounted, unmounted and remounted.

## What strict/dev modes do (React's StrictMode as the reference)

| Check | What happens (development only) | The bug it surfaces |
| --- | --- | --- |
| Double render | each component function, state initialiser, memo and reducer runs twice; one result is discarded | side effects during render: mutating a module variable, counting calls, mutating props, fetching |
| Mount → unmount → mount | on first appearance every effect runs setup, cleanup, setup; state and refs are kept | effects without matching cleanup: listeners added twice, timers left running, subscriptions leaked |
| Deprecation warnings | outdated APIs logged | code that breaks in the next major |

Vue, Svelte and Solid don't double-invoke on purpose, but HMR, `<KeepAlive>` (`onActivated`/`onDeactivated`) and conditional rendering exercise the same paths. The rules are identical.

**Keep strict mode on.** It costs production nothing and finds these bugs on the first run instead of in a rare production sequence.

## Rules

1. **Rendering is pure.** A render (component function, template expression, computed) reads props/state and returns markup. No requests, no writes to module variables, no `Date.now()`-based IDs stored outside state, no mutation of props or shared objects.

2. **Every effect cleans up what it set up.**

   ```
   // React
   useEffect(() => {
     window.addEventListener("resize", onResize)
     return () => window.removeEventListener("resize", onResize)
   }, [])

   // Vue
   onMounted(() => window.addEventListener("resize", onResize))
   onUnmounted(() => window.removeEventListener("resize", onResize))

   // Svelte
   onMount(() => { window.addEventListener("resize", onResize); return () => window.removeEventListener("resize", onResize) })
   ```

   Listeners, timers (`setTimeout`, `setInterval`, the 200 ms delayed-loading timer, the 3 s "Copied" timer), `IntersectionObserver`/`ResizeObserver`, subscriptions, WebSockets, focus traps, `requestAnimationFrame`, third-party widget instances (a CAPTCHA): each has a teardown.

3. **Never assume an effect runs once.** No "initialised" flags in module scope; no one-time fetch in an effect (data comes through the resource hook, `data-loading.md`); no analytics "page viewed" fired from a mount effect without dedupe. A setup that runs twice must leave the same result as once (idempotent), and the cleanup must undo it exactly.

4. **Guard async results after cleanup.** A promise that resolves after unmount must not set state, call `onCopied`, or navigate: check a `cancelled`/`mounted` flag set in cleanup, or abort the request (`AbortController`).

5. **Refs survive the remount** (React keeps state and refs across the dev mount/unmount/mount), so in-flight guards such as the action hook's "running" ref keep working. Don't reset such a guard in cleanup unless the action is really cancelled.

6. **Measure timing and animation on the production build.** Dev adds a render and a remount on first appearance, which changes what you see in the first frames. "It flickers only in dev" may be strict mode; judge it on the production build before "fixing" it.

7. **Libraries must survive it too.** Overlay, chart and animation libraries that portal or create instances on mount must handle the remount (portal once, animate from attributes). If a library misbehaves only in dev, check it on a production build before working around it, and record the workaround.

8. **Nothing user-specific in module scope.** Module state survives remounts, HMR keeps it across edits, and in production it survives sign-out. Session, tenant and cached data live in context and the cache library.

## Where strict mode applies

Typically: the dev server only. Production builds drop it; unit tests and end-to-end runs usually don't wrap in it unless configured. So something that happens only on the dev server may be strict mode, and the production build is where to judge it. Whether tests should enable it is `testing`'s call.

## Before you finish

| Pattern | Fix |
| --- | --- |
| An effect adding a listener, observer or subscription with no teardown | Return the matching cleanup |
| `setTimeout`, `setInterval`, `requestAnimationFrame` or a delayed-loading/"Copied" timer not cleared | Clear it in cleanup |
| A module-level `initialised`/`hasRun` guard | Remove it; make the effect idempotent |
| A fetch in a mount effect | The resource hook (`data-loading.md`) |
| An analytics "page viewed" fired on mount without dedupe | Dedupe by key, or fire once per route change |
| `Date.now()` or `Math.random()` producing an ID during render | State, or an ID from the API |
| A promise that sets state, calls `onCopied` or navigates after unmount | A `cancelled`/`mounted` flag or `AbortController` |
| Props or shared objects mutated during render | Compute from props and state |
| An in-flight guard reset in cleanup | Keep it; only a really cancelled action resets |
| A portal or library instance created on mount with no teardown | Create once and dispose in cleanup |
| Session, tenant or fetched data in a module-level variable | Context and the cache library |
| A timing or animation fix judged on the dev server | Re-check on the production build |
| A ref counting calls across renders to detect "twice" | Remove the counter; rendering is pure |
