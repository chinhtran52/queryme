#!/bin/sh
# Installs the latest queryme release:
#   curl -fsSL https://raw.githubusercontent.com/chinhtran52/queryme/main/install.sh | sh
# Env: QUERYME_VERSION (e.g. v1.0.1, default latest), QUERYME_INSTALL_DIR (default ~/.local/bin).
set -eu

REPO="chinhtran52/queryme"
INSTALL_DIR="${QUERYME_INSTALL_DIR:-$HOME/.local/bin}"

fail() { echo "queryme install: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS $(uname -s); build from source instead" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m); build from source instead" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL -o "$2" "$1"; }
  latest() { curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  latest() { wget -S --spider "https://github.com/$REPO/releases/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: //p' | tail -n 1; }
else
  fail "curl or wget is required"
fi

version="${QUERYME_VERSION:-}"
if [ -z "$version" ]; then
  # The /releases/latest redirect ends in the tag, which avoids the rate-limited API.
  version="$(latest | tr -d '\r')"
  version="${version##*/}"
  case "$version" in v*) ;; *) fail "could not find the latest release" ;; esac
fi

archive="queryme_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading queryme $version ($os/$arch)..."
fetch "$base/$archive" "$tmp/$archive" || fail "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

want="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
else
  got="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" queryme
mkdir -p "$INSTALL_DIR"
install -m 755 "$tmp/queryme" "$INSTALL_DIR/queryme"
echo "Installed $INSTALL_DIR/queryme ($version)"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add it to your PATH: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
