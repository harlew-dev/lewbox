# Incident response

A runbook for the person on call when something has gone wrong: an attack, a leak, an outage or a mistake. It should use only tools the app and CLI already have. Where a step needs something that isn't built, the runbook says so, and that becomes a backlog item. Keep the project's own copy in the docs (e.g. `docs/incident-response.md`), with command lines filled in for your deployment.

**Write down the time you became aware.** GDPR's 72-hour clock starts then, not when the incident started.

## 1. Detect

Signals that something is wrong:

- **the external uptime check:** the app or the database is down;
- **an alert or dashboard tile** at warn or critical (security, rate limits, 5xx, integrity);
- **the heartbeat service:** the alert loop itself stopped;
- **the logs:**
  - an admin-door refusal from outside the allowlist (the admin key leaked);
  - a burst of 5xx or slow queries;
  - "audit lines are not written" at startup;
- **a report** from a user, a security researcher, or your hosting, SMTP or CDN provider.

## 2. Triage

Work out **what** is happening and **to whom** before doing more than the obvious.

- **Dashboard security views:** failed sign-ins and from where, credential stuffing (many emails from one network), reset and sign-up floods, admin-door activity, possible takeovers (a sign-in from a new network followed by a credential change).
- **The event viewer:** filter by IP or range, account, tenant, email and time. Use the failed-checks filter. An event's **request ID** finds its log lines and the other events of the same request.
- **The logs:** search the request ID from a 500 or an event. Access lines name route, status, user and IP.
  - **If the database itself may have been altered,** trust the **audit lines shipped off-host**. An audit line whose row is missing was deleted, unless its request's access line shows an error (a rollback).
- **Grouped errors:** open groups by last seen, with their last request ID.
- **Who is affected:** user and tenant pages, and an account's activity.

## 3. Contain

Stop it getting worse. Console actions need the operator role, a fresh MFA code and a reason, and are audited. CLI commands preview without `--confirm`.

| Situation | Do |
| --- | --- |
| An account taken over, or abusing others | **Suspend** it (ends every session, revokes its invitations, refuses sign-in). Then **reset MFA** if the attacker enrolled their own authenticator, but only after confirming the owner's identity out of band |
| An account's sessions may be stolen; the owner is fine | **Sign out everywhere** for that account |
| One network attacking | **Block** the address or range (bounded, up to 30 days). For a flood, block at the edge (WAF, CDN, load balancer) |
| A tenant sending abusive invitations | **Revoke its pending invitations**, and suspend the sender |
| Sessions may have leaked in general (a log or backup exposed, a bug) | **Revoke all sessions** (CLI): every app and admin session and every pending sign-in |
| An admin account compromised | **Revoke admin** (CLI), sign out their sessions, then treat it as a taken-over account |
| The admin path key exposed | **Rotate outright** (no previous slot); set the IP allowlist if it isn't set |
| An API key leaked | Revoke it. Password, MFA and session resets do **not** revoke API keys, so review keys separately |
| SMTP password, metrics token, DB password or provider token exposed | Rotate at the provider and in the secret store, then restart |
| The master key exposed | If it can't be rotated, treat every sealed secret as exposed: MFA users must re-enrol, keyed pseudonyms are reversible by guessing, sealed receiver URLs are readable. Plan the rotation |
| A bad release | **Roll the code back** to the previous image. Don't run down migrations in production; fix forward |

### Preserve evidence

- **Pause destructive scheduled jobs** (prune, pseudonymise/minimise) until scoping is done. Pseudonymising cuts IPs and replaces addresses, and pruning deletes old events. Both are permanent, so a run mid-incident destroys the detail you're scoping with. Record in the breach register when and why you paused them. Turn them back on once you've exported what you need.
- **Don't restore over the database** or delete anything while you're still scoping. Snapshot it first if you must change it.
- **Export now** (next step), before retention catches up.
- **Keep exports** with the incident record, not in a shared folder. They contain personal data.

