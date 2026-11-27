# Layout

Layout is grouping, order and breathing room before it is columns or breakpoints. Space groups content, shapes reinforce that grouping, and lines are the last resort. A few shared edges and spacing steps repeat everywhere, and the structure holds when text grows, the viewport shrinks or the direction mirrors. The phone and desktop checks live in [design-system.md#mobile-and-desktop-checks](design-system.md#mobile-and-desktop-checks).

## Group with space, not lines

- **Group with negative space first, a background shape second and a separator line last**, only where space alone can’t carry the structure.

  | Tool | For |
  | --- | --- |
  | Negative space | the default; distance alone groups |
  | Background shape (card, filled row) | a group must read as one unit, such as a selectable row or a draggable card |
  | Separator line | dense data where space costs too much: tables, long settings lists |

- **The gap between two groups is at least 2× the gap inside one**; `8px` within means `16px` or more between. Use the project’s spacing tokens where it has them.
- **One spacing step per level of nesting.** `8px` inside a group, `16–24px` between groups, `24px+` between sections; repeat those steps instead of inventing a value per component.
- A separator is a low-contrast hairline. **Never pair a line with the gap that already did the job.**

```css
/* space groups; no line needed */
.form { display: flex; flex-direction: column; gap: 24px; }
.field-group { display: flex; flex-direction: column; gap: 8px; }
```

## Keep controls distinct from content

- **Every interactive element looks interactive**: a background shape, a border, an underline, or a consistent placement zone such as a toolbar. A control styled exactly like the text beside it is invisible.
- **The inverse fails too:** a static badge shaped like the buttons around it collects dead clicks. Status and metadata never borrow a control’s shape.
- **Borderless controls need more clearance**, because nothing marks where one target ends and the next begins:

  | Between | Start at |
  | --- | --- |
  | Adjacent bordered or filled controls | `12px` |
  | Visible glyphs of borderless text or icon buttons | `24px` |
  | Unrelated control groups | `24px` or more |

  Compact layouts may go tighter, provided the 44 px touch targets ([design-system.md#mobile-and-desktop-checks](design-system.md#mobile-and-desktop-checks)) never overlap.

## Align to shared edges

- **Choose a small set of alignment edges and put everything on them.** The usual strays are an icon 2 px off the text edge and a card padded unlike its neighbour.
- **Deep nesting repeats a spacing step** rather than inventing a new one; three unrelated leading edges in one column read as three layouts.
- **Numbers align to the end and text to the start** (`text-align: end` / `start`), with `font-variant-numeric: tabular-nums` on numeric columns ([typography.md#numbers](typography.md#numbers)).
- An icon beside text is nudged optically; that recipe is [surfaces.md#optical-alignment](surfaces.md#optical-alignment).

```css
/* one shared leading edge, one indent step */
.section { padding-inline: 24px; }
.section > .nested { margin-inline-start: 16px; }
```

## Order by importance

- **The most important content sits near the top and the leading edge.** Identifying content leads a row; metadata and actions trail. Secondary detail moves into a collapsed section, a tab or a detail view instead of burying the key fact.
- **Visual order matches DOM order.** NEVER move content out of source order with `order`, `row-reverse` or grid placement. Why: keyboard focus, screen readers and copy order follow the DOM, so the visual order becomes a lie.
- A view stays short and links deeper rather than showing everything at the top level.

## One primary action, few secondary

- **Each view has one primary action.** Its style, position and wording are [forms-and-actions.md#where-the-action-goes](forms-and-actions.md#where-the-action-goes).
- **More than about three secondary actions go behind a ⋯ menu**, not across the page: a record page’s actions and a table row’s actions are menu items, never loose buttons. Fewer than that may sit as plain or outline buttons.
- A destructive action that is one of those secondary actions is a red menu item, not a loose red button beside the page’s primary.

## Hint at hidden content

- **Every piece of off-screen or collapsed content gets a visible cue.** A scroller that ends exactly at the container edge looks complete, so let the next item peek `16–32px` past it; matching `scroll-padding-inline` keeps a snapped item on the content edge.
- **A disclosure control names what is hidden**: “Show 12 more results”, never “More”.
- **A truncated value keeps a way back to the full text** – an expand control, a detail link, or a tooltip that also opens on focus. The clipping mechanics are [typography.md#truncation](typography.md#truncation).

## Content bleeds, controls float

- **Backgrounds and media reach the viewport edges; text and controls stay inside the layout margins.** Full-bleed media inside a constrained article is a grid whose centre column carries the text and whose media spans every column.
- **Keep full-width buttons inside the margins**, starting near `16px` inline on phones. A control pressed against the viewport reads as system chrome and collides with rounded corners and gesture zones.
- **Safe-area insets are physical.** They resolve only with `viewport-fit=cover` in the viewport meta. Pair `left`/`right` with the matching inset and override under `[dir="rtl"]`; never combine `inset-inline-*` with a physical inset.

```html
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
```

```css
.fab {
  position: fixed;
  right: calc(16px + env(safe-area-inset-right));
  bottom: calc(16px + env(safe-area-inset-bottom));
}
[dir="rtl"] .fab { right: auto; left: calc(16px + env(safe-area-inset-left)); }
```

- **Content scrolls under sticky chrome.** Set `scroll-padding-block-start` to the sticky header’s height, so anchored targets and focused fields never land beneath it. Sticky headers stay below z-index `50` ([design-system.md#z-index-scale](design-system.md#z-index-scale)).

## Hold structure until it breaks

- **Breakpoints come from content, not device presets.** Keep the expanded layout until it stops fitting – the sidebar squeezing text below its measure, a card grid below a usable column width – then collapse.
- **Page-level adaptation uses media queries; a reusable component uses a container query**, so the same card adapts inside a narrow sidebar and on a wide page.
- **A query styles the container’s descendants, never the container itself.** `container-type: inline-size` adds size containment, so a shrink-to-fit box (a flex item with no width, an inline-block, an absolutely positioned panel) collapses to zero unless given a definite width.
- **Check `320px` width and 200 % zoom** before calling the layout done; both force the reflow a narrow phone and a zoomed reader see.

## Plan for growth and clipping

- **Translated strings grow, and short ones grow proportionally more**, so a one-word button label is the riskiest text on the screen. Size text boxes with `max-width` and `min-height`, NEVER a fixed width or height, and let rows wrap. Test one long-string locale.
- **A grid track or flex child refuses to shrink below its content’s minimum**, so long words, IDs and wide tables push past the viewport. Use `minmax(0, 1fr)` on the track and `min-width: 0` on the flex child.
- **`100vw` includes the scrollbar**; use `100%`. **Full-height mobile panes use `100dvh`**, because `100vh` ignores the browser toolbar.
- **A dialog taller than the viewport** gets `max-height: 100dvh`, a scrolling body and an action row outside the scroll area.
- **Never park a critical action where zoom or scrolling clips it.** Keep it in normal flow or in stable chrome – a sticky footer with safe-area padding, or the top of the view.

## Logical properties for anything that mirrors

- **Write direction-dependent layout as leading and trailing**, not left and right:

  | Physical | Logical |
  | --- | --- |
  | `margin-left` | `margin-inline-start` |
  | `padding-right` | `padding-inline-end` |
  | `left: 0` | `inset-inline-start: 0` |
  | `text-align: left` | `text-align: start` |
  | `border-right` | `border-inline-end` |
  | `border-top-left-radius` | `border-start-start-radius` |

- **Reserve physical `left` and `right` for physical geometry**, mainly safe-area insets.
- Flex rows and grid columns follow the inline direction, so they mirror under `dir="rtl"` on their own. Physical offsets, `translateX`, `background-position` and hand-placed `left`/`right` do not; give those an explicit RTL rule.
- **A progression mirrors** – stars, steps and progress fill from the leading edge, the right in RTL – while digits inside a number never reverse. Direction-bearing icons are handled in [surfaces.md#mirror-only-direction-bearing-icons](surfaces.md#mirror-only-direction-bearing-icons).

## Before you finish

| Pattern | Fix |
| --- | --- |
| `grid-template-columns: … 1fr`, or a flex child holding long text or a wide table, overflowing its track | `minmax(0, 1fr)` on the track, `min-width: 0` on the flex child |
| `width: 100vw` | `width: 100%`; `100vw` includes the scrollbar and scrolls sideways |
| `height: 100vh` on a full-height mobile pane | `100dvh`; keep the bottom action in stable chrome |
| `height:` with a fixed length on a text box | `min-height`, or `max-height` with `overflow-y: auto` |
| A fixed `width` on a button, label or field holding text | `max-width`/`min-height`, and a wrapping row |
| `@media (max-width: …)` inside a reusable component | `@container` on the component’s parent |
| `container-type: inline-size` on a shrink-to-fit box with no width | Give it a definite width; containment collapses it to zero |
| `env(safe-area-inset-*)` with no `viewport-fit=cover` in the viewport meta | Add it, or the insets resolve to `0` |
| `inset-inline-*` combined with `safe-area-inset-left` or `-right` | Physical `left`/`right` plus a `[dir="rtl"]` override |
| `float: left` in mirrored UI | `float: inline-start` |
| `margin-left`, `padding-right` or `text-align: left` in mirrored UI | The logical `margin-inline-start`, `padding-inline-end`, `text-align: start` |
| `translateX` or `background-position: left` on mirrored UI | A `[dir="rtl"]` override; neither has a logical form |
| A sticky header with no `scroll-padding-block-start` | Set it to the header height |
| `order:`, `row-reverse` or grid placement moving content out of DOM order | Put it back in source order |
| More than three loose secondary buttons in one view | One ⋯ menu |
| A horizontal scroller ending flush at the container edge | A `16–32px` peek, or a disclosure control naming what is hidden |
| A control with no background, border, underline or placement zone beside body text | Give it a control shape or a toolbar zone |
