---
title: Configuration
description: Every environment variable, file and default Lewbox uses, in one place.
---

Flags win over environment variables, which win over saved settings, which win over defaults.

## Environment variables

### Choosing an orchestrator

| Variable | Flag | |
|---|---|---|
| `LEWBOX_CONTEXT` | `--context` | Saved sign-in to use |
| `LEWBOX_API_URL` | `--api-url` | Orchestrator URL, without a saved context |
| `LEWBOX_TOKEN` | | Session token to send with `LEWBOX_API_URL` |
| `LEWBOX_OUTPUT=json` | `--json` | Always print JSON |

### The orchestrator (`lewbox serve`) and local mode

| Variable | Flag | Default |
|---|---|---|
| `LEWBOX_PORT` | `--port` | `8080` |
| `LEWBOX_LISTEN` | `--listen` | `:8080` |
| `LEWBOX_CHECK_INTERVAL` | `--check-interval` | `1m` (`0` turns checks off) |
| `LEWBOX_COOKIE_SECURE` | `--cookie-secure` | `auto`. `always` forces `Secure` browser session cookies; use it behind a TLS proxy that does not set `X-Forwarded-Proto` |
| `LEWBOX_TRUSTED_ORIGINS` | `--trusted-origin` | Extra origins the CSRF check accepts, comma-separated, such as `https://lewbox.example.com` |
| `LEWBOX_TRUSTED_PROXIES` | `--trusted-proxy` | CIDRs of proxies whose `X-Forwarded-For` sets the client IP, comma-separated. Empty trusts no proxy |
| `LEWBOX_DB` | `--db` | `$XDG_DATA_HOME/lewbox/lewbox.db`, i.e. `~/.local/share/lewbox/lewbox.db` |
| `LEWBOX_KEYRING_FILE` | `--keyring` | `keyring.key` next to the database |
| `LEWBOX_BOOTSTRAP_ADMIN_EMAIL` | | First instance admin, created only when there are no accounts |
| `LEWBOX_BOOTSTRAP_ADMIN_PASSWORD` | | Its password |
| `LEWBOX_BOOTSTRAP_ADMIN_PASSWORD_FILE` | | Or read the password from this file |
| `LEWBOX_DOCS_DIR` | | Development only: serve the docs from this folder instead of the embedded copy. It holds the `consumer/` and `platform/` books. Restart after editing |
| `LEWBOX_WEB_DIR` | | Development only: serve the web console from this folder instead of the embedded build |

### Machines (`install`, `node add`, `update`)

| Variable | Flag | |
|---|---|---|
| `LEWBOX_AEROLVM_FROM` | `--from` | Folder of AerolVM release files, as `DIR/<tag>/install.sh`, `checksums.txt`, `sandboxd_linux_<arch>`, `toolboxd_linux_<arch>`, `caddy_linux_<arch>` and `aerolvm_linux_<arch>`. Nothing is downloaded |
| `LEWBOX_RELEASE_URL` | | Where Lewbox releases are read from. Default `https://github.com/harlew-dev/lewbox/releases` |
| `LEWBOX_RELEASE_FROM` | `--release-from` | A folder holding one Lewbox release: `release.json`, `checksums.txt`, `lewbox_<os>_<arch>` |
| `LEWBOX_ALLOW_ROOT=1` | | Allow running as root. Not recommended. Set it on the same command (`LEWBOX_ALLOW_ROOT=1 lewbox install …`) or `export` it |

## Files

| File | Written by | Mode |
|---|---|---|
| `~/.local/share/lewbox/lewbox.db` | The orchestrator, and the CLI in local mode | Your umask; the folder is `0700` |
| `~/.local/share/lewbox/keyring.key` | Same, on first use | `0600`. Refused if anyone else can read it |
| `~/.config/lewbox/config.json` | `login`, `context use` | `0600` |
| `~/.config/lewbox/credentials.json` | `login`, `logout` | `0600` |
| `~/.config/lewbox/node.json` | `install`, `node add`, `update` on a node | `0600`. No secrets |
| `~/.cache/lewbox/aerolvm/<tag>/install.sh` | `install`, `node add`, `update` | The verified installer that was run |

`$XDG_DATA_HOME`, `$XDG_CONFIG_HOME` and `$XDG_CACHE_HOME` move these folders.

On each node, Lewbox also writes two system files. See [What Lewbox changes on a machine](what-lewbox-changes.md#lewboxs-settings).

## Pinned AerolVM version

This Lewbox release installs AerolVM **v0.7.0** by default. It checks the installer against the SHA-256 built into the binary: `0305d18cb86ef06b11dd6b3cc40f91f8caa6abc8b5975895231ff894b9848d33`. Other versions are checked against that release's `checksums.txt`, and Lewbox warns that they are untested.
