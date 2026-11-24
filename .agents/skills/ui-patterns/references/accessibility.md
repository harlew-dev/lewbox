# Accessibility

Accessibility is part of "done" for every UI change, not a later pass. The component foundation should supply most ARIA and keyboard behaviour (check it does); these rules cover what it can't know. How to test it is `testing`.

## The baseline

| Requirement | How |
| --- | --- |
| **Keyboard** | Everything works with Tab, Shift+Tab, Enter, Space, Escape and arrows. No `div` with `onClick`: use `<button>` or `<a href>`. Lists of options use roving focus (arrows), not one tab stop per item. |
| **Visible focus** | A focus ring on `:focus-visible` on every interactive element; never `outline: none` without a replacement. |
| **Accessible names** | Every control has one: a visible `<label>`, or `aria-label` for icon-only buttons ("Copy workspace ID", "Hide sidebar"). A visually hidden label (`sr-only`) beside a section title still counts. Hidden table column headers name icon/menu columns ("Actions"). A select/listbox trigger's name is its **label** ("Status"), never the selected value; the value is announced as the control's value. Why: a name that changes with each choice ("Planned soon") leaves the user not knowing what the control is for. |
| **Descriptions** | Field help and errors wired with `aria-describedby`; `aria-invalid` on invalid fields. A disabled button's reason is its description. A locked field's reason is its description. |
| **Errors announced** | Error notices and error toasts `role="alert"`; an error that repeats is announced again (re-key it). |
| **Status announced** | Success notices, toasts, "Copied", "Checking…" `role="status"` (polite). |
| **Busy** | `aria-busy="true"` on a region showing stale data while new data loads; buttons in `loading` state are busy and ignore clicks. |
| **Current** | `aria-current="page"` on the current nav item and the current item in a menu. |
| **Sort** | `aria-sort` on the sorted `<th>` only. |
| **Toggles** | `aria-pressed` on toggle buttons (reveal/hide); `aria-expanded`/`aria-controls` on disclosure triggers; `aria-checked="true|false|mixed"` for tree pickers. |
| **Reduced motion** | `prefers-reduced-motion` turns transitions and slides off (`design-system.md#motion`). |
| **Forced colours** | Decorative backgrounds (sliding highlights) hidden under `forced-colors`; state shown by borders/text that survive. |
| **Colour** | Never the only cue (`design-system.md#never-colour-alone`); text contrast ≥ 4.5:1, UI marks ≥ 3:1. |

## Focus management

- **Overlays return focus to their opener** when they close. A dialog opened from a menu item remembers the element focused when it opened (the menu button); the menu closes first, then the item's action runs.
- **Dialogs** trap focus, close on Escape (unless busy), and are labelled by their title and described by their description.
- **Revealing content moves focus to it** and announces it. Removing a filter badge moves focus to the next badge or the Filters button. Never let focus fall to `<body>` after something disappears.
- **Hidden UI is out of reach:** a collapsed sidebar or a closed panel is `hidden`/`inert`/`visibility: hidden` once it has finished sliding out, not just `opacity: 0` or off-screen; it reappears at once when shown. Otherwise keyboard users tab into invisible controls.
- While a modal is open the rest of the page is hidden from assistive tech (the component foundation should do this; check it, and don't fight it).
- The page heading is an `<h1>`; heading levels don't skip.

## Forms

- Every input has a label; placeholders are examples, never labels.
- Required fields are marked `required` and named in the "Fill in required fields" reason.
- Errors appear under their field, are linked by `aria-describedby`, and the field gets `aria-invalid`.
- One-time codes: one input (not six), `autocomplete="one-time-code"`, `inputmode="numeric"`, so paste, autofill and the label work; draw boxes visually over it if you like.
- Checkboxes: the box and its text in one `<label>`; links inside the label open without toggling it.
- A read-only locked field stays focusable; a disabled one is skipped (`forms-and-actions.md#read-only-vs-disabled-fields`).

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
