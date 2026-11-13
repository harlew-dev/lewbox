---
title: Storage profiles
description: Save an S3-compatible bucket and access key once and test it, so a cluster can archive stopped sandboxes without the secret being typed on every machine.
---

A **storage profile** is a saved S3-compatible bucket, endpoint, region, path style and access key. Lewbox tests it when you save it, seals the secret access key with the instance keyring, and shows only its last four characters afterwards.

A cluster with a storage profile is set up to archive stopped customer sandboxes to that bucket, so a node's small disk is not filled by sandboxes nobody is using. Every node of the cluster runs a registry in front of the bucket; `lewbox node add` and `lewbox node configure` install and configure it.

Instance admins only. The secret is fetched back only by `lewbox storage-profile credentials` and `lewbox cluster node-settings`, which exist to configure a node.

## The bucket key

Use a key **scoped to one bucket**. The access key and secret end up on every node of a cluster, so a key that can reach every bucket in the account gives a compromised node far more than it needs.

The bucket must already exist; Lewbox does not create one. Its name follows S3's rules: 3–63 lowercase letters, digits, dots and hyphens, starting and ending with a letter or digit, and not an IP address.

## Commands

AWS S3, where the endpoint is empty:

```bash
printf '%s' "$SECRET_ACCESS_KEY" | lewbox storage-profile create archive \
  --bucket lewbox-archive --region eu-west-1 \
  --access-key-id AKIAIOSFODNN7EXAMPLE --secret-access-key-stdin
```

Cloudflare R2, whose region is `auto`:

```bash
printf '%s' "$SECRET_ACCESS_KEY" | lewbox storage-profile create r2 \
  --endpoint https://<account id>.r2.cloudflarestorage.com --region auto \
  --bucket lewbox-archive --prefix clusters \
  --access-key-id <access key id> --secret-access-key-stdin
```

MinIO on a private network, which needs an `http://` endpoint and path-style addressing:

```bash
printf '%s' "$SECRET_ACCESS_KEY" | lewbox storage-profile create minio \
  --endpoint http://minio.internal:9000 --bucket lewbox --path-style=true \
  --access-key-id minioadmin --secret-access-key-stdin
```

Then:

```bash
lewbox storage-profile list
lewbox storage-profile show archive
lewbox storage-profile test archive
printf '%s' "$NEW_SECRET" | lewbox storage-profile update archive --secret-access-key-stdin
lewbox storage-profile delete archive --yes
```

`create` and `update` read the secret from stdin, never from a flag. `update` changes only the flags you pass; anything but the name is tested before it is saved and bumps the profile's `version`. `--prefix` is optional; Lewbox stores a cluster's archives under `<prefix>/clusters/<cluster id>/`.

`--path-style` defaults to true when an endpoint is set and false for AWS. Set it explicitly when the store needs the other addressing style: `http://<endpoint>/<bucket>/<key>` rather than `http://<bucket>.<endpoint>/<key>`.

## The test

Saving a profile, changing anything but its name, and `lewbox storage-profile test` all run the same test with a 30-second timeout, under an object at `<prefix>/lewbox-test/<random>`:

1. **write**: put the test object;
2. **read**: get it back and compare;
3. **list**: find it under its prefix;
4. **delete**: remove it.

The object is removed again, so a test leaves nothing behind. The step that failed comes back as `details.step`:

| Step | The message means | Fix |
|---|---|---|
| `write` | `the key was refused: <code>: <message>` | Check the access key ID and secret, and the policy on the bucket |
| `write` | `bucket <bucket> not found` | Create the bucket, or check its spelling and region |
| `write` | `could not reach <endpoint or AWS>: <error>` | Check the endpoint, the network and the DNS name |
| `read` | the object was written but could not be read | Check the bucket policy allows `GetObject` |
| `list` | the object is missing from the listing | Check the policy allows `ListBucket` |
| `delete` | the object could not be deleted | Check the policy allows `DeleteObject` |

A failing test on `create` or an `update` other than the name **saves nothing**: the command exits with a precondition error. A failing `storage-profile test` on a saved profile records the message in `test_error` and is not an error response.

`list` shows when each profile last passed: `ok`, `failed: <message>`, or `never`.

## Using a profile on a cluster

Point a cluster at the profile, then apply it on each of its nodes:

```bash
lewbox cluster set lab --storage-profile archive --stop-policy archive
lewbox node configure      # on each node
```

`--stop-policy keep` (the default) leaves stopped sandboxes on their node; `archive` hands them to the bucket. Archiving needs a storage profile and the Docker runtime. Clearing the profile while `--stop-policy archive` is set is refused, so change the policy first.

`lewbox cluster node-settings lab` shows exactly what each node must apply, including the bucket path: `<prefix>/clusters/<cluster id>/`.

### What a node installs

On a node of a cluster with a storage profile, `node add` and `node configure`:

1. **Install the registry**: CNCF Distribution, pinned and SHA-256 checked, as `/usr/local/bin/lewbox-registry`, listening on `127.0.0.1:5000` only. Its config is `/etc/lewbox-registry/config.yml` (`0640 root:lewbox-registry`), with the bucket's key and the cluster's root directory. A hardened `lewbox-registry.service` runs it as the `lewbox-registry` user.
2. **Turn on snapshot push** in `/etc/sandboxd/lewbox.env`, then restart sandboxd if it changed.
3. **Install the cleanup timer**: `lewbox-archive-cleanup.timer` runs `lewbox node cleanup` as root every five minutes, from a root-owned copy of the binary in `/usr/local/lib/lewbox/lewbox`.

Without a profile, the same commands remove the registry, its config and user files, the push lines, and the timer. See [What Lewbox changes](../reference/what-lewbox-changes.md#the-snapshot-registry).

### Outdated nodes

Each node reports the profile versions it applied. A node is **outdated** when the cluster's profile is newer, when it applied another profile, when the cluster stopped using one, or when it applied none yet. The node list shows the reason and the cluster check reports it:

```text
node-a: storage profile archive is at version 2; this node has version 1; run `lewbox node configure` on each
```

Run `lewbox node configure` on each node to catch up. A profile change does not reach a running node by itself: its token and the bucket path are on the node.

## Deleting

A profile a cluster uses cannot be deleted:

```text
storage profile "archive" is used by 1 cluster(s); stop using it in those clusters first
```

Point the cluster at another profile first, then delete this one.

## Next

Archiving stopped sandboxes is the next step: a stopped sandbox is snapshotted, pushed to the registry, and restored from any node as a new sandbox. See [What Lewbox changes](../reference/what-lewbox-changes.md#the-snapshot-registry) for what already runs on a node.
