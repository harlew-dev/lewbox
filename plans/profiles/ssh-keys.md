# SSH keys

> Status: Draft · Milestone: Alpha (M2) · Core · Updated: 2026-10-29

A profile can hold **public SSH keys**, written into every one of its sandboxes' `authorized_keys`, so its customer can SSH into any of them with the same key once a template runs `sshd` and port 22 is exposed. A single sandbox can also hold **keys of its own**, which reach only it, set by whoever works with that sandbox through the consumer API. Lewbox never holds private keys.

## Today

Built in legacy (`lewbox-legacy/docs/profiles.md` § *SSH keys*; `app/Models/SshKey.php`; `app/Actions/Profiles/{AddSshKey,RemoveSshKey}.php`; `app/Sandboxes/Setup/SshSetup.php`; setup page `components/profile/SshKeyList.svelte`; dashboard `pages/tenants/sandboxes/SshKeys.svelte`):

- `ssh_keys` (`ssh_`): `profile_id`, `sandbox_id` (null for a profile's key), `name`, `type`, `public_key` (normalized `type base64 comment`), `fingerprint` (`SHA256:…`); uniqueness on name and fingerprint per profile (partial indexes where `sandbox_id is null`) and per sandbox.
- A key is one line of a `.pub`, of a type OpenSSH accepts by default, whose encoded type matches its label; private keys, several keys, and `authorized_keys` options (`command=`) are refused. At most 20 per profile and 20 per sandbox (`lewbox.ssh_keys`).
- Written by a setup step: a block between `# lewbox begin` and `# lewbox end` in `/root/.ssh/authorized_keys` (`0600`, directory `0700`), profile keys then the sandbox's, each fingerprint once; the rest of the file kept. Cleanup removes the block (so snapshots hold no keys). Needs only `sh`.
- Profile keys: setup page or Admin API (`GET, POST /profiles/{prf}/ssh-keys`, `DELETE …/{ssh}`); changes re-apply setup (`apply_setup`).
- Sandbox keys: consumer API by name (`GET /v1/sandboxes/{sbx}/ssh-keys`, `PUT`/`DELETE …/{name}`) with `sandboxes.exec`; names `^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`; `PUT` replaces, `DELETE` of a missing one succeeds; a key the profile has is refused. The app answers a change with a `sync` command (the block as it is now) that **the gateway runs before answering**, so a busy sandbox gets it at once; a failed sync keeps the change for retry.
- Logged as `ssh_key.added` / `ssh_key.removed`. The dashboard can generate an Ed25519 pair in the browser (the private key is shown once, never sent).
- Public guide: [lewbox.com/docs/guides/ssh-access](https://lewbox.com/docs/guides/ssh-access) (template with `sshd`, add a key, start `sshd`, expose 22 as `tcp`, connect to `host:host_port`).

## Goals

- Keep it as is; it's small and right. Port the validation, the block format and the immediate sync.
- Run the sync in-process (no app→gateway round trip).
- Make SSH easier: an optional built-in `sshd` path so the guide's five steps shrink (see [inside-a-sandbox/ssh](../inside-a-sandbox/ssh.md)).

### Non-goals

- Private keys, for SSH out of a sandbox (Lewagent stored those; not ported). Git uses HTTPS tokens.
- SSH certificates (Later, with an SSH CA per project; see [Open questions](#open-questions)).

## Design

### Data model

```sql
create table ssh_keys (
  id           bigint generated always as identity primary key,
  public_id    text not null unique,                      -- ssh_…
  project_id   bigint not null references projects(id) on delete cascade,
  profile_id   bigint not null references profiles(id) on delete cascade,
  sandbox_id   bigint references sandboxes(id) on delete cascade,   -- null: the profile's
  name         text not null,
  type         text not null,                             -- ssh-ed25519, ecdsa-sha2-nistp256, ssh-rsa, sk-ssh-ed25519@openssh.com, …
  public_key   text not null,                             -- "type base64 comment"
  fingerprint  text not null,                             -- SHA256:…
  created_by   jsonb not null,
  created_at   timestamptz not null default now()
);
create unique index on ssh_keys (profile_id, name)        where sandbox_id is null;
create unique index on ssh_keys (profile_id, fingerprint) where sandbox_id is null;
create unique index on ssh_keys (sandbox_id, name)        where sandbox_id is not null;
create unique index on ssh_keys (sandbox_id, fingerprint) where sandbox_id is not null;
```

Postgres keeps partial indexes through every migration, so legacy's SQLite workaround (re-creating them after table rebuilds) goes away.

### Validation

Parsed with `golang.org/x/crypto/ssh.ParseAuthorizedKey`:

- Exactly one key, no options (`command=`, `from=`, …): `422` "Paste one public key, as in a .pub file".
- A private key pasted by mistake (`-----BEGIN`) is refused with a message saying so, and never stored or logged.
- Types allowed: `ssh-ed25519`, `ecdsa-sha2-nistp256/384/521`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`, `ssh-rsa` with at least 2048 bits. DSA refused.
- Normalized to `type base64 comment`; the comment defaults to the key's `name`.
- Fingerprint `SHA256:` + base64 of the SHA-256 of the wire key.
- Names: profile keys any text up to 100 characters; sandbox keys `^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$` (they're in URLs).
- Caps: 20 per profile, 20 per sandbox (configurable).
- A sandbox key whose fingerprint the profile already has: `409 ssh_key_on_profile` (removing it from the sandbox wouldn't revoke access).

### Writing them into sandboxes

The SSH step of [profile setup](../sandboxes/profile-setup.md): `requires: sh`; writes the managed block in `/root/.ssh/authorized_keys` with the profile's keys then the sandbox's own, deduplicated by fingerprint, creating the directory and file with the right modes, leaving everything outside the block alone; cleanup removes the block. Applied on create and every start, removed before a requested stop.

**Profile key changes** enqueue `apply_setup` for the profile's running sandboxes, as credentials do.

**Sandbox key changes** are written in the request itself, because the person adding a key wants to connect now and a sandbox busy with another operation would skip `apply_setup`: the handler commits the change, then runs the step's sync command through the toolbox (`sh -c` with the block in its stdin, never in argv) and answers. If the sync fails (toolbox unavailable), the change is kept and the response is `200` with `sync: "failed"` and the reason; repeating the request (idempotent) retries, and the next start writes it anyway. A stopped sandbox's keys are written when it starts.

### SSH reaching the sandbox

Keys are only half: the sandbox needs `sshd` running and port 22 exposed as `tcp` ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md)). That path, and an optional Lewbox-provided `sshd` so a plain image works, are in [inside-a-sandbox/ssh](../inside-a-sandbox/ssh.md).

## API

Profile keys, management (`/v0/projects/{prj}/profiles/{prf}/ssh-keys`; owner, admin, member, setup session with `ssh_keys`, admin key with `profiles.credentials`):

| Method | Path | What |
|---|---|---|
| `GET` | `…/ssh-keys` | List: `id`, `name`, `type`, `fingerprint`, `public_key`, `created_by`, `created_at` |
| `POST` | `…/ssh-keys` | `{ name, public_key }`; `201` |
| `DELETE` | `…/ssh-keys/{ssh}` | `204` |

Sandbox keys, consumer (`/v0/sandboxes/{sbx}/ssh-keys`; profile key or sandbox key; `sandboxes.exec` to change):

| Method | Path | What |
|---|---|---|
| `GET` | `…/ssh-keys` | The sandbox's own keys, and its profile's (read-only, `source: "profile"`) |
| `PUT` | `…/ssh-keys/{name}` | `{ public_key }`; creates or replaces; answers the key and `sync` |
| `DELETE` | `…/ssh-keys/{name}` | `204`, also when it didn't exist |

The management API reaches sandbox keys the same way through `/v0/projects/{prj}/sandboxes/{sbx}/ssh-keys` for the dashboard.

### Errors

| Status | Code | When |
|---|---|---|
| `409` | `already_exists` | A profile key with that name or fingerprint |
| `409` | `ssh_key_on_profile` | A sandbox key the profile already has |
| `422` | `validation_failed` | Not one valid public key; a private key; an option; weak RSA; name invalid; over the per-profile or per-sandbox cap |

Changes to a stopped sandbox's keys aren't refused: they're stored and written when it starts.

## Security

- Public keys only; a pasted private key is refused before it's stored and is never logged (the request body is not logged for this route).
- The managed block is all Lewbox touches; keys the customer added themselves stay.
- Cleanup before stops keeps keys out of snapshots.

## Observability

- Activity: `ssh_key.added`, `ssh_key.removed` on the sandbox (sandbox keys); audit `profile.ssh_key_added`, `profile.ssh_key_removed` (profile keys).
- Webhooks: `profile.setup_changed` for profile keys.

## Testing

- Parser: each allowed type, DSA, short RSA, options, multiple keys, private key, CRLF and trailing spaces.
- Block writer: empty file, existing user keys above and below, duplicate fingerprints across profile and sandbox, removal leaving the rest.
- Sandbox key sync in the request, toolbox down, retry; stopped sandbox gets them on start.

## Build plan

`internal/profiles/sshkeys` (validation, storage, endpoints); the step in `internal/setup/steps/ssh.go`.

1. Parser and table.
2. Profile endpoints and `apply_setup` on change.
3. Sandbox endpoints with in-request sync.
4. Setup step and cleanup.

## Open questions

1. **SSH certificates.** A per-project SSH CA that signs short-lived certificates for members (`lewbox ssh sbx_…` in the CLI) would avoid managing keys at all. Later?
2. **Members' own keys.** Should dashboard users have personal SSH keys, written into sandboxes they open, rather than adding them to profiles?
