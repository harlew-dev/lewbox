# Secrets, sensitive values and the copy component

Screens are shared, recorded, screenshotted and shoulder-surfed. Two kinds of value stay unreadable until asked for. Server-side handling of secrets is `security-defaults`; this file is the UI.

## Two kinds

| Kind | Examples | Grants access alone? | In the DOM before reveal? | Hidden as |
| --- | --- | --- | --- | --- |
| **Secret** | recovery codes, a TOTP QR code and setup key, a new API key, a webhook signing key | **yes** | **no** | a decoy placeholder / bullets |
| **Sensitive** | your email address in settings, a phone number | no | yes | a blur drawn over the field |

## Secrets

- **Out of the DOM until revealed.** A `Reveal` component renders only a **decoy placeholder** (fake blurred content, `aria-hidden`) with a "Reveal recovery codes" button over it. The real value is not in the page, so dev tools, browser extensions, screen readers and the accessibility tree can't read it.
- **NEVER blur the real secret.** A blur is a reversible image filter; a screenshot of blurred text can be de-blurred. Blur is for sensitive values only.
- **A masked field holds bullets, never the value** (`value="••••••••"`, not `type="password"` holding the key). Revealing swaps in the value; hiding swaps it out.
- **Copy works while masked:** the copy hook reads the value from component state, not from the DOM.
- **Revealing moves focus to the revealed content and announces it** ("Recovery codes shown") through a status region; **Hide** puts the placeholder back. Re-key the component to start hidden again (a new setup).
- Fetch the secret only when the step that shows it opens; don't prefetch it with the page.

### One-time secrets (shown once at creation)

A created API key's plaintext comes back once in the creation response:

- **Keep it in the dialog's component state only.** NEVER in the data cache, a global store, `localStorage`/`sessionStorage`, the URL, navigation state, logs or error reports. Lists cache metadata only.
- **It's discarded** on Done, navigation, tenant switch and sign-out, because the component unmounts.
- The creation dialog switches to a **secret step**: the masked value, a ghost Copy icon at the callout's top right, an "Example request" disclosure, a link to the API docs (new tab, external-link icon), and **Done**.
- **Closing early** (Escape, backdrop, Cancel) opens a confirmation: "Close without copying? You won’t be able to see this key again." The key stays valid; it isn't revoked.
- **Never auto-retry the request that issues a secret.** A timeout may have created the key; a retry would mint a second one the user never sees.

### Proof fields

- Passwords and MFA codes are cleared on submit (success or failure) and on close; a resend asks for fresh proof.
- Step-up proof (MFA before a sensitive action) goes in a **nested dialog** over the filled form, sent **with** the real request, never as a separate "verify" call. Cancelling it clears the code, keeps the parent form and returns focus to its submit button.

## Sensitive values

- In the DOM, drawn blurred **over** the input (an overlay with `backdrop-filter`, marked `data-blurred`), not as a text shadow (the input clips it to its line). The field still reads as a normal field: screen readers read it, it's not a password input.
- **A click anywhere on it reveals it** (and a "Click to reveal" tooltip).
- **An eye button** first in the field's trailing actions toggles it for keyboard users: tooltip and label "Reveal"/"Hide", `aria-pressed`.
- Elsewhere (the sidebar's account menu) show the name, not the email.

## Never in URLs or storage

- No session token, CSRF token, API key, reset token or email in a query string or `localStorage`. The single exception is a single-use emailed link token in its own page's path (and in a `?next=` back to that page); it travels to the API in the JSON body, never in an API URL.
- Client error reports carry the route pattern, never the path or query (`routing.md#status-pages-and-errors`).

## The copy component

**Every Copy button is one hook plus one component.** Never write `navigator.clipboard.writeText` or a "Copied!" message by hand.

```
copy = useCopy(value, { onCopied })            // value read from state, works while masked
<CopyButton copy={copy} label="Copy workspace ID" variant="icon" />   // in an input's trailing actions
<CopyFailure copy={copy}>Couldn’t copy. Select the ID and copy it instead.</CopyFailure>
```

| Variant | Where |
| --- | --- |
| `icon` | inside an input's trailing actions, "Copy" tooltip on hover/focus |
| `plain` | a ghost icon-only button in a callout's corner (the one-time secret) |
| `button` | an outline button with text ("Copy codes") |

- **"Copied" is a tooltip on the button, never text on the page.** After a click it shows for **3 s** and hides itself; meanwhile the icon turns into a check. It doesn't shift the layout.
- **Screen readers hear "Copied" once**, from a visually hidden `role="status"` region beside the button, **emptied before each copy** so a second copy is announced again. The tooltip's own text is `aria-hidden` so it isn't read twice.
- **Clipboard unavailable** (insecure origin, permission refused, old browser): `CopyFailure` shows an instruction under the field or button as an alert and **keeps it until the next try**; an instruction mustn't vanish before it's read. Keep the value selectable (read-only, never disabled) so the instruction can be followed.
- **`onCopied`** runs after each copy that worked (e.g. recovery codes now count as saved, enabling Continue), **never after the component unmounted**, however late the clipboard answers (guard with a mounted ref and clean up the timer).
- Recovery codes offer Copy **and** Download; either counts as saved, and the setup's final button waits for one.
