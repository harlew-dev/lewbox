# Enumeration resistance: the principle, the threat model and the techniques

Read this before designing any flow that takes an email, username, ID, token or key from someone who hasn't proven who they are. The flow-by-flow designs are in the other reference files; this file is the "why" and the toolbox they all use.

## The principle

> **Nothing an outsider can observe reveals whether an account, address or hidden resource exists, or what state it is in, until they have proven ownership.**

"Proven ownership" means one of:

| Proof | Who holds it | What it unlocks |
| --- | --- | --- |
| The password | the owner (or whoever stole it) | "password right": the MFA step, and states if MFA is off |
| Password + second factor | the owner | every account state: suspended, locked, unverified |
| A code or link sent to the inbox | whoever reads the inbox | the inbox's own facts: "you already have an account", "this address is blocked", "this address is taken" |
| A live session | the signed-in owner | their own data, their own tenants |
| Tenant membership | members | the tenant's own resources (403 is allowed from here on) |
| A valid API key | the key holder | the key's own tenant and scope |
| The admin route key + admin credentials | operators | the audit trail, where the truth was written all along |

Until then, every input gets the same answer. The difference, if there must be one, goes somewhere only the proof holder can see: their inbox, or the audit log.

## Threat model

The adversary is anonymous (or signed in to their own account), holds a list of emails or IDs, and can send unlimited requests from many IPs. They want to learn:

1. **Existence:** which emails have accounts, which tenant/project IDs exist, whether the admin console exists and where.
2. **State:** which accounts are suspended, locked, unverified, MFA-protected, or admins; which addresses are blocked.
3. **Activity:** whether someone else (the owner, another prober) touched an account or address recently.
4. **Leverage:** spam an inbox through your server, lock a victim out, take over a pending sign-up, or put their own words into your email.

Why it matters: an existence list feeds credential stuffing and targeted phishing ("your account at X…"); a state list tells an attacker which accounts are worth the effort (no MFA) or who has been suspended; for some products the mere fact of having an account is sensitive (a health app, a dating site, "is my ex on this site?"); a blocked address tells a harasser a real person reads that inbox.

## Every observable channel

Each one is a separate oracle. A design is only as strong as the weakest channel.

| Channel | How it leaks | Default defence |
| --- | --- | --- |
| **Status** | 401 vs 404, 202 vs 409, 403 "suspended" before the password | One status per flow for every unproven input |
| **Body** | "No account with that email", different JSON keys, a field present only sometimes, field order, trailing newline | Byte-identical text; the typed value echoed the same way for everyone |
| **Headers** | an extra `Set-Cookie` only for real accounts, a different `Content-Length`, `Retry-After`, `WWW-Authenticate` sometimes | Same headers in the same order; the only allowed difference is the request ID |
| **Cookies** | a flow cookie set only when the account exists; a different `Max-Age` | Set the same cookie (random value) for every input |
| **Timing** | unknown email answers in 1 ms, known in 60 ms (password hash); an extra query for real rows | Dummy hash; no request-time lookup (202 + listener); one fixed-shape query |
| **Emails sent** | a code arrives only for new addresses | Only the inbox differs, and only the inbox owner sees it |
| **Rate-limit counters** | a 429 that only real accounts can reach; a counter shared with the owner's signed-in activity; `Retry-After` revealing when the window started | Count every typed address alike, before lookups; separate counters per flow; aligned windows |
| **CAPTCHA appearance** | a challenge only for real accounts | Challenge decided only by counts that treat every address alike |
| **DB writes** | a row (or a sequence gap visible in IDs) only for real accounts; different tables touched | Same rows, same tables, same order; asserted by a query hook |
| **Events** | an event visible to tenants or exports that exists only for some addresses | Tell-tale events are operator-only; excluded from tenant logs and personal exports |
| **UI copy** | "We sent you a code" vs "You already have an account"; help text that changes per address | Conditional copy ("If an account uses this email…"); identical help for everyone |
| **Redirects** | `/verify` for new addresses, `/login` for taken ones | Every input goes to the same next page; only proof changes the route |

## The four patterns

