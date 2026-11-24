---
title: Incident response
description: Detect, contain, rotate secrets, restore from backup, and notify after a Lewbox incident.
---

This is the runbook for the person on call: a leaked credential, a taken-over account, a compromised orchestrator, a bad deploy or an outage. It uses only commands Lewbox has; where a step is not built, it says so.

**Write down the time you became aware.** The GDPR 72-hour clock for notifying a supervisory authority starts then, not when the incident started.

## 1. Preserve evidence first

Before you restart, restore over, or clean anything, take a copy of the live state. It contains personal data and secrets: keep it private and with the incident record.

```bash
d=/root/incident-$(date -u +%Y%m%dT%H%M%SZ)
sudo install -d -m 0700 "$d"
sudo sqlite3 /srv/lewbox/lewbox.db ".backup '$d/lewbox.db'"
sudo cp -a /srv/lewbox/keyring.key "$d/keyring.key"
sudo docker compose --project-directory /opt/stack logs --no-color --since 7d > "$d/lewbox.log" 2>&1
sudo docker compose --project-directory /opt/stack ps > "$d/containers.txt" 2>&1
```

Use `.backup`, not a plain copy of a live SQLite file. Do not run `lewbox-deploy backup` over the top; keep this copy separate from the nightly rotation.

The daily prune (31-day samples, expired sessions, 400-day usage) keeps running while the orchestrator is up. There is no pause switch, so copy first. Truncated logs rotate at 3 × 10 MB per container.

## 2. Detect

Signals to check:

- the external uptime monitor and `curl -fsS https://DOMAIN/v0/health`;
- `lewbox status --check` and `lewbox cluster check SLUG`;
- `lewbox-deploy status`, `docker compose ps`, and container logs (one access line per request: method, route pattern, status, duration, request ID);
- a report from a customer, a researcher, or the hosting or DNS provider.

Access lines deliberately hold no account, IP, path or body. Account and IP scoping has to come from the database and from what the affected people tell you.

## 3. Triage

Answer: what happened, whose data, over what time, and is it still happening?

```bash
lewbox user list --json
lewbox org list --json
lewbox org get SLUG --json
lewbox member list --org SLUG --json
lewbox token list --user EMAIL --json
lewbox admin-key list --org SLUG --json
lewbox key list --org SLUG --json
lewbox setup-link list --json
lewbox cluster list --json
lewbox sandbox list --all --json
```

A request ID from a log line or an error body finds the other lines of that request. Compare account creation, token creation and last-use times with when the incident started. A disabled account has no sessions or access tokens left; re-enable it only deliberately.

## 4. Contain

These are the containment tools that exist. All CLI commands act on the orchestrator's own machine in local mode, or on a remote context with a signed-in instance admin.

| Situation | Do |
|---|---|
| An account is taken over or abusing the service | `lewbox user disable EMAIL --yes`. It ends every session, revokes every access token and revokes live sandbox keys the account created. It does **not** revoke workspace admin keys the account created: list them with `lewbox admin-key list --org SLUG` and revoke any you do not trust. Confirm the owner out of band before re-enabling |
| An account's sessions may be stolen but the owner is fine | Disable and re-enable the account, or change the password with `lewbox user set-password EMAIL --password-stdin`, which ends the account's other sessions |
| An `lbp_` access token leaked | `lewbox token revoke ID\|NAME --user EMAIL --yes` |
| A workspace `lba_` admin key leaked | `lewbox admin-key revoke ID\|NAME --org SLUG --yes` |
| A sandbox `avm_` key leaked | `lewbox key revoke ID\|NAME --org SLUG --yes`. This also revokes it upstream on the cluster |
| A setup link leaked | `lewbox setup-link revoke ID --yes`. A used link cannot be revoked |
| A workspace is abusing the service | `lewbox org suspend SLUG`. It blocks new sandboxes and keys; the sync loop stops running ones |
| A profile credential (Cloudflare, S3) leaked | Rotate at the provider, then `lewbox dns-profile update NAME --api-token-stdin` or `lewbox storage-profile update NAME --secret-access-key-stdin`, and run `lewbox node configure` on every node of each cluster that uses it |
| The orchestrator host is compromised | Stop it: `sudo docker compose --project-directory /opt/stack stop lewbox`. Preserve evidence, then restore a known-good backup on a rebuilt host. Treat every secret below as exposed |
| A bad release | `lewbox-deploy rollback` (the previous image). The database is **not** restored, and migrations do not roll back |

