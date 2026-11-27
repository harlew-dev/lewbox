# Surfaces

Radius, borders, shadows and icons decide whether a surface looks drawn or assembled. Values here are exact where they can be; optical nudges are judged by eye. Colour values appear in `oklch()`; write them in the project’s notation, behind role-named tokens. The icon rules are the detail behind the Icons bullet in [design-system.md#components](design-system.md#components).

## Concentric radius

- **A nested surface’s outer radius equals the inner radius plus the padding plus the border width.** The same radius on both levels leaves the outer corner visibly too tight.

```css
.card { border-radius: 20px; padding: 8px; } /* 12 + 8 */
.card-inner { border-radius: 12px; }
```

- **Past about `24px` of padding, or with deliberately asymmetric padding, treat the layers as separate surfaces** and keep each one’s own radius token; the formula stops looking concentric.
- Use radius tokens; where the outer value must be computed, derive it from the inner token and the padding rather than writing a new number.

## Optical alignment

- **Fix geometry that looks wrong, not geometry that measures wrong.** Start a button with an icon at `2px` less padding on the icon side than the text side, then judge by eye.
- **A play glyph is centred by its bounding box but reads pushed toward its flat side**, so nudge it about `2px` toward its point. Playback icons never mirror, so the nudge stays physical.
- **Asymmetric glyphs** (stars, carets, arrows) carry uneven weight: correct the `viewBox` or path in the SVG, or nudge the wrapper by a pixel when the asset can’t change.
- An optical nudge never changes the hit area or the layout box shared with other elements.

```css
.button-with-icon {
  padding-inline-start: 16px;
  padding-inline-end: 14px;
}
```

## Shadows and borders

- **Shadows carry elevation; borders carry structure.** A popover, dialog or raised card floats; an input, divider, table cell and selected row is bounded.

  | Cue | For |
  | --- | --- |
  | Shadow | floating or raised: menus, popovers, dialogs, a card lifting on hover |
  | Border | boundaries and structure: inputs, dividers, table cells, selected states |

- **Keep the border on inputs** even when they also carry a shadow: a field’s boundary needs `3:1` non-text contrast ([accessibility.md#the-baseline](accessibility.md#the-baseline)). NEVER replace it with a shadow alone.
- **A shadow is several transparent layers, not one grey blur.** Light mode pairs a `1px` ring with a short lift and a wider ambient layer. Dark mode loses depth, so it uses one white ring.

```css
:root {
  --shadow-raised:
    0 0 0 1px oklch(0 0 0 / 0.06),
    0 1px 2px -1px oklch(0 0 0 / 0.06),
    0 2px 4px oklch(0 0 0 / 0.04);
}

/* the project’s dark selector, such as .dark */
.dark { --shadow-raised: 0 0 0 1px oklch(1 0 0 / 0.08); }

.raised {
  border: 1px solid transparent;
  box-shadow: var(--shadow-raised);
  transition-property: box-shadow;
  transition-duration: 150ms;
}
```

- **Keep `border: 1px solid transparent` under a shadow ring.** Forced-colours mode removes box shadows, and this border is the edge it paints instead; transparent keeps the box the same size.
- Hover raises the ring’s opacity a step, not the blur. Use shadow and radius tokens, never a new `rgba()` per component, and transition the named property only ([design-system.md#motion](design-system.md#motion)).

## Outline content images

- **Give a content image a `1px` outline inset by `1px`**, at pure black `10%` in light mode and pure white `10%` in dark: `oklch(0 0 0 / 0.1)` / `oklch(1 0 0 / 0.1)`.
- **Use `outline` with `outline-offset: -1px`, never a border**: an outline never changes the image’s size and follows its corner radius.
- **NEVER a tinted neutral, a near-black palette colour or the accent**: the tint shows as a coloured fringe against the image edge. Skip transparent artwork such as logos and illustrations.

```css
:root { --image-outline: oklch(0 0 0 / 0.1); }
/* the project’s dark selector, such as .dark */
.dark { --image-outline: oklch(1 0 0 / 0.1); }

.content-image {
  outline: 1px solid var(--image-outline);
  outline-offset: -1px;
}
```

## Hover only where hover exists

- **Wrap hover-only styling in `@media (hover: hover)`.** On touch, `:hover` latches after a tap and reads as a stuck selected state. Check the compiled CSS: a utility framework’s `hover:` class may not add the query.
- **On a control that draws its own pressed state, set `-webkit-tap-highlight-color: transparent`**, so the platform flash doesn’t double it. The focus ring stays on `:focus-visible` ([design-system.md#styling-state](design-system.md#styling-state)).
- Touch has no hover, so no information or action exists only under `:hover`; a tooltip is a shortcut, never the only route.

## Contain scroll in overlays

- **Give every scrolling dialog, drawer, menu and side panel `overscroll-behavior: contain`**, so scrolling past its end never scrolls the page behind. Pair it with the scroll lock in [design-system.md#overlays](design-system.md#overlays).
- Only a container that actually scrolls needs the rule; a short list that fits is not a scroll area.

## Icons

### One set, a few sizes

- **One icon set per surface**, at a small fixed set of sizes (`16`, `20`, `24`). The component around the icon sizes and colours it; the call site doesn’t.
- **Inline with text, size at `1em`–`1.25em`** of the adjacent font so the pair scales together.
- Always SVG, never raster, and prefer a size-specific grid where the set ships one: a `24px` drawing scaled to `16px` lands off the pixel grid and renders soft.

### Stroke follows the text

- **An icon’s rendered stroke matches the weight of the text beside it:**

  | Adjacent text | Rendered stroke |
  | --- | --- |
  | `400`, 14–16 px | `1.5px` |
  | `500`–`600` | `2px` |
  | `700`, or an emphasised standalone icon | `2.5px` |

- The widths are rendered pixels. A `24`-unit grid drawn at `16px` scales `stroke-width="2"` to about `1.33px`, so use the set’s absolute-stroke option or `vector-effect: non-scaling-stroke`.
- **Never mix two icon sets on one surface.**

### Colour and state

- **Draw every icon in `currentColor`** and strip hard-coded fills and strokes when importing (`fill="#666"` → `currentColor`).
- **States come from CSS colour and opacity, never a second asset**: hover and selected from tokens, disabled from opacity. The state is still exposed in ARIA, not only in the colour ([design-system.md#never-colour-alone](design-system.md#never-colour-alone)).
- **Outline is the default; fill marks the active state** – the selected tab, a toggled bookmark, a liked heart. Never use the two interchangeably.

### Mirror only direction-bearing icons

- Under `dir="rtl"`, mirror icons whose meaning follows reading direction and leave the rest:

  | Mirror | Leave |
  | --- | --- |
  | Back and forward arrows, navigation chevrons, send | Logos, checkmarks, clocks and other physical objects |
  | Text-block glyphs for lists, alignment and indent | Media playback, which stays LTR by convention |
  | Speaker and volume waves | Symmetrical glyphs such as search, settings and close |

- **One mirroring mechanism per icon**; two flips compound back to unflipped. Analyse composite icons part by part: an overlay badge may keep its position while the base glyph flips.

## Before you finish

| Pattern | Fix |
| --- | --- |
| A padded parent and child with the same `border-radius` | Outer radius = inner + padding + border width |
| A `box-shadow` ring on a card with no border beside it | `border: 1px solid transparent` |
| A shadow ring used as the only boundary on an input | Keep a real border; a shadow is not structure |
| `outline: 1px solid` on an image in a tinted neutral, near-black or accent | Black `10%` / white `10%` |
| A `border` on a content image | `outline` with `outline-offset: -1px` |
| Plain `:hover` or a framework `hover:` outside `@media (hover: hover)` | Wrap it in the query |
| No `-webkit-tap-highlight-color` on a control with its own pressed state | Set it to `transparent` |
| `overflow: auto` on a dialog, drawer or menu with no overscroll rule | `overscroll-behavior: contain` |
| `fill="#..."` or `stroke="#..."` inside an icon SVG | `currentColor` |
| More than one icon set, or an icon size chosen at the call site | One set, component-owned size |
| A `1.5px` stroke beside bold text, or a `2.5px` stroke beside regular text | Match the neighbouring weight |
| A hard-coded stroke width on a scaled icon | Absolute stroke or `non-scaling-stroke` |
| Filled icons everywhere with no outline default | Outline default, fill for the active state |
| A hover or pressed state shipped as a separate icon asset | One asset, styled with colour and opacity |
| `scale: -1 1` on every icon under RTL | Only direction-bearing icons |
| A shadow written as a per-component `rgba()` | The shadow token |
| `transition: all` on a surface | The named property, such as `box-shadow` |
