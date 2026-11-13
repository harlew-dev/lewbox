Real responses from AerolVM v0.7.0, captured 2026-11-11 from a two-node
cluster (`proto-a` leader, `proto-b` joined) running in Docker. Secrets are
replaced with `REDACTED`.

| File | Request | Status |
|---|---|---|
| `health.json` | `GET /health` (no auth) | 200 |
| `whoami.json` | `GET /v1/whoami` with an admin token (`--role admin --team '*'`) | 200 |
| `members.json` | `GET /v1/cluster/members` | 200 |
| `members_domain.json` | `GET /v1/cluster/members` on a one-node **domain-mode** cluster (`install.sh --domain`), captured 2026-11-13 on a VPS. The public IP and domain are replaced with `203.0.113.10` and `eu.example.com`. `public_host` is the **domain**; the machine's IP is in `data_plane_host` | 200 |
| `token_create.json` | `POST /v1/tokens {"name","role","team","expires_in":"36500d"}` | 201 |
| `tokens_list.json` | `GET /v1/tokens` | 200 |
| `join_token_create.json` | `POST /v1/cluster/join-tokens {"role":"server","ttl":"15m"}` | 201 |
| `err_unauthorized.json` | any route with a bad token | 401 |

Other answers seen, not saved as files:

| Request | Status | Body |
|---|---|---|
| `DELETE /v1/tokens/{id}` | 204 | empty |
| `DELETE /v1/cluster/nodes/{unknown}` | 404 | `{"error":"cluster: unknown node"}` |
| `POST /v1/cluster/nodes/{unknown}/drain` | 204 | empty (it does not check the node exists) |
| `POST /v1/tokens` just after start, before raft has a leader | 503 | `{"error":"the token store did not confirm the write; after a create the token may exist: list tokens, revoke it by id, then create it again"}` (log: `API token store unavailable: cluster: not raft leader`). Retry. |
| any `/v1/cluster/*` route on a node that is not in cluster mode | 503 | `{"error":"cluster: not enabled on this node"}`. Do not retry. |
| `GET /v1/whoami` with the PAT from another host when `SB_PAT_SCOPE=host` | 401 | `{"error":"the operator token works only on this host; sign in with ` + "`aerolvm login --sso`" + ` or use a named token"}` |
| `GET /v1/whoami` with a valid token on a node without `SB_IDENTITY_ENABLED=true` | 401 | `{"error":"unauthorized"}` |
| `GET /v1/cluster/upgrade` on a cluster that never upgraded | 404 | `{"error":"not_found","reason":"this cluster never had a rollout"}` |

Errors are `{"error": "<text>"}` and sometimes `{"error": "<code>", "reason": "<text>"}`.
A new token can take a few seconds to work on the other nodes.
