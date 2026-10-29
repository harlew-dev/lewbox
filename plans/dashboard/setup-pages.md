# Dashboard: setup pages

> Status: Draft · Milestone: Alpha (M3) · Updated: 2026-10-29

The hosted pages a profile's owner opens from a setup link to set up what their sandboxes get: their coding agents' sign-ins, Git, SSH keys and telemetry. A project sends the link to one of its customers (or opens it themselves); the page shows the **project's** branding, not Lewbox's, and needs no Lewbox account. The rules behind it belong to [profiles/setup-links](../profiles/setup-links.md), [profiles/agent-credentials](../profiles/agent-credentials.md), [profiles/agent-sign-ins](../profiles/agent-sign-ins.md), [git/github-app](../git/github-app.md), [profiles/ssh-keys](../profiles/ssh-keys.md) and [profiles/telemetry](../profiles/telemetry.md).

## Today

| Legacy page or component (`lewbox-legacy/resources/js/`) | What it does | Lines |
|---|---|---|
| `pages/setup/Link.svelte` | Landing on a link: "This link works once. Keep this tab open until you're done." Exchanges the token for a session | 46 |
| `pages/setup/Profile.svelte` | "Your Profile: set up your sandbox profile for AI, Git, SSH and telemetry", the sections the link allows | 131 |
| `pages/setup/Done.svelte` | "All set. Your sandboxes have what you set up. You can close this tab." (or goes to the return URL) | 14 |
| `pages/setup/Ended.svelte` | "Your session has ended. What you saved is kept. To set up more, ask for a new setup link." | 14 |
| `components/profile/AgentList.svelte` | Each agent: API key (with a warning that anything in a sandbox can read it), or subscription sign-in: "Approve on {site}, then enter the code {site} shows you" (`awaiting_code`) or "Enter this code on {site}, then approve" (`awaiting_approval`); "Subscription sign-ins are not enabled"; "In use" | 385 |
| `components/profile/GitSettings.svelte` | A token (`github_pat_…`) or the GitHub App: authorize, accounts, repositories, errors | 402 |
| `components/profile/SshKeyList.svelte` | Paste or upload a public key, or make an Ed25519 pair in the browser ("its private key is shown to you once, never sent") | 375 |
| `components/profile/TelemetrySettings.svelte` | Provider (Langfuse, Honeycomb, Grafana Cloud, LangSmith, any OTLP endpoint), endpoint, protocol, headers, signals, whether to send content ("Content can hold code and secrets") | 426 |

How it works today: the link (`profile_setup_links`, a one-time hashed token, valid a week, with `sections` and a `return_url`) becomes a session of up to an hour on first use. The page polls every 2 s during a sign-in. It's Laravel-rendered with Inertia, branded with the tenant's name only.

## Goals

