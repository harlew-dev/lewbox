# SSH

> Status: Draft · Milestone: Alpha (M2); SSH gateway Later · Updated: 2026-10-29

SSH lets people reach a sandbox with `ssh`, an editor's remote mode (VS Code, Zed, JetBrains) or `rsync`. Lewbox manages the **keys**: a sandbox's own keys reach only it, and a profile's keys reach every one of the profile's sandboxes. Both are written into root's `~/.ssh/authorized_keys`. In the alpha, Lewbox doesn't run an SSH server. The sandbox's image runs `sshd`, as a session or its own command, with port 22 exposed as `tcp`. Later, a Lewbox SSH gateway could remove all of that.

## Today

- **Handlers:** `lewbox-gateway-legacy/internal/server/sshkeys.go` (160 lines): `listSSHKeys`, `setSSHKey`, `removeSSHKey`, `syncSSHKeys` (`7097551`).
- **Records:** the app's `ssh_keys` table (`ssh_`), belonging to a profile, or to one sandbox when `sandbox_id` is set.
  - Partial unique indexes on name and fingerprint, with a virtual-column trick for MySQL.
  - Limits: 20 per profile (`LEWBOX_SSH_KEYS_PER_PROFILE`) and 20 per sandbox (`LEWBOX_SSH_KEYS_PER_SANDBOX`).
- **Validation:** `lewbox-legacy/app/Rules/SshPublicKey.php`.
  - It accepts one OpenSSH public key of a type OpenSSH accepts by default, whose encoded type matches its label.
  - It normalizes the key to one `authorized_keys` line (control characters stripped from the comment) and computes the `SHA256:` fingerprint as `ssh-keygen -l` prints it.
  - It refuses private keys and `authorized_keys` options (`command=` and the rest).
- **Writing:** `lewbox-legacy/app/Sandboxes/Setup/SshSetup.php` builds a shell command that:
  - replaces the block between `# lewbox begin` and `# lewbox end` in `/root/.ssh/authorized_keys` with the profile's keys, then the sandbox's, each oldest first, a key on both written once;
  - leaves the rest of the file alone;
  - makes the file and directory root-only, as sshd insists.

  The app hands the command to the gateway in the response to a key change (`sync`), and the gateway runs it while the sandbox runs. Every start writes the keys as part of [profile setup](../sandboxes/profile-setup.md); a stop that's asked for removes them first, so a saved disk never holds them.
