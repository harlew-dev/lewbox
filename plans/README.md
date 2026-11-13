# Plans

Lewbox's goal: **a self-hosted E2B.** An operator installs AerolVM on their own servers, and Lewbox turns that compute into a sandbox service that other people sign up for, get API keys for, and use with the AerolVM SDK (or the E2B-compatible SDK). The operator sets plans and limits, and sees customers, usage and the health of every server.

| File | What |
|---|---|
| [00-product.md](00-product.md) | The product in one page: who uses it, what they see, the words we use |
| [01-customers-and-plans.md](01-customers-and-plans.md) | Phase 1. Sign-up, customer accounts, plans and limits, instance settings |
| [02-sandbox-keys.md](02-sandbox-keys.md) | Phase 2. Per-region sandbox API keys minted on the clusters |
| [03-sync-and-metering.md](03-sync-and-metering.md) | Phase 3. Sandbox mirror, limits synced across clusters, usage metering |
| [03b-region-plans.md](03b-region-plans.md) | Phase 3b. A plan per region, assigned by the operator (replaces shared limits) |
| [03c-snapshots.md](03c-snapshots.md) | Phase 3c. Customers' snapshots per region, listed and deleted through Lewbox |
| [03d-member-shares.md](03d-member-shares.md) | ~~Phase 3d. Member shares, one AerolVM team per member~~ Built, then reverted (D29) |
| [04-monitoring.md](04-monitoring.md) | Phase 4. 30-day cluster and node health, uptime, fleet metrics |
| [05-billing.md](05-billing.md) | ~~Phase 5. Prices, credits, automatic suspension~~ Dropped (D24) |
| [06-web.md](06-web.md) | Phase 6. Customer portal and operator console (UI) |
| [07-review-and-release.md](07-review-and-release.md) | Phase 7. Review for bloat, docs, e2e, release packaging |
| [08-dns-storage-archive.md](08-dns-storage-archive.md) | Phase 8. DNS and storage profiles, the registry on nodes, archiving stopped sandboxes |
| [DECISIONS.md](DECISIONS.md) | Every decision made while building, the alternatives, and the caveats |
| [RELEASING.md](RELEASING.md) | How to release and put this on live servers |
| [STATUS.md](STATUS.md) | What was built, what was verified, and what is left |
| [aerolvm-push-on-stop-request.md](aerolvm-push-on-stop-request.md) | Request to AerolVM: archive a stopped sandbox to the registry and start it on any node with the same ID |
| [missing.md](missing.md) | What the archived rebuild design had that isn't built here, and whether each fits |

How the work was done: each phase was written as a brief, implemented by an OpenCode Go agent (DeepSeek) or a Claude sub-agent (UI), then reviewed and checked (`make test`, and `deploy/dev-cluster/e2e.sh` for cluster work) before the next phase started. Each phase is one or more commits.
