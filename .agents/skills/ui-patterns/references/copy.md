# Copy and UX writing

Every visible word: labels, titles, descriptions, toasts, errors, empty states, placeholders. Copy is UI; a wrong word is a bug.

## Voice

- **Plain, short, specific.** Say what happened or what will happen, in the user's terms, not the system's ("Project deleted.", not "DELETE succeeded").
- **Sentence case** for everything: buttons, titles, menu items, column headers, tabs ("Create project", "Sign out other sessions", "API keys"). Proper nouns and product names keep their capitals.
- **Second person** for the user ("Your data", "You left Acme."); no "I"/"my" in labels.
- **No exclamation marks, no jokes in errors, no blame** ("You entered an invalid…" → "Enter a valid email address.").
- **One word per concept** across the app: if it's "Remove from workspace" in the members list, it's not "Kick" on the member page; "Sign in"/"Sign out" or "Log in"/"Log out", never mixed.

## Patterns

| Element | Pattern | Examples |
| --- | --- | --- |
| Action button / menu item | **verb + object** | Create project · Save changes · Delete workspace · Revoke API key · Sign out other sessions |
| Bare verb | only when the object is the whole page or dialog | Save (a settings form), Sign in, Next, Back, Cancel, Done, Close |
| Never | — | OK · Submit · Yes/No · a bare "Delete" · Click here |
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
- **NEVER reveal whether an account, email or record exists.** "If an account exists for that address, we’ve sent a link." Show the same text and the same screen for every address; a "not found" vs "sent" difference is an enumeration oracle (see `security-defaults`).

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

## Numbers, dates and names

- **One formatting helper** for dates and date-times, with an explicit time zone (stated in the UI if it isn't the user's local one). Never `toString()` a date in a component.
- Compact numbers in charts and tiles (1.5K), full numbers in tables and tooltips.
- Pluralise properly ("1 project", "2 projects"); use the platform's plural rules (`Intl.PluralRules`) if you localise.
- User-supplied names are always rendered as text, never HTML; truncate visually with a full title, never cut the string.
- Role and resource names with the right article ("an owner", "a member") via a helper.

## Toasts and arrival messages

- Every finished action gets one (`mutations.md`). Name the specific thing when you have it ("Acme deleted." beats "Workspace deleted.").
- When the action ends somewhere else (deleted the last tenant → onboarding), the next page says it: "Acme deleted.", "Your account has been deleted.".