Every flow in this skill is built from these. When designing a new flow, name which one you are using.

1. **The same front door for every input.** Status, body, headers, cookies, stored row shape and tables touched are identical. The branching happens in an asynchronous listener after the response (typically: "send mail"), where the requester can't see it.
2. **Fall through to the real "unknown" handler**, rather than imitating it. A wrong admin key is served by the same handler as `/nowhere`; a valid key on another tenant's path gets the same 404 as an unknown route. Imitation drifts (a header, a newline); falling through can't.
3. **Reveal only after proof.** State, conflicts and "blocked" are told to whoever proved ownership, at the point where they can act on it.
4. **The truth goes to the audit trail, not the response.** `auth.login_attempted {result: "unknown_account" | "wrong_password" | "suspended" | "locked" | …}` lets operators diagnose without the caller learning anything.

## Statuses that don't leak

| Status | Means | Use for |
| --- | --- | --- |
| **202** | "We'll do whatever is appropriate" | Reset requests, sign-up with verification, resend, email change requests |
| **401** (one body) | "Those credentials don't work" | Every bad password, unknown email, malformed input at sign-in, every bad API key |
| **404** (one body per resource) | "You can't see this" | Missing, malformed, another tenant's, not a member, hidden route, wrong admin key |
| **403** | "You're established, but not allowed" | Only after membership/ownership is proven: a member lacking a permission; an account state after full sign-in |
| **422** (on a field) | "This input is wrong" | Body-only validation (runs before any lookup), and wrong proof by an already-signed-in user |
| **409** | "Conflicts with existing state" | Only after proof (a right code), or in a mode whose leak is accepted and documented |
| **429 / 428 / 503** | Rate limit, challenge, busy | Decided by counters that treat every address alike, before any lookup |

## Techniques catalogue

