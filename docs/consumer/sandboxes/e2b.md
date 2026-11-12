---
title: E2B SDK
description: Use the E2B SDK against your region's E2B-compatible endpoint.
---

A region that runs the Docker runtime also serves an E2B-compatible endpoint at `/e2b`. A [sandbox API key](api-keys.md) works there as an E2B API key, so an app written for E2B can point at Lewbox without a rewrite.

The compatible SDK line is up to **2.50.0**. Later versions are not supported.

## Set the variables

Creating a key in a Docker region shows these values:

```bash
export E2B_API_URL=https://eu-west.api.example.com/e2b
export E2B_SANDBOX_URL=https://eu-west.api.example.com/e2b/runtime
export E2B_API_KEY=avm_…
```

## Run a sandbox

```bash
npm i e2b@2.50.0
```

```ts
import { Sandbox } from "e2b"

const sbx = await Sandbox.create()
console.log((await sbx.commands.run("echo hello")).stdout)
await sbx.kill()
```

The sandbox is created in your workspace's region, appears under **Sandboxes**, and counts against the same limits as a sandbox created with the AerolVM SDK.

## Limits of the compatible endpoint

- Only Docker regions serve E2B; a Firecracker region does not.
- The SDK is tested to 2.50.0. Pin that version.
- The token is the same `avm_…` key as the AerolVM endpoint. Revoking it stops both.

## Also see

- [AerolVM SDK](aerolvm-sdk.md): the native client and its full sandbox options.
- [Sandboxes and snapshots](sandboxes-and-snapshots.md): see and clean up what you run.
