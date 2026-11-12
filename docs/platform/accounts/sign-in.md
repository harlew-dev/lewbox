---
title: Sign in
description: Sign in to an orchestrator from any machine, switch between orchestrators with contexts, and use Lewbox from scripts and CI.
---

## Sign in

```bash
lewbox login https://lewbox.example.com
```

It asks for your email and password, then saves the session as a **context** and makes it current. Every later command goes to that orchestrator.

```bash
lewbox whoami
```

```text
api_url: https://lewbox.example.com
context: lewbox.example.com
user   : you@example.com (instance admin)
via    : session
```

Sessions last 30 days. `lewbox logout` ends the session and deletes the saved token; `lewbox logout --all` does it for every context.

## In a browser

The orchestrator also has a web console. Open `http://your-host:8080/` and sign in with the same email and password:

```text
http://your-host:8080/
```

The browser gets its own session cookie, separate from the CLI's saved session. It lasts 7 days at most and ends after 12 hours idle, so you may need to sign in again sooner there. See [Use the web console](../getting-started/web-console.md).

## Contexts

A context is one orchestrator you've signed in to. Its name defaults to the URL's host; choose one with `--context-name`.

```bash
lewbox context list
```

```text
NAME                API_URL                         CURRENT
local               -
lewbox.example.com  https://lewbox.example.com      *
staging             https://staging.example.com
```

```bash
lewbox context use staging                  # switch
lewbox --context local node list            # one command, another context
lewbox context remove staging               # forget it and its token
```

`local` always exists: it opens this machine's own orchestrator database directly, with no sign-in. That's for the machine that runs `lewbox serve`.

## Which orchestrator a command uses

The first of these that's set wins:

1. `--context <name>`, or `LEWBOX_CONTEXT`;
2. `--api-url <url>`, or `LEWBOX_API_URL`, with a token from `LEWBOX_TOKEN` or from a saved context for that URL;
3. the current context;
4. `local`.

`lewbox status` shows which one is in use, and whether it's healthy:

```bash
lewbox status          # --check exits 1 when not healthy or not signed in
```

## Scripts and CI

Never put a password on the command line. Pass it on stdin:

```bash
printf '%s\n' "$LEWBOX_PASSWORD" | lewbox login https://lewbox.example.com \
  --email ci@example.com --password-stdin --context-name ci
```

Or, with a session token you already have, skip `login` entirely:

```bash
export LEWBOX_API_URL=https://lewbox.example.com
export LEWBOX_TOKEN=lbs_…
lewbox node list --json
```

Output is JSON whenever it isn't going to a terminal, so piping into `jq` just works. See [Output and errors](../reference/output-and-errors.md).

An organization's API key (`lba_…`) works the same way for that organization's routes; see [Organizations and API keys](organizations-and-api-keys.md). A key can't reach clusters, nodes or accounts, so a job that manages those needs a dedicated account. Give it instance admin only if the job changes things.

## Where it's stored

| File | Holds |
|---|---|
| `~/.config/lewbox/config.json` | Contexts and which one is current |
| `~/.config/lewbox/credentials.json` | Session tokens |

Both are written atomically, mode `0600`. `$XDG_CONFIG_HOME` moves them.

## Next

- [Manage accounts](manage-accounts.md)
- [Configuration reference](../reference/configuration.md)
