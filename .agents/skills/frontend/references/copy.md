# Copy and UX writing

Every visible word: labels, titles, descriptions, toasts, errors, empty states, placeholders. Copy is UI; a wrong word is a bug.

## Voice

- **Plain, short, specific.** Say what happened or what will happen, in the user's terms, not the system's ("Project deleted.", not "DELETE succeeded").
- **Sentence case** for everything: buttons, titles, menu items, column headers, tabs ("Create project", "Sign out other sessions", "API keys"). Proper nouns and product names keep their capitals.
- **Second person** for the user ("Your data", "You left Acme."); no "I"/"my" in labels.
- **No exclamation marks, no jokes in errors, no blame** ("You entered an invalid…" → "Enter a valid email address.").
- **Cut the words that do no work:** "successfully", "please" in routine instructions, "Oops" and "just". "Changes saved." not "Changes successfully saved.", "Unable to save. Check your connection." not "Oops! Please try again."
- **One word per concept** across the app: if it's "Remove from workspace" in the members list, it's not "Kick" on the member page; "Sign in"/"Sign out" or "Log in"/"Log out", never mixed.
- **Match the input device:** "tap" on touch, "click" with a pointer, "select" when both are possible.

**Tone follows the stakes.** Lightness belongs in success and empty states only:

| Context | Tone |
| --- | --- |
| Success, empty states | Warm; a light touch is fine |
| Routine actions, settings, onboarding | Neutral and minimal |
| Errors, destructive confirmations | Calm and plain, no playfulness |
| Data loss, security | Serious and explicit |

## Patterns

| Element | Pattern | Examples |
| --- | --- | --- |
| Action button / menu item | **verb + object** | Create project · Save changes · Delete workspace · Revoke API key · Sign out other sessions |
| Bare verb | only when the object is the whole page or dialog | Save (a settings form), Sign in, Next, Back, Cancel, Done, Close |
| Link | names its destination, and reads alone in a list of links | Read the billing docs · Learn more about exports |
| Toggle / setting | describes the ON state | Send read receipts (not Don’t send read receipts) |
| Placeholder | a realistic example in the format the field accepts, never an instruction | name@example.com · DD/MM/YYYY |
| Field helper | the requirement, shown before the user types | Use at least 12 characters |
| Never | — | OK · Submit · Yes/No · a bare "Delete" · Click here · a bare "Learn more" |
| Success toast | past-tense fact, **full stop**, naming the thing | Project created. · Acme renamed. · You left Acme. · All other sessions have been signed out. |
| Confirmation title | a **question naming the target** | Delete Acme? · Remove Sam from Acme? · Revoke this API key? |
| Confirmation description | the **consequence**, irreversibility last | This permanently deletes the project. It can’t be undone. |
| Confirmation button | the same verb + object as the action that opened it | Delete project (not Confirm, not Yes) |
| Disabled reason | exactly one of two | No changes made · Fill in required fields |
| Locked field reason | who can, or why not | Only owners can rename this · Can’t be changed |
| Empty state | title + one line of purpose + the action | **No projects yet** · Projects group your… · [Create project] |
| No matches | what happened + the way out | No projects match your filters. [Clear filters] |
| Past the end | the fact + the way back | There’s nothing on page 7. [Go to last page] |
| Not found | friendly, with a way home | This page has wandered off. [Go to dashboard] |
| Load-more failure | short, inline | Couldn’t load more · Retry |
| Copy failure | what to do instead | Couldn’t copy. Select the ID and copy it instead. |
| Skeleton label (screen readers) | Loading + the thing | Loading projects |
| Footer count | range of total, en dash | Showing 21–40 of 57 |
| In-progress status | present participle + ellipsis character | Sending… · Checking… |
| Value not loaded yet (a stat) | an en dash | – |

A bare "Learn more" is a broken link label: with two on a page, neither says where it goes. Suffix each one ("Learn more about exports"), and name a link’s destination rather than the gesture: "Read the billing docs", never "Click here".

## Descriptions

A title doesn't need a description. Add one only when it tells the reader something they need that the title and the content beside it don't make clear.

- **Never restate the title:** "Password" → "Change your account password" adds nothing.
- **Leave it out** for sections whose fields, buttons and lock tooltips explain themselves (General, Profile, Password, Sessions).
- **Keep it** where it carries consequences or rules the reader can't infer: every danger zone row, every confirmation, every empty state.
- **It sits right under its title** (4–8 px), read as a subheading, never spaced like the next section.
- A reason something can't be changed goes in the field's lock, not a description.

## Errors

