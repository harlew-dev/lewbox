---
title: Install the CLI
description: Build the lewbox binary and put it on every machine that runs the orchestrator or an AerolVM node.
---

`lewbox` is one binary: it's both the CLI and the orchestrator server. Every machine that runs the orchestrator, or that you turn into a node, needs it.

## From source

You need Go 1.27 or later, and Node 24 or later for the web console.

```bash
git clone https://github.com/harlew-dev/lewbox.git
cd lewbox
make
./bin/lewbox version
```

`make` builds the web console and then `bin/lewbox` with it embedded. Add `bin/` to your `PATH`, or copy the binary somewhere you can write to.

A plain `go build ./cmd/lewbox` still works, but the binary has no console until `make web` has built `web/dist`; until then the console page says it isn't built. Use `make build-go` for a fast Go-only rebuild once the console exists.

## For another machine

Build the binary for the target. `make build` still builds the console for the host first, then passes your `GOOS` and `GOARCH` to Go:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make build BIN=lewbox-linux-amd64
scp lewbox-linux-amd64 node-1:~/.local/bin/lewbox
```

Use `GOARCH=arm64` for ARM servers. Install it into a folder **your user can write to**, such as `~/.local/bin`, so that [`lewbox update`](../operations/update.md) can replace it later without `sudo`.

> [!NOTE]
> A one-line installer (`curl … | sh`) and prebuilt release downloads come with the first release.

## Shell completion

```bash
source <(lewbox completion bash)     # also: zsh, fish, powershell
```

Add the line to your shell's rc file to keep it.

## Next

- [Run the orchestrator](run-the-orchestrator.md)
- [Quickstart](../getting-started/quickstart.md)
