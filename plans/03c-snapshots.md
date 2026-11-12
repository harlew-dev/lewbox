# Phase 3c: customers' snapshots per region

**Asked for on 2026-11-12.** A customer (and the operator) can see the snapshots the customer owns in each region, and delete them.

## AerolVM facts (v0.7.0 source; see the research notes in DECISIONS D7)

- `GET /v1/snapshots?limit=500&starting_after=<name>` → `{"snapshots":[…],"has_more":bool}`. Each: `name`, `image`, `image_id`, `source_sandbox_id`, `created_at`, `cpu`, `memory_mb`, `disk_gb`, `push_state`, `push_error`, `image_registry_ref`. **No owner and no size.**
- It is **node-local**: each node lists only its own snapshots. Ask every member's `api_url`.
- With the cluster **admin** token the list mixes every team's snapshots with no way to tell them apart, and `GET/DELETE /v1/snapshots/{name}` resolve names in the admin's own (empty) namespace, so the admin token can't address a customer's snapshot.
- A token **bound to the team** (`viewer` or `runner`) sees and addresses only that team's snapshots. That is how Lewbox gets exact attribution.

## Design (DECISIONS D23)

- **Short-lived viewer tokens, kept in memory.** Snapshots belong to the workspace's AerolVM team. To list them in a region, Lewbox mints `POST /v1/tokens {"name":"lbx.view.<org id>.<unix time>","role":"viewer","team":<workspace team>,"expires_in":"1h"}`, keeps it **in memory only**, and reuses it for 50 minutes. Nothing is stored; tokens expire on their own. Every member of the workspace sees the workspace's snapshots (as with sandboxes).
- **Listing on demand, not in the sync loop.** `GET /v0/snapshots?region=` asks every node of the region (bounded concurrency 4, 5 s timeout each) with the viewer tokens, merges the pages, and caches the result per (workspace, region) for 30 s in memory. A node that doesn't answer is listed in `partial_nodes`.
- **Deleting.** `DELETE /v0/snapshots/{name}?region=` mints a `runner` token for the workspace team with `expires_in: "1h"`, deletes `DELETE /v1/snapshots/{name}` with it (owner-scoped, so it can only hit the customer's own), then revokes the token (with `context.WithoutCancel`). Owners, admins and members of the organization, the org API key, and instance admins may delete; viewers may not.
- **No storage billing.** AerolVM reports no snapshot size, so snapshots aren't charged (D9 caveat stands).

## API

```json
// GET /v0/snapshots?region=eu-west   (Lewbox-Organization header; region optional = every subscribed region)
{"data":[{"name":"ckpt-1","region":"eu-west","source_sandbox_id":"sb-…","image":"…","created_at":"…",
          "cpu":1,"memory_mb":1024,"disk_gb":10,"push_state":"active"}],
 "partial_nodes":[]}
```

CLI: `lewbox snapshot list [--region] [--org]`, `lewbox snapshot delete NAME --region R [--yes] [--org]`.

## Web

- Portal: **Snapshots** page (between Sandboxes and API keys): table with region, name, source sandbox, created, size settings, push state; delete with confirmation; empty state "Snapshots you take with the SDK (`sandbox.snapshot()`) show up here."
- Console: a **Snapshots** tab on the customer detail page.

## Done when

- The fake serves `/v1/snapshots` per member with team scoping for team-bound tokens; tests cover attribution, merge across nodes, partial nodes, cache expiry, delete with a temporary token that is revoked, token reuse and expiry in the memory cache, scoping (another org → 404).
- `customers.sh` gains a step: customer a snapshots a sandbox with its key (`POST /v1/sandboxes/{id}/snapshot` — check the exact route in AerolVM's routes first), Lewbox lists it for a and not for b, a deletes it through Lewbox, it's gone.
