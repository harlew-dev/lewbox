---
title: Output and errors
description: Text for people, JSON for scripts, and the error codes and exit codes both use.
---

## Text and JSON

On a terminal, `lewbox` prints tables for lists and `key: value` lines for single things. Everywhere else it prints JSON:

- when you pass `--json`, or set `LEWBOX_OUTPUT=json`;
- when output goes to a pipe or a file.

```bash
lewbox node list                          # a table
lewbox node list | jq -r '.data[].name'   # JSON, automatically
```

For every command that maps to an API route, JSON output is **byte-for-byte the HTTP API's response body**, on one line, so a script can switch between the CLI and the API without changing its parsing. Lists are always `{"data": [...]}`, and `data` is never `null`. The exceptions are about your own session: `login` leaves out the token, and `whoami` adds `context` and `api_url`. CLI-only commands (`status`, `context list`, `install`, `node add`, `update`) print their own JSON objects.

Progress (checks, commands about to run, installer output) goes to **stderr**. stdout only carries the result.

## Errors

Every error from the orchestrator or a check has the same shape, from the CLI (on stderr) and the API:

```json
{"error":{"code":"invalid_request","message":"cluster \"eu-west\" failed its checks","details":[{"name":"admin_token","ok":false,"fatal":true,"detail":"whoami returned 401: check SB_IDENTITY_ENABLED=true on every node …"}]}}
```

On a terminal, the CLI prints `Error: <message>` instead, followed by any failed checks. Usage errors (exit 2) are always plain `Error: …` text, even with `--json`.

| Code | HTTP | Means |
|---|---|---|
| `bad_request` | 400 | The request can't be read: malformed JSON, trailing data, an unknown or mistyped field, an oversized body, or an unknown, repeated or malformed query parameter |
| `unauthorized` | 401 | Not signed in, or the session expired |
| `forbidden` | 403 | Signed in, but not allowed: missing permission, a suspended or pending workspace, a bad Origin |
| `not_found` | 404 | No such cluster, node or account, not yours, or a malformed ID |
| `method_not_allowed` | 405 | A known path with a method it doesn't take |
| `conflict` | 409 | The input is valid but the current state forbids it (last node, last admin, a region still being set up) |
| `invalid_request` | 422 | A parsed value breaks a rule (`details.field` names the field), or a quota is reached (no field) |
| `rate_limited` | 429 | Too many requests or sign-in attempts. `details.retry_after_seconds` (and the `Retry-After` header) says how long to wait |
| `unavailable` | 503 | A dependency is down (AerolVM, the database, S3, Cloudflare). `Retry-After` says how long to wait |
| `internal` | 500 | A bug. The body carries a `request_id`; the orchestrator's log has the cause under it |
| `precondition_failed` | — | CLI-only: a check on this machine failed. `details` lists the checks; it is never an HTTP response |

## Exit codes

| Exit | |
|---|---|
| `0` | Success, or you answered "no" to a confirmation |
| `1` | The command failed (any error above, including `rate_limited`), or `status --check` found a problem |
| `2` | Usage error: unknown flag, wrong arguments, a destructive command without `--yes` outside a terminal |
| `130` | Interrupted (Ctrl-C) |

## Confirmations

`install`, `update`, `node remove`, `cluster remove`, `user remove` and `user disable` ask before acting. `--yes` (`-y`) skips the question. Outside a terminal (including JSON mode) they **require** `--yes` and exit 2 without it, so a script can't remove things by accident.
