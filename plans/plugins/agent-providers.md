# Agent providers

> Status: Draft · Milestone: Alpha (M3) for the interface, `agents/claude-code` and `agents/codex`; `agents/opencode` Later · Updated: 2026-10-29

A coding agent (Claude Code, Codex, OpenCode, Gemini CLI…) is an **agent provider plugin**. The core's agent framework ([agents](../agents.md)) runs conversations, turns, events, approvals, worktrees and agentd the same way for every agent; a provider says what's particular to one: how to install and start it, how its modes map, which credentials it takes and how to check them, how a person signs in, what setup writes into the sandbox, and, only where a manifest can't say it, a driver inside agentd.

The aim: adding an agent that speaks ACP (the Agent Client Protocol) is a manifest and a setup step, no agentd code; an agent with quirks adds a small driver.

## Today

Agents are spread over two repositories ([plugins](../plugins.md#today)):

- **Specs** in the gateway's agentd: `lewbox-gateway-legacy/internal/agentd/agents.go` (adapters, modes, env, stop), `codexproxy.go` (the Codex stand-in), `tasks.go` (Claude's background tasks), `options.go` (probing models and modes).
- **Credentials** in the app: `app/Enums/AgentCredentialProvider.php`, verification per provider, `app/Http/Controllers/SetupLink/*`.
- **Setup** in the app: `app/Sandboxes/Setup/{ClaudeCodeSetup,CodexSetup}.php`.
- **Sign-ins** in the app: `app/Sandboxes/SignIns/{ClaudeSignIn,CodexSignIn}.php`, and the ChatGPT refresh, `RefreshSubscriptionLogins`.
- **The `agents` template**: `app/Sandboxes/Templates/*` with each CLI's install lines.

The rebuild's agent plans already describe each piece per agent ([agents/agentd](../agents/agentd.md#agents), [profiles/agent-credentials](../profiles/agent-credentials.md#agents-and-providers), [profiles/agent-sign-ins](../profiles/agent-sign-ins.md), [sandboxes/profile-setup](../sandboxes/profile-setup.md#the-steps)); this plan gathers them behind one interface.

## Goals

- One plugin per agent, holding everything particular to it.
- The core never names an agent: no `if agent == "codex"` outside `plugins/agents/codex`.
- ACP agents run from a manifest. Drivers only for quirks.
- The dashboard's and setup pages' agent cards, the options endpoint and the `agents` template are built from the registered providers.

## Design

### The interface

```go
package agent // github.com/harlew-dev/lewbox/plugin/agent

type Provider interface {
    // Spec is everything agentd and the conversation API need to run the agent.
    // It's sent to agentd with each prompt, so a manifest-only agent needs no agentd code.
    Spec() Spec

    // Credentials lists the ways a profile can give the agent a login.
    Credentials() []CredentialMethod

    // Verify checks a credential as it's set (pasted key, or a login a sign-in produced).
    // Called outside any transaction, with the request's deadline (10 s).
    Verify(ctx context.Context, method string, secret Secret) (Verified, error)

    // Setup is the agent's setup contributor: its files, env, check, cleanup, scrub paths
    // and (optionally) harvest. See plugins/extension-points § Setup contributors.
    Setup() setup.Contributor

    // Optional parts: nil when the agent has none.
    SignIn(method string) SignInFlow   // subscription sign-in in a helper sandbox
    Refresher() Refresher               // keeps a stored login fresh (ChatGPT's rotating refresh token)
    TemplateLines() []string            // Dockerfile lines installing the CLI and adapter into the provided `agents` template
}
```

Registered in `Register`: `r.AgentProvider(Provider{})`. The agent's id is its `Spec().ID`; the plugin's name is `agents/<id>`.

### The spec

```go
type Spec struct {
    ID          string            // "claude-code": the `agent` field in the API; [a-z0-9-]
    Name        string            // "Claude Code"
    Vendor      string            // "Anthropic"
    Binary      string            // command that must exist in the sandbox: "claude"
    Adapter     Adapter           // how agentd starts it
    Modes       []Mode            // Lewbox's mode names → the agent's own
    DefaultMode string
    Options     OptionsProbe      // how models, efforts and fast mode are discovered
    Capabilities Capabilities     // what the framework may offer for this agent
    Env         map[string]string // always set for the adapter: IS_SANDBOX=1
    EnvUnset    []EnvRule         // remove A when B is set: ANTHROPIC_API_KEY when CLAUDE_CODE_OAUTH_TOKEN
    Stop        StopPolicy        // CloseStdinThenTerm | Term
    HiddenSlashCommands []string  // "/login", "/logout"
    MemoryMB    int               // rough peak per conversation, for the docs and sizing hints
    Driver      string            // "" = generic ACP driver; else a driver registered in agentd
}

type Adapter struct {
    Kind     AdapterKind // NPM (an ACP adapter package) | Native (the agent speaks ACP itself)
    Package  string      // "@agentclientprotocol/claude-agent-acp"
    Version  string      // "0.94.0": pinned; overridable by instance setting agents.adapters.<id>
    Bin      string      // "claude-agent-acp"
    Command  []string    // Native: ["opencode", "acp"]
    NodeMajor int        // 22: minimum Node.js for NPM adapters
    ExtraEnv map[string]string // CLAUDE_CODE_EXECUTABLE=$(command -v claude)
}

type Mode struct {
    Name     string // Lewbox's: default | accept_edits | plan | auto | bypass
    ACPMode  string // the agent's ACP mode id
    Config   map[string]string // config options to set with it (Codex: collaboration_mode=plan)
}

type Capabilities struct {
    Instructions   InstructionsVia // SystemPromptAppend | ConfigKey("developer_instructions") | None
    OutputSchema   SchemaVia       // SessionMeta | TurnStart (needs a driver) | None
    BackgroundTasks bool           // reports background tasks (needs a driver)
    FastMode       bool
    Questions      bool            // elicitation forms
    Images         bool            // image content blocks in prompts
}
```

Core uses the spec for: validating `agent`, `mode`, `effort`, `fast`, `instructions` and `output_schema` on conversation requests (`422 validation_failed` with "Codex can't take output_schema with …" where a capability is missing, [agents/conversations](../agents/conversations.md)); the options endpoint ([agents/models-and-modes](../agents/models-and-modes.md)); memory hints in the docs; and sending it to agentd.

### Credentials

```go
type CredentialMethod struct {
    Provider string         // "anthropic" | "anthropic_subscription" | "openai" | "chatgpt_subscription" | "openrouter" | …
    Kind     MethodKind     // APIKey | Subscription | Cloud
    Title    string         // "Anthropic API key"
    Fields   []Field        // {Name, Label, Help, Secret, Pattern, CreateURL}
    SignIn   bool           // a SignInFlow exists for it
    Milestone string
}

type Secret map[string]string
type Verified struct {
    Identity string      // "sam@acme.dev", or the key's last four
    ExpiresAt *time.Time // a subscription token's expiry, if known
    Metadata  map[string]string
}
```

The core stores credentials in `profile_credentials` (agent id, provider, encrypted secret, `verified_at`, metadata), validates fields against `Fields`, calls `Verify`, and serves the registry as `GET /v0/agents/providers` for forms ([profiles/agent-credentials](../profiles/agent-credentials.md)). A `Verify` error with code `credential_invalid` is shown to the person; anything else is `502 provider_unavailable` and the credential isn't saved.

### Sign-in flows

The core's sign-in machinery ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)) owns the helper sandbox, the job, the state machine, the queue and the API. A provider describes the CLI's side:

```go
type SignInFlow struct {
    Kind     SignInKind      // PasteCode (the person pastes a code back: Claude) | DeviceCode (the person types a code on the site: ChatGPT)
    Prepare  []setup.File    // written before the CLI starts (Codex: config keeping the login in a file)
    Command  string          // run in a PTY in /root: "claude setup-token" | "codex login --device-auth"
    Link     *regexp.Regexp  // the URL to show
    Code     *regexp.Regexp  // DeviceCode: the code to show
    Submit   func(code string) [][]byte // PasteCode: what to type (the code, then "\r" after 1 s)
    Done     Detector        // OutputMatch(regexp) | PollCommand{Cmd, Every, Until} | FileAppears(path)
    Read     func(output []byte, files map[string][]byte) (Secret, Verified, error) // the login
    Refused  *regexp.Regexp  // output meaning the code was refused → code_refused
    Hint     string          // shown when no link appears ("Check device code sign-in is on…")
    Timeout  time.Duration   // the flow's own, within the machinery's 15 minutes
}
```

The `agents` template, built from every enabled agent provider's `TemplateLines`, has every CLI, so one helper image serves all sign-ins ([templates/provided-templates](../templates/provided-templates.md)).

### Refresh

```go
type Refresher interface {
    // Due says when a stored login next needs refreshing (nil: never).
    Due(c StoredCredential) *time.Time
    // Refresh exchanges it for a new one, outside any sandbox. Called by a job at Due,
    // under the credential's row lock. Returning a new secret stores it and re-applies
    // the agent's setup to the profile's running sandboxes (a partial apply).
    Refresh(ctx context.Context, h plugin.Host, c StoredCredential) (*Secret, error)
}
```

Today only Codex's ChatGPT login has one. With [harvest](extension-points.md#harvest) (logins the agent refreshed itself inside a sandbox), this keeps a rotating refresh token from ever being used twice ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md#refreshing-chatgpt-logins)).

### Setup

The provider's `Setup()` is a setup contributor ([extension-points](extension-points.md#setup-contributors)) in the agents' order range (100–199), with `Requires: [Spec().Binary]`, so a sandbox without the CLI reports the step `not_applicable`. It writes the login where the agent reads it, secrets on `/run/lewbox` only, symlinks where the agent expects a file elsewhere, and lists every path for the scrub. The steps for Claude Code and Codex are as [sandboxes/profile-setup](../sandboxes/profile-setup.md#the-steps) describes; they move into their plugins unchanged.

### The agentd driver

agentd ([agents/agentd](../agents/agentd.md)) runs every agent through one ACP client. Per agent it needs a **driver**: the code between the framework's commands (prompt, steer, cancel, reply, options, stop task) and that agent's ACP sessions.

```go
package driver // github.com/harlew-dev/lewbox/plugin/agent/driver; compiled into lewbox-sandbox

type Driver interface {
    ID() string
    // Env adjusts the adapter's environment (after the login shell's and the spec's).
    Env(env map[string]string, c Conversation) map[string]string
    // NewSession adds what this agent needs to session/new, load or resume: instructions,
    // output schema, MCP servers, meta.
    NewSession(req *acp.NewSessionRequest, c Conversation) error
    // Prompt adjusts a turn's prompt request (Codex: nothing; the stand-in adds the schema).
    Prompt(req *acp.PromptRequest, c Conversation, t Turn) error
    // Notification sees each ACP notification first, and may turn agent-specific ones into
    // framework events (Claude's raw SDK task messages → task events). Return false to drop it.
    Notification(n acp.Notification, emit func(Event)) bool
    // StopTask stops a background task, if the agent has them.
    StopTask(ctx context.Context, taskID string) error
}
```

- **The generic driver** implements all of it from the `Spec` the control plane sends: env rules, instructions via the declared method, modes via ACP `session/set_mode` and config options, no background tasks, no output schema unless `SessionMeta` names the meta key. An ACP-native agent with no quirks (OpenCode with `opencode acp`; Gemini CLI with its ACP mode) needs nothing else.
- **A custom driver** lives in `plugins/agents/<id>/agentd/`, `//go:build sandbox`, embedding the generic driver and overriding what differs. `agents/claude-code` overrides `NewSession` (`_meta.claudeCode.options.outputFormat`, `emitRawSDKMessages`) and `Notification` (tasks); `agents/codex` overrides `Env` (`CODEX_PATH` = the stand-in, `CODEX_CONFIG` for instructions and telemetry) and ships the stand-in itself (`lewbox agentd codex-proxy`, registered as a subcommand of the sandbox build).
- `Spec().Driver` names the driver; agentd falls back to the generic one, and logs, if the build has no driver of that name (a control plane newer than the sandbox build can't happen in practice, since they ship together).

### Errors

| Code | Status | When |
|---|---|---|
| `plugin_disabled` | 409 | A conversation, credential or sign-in for an agent whose plugin is off for the project or instance |
| `validation_failed` | 422 | An `agent` that isn't registered; a mode, effort or field the agent doesn't take |
| `credential_invalid` | 422 | `Verify` refused the key or login, with the provider's reason in `provider_message` |
| `provider_unavailable` | 502 | `Verify` couldn't reach the provider |
| `agent_unavailable` | 502/503 | As today: the adapter couldn't start (no Node, install failed) ([agents](../agents.md)) |

### Contract tests

`plugin/agent/agenttest.Run(t, provider, agenttest.Options{…})`, which every agent provider's tests call:

- **Spec:** ids and mode names valid; every mode maps; capabilities consistent with the driver (an `OutputSchema: TurnStart` needs a driver); adapter pinned to an exact version.
- **Credentials:** each method's fields render; `Verify` against a recorded fake of the provider's API (accepted, refused, unreachable).
- **Setup:** `Build` for each method writes secrets only under `/run/lewbox` or listed in `Scrub`; `Cleanup` removes everything `Build` wrote (run against the fake toolbox's filesystem); `ScrubPaths` covers every secret file, symlink targets included; a disabled provider's `Cleanup` and `ScrubPaths` still work from a recorded report.
- **Sign-in** (if any): the flow against a scripted fake CLI in the fake toolbox: link found, code submitted or approval polled, login read; refused code; no link.
- **Driver:** the agentd fake-adapter harness (legacy's re-exec trick, [agents/agentd](../agents/agentd.md#testing)) runs a prompt, a mode change, a cancel and (if supported) a schema and a task through the driver.
- **Live** (nightly, behind credentials in CI secrets): one real prompt in a real sandbox with the real CLI.

## Adding a new agent

Taking **Gemini CLI** as the example:

1. **Check it speaks ACP.** Gemini CLI has an ACP mode; if an agent doesn't, it needs an ACP adapter package first (the Zed-maintained adapters are the model).
2. **Make the plugin:** `plugins/agents/gemini/plugin.go` with `Info{Name: "agents/gemini", Kind: KindAgent}`, and add it to `plugins/config.json` with `enabled: false` until it's tested.
3. **Write the spec:** id `gemini`, binary `gemini`, adapter `Native` with its ACP command, modes mapped to Lewbox's (`default`, `auto`, `plan` where it has them), capabilities honestly (no output schema until it supports one).
4. **Credentials:** its methods (a Google AI API key; Vertex later), fields, and `Verify` (list models with the key).
5. **Setup:** where the CLI reads its key (env in `/run/lewbox/env`, or a settings file through a symlink into `/run/lewbox`), the trusted-directory or onboarding settings that let it run without prompts, the check command, cleanup and scrub paths.
6. **Sign-in** (if it has a subscription login that can be run in a terminal): a `SignInFlow`. Skip otherwise.
7. **Template lines:** the install for the provided `agents` template, pinned.
8. **Driver** only if the generic one isn't enough: add `plugins/agents/gemini/agentd/`.
9. **Docs** (`plugins/agents/gemini/docs/`): what it supports, known gaps, memory per conversation.
10. **Run `agenttest`**, then the live test, then set `enabled: true` in `config.json`.

## Testing

Beyond the contract suite: the conversation API's tests run once per registered agent, with the fake adapter, so every agent gets queue, steer, cancel, approvals and replay coverage from the framework's suite.

## Build plan

1. `plugin/agent`: `Provider`, `Spec`, methods, `SignInFlow`, `Refresher`; `plugin/agent/driver` and the generic driver in agentd (M3).
2. Move Claude Code and Codex into `plugins/agents/claude-code` and `plugins/agents/codex`, with their drivers (M3).
3. Credentials registry, options endpoint and the `agents` template built from providers (M3).
4. `agenttest` (M3).
5. `agents/opencode` with the generic driver (Later).

## Open questions

1. **Adapter versions per project.** Instance setting only (proposed), or may a project pin its own?
2. **Agents in the image vs installed on first use.** Should a provider be able to say "requires the template to have it" and refuse in sandboxes without it, rather than installing the adapter (57 MB, ~26 s)? Proposed: keep installing on first use, as agentd does.
3. **Third-party ACP adapters' trust.** An adapter package runs as root in the sandbox with the profile's logins: is pinning a version enough, or do we vendor and mirror adapter packages?
