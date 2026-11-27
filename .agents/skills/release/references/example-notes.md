# Example release notes

The real notes of Lewbox v0.3.4 (title: `v0.3.4: domain-mode clusters get their DNS records and register`), a fix-only release, so it has no Highlights section. Everything below the rule is the release body, exactly as published.

---

Domain-mode clusters with a DNS profile now register and get their DNS records. The v0.3.2 fix didn't work on real domain-mode nodes. It read the node's IP from AerolVM's `public_host`, which in domain mode is the domain itself.

### Fixed

- **`lewbox node add … --new-cluster` in domain mode still failed with `no such host`, and domain-mode clusters never got DNS records.** On v0.7.0, AerolVM reports a domain-mode member's `public_host` as the domain and the machine's IP as `data_plane_host`. Lewbox only read `public_host`. So `node add` had no IP to send, printing `! couldn't read this node's public IP`, and the orchestrator created no `A` records, because no member had an IP in `public_host`. Lewbox now takes a member's IP from `public_host` when that is an IP (IP mode), and otherwise from `data_plane_host` (domain mode):
  - `node add --new-cluster` prints `✓ this node's public IP is …` and sends that IP with the registration;
  - the orchestrator reaches the node there while the domain doesn't resolve, and creates `<domain>` and `*.<domain>` pointing at it as soon as the cluster is registered.

  A private `data_plane_host`, as behind a cloud NAT, still gets no record, and the `dns records` check now names that IP instead of the domain.
- The [DNS profiles](https://github.com/harlew-dev/lewbox/blob/v0.3.4/docs/platform/clusters/dns-profiles.md#the-records-lewbox-keeps) page no longer says the installer puts the machine's IP in `public_host`.

### Upgrade notes

- **No database migrations.**
- Update **both** the orchestrator (`lewbox update` or `lewbox-deploy`) and the CLI on the node you register from.
- **If `node add --new-cluster` already failed on a node:** run the same `lewbox node add` command again after updating. There's no need to reinstall. `init` doesn't start a second cluster, Lewbox's settings are already in place, and the unused `lewbox-<slug>` admin token is revoked and minted again.
- The orchestrator's resolver may have cached the failed lookups for up to about 30 minutes. Registration doesn't depend on it, but the cluster can show as unreachable until that cache expires.
- **Domain-mode clusters already registered with a DNS profile** (records added by hand) get Lewbox's records at their next check. Identical records are adopted rather than duplicated.
- AerolVM stays pinned at v0.7.0.

### Tested

- `make test`: gofmt, vet, Go tests, and 130 web tests. A real domain-mode `/v1/cluster/members` answer is now saved as `internal/aerolvm/testdata/members_domain.json`, with its IP and domain replaced. New tests cover reading the IP from it, records created at registration for a domain-mode cluster (with a private `data_plane_host` skipped), and `node add` reading the IP.
- `deploy/dev-cluster/e2e.sh` on Docker nodes: all 18 steps (171 checks). The dev cluster runs in IP mode, so domain mode is verified by the tests above and on a real node.

## What's Changed

- fix(dns): read a domain-mode node's IP from data_plane_host in 2e6de48

