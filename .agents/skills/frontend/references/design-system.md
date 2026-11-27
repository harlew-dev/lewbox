# Design system

A restrained, neutral look: one neutral scale, one accent, light and dark modes following the system setting, one or two fonts, one icon set. Whatever the stack uses for styling (utility classes, CSS modules, scoped styles, CSS-in-JS) and for behaviour (a headless kit, a copied-in component registry, hand-built parts), the rules below are the same. Pick one of each per project, record it in the project docs, and follow it everywhere.

## Styling rules

- **One styling approach for the whole app.** Never mix two (no utility classes in one area and a CSS-in-JS library in another). Why: one vocabulary, no specificity fights, no dead CSS, and a reviewer knows where a look comes from.
- **Design tokens, not raw values.** Colours, spacing, radii, font sizes, shadows and motion timings come from one shared set of tokens (CSS variables or the styling system's theme), named by role (`background`, `surface`, `foreground`, `muted`, `border`, `accent`, `danger`, `warning`, `success`). **NEVER a raw hex value, a one-off pixel size or an arbitrary font size in a component.** Dark mode is the same role names with different values, never a second set of colours chosen per component.
- **Global CSS stays small:** tokens, the font, element defaults (box sizing, the focus ring) and nothing else.
- **No dynamic style names the build can't see.** If the build scans source for class names, write them out in full and pick one from a map (`variants[variant]`), never build one by interpolation (`` `bg-${color}` ``).
- **No inline `style` for what the styling system can do.** The exception is a value only known at runtime (a measured width, a progress percentage), passed as a CSS variable. Popups size and place themselves with the CSS variables or props their positioning code provides (anchor width, available height, transform origin), never hard-coded sizes.
- **Callers control placement only.** A component accepts a class or style prop for margin, width and grid placement, merged with its own. A caller never overrides the component's look (`<Button class="red">`): add a variant instead.
- **Type, colour, layout and surfaces live beside this file:** [typography.md](typography.md), [color.md](color.md), [layout.md](layout.md) and [surfaces.md](surfaces.md). This file owns components, state, overlays, motion and toasts.

## Styling state

Interactive state (hover, pressed, keyboard focus, disabled, invalid, checked, selected, highlighted, open, entering/leaving) is styled from **what the element already exposes**: native pseudo-classes (`:hover`, `:focus-visible`, `:disabled`, `:checked`), ARIA attributes (`[aria-expanded]`, `[aria-selected]`, `[aria-invalid]`, `[aria-current]`) or the data attributes the component kit sets. Never mirror that state into script to compute class names.

Why: the browser or the kit already knows the state; styling from it keeps that knowledge in one place, works in dev tools, and lets tests assert state by the same attribute. Where a plain link or button needs to match a kit part's states, use the same attributes or pseudo-classes the kit's parts use, from one shared helper.

- **Focus rings on `:focus-visible` only**, never on pointer focus, never `outline: none` without a replacement. One focus style, defined once.

## Components

- **One component foundation for behaviour** (focus trapping, keyboard, overlays, positioning, ARIA). Whatever it is, it is the only one: **NEVER a second component kit beside it.** A new interactive control starts from the foundation's part that already has its behaviour, never from a bare `<div>` with handlers.
- **Pages use the project's components**, never a third-party UI package directly (enforce with a lint import rule). If components are copied into the repo as source, those files are the project's components; if a package is installed, a thin wrapper in the components folder is. Either way, that is where styling and our conventions live, and the one place to change when the foundation changes.
- **Copied-in component source** is owned code: reviewed, tested and kept consistent with these rules. Compose and wrap it; edit its internals only deliberately, and note that the next update from upstream may overwrite the edit.
- **Reuse before adding:** extend an existing component rather than writing new markup. Where page-only vs shared components live is `structure`.
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

Motion is functional first: it shows where a surface came from, that a state changed and that an action landed. Before choosing a curve, decide whether to animate at all — removing an animation is a valid fix.

- **Animate only where motion carries information** (a popup's origin, a state that changed, content that is new). **Expressive motion** — stagger, blur, longer travel — is for rare moments: a view's first load, a success step, an empty state. Never for routine work.
- **How often it is seen decides whether it moves.** Keystrokes, row hovers and the content behind a tab change instantly, or with a transition of ≤ 150 ms on `opacity` or `background-color`. A menu opened many times a day (a context menu, a command menu) does not animate open; it may animate as it closes. A settings-page menu may animate both ways.
- **Every animated change leaves a static cue** (a colour, icon or label). Motion is never the only feedback.
- **Transitions for interactive state, keyframes for one-off sequences.** A transition retargets when the user reverses mid-flight (closing a drawer that is still opening); a keyframe runs a fixed timeline and snaps or restarts. Hover, toggle, open and close are transitions.
- **Duration grows with size:**

  | Element | Enter |
  | --- | --- |
  | Tooltip, badge, small element | ~150 ms |
  | Menu, dropdown, popover | ~200–250 ms |
  | Dialog, drawer, panel | ~300–400 ms |
  | Page enter (fade and lift) | 200 ms |

  An exit is shorter than its enter — around 150 ms — and never travels the container's full height. Timings come from the motion tokens, not a number per component.
- **Ease-out for everything that enters, exits or changes state; linear for continuous loops** (spinners, progress, skeletons). A physical settle needs no spring in CSS: use a strong ease-out such as `cubic-bezier(0.2, 0, 0, 1)` at the same duration. **NEVER bounce or overshoot.**
- **Scale shrinks as the element grows:** a tooltip or menu enters from 0.96–0.97, a dialog or panel from 0.98–0.99 (a large surface scaling from 0.95 changes too much of the screen). A panel exits to 0.99, a smaller change than its enter.
- **Scale from the trigger.** An anchored popup points `transform-origin` at the side its trigger sits on, using the variable the positioning code publishes, so the origin follows when the popup flips. A badge or indicator scales from its corner; a dialog centred on the screen keeps the centre.
- **Travel only far enough to show direction:**

  | Movement | Distance |
  | --- | --- |
  | A text swap | ~4 px |
  | A page transition | 6–8 px, over 200 ms |
  | Dropdowns, tooltips, most elements | 8–32 px |
  | Drawers and side panels | Up to ~70% of their own width or height |

  A dropdown moves away from its trigger; a panel enters from the side it was opened from; tab content slides ~12 px from the side of the chosen tab; a hover/focus background or a current-page indicator slides between the items of a group.
- **Press scale 0.96 over ~150 ms** with ease-out, and **NEVER on a disabled button** (`:not(:disabled):active` — `:enabled` never matches a link). Where a pressed scale would distract, the component offers an opt-out (`static` prop or a variant).
- **Stagger only an infrequent entrance**, ~100 ms apart: split it into semantic chunks (title, description, actions), each entering with opacity, ~4 px of blur and ~12 px of travel over ~300 ms. Never stagger a menu, a tab or a keystroke.
- **No animation on the first render of a state swap.** The default state (a toggle already on, a tab already selected) paints in place rather than animating in on mount; only an intentional entrance animates.
- **Cross-fade an icon that swaps** on an infrequent state change (play → pause, copy → copied): scale from ~0.25, opacity 0 → 1, ~4 px of blur, over ~300 ms, both icons mounted in one grid cell so the size doesn't change. Hover-revealed row icons and tab icons change instantly.
- **Animate height by measuring an inner wrapper.** The container stays `height: auto` on both sides; measure an inner wrapper with a `ResizeObserver` and animate the outer element to that number. The measuring ref goes on the inner wrapper, never the element whose height animates. Give the first state a fixed height (the measurement is 0 until content mounts), and skip the animation where content changes every keystroke (a results list).
- **Name the properties.** `transform`, `opacity`, `filter` and `clip-path` skip layout. **NEVER `transition: all`** — it animates colours on a theme flip and re-runs layout on `width` and `padding`. A measured height is the one layout property worth animating.
- **`will-change` only after an observed jank**, naming the property (`transform`, `opacity`, `filter`), never `all` and never preemptively. Each layer costs memory, and a transform or filter layer becomes the containing block for its fixed descendants.
- **Suppress transitions during a theme switch.** Disable every transition, force a style flush, swap the theme, then restore after the next frame; otherwise every colour transition fires at once and the switch smears.
- **Reduced motion:** movement, scale and blur run only under `prefers-reduced-motion: no-preference`; under the preference they become a plain opacity fade, never an instant removal. A brief press scale may stay. A global stylesheet reset covers CSS transitions but not script-driven animation; gate both.
- **Replay a new animation slowed to 10 %** (devtools animation panel, or `document.getAnimations().forEach(a => (a.playbackRate = 0.1))`) before calling it done, then at full speed. Look for content appearing before its container has room, an empty gap before it shrinks, a 1–2 px jump on the first or last frame, a popup growing from its centre.
- **Leave no transform behind:** end at `transform: none`, because a leftover `transform` creates a containing block that breaks fixed and sticky children.
- **Nothing animates on arrival or when only the tenant changes**; key page transitions by pathname, not by search params.
- **Judge timing on a production build**, not the dev server (`dev-double-render.md`).
- **A sliding element measured in page coordinates inside a `position: fixed` pane** needs that pane marked as its own scroll root, or it slides in from a scroll's distance away.

## Toasts

- Stack at the bottom right (across the bottom on phones), newest in front, up to 5, fanning out on hover/focus.
- **Success toasts hide after 5 s**, paused while hovered or focused. **Error toasts and toasts carrying an action** (Undo, Retry) **stay until dismissed**: they hold the only record of a failure or the only way back. Every toast has a dismiss button. No swipe-to-dismiss.
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

## Before you finish

| Pattern | Fix |
| --- | --- |
| `transition: all` or `transition-all` | Name the properties that change |
| `animation:` on `:hover`, `.open` or another toggled class | A transition on the same properties |
| `active:scale-95`, `scale(0.9)` or any press below 0.96 | 0.96 over ~150 ms |
| A pressed scale with no `:disabled` exclusion | `:not(:disabled):active` |
| A tooltip or row hover transitioned over 150 ms | ≤ 150 ms on `opacity` or `background-color`, or none |
| A menu opened many times a day that animates open | Animate the close only |
| An exit as long and as far as its enter | ~150 ms, a small fixed distance |
| A modal or panel entering from below `scale(0.98)` | 0.98–0.99 |
| A popup anchored to a trigger with no `transform-origin` or a centre origin | The positioning code's origin variable |
| A spring or `bounce` with overshoot | ease-out, `cubic-bezier(0.2, 0, 0, 1)` |
| A spinner, progress bar or skeleton with `ease-out` | `linear` |
| `will-change: all`, or `will-change` on an element that never animates | Name the animated property, or delete it |
| `height: auto` animated on a container whose children swap | Measure an inner wrapper |
| A measuring ref on the element whose height animates | An inner wrapper |
| `opacity: 0` in a base rule that only an animation reveals | Hidden state in the `from` keyframe with `fill-mode: both` |
| Movement, scale or blur with no `prefers-reduced-motion` guard | Gate it; a plain opacity fade under the preference |
| A toggle or tab that animates in on mount | Paint the default state in place |
| A success toast with `duration: Infinity` | 5 s, paused on hover/focus |
| An error toast or one carrying an action that hides on a timer | Keep it until dismissed |
| `z-[9999]` or any value outside the z-index scale | The nearest layer from the scale |
| A raw hex, one-off pixel size or arbitrary font size | The role token |
| A leftover `transform` after the animation ends | `transform: none` |
| A page transition keyed by search params | Key by pathname |