A workspace's own keys stop working when the workspace is suspended, but a **leaked cluster admin token is not something Lewbox can rotate**: no command replaces the token stored for an existing cluster. Revoke the `lewbox-<slug>` token inside AerolVM (`DELETE /v1/tokens/{id}` on the cluster's admin API), mint a new admin token there with the node's operator token, then remove and re-add the cluster with the new token. Removing and adding the cluster deletes its nodes, sandboxes, archives and samples (archive repositories in the bucket are left behind), so it is a last resort; otherwise accept the gap and plan the rotation.

### Rotate secrets, in order

| Secret | Where it lives | How to rotate |
|---|---|---|
| Account password | `users.password_hash` | `lewbox user set-password` |
| CLI and browser sessions | `sessions` hashes | Disable/re-enable the account, or change its password |
| Access tokens (`lbp_`) | `access_tokens` hashes | `lewbox token revoke` |
| Workspace admin keys (`lba_`) | `admin_keys` hashes | `lewbox admin-key revoke` |
| Sandbox keys (`avm_`) | `sandbox_keys` plus the cluster | `lewbox key revoke` |
| Setup links | `setup_links` hashes | `lewbox setup-link revoke` |
| Cloudflare token | `dns_profiles.api_token_enc`, sealed | `lewbox dns-profile update … --api-token-stdin` |
| S3 secret | `storage_profiles.secret_key_enc`, sealed | `lewbox storage-profile update … --secret-access-key-stdin` |
| Cluster admin token | `clusters.admin_token_enc`, sealed | Not supported; see above |
| The keyring | `keyring.key` beside the database | Not supported; see below |

The keyring is one AES-256-GCM key with no key IDs and no rotation. **If both the database and the keyring file were exposed**, assume every sealed value is readable: cluster admin tokens, DNS tokens and storage secrets. Rotate at each provider as above, and re-enter the cluster tokens by re-registering clusters. If only the keyring file was exposed, without the database it cannot decrypt anything, but replace it from a known-good backup anyway. **If the keyring is lost or replaced**, every sealed value becomes unreadable; restore the matching keyring from backup.

## 5. Restore from backup

Backups are the database and keyring together. They must come from the same moment, or sealed values will not open. The VPS setup takes a nightly backup at 03:30 UTC and one before each deploy.

```bash
cd /opt/stack
b=/var/backups/lewbox/pre-v0.2.0-20261112T120000Z   # or a nightly one
sudo docker compose stop lewbox
sudo install -m 0600 -o lewbox -g lewbox "$b/lewbox.db" /srv/lewbox/lewbox.db
sudo install -m 0600 -o lewbox -g lewbox "$b/keyring.key" /srv/lewbox/keyring.key
sudo rm -f /srv/lewbox/lewbox.db-wal /srv/lewbox/lewbox.db-shm
sudo lewbox-deploy v0.2.0                            # the version that wrote the backup
```

Confirm `https://DOMAIN/v0/health` is OK, then `lewbox status --check` and `lewbox cluster list`. A restored database brings back accounts, sessions and tokens that were deleted or revoked since the backup: disable or revoke them again before reopening access. If the compromise was in the orchestrator itself, also rotate the provider credentials that the host could read and rebuild from a clean image.

## 6. Notify

Lewbox has no email, so every notice goes out through a channel you control and can verify.

- A personal-data breach is personal data destroyed, lost, altered or disclosed. Notify the supervisory authority within **72 hours** of becoming aware, unless it is unlikely to put people at risk. Notify affected people without undue delay when the risk is high.
- A customer is the controller of its workspace data and Lewbox's operator is its processor. Check the contracts: they often set 24–48 hours.
- Collect the affected addresses with `lewbox user list --json` and `lewbox member list --org SLUG --json`. Lewbox holds no phone numbers or postal addresses.
- Say what happened, what data was involved, what you did, and what the person should do (change the password, revoke keys). Do not describe which defence failed.
- Never claim more than you know. If scoping is incomplete, notify what you know and update it.

## 7. Record and review

Keep an incident record with the time you became aware, the timeline, affected accounts and workspaces, decisions and who made them, the evidence copy, and why you did or did not notify. Put every breach in the breach register, including unnotified ones.

Within two weeks, hold a blameless review: what would have detected it sooner, what slowed containment, and which fix has which owner. Add missing steps to this runbook.

## Known gaps

- No audit or event log, and access lines carry no account or IP, so scoping needs the database and the affected people.
- No command rotates a cluster's stored admin token, the keyring, or all sessions at once.
- No email, so notification and password recovery are manual.
- Backups are local unless copied off the server; a compromised host may have lost both the live data and the local backups.
- Revoked rows and destroyed sandboxes are kept, which helps scoping but means deletion is not erasure from the database.
