# Accessibility

Accessibility is part of "done" for every UI change, not a later pass. The component foundation should supply most ARIA and keyboard behaviour (check it does); these rules cover what it can't know. How to test it is `testing`.

## The baseline

| Requirement | How |
| --- | --- |
| **Keyboard** | Everything works with Tab, Shift+Tab, Enter, Space, Escape and arrows. No `div` with `onClick`: use `<button>` or `<a href>`. Lists of options use roving focus (arrows), not one tab stop per item. |
| **Visible focus** | A focus ring on `:focus-visible` on every interactive element; never `outline: none` without a replacement. |
| **Accessible names** | Every control has one: a visible `<label>`, or `aria-label` for icon-only buttons ("Copy workspace ID", "Hide sidebar"). The name **contains the visible label text** ("Send" stays "Send", never "Submit message"); an icon-only button names its action, not its glyph ("Close", not "X"). A visually hidden label (`sr-only`) beside a section title still counts. Hidden table column headers name icon/menu columns ("Actions"). A select/listbox trigger's name is its **label** ("Status"), never the selected value; the value is announced as the control's value. Why: a name that changes with each choice ("Planned soon") leaves the user not knowing what the control is for. |
| **Descriptions** | Field help and errors wired with `aria-describedby`; `aria-invalid` on invalid fields. A disabled button's reason is its description. A locked field's reason is its description. |
| **Errors announced** | Error notices and error toasts `role="alert"`; an error that repeats is announced again (re-key it). |
| **Status announced** | Success notices, toasts, "Copied", "Checking…" `role="status"` (polite). |
| **Busy** | `aria-busy="true"` on a region showing stale data while new data loads; buttons in `loading` state are busy and ignore clicks. |
| **Current** | `aria-current="page"` on the current nav item and the current item in a menu. |
| **Sort** | `aria-sort` on the sorted `<th>` only. |
| **Toggles** | `aria-pressed` on toggle buttons (reveal/hide); `aria-expanded`/`aria-controls` on disclosure triggers; `aria-checked` (`true`, `false` or `mixed`) for tree pickers. |
| **Landmarks** | One `<main>` per page; `<header>`, `<nav>`, `<aside>` and `<footer>` map to landmarks. Repeated landmarks of one type get a label (`<nav aria-label="Primary">`, `<nav aria-label="Breadcrumbs">`). |
| **Reduced motion** | `prefers-reduced-motion` turns transitions and slides off (`design-system.md#motion`). |
| **Forced colours** | Decorative backgrounds (sliding highlights) hidden under `forced-colors`; state shown by borders/text that survive. |
| **Colour** | Never the only cue (`design-system.md#never-colour-alone`); text contrast ≥ 4.5:1, UI marks ≥ 3:1. |

## Route changes

A client-side navigation resets neither `document.title` nor focus, so a screen-reader user hears nothing and stays where the old page left them. On every route change:

- **Set `document.title` to the new page's context**, most specific first: "Billing · Settings · Acme".
- **Move focus to the new page's `<h1>`**, given `tabindex="-1"`, or to `<main tabindex="-1">` where the content has no heading yet, so the new context is read.
- **Scroll to the top** on a new navigation and **restore the position** on Back/Forward (`history.scrollRestoration = "auto"`; reset the top yourself).
- A route that only changes search params (a page number, a tab) is not a new page: update the title when it changes the context, but don't steal focus from the list or filter being used.

