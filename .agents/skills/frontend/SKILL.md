---
name: frontend
description: How the web frontend is built in any framework (React, Vue, Svelte, Solid…) - routing and layouts, loading data through a cached resource hook, mutations through one action hook, URL-driven paginated lists, form and button placement, confirmations, the design system (typography, colour, layout, surfaces, motion), UX copy, secrets in the UI, copy-to-clipboard, accessibility (including charts), mobile checks, seeing every state before calling UI done, and dev double-render discipline. Use when adding or changing a page, route, list, table, form, dialog, button, toast, component, chart, animation, style or any UI text, when building from a design, or when reviewing frontend code.
---

# UI patterns

The rules for a single-page app talking to a JSON API. Framework names (React, Vue, Svelte) and helper names (`useResource`, `useAction`) are illustrations: build the same contract with whatever the stack offers. Page file and route naming is owned by `naming`; where code lives by `structure`; how to test UI by `testing`; CSRF, sessions and headers by `security`.

## When to use

- Adding a page, route, layout, tab or redirect.
- Showing data from the API, or changing data through it.
- Building a list, table, search, sort, filter or pagination.
- Placing buttons, writing a form, a dialog or a confirmation.
- Adding or styling a component, writing UI text, a toast or an error message.
- Setting type, colours, spacing, radii, shadows, icons or animation; building from a design.
- Showing a secret, a sensitive value or a Copy button.
- Reviewing frontend code before merge.

## Reference files

| Read | When |
| --- | --- |
| [references/routing.md](references/routing.md) | Adding a page, layout, tab, loader, redirect, `?next=` or reading an ID from the URL |
| [references/data-loading.md](references/data-loading.md) | Anything that displays API data, polls, or must refresh after a change |
| [references/mutations.md](references/mutations.md) | Any create/update/delete/action button, where its error and success go |
| [references/lists-and-pagination.md](references/lists-and-pagination.md) | Tables, search, sort, filters, page selectors, cursor and infinite lists |
| [references/forms-and-actions.md](references/forms-and-actions.md) | Placing or styling any button, building a form, dialog or confirmation |
| [references/design-system.md](references/design-system.md) | Writing or styling components, overlays, motion, toasts, preferences, mobile/desktop checks |
| [references/typography.md](references/typography.md) | Type scale, font loading, line-height, wrapping, truncation, numbers, inputs on iOS |
| [references/color.md](references/color.md) | Colour ramps, semantic tokens, dark mode, contrast measurement |
| [references/layout.md](references/layout.md) | Grouping, spacing, alignment, responsive structure, overflow, RTL, room for translation |
| [references/surfaces.md](references/surfaces.md) | Radii, shadows and borders, image outlines, hover on touch, overlay scroll, icons |
| [references/copy.md](references/copy.md) | Writing any visible text: labels, titles, toasts, errors, empty states |
| [references/sensitive-values-and-copying.md](references/sensitive-values-and-copying.md) | Showing secrets, one-time keys, personal data, or a Copy button |
| [references/accessibility.md](references/accessibility.md) | Every UI change; charts and data visualisations in particular |
| [references/verifying-states.md](references/verifying-states.md) | Before calling UI done: state pages, stress scenarios, building from a design, reviewing a diff |
| [references/dev-double-render.md](references/dev-double-render.md) | Writing effects, subscriptions, timers; "it only happens in dev" bugs |

## 1. Routing and pages

- **MUST register every page in one explicit route registry** (URL → page file), not file-system routing. One file to review, one place to derive the route patterns used by error reporting and `?next=` allowlists. File and folder names follow `naming`.
- **MUST build URLs with typed helpers** (`routes.project.show(tenantId, projectId)`), never by string concatenation in components. API paths live only in the API client module; nothing else calls `fetch` or writes `/api/...`.
- **Pages render on the first frame and fetch their own data**, showing skeletons until it arrives. Moving between pages never waits on the network. **NEVER add a data loader to a page** just to fetch its content.
- **Route loaders (pre-render guards) do only two things:** load the session once for the signed-in area, and redirect (signed out → login with `?next=`, no tenant → onboarding, signed in on a guest page → app). API down → a "service unavailable" page with the error's reference.
- **Layouts persist.** Signed-in pages render inside one layout route that stays mounted between pages (sidebar, menus, indicators keep their state). A page renders only its content.
- **Tabs:** tabs that are separate pages are child routes of a layout that renders the heading and tab bar; tabs on a record page use `?tab=` (first tab = no param).
- **MUST validate an ID from the URL by the API's own rule** (`parsePublicId("prj", value)`: prefix + exact length/alphabet) before fetching. Invalid → render "not found", send no request. A 404 from the API also renders "not found" inside the layout.
- **Transient data travels in navigation state, not the URL:** a typed email handed from login to "forgot password", a "Project deleted." message for the next page, a cursor stack. URLs end up in history, logs, referrers and screenshots.
- **Navigation is a link** (`<a href>` rendered by the router), never a button with `onClick={navigate}`: middle-click, open-in-new-tab and the status-bar preview must work.
- **A route change sets `document.title` and moves focus to the new page's `<h1>`**: client-side navigation resets neither, so screen-reader users hear nothing.
- **NEVER keep the session or current tenant in module-level variables.** Read them from context; they change on sign-out and tenant switch.