## 4. Scope

Answer five questions: **what data, whose, over what time, by whom, and is it still happening?**

- **Event CSV export:** set the window and filters (IPs, accounts, event names) and download it. The export is capped (e.g. 10,000 rows, newest first), so narrow the window and export several times if needed. Each export is itself audited.
- **Who was affected:** collect the accounts and tenants from the export. For address-only events (attempts, invitations), use the address. After 30 days it's pseudonymised, but searching by the address still matches.
- **What was read:**
  - **admin reads are all recorded**, with filters and searches;
  - **ordinary users' reads are not**, only their changes. Assume an attacker in an account saw everything its role could see;
  - key-authenticated API reads may not be audited individually, so scope by the key's grants.
- **Logs** cover what events don't (reads, refused requests), by IP and account, within their 30 days.

## 5. Notify

Under GDPR, a **personal-data breach** is personal data destroyed, lost, altered, disclosed or accessed without authorisation.

- **Notify the supervisory authority within 72 hours** of becoming aware (art. 33), unless the breach is unlikely to risk people's rights. If you're late, give the reasons. If you don't know everything yet, notify what you know and add to it in phases.
- **Notify the people affected without undue delay** when the risk to them is likely to be **high** (art. 34). Use plain words: what happened, what it means for them, what you've done, and what they should do.
- **Processors notify the controller** without undue delay. When your provider tells you of a breach, your clock starts then.
- **Check customers' contracts** (DPAs), which often set shorter deadlines (24–48 h). Other regimes (US state laws, sector rules) have their own timelines.

**Template: to the authority** (each authority has its own form; it asks for this):

> On [date, time, timezone] we became aware that [what happened, e.g. "an attacker signed in to 14 accounts using passwords leaked from another service"] between [start] and [end]. Affected: about [number] people ([who: customers, their invitees]) and [number] records of [kinds of data: names, email addresses, IP addresses, project names]. Likely consequences: [e.g. "the attacker could read those accounts' tenants"]. Measures taken: [suspended the accounts, revoked their sessions, blocked the addresses, …]. Measures for those affected: [asked them to set a new password and turn on two-factor authentication, …]. Contact: [name, email, phone].

**Template: to the people affected:**

> Subject: Security incident affecting your [app] account
>
> On [date] we found that [what happened, in one sentence]. This affected your account: [what of theirs was involved]. We have [what you did: signed out every session, …]. Please [what they should do: set a new password, turn on two-factor authentication, be wary of emails asking for …]. We're sorry. If you have questions, reply to this email or write to [contact].

**Don't put details an attacker could use** (which defence failed, and how) in a public notice. Send notices through your normal mailer, so the suppression list still applies, and don't add tracking.

## 6. Record

Put **every** breach in the organisation's **breach register** (GDPR art. 33(5)), **including ones not notified**:

- the facts, effects and actions taken;
- why it was or wasn't notified;
- the event exports, relevant audit lines and log excerpts;
- the timeline (aware, contained, notified);
- each decision, with who made it.

The app doesn't hold the register. The organisation does.

## 7. Blameless review

Within two weeks, without blame (people acted reasonably on what they knew):

- **What would have caught it sooner?** A signal or threshold, an alert, a rate limit or quota, an integrity check.
- **What made it worse or slower?** A missing action or tool, a step the runbook didn't have, access people needed and didn't have (or had and shouldn't).
- **What changes?** One owner and one plan per fix, the runbook updated, and new known gaps written down.

## Make the runbook work before you need it

- **Rehearse:** a tabletop exercise each quarter. Restore a backup into staging. Stop the servers and confirm the heartbeat pages someone.
- **Keep the contacts current:** the on-call rota; the supervisory authority's form; provider security contacts (hosting, SMTP, CDN); legal and DPO.
- **Pre-build the containment tools** this runbook names (suspend, sign out everywhere, revoke all sessions, IP block, key rotation, event export) before launch. During an incident is the wrong time to write SQL by hand.
