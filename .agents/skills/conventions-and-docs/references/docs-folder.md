# The docs folder

Read when setting up `docs/`, adding a doc, or unsure what a doc should hold.

## Layout

```
AGENTS.md                 agent guide: stack, the doc table, non-negotiable rules, commands, etiquette
README.md                 what the product is, how to run it (points into docs/)
docs/
  README.md               the index: one row per doc, what it covers
  naming.md               every naming rule + the Say/Not vocabulary   (applies to every change)
  checks.md               which check enforces which rule, reading failures, adding a check   (applies to every change)
  testing.md              local setup, running, manual demo, failure checks
  deployment.md           production config, releasing, security checklist
  compliance.md           data map, retention, erasure, known gaps
  api/
    README.md             package layout, request flow, environment variables
    endpoints.md          every endpoint, its protection and responses
    auth.md  database.md  rate-limits.md  events.md  mail.md  …
  ui/
    README.md             folder layout, where new code goes, dependencies
    pages.md  api.md  design.md  forms-and-actions.md  testing.md  …
.agents/plans/
  features/<name>.md          open plans
  features/implemented/       shipped plans (historical record)
```

## The agent guide's doc table

`AGENTS.md` (or `CLAUDE.md`, `CONTRIBUTING.md`) holds a table: **Doc | Covers**, one row per doc, with a bold trigger on the ones that must be read before a kind of change:

| Doc | Covers |
| --- | --- |
| `docs/naming.md` | Naming for routes, controllers, permissions, events, limits, client methods, pages, declaration lists. **Applies to every change** |
| `docs/compliance.md` | Every table's personal data, purpose, retention. **Read before adding a table or storing personal data** |
| `docs/ui/forms-and-actions.md` | Where buttons and actions go. **Read before adding any button** |

The protocol stated right above the table: find every area the change touches, read each doc in full, read the plan, follow the docs, stop on disagreement, update docs in the same change.

## What a doc contains

A doc is a **short summary of how one area works and the rules it follows** — what the code alone doesn't show:

- How it works, in a paragraph and a table or flow (the request chain, the folder table).
- The rules, as bullets, with the reason where it isn't obvious.
- The names it uses and where they're checked (link to `checks.md`).
- Security rules and what they prevent.
- **Known issues and workarounds** as a table: Issue | Workaround (where).
- "Adding a …" steps for the common change in that area (adding an endpoint, a table, a limit).
- Links to the plans that shaped it.

Not in a doc: code listings that duplicate the source, tutorials for the framework, history (that's the implemented plans), aspirations (that's open plans).

Style: short sentences, tables where they compress, exact names in backticks, relative links with anchors (`[naming](naming.md#web-app)`), UK spelling (or whatever the project chose — consistently).

## Keeping docs true

- A change that makes any sentence in a doc false updates that doc **in the same change**. Search the docs for the names you touched.
- A new doc: add its row to `docs/README.md` and the agent guide's table.
- Rename or move a doc: `git mv`, then let the link check find every broken link.
- Never claim in a doc that something exists before it's implemented and tested; put it in a plan.
- When a doc and the code disagree, don't guess which is right — tell the user (SKILL.md §1).

## The link check

Run by `check`. Every relative link and anchor in tracked/staged markdown resolves; a tracked doc linking an untracked file warns (stage it or drop the link). Fix dead links; don't add them to the exception list without the user's agreement. See [writing-a-check.md](writing-a-check.md#the-docs-link-check).
