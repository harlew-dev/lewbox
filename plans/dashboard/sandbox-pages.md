# Dashboard: sandbox pages

> Status: Draft · Milestone: Alpha (M2, conversations M3) · Updated: 2026-10-29

Pages for a project's sandboxes: the list, creating one, and everything about one sandbox: its overview, operations, activity, terminal, sessions, files, ports, SSH keys and conversations. These are the pages people spend the most time in, and the ones whose components Lewagent shares. Behaviour belongs to [sandboxes](../sandboxes.md), [inside-a-sandbox](../inside-a-sandbox.md) and [agents](../agents.md).

## Today

| Legacy page or component (`lewbox-legacy/resources/js/`) | What it does | Lines |
|---|---|---|
| `pages/tenants/Sandboxes.svelte` | List across profiles | 113 |
| `components/CreateSandboxDialog.svelte` | Create for any profile: template or image, cluster (warns when no key of the profile reaches it), size and timers (defaults or the profile's limit), repositories with a branch picker (`RepositoryBranchSelect.svelte`), env, metadata | 524 |
| `components/SandboxActions.svelte` | Start, stop, restart, delete | 162 |
| `pages/tenants/sandboxes/Show.svelte` | Overview: details, metadata, setup report, repositories, operations with step times, activity | 576 |
| `pages/tenants/sandboxes/Terminal.svelte`, `components/SandboxTerminal.svelte` | xterm.js login shell over the gateway's WebSocket | 56, 255 |
| `pages/tenants/sandboxes/Sessions.svelte`, `sessions/Show.svelte` | Sessions; a session's log and attach | 208, 285 |
| `components/RunCommandDialog.svelte` | Run one command, see its output | 149 |
| `pages/tenants/sandboxes/Files.svelte` | Browse, upload, download, rename, mkdir, delete; "only reached while it runs" | 404 |
| `pages/tenants/sandboxes/Ports.svelte` | Expose (port, protocol, access, persist), visit, new link for private ports, close | 312 |
| `pages/tenants/sandboxes/SshKeys.svelte` | The sandbox's own SSH keys; generate in the browser | 426 |
| `pages/tenants/sandboxes/Conversations.svelte`, `conversations/Show.svelte` | Conversations; the chat | 168, 439 |
| `components/conversation/*` | `ConversationWindow`, `ConversationHistory`, `ConversationChunk`, `ToolCall`, `UserMessage`, `Markdown`, `CodeBlock`, `PromptBox`, `ModelPicker`, `DirectoryPicker`, `PendingRequest(s)`, `QueuedPrompts`, `WorkingIndicator`, `AgentIcon` | ~1,900 |
| `lib/conversation.ts` | `mergeEvent` (events → chunks), `promptText`, `agentOptions`, `summarizeToolCall` | 430 |
| `lib/gateway.ts` | Calls the gateway with a sandbox key | 313 |

Lewagent's chat is further along (`lewagent-legacy/resources/js/lib/agents.ts`, 1282 lines; `components/chat-*.tsx`): it follows a turn through drops and quiet periods (reconnects after 20 s of silence, gives up after 3 reconnects without progress, then reads the conversation's status), handles queued and steered turns, background tasks, attachments, slash commands, usage and cost, and the turns an agent starts itself. The docs' [building a chat page](https://lewbox.com/docs/agents/building-a-chat-page) describes the same model.

Problems: every inside-a-sandbox page needs a sandbox key, minted by the app and renewed by the page; status changes need a reload; the chat logic exists twice (Svelte and React) and drifts.

## Goals

- Everything a person might do to a sandbox, without the CLI.
- Live everything: status, operation steps, sessions ending, ports changing, activity, conversations.
- **One chat implementation**, in React, shared with Lewagent, built on the TypeScript SDK's event handling.
- Pages that don't break when the sandbox isn't running: they say so and offer Start.

## Design

### List

`/p/{project}/sandboxes`:

