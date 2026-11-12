---
title: Install the CLI
description: Install lewbox on every machine that runs the orchestrator or an AerolVM node.
---

`lewbox` is one binary: both the CLI and the orchestrator server. Install it on every machine that runs the orchestrator or that you turn into a node.

## First: a normal user with sudo

Lewbox **refuses to run as root**. It runs as your user and uses `sudo` for each step that needs root, printing the command first, so you can see everything it changes.

Many servers (Hetzner, DigitalOcean, OVH and others) start with only `root`. If that's you, create a user once, as root:

```bash
adduser lewbox                      # asks for a password; sudo will use it
usermod -aG sudo lewbox
install -d -m 700 -o lewbox -g lewbox /home/lewbox/.ssh
install -m 600 -o lewbox -g lewbox ~/.ssh/authorized_keys /home/lewbox/.ssh/   # your SSH keys
```

Then log out, log back in as that user (`ssh lewbox@your-server`), and do everything below as that user. If you already installed the CLI as root, install it again: root's copy in `/root/.local/bin` isn't on the new user's `PATH`.

On a machine that will run the orchestrator as a service, this login user is separate from the `lewbox-server` account the service runs as ([Run in production](run-in-production.md)).

In a terminal, `sudo` asks for the password once per command. Scripts have no terminal, so for those allow passwordless `sudo`: `echo 'lewbox ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/90-lewbox`.

> [!NOTE]
> To run as root anyway, put the variable on the same command: `LEWBOX_ALLOW_ROOT=1 lewbox install …`. `LEWBOX_ALLOW_ROOT=1 && lewbox install …` doesn't work: `&&` makes it a separate command, so `lewbox` never sees it.

## Download a release

Each release has prebuilt binaries for Linux `amd64` and `arm64`, with the web console included. You don't need Go. This downloads the latest, checks its checksum, and installs it into `~/.local/bin`:

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

## With Go

You need **Go 1.21 or later** already installed. From 1.21, Go downloads the Go 1.27 that Lewbox needs by itself.

```bash
export PATH="$PATH:$HOME/go/bin" && go install github.com/harlew-dev/lewbox/cmd/lewbox@latest && lewbox version
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.bashrc     # keep it on PATH in new shells
```

| System | `golang-go` / `golang` package | |
|---|---|---|
| Ubuntu 24.04 and later, Debian 13, Fedora | Go 1.21 or later | `sudo apt install -y golang-go` (Fedora: `sudo dnf install -y golang`), then the lines above |
| Ubuntu 22.04, Debian 12 | Go 1.18 and 1.19 | Too old: `go install` fails with `invalid go version '1.27.3': must match format 1.23`. Download a release instead, or install Go from [go.dev/dl](https://go.dev/dl/) |

> [!NOTE]
> A binary from `go install` has no web console. That's fine on nodes. For the machine that runs `lewbox serve`, download a release or use [Docker](run-with-docker.md).

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
