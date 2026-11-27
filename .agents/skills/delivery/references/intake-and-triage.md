# Intake: turning the user's raw notes into work

Read when the user hands over a notes file, a voice-to-text dump, a list of ideas, or a single idea "for a plan".

Raw notes are written fast and for the writer. They mix one-line UI tweaks with whole features, they use names that don't exist in the code, and they sometimes contradict written rules. Building them literally gets the wrong thing built. Ignoring part of them breaks trust. **Every note ends up somewhere visible, and the user can see where.**

## The rules

1. **NEVER edit the user's notes file.** It is their scratchpad and their record of what they asked for. Triage output goes into plans, findings and `read/` files. Don't tick items off, reorder them or "tidy" them.
2. **Split before you sort.** One paragraph often holds several asks. "Improve the events page, add graphs, multi-select filter with groups, date range in one dropdown, a Filters dialog with badges" is five items. Each gets its own line in the triage.
3. **Route each item to exactly one destination:**

| Destination | When | Example |
| --- | --- | --- |
| **Notes batch** (no plan) | A small fix or change within the existing design. No new table, endpoint family, dependency or security change | "Make 'Forgot password' sit under the password input"; "remove Refresh buttons" |
| **New plan** | Anything that needs a plan (`conventions` §5) | "Terms of service and privacy policy from Markdown files, an env setting, a checkbox on sign-up" |
| **Existing open plan** | It extends or changes a plan not yet built | A filter request for a page whose redesign plan is still a draft |
| **A future plan, or "Later, not planned yet"** | A direction, not a feature for now | "Getting started progress card in the sidebar" |
| **A question back** | It can't be routed without the user | Two readings that lead to different builds |
| **Declined, with the reason** | It conflicts with a security rule and can't be adapted | Rare. Prefer adapting (rule 5) |

4. **"Make a plan for X" is not "build X".** Write the plan and stop at its Decisions. Build it only when the user asks for it to be built, or puts the run in auto mode. If an auto-mode instruction ("build every plan") sweeps it in, say so in `read/questions.md`: "You first asked for a plan only; it's now built as recommended, since you asked for every plan."
5. **A note that conflicts with a written rule is adapted to the rule, and you say so.** Record it under "Where a note was adapted to a written rule" in the findings file. Examples:
   - The note "a modal: resend, plus 'your email might be blocked'". The rule: never reveal that an address is blocked (that would allow enumeration; see `security`). Built: the same text for **everyone**, never only for blocked addresses.
   - The note "a [Resend] button". The rule: button labels say what happens. Built: **Resend code**.
   - The note "remove refresh buttons". The rule: a failed load offers Retry. Built: Refresh removed, Retry on failure kept.
6. **An ambiguous note gets the reading most consistent with the docs, and an explicit check.** Before building (or, in auto mode, after building with a default), state your reading and ask "tell me if you only wanted X". Example: "admin logout button not danger variant" could mean "check it isn't", "change admin's only", or "change every account menu". Sign out isn't destructive, so the rule changed for every menu, and the question asked "only admin's?". Record it under "Corrected after your notes were misread" when the first reading turns out wrong.
7. **Names in notes are mapped to real names.** A note's example ("event groups like `admin.read`") may name things that don't exist. Build with the real names (`auth.*`, `auth.mfa.*`) and say so in the plan's Decisions.
8. **"Not sure if other pages have the same issue, take a look"** is a request to sweep. Do the sweep, list every place you checked and changed, and give the list in the findings.
9. **A note that's really a bug report** (a NUL byte answering 500) gets a small findings file of its own (What · Why it matters · Suggested fix), then joins the notes batch or becomes a question.

## The notes batch

Small items are built together, on one line (often their own worktree), but **one commit per note**. Then any one of them can be reverted without the others, and the findings table maps each note to its commit:

| Note | Commit | What changed | Screenshot |
| --- | --- | --- | --- |

The batch still goes through the full build loop: the orchestrator's diff check, the blind security review (small UI changes have leaked before: a Back link could be an open redirect, and a "copied" tooltip could put a secret in the DOM), checks, and screenshots for every visible change.

## The triage message (or file)

Before any building, show the user the routing, as a table:

```markdown
| # | Your note (quoted, shortened) | Becomes | Notes |
| --- | --- | --- | --- |
| 1 | "email block inside a modal…" | notes batch | Same text for everyone (rule: never reveal a block) |
| 2 | "a plan for terms of service & privacy policy…" | new plan: policies (plan only) | 19 decisions to confirm |
| 3 | "improve UI on events page…" (5 asks) | new plan: events-page | Your 4 asks marked decided; 18 more decisions |
| 4 | "admin logout button not danger variant" | notes batch | Read as: make Sign out plain everywhere. Only admin's? |
```

In auto mode, this table goes at the top of `read/questions.md` instead of being sent as a message, and the run continues.
