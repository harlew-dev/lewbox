---
title: Register an existing cluster
description: Bring an AerolVM cluster you set up without Lewbox under Lewbox's management with an admin token.
---

If you already run an AerolVM cluster, you don't need `lewbox install`. Give Lewbox an admin token and the cluster's URLs.

## 1. Prepare the cluster

On **every** node, AerolVM needs these three settings. They're the same ones `lewbox node add` would apply. On each node:

1. Write the create policy that allows only the cluster's runtime. Use `docker` or `firecracker`, matching the `--runtime` you register the cluster with:

   ```bash
   sudo tee /etc/sandboxd/lewbox-create-policy.yaml >/dev/null <<'EOF'
   version: 1
   runtimes: [docker]
   EOF
   sudo chmod 644 /etc/sandboxd/lewbox-create-policy.yaml
   ```

2. Write the settings to Lewbox's own file. Copy all five lines; they are one command:

   ```bash
   sudo tee /etc/sandboxd/lewbox.env >/dev/null <<'EOF'
   SB_IDENTITY_ENABLED=true
   SB_TOKEN_MAX_TTL=876000h
   SB_CREATE_POLICY_FILE=/etc/sandboxd/lewbox-create-policy.yaml
   SB_HTTP_CLIENT_TIMEOUT=15m
   EOF
   ```

3. Make the file readable by root only:

   ```bash
   sudo chmod 600 /etc/sandboxd/lewbox.env
   ```

4. Make the folder for a systemd drop-in:

   ```bash
   sudo mkdir -p /etc/systemd/system/sandboxd.service.d
   ```

5. Add the drop-in that loads the file into AerolVM's service:

   ```bash
   printf '[Service]\nEnvironmentFile=/etc/sandboxd/lewbox.env\n' | sudo tee /etc/systemd/system/sandboxd.service.d/lewbox.conf >/dev/null
   ```

6. Make systemd read the drop-in:

   ```bash
   sudo systemctl daemon-reload
   ```

7. Restart AerolVM with the new settings:

   ```bash
   sudo systemctl restart sandboxd
   ```

Restart one node at a time, and wait for each to be healthy again before the next. Without identity mode, a node rejects every token, including Lewbox's. A missing or invalid policy makes every create fail with `create_policy_invalid`, so write the file before the env line that points at it.

## 2. Create an admin token

On any node (or wherever you use the `aerolvm` CLI as an admin):

```bash
aerolvm token create --name lewbox --role admin --team '*' --expires-in 36500d
```

Copy the `avm_…` token it prints.

## 3. Register it

```bash
printf '%s' "$AEROLVM_ADMIN_TOKEN" | lewbox cluster add us-north \
  --admin-url https://us-north.api.example.com \
  --runtime docker \
  --admin-token-stdin
```

The token is read from stdin, so it never appears in your shell history or in `ps`. Lewbox runs [every check](health-and-checks.md) first. If a required check fails, nothing is stored and the error lists what to fix. Otherwise the cluster is registered, and its nodes show up in `lewbox node list`.

| Flag | |
|---|---|
| `--admin-url` | Where the orchestrator reaches the cluster's API (required) |
| `--endpoint-url` | What your customers' SDKs use. Defaults to `--admin-url` |
| `--runtime` | `docker` (default) or `firecracker`. Every node must offer it |
| `--name` | A display name. Defaults to the slug |

The same command takes the [advanced AerolVM settings](advanced-settings.md) (`--max-sandbox-cpu`, `--idle-stop-minutes`, and the rest). They are stored on the cluster and applied on every node the next time `lewbox node configure` runs there.

## Next

- [Health and checks](health-and-checks.md)
- [Add nodes](add-nodes.md): later machines can join with `lewbox node add --cluster us-north`.
