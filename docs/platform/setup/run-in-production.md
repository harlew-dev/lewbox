---
title: Run in production
description: Keep the orchestrator running as a systemd service behind TLS, and back up what matters.
---

`lewbox serve` is one process with no dependencies, so production is mostly about keeping it running, putting TLS in front of it, and backing it up.

## Run it as a service

Create a system account for the service, with its data folder. It has no password, no login shell and no `sudo`. Keep it separate from the `lewbox` login user you work as ([Install the CLI](install-the-cli.md#first-a-normal-user-with-sudo)).

1. Create the `lewbox-server` account, with `/var/lib/lewbox` as its home and data folder:

   ```bash
   sudo useradd --system --create-home --home-dir /var/lib/lewbox --shell /usr/sbin/nologin lewbox-server
   ```

2. Copy the `lewbox` binary where the service can run it:

   ```bash
   sudo install -m 0755 lewbox /usr/local/bin/lewbox
   ```

3. Write the service unit to `/etc/systemd/system/lewbox.service`:

```ini
[Unit]
Description=Lewbox orchestrator
After=network-online.target
Wants=network-online.target

[Service]
User=lewbox-server
Environment=LEWBOX_DB=/var/lib/lewbox/lewbox.db
Environment=LEWBOX_KEYRING_FILE=/var/lib/lewbox/keyring.key
ExecStart=/usr/local/bin/lewbox serve --listen 127.0.0.1:8080
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

4. Make systemd read the new unit:

   ```bash
   sudo systemctl daemon-reload
   ```

5. Start the service now and at every boot:

   ```bash
   sudo systemctl enable --now lewbox
   ```

6. Watch its logs, which are JSON lines. Press Ctrl+C to stop watching:

   ```bash
   journalctl -u lewbox -f
   ```

7. Create the first admin. Or use the bootstrap variables from [Run the orchestrator](run-the-orchestrator.md#create-the-first-admin):

   ```bash
   sudo -u lewbox-server LEWBOX_DB=/var/lib/lewbox/lewbox.db LEWBOX_KEYRING_FILE=/var/lib/lewbox/keyring.key lewbox user create you@example.com --instance-admin
   ```

> [!NOTE]
> `lewbox orchestrator enable` will write this unit for you; it's planned, not built yet.

## Put TLS in front

The orchestrator speaks plain HTTP. Passwords and session tokens cross the network when people sign in, so serve it over HTTPS. With Caddy:

```caddy
lewbox.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Then everyone signs in with `lewbox login https://lewbox.example.com`.

Behind a proxy, every request reaches Lewbox from the proxy's address, so the per-IP sign-in and sign-up limits would see one client. Tell Lewbox to trust the proxy's `X-Forwarded-For` header:

```ini
ExecStart=/usr/local/bin/lewbox serve --listen 127.0.0.1:8080 --trusted-proxy 127.0.0.1/32
```

`--trusted-proxy` is repeatable. Only add the proxy's own network: Lewbox believes the header only from an address inside those CIDRs, and takes the right-most entry that is not itself trusted. With Caddy on the same host, `127.0.0.1/32` is enough.

## Back up

Back up these two files together:

- `/var/lib/lewbox/lewbox.db` (use `sqlite3 lewbox.db ".backup 'lewbox-backup.db'"` while it runs);
- `/var/lib/lewbox/keyring.key`. Without it, the cluster admin tokens in the database can't be decrypted.

Store the keyring backup somewhere separate and safe: together, the two files give full control of every registered cluster.

## Next

- [Start a cluster](../clusters/start-a-cluster.md)
- [Security](../reference/security.md): what Lewbox stores and how.
- [Update](../operations/update.md)
