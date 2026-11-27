# Email delivery

Email is how attackers reach your users with your name on it, and how your server could be turned into a spam cannon. So every email must be:

- **triggered safely**: limits on the request, never on the send;
- **rendered safely**: escaped templates, fixed headers;
- **delivered reliably**: queued, retried, never blocking a request.

## Send through listeners, not handlers

```
handler:  validate → write change + record event (same transaction) → 202/201
listener: on event → render (HTML + text) → check block list → driver.send → mark delivered
```

- **Handlers never send mail.** They record an event, and a background listener sends. A slow or failing provider then never holds up a request, and a failure is retried.
- **Retries with backoff:** 1 s, 10 s, 1 min, 10 min, 1 h, then failed and reported as an error group. Retry permanent refusals too, since providers misreport.
- **A duplicate must be harmless.** If the provider accepted a message and the process died before marking it done, the listener runs again. Make it send a **fresh** link that replaces the first, rather than a second copy of a still-valid one.
- **Log `mail sent` with the template and Message-ID only:** never the recipient or the body. Store errors scrubbed (the SMTP reply's recipient becomes `<email>`).
- **Check state at send time:**
  - send nothing for a suspended account's requests;
  - skip data that has been minimised or erased;
  - a listener must drop work older than its link's lifetime.

## Rate-limit the trigger, never the send

**Limits go on the request that causes an email. The driver and listeners never delay, throttle or drop anything once queued.** Delaying sends per recipient was considered and rejected, for three reasons:

- a delayed reset link arrives expired or replaced;
- the person who asked is told it worked while nothing arrives;
- a per-recipient send budget is exactly what an attacker would spend to **suppress a victim's security mail**.

Every email type is one of four kinds, and a new one isn't done until it fits one:

| Kind | Examples | Rule |
| --- | --- | --- |
| **Asked for by a user** (for themselves or someone else) | password reset, sign-up code, invitation, email-verification code | a `<feature>:per-email` limit on the request (e.g. 3 / hour), counted for **every address alike**, registered or not, so the 429 reveals nothing; plus per-IP / per-user / global limits. Mail that can reach a stranger (an invitation) is **blockable** |
| **Security mail** about the recipient's own account | email changed (to old and new inbox), password changed, new sign-in | **never limited:** an attacker must not be able to spend the victim's budget and hold back the warning. The actions that trigger it are already limited |
| **Operational mail to admins** | alerts | opt-in subscriptions plus the alert damping (see `monitoring-and-alerting.md`) |
| **Notifications** about others' actions | "a project was deleted in your tenant" | not rate limited, but **opt-in only**, off by default, and only the user can turn them on |

**The one exception** is a cap counted in the listener, after the response. Use it only when a per-account limit on the request would reveal whether an account exists. Example: "You already have an account" notices, at most 5 a day per account. Above the cap, send nothing, record `rate_limit.reached` once, and count only first attempts so retries don't spend it. Never apply it to codes or security mail.

## Block list (suppression)

- A recipient who never asked for your mail (an invitee) can **block their address** from a link in the email. After that, **every** send to that address is refused before rendering (`ErrBlocked`), whatever the template. Make the mailer impossible to construct without the block-list dependency.
- **Blockable templates** get a fresh 256-bit block-link token per send (stored as a digest, kept a year). It appears in the footer of both parts and as `List-Unsubscribe: <https://…>` (RFC 2369).
  - **Use the URL form only, not `List-Unsubscribe-Post` one-click.** One click would block without the page's confirmation, and mail scanners pre-fetch links. Note that some large mailbox providers now require one-click unsubscribe for bulk senders. If you send marketing or bulk mail, revisit this.
- **Not blockable:** password reset, codes and security mail. Those go only to account holders, who can delete the account.
- **Treat `ErrBlocked` as done:** no retry, no "sent" event. Show the inviter "Sent" anyway, so nothing anyone can type reveals that an address is blocked.
- **Send no confirmation email after blocking.** The page confirms it.
- **Unblocking:** via the owner's own link, or by an operator (CLI or console, with a reason). The console can **unblock, never block**.
- Keep the suppression list with the database's backups. It must outlive everything else.

## Templates

- **Every email has an HTML part and a plain-text part.** Clients that don't render HTML show the text, and sending both lowers spam scores. Each `.txt` holds the same words, the link on its own line, and no markup.
- **Parse and render-check every template at startup.** A missing or broken template, of either kind, **stops the server from starting**. Render both parts before delivering. If either fails, send nothing.
- **Templates are trusted, data isn't.** Templates come only from files shipped in the build, never from the database or a request.
- **Shared layout:** the document, the card and the logo plus app name at the top (the name as text, so the email is branded with images off), with the block link in the footer for blockable mail. Each template defines only its `title` and `content`.
- **Data structs hold plain strings only.** Add example data for each template; a test renders every template with it.

### Autoescaping

- **Use a context-aware autoescaping engine for HTML** (Go `html/template`; Jinja2 or Twig with autoescape on; Handlebars `{{ }}`; React Email or MJML with escaped props). A user's name like `<h1>Hack</h1>` must render as text. Mail clients don't run scripts, but they **do** render injected HTML: a fake button, a different link, a hidden message.
- URLs in `href` must be filtered to `http`, `https` and `mailto`. A `javascript:` value must not survive.
- **NEVER mark data as safe** (`template.HTML`, `|safe`, `{{{ }}}`, `dangerouslySetInnerHTML`). Those switch escaping off.
- Plain-text templates may use a non-escaping engine, because there's no markup to inject. Still, refuse control characters in names at the API, so a name can't add lines.
- A test checks the HTML engine's import (or config) so nobody swaps in the non-escaping one.

### Headers: fixed subjects, CR/LF validation

- **Subjects are fixed strings in code**, not in template files and never built from user data. They're headers, and HTML escaping would garble them (`&` becomes `&amp;`). Where a subject must carry a variable (an alert title), build it in code and check it against a strict pattern (words, digits, a little punctuation).
- **The recipient must be a bare address with no display name, no CR and no LF**, checked before rendering. Otherwise `"x@y.com\r\nBcc: everyone@…"` adds headers.
- **From** comes from config (`MAIL_FROM_NAME`, `MAIL_FROM_ADDRESS`, as two variables). `List-Unsubscribe` is the app origin plus a base64url token; the driver refuses CR, LF, `<` and `>` in it. **Nothing else** is set from data.
- **Add `Auto-Submitted: auto-generated`** (RFC 3834), so out-of-office replies don't answer, and a random Message-ID on your domain. Omit `User-Agent`.

### One link per email: what goes in the body

- **Emails a stranger can trigger must carry nothing the requester chose.** A sign-up code email goes to whatever address someone typed, so it contains **no name and no link**, only the code ("482 913") and fixed text. Otherwise attackers use your sign-up form to send phishing text through your trusted domain.
- **One call-to-action link per email,** built from `APP_ORIGIN`, never from the request's `Host` header. Host-header injection would let an attacker make your reset email point at their domain. Alongside it, at most a fixed homepage link (for invitees who don't know the app) and the block link.
- **Show the account's own name, never the name typed into the request.** "You already have an account" uses the stored name.
- Security notices about an email change never reveal the other address and never offer a reversal token.
- **Alert emails contain no link at all** (see `monitoring-and-alerting.md`).

