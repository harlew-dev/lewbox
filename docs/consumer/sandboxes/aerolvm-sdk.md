---
title: AerolVM SDK
description: Create, use and destroy sandboxes in your region from TypeScript or Python.
---

The AerolVM SDK talks to your region's API directly. It reads `SB_API_URL` and `SB_API_TOKEN`, so export a [sandbox API key](api-keys.md) first.

## TypeScript

```bash
npm i @aerol-ai/aerolvm-sdk
```

```ts
import { MicroVM } from "@aerol-ai/aerolvm-sdk"

const avm = new MicroVM() // reads SB_API_URL and SB_API_TOKEN
const sb = await avm.create({ image: "ubuntu:22.04", cpu: 1, memoryMB: 1024 })
const r = await sb.exec("echo hello")
console.log(r.stdout)
await sb.destroy()
```

## Python

```bash
pip install aerolvm-sdk
```

```python
from microvm import MicroVM

avm = MicroVM()  # reads SB_API_URL and SB_API_TOKEN
sb = avm.create({"image": "ubuntu:22.04", "cpu": 1, "memoryMB": 1024})
print(sb.exec("echo hello")["stdout"])
sb.destroy()
```

## The pieces

- `MicroVM()` is the client. Construct it with no arguments to use the two environment variables.
- `create` takes an `image`, `cpu` and `memoryMB`. On a region that runs Firecracker, add `runtime: "firecracker"` (`"runtime": "firecracker"` in the Python options).
- `exec` runs a command in the sandbox and returns its output and exit status.
- `destroy` deletes the sandbox.

Every sandbox you create this way appears under **Sandboxes** in the portal, where you can also stop or delete it.

## Also see

- [E2B SDK](e2b.md): the same region through an E2B-compatible endpoint.
- [Sandboxes and snapshots](sandboxes-and-snapshots.md): the portal view of what you run.
