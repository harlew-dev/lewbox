---
title: Install the CLI
description: Install lewbox on every machine that runs the orchestrator or an AerolVM node.
---

`lewbox` is one binary: both the CLI and the orchestrator server. Install it on every machine that runs the orchestrator or that you turn into a node. Run these as your own user, not root.

## Ubuntu and Debian

```bash
sudo apt install -y golang-go && export PATH="$PATH:$HOME/go/bin" && go install github.com/harlew-dev/lewbox/cmd/lewbox@latest && lewbox version
```

That's it. Ubuntu's Go may be older than the Go 1.27 Lewbox needs; Go downloads the newer version by itself.

To keep `lewbox` on your `PATH` in new shells:

```bash
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.bashrc
```

On Fedora, use `sudo dnf install -y golang`. Anywhere else, install Go from [go.dev/dl](https://go.dev/dl/) and run the same `go install` line.

> [!NOTE]
> A binary from `go install` has no web console. That's fine on nodes. For the machine that runs `lewbox serve`, download a release (below) or use [Docker](run-with-docker.md).

## Without Go: download a release

Each release has prebuilt binaries for Linux `amd64` and `arm64`, with the web console included. This downloads the latest, checks its checksum, and installs it into `~/.local/bin`:

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
URL=https://github.com/harlew-dev/lewbox/releases/latest/download
cd "$(mktemp -d)" &&
  curl -fsSL -O "$URL/lewbox_linux_$ARCH" -O "$URL/checksums.txt" &&
  sha256sum --check --ignore-missing checksums.txt &&
  install -D -m 755 "lewbox_linux_$ARCH" ~/.local/bin/lewbox &&
  cd - >/dev/null
export PATH="$HOME/.local/bin:$PATH"
lewbox version
```

## From source

You need Go 1.27+ and Node 24+.

```bash
git clone https://github.com/harlew-dev/lewbox.git && cd lewbox && make && ./bin/lewbox version
```

## Update

```bash
lewbox update
```

See [Update](../operations/update.md).

## Shell completion

```bash
source <(lewbox completion bash)     # also: zsh, fish, powershell
```

## Next

- [Run the orchestrator](run-the-orchestrator.md)
- [Quickstart](../getting-started/quickstart.md)