- **Columns:** status (with the operation's step while one runs, e.g. "stopping · snapshotted"), id, profile, template or image, cluster, isolation, size (vCPU · memory · disk), persist, timers, metadata (first two pairs, then "+3"), created. Column visibility is saved per user.
- **Filters** in the URL: status (default: everything but deleted), cluster, profile, template, metadata (`key=value`, several), and a text search over id and metadata values.
- **Bulk actions** on selected rows: Stop, Start, Delete, with a summary of what will happen to each (non-persisted ones are deleted by a stop).
- Rows update live; new sandboxes appear at the top with a highlight.

### Creating a sandbox

"New sandbox" opens a dialog (a page on narrow screens), replacing `CreateSandboxDialog.svelte` field for field ([sandboxes/lifecycle](../sandboxes/lifecycle.md)). Fields owned by plugins (a sandbox field a plugin registers) appear in its advanced section through the `sandbox.create.fields` slot, and only when the plugin is on ([plugins/dashboard-slots](../plugins/dashboard-slots.md#slots)):

1. **Profile.** Its readiness shows underneath (agents, Git), from [profile-pages](profile-pages.md#a-profile).
2. **Template or image.** A template picker shows only templates usable on the chosen cluster, others disabled with the `reason`; or a public image.
3. **Cluster**, defaulting to the profile's. Warns, as legacy does, when none of the profile's keys reach it.
4. **Isolation:** the levels the cluster offers that the profile may use (`container`, `gvisor`, `microvm`), the default preselected, each with a line on what it isolates and what it can't do there (e.g. "microVM: can't persist yet"), others shown disabled with why ([clusters/runtimes](../clusters/runtimes.md#choosing-a-sandboxs-level)).
5. **Size and timers:** cpu, memory, disk, idle timeout, lifetime. Placeholders show the default or the profile's limit where lower; values over a limit are refused inline with the limit named.
6. **Persist**, with what it means; disabled with the reason at a level that can't snapshot.
7. **Repositories:** up to 20 rows of repository (picker over the profile's GitHub repositories, or `owner/name`, or a URL), ref (branch picker), new branch, path, and **Git access** for GitHub repositories (*Default*, *Read*, *Write*, *Full*, *Custom…* with a permission picker). When GitHub can't list repositories, or the profile has no token, the same messages as legacy explain why and allow typing.
7. **Git access** for the sandbox: *Full* (default), *Write*, *Read* or *Custom…*, used by every repository on *Default*. Options above the project's cap are disabled, naming the cap. *Write* and *Full* show a short warning: anything in the sandbox, an agent or a package it installs, can push to these repositories, and with *Full* change their workflows ([git/tokens](../git/tokens.md#access-levels)).
8. **Environment** (write-only) and **metadata**.
9. **Advanced: hooks and scrub** (collapsed; shown to those with `sandboxes.exec`) ([sandboxes/hooks](../sandboxes/hooks.md)):
   - three lists, **On create**, **On start** and **On stop**, each row a command (a small CodeMirror shell editor) with label, directory, timeout and, for create and start, *Run in background* and *Required*; rows reordered by dragging; the template's hooks shown above, read-only, marked *From template*;
   - **Stop grace** (seconds, default 60, at most 600), with the line "On-stop hooks share this, and the sandbox keeps running, unbilled, while they do";
   - **Remove before saving** (`scrub`): paths, one per row, validated inline (absolute or `~/`, refused paths named), with the line "Deleted on the node before every snapshot; gone when the sandbox starts again";
   - **Idle warning** (`warn_before`, default 5 minutes, *Off*).
10. **Wait:** the dialog closes at once and the new sandbox's page shows the create operation's steps live.

The request carries an idempotency key made when the dialog opened, so a double click can't create two.

### Overview

`/p/{project}/sandboxes/{sbx}`. Plugins add cards here (`sandbox.summary.cards`: the repositories and their Git access from `git/github`, this run's cost from `credits`) and tabs beside the core's (`sandbox.tabs`):

- **Resource header** ([navigation-and-layout](navigation-and-layout.md#breadcrumbs-and-resource-headers)): status, id, cluster, node, size, profile, template or image and its build, persist. Actions: Start, Stop, Restart, Delete; `⋯`: Run command, Create sandbox key (with permissions, shown once, for testing a browser integration), Copy id, Edit settings.
- **Operation in progress:** a stepper of its steps (`created`, `running`, `setup_applied`, `repositories_cloned`, `ports_exposed`, `hooks_ran`; or `timers_disarmed`, `stop_hooks_ran`, `harvested`, `setup_cleaned`, `stopped`, `scrubbed`, `snapshotted`, `uploaded`, `removed`) with times, live; while hooks run, the one running is named. A failed operation shows its `error`, `error_code` and what's left ([sandboxes/operations](../sandboxes/operations.md)).
- **Settings:** idle timeout and lifetime (with when each will stop it: "idle 4 m of 30 m", "stops at 18:40"), metadata (edit inline), environment names only.
- **Idle warning:** after `sandbox.idle_warning`, a banner across the page, "Stopping in 4 minutes as it's idle", with **Keep running** (`POST …/touch`) ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop)).
- **Hooks:** each hook by list (template's first, marked), with its last run: status dot, exit code, duration, when, the operation, and the redacted stderr tail of a failure behind *Show output*; **Run again** (sends the hook's command as a command, labelled `hook.<label>`), and **Edit** (hooks, stop grace, scrub paths) for those with `sandboxes.update` and `sandboxes.exec`. A stop by the node's backstop is called out ("Stopped by its node while Lewbox was unreachable; hooks ran there") ([sandboxes/hooks](../sandboxes/hooks.md#the-report)).
- **Setup:** each part's status (`ready`, `configured`, `failed`, `not_configured`, `not_applicable`) with its message, and when it was applied ([sandboxes/profile-setup](../sandboxes/profile-setup.md)).
- **Repositories:** each clone's name, ref, branch, path and status, with git's message when it failed.
- **Git:** the sandbox's access and each repository's (with its effective permissions on hover), whether the project's cap lowered it, each token's account and expiry, and Edit (`PUT …/git`), for those with `sandboxes.git`.
- **Ports:** a summary with links, and "Manage".
- **Recent activity:** the last 10 events, problems highlighted.

### Operations

`…/{sbx}/operations`: every operation on the sandbox, newest first: kind, status, who asked (a user, a key, or Lewbox with a reason such as `idle_timeout` or `credits_exhausted`), duration, step timings as a small waterfall, error. `…/operations/{op}` shows one in full.

### Activity

`…/{sbx}/activity` ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)):

- A timeline, newest first, live. Each event shows its kind as a short sentence ("Ran `git status` · exit 0 · 120 ms", "Refused `POST /commands`: sandbox_not_running", "Port 3000 exposed, private"), its actor, label, outcome, and request id. Expanding shows `details` and, for a failed command, the redacted stderr tail.
- **Filters** in the URL: group (operations, commands, requests, sessions, ports, files, keys, SSH keys, agents, app), outcome (succeeded, failed, refused, problems), kind, label (with `*`), text, time range, actor, conversation.
- `app.*` events from clients are marked as the client's claim (`source: client`).

### Terminal

`…/{sbx}/terminal`: a login shell in an xterm.js terminal, as a [session](../inside-a-sandbox/sessions.md) with a PTY, so it survives the page closing.

- Opening the tab starts a new session (`pty: true`, cols and rows from the terminal's size) and attaches over WebSocket. `?session=ses_…` reattaches to an existing one, replaying its buffer first.
- **Tabs** within the terminal page for several shells; each is a session, listed with its directory and start time. Closing a tab detaches; "End" kills the session.
- Resizes are sent as they happen (debounced 100 ms). Copy on select, paste with the browser's shortcuts, search (`⌘F`), clickable links, and a font-size control.
- If the socket drops (sleep, network), the terminal shows "Reconnecting…" and reattaches with the replay; if the sandbox stopped, it says so and offers Start.
- The WebSocket authenticates with the session cookie and a one-time ticket ([stack-and-structure](stack-and-structure.md#authentication)).
- The terminal component is shared ([stack-and-structure](stack-and-structure.md#shared-components)); Lewagent's terminal panel uses the same one.

### Sessions

`…/{sbx}/sessions` and `/{ses}` ([inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md)):

- **List:** id, command (or "login shell"), directory, PTY, watched, status (running, exited with code, killed, lost), started, ended. Filter by status. "New session" with command, directory, environment, PTY.
- **A session:** for a running session with a PTY, an attached terminal; without a PTY, its log following live. For an ended one, its log (last 1 MiB; "Tail" for the last N lines) and how it ended. Actions: Signal (INT, TERM, KILL, HUP, QUIT), Resize, Kill, Download log.

### Run a command

From the header's `⋯` or `.` as a shortcut: a dialog with command, directory, environment, timeout and label. Output streams in as it runs (SSE), then shows the exit code and duration. Replaces `RunCommandDialog.svelte` ([inside-a-sandbox/commands](../inside-a-sandbox/commands.md)).

### Files

`…/{sbx}/files?path=/root/workspace` ([inside-a-sandbox/files](../inside-a-sandbox/files.md)):

- A directory listing (name, size, modified, mode), with breadcrumbs for the path and a path box you can type into. Sort by any column. Hidden files toggle.
- **Upload** by drag and drop or picker, several files and folders, each streamed with progress, cancellable. Uploading over an existing file asks.
- **Download** a file, streamed; a folder as a `.tar.gz` built in the sandbox (`tar` via a command, streamed back) if [inside-a-sandbox/files](../inside-a-sandbox/files.md) adds archive downloads.
- **Open** a text file under 2 MB in CodeMirror, edit and save, with a conflict check if the API supports conditional writes (Lewagent asked for `If-Match`); images and PDFs preview in a sandboxed iframe or `<img>` from a blob URL.
- **New folder, rename or move, delete** (with a confirmation listing what goes).
- A sandbox that isn't running shows "Its files are only reached while it runs", as legacy does, with Start.
- The file browser component is shared with Lewagent.

### Ports

`…/{sbx}/ports` ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md), [inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)):

- **List:** port, protocol, access, persist, URL (or `tcp://host:port`), and whether it's reachable now. While the sandbox isn't running, URLs show "not reachable" and persisted ports say they'll be exposed again on start, at a new URL.
- **Expose:** port (with ports that something in the sandbox is listening on suggested, from `ss -ltn` via a command, if cheap enough), protocol, access (private only for HTTP, and only on a cluster with a domain), persist. Errors explained inline: `port_protocol_conflict`, the proxy range, `private_ports_unavailable`.
- **Open** a public port's URL in a new tab; a private one by making a link (one minute, opened at once) with an optional path; copy a TCP address with an `ssh` or `psql` snippet.
- **Change** access or persist; **Close**.
- A private port opened inside the dashboard (e.g. a desktop on 6080) can preview in a panel, an iframe on the port's own origin, with the `lewbox:port-unauthorized` message handled by offering "Open in a new tab" ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)).

### SSH keys

`…/{sbx}/ssh-keys` ([inside-a-sandbox/ssh](../inside-a-sandbox/ssh.md)): the keys that reach only this sandbox, beside the profile's keys it also gets (read-only here, linked to the profile). Add by pasting or uploading a `.pub`, or **generate an Ed25519 pair in the browser** (WebCrypto), download the private key, and add the public one, as legacy's `lib/sshKeys.ts` does. Remove. A help panel shows how to connect through a persisted TCP port on 22, from the [SSH guide](https://lewbox.com/docs/guides/ssh-access).

### Conversations

`…/{sbx}/conversations` and `/{cnv}` ([agents](../agents.md)).

#### List

Conversations in the sandbox: title, agent, directory or worktree, status (idle, running, waiting on you, failed), last turn's time, usage. Live status dots. "New conversation". A deleted sandbox's conversations stay readable from the profile's activity and the project-wide conversation search, but can't be prompted.

#### The chat

The chat is a shared component (`components/chat`), used by the dashboard and published for Lewagent. It's built on the TypeScript SDK's conversation helpers ([sdks-and-cli/typescript](../sdks-and-cli/typescript.md#conversations)), which own following a turn, reconnecting, and merging events into chunks. The UI owns rendering and input.

**Layout:**

```
┌ Fix the login redirect · Claude Code · /root/workspace/.worktrees/cnv_… (branch lewbox/cnv_…) ┐
│                                                                                                │
│  You · 18:02                                                                                   │
│  The login page redirects to / instead of ?next=. Fix it and add a test.                       │
│                                                                                                │
│  Claude · thinking ▸                                                                           │
│  ▸ Read app/Http/Controllers/Auth/LoginController.php                                          │
│  ▸ Edit LoginController.php  +4 −1                                                             │
│  ▸ Run php artisan test --filter LoginTest  ✓ exit 0 · 3.2 s                                   │
│  The redirect now honours `next`, …                                                            │
│                                                                                 usage 12k · $0.04 │
│ ┌ Approval ──────────────────────────────────────────────────────────────────────────────────┐ │
│ │ Run `rm -rf storage/framework/cache`?   [Allow once] [Always allow] [Reject]               │ │
│ └────────────────────────────────────────────────────────────────────────────────────────────┘ │
│ Queued (1): "Also update the docs"  [Steer now] [Remove]                                       │
│ ┌────────────────────────────────────────────────────────────────────────────────────────────┐ │
│ │ Message Claude…                                               Claude Opus · high · Default ▾│ │
│ └────────────────────────────────────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────────────────────────────┘
```

**What it shows** ([agents/turns-and-events](../agents/turns-and-events.md)):

- Stored messages, as chunks, and the running turn's live events merged into the same chunk model, so a turn looks the same live and after a reload ("one model for stored and live turns", from the docs).
- **Text** as markdown (no raw HTML, highlighted code, copy buttons). **Reasoning** collapsed by default.
- **Tool calls** as cards: title, kind, status (pending, in progress, completed, failed, unreported), input summary (`summarizeToolCall` from legacy), output (command output with its exit code; diffs for edits, `+n −m`; file reads truncated with "show all"), and `truncated` markers where storage cut them.
- **Plan** as a checklist that updates in place.
- **Approvals and questions** in a stack above the prompt box ([agents/approvals-and-questions](../agents/approvals-and-questions.md)): an approval's options as buttons; a question's form (single choice, multiple choice, free text, per the agent's forms); Decline. Answered ones collapse into the transcript with the answer. Pending ones survive a reload. A tab's title and favicon show a dot while something waits on you.
- **Background tasks** in a strip: each task's description, status, and Stop ([agents/background-tasks](../agents/background-tasks.md)).
- **Queued prompts** below the transcript with Steer now and Remove ([agents/conversations](../agents/conversations.md)).
- **Usage** per turn and total: tokens, context used, cost where the agent reports it; the rate-limit state if the agent reports one.
- **Turns the agent starts itself** (peer and agent origins) appear as they're found, marked as such.
- **How a turn ended:** `completed`, `cancelled`, `interrupted` (the sandbox stopped), `failed` with its error, `steered`; and for `output_schema` turns, the JSON output in a viewer.

**Input:**

- A prompt box with: agent and model and thinking level (`ModelPicker`, options read from `GET …/agents` once and kept), mode (default, accept edits, plan, auto, bypass, per the agent), fast mode, and the directory (a `DirectoryPicker` over the files API) or a worktree (new, with branch, from, files to copy, and an optional name).
- Enter sends, Shift+Enter makes a newline. While a turn runs, sending **queues** by default; `⌘Enter` **steers**. Cancel (`Esc` twice) cancels the running turn.
- **Attachments:** files uploaded into the sandbox and referenced as content blocks; images pasted or dropped.
- **Slash commands** the agent lists, completed as you type.
- **Instructions** and **output schema** under "Advanced" on the first prompt, for testing an integration.

**Following a turn** (from the SDK, as Lewagent's `followTurn` does today): the stream resumes from the last event id after a drop; a stream silent for 20 s is reopened in case it stopped without closing; after three reconnects without progress, the page reads the conversation's status and messages instead. On `finished`, messages and the conversation are read again. Several turns can be followed in several tabs.

**Deleting** a conversation with a dirty worktree asks whether to force it, and whether to keep the worktree ([agents/worktrees](../agents/worktrees.md)).

## API

From [sandboxes](../sandboxes.md), [inside-a-sandbox](../inside-a-sandbox.md), [agents](../agents.md) and [activity-and-webhooks](../activity-and-webhooks.md), all callable with the dashboard's session as a project member ([stack-and-structure](stack-and-structure.md#authentication)), plus WebSocket tickets for attach.

## Testing

Playwright with the fake cluster, toolbox and ACP adapter: create a sandbox and watch its create steps; open a terminal, type, reload and reattach with the replay; start a session that exits and see its exit code arrive live; upload, open, edit, save and download a file; expose a private port and open its link; generate an SSH key; start a conversation in a worktree, answer an approval and a question, queue a prompt and steer it, stop a background task, drop the network mid-turn and see it resume without duplicated text; stop the sandbox mid-turn and see the turn end `interrupted`.

Component tests render every chunk kind and every tool-call state from recorded event fixtures (shared with the SDK's tests).

## Build plan

1. List and create dialog. (M2)
2. Overview, operations, settings. (M2)
3. Terminal component and page; sessions; run command. (M2)
4. File browser component and page; editor. (M2)
5. Ports, with private-port links and preview. (M2)
6. SSH keys with browser generation. (M2)
7. Activity timeline. (M4, once the log has its final shape)
8. Chat component on the SDK's helpers; conversations list and page. (M3)
9. Bulk actions, column settings. (M4)

## Open questions

1. **Folder downloads as archives:** worth an API endpoint, or a command the dashboard runs? (Proposed: an endpoint, `GET …/files/archive?path=`, in [inside-a-sandbox/files](../inside-a-sandbox/files.md).)
2. **Listening-port suggestions** in the expose form: a command per open of the form, or something the toolbox reports? (Proposed: leave it out of the alpha.)
3. **Desktop preview in the dashboard** for templates with a desktop (Selkies on 6080), or leave that to Lewagent? (Proposed: generic private-port preview only.)
4. **Project-wide conversation search** across sandboxes, including deleted ones: a dashboard page, or Lewagent's job? (Lewagent's `small-todo.md` asks for a `conversation_search`.)
