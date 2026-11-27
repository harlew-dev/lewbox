---
title: Advanced AerolVM settings
description: Per-cluster sandbox timeouts, size caps, lifecycle timers and the Firecracker pool, applied on every node.
---

A cluster has a small set of AerolVM settings Lewbox manages for you. They are stored on the cluster and written into Lewbox's own files on every node by `lewbox node configure`. Their defaults are safe: only the Docker timeout is set, to a value Lewbox needs.

Set them when you create a cluster, or change them later:

```bash
lewbox node add node-1 --new-cluster eu-west --max-sandbox-cpu 4 --max-age 168h
lewbox cluster add eu-west --admin-url https://eu-west.example.com:8443 --admin-token-stdin \
  --max-sandbox-cpu 4 --max-sandbox-memory-mb 8192 --max-age 168h

lewbox cluster set eu-west --idle-stop-minutes 30
lewbox cluster set eu-west --max-sandbox-cpu none
```

`none` (or an empty value) clears a setting on `lewbox cluster set`. The console shows the same fields under **Advanced** in the register dialog and the cluster's **Settings** card. A change shows on its nodes as `outdated` until `lewbox node configure` runs there, and the cluster's `node settings` check names every setting that differs.

## The settings

| Setting | Flag | Where it goes | Default | Limits |
|---|---|---|---|---|
| `docker_timeout` | `--docker-timeout` | `SB_HTTP_CLIENT_TIMEOUT` | `15m` | 1m–2h. Clearing restores 15m |
| `max_sandbox_cpu` | `--max-sandbox-cpu` | create policy `limits.max_cpu` | no cap | At least 1 |
| `max_sandbox_memory_mb` | `--max-sandbox-memory-mb` | create policy `limits.max_memory_mb` | no cap | At least 1024 |
| `max_sandbox_disk_gb` | `--max-sandbox-disk-gb` | create policy `limits.max_disk_gb` | no cap | At least 10 |
| `idle_stop_minutes` | `--idle-stop-minutes` | `SB_IDLE_TIMEOUT_MIN` | off | Whole minutes |
| `destroy_if_idle_for` | `--destroy-if-idle-for` | `SB_DEFAULT_DESTROY_IF_IDLE_FOR` | off | A duration, at most 720h |
| `max_age` | `--max-age` | `SB_MAX_AGE` | off | A duration, at most 720h |
| `firecracker_vm_slots` | `--firecracker-vm-slots` | `SB_FIRECRACKER_TAP_POOL_SIZE` | 256 | 256–1024, Firecracker clusters only, raise only |

## Sandbox size caps

The three size caps go into the same create-policy file as the runtime guard:

```yaml
version: 1
runtimes: [docker]
limits:
  max_cpu: 4
  max_memory_mb: 8192
  max_disk_gb: 50
```

They apply to the size a sandbox really gets. A create that leaves out `cpu`, `memoryMB` or `diskGB` gets AerolVM's default (1 CPU, 1024 MB, 10 GB), and that default is checked too. That is why a cap below the default is refused: it would refuse every create that leaves the size out. A refused create gets `403 create_policy_denied` naming the rule. Resizes are checked too; a new policy never changes a sandbox that already runs. See [Limit what users can create](https://github.com/aerol-ai/microvm/blob/v0.7.0/docs/src/content/docs/enterprise/create-policy.mdx) in AerolVM's documentation.

## Lifecycle timers

`idle_stop_minutes` stops a running sandbox that has been idle that long and set no lifecycle timer of its own. `destroy_if_idle_for` and `max_age` write a default delete timer into a new sandbox that sets none, so a crashed client cannot leak sandboxes. They do not reach sandboxes created before they were set. The idle stop is skipped when it would come after an idle delete.

## Firecracker VM slots

`firecracker_vm_slots` is the size of each node's TAP pool: the most Firecracker sandboxes it can run at once. With the default `172.16.0.0/20` CIDR the maximum is 1024.

AerolVM seeds the pool once and only ever adds slots. A lower value does not remove running sandboxes and does not remove the old slots: it silently leaves the node on the pool it already has. Lewbox therefore accepts only values at or above the cluster's current value. To return to a smaller pool, rebuild the node (or drop and reseed the pool there) — a later `lewbox node configure` will not do it.

## Next

- [Health and checks](health-and-checks.md): the `node settings` check and how to fix drift.
- [What Lewbox changes on a machine](../reference/what-lewbox-changes.md#lewboxs-settings): the files themselves.
