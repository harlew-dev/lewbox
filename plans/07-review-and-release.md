# Phase 7: review, end-to-end test, release

**Goal:** the code is small and honest, the whole customer journey is proven against real AerolVM, and a release can be cut and installed on real servers by following [RELEASING.md](RELEASING.md).

## Review

Independent reviewers (Claude sub-agents that didn't write the code) read the diff since the baseline commit for:

- **Bugs and security:** tenant isolation (a customer can never see or act on another organization's sandboxes, keys, usage or credits; never sees clusters/nodes/admin URLs); secrets (sandbox keys shown once, never stored, never logged); auth on every route; races in sync and billing (idempotency of charges and grants).
- **Bloat and AI slop:** dead code, needless abstraction, comments that restate the code, duplicated helpers, over-long docs, unused API fields, components that should be library components. Anything not needed by a page or a test goes.

Findings are fixed (by opencode for mechanical work, by hand for subtle work) and re-checked.

## End to end

Extend `deploy/dev-cluster/e2e.sh` (real AerolVM v0.7.0 in Docker) with the customer journey:

1. Operator sets sign-up to `open`, creates a plan, makes it default.
2. A customer signs up over HTTP (cookie session), gets an organization on the plan.
3. The customer creates a sandbox key for the region; the key is printed once.
4. With that key, straight against the cluster's API (no Lewbox), the customer creates a sandbox and runs a command in it.
5. Within one sync interval, Lewbox lists the sandbox for that customer, and not for another customer.
6. A second create beyond the plan's sandbox limit is refused by AerolVM (`429`): the limit was pushed to the cluster.
7. The customer stops and deletes the sandbox from Lewbox; it's gone on the cluster.
8. Usage for the customer is non-zero; the operator overview shows it; the cluster health history has samples.
9. Suspending the customer blocks creates; restoring allows them again.
10. Revoking the key makes the cluster answer `401` to it.

## Release

- `make release VERSION=vX.Y.Z` builds `dist/lewbox_<version>_linux_{amd64,arm64}.tar.gz` (binary with the console embedded, LICENSE, README) and `dist/checksums.txt`; `make docker VERSION=…` builds the image.
- `lewbox version` prints the version; `/v0/health` reports it.
- RELEASING.md: how to tag, build, verify, install on a server (systemd + Caddy), upgrade (back up the DB first; migrations run on start), roll back, and the live smoke test.

## Done when

- `make test` passes; `e2e.sh` passes all steps on a fresh dev cluster.
- `plans/STATUS.md` records what was verified and what wasn't.
