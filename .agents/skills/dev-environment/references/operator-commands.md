# Operator and destructive commands

Read this when you write or change a CLI that migrates, seeds, prunes, administers or otherwise changes data, or a task-runner target that calls one. This file owns the local mechanics and the CLI contract. `production-hardening` owns the production side (`references/deployment.md` "Destructive-command guards", `references/admin-and-privileged-actions.md`: reasons, audit events, roles, break-glass). Seeder behaviour is `testing`'s (`references/backend-integration.md#seeders-development-data`).

## The contract

Every command that changes or deletes data:

1. **Previews unless confirmed.** Without `-confirm` (`CONFIRM=1` through the runner) it prints exactly what it would do and exits 0, changing nothing: `events: would delete 12408 older than 2026-08-19 (90d); kept 3 with pending deliveries`. With confirmation it prints the same line in the past tense.
2. **Refuses production where the action makes no sense there**, from the loaded config, before connecting: `migrate fresh` and `seed` refuse `APP_ENV=production`. Commands meant for production (`prune` from cron, `admin`) are allowed there and still preview by default.
3. **Needs a target where scope matters.** A bare `prune` previews every target at its default age **and never deletes, even with `CONFIRM=1`**. Deleting needs `target=` (and optionally an age), so one typo can't empty every table.
4. **Validates strictly:** typed flags with ranges (`-count` 0..max), allowed formats only (`-older-than` accepts `<n>d` or `<n>h`, at least one hour, only with a target), an unknown target or seeder refused with the list of valid ones.
5. **Exits non-zero on any failure** (bad input, database error, refused environment), so scripts and cron notice.
6. **Bounds its run time** with a per-command timeout and handles Ctrl-C by cancelling cleanly (the transaction rolls back).
7. **Prints a usage text** when run bare (if it has subcommands) or with `-h`, listing every subcommand and flag.

## Typical commands

| Command | Preview without confirm | Production |
| --- | --- | --- |
| `migrate` / `migrate status` | n/a (additive / read-only) | allowed |
| `migrate rollback` | lists the down SQL it would run | allowed, but fix forward instead (`production-hardening`) |
| `migrate fresh` | lists the tables it would drop | **refused** |
| `migration name=…` | n/a (writes an empty up/down pair) | n/a |
| `seed [only=…] [count=n]` | n/a (insert-only, never deletes) | **refused** |
| `prune [target=… older-than=…]` | counts per target | allowed |
| `admin cmd=…` (grant, revoke, suspend…) | describes the change | allowed |
| `app-key` | n/a (never replaces an existing key) | n/a |

## Locally

- **Allowed without asking:** `make migrate-fresh CONFIRM=1 && make seed` on *your own* dev database while migrations are squashed in development, and anything the e2e scripts do to `*_test_<port>` databases.
- **Everything else destructive needs the user's explicit authorisation:** rolling back migrations, dropping or resetting any other database, deleting real data (`conventions-and-docs`, `references/agent-etiquette.md`).
- **The `.env` trap:** the task runner loads `.env`, so `make prune` always targets `.env`'s `DATABASE_URL`, whatever you export in front of it. To act on another database, run the CLI directly with the environment you mean, and read the preview's target line before confirming.
- After pulling a squashed schema change, the documented reset is `make migrate-fresh CONFIRM=1 && make seed`. Put that sentence in the testing doc, so nobody improvises a `DROP DATABASE`.

## Common mistakes

- `--force` that skips the preview **and** the production check. Confirmation and environment refusal are separate gates.
- A preview that runs a different code path from the real action and drifts. Compute the plan once, print it, and execute that plan only when confirmed.
- A command that prints nothing on success. The confirmation line is the operator's record.
- A bare binary that ignores unknown arguments and starts the server (`./bin/app migrate` serving traffic against the target database). Refuse unknown arguments, or document loudly that only the container image installs the subcommands.
