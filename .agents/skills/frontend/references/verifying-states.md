# Verifying states

A UI change is done when someone has looked at every state it can reach, not when the happy path compiles. This file is the workbench: a throwaway page per component, the stress cases its inputs allow, the comparison against a design, the animation at a tenth of its speed and the removed lines of a diff. It reports what it saw; the fix belongs to the reference that owns it, and automated tests to `testing`.

## A state page for one component

- **One component per page** (the audit log table, the billing card), never a page's worth of them. Restate in one sentence what it shows, where it renders and where its data comes from. Name each state the way the product talks about it (`empty`, `enterprise`, `member-no-access`) and group them by what changes.
- **Read the component and its data hook for the states its code can reach:**

  | Kind | States to render |
  | --- | --- |
  | Data | loading held still, empty, error, one item, a typical set, enough to paginate or scroll, stale data refreshing |
  | Account | each role and permission, each plan tier, owner against member |
  | Feature | a flag off and on, a limit reached, a locked feature |

- **A state the code cannot reach is not a state.** Never add a branch to the component to render one; list the states the design draws and the code lacks as missing. Combine kinds only where the code branches on the combination (`member-empty`), never as a full cross product.
- **Build it as a throwaway dev-only route, registered in the route registry** ([routing.md#the-route-registry](routing.md#the-route-registry)) and excluded from production builds by the dev flag the build already has. One folder holds the page, its fixtures and its switcher (`states/audit-log/`), so removal is one delete.
- **The page renders the real component, imported untouched, and adds a container at its production width, the fixtures and the switcher – nothing else:** no fonts, styles, theme or token overrides. It inherits the app's layout, providers and global styles. Where the framework splits server from client components, the page is client code, or fixtures vanish at that boundary and the stage renders empty.
- **Feed each state at the data boundary, never by editing the component.** In order: props; the cached resource hook's cache seeded under the exact key its path builds ([data-loading.md#keys](data-loading.md#keys)); the project's request mocks. A fixture threaded down five prop levels tests a path production never takes; where the only way in is a new seam, stop and ask.
- **Hold loading and pending states still.** Keep them unresolved until the switcher moves; a timer that resolves them turns a held state into a race.
- **Fixtures look like the product:** real names, amounts, dates and the counts users actually have. "Test item" ×3 makes every state look fine.
- **The switcher sets a `?__state=` search param and reads the active state back, so every state is a link** you can paste into a review. A grouped vertical list beside the component – one group per kind – or a `<select>` with the same groups above it when there is no room. Arrow keys step through it, the active button is `aria-pressed="true"`, and `H` hides it for screenshots.
- **MUST keep the switcher visibly outside the design system:** system font stack, one dark neutral surface, no project variables or tokens. Styled like the product, it becomes part of what you are judging.
- **Load the page and flip through every state once** before handing the URL over. Fixture data that never appears – a cache refetching over the seed, a server boundary, a live subscription – is the failure to fix first.
- **After every visual change, flip through again:** a fix for the error state often breaks the empty one.
- **Remove it in one step when done:** delete the folder, drop the route entry, remove any approved seam, then grep for the route and fixture names.

## Stress scenarios

Stress only what varies. Walk the axes below, keep those whose cue matches the component, write the kept scenarios down one line each and say which axes you dropped.

| Axis | Cue | Values |
| --- | --- | --- |
| Content length | it renders text it does not author: user input, API data, translations | empty, one word, typical, several sentences, one unbreakable 60-character string |
| Content shape | text can come from users or locales we do not write in | emoji alone and inside a sentence, RTL, mixed direction, diacritics and tall scripts, numbers in aligned columns |
| Quantity | it repeats over items: lists, tables, grids, badge rows | 0, 1, many, enough to overflow |
| Container width | always: the component does not choose its width | 320 px, phone, desktop, a narrow sidebar |
| Environment | the project supports the mode | both themes, 200 % zoom, reduced motion, forced colours |

- **Render each scenario in a fixed-width container on one page, labelled above it**, in a single column: one load shows them all and each width stays what the scenario names. **NEVER resize the viewport scenario by scenario.**
- **Switch environments for real:** the actual theme, browser zoom and forced-colours emulation, the OS motion preference. Re-declaring dark tokens under a class paints a different component.
- **Report only what visibly broke** – "Text escapes the field's right edge", never "spacing feels tight", and never a predicted failure:

  | Scenario | Observed | Fix in |
  | --- | --- | --- |
  | 60-character unbroken name | Overflows the card; no wrap, no ellipsis | [typography.md#truncation](typography.md#truncation) |
  | Zero items | Blank region with no message | [copy.md](copy.md), [lists-and-pagination.md#the-paginated-table](lists-and-pagination.md#the-paginated-table) |
  | 320 px container | Page scrolls sideways; the table pushes past its card | [layout.md#plan-for-growth-and-clipping](layout.md#plan-for-growth-and-clipping) |

"Everything survived" is a complete report. Say where the page is running so the user can tab through every scenario.

## Building from a design

- **Read values from the design file, not a screenshot, when the design file exists.** A screenshot has no values: sizes are ratios to the capture, so state the scale you assumed and treat every measurement as an estimate. Scope the run to the named frames; frames showing one piece at several widths are breakpoints, not separate builds.
- **Map every value onto what the project already has:**

  | In the design | In code |
  | --- | --- |
  | A token or variable by name | The project token of that name, even where its value drifted |
  | A raw value equal to a token | That token |
  | A raw value with no token | The nearest token, listed in the report as rounded |
  | A component instance | Our component of that name, with the variant the frame shows |
  | A component instance with no equivalent | Plain markup in place, listed as a deviation; ask whether a component should exist |
  | A one-off group of layers | Plain markup, not a new component |

- **MUST round to the nearest token and list the rounding**, and **NEVER add a token, a variant or an arbitrary value to hit a number.** Ask first when rounding would visibly change the design: more than 2 px on a spacing or size, or a colour landing on a different step.
- **Build only the frames, breakpoints and states the design draws.** A state the design leaves out is not yours to design: keep the component's default and list it as missing. Where part of it has no API yet, build it from props and list it under **Unwired**.
- **No neighbouring edits** – no copy tweaks, icon swaps or "while I was here" cleanups outside the frames. A diff wider than the design is the usual way this goes wrong.
- **Compare at each frame's width, element by element**, and report:

  | Element | Design | Built | Status |
  | --- | --- | --- | --- |
  | Card padding | 20 px | `space-5`, 20 px | Matches |
  | Body size | 15 px | `body`, 16 px | Rounded to token |
  | Status badge | Filled pill | Text label | Deviates, asked: no badge component |

  A difference is a value ("Gap is 12 px, design is 16 px"), never a feeling. Fix what you caused and compare again; every remaining difference is a rounding or a question.

## Animations slowed down

- **Replay each new animation at about 10 % speed and step through it.** The devtools animation panel sets 10 % and lets you scrub; from the console, run this right after triggering the change:

  ```js
  document.getAnimations().forEach((animation) => { animation.playbackRate = 0.1 })
  ```

  It misses durations a library drives from JavaScript; multiply those by 10 for the review and put them back.
- **What a tenth of the speed shows:**

  | Symptom | Usual cause |
  | --- | --- |
  | Content appears or vanishes at once while its container still moves | It mounts with no fade of its own |
  | Content clipped while the container grows | It arrives before the container has room |
  | An empty gap before the container finishes shrinking | Content removed before the close |
  | A 1–2 px jump on the first or last frame | A wrong start value, or a layer promoted and dropped |
  | A popup growing from its centre while anchored to a trigger | No `transform-origin` |
  | A drawer that snaps when reversed mid-flight | A keyframe where a transition belongs |
  | Layout shift around the animated part | A layout property or an unmeasured height |

- **Then watch it at full speed.** The slow pass finds structure; the full pass confirms it reads as one motion. Duration, distance, easing and reduced-motion rules are [design-system.md#motion](design-system.md#motion).

## Reviewing a diff, read the removed lines

The `-` side of a hunk carries what a change quietly took away. Read it before judging the additions: a removed signal is a regression unless the same change replaces it.

| Removed | What to check |
| --- | --- |
| `aria-label`, `aria-labelledby`, `aria-describedby`, `aria-live`, `role` | The control or region lost its name, description or announcement |
| `alt`, `<label>`, `for`/`htmlFor`, `scope` | An image, field or header lost its association |
| `:focus-visible`, `outline`, `tabindex` | Keyboard users lost the focus ring, or the element left the tab order |
| `prefers-reduced-motion` | Motion now ignores the system preference |
| `aria-current`, `aria-sort`, `aria-expanded`, `aria-invalid` | A state became invisible to assistive tech |
| `dir`, `lang` | Direction or language metadata dropped |
| `overflow-wrap`, `text-wrap`, `line-clamp`, `tabular-nums` | Wrapping, truncation or digit alignment silently changed |
| A token swapped for a literal | Measure the rendered pair again ([color.md#measure-the-rendered-pair](color.md#measure-the-rendered-pair)) |
| `type="submit"`, or a `<button>`/`<a href>` that became a `<div>` | Enter, keyboard and middle-click behaviour |
| A user-facing string deleted or shortened | A label, error or empty state lost its information |

**Some removals are the fix:** `aria-label` giving way to `aria-labelledby` on visible text, a `role` dropped because the element is now native, `outline` replaced by a `box-shadow` ring on the same `:focus-visible`, a physical `left` replaced by `inset-inline-start`. Clear those first, then read the rest.

- **Search only deleted lines**, so additions do not mask a removal: `git diff -U0 <base> | grep '^-' | grep -E 'aria-|role=|alt=|<label|for=|focus|outline|tabindex|prefers-|dir=|lang=|text-wrap|line-clamp|overflow-wrap|tabular-nums'`. Read each hunk with its context.
- **Also look for the change that is not finished:** a new variant with no hover, focus, disabled or loading state; a new component with no empty, loading, error or narrow-width rendering; a removed line with no replacement anywhere in the diff.

## Looking, or not

- **When a browser is available, look at the page yourself** before writing a report. A screenshot of one state is not a check of the others.
- **When one is not, say so and hand over the URL, the state list and the widths to check.** **NEVER claim a visual check you did not make**; "unverified" is a complete answer.
- **Remove the state page and its fixtures when the work lands**, and grep the route and fixture names so nothing of the setup remains in the diff. Automated tests for these states belong to `testing`; this page is for the eye.

## Before you finish

| Pattern | Fix |
| --- | --- |
| A dev state route still registered in the production build | Guard it behind the dev flag, or remove the folder and its registry entry |
| A fixture imported outside the states folder | Fixtures live only in that folder |
| A state page with fonts, theme or token overrides of its own | Keep it outside the design system, or delete the overrides |
| A switcher styled with project tokens or variables | System font, one neutral surface, no project variables |
| `setTimeout` or a promise resolving a loading fixture | Hold the state until the switcher moves |
| "Test item", "Item 1", "Lorem ipsum" in a fixture | Product-shaped data in real quantities |
| A fixture that never renders: the stage is blank | Feed at the data boundary – props, seeded cache key, request mocks |
| The viewport resized once per stress scenario | Fixed-width containers on one page |
| An arbitrary value (`w-[343px]`, `padding: 13px`) or a raw hex in a design build | The nearest token, listed as rounded; ask if it shows |
| A state branch added that production code cannot reach | Remove it; list the design's unbuilt states as missing |
| A design comparison with no render of the build | Screenshot each frame width, or say it is unverified |
| A removed `aria-label`, `alt`, `:focus-visible`, `prefers-reduced-motion` or `type="submit"` with no replacement | Restore it, or use the equivalent replacement |
| A new variant with no hover, focus, disabled or loading state | Fill every state before review |
| A new component with no empty, loading, error or narrow rendering | Add them, or list them as missing |
| A leftover state page, fixture or route after removal | Grep the names; the diff has nothing of the setup |