- Someone who's never heard of Lewbox can open the link and get their agents signed in, in a minute.
- The project's brand, and nothing that confuses the customer about whose product this is.
- Embeddable: a project can put it in its own app, in an iframe, if it allows the origin.
- The same components the project sees on its own profile pages ([profile-pages](profile-pages.md#setup)).
- Secrets go in, never come back out.

## Design

### Flow

```
Project's backend                       Customer's browser                        lewbox serve
POST …/profiles/{prf}/setup-links  ──▶  link: https://lewbox.example.com/setup/lbl_…
  {sections, return_url}                 │
                                         ├─ GET /setup/lbl_…  (SPA route, Bare layout)
                                         ├─ POST /v0/setup/sessions {token} ──────────▶ check, mark used,
                                         │                                              set setup cookie
                                         ├─ GET /v0/setup/profile ◀── sections, branding, state
                                         ├─ … set things up …
                                         └─ Done → return_url, or /setup/{token}/done
```

1. **Open the link.** `/setup/{token}` runs in the dashboard's SPA with the `Bare` layout. The page exchanges the token (`lbl_…`) for a **setup session**: a separate cookie (`__Host-lewbox_setup`, `SameSite=None; Secure; Partitioned` when embedding is allowed, else `Lax`), valid an hour, scoped to one profile and the link's sections. The token is then used up, and the URL is replaced (`history.replaceState`) so it isn't left in history ([profiles/setup-links](../profiles/setup-links.md)).
2. **Set up.** One page, a card per section the link allows, in this order: Agents, Git, SSH keys, Telemetry. Each card saves on its own.
3. **Done.** Goes to the `return_url` if the link had one (with `?lewbox_setup=done`), else `/setup/{token}/done`.
4. **Expired or used.** An unknown, used or expired link, or an ended session, goes to `/setup/ended`, which says what's kept and to ask for a new link. It never says which of those it was, so a link can't be probed.

### Branding

From the project's settings ([project-pages](project-pages.md#general)):

- The project's display name and logo at the top; its accent colour on buttons and focus rings (checked for contrast; falls back to the default when too light or dark); a support link at the bottom.
- The page title is "Set up your profile · {project name}".
- **No Lewbox branding** by default, as legacy. A small "Powered by Lewbox" is an instance setting, off unless the instance turns it on.
- The profile's name shows as "Setting up {profile name}", so a customer with several knows which.

### Embedding

- By default, `frame-ancestors 'none'`.
- A project may list origins allowed to embed its setup pages (e.g. `https://app.acme.com`). The setup routes then send `frame-ancestors` with those origins, and the setup cookie is `SameSite=None; Partitioned` so it works as a third-party cookie where browsers allow it.
- Where a browser blocks the cookie in an iframe, the page posts `{type: "lewbox:setup-unauthorized"}` to its parent, as private ports do with `lewbox:port-unauthorized`, so the embedding app can open the link in a tab instead.
- When done, an embedded page also posts `{type: "lewbox:setup-done", sections: […]}` to its parent.
- GitHub's and the agents' providers' own pages always open in a new tab or popup, never in the iframe.

### Agents

A card per agent plugin on for the project ([project-pages](project-pages.md#agents-and-sign-ins)): Claude Code and Codex in the default build. Cards are built from each agent provider's credential methods and sign-in flow, or are the plugin's own component where it has one, through the `profile.credentials.cards` slot ([plugins/dashboard-slots](../plugins/dashboard-slots.md#the-setup-pages)); a new agent plugin appears here without changes to this page.

Each shows its state, "Ready", "Not set up", "Failed: …" (from the last verification), and how it's signed in, with the choices the project allows:

- **API key.** Paste an Anthropic or OpenAI key. It's verified against the provider's model list before saving ([profiles/agent-credentials](../profiles/agent-credentials.md)); a bad key is refused with the provider's message. Saved keys show as "sk-…abcd, verified 2 m ago" with Replace and Remove. A warning, as legacy has: anything that runs in a sandbox can read it.
- **Subscription** (Claude, ChatGPT). "Sign in" starts a sign-in ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)), and the card walks through its states live, from the setup session's event stream (`GET /v0/setup/events`, SSE) instead of a 2 s poll:
  - `queued`: "Waiting for a sign-in slot…" with position if known;
  - `starting`: "Starting a secure sign-in…";
  - `awaiting_code`: "Approve on claude.ai, then enter the code it shows you", with the link (new tab) and a code field;
  - `awaiting_approval`: "Enter this code on chatgpt.com, then approve the sign-in", with the code large and copyable, and the link;
  - `submitting_code`: "Signing in…";
  - `completed`: "Ready";
  - `failed`, `expired`: what went wrong and "Try again";
  - Cancel at any point.
- If subscription sign-ins aren't set up on the project, the option is shown disabled with "Subscription sign-ins are not enabled", as legacy does.
- **In use:** when running sandboxes already use a credential, replacing it says they'll get the new one within a minute ([sandboxes/profile-setup](../sandboxes/profile-setup.md)).

### Git

A card for connecting GitHub through the project's App ([git/github-app](../git/github-app.md)); there's no token box, as pasted tokens are gone:

- "Connect GitHub" opens GitHub's authorize-and-install flow in a popup. Back on the page, two lists:
  - **People**: each GitHub person connected to the profile, with their avatar and login, *Default* on one (and *Make default* on the others), whether their authorization still works ("Authorize again" when it lapsed), and Remove.
  - **Accounts**: each personal account and organization, with its repository selection ("All repositories" or "12 selected"), which person it's reached through, where to change it on GitHub, its error if any, and Remove; then **available accounts** (reachable but not linked, over the limit) with Link.
  - "Add another GitHub person" (the same flow; the page shows which GitHub user the browser is signed in as and links to GitHub's sign-out, so a machine user can be added), "Install on another account" and "Already installed?" (authorize again to find existing installations).
  - A counter, "5 of 25 GitHub people and accounts", and at the limit a note to remove one before adding more ([git/github-app](../git/github-app.md#the-limit)).
  - An organization that forbids third-party Apps can't be connected: GitHub says so in the popup, and the page explains that an organization owner must allow the App.
- **Identity:** the name and email commits are made as, prefilled from the default person when connected.
- **Repositories:** the repositories every linked account reaches, searchable, so the customer can check the one they care about is there. A failure to list says "GitHub couldn't list the repositories" with Retry.

### SSH keys

A card for the profile's SSH keys, which every one of its sandboxes gets ([profiles/ssh-keys](../profiles/ssh-keys.md)):

- **Add:** a name, and a public key pasted or chosen from a file (`~/.ssh/id_ed25519.pub`); type and fingerprint shown before saving; duplicates refused.
- **Generate:** an Ed25519 pair made in the browser with WebCrypto; the public key is added, and the private key is shown once (download as `id_ed25519_lewbox`, copy), never sent, with how to use it (`ssh -i … -p … root@host`).
- **List:** name, type, fingerprint, added; Remove.

### Telemetry

A card for exporting the agents' metrics, logs and traces ([profiles/telemetry](../profiles/telemetry.md)):

- **Provider:** Langfuse, Honeycomb, Grafana Cloud, LangSmith, or any OTLP endpoint. Each provider's form asks only for what it needs (Langfuse: host, public and secret key; Honeycomb: API key and dataset; …) and builds the endpoint and headers; "Custom" asks for the endpoint ("The base URL. Over HTTP the agents add /v1/metrics, /v1/logs and /v1/traces"), protocol (`http/protobuf`, `http/json`, `grpc`) and headers.
- **Signals:** metrics, logs, traces.
- **Content:** whether prompts, responses and tool input and output are sent. Off by default: "Unticked, the agents send only metadata, such as tokens, cost, durations and tool names. Content can hold code and secrets."
- **Test:** sends a test span and reports what the endpoint said, where the provider allows it.
- Headers' values are write-only once saved.
- When changes apply: new conversations at once, running ones on their next start of the agent ([profiles/telemetry](../profiles/telemetry.md)).

### Sections

Sections come from the plugins on for the project (`profile.setup.sections`): `agents` (the agent plugins), `git` (`git/github`), `telemetry` (`telemetry`), and the core's `ssh_keys`. A link's `sections` decide which cards appear. A section not in the link isn't shown, and its API refuses the setup session (`403`, `permission_denied`). A link with one section shows that card alone, without the page's list.

### What the project sees

The project's own profile page shows the same cards, with project-only additions, through the Admin API ([profile-pages](profile-pages.md#setup)). The components take a client (setup session or project session) as a prop, so one implementation serves both.

## API

From the plans named above: `POST /v0/setup/sessions` (exchange a link), `GET /v0/setup/profile` (branding, sections, state), the setup-session versions of agent credentials, sign-ins, Git, SSH keys and telemetry, and `GET /v0/setup/events` (SSE). These live under `/v0/setup/…` and accept only the setup cookie, so a setup session can't reach anything else ([api/authentication](../api/authentication.md)).

## Security

- The token is single-use, hashed at rest, and expires in a week; the session it makes lasts an hour and reaches one profile's chosen sections.
- Secrets (API keys, tokens, telemetry headers) are write-only through the setup API; the page shows only masked forms and verification state.
- CSRF: the setup session has its own CSRF token, as the dashboard's session does.
- Rate limits on exchanging links and on sign-ins per profile.
- Agent output and provider messages are rendered as text.
- Content Security Policy: as the dashboard's, with `frame-ancestors` from the project's allowed origins, and `connect-src 'self'`.

## Testing

Playwright: exchange a link, see only its sections, set an API key with the fake provider's verification (good and bad), run a subscription sign-in with the fake sign-in flow through each state, connect the fake GitHub App and see accounts, generate an SSH key and check only the public key was sent, save telemetry and send a test, reach Done and the return URL; a used link and an ended session land on `/setup/ended`; embedded in an allowed origin, and refused in another.

## Build plan

1. Setup session exchange, Bare layout with branding, Done and Ended. (M3)
2. Agents card with API keys. (M3)
3. Subscription sign-ins with live states. (M3)
4. Git card: GitHub people, accounts, available accounts and the limit. (M3)
5. SSH keys card with browser generation. (M3)
6. Telemetry card with provider forms and test. (M3)
7. Embedding: allowed origins, partitioned cookie, postMessage events. (M4)

## Open questions

1. **Should a setup session last longer than an hour** while a sign-in is in progress? (Proposed: extend it while a sign-in runs.)
2. **"Powered by Lewbox":** off by default on every instance, or on by default for lewbox.com's hosted projects?
3. **Several links at once** for one profile: allowed, or does a new link revoke the old? (Legacy allows several.)
4. **Setup page on a project's own domain** (custom domains, Later): does that change the cookie and embedding story enough to plan for now?
