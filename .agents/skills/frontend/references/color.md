# Colour

Colour is a small system of ramps, not a bag of values picked per screen. Build the ramps once, give every step a job, and let components reference roles only. Whether a pair is required to pass is in [accessibility.md#the-baseline](accessibility.md#the-baseline); keeping meaning off colour alone is [design-system.md#never-colour-alone](design-system.md#never-colour-alone).

## Ramps, not colours

- **One neutral ramp, one accent ramp, and only the status ramps the product renders** – here `danger`, `warning`, `success`. A warning ramp nothing imports is maintenance for zero pixels.
- **A second accent hue earns its place only** when two things must be told apart at a glance; otherwise use more steps of the one accent.
- Neutrals carry most of the interface, so **never give them fewer steps than the accent**. A pure grey ramp is a fine default; tinting it towards the accent (OKLCH chroma `0.005`–`0.02`) is a style choice, and must hold across the whole ramp – a warm border on a cool background is visible even when neither colour can be named alone.
- Status ramps need fewer steps than the accent: usually a subtle background, a border, a solid fill and a text value.

## Every step has a job

A ramp is not a gradient to pick from by eye. Each step exists because a role consumes it:

| Role | 12-step role scale | 50–950 lightness scale |
| --- | --- | --- |
| `background` page | `1` | `50` |
| `background`, subtle | `2` | `50` |
| `surface` (cards, inputs, wells) | `3` | `100` |
| `surface` hover | `4` | `200` |
| pressed / selected | `5` | `200` |
| `border` subtle, separator | `6` | `200` |
| `border`, focus ring | `7` | `300` |
| `border` hover | `8` | `400` |
| `accent` solid fill | `9` | `600` |
| `accent` solid hover | `10` | `700` |
| `muted` text | `11` | `700` |
| `foreground` text | `12` | `900` |

- **Two scales, two logics.** In the 12-step scale a number is a role: the dark ramp reuses the same numbers, so `accent` fill is step `9` in both themes and component CSS never changes. In the 50–950 scale a number is lightness: the mapping above holds in light mode and flips in dark (page `950`, text `50`), so let a semantic token do the swap rather than branching in every component.
- Where the table repeats a step, those roles render identically today. A design that needs, say, hover and a subtle border to differ needs the finer ramp.
- **Solid fills sit at `600`, not `500`**, because white text on most mid `500` hues fails 4.5:1. Several hues still fail at `600`; measure the actual pair.

## Two tiers

```css
:root {
  /* primitives: a value, named by hue and step. Never used in a component. */
  --blue-600: oklch(0.55 0.19 255);
  --neutral-200: oklch(0.9 0.005 255);

  /* semantic: a role in this interface. The only tier components use. */
  --color-accent: var(--blue-600);
  --color-border: var(--neutral-200);
}
```

- **Primitives are named by hue and step** (`--blue-600`, `--neutral-200`) and **NEVER appear in a component**: `var(--blue-600)` or `bg-blue-600` in a page is the thing to flag.
- **Semantic tokens are named by role and are the only tier components reference:** `--color-background`, `--color-surface`, `--color-foreground`, `--color-muted`, `--color-border`, `--color-accent`, plus `--color-danger`, `--color-warning`, `--color-success`. Each points at a primitive. That seam is what lets dark mode, a white-label theme or an increased-contrast variant repoint colour without touching a page.
- **One vocabulary:** `accent` is the brand, never `primary`. Reserve `primary` for “the most prominent of its group”; a brand token called `primary` makes every later `primary` ambiguous. A semantic name holds no hue and no component (`--color-accent`, not `--color-blue-button` or `--color-sidebar-grey`).

## Use a token only in its role

- **NEVER borrow a token because its value looks right today.** A `border` token used for text works until borders get lighter, and the text goes with them. If a role has no token, add the token.
- `border` inside `color:`, or a text token inside `background:`, is a missing role, not a shortcut.
- A status colour is only for that status: `danger` never decorates, `success` never marks “selected”.
- Opacity modifiers (`/50`), `color-mix()` and relative colour syntax resolve at render time; a translucent surface can’t be checked against a fixed background, so use a solid token wherever text sits on it.

## Build the ramp

- **Use a colour library in a perceptual space, never by eye and never HSL lightness**, which bunches steps at one end and drifts hue. For a new system `oklch()` is the default; everywhere else emit the project’s notation from the same math – one stray `oklch()` in a hex codebase is worse than either.
- **Hold one hue end to end.** Every step must read as the same colour; a wandering hue looks like two colours blended.
- **Vividness peaks mid-ramp and falls off at both ends**; the palest and darkest steps are nearly neutral. Both ends stop short of pure black and white, which have no hue left.
- **Steps are finer at the light end:** about `0.04`–`0.05` of OKLCH `L` between `50` and `200`, `0.07`–`0.10` mid-ramp. No adjacent steps less than `0.03` apart – that is more steps than decisions.
- **Across hues, match lightness exactly and chroma by proportion.** `red-500` and `blue-500` must read equally bright; chroma is set to the same share of each hue’s own maximum, because yellow and cyan peak far below red and blue. Copying one chroma number across every hue makes the warning look weak beside the danger.
- **A brand colour lands where it measures.** If white on it fails, it is not the solid-fill step: put it where it lands and fill with a darker step. Never quietly darken the brand.

## Dark mode

- **A dark ramp is its own tuned ramp, not the light one reversed.** Reversal is the starting point: swap the roles (page → `950`, foreground → `50`), then bring the accent’s chroma down a step or two, widen the separation at the dark end (surfaces that differed as pale backgrounds collapse), and remeasure every pair.
- **One switching mechanism, used everywhere:** `prefers-color-scheme` alone when there is no toggle, a `.dark` class when users can override it, or `light-dark()` with `color-scheme`. A media query setting some tokens and a class setting others gives a half-themed interface the moment a user overrides their system.
- `prefers-contrast: more` needs a value per appearance, at least `0.15` of OKLCH `L` further from its background than the default. One block shared by light and dark darkens the colour on a dark background and lowers contrast there.

## One colour, one meaning

- **One purpose per hue across the interface**, counting hues within about `15°` of OKLCH hue as the same colour: a near-miss reads as a slightly different shade, not a different meaning.
- **The accent means interactive and selected.** Decorative headings, stats and icons stay `foreground` or `muted`; an interactive element left neutral is as misleading as a heading wearing the accent.
- **Keep every status hue at least `15°` from the accent.** If the brand is red, danger can’t also be red: give destruction a distinct treatment (outline plus icon) or move the accent. Keep the conventional meanings (red danger, amber warning, green success), and check them per locale where a market reverses gain and loss.
- **Chart series colours, their fixed order and their second cues live in [accessibility.md#charts](accessibility.md#charts) and the `dataviz` skill**; they are not chosen from this palette.

## One filled action

- **Filled colour marks exactly one primary action per view.** Within a group that is the one solid button; across the page the other groups’ actions stay outline or plain.
- **Put the colour on the background, not the label.** Accent-coloured text on a neutral button reads as a link.
- Selected states (an active tab, a checked segment) may use the accent – that is state, not emphasis. Several coloured backgrounds are fine when each encodes a distinct state or category rather than competing as peers.

## Measure the rendered pair

- **Measure the foreground against the background it actually renders on** – the nearest painted ancestor, not the page – with alpha composited, in both themes. A translucent header, an overlay or `/50` shifts with whatever sits behind it; measure the lightest and darkest content it can cover.
- **Thresholds:** text `4.5:1`; large text (≥ `24px`, or `≥ 18.66px` bold) and UI marks `3:1`, as in [accessibility.md#the-baseline](accessibility.md#the-baseline). Disabled controls and logos are exempt.
- **White on most mid `500` fills fails 4.5:1.** Set solid fills at `600` or darker and measure; green and amber can fail even there.
- **Fix a failing pair by changing lightness only**, holding hue and chroma, then remeasure. A fix that moves hue is a palette change. A mid-lightness background caps what any foreground can reach, so that is the value to move.
- **Never report a contrast value you did not measure.**
- Keep text off gradients where you can; where it must sit on one, measure the worst region or put a scrim behind it.

## Gamut and gradients

- **Declare the sRGB value first, then the vivid one inside `@media (color-gamut: p3)`.** Clipping a colour a display can’t render flattens neighbouring ramp steps, so the top of the ramp loses its distinctions.
- **Gradient interpolation space is a look, not a correctness setting.** Default to `in oklab` for even brightness; switch to `in oklch` when a two-hue gradient goes grey in the middle, accepting that it travels through every hue between the stops. The default is the shorter way round. Banding on large areas comes from closely spaced stops: widen the contrast, shrink the area or add subtle noise.

## Before you finish

| Pattern | Fix |
| --- | --- |
| A hex, `rgb(` or `oklch(` literal in a component where a token exists | Use or add the role token, in the project’s notation |
| One `oklch(` value in a codebase otherwise written in hex | The project’s notation, unless a migration is in scope |
| `var(--blue-600)`, `bg-blue-600` or another primitive in a component | Point a semantic token at it and use that |
| A semantic name holding a hue or a component (`--color-blue-button`, `--color-sidebar-grey`) | Rename for the role (`--color-accent`, `--color-surface`) |
| `--color-primary` beside `--color-foreground` | Rename the brand token `accent` |
| A `border` token inside `color:`, or a text token inside `background:` | Add the missing role |
| `hsl(` ramp values that differ only in lightness | Rebuild in a perceptual space, in the project’s notation |
| `50`–`200` stepping as far apart in OKLCH `L` as the mid-ramp steps | Tighten the light end to `0.04`–`0.05` per step |
| One chroma number copied across every hue’s ramp | The same proportion of each hue’s own maximum |
| A status hue within `15°` of the accent | Move it at least `15°`, or give destruction a distinct treatment |
| Dark tokens equal to the light ramp in reverse step order | Lower the accent’s chroma, widen the dark end, remeasure |
| Tokens set under both `prefers-color-scheme` and a `.dark` class | Pick one mechanism |
| A contrast fix that changes hue and leaves lightness unchanged | Change lightness, the channel contrast responds to |
| `prefers-contrast: more` shared by both appearances in one block | Give each appearance its own value |
| `text-white` on a `500` fill | Measure; use a `600` or darker fill, or dark text |
| Text on an alpha surface (`/50`, `rgba(`, `color-mix(` with `transparent`) | Measure the composited result, or use a solid token |
| `oklch()` outside sRGB with no sRGB value before it | sRGB first, the vivid value inside `@media (color-gamut: p3)` |
| Two filled buttons in one view | One primary; neighbouring actions outline or plain |
| An accent-coloured decorative heading or icon | `foreground` or `muted`; the accent means interactive |
