# DNS providers

> Status: Draft · Milestone: Alpha (M1) for the interface, the `dns/libdns` plugin and **Cloudflare only**; the other providers after the alpha, with DNS-01 through the control plane (F43) · Updated: 2026-10-29

A DNS provider lets Lewbox manage records in a zone:

- each node's `node-<id>.<domain>` and its wildcard;
- the `_acme-challenge` records for its certificates, which the control plane sets on the node's behalf so DNS credentials never reach nodes;
- the connection test;
- the nightly reconcile.

The core owns *which* records exist and when ([clusters/pki-and-dns](../clusters/pki-and-dns.md)). A provider only knows how to talk to one DNS host.

**Decided (2026-10-29): DNS goes through [libdns](https://github.com/libdns), in one plugin, `dns/libdns`.** It bundles a curated set of libdns providers, with Cloudflare as the default. Which providers are compiled in is set in `plugins/config.json`. Adding one is a libdns import and a credentials schema, about 20 lines. A host without a libdns package can still be its own plugin implementing `dns.Provider` directly.

## Today

Legacy supports Cloudflare only, in three places:

- the app's connection test (`lewbox-legacy/app/Actions/Clusters/TestDnsConnection.php`);
- the node API's `PUT dns` (the app calls Cloudflare);
- `install.sh`, which gives each node the Cloudflare token for Caddy's DNS-01. That's why AerolVM's Caddy, built with the Cloudflare module only, has been the reason Lewbox supported nothing else.

## Goals

- **Many DNS hosts with almost no code.** libdns is the provider library Caddy's own DNS modules are built on, so Lewbox uses the same packages.
- **The alpha matches legacy: Cloudflare only** ([D16](../README.md#decisions)), with Caddy's own Cloudflare module on nodes. The interface and plugin are built in the alpha, so the rest is adding providers.
- **After the alpha, AerolVM never needs to know which DNS host a cluster uses.** DNS-01 is relayed through the control plane ([F43](../clusters/aerolvm-fork.md#api-clustering-and-lifecycle)), so the fork's Caddy only ever talks to Lewbox, not to the DNS host.
- **Credentials stay on the control plane** once F43 is in: they're a schema the dashboard renders, and with the relay they never leave the control plane. In the alpha the Cloudflare token is also on nodes, as legacy has it ([clusters/pki-and-dns](../clusters/pki-and-dns.md#dns-01-through-the-control-plane)).
- **Every provider passes the same contract suite.**

### Non-goals

- Hosting DNS ourselves.
- Records other than the ones nodes and the relay need.

## Design

### The plugin

```
plugins/dns/libdns/
  plugin.go               Register: one dns.Provider per compiled-in provider
  providers_gen.go        generated from plugins/config.json: imports only the providers listed
  providers/
    cloudflare.go         schema + constructor
    route53.go
    digitalocean.go
    hetzner.go
    googleclouddns.go
    azure.go
    gandi.go
    porkbun.go
    desec.go
    rfc2136.go
```

The plugin registers several providers. Each keeps its own id (`cloudflare`, `route53`, …), which is what `cluster_dns.provider` stores, so a cluster names a DNS host, not a plugin.

| Provider | libdns package | Default build (after the alpha) | Status |
|---|---|---|---|
| Cloudflare | `github.com/libdns/cloudflare` | yes | **supported** (M1); the default, and the only provider in the alpha |
| Amazon Route 53 | `github.com/libdns/route53` | yes | after the alpha (with F43); supported once the live suite passes |
| DigitalOcean | `github.com/libdns/digitalocean` | yes | experimental until the live suite passes |
| Hetzner | `github.com/libdns/hetzner` | yes | after the alpha (with F43); supported once the live suite passes |
| Google Cloud DNS | `github.com/libdns/googleclouddns` | yes | experimental |
| Azure DNS | `github.com/libdns/azure` | yes | experimental |
| Gandi | `github.com/libdns/gandi` | yes | experimental |
| Porkbun | `github.com/libdns/porkbun` | yes | experimental |
| deSEC | `github.com/libdns/desec` | yes | experimental |
| RFC 2136 (self-run BIND, Knot, PowerDNS…) | `github.com/libdns/rfc2136` | yes | after the alpha (with F43); supported once the live suite passes |

The others are after the alpha too, experimental until the live suite passes. **The alpha's build compiles in Cloudflare only.** *Experimental* means the dashboard labels it as such and the docs say it hasn't passed the live suite yet. It works the same way, and graduates to *supported* when it passes.

### Choosing providers

**At build time, in `plugins/config.json`:**

```json
{ "name": "dns/libdns", "package": "./plugins/dns/libdns", "enabled": true, "projects": "all",
  "options": { "providers": ["cloudflare", "route53", "digitalocean", "hetzner", "googleclouddns",
                             "azure", "gandi", "porkbun", "desec", "rfc2136"] } }
```

The generator writes `providers_gen.go` with one blank import per listed provider. A provider that isn't listed isn't in the binary, and neither are its dependencies (some, like Azure's and Google's, are large). A slim build lists only `cloudflare`.

**At the instance, in configuration only** (`[plugins."dns/libdns".settings] providers = ["cloudflare", "rfc2136"]`): which of the compiled-in providers this instance offers. The default is every compiled-in one. Like all instance plugin settings this is configuration, never a dashboard switch ([building-and-enabling](building-and-enabling.md#configuration)).

**For projects:** the cluster form offers the instance's providers. A project can't turn single providers on or off. It can turn `dns/libdns` off only if no cluster of its uses it.

### The interface

```go
package dns // github.com/harlew-dev/lewbox/plugin/dns

type Provider interface {
    Info() Info
    // New returns a client for one cluster's (or the relay's) credentials, validated against
    // Info().Credentials. Called per use; cheap. Must not do I/O.
    New(creds json.RawMessage) (Client, error)
}

type Info struct {
    ID          string   // "cloudflare": stored in cluster_dns.provider
    Title       string   // "Cloudflare"
    Credentials []byte   // JSON Schema: {api_token: secret, …}
    Help        string   // how to make a scoped token, with a link
    Status      Status   // Supported or Experimental
    Features    Features // Comments (records can carry a note), Proxy (provider has a proxy to keep off), MinTTL,
                         // CaddyModule (the Caddy DNS module name, for the pre-F43 fallback; "" if none)
}

type Client interface {
    // Zone finds the closest zone the credentials can see for a domain ("acme.dev" for "lewbox.acme.dev").
    Zone(ctx context.Context, domain string) (string, error)
    // SetRecords creates or replaces records by name and type, idempotently.
    SetRecords(ctx context.Context, zone string, rrs []Record) error
    // DeleteRecords deletes exactly these records; deleting one that's gone is not an error.
    DeleteRecords(ctx context.Context, zone string, rrs []Record) error
    // ListRecords lists records under a name (for the nightly reconcile); may be filtered client-side.
    ListRecords(ctx context.Context, zone, under string) ([]Record, error)
}

type Record struct {
    Name    string        // relative to the zone: "node-abc", "*.node-abc", "_acme-challenge.node-abc"
    Type    string        // A, AAAA, TXT
    Value   string
    TTL     time.Duration // 60 s for node records
    Comment string        // "Lewbox node_…" where Features.Comments
}
```

### A provider is about 20 lines

```go
// plugins/dns/libdns/providers/route53.go
package providers

import "github.com/libdns/route53"

func init() {
    Add(dns.Info{
        ID:          "route53",
        Title:       "Amazon Route 53",
        Credentials: route53Schema, // access_key_id, secret_access_key (x-lewbox-secret), region
        Help:        "An IAM user with route53:ListHostedZonesByName, and ChangeResourceRecordSets and ListResourceRecordSets on the zone.",
        Status:      dns.Experimental,
        Features:    dns.Features{Comments: false, CaddyModule: "dns.providers.route53"},
    }, func(c route53Creds) (dns.LibDNS, error) {
        return &route53.Provider{AccessKeyId: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, Region: c.Region}, nil
    })
}
```

- **`Add`** registers the provider with the plugin, if it's listed for the build.
- **`dns.FromLibDNS`**, used by `Add`, maps libdns's `GetRecords`, `SetRecords`, `DeleteRecords` and zone lookup (`libdns.ZoneLister` where the provider has it, else walking up the name) onto `Client`.
- **Libdns differences** are handled once, in `FromLibDNS`: relative names, TTL rounding to the provider's minimum, and TXT quoting.

### What the core calls when

| When | Core | Provider called |
|---|---|---|
| Saving a cluster's DNS (`PUT …/clusters/{clu}/dns`) | Validate credentials against the schema, encrypt, run the connection test | `New`, then the test below |
| The connection test | Find the zone; set and delete `_lewbox-connection-test.<domain>` TXT; report the failing step (`credentials_refused`, `zone_not_found`, `not_permitted`) | `Zone`, `SetRecords`, `DeleteRecords` |
| A node enrolls, or its IP changes (`PUT /node/v0/dns`) | Decide the records ([clusters/pki-and-dns](../clusters/pki-and-dns.md#records)) | `SetRecords` |
| A certificate's DNS-01 challenge (`POST /node/v0/acme-challenges`) | Check the name is the node's own; set, then delete when told | `SetRecords`, `DeleteRecords` |
| A node is removed | Delete its records | `DeleteRecords` |
| Nightly reconcile | Compare Lewbox-named records with live nodes; repair or delete | `ListRecords`, `SetRecords`, `DeleteRecords` |
| The relay domain (tunnel clusters) | Same, with the instance's provider (`LEWBOX_RELAY_DNS_PROVIDER`, any provider id the build has) | All |

Every call has a 30 s deadline. It's retried by its job, or the request fails with `502 dns_provider_refused` and the provider's message. Calls are throttled per project; the connection test is limited to 10 a minute.

### AerolVM and the fork

**AerolVM doesn't need per-provider support.** With the fork's DNS-01 relay ([F43](../clusters/aerolvm-fork.md#api-clustering-and-lifecycle)):

1. The fork's Caddy uses one DNS provider module, `lewbox`.
2. That module asks the node agent.
3. The node agent asks the control plane (`POST`/`DELETE /node/v0/acme-challenges`).
4. The control plane calls whichever libdns provider the cluster uses.

So any provider Lewbox supports works for certificates, and DNS credentials never leave the control plane ([clusters/pki-and-dns](../clusters/pki-and-dns.md#dns-01-through-the-control-plane)).

**In the alpha, before F43,** clusters work as legacy's do: the Cloudflare token (scoped to the zone) is on the nodes for Caddy's own Cloudflare module. That's the alpha's design, not a fallback kept afterwards: when F43 is in Lewbox's minimum fork release, nodes stop getting the token, and the other providers are offered. There's no per-cluster version check in between ([D15](../README.md#decisions)).

We don't plan to compile more Caddy DNS modules into the fork: F43 makes them unnecessary.

### Credentials and where they go

Credentials are stored encrypted in `cluster_dns.credentials`, shaped by the provider's schema. The dashboard's form is built from that schema ([dashboard-slots](dashboard-slots.md#settings-forms)). With F43 they never leave the control plane; without it, see the fallback above.

### Turning things off

- **The plugin.** If any of a project's clusters uses a `dns/libdns` provider, turning `dns/libdns` off for that project fails with `409 plugin_in_use`, naming those clusters. The same is checked at start-up for the instance ([building-and-enabling](building-and-enabling.md#api)).
- **A provider no longer offered.** If a provider in use is dropped from the instance's `providers` setting, or from a build's `config.json`, start-up fails and names the clusters that use it. Setting `LEWBOX_PLUGINS_ALLOW_MISSING=dns/libdns:route53` overrides this, acknowledging that those clusters' records won't be maintained.

### Errors

| Code | Status | When |
|---|---|---|
| `validation_failed` | 422 | Credentials don't match the schema; an unknown `provider`, or one the instance doesn't offer; a provider the cluster's fork version can't use without F43 |
| `plugin_disabled` | 409 | Saving a cluster with a provider whose plugin is off |
| `plugin_in_use` | 409 | Turning `dns/libdns` off while clusters use one of its providers |
| `dns_provider_refused` | 502 | The provider refused a call, with its reason |
| `dns_connection` | 422 | The connection test failed: `step` and `reason` say where |
| `provider_unavailable` | 502 | The provider's API couldn't be reached |

### Contract tests

`plugin/dns/dnstest.Run(t, provider, creds)` runs for every provider in `dns/libdns`, and for any separate DNS plugin:

- **Offline** (every CI run):
  - the schema accepts the documented credentials and refuses others;
  - `New` does no I/O.
- **Against a fake** (`dnstest.Fake`, a libdns-shaped in-memory zone), for `FromLibDNS` and every adapter:
  - zone lookup picks the closest zone;
  - set is idempotent;
  - deleting a missing record is fine;
  - list under a name.
- **Live** (nightly, credentials in CI secrets, one dedicated test zone per provider):
  - the full cycle: zone, set A, AAAA, wildcard and TXT, list, delete;
  - reads go through the provider's API, not DNS, so propagation doesn't matter.
  - Passing the live suite is what makes a provider *supported*.

## Adding a new DNS provider

**With a libdns package**, e.g. Linode:

1. **Add `plugins/dns/libdns/providers/linode.go`:** import `github.com/libdns/linode`, write the credentials schema (mark secrets `x-lewbox-secret`), help text with the least-privilege token, `Features` (with `CaddyModule` if Caddy has one), `Status: Experimental`, and a constructor.
2. **Add `"linode"`** to the `providers` list for `dns/libdns` in `plugins/config.json`.
3. **Run `dnstest`** offline and against the fake. Then add a test zone to the nightly live suite, and switch it to `Supported` when it passes.
4. **Docs:** a page from the template (token, permissions, zone).

**Without a libdns package:** write the libdns package upstream (preferred, as it helps Caddy too), or make a separate plugin `plugins/dns/<name>/` implementing `dns.Provider` and `Client` directly. That's four methods, and it runs the same `dnstest`.

## Build plan

1. `plugin/dns`, `dns.FromLibDNS`, `dnstest` (M1).
2. `plugins/dns/libdns` with the generator's `providers` option and Cloudflare, supported (M1).
3. After the alpha, with F43 (beta): the other nine providers compiled in as experimental; Route 53, Hetzner and RFC 2136 through the live suite to supported; the rest as test zones allow.

## Open questions

1. ~~**One plugin per provider, or a `dns/libdns` plugin with many?**~~ Decided (2026-10-29): one `dns/libdns` plugin, providers chosen in `plugins/config.json`; separate plugins only for hosts without a libdns package.
2. ~~**The instance's relay provider.**~~ Decided (2026-10-29): any provider the build has, set in configuration (`LEWBOX_RELAY_DNS_PROVIDER` and its credentials). A built-in DNS server for the relay domain is a Later option for instances with no DNS API ([clusters/connectivity](../clusters/connectivity.md#dns-for-the-relay-domain)).
3. ~~**Default build size.**~~ Decided (2026-10-29): after the alpha, every provider is compiled into the default build (the alpha's has Cloudflare only). Those that haven't passed the live suite are labelled *experimental* in the dashboard and docs until they do.
