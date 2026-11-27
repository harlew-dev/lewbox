#!/bin/sh
# Install Lewbox from GitHub releases on Linux (amd64 or arm64).
#
#   curl -fsSL https://raw.githubusercontent.com/harlew-dev/lewbox/main/install.sh | sh
#
# LEWBOX_VERSION=0.5.0 pins a release, with or without the leading v.
# LEWBOX_INSTALL_DIR=/path chooses where the binary goes (default: ~/.local/bin).
# LEWBOX_RELEASE_URL overrides the releases base URL, as `lewbox update` does.
# LEWBOX_ALLOW_ROOT=1 overrides the refusal to run as root.
set -eu

repo="harlew-dev/lewbox"
docs_url="https://github.com/harlew-dev/lewbox/blob/main/docs/platform/setup/install-the-cli.md#first-a-normal-user-with-sudo"
release_url=${LEWBOX_RELEASE_URL:-"https://github.com/$repo/releases"}
release_url=${release_url%/}

tmpdir=""
install_tmp=""
cleanup() {
  if [ -n "$tmpdir" ]; then
    rm -rf "$tmpdir"
  fi
  if [ -n "$install_tmp" ]; then
    rm -f "$install_tmp"
  fi
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'lewbox install: %s\n' "$1" >&2
  exit 1
}

download() {
  url=$1
  dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest" || fail "could not download $url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url" || fail "could not download $url"
  else
    fail "curl or wget is required."
  fi
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print tolower($1) }'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{ print tolower($1) }'
  else
    fail "sha256sum or shasum is required."
  fi
}

# Everything runs from main, called on the last line: if the download is cut
# off while piping into sh, nothing runs at all.
main() {
  if [ "$(id -u)" -eq 0 ] && [ "${LEWBOX_ALLOW_ROOT:-}" != "1" ]; then
    fail "refusing to run as root; run it as your normal user with sudo. See $docs_url
  Set LEWBOX_ALLOW_ROOT=1 to override."
  fi

  case "$(uname -s)" in
    Linux) ;;
    *) fail "only Linux is supported." ;;
  esac

  case "$(uname -m)" in
    x86_64 | amd64) arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *) fail "unsupported architecture $(uname -m); use amd64 or arm64." ;;
  esac

  asset="lewbox_linux_$arch"
  if [ -n "${LEWBOX_VERSION:-}" ]; then
    version=${LEWBOX_VERSION#v}
    [ -n "$version" ] || fail "LEWBOX_VERSION is empty."
    case "$version" in
      *[!0-9A-Za-z._-]*) fail "LEWBOX_VERSION is not a valid release version: $LEWBOX_VERSION" ;;
    esac
    base="$release_url/download/v$version"
  else
    base="$release_url/latest/download"
  fi

  if [ -n "${LEWBOX_INSTALL_DIR:-}" ]; then
    install_dir=$LEWBOX_INSTALL_DIR
  elif [ -n "${HOME:-}" ]; then
    install_dir="$HOME/.local/bin"
  else
    fail "HOME is not set; set LEWBOX_INSTALL_DIR."
  fi

  tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/lewbox-install.XXXXXX") || fail "could not create a temp directory."
  printf 'Downloading %s…\n' "$asset"
  download "$base/$asset" "$tmpdir/$asset"
  download "$base/checksums.txt" "$tmpdir/checksums.txt"

  expected=$(awk -v name="$asset" '
    { file = $2; sub(/^\*/, "", file); if (file == name) { sum = tolower($1); sub(/^sha256:/, "", sum); print sum; exit } }
  ' "$tmpdir/checksums.txt")
  [ -n "$expected" ] || fail "checksums.txt does not list $asset."
  actual=$(hash_file "$tmpdir/$asset")
  [ "$actual" = "$expected" ] || fail "checksum mismatch for $asset; not installing."

  mkdir -p "$install_dir" || fail "could not create $install_dir."
  install_tmp="$install_dir/.lewbox.$$"
  cp "$tmpdir/$asset" "$install_tmp"
  chmod 0755 "$install_tmp"
  mv -f "$install_tmp" "$install_dir/lewbox"
  install_tmp=""
  printf 'Installed lewbox to %s\n' "$install_dir/lewbox"

  case ":${PATH:-}:" in
    *":$install_dir:"*) ;;
    *)
      # The default folder is written with $HOME, the same line the manual steps add.
      if [ "$install_dir" = "${HOME:-}/.local/bin" ]; then
        line='export PATH="$HOME/.local/bin:$PATH"'
      else
        line="export PATH=\"$install_dir:\$PATH\""
      fi
      added=""
      if [ -n "${HOME:-}" ]; then
        for f in "$HOME/.profile" "$HOME/.bashrc"; do
          if [ ! -f "$f" ] || ! grep -qxF "$line" "$f" 2>/dev/null; then
            printf '%s\n' "$line" >> "$f" || fail "could not write $f."
            added="$added $f"
          fi
        done
      fi
      if [ -n "$added" ]; then
        printf 'Added %s to PATH in%s\n' "$install_dir" "$added"
      fi
      printf 'Run this in your current shell:\n  %s\n' "$line"
      ;;
  esac

  installed_version=$("$install_dir/lewbox" version 2>&1) || fail "installed lewbox, but running it failed: $installed_version"
  printf '%s\n' "$installed_version"
}

main "$@"