- **Profile keys** are managed through the Admin API and the hosted setup page, which can also generate an Ed25519 pair in the browser ([profiles/ssh-keys](../profiles.md)).
- **Docs:** [SSH keys](https://lewbox.com/docs/inside-a-sandbox/ssh-keys), [SSH access](https://lewbox.com/docs/guides/ssh-access).
- **Lewagent's template** runs `sshd` with port 22 public (`lewagent-legacy/templates/lewagent-computer`).

**What's wrong:**

1. **A shell command passed from the app to the gateway in a JSON response** to write a file. In one binary, Lewbox writes the block itself.
2. **SSH bypasses Lewbox entirely,** so it doesn't count as activity: a sandbox with an idle timeout can stop under a signed-in user. The docs warn about it.
3. **Setup is heavy for users:**
   - a template with `sshd`;
   - a session to run it;
   - a `tcp` port, whose host and port change on every start;
   - a `HostKeyAlias` so `known_hosts` survives.

## Goals

- Keep the documented API and behaviour: names, idempotent add and remove, the `# lewbox` block, keys written on every start and removed before a stop.
- Write `authorized_keys` directly through the files API, atomically, without shell scripts passed around.
- Document SSH access through a `tcp` port clearly, for the alpha.
- Plan a gateway (`ssh sbx_…@ssh.<instance>`) that needs no `sshd`, no exposed port, counts as activity, and is logged.

## Design

### Data model

`ssh_keys`, shared with [profiles](../profiles.md):

| Column | Type | |
|---|---|---|
| `id`, `public_id` | | `ssh_…` |
| `project_id`, `profile_id` | fk | |
| `sandbox_id` | fk null | Set for a sandbox's own key |
| `name` | `text` | 1–64 of letters, digits, `.`, `-`, `_`, `@`, starting with a letter or digit |
| `type` | `text` | e.g. `ssh-ed25519` |
| `public_key` | `text` | Normalized `authorized_keys` line |
| `fingerprint` | `text` | `SHA256:…` |
| `created_by` | `jsonb` | Actor |
| `created_at` | | |

Unique indexes, both Postgres partial indexes (no MySQL tricks):
- `(profile_id, name) WHERE sandbox_id IS NULL`, `(sandbox_id, name)`;
- `(profile_id, fingerprint) WHERE sandbox_id IS NULL`, `(sandbox_id, fingerprint)`.

### API

| Method and path | What | Answers |
|---|---|---|
| `GET /v0/sandboxes/{sbx}/ssh-keys` | The sandbox's own keys, oldest first, whatever its status. No permission | `200` list |
| `PUT /v0/sandboxes/{sbx}/ssh-keys/{name}` | Add or replace by name, body `{"public_key"}`. `sandboxes.exec` | `201` new, `200` replaced or unchanged |
| `DELETE /v0/sandboxes/{sbx}/ssh-keys/{name}` | Remove. `sandboxes.exec` | `204`, also for a name it doesn't have |

The SSH key object: `id`, `name`, `type`, `public_key`, `fingerprint`, `created_at`.

**Rules**, as documented:
- A name already on the sandbox is replaced; the same key under the same name changes nothing.
- A key already on the sandbox under another name is refused: one key, once per sandbox.
- A key already on the profile is refused, since removing it from the sandbox wouldn't stop it working.
- At most 20 keys per sandbox (configurable).
- Adding to a deleted sandbox is `409 invalid_state`.

### Parsing keys

`internal/sshkeys.Parse`, using `golang.org/x/crypto/ssh.ParseAuthorizedKey`:

- **Accepted types:** `ssh-ed25519`, `ecdsa-sha2-nistp256/384/521`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`, and `ssh-rsa` of at least 2048 bits. `ssh-dss` is refused.
- **Options are refused:** a line whose parse yields options (`command=`, `from=`, …) is invalid.
- **Private keys are refused** with a message that says it looks like a private key, and the body is never logged.
- **Normalized** to `type base64 comment`, the comment stripped of control characters and cut to 255 characters.
- **Fingerprint:** `ssh.FingerprintSHA256`.

### Writing `authorized_keys`

`internal/sshkeys.Sync(sandbox)`, run:
- when a sandbox's or its profile's keys change, for each running sandbox it affects;
- as a step of [profile setup](../sandboxes/profile-setup.md) on every create and start.

1. **Read** `/root/.ssh/authorized_keys` through the files API (missing is empty).
2. **Replace** the block between `# lewbox begin` and `# lewbox end` (appended on a line of its own if absent) with the profile's keys, then the sandbox's, oldest first, de-duplicated by fingerprint. With no keys, remove the block.
3. **Write it back** with a conditional write (`If-Match` on the ETag from step 1, [files](files.md#uploads)), so a user's own edit between the read and the write isn't lost. On `412`, start again, up to three times.
4. **Ensure** `/root/.ssh` is `0700` and the file `0600`, owned by root, with one `chmod` command (checked by `Stat`, as gVisor can lose its exit code).

- **For a running sandbox,** the request waits for the write. If it fails, the change is saved and the request answers `502 sandbox_error` with why; retrying the same request is safe. A stopped sandbox gets the keys at its next start.
- **Profile key changes** fan out as a job ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)) over the profile's running sandboxes, each synced independently.
- **Before a stop that's asked for,** the block is removed as part of setup cleanup, so the saved disk never holds keys ([sandboxes/profile-setup](../sandboxes/profile-setup.md)). A stop by idle timeout or lifetime has already happened, so the block stays in that saved disk; the next start rewrites it with the current keys. The docs say so.

### SSH access in the alpha

The [SSH access guide](https://lewbox.com/docs/guides/ssh-access) stays:

1. A template with `openssh-server`, password and keyboard-interactive sign-in off, and the package's host keys removed so each sandbox makes its own.
2. A sandbox from it, with `persist`, and a key added (or a profile key).
3. `sshd` started: as the template's own command (best, since it comes back after every start), or as a [named session](sessions.md#start) `sshd`, which makes host keys with `ssh-keygen -A` and runs `sshd -D -e`.
4. Port 22 exposed as `tcp`, persisted.
5. Connect to `host` on `host_port` as root, with `-o HostKeyAlias=sbx_…` so `known_hosts` remembers the sandbox, not the node's address.

**The CLI makes this one step:** `lewbox ssh sbx_…`.
- It reads the sandbox's port 22, and fails with what to do if there's none.
- It runs `ssh -p <host_port> -o HostKeyAlias=<sbx> root@<host>` with the user's own `ssh`, so their agent and keys work.
- `lewbox ssh --config` prints a `Host` block for `~/.ssh/config` with a `ProxyCommand` of `lewbox ssh --proxy %h`, which looks the port up on each connect. That survives starts, and editors' remote modes use it ([sdks-and-cli](../sdks-and-cli.md)).

**Activity.** A connection through an exposed port is invisible to Lewbox. The docs keep saying to give such sandboxes a longer idle timeout, or none.

### Later: an SSH gateway

An SSH server in Lewbox, so `ssh sbx_Qm4pL0sX2bN8cV@ssh.lewbox.com` reaches a sandbox with no `sshd` in it and no exposed port.

- **Where it runs:** in `lewbox serve` (`LEWBOX_SSH_ADDR`, e.g. `:22` on its own address), or on each node in `lewbox node`, so traffic doesn't cross to the control plane. The node is better for latency and for [tunnelled clusters](../clusters/connectivity.md).
- **Auth:** the client's public key must be one of the sandbox's or its profile's keys. The username is the sandbox id. The gateway's host key is one per instance, so `known_hosts` is set once.
- **Inside:** a session is an [interactive command](commands.md#interactive) with `pty` (a login shell), or the requested command. `subsystem sftp` runs `sftp-server` in the sandbox when it has one. Port forwarding (`-L`) dials `127.0.0.1:<port>` in the sandbox through the toolbox, which gives editors' remote modes what they need.
- **Activity:** counts as activity like any interactive command, and writes `terminal.opened` and `terminal.closed`, so the idle timeout and the log see it.
- **AerolVM may already have this.** AerolVM counts "SSH activity" for idle timeouts, and its toolbox has "SSH attach" that reuses a session by name (`GetOrCreate`). Before building our own, find out what AerolVM's SSH is and whether the fork can serve it with Lewbox's key checks ([open question 4](../inside-a-sandbox.md#open-questions)).

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | Adding or removing without `sandboxes.exec` |
| `404` | `not_found` | No such sandbox |
| `409` | `invalid_state` | A deleted sandbox |
| `422` | `validation_failed` | A bad name or key, a private key, options, a key on the profile, on the sandbox under another name, or a 21st key |
| `502` | `sandbox_error` | Saved, but `authorized_keys` couldn't be written. Retry |
| `503` | `toolbox_unavailable` | |

## Security

- **Keys only.** The guide's template turns password sign-in off, and the docs say never to give root a password on a public port.
- **No options** in keys, so a key can't be given a forced command that hides what it does, or a `from=` that the project didn't choose.
- **Removing a key takes effect at once** for new connections; open ones aren't closed. With the gateway, Lewbox can close them.
- **Public `tcp` ports are reachable by anyone,** so `sshd` is the only thing keeping people out. The gateway removes the exposed port.

## Observability

- **Activity:** `ssh_key.added`, `ssh_key.removed` (with name and fingerprint); with the gateway, `terminal.opened`/`closed` with `via: ssh` and the key's fingerprint.
- **Metrics:** `lewbox_ssh_key_syncs_total{outcome}`; with the gateway, `lewbox_ssh_connections_open`, `lewbox_ssh_auth_failures_total`.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_SSH_KEYS_PER_SANDBOX` | `20` | |
| `LEWBOX_SSH_KEYS_PER_PROFILE` | `20` | |
| `LEWBOX_SSH_ADDR` | unset | Later: the gateway's listen address |

## Testing

- **Parsing:** a table of valid keys of each type, comments with control characters, every option refused, private keys (PEM and OpenSSH) refused, RSA under 2048 refused, a mislabelled type.
- **Block replacement:** empty file, a file without the block, with it, user lines above and below kept, removal with no keys, de-duplication.
- **Sync:** the conditional write retries on `412`; modes set; the `chmod` exit code lost under gVisor is checked by `Stat`.
- **API rules:** same name same key (`200`, no write), same key other name (`422`), profile key (`422`), the 21st key, deleted sandbox, removal idempotent.
- **Fan-out:** a profile key change syncs each running sandbox, and one failing doesn't stop the others.

## Build plan

1. `internal/sshkeys`: parse, normalize, fingerprint, block replace.
2. Migration (shared with profile keys), store, API handlers.
3. `Sync` through the files API; the profile setup step and the stop cleanup use it.
4. CLI `lewbox ssh` and `--config`/`--proxy`.
5. Later: investigate AerolVM's SSH, then the gateway.

## Open questions

1. **Gateway location:** control plane or node? A node gateway needs each node reachable on an SSH port, or DNS per cluster (`ssh.<cluster domain>`).
2. **Usernames:** `sbx_…@` is unambiguous but long. Allow a sandbox's metadata name, or the profile's alias, instead?
3. **Should the alpha's CLI `--proxy` mode tunnel over Lewbox's API** (an interactive command running `nc 127.0.0.1 22`, or a port-forward WebSocket), so SSH works without an exposed `tcp` port even before the gateway?
