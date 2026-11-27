# Typography

Type carries most of the hierarchy before colour or space arrive. Set it from one role-named scale, load the faces that scale names, and let the browser break lines wherever it can. The words themselves – punctuation, sentence case – belong to [copy.md#typography](copy.md#typography); this file is how they render.

## The scale

One small set of roles, each pairing a size with its line-height and weight, so a role is one decision instead of three:

| Role | Size | Line-height | Weight | Used for |
| --- | --- | --- | --- | --- |
| `display` | `2.25rem` (36 px) | `1.1` | `600` | a page title on a low-density screen, an empty-state headline |
| `title` | `1.5rem` (24 px) | `1.2` | `600` | page and dialog titles |
| `heading` | `1.125rem` (18 px) | `1.3` | `600` | section headings, card titles |
| `body` | `1rem` (16 px) | `1.5` | `400` | paragraphs, descriptions, field values |
| `ui` | `0.875rem` (14 px) | `1.4` | `500` | buttons, inputs, menus, table cells |
| `caption` | `0.8125rem` (13 px) | `1.4` | `400` | field help, timestamps, counts, secondary lines |

- **Name sizes by role, never by look** (`text-body`, not `text-sm`). Why: a card title can’t drift from a dialog title, and the rules survive the next person.
- **MUST keep the set small.** A new size needs a role that none of the six covers; “it looked better a little bigger” is not one. Emphasis inside a role is one weight step up (body `400` → `500`, `ui` `500` → `600`), never a size change.
- **Heading levels descend with the scale** (`h1` → `title` or `display`, `h2` → `title`, `h3` → `heading`). Two deep levels may share a step if weight or tracking keeps them distinct, and a heading is **NEVER** smaller than body text except a deliberate overline.

## Few fonts, few weights

- **One UI face, at most one display face, two or three weights.** Each extra face, size or weight dilutes the hierarchy the others were carrying.
- Pair for contrast: a serif headline over a sans body reads as deliberate; two near-identical sans faces read as a mistake.
- `system-ui` is a valid free UI face; a brand face always ships with a fallback stack (`"Brand Sans", "Helvetica Neue", Arial, sans-serif`).

## Load every face used

- **Load one `woff2` face per weight and style the design applies.** A browser asked for a weight or italic the family doesn’t ship synthesises it, slanting and thickening the roman: headings and emphasis look wrong in a way no CSS fixes.
- Prevent synthesis only for a treatment you have checked against the fallback, using the narrow longhand (`font-synthesis-weight: none`), never `font-synthesis: none` on body text.
- **CSS properties over raw OpenType tags:** `font-weight: 600`, never `font-variation-settings: "wght" 600`; `font-variant-numeric: tabular-nums`, never `font-feature-settings: "tnum" 1`. Why: the property still picks the nearest face when a non-variable fallback renders; the raw tag silently does nothing.
- Leave `font-optical-sizing: auto`. Raw tags are for axes and features with no property of their own (`"GRAD" 80`, `"ss01" 1`).

## Line-height and letter-spacing

- **Line-height is unitless and set by role** (the table above). `px`/`rem` values don’t grow with the reader’s font size.
- **Text that wraps to three or more lines takes at least `1.4`**, even in a tight card row or table cell. Tight leading is for one or two short lines; at three they start to merge.
- **Letter-spacing is in `em`, by size:** `-0.01em` to `-0.02em` on headings 24 px and up, `0.05em` on uppercase labels 14 px and down, `0` on body copy.
- Kerning is on by default and belongs to the font. `font-kerning: none` is never a fix.

## Measure and wrapping

- **Cap long-form text at 60–75 characters per line**, for example `max-width: 60ch`. One `ch` is the width of `0`, wider than the average letter, so count a real line once rather than trusting the unit.
- **`text-wrap: balance` on headings and short descriptions**, never on a paragraph. **`text-wrap: pretty` on paragraphs**, which keeps a lone word off the last line.
- **`overflow-wrap: break-word` where a long word, URL or ID could escape** its container.
- **`white-space: nowrap` on badges, pills, buttons and short labels** where a break looks broken; **NEVER** on prose.
- Interface text stays `text-align: start`. `justify` belongs only to editorial layouts.
- Hyphenation needs the right `lang` first (`hyphens: auto`); without it, words break in the wrong places.

## Numbers

- **`font-variant-numeric: tabular-nums` on anything whose digits change** (timers, counters, prices, percentages) **and on numeric table columns**, so nothing shifts sideways as values update.
- Leave figures in prose proportional; tabular digits space unevenly in a sentence.

## Truncation

```
/* one line */
overflow: hidden;
text-overflow: ellipsis;
white-space: nowrap;

/* several lines */
display: -webkit-box;
-webkit-box-orient: vertical;
-webkit-line-clamp: 3;
overflow: hidden;
```

- Truncate the rendering, never the data: a long name is cut visually, not in the string, and the value the user sees is the value that was stored.
- **Every truncated value keeps a way back to the full text** – a title, a tooltip, an expansion row. That rule is in [design-system.md#mobile-and-desktop-checks](design-system.md#mobile-and-desktop-checks); this file only supplies the clipping.

## Case and underlines

- **Store copy in its natural case and set case with `text-transform`** (`uppercase` on an overline, `small-caps` on abbreviations). Why: a redesign changes one declaration instead of rewriting strings, and screen readers still get normal words.
- **Underlines come from the font:** `text-underline-position: from-font` and `text-decoration-thickness: from-font`, tuned with `text-underline-offset` only where the default collides with descenders.
- A dotted underline marks an abbreviation or a defined term.

## Inputs at 16 px on iOS

Safari zooms the whole page when a focused field’s text is smaller than `16px`. Both fixes keep a `16px` field; offer both and let the design choose, because they look different.

- **Size up on small screens.** Set `font-size: 1rem` for the phone and the design size from the breakpoint (`0.875rem`). Simplest, but the mobile field no longer matches desktop.
- **Keep `16px` and scale the text down.** Render the intended size with `transform: scale(0.875)`, widen by the inverse (`width: calc(100% / 0.875)`), divide the line-height by the same factor, and set `transform-origin: left` (`right` under RTL). Let a wrapper draw the surface, border and focus ring, because the transform shrinks the whole box.

## Size floors

- Body text starts at **`16 px`** (the browser default); UI text at **`14 px`**; captions at **`13 px`**; **rarely below `12 px`**.
- **Set `font-size` in `rem`**, so a reader who raises the browser’s default font size gets larger text throughout, not only in prose.
- Move below a floor only for a reason you can name (a face that runs small, a dense professional table), never to make a layout fit.

## Smoothing, language, trimming

- **Font smoothing once, on the root:** `-webkit-font-smoothing: antialiased` and `-moz-osx-font-smoothing: grayscale`. Never per component, where the same text renders differently from every other element.
- **Set `lang` on `<html>`** (and on any passage in another language) so the browser picks the right quotes, hyphenation and pronunciation; **set `dir` on the document and wherever direction changes**.
- **Wrap a mixed-direction value – a name, a number, a URL – in `<bdi>`**, or give it `dir`. Digits are never reordered by hand.
- **`text-box: trim-both cap alphabetic` on buttons and badges is a progressive enhancement**, removing the space the font reserves above and below its letters. Unsupported browsers keep their normal leading, so never let alignment depend on it.

## Keep text selectable

- Text stays selectable by default, including table cells, errors and the request reference. `::selection` may carry the accent, provided the selected pair stays legible.
- **`user-select: none` belongs only on a drag or gesture surface.** A button label being highlightable is not a reason, and never apply it to a layout root.
- Decorative type – drop caps, gradient text, outlines, shadows – is built in CSS (`::first-letter`, `background-clip: text`, `-webkit-text-stroke`, `text-shadow`) so the words stay selectable and searchable.

## Before you finish

| Pattern | Fix |
| --- | --- |
| `font-size` in `px` | The same value in `rem` |
| A size outside the scale (`text-[15px]`, a one-off `font-size`) | Use the nearest role, or add the role to the scale |
| `font-weight: 100`–`300`, or `font-light`/`font-thin`, under 18 px | `400` or heavier |
| A weight or italic with no matching `@font-face` | Load that face, or use one the family ships |
| `font-synthesis: none` on body or UI text | Remove it; the narrow longhand only, after checking the fallback |
| `font-variation-settings: "wght"` or `"opsz"` | `font-weight`; leave optical sizing `auto` |
| `font-feature-settings: "tnum"` or `"zero"` | `font-variant-numeric: tabular-nums` or `slashed-zero` |
| `line-height` in `px` or `rem` | A unitless value |
| `1.1`–`1.3` leading on a description, card body or table cell that wraps to three lines | `1.4` or more |
| `letter-spacing` in `px` | The same value in `em` |
| `text-wrap: balance` on a paragraph | `text-wrap: pretty` |
| `text-align: justify` in application UI | `text-align: start` |
| `-webkit-line-clamp` without `display: -webkit-box` | Add `display` and `-webkit-box-orient: vertical` |
| `text-overflow: ellipsis` without `overflow: hidden` and `nowrap` | All three, or the clamp recipe |
| A long ID, URL or unbroken word escaping its container | `overflow-wrap: break-word` |
| A truncated name with no title, tooltip or expansion | A way back to the full text |
| A timer, counter, price or numeric column without `tabular-nums` | `font-variant-numeric: tabular-nums` |
| `<input>` at `0.875rem`/`14px` with no mobile override | One of the two 16 px fixes |
| Body or UI text at `0.75rem` (12 px) or below | `0.875rem`/`1rem`; `0.75rem` only for a badge |
| `-webkit-font-smoothing` inside a component | Move it to the root |
| A mixed-direction name, number or URL without `<bdi>` or `dir` | Wrap it in `<bdi>` |
| `white-space: nowrap` on a paragraph | Remove it; badges and labels only |
| `user-select: none` on a text container or layout root | Remove it; drag surfaces only |
| A label stored as `"API KEYS"` in the source | Natural case, plus `text-transform` |