The short rule is in [routing.md#persistent-layouts](routing.md#persistent-layouts); this is the detail.

## Skip link and headings

- **A "Skip to content" link is the first focusable element on any page where navigation precedes `<main>`.** Visually hidden until focused, it targets `<main id="main" tabindex="-1">`.
- **One `<h1>` per page, and heading levels don't skip.** A heading is structure: pick the level it belongs at and style it with CSS, never choose the tag by size.

## Focus management

- **Overlays return focus to their opener** when they close. A dialog opened from a menu item remembers the element focused when it opened (the menu button); the menu closes first, then the item's action runs.
- **Dialogs** trap focus, close on Escape (unless busy), and are labelled by their title and described by their description.
- **Prefer a native `<dialog>` opened with `showModal()`**: the focus trap, Escape, initial focus and an inert background come free. A custom overlay needs `role="dialog"`, `aria-modal="true"` and `aria-labelledby`, plus **`inert` on the rest of the page**; render it in a portal outside the inert container, because `inert` on an ancestor disables the dialog too. The component foundation should do this; check it and don't fight it.
- **Revealing content moves focus to it** and announces it. Removing a filter badge moves focus to the next badge or the Filters button. Never let focus fall to `<body>` after something disappears.
- **Hidden UI is out of reach:** a collapsed sidebar or a closed panel is `hidden`/`inert`/`visibility: hidden` once it has finished sliding out, not just `opacity: 0` or off-screen; it reappears at once when shown. Otherwise keyboard users tab into invisible controls.
- **A focused element is never hidden behind sticky chrome** (a header, a footer bar, a cookie banner): give the scroller `scroll-padding-block-start`/`-end` for the chrome's height, or the anchor target `scroll-margin-block-start`, one or the other and never both (`layout.md#content-bleeds-controls-float`).
- **`tabindex` is never positive.** `0` is only for a custom control with no native focusable element; `-1` is for a heading that receives focus, a dialog container, or a roving item. Where a positive value was patching the order, fix the DOM order instead.
- **Composite widgets take one Tab stop.** Tabs, menus, toolbars and radio groups use **roving tabindex**: the active item is `0` and the rest `-1`, and arrows move both focus and the `0`. A combobox keeps DOM focus in its input and points **`aria-activedescendant`** at the highlighted option's `id`.

## Native elements first

- **Use the native element before ARIA:** `<a href>` for navigation, `<button>` for actions, `<label>` for labels, `<dialog>` for dialogs, `<details>` and `<select>` where they fit. A `<div>` with `onClick` has no role, no focus and no keyboard, so it is never a control.
- **A "button" that goes somewhere is a link; a "link" that acts is a button.**
- **Remove ARIA rather than add it.** The native element already exposes its role (`<button role="button">` is noise), naming the generic role does nothing (`aria-label` on a plain `div` or `span`), and `role="menu"` on site navigation promises arrow-key behaviour it doesn't have. If a fix needs several ARIA attributes to patch a native element, the element is wrong: start over.
- **`aria-hidden="true"` is never on a focusable element or an ancestor of one.** It takes the subtree out of assistive tech but leaves its Tab stops, which is how a screen reader reaches silent controls. Hide the decorative icon or the visual duplicate, never the control; an interactive subtree is hidden from everyone (`hidden`, `inert`) or not at all.
- **`disabled` removes a control from the tab order and from hover**, so a reason only a tooltip carries never reaches a keyboard or touch user. Keep native `disabled` for a genuinely unavailable control whose reason is visible beside it (or on a wrapper with `aria-describedby`), as the settings Save does. Where the control must stay focusable to explain itself, use **`aria-disabled="true"`**, style it explicitly, ignore activation in the handler, and give the reason as its description. NEVER both attributes at once.

## Hit areas and gestures

- **WCAG 2.5.8's floor is 24 × 24 px; ours is 44 × 44 px on touch.** Keep the visible mark small where the design needs it and extend the hit area with a pseudo-element on the `<label>` or `<button>` (never the `<input>`, whose pseudo-elements don't render reliably). Anything that looks clickable is clickable across its whole visible extent.
- **Extended hit areas never overlap another target** or another extended area: shrink to the largest size that stays clear, or give the control its real 44 px box.
- **Decorative layers get `pointer-events: none`.** A scrim, glow, sheen, gradient or full-bleed `::after` drawn over a control swallows every click on it, and no target sizing fixes that. A layer that is an element also gets `aria-hidden="true"`. A modal scrim that dismisses on click is a control, not decoration, and keeps its pointer events.
- **Every drag needs a single-pointer alternative:** move-up and move-down buttons on a sortable row, a menu on a kanban card. Pinch, swipe and other path gestures need one too; `touch-action: none` is scoped to the surface that implements them, never set on the page.

## Forms

- Every input has a label; placeholders are examples, never labels. Known requirements are helper text under the label **before** the field is filled, not only in the error.
- **Every field about the user carries a valid `autocomplete` token, plus a descriptive `name`, inside a real `<form>`**, or password managers and autofill can't fill it:

  | Field | `autocomplete` |
  | --- | --- |
  | Name | `name` (or `given-name` / `family-name`) |
  | Email · phone | `email` · `tel` |
  | Sign in | `username`, `current-password` |
  | Sign up / reset | `username` (or `email`), `new-password` |
  | One-time code | `one-time-code` |
  | Address | `street-address`, `postal-code`, `country` |
  | Card | `cc-number`, `cc-exp`, `cc-csc` |