### Images

- **No remote images.** They're blocked by default in many clients and act as tracking pixels.
- **No SVG** (Gmail and Outlook don't show it) and **no `data:` URLs** (Gmail blocks them).
- Embed images as PNG **inline attachments** referenced with `cid:` (e.g. a 64×64 logo shown at 32×32).
- Use inline styles only, and no scripts.

## Drivers

| Driver | Use |
| --- | --- |
| `smtp` (default, production) | any provider over SMTP: Resend, Postmark, SES, Mailgun. One connection per message, 10 s per step, inside the listener's 30 s |
| `log` / file | development without a local mail catcher. Writes to a `0600` file. **Refused in production:** it holds working reset links |
| `off` | no mail. Features that need it answer 503 ("Password reset isn't available."), the UI hides them, and email verification must be off too. **Production must say `off` explicitly** |

In development and e2e tests, send SMTP to a local catcher (e.g. Mailpit), bound to localhost only because it holds working links. E2E tests read it through its API.

### SMTP settings

| Setting | Rule |
| --- | --- |
| `SMTP_HOST` | a host name only: no `user:pass@`, `/` or spaces (credentials in a URL would appear on config pages) |
| `SMTP_TLS` | `starttls` (587, refuse servers without STARTTLS) or `tls` (465); `none` only for the local catcher. **Refused in production, and anywhere with credentials** |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | **both or neither**; AUTH only over TLS; the password from the secret store |
| `MAIL_FROM_ADDRESS` | required, a bare address on a domain the provider has verified |

- Always verify certificates against system roots, with TLS 1.2 minimum.
- Startup refuses a broken config, naming the **setting, never the value**. The startup line shows host, port, TLS mode and sender, never credentials.

## Deliverability (before the first email)

- Verify the sending domain with the provider and publish **SPF, DKIM and a DMARC policy**. Without them mail lands in spam or is refused. The provider signs; the app doesn't.
- **Outbound SMTP:** some hosts block ports 587 and 465 on new accounts. Ask them to open it, or use the provider's HTTPS API.
- If the provider is down, mail waits and retries for about an hour, then fails and is reported. Requests keep working because handlers only queue.

## Adding an email

1. Write `<name>.html` and `<name>.txt` with the same text and the link on its own line. Use inline styles, no scripts and no remote images.
2. Add a constant, a **fixed subject in code**, and a data struct of plain strings.
3. Add example data. The render-every-template test fails without it.
4. Decide its kind: asked-for mail gets a `<feature>:per-email` limit on the triggering request, with an integration test; security mail gets none; notifications need an opt-in setting first.
5. Decide whether it's blockable. Mail that can reach someone without an account who didn't ask for it must be.
6. Check that the body contains nothing a stranger could choose.
