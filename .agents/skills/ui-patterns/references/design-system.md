# Design system

A restrained, neutral look: one neutral scale, one accent, light and dark modes following the system setting, one or two fonts, one icon set. Whatever the stack uses for styling (utility classes, CSS modules, scoped styles, CSS-in-JS) and for behaviour (a headless kit, a copied-in component registry, hand-built parts), the rules below are the same. Pick one of each per project, record it in the project docs, and follow it everywhere.

## Styling rules

- **One styling approach for the whole app.** Never mix two (no utility classes in one area and a CSS-in-JS library in another). Why: one vocabulary, no specificity fights, no dead CSS, and a reviewer knows where a look comes from.
- **Design tokens, not raw values.** Colours, spacing, radii, font sizes, shadows and motion timings come from one shared set of tokens (CSS variables or the styling system's theme), named by role (`background`, `surface`, `foreground`, `muted`, `border`, `accent`, `danger`, `warning`, `success`). **NEVER a raw hex value, a one-off pixel size or an arbitrary font size in a component.** Dark mode is the same role names with different values, never a second set of colours chosen per component.
- **Global CSS stays small:** tokens, the font, element defaults (box sizing, the focus ring) and nothing else.
- **No dynamic style names the build can't see.** If the build scans source for class names, write them out in full and pick one from a map (`variants[variant]`), never build one by interpolation (`` `bg-${color}` ``).
- **No inline `style` for what the styling system can do.** The exception is a value only known at runtime (a measured width, a progress percentage), passed as a CSS variable. Popups size and place themselves with the CSS variables or props their positioning code provides (anchor width, available height, transform origin), never hard-coded sizes.
- **Callers control placement only.** A component accepts a class or style prop for margin, width and grid placement, merged with its own. A caller never overrides the component's look (`<Button class="red">`): add a variant instead.

## Styling state

Interactive state (hover, pressed, keyboard focus, disabled, invalid, checked, selected, highlighted, open, entering/leaving) is styled from **what the element already exposes**: native pseudo-classes (`:hover`, `:focus-visible`, `:disabled`, `:checked`), ARIA attributes (`[aria-expanded]`, `[aria-selected]`, `[aria-invalid]`, `[aria-current]`) or the data attributes the component kit sets. Never mirror that state into script to compute class names.

Why: the browser or the kit already knows the state; styling from it keeps that knowledge in one place, works in dev tools, and lets tests assert state by the same attribute. Where a plain link or button needs to match a kit part's states, use the same attributes or pseudo-classes the kit's parts use, from one shared helper.

- **Focus rings on `:focus-visible` only**, never on pointer focus, never `outline: none` without a replacement. One focus style, defined once.

## Components

- **One component foundation for behaviour** (focus trapping, keyboard, overlays, positioning, ARIA). Whatever it is, it is the only one: **NEVER a second component kit beside it.** A new interactive control starts from the foundation's part that already has its behaviour, never from a bare `<div>` with handlers.
- **Pages use the project's components**, never a third-party UI package directly (enforce with a lint import rule). If components are copied into the repo as source, those files are the project's components; if a package is installed, a thin wrapper in the components folder is. Either way, that is where styling and our conventions live, and the one place to change when the foundation changes.
- **Copied-in component source** is owned code: reviewed, tested and kept consistent with these rules. Compose and wrap it; edit its internals only deliberately, and note that the next update from upstream may overwrite the edit.
- **Reuse before adding:** extend an existing component rather than writing new markup. Where page-only vs shared components live is `project-structure`.
- **Alternatives are a `variant` prop** (`<Notice variant="error|success|info|warning">`). Never `tone`, never a separate component per look (`ErrorBox`, `SuccessNotice`, `WarningBanner`) for the same thing. (A prop that is the *value* being shown, such as a monitoring `level`, keeps its own name.)
- **A binding wrapper is not a variant.** `ErrorNotice` takes an action's `error` and renders `<Notice variant="error">` from it: nothing when there's no message, the request ID as its reference, and re-keyed on each failure so a repeated identical error is announced again. Why: the same three-line conditional (check message, set key, pick variant) was repeated on every form, and each copy forgot the re-key. It adds no look of its own; it only binds data to the one `Notice`.
- Component files are kebab-case and export a family (`dialog.<ext>`: `Dialog`, `DialogTitle`, `DialogDescription`, `DialogBody`, `DialogActions`). Keep props stable once used.
- **Icons** come from one set at a few fixed sizes; the component around an icon sizes and colours it, the call site doesn't.

Expected primitives: Button (`variant`, `href`, `loading`, `disabled`, `disabledReason`), Link, Badge (`color`, `onRemove`), Notice (`variant`, `onRetry`, `onDismiss`), ErrorNotice (binds an action's `error` to `Notice variant="error"`), Reference (request ID), EmptyState, ListSkeleton, Toast, Field/Label/Description/ErrorMessage, Input (`blurred`, read-only lock), InputGroup with at most two trailing actions, Checkbox, RadioGroup (2 to 4 visible options; longer → Listbox), Listbox, Combobox, Dialog, ConfirmationDialog, Dropdown (⋯ menu), Tooltip, Table, PaginatedTable, Pagination, SearchField, SettingsSection (`variant="danger"`), Heading, Text, CopyButton, Reveal, Disclosure (instead of raw `<details>`), Stat, charts.

## Never colour alone

Every colour that carries meaning comes with a word or an icon:

- Status badges are words ("Active", "Expired", "Not verified"), colour is a second cue.
- Levels are icon + word + colour ("Warning" in amber with a warning icon).
- Invalid fields have an error message, not just a red border.
- Chart series have a legend, gaps between segments and a table (`accessibility.md`).

Why: ~8% of men can't separate red from green, and high-contrast/forced-colours modes strip colour.

Colour conventions: green = active/in use; red = destructive or critical; amber = warning; blue = informational and the selected option; a distinct colour (purple) for things apart from the app (an "Admin" badge). Red, amber and green as **levels** never double as chart series colours.

## Z-index scale

Overlays portal into `<body>`, so stacking is by z-index alone. Use exactly:

| Layer | z-index |
| --- | --- |
| page content, sticky headers | < 50 |
| dialogs and their backdrops | 50 |
| menus, listboxes, combobox popups | 60 |
| tooltips | 70 |
| toasts | 80 |

So a listbox opened inside a dialog sits above it, a tooltip above both, a toast above everything. **NEVER `z-[9999]`.** A dialog over a dialog uses the kit's nested dialog support: each layer renders its own backdrop (the dim compounds), the parent recedes slightly, and Escape closes only the top one.

## Overlays

- Overlays animate in and out with short fades, using the entering/leaving state the kit exposes (the overlay stays mounted until its exit finishes). Prefer CSS transitions to script-driven animation.
- **Open overlays lock page scroll** without shifting layout (hide the scrollbar and compensate its width).
- **Focus returns to the opener** when an overlay closes (see `forms-and-actions.md#dialogs`).
- While a modal overlay is open, the rest of the page is hidden from assistive tech (`inert`/`aria-hidden`), which the kit should do; check it.
- A listbox opens over its button with the selected option aligned to it (off for touch), at least as wide as the button.

## Motion

- **Subtle and functional:** a new page fades in and lifts 6 px over 200 ms; tab content slides 12 px from the side of the chosen tab; a hover/focus background slides between items in a group (sidebar, tabs, menu); a current-page indicator slides between items.
- **CSS first** (transitions over script animation), leaving no transform behind on the element (a leftover `transform` creates a containing block that breaks fixed/sticky children).
- **Respect `prefers-reduced-motion`:** plain transitions turn off; script animation follows the user's setting (things still move to their place, without sliding). Use motion timings from the tokens, not per-component durations.
- Nothing animates on arrival or when only the tenant changes; key page transitions by pathname, not by search params.
- Judge timing on a production build, not the dev server (`dev-double-render.md`).
- A sliding element measured in page coordinates inside a `position: fixed` pane needs that pane marked as its own scroll root, or it slides in from a scroll's distance away.

## Toasts

- Stack at the bottom right (across the bottom on phones), newest in front, up to 5, fanning out on hover/focus.
- Hide after 5 s, **paused while hovered or focused**; `duration: Infinity` keeps one until dismissed. Each has a dismiss button. No swipe-to-dismiss.
- Same icon and role as the matching notice variant: success → `role="status"`, error → `role="alert"`. (If the kit makes each toast a `dialog`, override the container to `presentation`; a toast is not a dialog.)
- The toaster is mounted by the signed-in layout only; signing out unmounts it and its toasts. Calling `toast()` with no toaster mounted shows nothing and **never throws**, so a shared action used on a toaster-less page can't crash it (those pages show inline notices instead, `mutations.md`). What goes in a toast: `mutations.md#every-success-is-a-toast`.

## Persisted UI preferences

A per-device preference (sidebar hidden, a table's density) is stored in `localStorage` under a namespaced key (`app:sidebar=hidden`), **only when it differs from the default**.

- **Read it during render** (the initial state's initialiser), so the first paint is already right. **NEVER read it in an effect after mount** and flip the UI: that paints the default, then flashes and plays the transition.
- This is only correct where the page renders in the browser. For server-rendered or prerendered pages, use a cookie the server reads, or the server's HTML and the client's first render disagree (hydration mismatch).
- Wrap storage access in `try/catch`: blocked storage and private windows throw. Without storage, use the default and keep the control working.
- Never store user data, tokens or anything sensitive there.

## Where you are

- A page in the sidebar is its current item (`aria-current="page"`, a sliding indicator).
- A page reached through a menu marks the menu's button as selected (grey background), and inside the menu that item is `aria-current="page"` and focused when the menu opens.
- The current tenant keeps a check mark in the switcher.

## Mobile and desktop checks

Look at every UI change at **phone width (~375 px)** and **desktop width (≥ 1280 px)**, light and dark, before calling it done:

- [ ] Actions bottom right; dialog actions stacked full width on phones with the primary on top.
- [ ] Page-header actions wrap below the title on phones; danger zone buttons drop below their text.
- [ ] Tables scroll horizontally **inside** their container; the page never scrolls sideways. Visually hidden tables sit in a visually hidden box (a table is never narrower than its text and would widen the page).
- [ ] Page numbers hidden on phones; Previous/Next remain.
- [ ] Touch targets ≥ 44 × 44 px on touch screens (an invisible touch-target span extends small buttons).
- [ ] The mobile navigation is a slide-over that closes on navigation; the tenant switcher is reachable in one tap.
- [ ] Nothing depends on hover (tooltips are shortcuts; touch has no hover).
- [ ] Toasts span the bottom on phones and don't cover the primary button for long.
- [ ] Long names truncate with an ellipsis and a full title, never overflow.
