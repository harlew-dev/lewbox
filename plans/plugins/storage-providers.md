# Storage providers

> Status: Draft · Milestone: Alpha (M1) for the interface and `storage/s3`; other stores Later · Updated: 2026-10-29

A cluster's object storage holds what must outlive a node: snapshots of stopped sandboxes and template images, both as registry layers ([clusters/storage-and-registry](../clusters/storage-and-registry.md)). It's used from two places: **the control plane** (the connection test, garbage collection, sizes and usage) and **each node's registry** (CNCF Distribution, which reads and writes the bucket with its own storage driver). A storage provider plugin serves both. The default build ships `storage/s3`, which covers AWS S3, Cloudflare R2, MinIO and any other S3-compatible store.

## Today

S3 and R2 only. The app tests and garbage-collects the bucket with flysystem (`lewbox-legacy/app/Actions/Clusters/TestStorageConnection.php`, `CleanUpRegistry.php`); `install.sh` writes the node registry's `s3` driver configuration with the bucket's keys. The rebuild's plan describes the bucket, the test and GC ([clusters/storage-and-registry](../clusters/storage-and-registry.md#the-bucket)); this plan makes the store a plugin.

## Goals

- Adding a store is a plugin implementing a small object API and saying how the node registry reaches it.
- The node registry's driver is the constraint: a provider must map to one Distribution supports (`s3`, `gcs`, `azure`), or be S3-compatible.
- Every provider passes the same contract suite, including the multipart and listing behaviour GC relies on.

## Design

### The interface

```go
package storage // github.com/harlew-dev/lewbox/plugin/storage

type Provider interface {
    Info() Info
    // New returns a bucket client for one cluster's settings, validated against Info().Settings.
    New(settings json.RawMessage, secrets json.RawMessage) (Bucket, error)
    // Registry returns the node registry's storage section for these settings: the Distribution
    // driver name and its parameters, secrets included (they reach nodes; see Security).
    Registry(settings json.RawMessage, secrets json.RawMessage, prefix string) (RegistryStorage, error)
}

type Info struct {
    ID       string // "s3": stored in cluster_storage.provider
    Title    string // "S3-compatible storage"
    Flavours []Flavour // for s3: aws, r2, minio, s3_compatible: each sets defaults (endpoint, path style, chunk size)
    Settings []byte // JSON Schema: bucket, region, endpoint, prefix…
    Secrets  []byte // JSON Schema: access_key_id, secret_access_key (x-lewbox-secret)
    Help     string // how to make a key scoped to one bucket
}

type Bucket interface {
    Put(ctx context.Context, key string, r io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Head(ctx context.Context, key string) (ObjectInfo, error)          // ErrNotFound
    Delete(ctx context.Context, keys ...string) error                 // batched where the store allows; missing keys aren't errors
    List(ctx context.Context, prefix, after string, limit int) ([]ObjectInfo, string, error) // lexicographic; a cursor
    Multipart(ctx context.Context, key string, parts [][]byte) error  // for the connection test
    Presign(ctx context.Context, key string, ttl time.Duration) (string, error) // optional: ErrNotSupported
}

type RegistryStorage struct {
    Driver     string            // "s3" | "gcs" | "azure"
    Parameters map[string]any    // Distribution's config for that driver (region, bucket, regionendpoint, rootdirectory…)
}
```

### What the core calls when

