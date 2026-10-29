# PKI and DNS

> Status: Draft · Milestone: Alpha (M1; delegated DNS-01 in M4) · Core PKI; DNS through the `dns/libdns` plugin (Cloudflare by default, and the other libdns providers) · Updated: 2026-10-29

Two kinds of trust hold a cluster together. **The cluster CA** signs every node's certificate for AerolVM's internal mTLS (Raft, gossip and the internal channel). Lewbox holds it, signs each node's certificate as it joins, and renews them. **Public TLS** lets the control plane and browsers reach nodes over HTTPS: each node has its own subdomain with a wildcard certificate, issued by Let's Encrypt through DNS-01. This plan covers both, plus the DNS records Lewbox keeps for nodes, and the DNS providers it supports.

## Today

### The cluster CA (`lewbox-legacy/docs/clusters.md` §§ *How nodes join*, *CA key in Lewbox*, *6. Renewing certificates*)

- A cluster is four shared secrets: the operator PAT, a gossip key, the cluster CA, and a credential encryption key.
- **On the seed,** `cluster-init.sh` generates the CA (10 years) and the credential key; the seed uploads `ca.crt`, `ca.key` and the credential key to the app (`PUT /api/node/v1/bundles`). The app keeps them in the cluster's `ClusterSecrets` credential, encrypted with `APP_KEY`.
- **On joining nodes,** `cluster-join.sh` runs in two passes:
  1. make `node.key` and `node.csr`, then exit `2`;
  2. after **the app signs the CSR itself** (`PUT /api/node/v1/certificate`) as `node:<id>` for 90 days, install the certificate.

  The node id comes from the app's records, never the CSR. Upstream's `cluster-sign-node.sh` trusts its `--node-id` flag, and a node choosing its id could impersonate another node over cluster mTLS.
- **AerolVM checks** each certificate is in date and names `node:<id>`, but **never renews** one. It reloads a replaced `node.crt` on its next connection, for the same id only.
- **Renewal:** a daily timer on each node renews within 30 days of expiry. The app records `certificate_expires_at`, and notifies the tenant at 14 days. Built, but no node had renewed on its own yet.
- **`APP_KEY` guards every cluster's CA:** lose it, and no cluster can add nodes or renew.

### DNS (`docs/clusters.md` § *DNS*)

