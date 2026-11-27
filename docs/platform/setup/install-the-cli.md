---
title: Install the CLI
description: Install lewbox on every machine that runs the orchestrator or an AerolVM node.
---

`lewbox` is one binary: both the CLI and the orchestrator server. Install it on every machine that runs the orchestrator or that you turn into a node.

## First: a normal user with sudo

Lewbox **refuses to run as root**. It runs as your user and uses `sudo` for each step that needs root, printing the command first, so you can see everything it changes.

Many servers (Hetzner, DigitalOcean, OVH and others) start with only `root`. If that's you, create a user once. Run each command as root, one at a time:

1. Create the user. It asks for a password, which `sudo` will use:

   ```bash
   adduser lewbox
   ```

2. Let the user run `sudo`:

   ```bash
   usermod -aG sudo lewbox
   ```

3. Make the user's SSH folder:

   ```bash
   install -d -m 700 -o lewbox -g lewbox /home/lewbox/.ssh
   ```

4. Copy your SSH keys into it, so you can log in as the user:

   ```bash
   install -m 600 -o lewbox -g lewbox ~/.ssh/authorized_keys /home/lewbox/.ssh/
   ```

Then log out, log back in as that user (`ssh lewbox@your-server`), and do everything below as that user. If you already installed the CLI as root, install it again: root's copy in `/root/.local/bin` isn't on the new user's `PATH`.

On a machine that will run the orchestrator as a service, this login user is separate from the `lewbox-server` account the service runs as ([Run in production](run-in-production.md)).

In a terminal, `sudo` asks for the password once per command. Scripts have no terminal, so for those allow passwordless `sudo`: `echo 'lewbox ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/90-lewbox`.

> [!NOTE]
> To run as root anyway, put the variable on the same command: `LEWBOX_ALLOW_ROOT=1 lewbox install …`. `LEWBOX_ALLOW_ROOT=1 && lewbox install …` doesn't work: `&&` makes it a separate command, so `lewbox` never sees it.

## One-line install

Each release has prebuilt binaries for Linux `amd64` and `arm64`, with the web console included. You don't need Go. The install script downloads the latest, checks it against the release's `checksums.txt`, and installs it. Run it as your normal user, not root:

```bash
curl -fsSL https://raw.githubusercontent.com/harlew-dev/lewbox/main/install.sh | sh
```

It installs to `~/.local/bin`. If that folder isn't on your `PATH`, it adds `export PATH="$HOME/.local/bin:$PATH"` to `~/.profile` and `~/.bashrc` (only once), says so, and prints the line to run in the current shell. On zsh, add that line to `~/.zshrc` too. It ends by printing the installed `lewbox version`. Run it again to upgrade.

| Setting | Default | Change it |
|---|---|---|
| Install folder | `~/.local/bin` | `LEWBOX_INSTALL_DIR=/path` |
| A specific release | latest | `LEWBOX_VERSION=0.5.0` (or `v0.5.0`) |
| Release base URL | GitHub | `LEWBOX_RELEASE_URL=https://…` (a mirror) |

Put a setting before `sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/harlew-dev/lewbox/main/install.sh | LEWBOX_VERSION=0.5.0 sh
```

To run it as root anyway, put `LEWBOX_ALLOW_ROOT=1` before `sh` the same way.

It needs `curl` or `wget`, and `sha256sum` or `shasum`. It refuses to install if the checksum doesn't match or `checksums.txt` has no line for the binary.

## Manual download

Download the file for this machine from the [latest release](https://github.com/harlew-dev/lewbox/releases/latest), check it against `checksums.txt`, and install it.

| Platform | File |
|---|---|
| Linux `amd64` | `lewbox_linux_amd64` |
| Linux `arm64` | `lewbox_linux_arm64` |

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
base=https://github.com/harlew-dev/lewbox/releases/latest/download
curl -fsSL -O "$base/lewbox_linux_$ARCH" -O "$base/checksums.txt"
sha256sum --check --ignore-missing checksums.txt     # must print lewbox_linux_…: OK
install -D -m 755 "lewbox_linux_$ARCH" ~/.local/bin/lewbox
```

For a specific release, use `https://github.com/harlew-dev/lewbox/releases/download/v0.5.0` as `base`.

Then keep `~/.local/bin` on your `PATH` and check that it runs:

```bash
for f in ~/.profile ~/.bashrc; do grep -qxF 'export PATH="$HOME/.local/bin:$PATH"' "$f" 2>/dev/null || echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$f"; done
export PATH="$HOME/.local/bin:$PATH"
lewbox version
```

## With Go

You need **Go 1.21 or later** already installed (see the table below). From 1.21, Go downloads the Go 1.27 that Lewbox needs by itself.

1. Build and install Lewbox into `~/go/bin`:

   ```bash
   go install github.com/harlew-dev/lewbox/cmd/lewbox@latest
   ```

2. Keep `~/go/bin` on your `PATH` after you reconnect:

   ```bash
   for f in ~/.profile ~/.bashrc; do grep -qxF 'export PATH="$PATH:$HOME/go/bin"' "$f" 2>/dev/null || echo 'export PATH="$PATH:$HOME/go/bin"' >> "$f"; done
   ```

3. Put it on the `PATH` of this shell too:

   ```bash
   export PATH="$PATH:$HOME/go/bin"
   ```

4. Check that it runs:

   ```bash
   lewbox version
   ```

| System | `golang-go` / `golang` package | |
|---|---|---|
| Ubuntu 24.04 and later, Debian 13, Fedora | Go 1.21 or later | `sudo apt install -y golang-go` (Fedora: `sudo dnf install -y golang`), then the steps above |
| Ubuntu 22.04, Debian 12 | Go 1.18 and 1.19 | Too old: `go install` fails with `invalid go version '1.27.3': must match format 1.23`. Download a release instead, or install Go from [go.dev/dl](https://go.dev/dl/) |

> [!NOTE]
> A binary from `go install` has no web console. That's fine on nodes. For the machine that runs `lewbox serve`, download a release or use [Docker](run-with-docker.md).

## From source

You need Go 1.27+ and Node 24+.

1. Get the source:

   ```bash
   git clone https://github.com/harlew-dev/lewbox.git
   ```

2. Go into it:

   ```bash
   cd lewbox
   ```

3. Build the web console and the binary, `bin/lewbox`:

   ```bash
   make
   ```

4. Check that it runs:

   ```bash
   ./bin/lewbox version
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
