---
title: Quickstart
description: Create a workspace, get a sandbox API key, and run a first sandbox.
---

This walkthrough takes a few minutes. You need a browser and Node or Python for the SDK.

## 1. Get an account

Your provider gives you one of two ways in:

- **A setup link.** Open the link they sent you, for example `https://lewbox.example.com/setup/lbs_…`. It works once: you create your account and your workspace, and the workspace gets the plan and region your provider chose.
- **The sign-up page.** Go to the Lewbox address your provider gave you and choose **Sign up**. Whether sign-up is open depends on the provider; if it is closed, ask them for a setup link.

Already have an account? Sign in instead.

## 2. Create a workspace

When you sign in without a workspace, Lewbox sends you to onboarding. Give the workspace a name, and it is ready. If your provider set a default plan and region, the new workspace starts there; otherwise it has no plan until your provider assigns one.

If you opened a setup link, the workspace is created with the link's plan already in place. You can create other workspaces later from the workspace switcher, under **New workspace…**.

## 3. Create a sandbox API key

Open **API keys** in the sidebar and choose **Create API key**. Give it a name and keep the default 90-day lifetime, or pick another.

The key is shown once. Copy it with the environment variables:

```bash
export SB_API_URL=https://eu-west.api.example.com
export SB_API_TOKEN=avm_…
```

The URL is the API endpoint of your workspace's region. A key works only there, and only for your workspace's sandboxes.

## 4. Run a sandbox

Install the SDK:

```bash
npm i @aerol-ai/aerolvm-sdk
```

Then create a sandbox, run a command in it, and destroy it:

```ts
import { MicroVM } from "@aerol-ai/aerolvm-sdk"
const avm = new MicroVM() // reads SB_API_URL and SB_API_TOKEN
const sb = await avm.create({ image: "ubuntu:22.04", cpu: 1, memoryMB: 1024 })
const r = await sb.exec("echo hello")
console.log(r.stdout)
await sb.destroy()
```

The sandbox shows up under **Sandboxes** in the portal. From there you can stop or delete it, and see its snapshots, without opening a terminal.

## Next

- [Sandbox API keys](../sandboxes/api-keys.md): lifetimes, revoking, and the E2B variables.
- [AerolVM SDK](../sandboxes/aerolvm-sdk.md): the same flow in Python, and the rest of the client.
- [Usage and limits](../usage/usage-and-limits.md): what your plan allows and what happens when you reach it.