- **Show the API's message.** The API writes human 4xx messages ("Too many requests. Try again in 12 minutes.", "That name is already taken."); don't replace them with "Something went wrong". Field errors go under the field.
- **5xx:** the generic message the API sends, plus "Reference: req_…" (`mutations.md#the-request-id`).
- **Network/timeout:** say it's the connection, and that trying again is safe only where it is.
- Write errors as what to do: "Enter a password of at least 12 characters." beats "Password too short."
- **Show a known requirement as helper text before input**, not only after a failed submit; the error then repeats the one rule that failed.
- **NEVER reveal whether an account, email or record exists.** "If an account exists for that address, we’ve sent a link." Show the same text and the same screen for every address; a "not found" vs "sent" difference is an enumeration oracle (see `security`).

## NEVER render text from the URL

Query parameters select a **fixed message by key**; unknown keys show nothing:

```
NOTICES = { "account-exists": "You already have an account. Sign in here, or reset your password." }
notice = NOTICES[params.notice]      // ?notice=<anything else> -> nothing
```

Why: `?message=Your account is locked, call +1-555…` rendered on your real domain is a phishing page with your TLS certificate. The same goes for names in URLs: look the record up through the API instead of echoing a parameter.

## Typography

| Use | Not |
| --- | --- |
| ’ (apostrophe): can’t, There’s | ' |
| “ ” around quoted names: Type “Acme” to confirm | "Acme" |
| – (en dash) in ranges and for "no value": 21–40, 9:00–17:00 | - |
| … (one character): Sending… | ... |
| · (middle dot) between inline alternatives: Terms of Service · Privacy Policy | \| |
| Non-breaking space between a number and its unit: 12 MB | |

Keep these in source as the actual characters (or one helper), and make the tests match them exactly (see `testing`).

## Strings, plurals and formatting

- **Build every string whole, with placeholders:** "{name} invited you to {project}". **NEVER splice a value into fragments** (`"You have " + n + " new messages"`): word order differs per language, and the translator can only move a placeholder.
- **Use the locale's plural rules, never a ternary.** `n === 1 ? "" : "s"` is wrong wherever a language has more than two forms; `Intl.PluralRules` (or the message format's `plural`) picks the category, and zero can be its own message ("No new messages").
- **Format values through `Intl`:** numbers, currency, units, dates, relative time and lists (`Intl.ListFormat`), never a hand-joined `", "` and `" and "`.

## Numbers, dates and names

- **One formatting helper** for dates and date-times, with an explicit time zone (stated in the UI if it isn't the user's local one). Never `toString()` a date in a component.
- Compact numbers in charts and tiles (1.5K), full numbers in tables and tooltips.
- Pluralise properly ("1 project", "2 projects") through the locale's categories, never by appending a suffix.
- User-supplied names are always rendered as text, never HTML; truncate visually with a full title, never cut the string.
- Role and resource names with the right article ("an owner", "a member") via a helper.

## Toasts and arrival messages

- Every finished action gets one (`mutations.md`). Name the specific thing when you have it ("Acme deleted." beats "Workspace deleted.").
- **A frequent action the API can reverse acts at once and offers Undo**, never a confirmation: "Project archived." with an **Undo** action. That toast stays until dismissed; a plain success toast hides after 5 s.
- **Confirm first when the action is irreversible, touches other people or acts on many items at once.** Permanent delete is always a confirmation dialog, never a page.
- When the action ends somewhere else (deleted the last tenant → onboarding), the next page says it: "Acme deleted.", "Your account has been deleted.".

## Before you finish

| Pattern | Fix |
| --- | --- |
| `Click here`, or a link reading `here` | Name the destination |
| Two bare `Learn more` links on one page | Suffix the subject: "Learn more about exports" |
| `Oops`, `Something went wrong` or `!` in an error string | Say what failed and the next step |
| `successfully` anywhere in a string | Cut it |
| `please` in a routine instruction | Cut it |
| `" + n + "`, or a sentence split around a value | One message with a placeholder |
| `n === 1 ? "" : "s"`, or any ternary picking a suffix | The locale's plural rules (`Intl.PluralRules`) |
| `.join(", ")` or `" and "` joining names by hand | `Intl.ListFormat` with the user's locale |
| `toLocaleDateString()` with no locale, or a date built from `getMonth()` | `Intl.DateTimeFormat` with the user's locale |
| A toggle label starting `Don’t`, `Disable` or `Hide` | The ON state: "Send read receipts" |
| A placeholder holding an instruction ("Enter your email") | A realistic example: "name@example.com" |
| A requirement that appears only after a failed submit | Helper text under the label before input |
| A reversible archive, move or trash behind a confirmation | Act at once, toast with Undo |
| `>OK<`, `>Yes<` or `>No<` on a dialog button | Verb + object |
| `Save Changes` beside `Discard changes` | One case per element type |
| "Click" in copy for a touch-only surface | "tap", or "select" when both are possible |