| # | Technique | Why (one line) |
| --- | --- | --- |
| 1 | **Dummy hash** for an unknown email, made at startup with the real hash function and current parameters | An unknown email costs one full password hash, like a real one |
| 2 | **One failure message** for unknown email, wrong password, malformed email, over-long password | Body and status can't separate them |
| 3 | **Record the attempt event on every path**, same insert shape (nullable user) | Equal write cost; the result goes to the audit trail |
| 4 | **202 + listener instead of lookup**: record an intent event, answer, look the account up after the response | Nothing to time: the request never reads `users` |
| 5 | **Pending rows**: no user row until an emailed code is right | Sign-up can't say "taken"; `users` holds only verified accounts |
| 6 | **Pending rows hold an HMAC of an unsent code** when the address is taken or blocked | Every guess fails exactly like a wrong code; status endpoints advance the same way |
| 7 | **Owner notice instead of requester answer** ("You already have an account", "Email change requested"), no code, no requester details | The owner learns; the prober learns nothing |
| 8 | **Listener-side caps** for account-specific mail, dropped silently, crossing recorded once | A request-time per-account cap would 429 only real accounts |
| 9 | **Per-address limits counted for every typed address, before any lookup**, keyed by HMAC | A 429 can't depend on existence; a dump of counters can't be reversed |
| 10 | **Separate counters per flow** (anonymous sign-in, sign-up, signed-in checks never share one) | The owner's own activity can't show up in a prober's 429; sign-up spam can't lock sign-in |
| 11 | **Aligned windows** for anonymous per-email limits | `Retry-After` is the same for every address and doesn't reveal when the first hit happened |
| 12 | **Browser-bound Strict cookies** for pending flows; rows found by cookie digest, never by email | A stranger's simultaneous sign-up can't be completed with the owner's code; reloads resume |
| 13 | **Shape-check before any query; malformed = unknown** | No fast path for garbage; same answer as missing |
| 14 | **Malformed code digested as a fixed non-code** and compared like a wrong one | No early exit for junk input |
| 15 | **Fall through to the real unknown handler** for a wrong secret | Byte-identical for free, no query |
| 16 | **Catch-all route behind every gate** | Otherwise the router answers 405 for a known path with the wrong method, revealing it |
| 17 | **Constant-time comparison of all candidate secrets**, no early exit | Timing reveals neither a match nor which key matched |
| 18 | **Secondary gates (IP allowlist) checked only after the key matched, answered as a wrong key** | Someone outside can't learn the key was right |
| 19 | **Look up through membership in one query** | "Missing" and "not yours" are the same empty result |
| 20 | **Scope before permission** (404 before 403) | A 403 would confirm an out-of-scope resource exists |
| 21 | **Filters before counts, search and pagination** | Totals don't leak hidden rows |
| 22 | **Fold states into existing answers**: "blocked" into "taken" (409) or into the normal 202; "suspended key owner" into the generic 401 | A new state never creates a new observable |
| 23 | **Reveal states only after full proof** (password + code) | State can't be probed by guessing |
| 24 | **Silent suppression at the mail layer**: accept, store and list as usual, then refuse to send and treat it as done | A harasser can't learn they were blocked |
| 25 | **Tell-tale events hidden from tenant logs and personal exports** | A missing `.sent` or an `existingAccount` flag would leak |
| 26 | **Conditional, identical copy** ("If an account uses this email, we've sent a link") | The UI can't leak what the API hides |
| 27 | **Fixed-text notices**: `?notice=account-exists` maps to a sentence in code; nothing echoed | No content injection; no email in a URL |
| 28 | **Navigation state, not URLs**, to carry an email between auth pages | Addresses stay out of history, logs and referrers |
| 29 | **No attacker-typed text in mail** sent to someone else's inbox (no name in a verification code email) | A stranger can't author your email |
| 30 | **One email-keyed lock** (e.g. a transaction-scoped advisory lock on a hash of the address) serialising sign-up, block and email change | No race where a blocked address gets an account, or two accounts share one |
| 31 | **Ownership generations** (`email_version`, `email_changed_at`) checked on queued mail, resets and challenges | Work queued for a previous owner of an address never reaches the next one |
| 32 | **The server picks the factor** from the locked account row | The client can't downgrade the proof |
| 33 | **Failures after proof are allowed to be specific** | Telling a code holder "this address is blocked" reveals nothing they don't control |
| 34 | **Feature-off pages are literally unknown URLs** | Config state isn't revealed |
| 35 | **Separate build for privileged UI**; fail the public build on any privileged import or string | The public bundle doesn't reveal the door |
| 36 | **Fail closed to the "unknown" answer** on infrastructure errors around hidden credentials | An outage mustn't confirm a secret |
| 37 | **Byte-for-byte test helpers and a query hook**, never a stopwatch | "Identical" becomes an assertion, not a promise |

## Accepted, documented leaks

Write these down in the security docs so nobody "fixes" them into worse ones or assumes they don't exist:

- `{mfaRequired: true}` tells a password guesser the password was right. Standard for TOTP; the second factor is the defence.
- The account-lock message tells someone with the right password (and code) that the account is locked. They proved ownership.
- Sign-up and email change with verification **off** answer 409 for a taken address. This is why verification is on in production (see `sign-up-and-verification.md`).
- A per-email 429 tells a prober that *someone* hit that address's counter in this window. It doesn't tell them whether an account exists; aligned windows and separate per-flow counters keep it at that.
- Previous-admin-key timing (a session lookup only when a cookie of the right shape is presented under the previous key) if you support key rotation.

## Common mistakes

- **Fixing the body and forgetting a channel.** The message is identical, but the 401 for a known email takes 60 ms and the unknown one 1 ms.
- **Comparing JSON key sets or parsed objects in tests.** Field order, whitespace and value differences survive. Compare bytes.
- **"Helpful" UX that branches.** "This email is already registered — sign in instead?" on the sign-up form. Every sign-up goes to the code page; the inbox says the rest.
- **A request-time per-account limit.** "At most 5 'you already have an account' emails a day" enforced in the handler returns 429 only for taken addresses.
- **Checking state before the hash.** `if user.suspended: return 403` before `verifyPassword` turns suspension into a probe and a timing oracle.
- **Revealing in the second step what the first step hid.** The code page's help text says "Check your inbox" for new addresses and "Did you block us?" for blocked ones.