## 2. Data loading

- **ALWAYS read display data through one cached resource hook/composable** keyed by `[path, userId]` (the user ID stops one account seeing another's cache). Its contract:

  | Field | Meaning | Render |
  | --- | --- | --- |
  | `data` | last loaded data, **kept while a new path loads and if the new request fails** | the content |
  | `loading` | nothing loaded for this path yet | skeleton |
  | `error` | the load failed (API message) | error notice with **Retry**, beside the kept data |
  | `missing` | the API answered 404 | "not found" |
  | `refreshing` | revalidating in the background | nothing (or dim if the key changed) |
  | `refresh()` | reload | Retry, after a mutation |

- **On a key change, keep the previous data visible, dimmed, with `aria-busy`**, never blank the page.
- **NEVER add a Refresh button.** Data refreshes on window focus, after a mutation, and by polling **only while something is pending** (an interval function returning 0 once nothing is "Sending…"). The only reload button is Retry on a failed load.
- **Retry automatically only server errors and network failures** (twice). **NEVER retry a 4xx**: a 403/404 won't change and a 429 spends more of the rate limit.
- **NEVER fetch in a lifecycle effect** (`useEffect`, `onMounted`, `onMount`). It races (a slow old response overwrites a newer one), duplicates under dev double-mount, has no cache, dedupe or focus refresh.
- **After a mutation revalidate every cached key of that resource** (lists, counts, single records): one change can move a record between pages and filters. **After a delete** drop the record's `show` key without refetching (Back shows "not found", not stale data), then revalidate the lists. **After sign-out or account deletion** navigate away first, then clear the whole cache, infinite lists included.

## 3. Mutations

- **ALWAYS run mutations through one action hook** returning a result union, never hand-written `try/catch/finally`:

  ```
  save = useAction(input => api.projects(tenantId).store(input))
  result = await save.run(form)   // { ok: true, value } | { ok: false }; never throws
  save.busy     // true at once, until settled         -> logic (refuse to close the dialog)
  save.loading  // true once busy lasts 200 ms, then shown >= 400 ms -> spinner, disabled fields
  save.error    // last failure, with the request ID    -> inline error notice
  ```

- **`run` ignores calls while one is in flight** (double click = one request) and **awaits everything the action awaits, including the navigation that ends it**, so the button keeps spinning until the next page has loaded.
- **Where errors go:** form → error notice below its action row; error naming a field (`error.field`) → under that field; button with no fields → error toast; failed load → notice with Retry beside kept data; inside a dialog → stays in the dialog with the typed values; 401 → login with `?next=`.
- **Every success is a toast** ("Project created."), not a `done` state or inline banner. A frequent, reversible action (archive, move, delete into a trash) runs at once and its toast offers **Undo**; irreversible, shared or bulk actions confirm first. If the action navigates out of the layout that hosts the toaster, pass the message in navigation state and show it on arrival.
- **A 5xx shows its request ID** ("Reference: req_…", selectable) under the error, so a user can quote it and support can find the log lines.

## 4. Lists

- **The URL is the list's state** (page, search, filters, sort). **One canonical URL per state:** omit defaults and page 1, so each state has one URL and one cache key. Parse forgivingly: invalid values fall back to defaults; normalise search exactly as the API validates.
- **Search, filter and sort changes reset to page 1.** Search waits 300 ms after typing, then navigates with `replace` so history doesn't fill with keystrokes.
- **Sort:** clicking the current column reverses it; another column starts in its natural direction (names ascending, dates descending). The sorted `<th>` carries `aria-sort`. Sort fields mirror the API's allowlist.
- **Table states:** skeleton before the first page; previous page dimmed while the next loads; past the end: "There’s nothing on page 7" + "Go to last page"; **empty** (no records: explain and offer Create) differs from **no matches** (offer clearing the filters); load errors in a notice **above, never inside** the table.
- **Footer:** "Showing 21–40 of 57" left, page selector right, only with more than one page. Page links are real links. Rows link to their record; row actions live in a ⋯ menu in the last column.
- Menus and pickers use infinite lists; many filters go in a Filters dialog shown as removable badges. Details in the reference.

## 5. Forms and actions

- **Actions sit at the bottom right of what they act on; the primary is rightmost** (on top when stacked on phones). Sign-in pages are the only full-width exception.
- **At most one solid button per group.** Secondary that cancels/goes back: plain; alternative that does something: outline.
- **Red only for destructive actions**, and only in a danger zone, a confirmation's confirm button, or a danger item in a ⋯ menu. Sign out is not destructive.
- **Labels are verb + object:** "Create project", "Delete workspace". NEVER "OK", "Submit", bare "Delete".
- **A settings Save is disabled until something differs and would be accepted**, and says why in one of two wordings: "No changes made" or "Fill in required fields". **Every other form keeps submit enabled and validates on submit**: errors under the fields, `aria-invalid`, focus on the first invalid field.
- **Submit is `type="submit"` inside a `<form>`** so Enter works; every other button in a form is `type="button"`.
- **ONE confirmation component for every "are you sure?"**: names the target in its title, states the consequence, confirm label says what it does, runs through the action hook, keeps the dialog open with the error on failure. Heavy destruction adds type-to-confirm (and the password). **Delete is a confirmation dialog, NEVER a page.**
- **Reset dialog state on every opening** (key the dialog by an open counter), never key it by its target.
- **A value the user may see but not change is read-only, not disabled** (still focusable, selectable, copyable), with a lock and the reason.

## 6. Design system

- **One styling approach for the whole app**, whatever it is (utility classes, CSS modules, scoped styles…). Never two side by side.
- **Design tokens named by role** (`background`, `surface`, `foreground`, `accent`, `danger`…) for colour, spacing, radii, type and motion. NEVER raw hex, one-off sizes or arbitrary font sizes in a component. No dynamic style names the build can't see.
- **A caller's class or style prop is for placement and spacing only**; never to override a component's own look.
- **State is styled from what the element exposes** (`:hover`, `:focus-visible`, `:disabled`, ARIA attributes, the kit's data attributes), never from class names computed in script. Focus rings on `:focus-visible` only.
- **Alternatives are a `variant` prop**, never `tone`, never one component per look. A wrapper that only binds data (`ErrorNotice` = an action's error → `Notice variant="error"`, re-keyed per failure) is allowed.
- **One component foundation for behaviour** (focus, keyboard, overlays, ARIA); never a second component kit beside it. Pages use the project's own components (wrappers, or copied-in source the project owns), never a third-party UI package directly.
- **NEVER colour alone:** every status/level colour comes with a word or icon.
- **A fixed z-index scale:** dialogs 50, popovers/menus 60, tooltips 70, toasts 80.
- **Type:** a small role-named scale pairing size, unitless line-height and weight; `rem` sizes; no weights below 400 under 18 px; `tabular-nums` on changing numbers; inputs ≥ 16 px on iOS. Details in [references/typography.md](references/typography.md).
- **Colour:** one neutral ramp, one accent, only the status ramps in use; components use semantic tokens only, never primitives; dark mode is its own tuned ramp; contrast is measured on the rendered pair. Details in [references/color.md](references/color.md).
- **Layout:** group with space before lines (between-group gap ≥ 2× within); DOM order is visual order; `minmax(0, 1fr)`, `100dvh`, container queries, logical properties; text boxes grow, never fixed heights. Details in [references/layout.md](references/layout.md).
- **Surfaces:** concentric radii (outer = inner + padding); shadows for elevation, borders for structure; hover styles only under `@media (hover: hover)`; `overscroll-behavior: contain` on scrolling overlays; icons in `currentColor`. Details in [references/surfaces.md](references/surfaces.md).
- **Motion is rare, short and CSS-first:** high-frequency interactions don't animate; duration grows with size (≈150 ms tooltips, ≈250 ms menus, ≈300–400 ms dialogs), exits are shorter, popups scale from their trigger, never `transition: all`; under `prefers-reduced-motion` movement becomes a plain fade. Details in [references/design-system.md#motion](references/design-system.md#motion).
- **Persisted UI preferences** (a hidden sidebar) are read from storage during render so the first paint is right; never flipped by an effect after mount (it flashes).

## 7. Copy

- Sentence case; verb + object on actions; toasts are a past-tense fact with a full stop ("Acme deleted."); a confirmation title is a question naming the target ("Delete Acme?"); descriptions only when they add something the title doesn't.
- **NEVER render text taken from the URL** (`?notice=` selects a fixed message by key; unknown keys show nothing): it is a phishing/content-injection vector.
- Links name their destination (never "Click here" or a bare "Learn more"); toggles describe the ON state; strings are built whole with plural rules, never concatenated.
- **Show the API's error message**; don't invent one. Never wording that reveals whether an account or address exists.
- Typography: ’ “ ” – … . Details in [references/copy.md](references/copy.md).

## 8. Secrets and sensitive values

- **Secrets** (grant access: recovery codes, TOTP QR/setup key, a new API key) stay **out of the DOM** until revealed: render a decoy placeholder, never the secret blurred (blur can be undone). Masked fields hold bullets, not the value; Copy works while masked.
- **One-time secrets live in the dialog's component state only**: never in the cache, a global store, storage or the URL; discarded on Done, navigation, tenant switch, sign-out. Closing early asks for confirmation. Never auto-retry the request that issued it.
- **Sensitive values** (your email on a shared screen) are in the DOM, drawn blurred, with click-to-reveal and an eye toggle.
- NEVER tokens in URLs or `localStorage`; NEVER report a URL (use the route pattern).

## 9. The copy component

- **Every Copy button is one component + one hook** (`useCopy` / `CopyButton` / `CopyFailure`); never a hand-written clipboard call. "Copied" is a 3-second tooltip and an icon swap, announced once via a `role="status"` region; a clipboard failure shows an instruction under the button that stays until the next try.

## 10. Accessibility

- Keyboard reachable, visible focus, accessible names, errors announced (`role="alert"`), statuses announced (`role="status"`), reduced motion, focus returned to the opener when an overlay closes.
- Hidden UI is out of the tab order and the accessibility tree, not just transparent.
- Native elements before ARIA; a skip link before repeated navigation; live regions rendered empty before their text changes; never block paste; never `maximum-scale=1`; nothing the user needs disappears on a timer (error and Undo toasts stay until dismissed).
- **Charts:** SVG `aria-hidden`; the figure carries a one-sentence summary and a visually hidden table of every value; a visible "Show as table" toggle; series colours in a fixed order, never status colours; tooltips and click-to-zoom are shortcuts, never the only way.

## 11. Mobile and desktop

- **Check every UI change at phone width (~375 px) and desktop width** (full list in [references/design-system.md](references/design-system.md#mobile-and-desktop-checks)) before calling it done: actions bottom right (stacked full width on phones, primary on top), header actions wrap below the title, tables scroll horizontally inside their container, page numbers hide on small screens, touch targets ≥ 44 px, nothing widens the page.

## 12. See every state before calling it done

- **Look at the states, don't assume them.** For a non-trivial component, render each state its code can reach (loading, empty, error, one, many, stale, each role) on a throwaway dev-only page fed with product-shaped fixtures, plus the stress cases its inputs allow (long and unbreakable text, emoji, RTL, 320 px, 200 % zoom, both themes). Building from a design maps every value to an existing token and reports what was rounded. Reviewing a diff reads the removed lines for lost labels, focus styles and motion guards. Details in [references/verifying-states.md](references/verifying-states.md).
- **Never claim a visual check you didn't do.** No browser at hand: say so and hand over the URL.

## 13. Dev double-render discipline

- **Rendering is pure; every effect cleans up what it set up** (listeners, timers, observers, subscriptions) and **never assumes it runs once**. Dev modes mount → unmount → mount (React StrictMode), HMR and keep-alive remount components. Keep strict mode on; judge timing and animation on the production build.

## Pre-merge checklist

- [ ] New page is in the route registry, named per `naming`, with typed URL helpers; return-path allowlist and route-pattern lists updated.
- [ ] Page renders instantly with a skeleton; no page data loader; no fetch in an effect.
- [ ] URL IDs validated by the API's rule; invalid or 404 → "not found".
- [ ] Every async UI handles pending, success, failure (with Retry or inline error) and empty.
- [ ] Previous data stays visible (dimmed) on key change and on refresh failure.
- [ ] No Refresh button; polling stops when nothing is pending; 4xx not retried.
- [ ] Mutations go through the action hook; double submit impossible; caches revalidated (delete drops `show`; sign-out clears all).
- [ ] Success toast; error in the right place; 5xx shows its reference.
- [ ] List state in the URL, canonical, page 1 on change, debounced search with `replace`, `aria-sort`.
- [ ] Primary rightmost, one solid per group, red only for destruction, verb + object labels, disabled Save says why; other forms validate on submit.
- [ ] Confirmation via the one component; dialog state reset per opening; delete is not a page.
- [ ] Styling follows the project's one approach, tokens only (no raw values), no interpolated class names, no colour-only meaning, z-index from the scale.
- [ ] Type, colour, layout and surface rules hold; every reference touched has its **Before you finish** detections checked against the diff.
- [ ] Motion only where it earns it, sized durations, named properties, a reduced-motion fade.
- [ ] Copy follows [references/copy.md](references/copy.md); nothing from the URL rendered.
- [ ] Secrets out of the DOM until revealed; one-time secrets in component state only.
- [ ] Keyboard, focus, names, announcements, reduced motion; charts have summary + table.
- [ ] Checked at phone and desktop width; every reachable state and the relevant stress cases seen in a browser (or stated as not verified).
- [ ] Effects clean up; works under dev double-mount.
- [ ] Tests per `testing`.
