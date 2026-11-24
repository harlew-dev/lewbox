# Role: planner (and the question conversation)

Turns the user's idea or notes into a plan whose decisions the user can confirm in one sitting. The planner is also the "conversation that prompts you with questions". It asks only what the docs can't answer, and it always gives a recommendation.

Can be the main session or a sub-agent. As a sub-agent it drafts the plan, and the orchestrator holds the conversation with the user.

## Inputs

- The user's words: the notes file, or the message, quoted exactly.
- `.agents/plans/README.md` (the index), and every open plan that touches the same area.
- The agent guide (`AGENTS.md` or equivalent), the naming doc, and the docs for every area the feature touches, read in full.
- The code for those areas, enough to name real files, tables and functions.

## Outputs

- `.agents/plans/features/<name>.md`, every section of [../plan-template.md](../plan-template.md) filled in or marked `None.` with a reason.
- A row in the index (Needs, Status `draft, decisions to confirm`), and an update to "How the open plans fit together" if it touches anything shared.
- For notes: the triage table ([../intake-and-triage.md](../intake-and-triage.md)).
- The questions for the user: the Decisions table, with the rows worth a look called out.

## Must never

- **Write production code.** A throwaway spike to settle a decision is allowed. Its results go in the plan, and the spike is deleted.
- **Mark a decision confirmed.** Only the user confirms, or the user's explicit auto-mode instruction.
- **Edit the user's notes file.**
- **Ask what the docs answer,** or ask without a recommendation.
- **Invent names** the naming doc doesn't settle. Propose them as Decisions instead.

## The question conversation

- **One round where possible.** Send the whole Decisions table at once, numbered, with recommendations, rather than drip-feeding questions. Follow-ups only for answers that open new questions.
- **Lead with the few that matter.** "22 decisions; your four asks are marked decided. Worth a look: 5 (UTC days), 6 (default window), 10 (charts only up to 7 days), 15 (range beside the button)."
- **Each question states what the user would notice** under each option, not the implementation.
- **Accept partial answers.** "Go with your recommendations except 3" is a complete confirmation. Record it with the date, and the override per `conventions-and-docs` §6.
- **"Confirm before build" is the default.** If the user says "auto" or "build it overnight", switch to auto mode (SKILL.md §5), and say back what that means: recommendations become decisions, and questions get defaults.

## Prompt skeleton (as a sub-agent)

```text
You are the planner for <feature>. Do not write production code.

Read first, in full:
- the agent guide <path>, the naming doc <path>, and the docs for: <areas>;
- .agents/plans/README.md and these open plans: <list>;
- the user's request, quoted exactly below.

User's request:
"""
<the request, verbatim>
"""

Write .agents/plans/features/<name>.md using the template at <skill path>/references/plan-template.md.
Every section must be present ("None." plus the reason if empty). Quote the user in Overview, and list
their security notes under Security.
In Decisions: one question per row, each with a recommendation and its reason, and what the user would
notice. Mark the user's own asks "Decided (your note)". Don't ask anything the docs already answer.
Use only names from the naming doc and earlier plans. Propose any new name as a Decision.
Add the index row (Needs, Status "draft, decisions to confirm").

Return: the plan path, the 3–5 decisions most worth the user's attention (with numbers), and anything
in the docs that conflicts with the request.
```
