# Renaming the product

Read when the product or brand name changes (e.g. `oldname` → `newname`) and the codebase, cookies, databases and docs must follow. A symbol rename is SKILL.md §7; this is the bigger, partly irreversible job.

**Why do it fully, in one change:** a half-rename leaves two names in the tree, and the old one keeps getting copied into new code, fixtures and docs. **Why do it carefully:** a few strings that contain the old name are not branding at all, and renaming them destroys data or breaks a rolling deploy.

A rename is not a feature: no schema change, no new endpoint, no behaviour change. Write it as a plan (`delivery-workflow`) whose only visible differences are the name, the few user-facing strings below, and the sessions that end.

## 1. Inventory first

`git grep -c <old>` and count files and occurrences, then sort every hit into a group. Typical groups:

| Group | Examples |
| --- | --- |
| Display name | page titles, logo text, email sender, TOTP issuer, dev banner |
| Code identity | module path and every import, package names in manifests and lockfiles |
| Image and deployment | image name, config file paths, database role names in deploy docs |
| Databases and roles | database, role, test database names |
| Local stack | container project name and volume |
| Cookies and browser storage | session/challenge cookie names, `localStorage` keys |
| User-facing filenames | `oldname-export-<date>.json` |
| Test fixtures | `@oldname.test` addresses, literal `otpauth://totp/oldname:…` URIs, `oldname.invalid` origins |
| Outbound labels | `User-Agent: oldname-alerts`, test mailer app names |
| Assets and docs | README image, every current doc and the agent instructions file |
| **History** | implemented plans, findings, review notes |

## 2. Classify what must NEVER be renamed

Some strings contain the old name but identify a key or a lock, not the product. Find them before the mechanical rewrite and exclude them.

| Kind | Example | What a rename breaks |
| --- | --- | --- |
| **Cryptographic domain separators** (HKDF / KDF labels, HMAC context strings) | `"oldname seal v1"`, `"oldname recovery-codes v1"`, `"oldname email-pseudonyms v1"` | Every derived key changes: sealed MFA secrets no longer decrypt (every MFA user is locked out), recovery-code and emailed-code digests stop matching, keyed pseudonyms split so old and new rows no longer join. Irreversible without the old label |
| **Coordination identifiers** (advisory-lock names or prefixes, leader-election keys, queue names) | `"oldname:alerts:"`, `"oldname:metrics-rollup:"` | During a rolling deploy the old and new binaries take different locks, so a once-per-fleet job (alert evaluator, rollup) runs twice at the same time |
| **Secrets** | `APP_KEY`, route keys, SMTP credentials, `.env` secrets | Never rewrite a secret's value as part of a rename, even if it happens to contain the name |

- **ALWAYS** put a comment at each kept constant saying why it keeps the old name and what a rename would break. The next person running `git grep` must not "finish the job".
- Example: `// Keeps the old product name on purpose: renaming this label changes every derived key and locks out every MFA account.`

## 3. One source for the display name

- The display name comes from **one setting** (e.g. `APP_NAME`) read by page titles, logo text, app copy, the email sender default and the TOTP issuer. The rename then changes **defaults** (config loader, frontend fallback, dev script, container build args in every stage that needs it, local stack config, CI, the example and test env files), not per-screen copy.
- If the name is hard-coded per screen, fix that first, as its own change.
- Keep the existing casing style unless the plan decides otherwise.
- Existing authenticator entries keep working after an issuer change: only the label changes, not the secret.

## 4. State the accepted breakage up front

- **Sessions end without a fallback.** Renaming cookie names means every session, MFA challenge, pending sign-up and admin pass under the old names stops being recognised. Users sign in again; in-flight challenges restart.
- **NEVER add a dual-read shim** (accept old and new cookie names). It keeps the old name alive indefinitely and adds a second session path that must be tested and secured.
- A renamed browser storage key resets that preference once. Say so in the plan.
- **Keep every cookie protection exactly as it was**: the `__Host-` / `__Secure-` prefixes, `HttpOnly`, `Secure`, `SameSite`, `Path`, and their production/development branching. Update the tests that assert them; don't delete them.

## 5. Databases and roles: `ALTER`, never recreate

- Rename in place, with the old stack stopped, from a maintenance connection:

  ```sql
  ALTER DATABASE oldname RENAME TO newname;
  ALTER DATABASE oldname_test RENAME TO newname_test;
  ALTER ROLE oldname RENAME TO newname;   -- grants and ownership follow the role
  ```

  Then change the connection string in one coordinated change, and confirm with the migration status command and the test suite.
- Why not recreate: dropping and recreating loses data and grants, and a half-done recreate leaves the app pointing at an empty database or an old volume.
- Container volumes usually can't be renamed: copy the old volume's files into the new one with a throwaway container. **Recreating the local volume destroys the dev database and needs the user's explicit approval**; it's a fallback, never the default.
- Give the local container stack an explicit project name in its config, so the project and volume names stop following the checkout folder (a later folder rename would otherwise silently start a new empty volume).
- No migration changes: database and role names are deployment identifiers, not schema. If a table or column contains the product name, that's a separate expand/contract change (`production-hardening`).

## 6. Code and package names

- Module path: rewrite every import in the same commit (mechanical, tool-driven).
- Package manifest: change the name, then regenerate only the lockfile (`npm install --package-lock-only`, `cargo update -w`, the equivalent) and **check the lockfile diff is only the name**. A full install that moves resolved versions doesn't belong in a rename.
- Prefer keeping generic binary names (`api`); rename the image only if docs use it as the command.

## 7. History stays untouched

- **NEVER rewrite implemented plans, findings or review notes.** They record what happened under the name it had; rewriting falsifies the record and breaks links to commits.
- The rename plan itself explains why old strings remain in those files.

## 8. Verify with a `git grep` allowlist review

1. `git grep -n <old>` after the rewrite. The output may contain **only** the classified keep-list (§2) and history (§7). Paste or summarise it in the review; every other hit is a miss.
2. Run the normal checks: tests, static checks, the build (the web build is regenerated, so CSP hashes recompute on their own), end-to-end.
3. Migration status after the database rename.
4. Manual: sign-in, MFA enrolment, admin sign-in, the export filename, the reset preference, one rendered email.
5. **No test is added just to assert the new name.** Update the existing assertions (cookie names, export filename, fixtures, mail text); they are the coverage.

## 9. Out of scope (leave to the user)

Renaming the repository, remote, checkout folder, worktrees and branches; new artwork, domains and production email addresses. List them in the plan as the user's steps.
