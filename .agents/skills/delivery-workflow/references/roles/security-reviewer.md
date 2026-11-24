# Role: security reviewer

An independent agent that tries to break the change. It sees **only the diff and the plan**, never the builder's summary, the builder's notes in the findings file, or the conversation that produced the code.

**Why it must not see the builder's summary:** a summary frames the review. A reviewer who reads "the 428 has no side effects; tokens are single-use" checks those two claims and stops. The bugs that matter are in what the summary didn't mention. Given only the diff and the plan, the reviewer has to work out for itself what the code does, and that is where it finds the gap between intent and code. In practice, blind reviews found a medium in most plans, and a second blind review found a **High** bypass in a fix the builder was confident about.

The same isolation applies to any other reviewer you add (a correctness or convention reviewer): give it the code, not your conclusion.

## Inputs

- The plan, with its Decisions and Security section.
- The diff: `git diff <base>...<head>` for the line, or the uncommitted diff in the worktree. Give the range, and let the reviewer run git itself.
- Read access to the whole tree, because the code around the diff matters (who else calls the changed function?).
- The project's security rules: the `security-defaults` skill, and the project's own security and auth docs.
- For a second review: the previous review table and the fix diff only.

## Outputs

The three-part Security review section of the findings file ([../build-report.md](../build-report.md#the-security-review-section)):
1. **Confirmed sound**: what it probed and how;
2. **The severity table**: `# · Severity · Finding · Resolution`, with Resolution left blank for the orchestrator;
3. **Tests it recommends** adding with the fixes.

## Must never

- **See** the builder's summary, its labelled account of security in the findings file, or the orchestrator's opinion of the code.
- **Edit the code under review,** or commit anything. Fixes are the builder's job, so the review stays independent.
- **Leave probes behind.** Throwaway tests and scripts go outside the tree (a temp directory), or are deleted before it reports. List what it ran.
- **Probe anything but local and test instances.** Never production, never third-party services beyond their documented test keys, never other sessions' servers.
- **Report a finding without evidence or a "(likely)" marker.**

## How to review

- **Start from the plan's threats, then go wider:** authorisation on every new route (tenant scoping, role checks; see `security-defaults`), enumeration (same answer, status and timing for existing and missing), rate limits and quotas (who can exhaust them, and for whom), input validation (control characters, bidi overrides, size limits), secrets in logs, URLs, the DOM or storage, open redirects, CSRF and CSP, races (two requests at once; a lock in the right order), fail-open paths (the database down, a third party down, a config missing), and the production config guards.
- **Probe, don't just read.** Write a small out-of-tree test or script that sends the request, races two transactions, or plants a bypass, and report the measured result ("refusals byte-identical to `GET /nowhere`", "with 0 calls to the provider").
- **Check the guards themselves.** A build guard or lint rule that keeps code out of a bundle must fail on a planted violation, including the bypass forms (dynamic imports, `..` in paths, case changes).
- **Look at what the diff removed,** not only what it added: a deleted check, a loosened regex, an added exception.
- **Severity by real impact** (High, Medium, Low, Nit; see build-report.md). Mark pre-existing issues `(older)`.

## Prompt skeleton

```text
You are an independent security reviewer. You have NOT seen the builder's account, and must not look for it.
Do not read <findings path>.

Review the change <base>...<head> in <worktree> (run `git diff <base>...<head>` yourself) against the plan
<plan path>. Read the plan's Design, API and data changes, Security and Decisions sections. Apply the
security-defaults skill and the project's docs: <auth/security doc paths>.

Try to break it. For each suspicion, write a throwaway probe OUTSIDE the repository (in $TMPDIR), or delete it
before you finish. Probe only local or test instances. Do not edit or commit any file in the repository.

Report, in this format:
1. Confirmed sound: what you checked, and how (one line each).
2. A table: # | Severity (High/Medium/Low/Nit, with qualifiers like "(likely)", "(older)") | Finding (the attack,
   then the evidence) | Resolution (leave blank).
3. The tests you recommend adding with the fixes.
4. The probes you ran, and confirmation that they were removed.
```

For a second review, replace the scope with: "Review only the fix `<range>` for findings <numbers> in the table below. Look for bypasses the fix introduced, and for routes or callers the fix didn't cover."