| When | Core | Provider called |
|---|---|---|
| Saving a cluster's storage (`PUT …/clusters/{clu}/storage`) | Validate, encrypt secrets, run the connection test, refuse a change while snapshots exist (`409 cluster_has_snapshots`) | `New`, then the test |
| The connection test | Write, read, list and delete `lewbox-connection-test/<random>`; a two-part multipart upload; a presigned GET if supported; report the failing step | `Put`, `Get`, `List`, `Delete`, `Multipart`, `Presign` |
| A node's desired state (`GET /node/v0/spec`) | Include the registry's storage section, so the node agent writes the registry's config ([clusters/node-agent](../clusters/node-agent.md)) | `Registry` |
| Registry GC (hourly, leader) | The three passes over the registry layout ([clusters/storage-and-registry](../clusters/storage-and-registry.md#garbage-collection)) | `List`, `Head`, `Delete` |
| Sizes and usage | Read manifests and blob sizes for snapshots and builds | `Get`, `Head`, `List` |

The registry layout (`docker/registry/v2/…` under the cluster's prefix) is Distribution's, the same on every store, so GC's logic is the core's and only the object calls are the provider's.

### Flavours

`storage/s3`'s flavours set defaults the person can override:

| Flavour | Endpoint | Region | Path style | Notes |
|---|---|---|---|---|
| `aws` | derived from region | required | no | |
| `r2` | `https://<account_id>.r2.cloudflarestorage.com` | `auto` | yes | Multipart parts must be equal-sized except the last (the test checks it); no presigned `PUT` with some SDK settings |
| `minio` | given | `us-east-1` | yes | For homelabs |
| `s3_compatible` | given | given | given | Wasabi, Backblaze B2, Ceph RGW… |

### Security

- **Bucket credentials reach nodes,** because each node's registry reads and writes the bucket directly ([clusters/storage-and-registry](../clusters/storage-and-registry.md#the-bucket)). The help text and the connection test push for a key scoped to the one bucket (the test notes when the key can list other buckets).
- They're encrypted at rest in the control plane, sent to node agents over their TLS connection only in the desired state, and written by the node agent with `0600` for the registry.
- A provider must never log secrets or full presigned URLs.

### Turning one off

As DNS: in use by a cluster → `409 plugin_in_use`; removed from a build while clusters use it → start-up fails unless acknowledged with `LEWBOX_PLUGINS_ALLOW_MISSING`, since snapshots there would be unreachable.

### Errors

| Code | Status | When |
|---|---|---|
| `validation_failed` | 422 | Settings or secrets don't match the schema; an unknown provider |
| `plugin_disabled` | 409 | Saving a cluster with a provider whose plugin is off |
| `plugin_in_use` | 409 | Turning it off while clusters use it |
| `storage_connection` | 422 | The connection test failed: `step` and `reason` |
| `provider_unavailable` | 502 | The store couldn't be reached |
| `cluster_has_snapshots` | 409 | Changing a cluster's storage while snapshots exist |

### Contract tests

`plugin/storage/storagetest.Run(t, provider, settings, secrets)`:

- **Against MinIO** in CI (a container), for `storage/s3`; against each store's emulator where one exists (Azurite for Azure, `fake-gcs-server` for GCS).
- Put, get, head, delete (including missing keys and batches over 1,000), list ordering and cursors over 2,500 objects, multipart with equal and unequal parts, presign where supported.
- **The registry round trip:** start Distribution with `Registry(...)`'s config against the store, push a small image, list it with `Bucket.List`, run the GC passes on a copy, pull it back.
- **Live** (nightly): AWS S3 and R2 with test buckets.

## Adding a new store

Taking **Google Cloud Storage** as the example:

1. **Check the node registry supports it.** Distribution has a `gcs` driver, so GCS can be native. (A store without a Distribution driver must be used through its S3-compatible API with `storage/s3`.)
2. **Make the plugin:** `plugins/storage/gcs/` with `Info{Name: "storage/gcs", Kind: KindStorage}`.
3. **Settings and secrets schemas:** bucket, prefix; a service-account key JSON (secret), with help for a role limited to the one bucket.
4. **`Bucket`** with `cloud.google.com/go/storage`; `Multipart` as a composed upload (GCS has no S3 multipart; the test checks a composed object instead).
5. **`Registry`:** driver `gcs` with `bucket`, `keyfile` (the node agent writes the key to a file), `rootdirectory`.
6. **Run `storagetest`** against `fake-gcs-server`, then live.
7. **Add it to `plugins/config.json`.**

**Azure Blob Storage** is the same with Distribution's `azure` driver (`accountname`, `accountkey`, `container`) and Azurite for tests.

## Build plan

1. `plugin/storage`, `storagetest` with MinIO (M1).
2. `plugins/storage/s3` with its flavours; GC moved onto `Bucket` (M1).
3. GCS and Azure as plugins when someone needs them (Later).

## Open questions

1. **Instance-provided storage** for hosted projects (README open question 1): would it be a provider, or a cluster setting pointing at an instance-owned bucket? Proposed: the latter, with this provider underneath.
2. **Short-lived node credentials.** Could the control plane hand nodes short-lived, prefix-scoped credentials (STS on AWS, temporary R2 tokens) instead of the bucket key? A provider capability, `IssueNodeCredentials`, Later.