- **`type` and `inputmode` pick the keyboard:** `type="email"`, `tel` or `url`; `type="text" inputmode="numeric"` for codes, PINs and card numbers (text semantics, no spinner); `inputmode="decimal"` for money; `type="number"` only for a true quantity. `spellcheck="false"` on emails, codes and usernames.
- **NEVER block paste**, and never filter characters as the user types: paste is how password managers, codes and long values arrive. Accept free text and validate after, trimming before you validate.
- **Only a settings form's Save is gated:** disabled until something changed and would be accepted, with its reason ("No changes made" / "Fill in required fields"; `forms-and-actions.md#a-disabled-save-says-why`). **Every other form (create, sign-in, wizard step) keeps submit enabled and validates on submit**: mark each invalid field `aria-invalid`, show its error underneath, and focus the first invalid field.
- Required fields are marked native `required`, and their names go in the settings Save's "Fill in required fields" reason.
- Errors appear under their field, linked by `aria-describedby`, and the field gets `aria-invalid`. Put `novalidate` on a `<form>` that renders its own errors, or native validation blocks the submit handler.
- A re-render or hydration never resets a typed value or moves focus out of its field.
- One-time codes: one input (not six), `autocomplete="one-time-code"`, `inputmode="numeric"`, so paste, autofill and the label work; draw boxes visually over it if you like.
- Checkboxes: the box and its text in one `<label>`; links inside the label open without toggling it.
- A read-only locked field stays focusable; a disabled one is skipped (`forms-and-actions.md#read-only-vs-disabled-fields`).

## Live regions

Pick the first that fits: focus moves there anyway (an opened dialog, the first invalid field — the move is the announcement), a message tied to a field (`aria-describedby`), a non-urgent update (`role="status"`, polite), an urgent form-level error (`role="alert"`).

- **Render the empty region first and change its text later.** A polite region inserted together with its content is announced inconsistently; one mounted empty and emptied again before each message is announced every time.
- **`role="alert"` only for urgent errors** not tied to a field: a form-level failure, an expired session. **NEVER `aria-live="assertive"` on a success message**; it interrupts whatever the user was reading. Default to polite.
- Keep messages short and self-contained, and never move focus to a toast.

## Timers and moving content

- **Nothing the user needs disappears on its own.** Error toasts and toasts carrying an action (Undo, Retry) stay until dismissed; success toasts hide after 5 s, paused on hover and focus, and every toast has a dismiss control (`design-system.md#toasts`). A clipboard-failure instruction stays until the next copy (`sensitive-values-and-copying.md#the-copy-component`).
- **Content that moves, blinks or updates by itself for more than 5 s needs a pause**, and autoplaying or looping media counts: give it pause/stop, including a muted looping clip. Auto-dismissal suits brief, low-stakes confirmations and nothing else.

## Zoom and reflow

- **NEVER `maximum-scale=1` or `user-scalable=no`** in the viewport meta: pinch-zoom is an accessibility feature, not a bug.
- **At 200 % text and 320 px wide (a 1280 px viewport at 400 % zoom) the page still works:** no clipped or overlapping text, no horizontal page scroll, and no two-dimensional scrolling outside a genuinely 2D container (a table, map or code block scrolls inside its own). Structure for this is `layout.md`.
- Size text and breakpoints in `rem` where the codebase allows, so a larger browser font size narrows the layout when the text needs it.

## Images and icons

- **`alt` is chosen by purpose, not appearance.** Every `<img>` has one; a missing `alt` is read as the file name.

  | Purpose | `alt` |
  | --- | --- |
  | Decorative, or repeated by adjacent text | `alt=""` (present but empty) |
  | Informative | The meaning it adds ("Ticket QR code") |
  | Functional (the image is the link or button) | The action or destination ("Search", not "Magnifying glass") |
  | Image of text | The exact text, and better: real text instead |
  | Complex (a chart, a diagram) | A short summary, with the full data in a table or text nearby |

- **Decorative SVG is `aria-hidden="true"`; meaningful inline SVG is `role="img"` with `aria-label`** (or a `<title>` referenced by `aria-labelledby`). Inside a labelled button or link, the icon is always `aria-hidden`.

## Tables and lists