- **Optional per cluster:** provider (Cloudflare only, as AerolVM's Caddy supports only it), domain, and an API token with Zone:Read and DNS:Edit. The dashboard tests it first: find the zone, then create and delete `_lewbox-connection-test.<domain>` TXT.
- **Records per node:** `node-<id>.<domain>` and `*.node-<id>.<domain>`, A or AAAA, TTL 60, DNS only (never proxied: AerolVM routes by TLS server name), commented `Lewbox node_…`. A record with the same name and type is replaced, so reinstalling repoints it.
  - The node's subdomain uses its id with a hyphen, as **names with underscores can't get certificates** (`Node::subdomainOf()`), or a subdomain chosen before enrolling.
- **Refused:** private or reserved IPs, or a Cloudflare refusal such as a CNAME on the name (`502` with Cloudflare's reason).
- **The token is copied to every node,** for AerolVM's Caddy to solve DNS-01 (`/etc/default/caddy`). It can edit the whole zone, so the modal asks for one scoped to that zone.
- **Not built:**
  - removing a node's records when it's removed;
  - `--acme-email`;
  - handling Let's Encrypt's limit of 50 new certificates per registered domain per week (each node takes two).

## Goals

- Keep Lewbox as the cluster's CA, signing by node id from its own records.
- Renew node certificates reliably. They're the one thing that cuts a node off from its cluster without anyone touching it.
- Make the CA survive key rotation on the control plane, and make the CA itself rotatable.
- Support DNS providers beyond Cloudflare.
- Stop copying a zone-wide DNS token to every node.
- Clean up DNS when nodes go away.

### Non-goals

- A public CA of our own. Public certificates come from ACME (Let's Encrypt, or ZeroSSL as a fallback).
- Managing a project's whole zone. Lewbox only touches the records it creates.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Lewbox holding the cluster CA and signing CSRs by node id; 90-day node certificates; renewal by the node; one subdomain per node with its wildcard; records DNS-only, TTL 60, commented; refusing private IPs in direct mode; connection tests before saving |
| **Change** | Secrets under `APP_KEY` → the keyring ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), rotatable without re-keying clusters. Daily renewal check → hourly, by the node agent, with retries. Cloudflare only → any provider in [libdns](https://github.com/libdns), behind one interface. The DNS token on every node → **DNS-01 through the control plane** |
| **Add** | CA rotation ([F42](aerolvm-fork.md#api-clustering-and-lifecycle)). Removing records with the node. `acme_email`. The instance's relay domain for tunnel clusters ([connectivity](connectivity.md)) |
| **Drop** | Nothing |

## Design

### The cluster CA

- **Generated by the seed** (`cluster-init.sh`), as AerolVM expects, then uploaded with its key and the credential key (`PUT /node/v0/bundles`, [installation](installation.md#steps)). Validated on upload: the key matches the certificate, the certificate is a CA, and the credential key is 32 bytes.
- **Stored** in `cluster_secrets` (one row per cluster, each field encrypted with the keyring):
  - `pat`, `gossip_key`;
  - `ca_certificate`, `ca_key`, `credential_key`;
  - their `rotated_at`.
- **Why Lewbox holds `ca.key`:**
  - Lewbox already holds the PAT, gossip key and credential key, which together are full control of the cluster.
  - Holding the CA too means no signer service on the seed, joining doesn't depend on the seed being up, a lost seed doesn't stop the cluster growing, and Lewbox can renew.
  - The project has root on its nodes, so they can read these secrets too; that's acceptable, as the cluster is theirs. **Never reuse a secret across clusters,** and never put Lewbox's own credentials on a node.

### Node certificates

`PUT /node/v0/certificate` (node token) with `{certificate_request: <PEM>}`:

1. Parse the CSR, and check its signature and key type (ECDSA P-256 or Ed25519, as AerolVM makes them; RSA ≥ 2048 accepted).
2. **Ignore every name in it.** The certificate names `node:<node id>` as a URI or DNS SAN, as `cluster-sign-node.sh` does, from the authenticated node's row.
3. Sign with the cluster CA for `LEWBOX_CERTIFICATE_LIFETIME` (90 days), with a random serial and `NotBefore` back-dated 5 minutes for clock skew.
4. Record `nodes.certificate_serial`, `certificate_expires_at`; resolve open `certificate_expiring` notifications.
5. Answer with `{certificate, ca_certificate}`.

Refused:

| Status | Code | When |
|---|---|---|
| 409 | `cluster_has_no_bundles` | The seed hasn't uploaded the CA yet |
| 422 | `invalid_certificate_request` | Not a PEM CSR, a bad signature, or a weak key |

A joining node calls this between `cluster-join.sh`'s passes. The seed calls it once after `cluster-init.sh`, to replace the certificate it signed itself, so Lewbox knows every node's expiry.

### Renewal

By the node agent ([node-agent](node-agent.md#certificates)):

- **Check every hour;** renew once under `LEWBOX_CERTIFICATE_RENEW_WITHIN` (30 days) is left.
- **On failure,** retry every hour.
- **Before replacing,** verify the new certificate chains to the cluster CA, names `node:<this id>`, and matches `node.key`. Replace with one atomic rename. AerolVM reloads it on its next connection, with no restart.
- **The control plane watches expiry:**
  - a daily job raises `certificate_expiring` at 14 days left, one open notification per node;
  - `certificate_expired` once it has expired, which means the node is cut off from its cluster;
  - both go to the dashboard, email to project admins, and webhooks (`node.certificate_expiring`).
- **Revocation:** AerolVM has no CRL. A removed node's token stops working, so it can't renew, and its certificate runs out within 90 days. To shorten that, the lifetime can be lowered (e.g. 30 days) now that renewal runs hourly from a daemon (open question 1).

### Rotating the CA

Needed if `ca.key` may have leaked, or near the CA's 10 years. It requires AerolVM to trust several CAs at once ([F42](aerolvm-fork.md#api-clustering-and-lifecycle)), so it comes after that fork fix.

1. **Generate a new CA** on the control plane (it holds keys; no seed involved).
2. **Push a trust bundle** with both CAs to every node (a spec change); wait until all have applied it.
3. **Reissue every node's certificate** from the new CA (`renew_certificate` action).
4. **Push a bundle** with only the new CA.
5. **Delete the old CA key.**

A dashboard action, *Rotate cluster CA*, shows each phase. Until F42 lands, rotation means re-keying the cluster: a new CA, and every node rejoined, one at a time, with drain.

### Rotating the other secrets

| Secret | How |
|---|---|
| PAT | `rotate_secret` to every node at once, then switch the control plane. AerolVM reads the PAT file at start, so sandboxd restarts one node at a time; during the switch the control plane tries the new PAT, then the old, for 10 minutes. Also the registry password |
| Gossip key | AerolVM's gossip keyring supports adding a key before removing the old one; confirm, then add, switch, remove |
| Credential key | Encrypts sandbox secrets in AerolVM; rotating needs AerolVM support. Not offered in the alpha |

### DNS providers

DNS is behind a **DNS provider** extension point ([plugins/dns-providers](../plugins/dns-providers.md)). Core (`internal/dns`) owns the interface, records, the nightly check and the connection test; providers come from **one plugin, `dns/libdns`** (decided 2026-10-29), which wraps [libdns](https://github.com/libdns) packages, the same ones Caddy's DNS modules use. Each provider registers its id, its credential fields (a JSON Schema the dashboard renders) and a constructor, about 20 lines. Which providers are compiled in is set in `plugins/config.json`; the default build has Cloudflare (supported) and Route 53, DigitalOcean, Hetzner, Google Cloud DNS, Azure, Gandi, Porkbun, deSEC and RFC 2136 (experimental until each passes the live suite). A host without a libdns package can still be its own plugin. Adding a provider never touches core. A cluster whose provider's plugin is turned off keeps its records and settings, but nothing new is enrolled on it until the plugin is back (`409 plugin_disabled`).

**AerolVM doesn't need per-provider support:** certificates' DNS-01 challenges are relayed through the control plane ([below](#dns-01-through-the-control-plane)), so the fork's Caddy only ever talks to Lewbox. Until that relay (F43) is in the fork, direct clusters can only use providers whose Caddy module the fork's Caddy includes, which is Cloudflare ([plugins/dns-providers](../plugins/dns-providers.md#aerolvm-and-the-fork)).

The interface:

```go
type Provider interface {
    Zone(ctx context.Context, domain string) (string, error)                 // the closest zone the credentials see
    SetRecords(ctx context.Context, zone string, rrs []Record) error         // create or replace, by name+type
    DeleteRecords(ctx context.Context, zone string, rrs []Record) error
}
```

- **Backed by libdns:** Cloudflare, Route 53, DigitalOcean, Hetzner, Google Cloud DNS, Azure, Gandi, Porkbun, deSEC, and RFC 2136 for self-run servers, all inside `dns/libdns`. Cloudflare is supported in the alpha (M1); Route 53, Hetzner and RFC 2136 become supported as they pass the live suite (M3); the rest stay experimental until they do.
- **Per cluster,** `cluster_dns`:
  - `provider`, `domain`;
  - `credentials` (encrypted; the shape depends on the provider);
  - `acme_email`;
  - `tested_at`.
- **The connection test** is unchanged in spirit: find the zone, create and delete `_lewbox-connection-test.<domain>` TXT. It reports a refused credential, no zone found, or no permission to edit. It's throttled (10 a minute per project).
- **The instance's own domains** (the relay domain for tunnel clusters, [connectivity](connectivity.md)) use the same interface, configured with `LEWBOX_RELAY_DNS_PROVIDER` (any provider the build has) and its credentials ([connectivity § DNS for the relay domain](connectivity.md#dns-for-the-relay-domain); a built-in DNS server for it is a Later option).

### Records

When a node enrolls on a cluster with DNS (`PUT /node/v0/dns`, [installation](installation.md#steps)), or when its IP changes:

| Mode | Records |
|---|---|
| Direct | `node-<id>.<domain>` and `*.node-<id>.<domain>` → the node's public IP (A or AAAA). TTL 60. DNS only. Comment `Lewbox node_…` (where the provider supports comments) |
| Tunnel | `node-<id>.<relay domain>` and `*.node-<id>.<relay domain>` → the relays' addresses. Or none, if the relay domain has one wildcard for every node |

- **The subdomain** is `node-<id>` (lowercased, with a hyphen, since underscores can't get certificates), or the one chosen on the installation:
  - one lowercase DNS label;
  - unique among live nodes on the same domain (in any cluster, as clusters may share a domain);
  - reusable once a node has failed or been removed.
- **Refused:** a private or reserved IP in direct mode; a provider refusal (`502 dns_provider_refused`, with the provider's reason).
- **Removing a node deletes its records** (new). A failed node's records are deleted when its installation is cancelled or a new node takes its subdomain.
- **A nightly job checks records against nodes:**
  - records for nodes that are gone are deleted, but only records with Lewbox's names in the cluster's domain;
  - nodes missing records are repaired.

### Public TLS on nodes

- **AerolVM's Caddy** (installed by its `install.sh`, `--domain`) terminates TLS on 443 for `node-<id>.<domain>` (the API) and `*.node-<id>.<domain>` (sandbox ports), routing by server name (caddy-l4).
- **Certificates come from Let's Encrypt** by DNS-01, as wildcards can't be issued any other way.
- **`acme_email`** is passed (`--acme-email`), so Let's Encrypt can warn before certificates expire.
- **Rate limits:** Let's Encrypt allows 50 new certificates per registered domain per week, and each node takes two (the name and its wildcard; one certificate with both names would halve that, which is a fork change to Caddy's config).
  - A project adding many nodes on one domain, or the hosted relay domain, can hit it. Renewals don't count.
  - Mitigations:
    - one certificate per node holding both names (fork);
    - ZeroSSL as a fallback issuer in Caddy;
    - for the relay domain, a request to Let's Encrypt for a higher limit.

#### DNS-01 through the control plane

Today each node holds a token that can edit its cluster's whole zone. Instead:

- **The fork adds a Caddy DNS provider, `lewbox`** ([F43](aerolvm-fork.md#api-clustering-and-lifecycle), proposed upstream as a generic "webhook" provider). It asks the node agent, which asks the control plane over its connection:
  - `POST /node/v0/acme-challenges {name, value}` to set a TXT record;
  - `DELETE /node/v0/acme-challenges/{id}` to remove it.
- **The control plane only sets `_acme-challenge.<name>`** where `<name>` is that node's own subdomain or its wildcard. Anything else is refused and logged.
- **It uses the cluster's provider credentials,** which never leave the control plane.
- **The same mechanism serves tunnel nodes** on the relay domain, which a project must never hold credentials for.
- **Until F43 is in the fork,** direct clusters fall back to legacy's way: the provider token is copied to nodes for Caddy's own DNS module, with a dashboard warning that it should be scoped to the zone. That only works for providers whose Caddy module the fork's Caddy includes, which is **Cloudflare**; a direct cluster on another provider needs a fork version with F43 (`422 validation_failed` on `provider` otherwise). F43 makes it unnecessary to compile more Caddy DNS modules into the fork.

### Node tokens

Not certificates, but the node's credential for the control plane:

- `lbn_` + 64 hex ([foundation/ids-and-keys](../foundation/ids-and-keys.md)), given once at enrollment, stored hashed.
- Rotated every 90 days, and on demand, through the agent's connection: the new token is sent, the old one is accepted for 10 more minutes ([node-agent](node-agent.md#security)).
- Revoked when the node is removed (`401`) or fails (`403`).
- An alternative is mTLS from the agent with a certificate from an instance-wide "node identity" CA (open question 3).

## API

Admin API (paths per [api](../api.md)):

| Method and path | What |
|---|---|
| `PUT /v0/projects/{prj}/clusters/{clu}/dns` | Set or change the cluster's DNS: `provider`, `domain`, `credentials`, `acme_email`. Tested first; `422 dns_connection` with the reason on failure. Changing the domain on a cluster with nodes is refused (`409 cluster_has_nodes`) |
| `DELETE …/clusters/{clu}/dns` | Only on a cluster with no nodes |
| `POST /v0/projects/{prj}/dns-tests` | Test credentials without saving (the dashboard's modal) |
| `POST …/clusters/{clu}/ca/rotation` | Start rotating the cluster CA; `GET` for progress |
| `GET …/nodes/{node}` | Includes `certificate: {expires_at, serial}` and `domain` |

Node API: `PUT /node/v0/dns`, `PUT /node/v0/bundles`, `PUT /node/v0/certificate`, `POST`/`DELETE /node/v0/acme-challenges`. See [installation](installation.md#node-api) and [node-agent](node-agent.md#node-api).

## Security

- **Keys at rest:** CA keys, PATs, gossip keys, credential keys and DNS credentials are encrypted with the keyring. Rotating the keyring re-wraps them; nothing on the clusters changes.
- **Signing is by authenticated node id only.** A node token can only get a certificate for its own node.
- **DNS credentials:** after F43 they stay on the control plane. Projects should still scope them to one zone, and the test reports the scope where the provider says.
- **Audit:** every certificate issued (node, serial, expiry, by which request), every DNS change, and every refused ACME challenge, in the cluster's activity.

## Observability

- **Node page:**
  - certificate expiry and serial;
  - last renewal and its result;
  - DNS records and when they were last checked;
  - Caddy's certificate status (from the agent).
- **Cluster page:** the CA's expiry; the DNS provider and the last test.
- **Metrics:**
  - `lewbox_node_certificate_expiry_seconds{node}`;
  - `lewbox_dns_changes_total{provider,result}`;
  - `lewbox_acme_challenges_total{result}`.
- **Notifications and webhooks:**
  - `node.certificate_expiring`, `node.certificate_expired`;
  - `cluster.dns_failed`.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_CERTIFICATE_LIFETIME` | `2160h` | Node certificate lifetime |
| `LEWBOX_CERTIFICATE_RENEW_WITHIN` | `720h` | Renew when this little is left |
| `LEWBOX_CERTIFICATE_WARNING` | `336h` | Notify when this little is left |
| `LEWBOX_DNS_PROVIDERS` | `cloudflare,route53,hetzner,rfc2136` | Providers offered |
| `LEWBOX_RELAY_DNS_PROVIDER` + credentials | | For the relay domain |

## Testing

- **Unit:**
  - CSR signing (names ignored, the node id used, weak keys refused);
  - certificate verification on the agent;
  - each DNS provider against a recorded fake;
  - subdomain rules;
  - the ACME challenge allow-list.
- **Integration:**
  - a seed's bundles, a joiner's CSR, and a renewal, with the fake AerolVM checking the chain;
  - CA rotation in each phase, with a fake that accepts a bundle of two CAs.
- **End to end:**
  - a real cluster on a test zone (Cloudflare): install, renew with `--force`, remove a node and see its records go;
  - delegated DNS-01 issuing a real certificate on staging Let's Encrypt.

## Build plan

1. **M1:**
   - `cluster_secrets` on the keyring;
   - bundle upload;
   - CSR signing;
   - the agent's renewal;
   - expiry notifications.
2. **M1:**
   - `internal/dns` (the interface, records, checks) and the `dns/libdns` plugin with Cloudflare;
   - records on enroll;
   - deleting on removal;
   - the nightly check;
   - the connection test.
3. **M3:** Route 53, Hetzner and RFC 2136 through `dns/libdns`'s live suite to supported; `acme_email`.
4. **M4:**
   - DNS-01 through the control plane (with F43);
   - relay-domain records for tunnel clusters.
5. **Beta:** PAT and gossip key rotation; CA rotation (with F42).

## Open questions

1. **Shorter node certificates** (30 days) now that renewal runs hourly from a daemon? This shortens how long a removed node can still talk to its cluster.
2. **One certificate per node** for the name and its wildcard, to halve Let's Encrypt usage: a change to how the fork configures Caddy.
3. **Node tokens vs agent mTLS:** should the node agent authenticate to the control plane with a client certificate instead of a bearer token?
4. **Should DNS be required for direct clusters,** or may projects bring their own certificates and records (an "unmanaged DNS" option)?