- Real `<table>`, `<th scope>`, captions or `aria-label` naming the table.
- A row link puts one tab stop on the first cell; the other cells are pointer overlays.
- Skeletons carry a label ("Loading projects") and `aria-busy`; the dimmed previous page too.

## Tooltips

- Open on hover **and** keyboard focus, close on Escape. Never the only way to get information or do something: a tooltip that acts on click is a pointer shortcut; the same action must exist elsewhere.
- Touch screens have no hover: don't hide essential information in tooltips.

## Charts

A chart is a picture of numbers; screen readers, keyboard users and people who'd rather read numbers need the numbers. (When designing a chart's colours and marks, also follow the `dataviz` skill if available.)

- **The SVG is `aria-hidden`.** The `<figure>` carries a **one-sentence summary** (its accessible name/description: "Sign-ins per hour over the last 7 days, peaking at 340 on Tuesday.") and a **visually hidden `<table>` with every value** (rows = buckets, columns = series).
- **A visible "Show as table" toggle** under each chart turns that table into a visible one, for anyone who can't use the pointer-only tooltip or prefers numbers.
- Put visually hidden tables inside a visually hidden **box**: a table never shrinks below its one-line text and would widen a phone's page.
- **Series colours in a fixed order, never cycled** (e.g. sky, orange, emerald, violet, pink), from complete class strings, validated so every pair is distinguishable (including for colour-blind viewers) and every mark has ≥ 3:1 contrast against the background, in light and dark. A sixth series folds into "Other".
- **Second cues:** a 2 px gap between stacked segments, a legend for two or more series, the tooltip naming the series, the table.
- **Status colours (red, amber, green) are levels, not series**, and always come with a word.
- **Tooltips show the value first**, then the series, keyed by a short line in the series colour. Ticks and labels use text colours, never the series colour. Quiet marks: no axis lines, hairline gridlines.
- **Click-to-select on a chart** (zoom into a bucket) is a shortcut; the same filter is reachable through the page's controls.
- **Animation off** under reduced motion.
- A ranked bar list (top 10 with bars) is an ordered list of links with proportional bars; it reads as it is, so no hidden table.
- A meter (how full something capped is) uses `role="meter"` with `aria-valuenow/min/max` and a `valueText`, and its level word beside it.
- Tests assert the table's numbers (the SVG doesn't render without layout).

## Before you finish

| Pattern | Fix |
| --- | --- |
| A positive `tabindex` (`tabindex="1"`) | `0` or `-1`, and fix the DOM order |
| `<div onClick`, `<span onClick>` or any click handler on a non-control | `<button>` for an action, `<a href>` for navigation |
| `role="button"` on a `<button>`, or `aria-label` on a `div`/`span` | Delete the ARIA; the native element already says it |
| `aria-hidden="true"` on or above an element containing `href`, `button`, `input` or `tabindex` | Hide the decorative part only, or take the subtree out of the tab order too |
| `disabled` on a control whose only reason is a hover tooltip (no `disabledReason` wired to its description) | Pass `disabledReason`, or `aria-disabled="true"` kept focusable with the reason as its description |
| A create, sign-in or wizard submit disabled until valid | Keep it enabled and validate on submit, focusing the first invalid field |
| `onPaste` with `preventDefault`, or a keystroke filter on an input | Let it paste; validate and trim after |
| `autocomplete="off"` or no `autocomplete` on a name, email, phone, password, address or card field | The field's real token |
| A small visible mark with no extended hit area, or hit areas that overlap | A 44 px pseudo-element on the label or button, sized clear of other targets |
| A scrim, glow or gradient over a control without `pointer-events: none` | Add it (and `aria-hidden` if it is an element) |
| An `aria-live="assertive"` success message, or a message region not mounted empty | `role="status"`, rendered empty before the text changes |
| An error toast, or one carrying Undo or Retry, that hides on a timer | Keep it until dismissed |
| Text that scrolls or updates for more than 5 s with no pause | A pause/stop control |
| An `<img>` with no `alt`, or `alt` that describes the file ("chart.png") | `alt=""` when decorative, the purpose when not |
| `maximum-scale=1` or `user-scalable=no` | Remove it |
| A drag or swipe with no buttons or menu alternative | A single-pointer alternative |
| A focusable element under sticky chrome with no `scroll-padding` or `scroll-margin` | Reserve the chrome's height |
| `document.title` unchanged after a route change, or focus left on the old page | Set the title and focus the new `<h1>` |
