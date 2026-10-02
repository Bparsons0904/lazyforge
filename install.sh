#!/usr/bin/env bash
set -euo pipefail

BASE="${LAZYFORGE_BASE_URL:-https://git.bobparsons.dev}"
DIR="${LAZYFORGE_INSTALL_DIR:-$HOME/.local/bin}"
REPO="deadstyle/lazyforge"

die() { echo "install.sh: $*" >&2; exit 1; }

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

tag="${LAZYFORGE_VERSION:-}"
if [ -z "$tag" ]; then
	tag=$(curl -fsSL "$BASE/api/v1/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1) ||
		die "could not look up the latest release"
	[ -n "$tag" ] || die "no tag_name in the latest release response"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

asset="lazyforge_${tag}_${os}_${arch}.tar.gz"
url="$BASE/$REPO/releases/download/$tag"
curl -fsSL -o "$tmp/$asset" "$url/$asset" || die "download failed: $url/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt" || die "download failed: $url/checksums.txt"

# Exact filename match so a prefix of another asset's name can't be picked.
line=$(awk -v f="$asset" '$2 == f' "$tmp/checksums.txt")
[ -n "$line" ] || die "no checksum for $asset"
if command -v sha256sum >/dev/null 2>&1; then
	sum() { sha256sum -c -; }
else
	sum() { shasum -a 256 -c -; }
fi
(cd "$tmp" && echo "$line" | sum >/dev/null 2>&1) || die "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp" lazyforge

# bash 3.2 (macOS) treats an empty array as unset under set -u, so use a function.
priv() { "$@"; }
mkdir -p "$DIR" 2>/dev/null || true
if [ ! -d "$DIR" ] || [ ! -w "$DIR" ]; then
	command -v sudo >/dev/null 2>&1 || die "$DIR is not writable and sudo is not available"
	priv() { sudo "$@"; }
	priv mkdir -p "$DIR"
fi
staged="$DIR/.lazyforge.tmp.$$"
if ! { priv cp "$tmp/lazyforge" "$staged" && priv chmod 755 "$staged" && priv mv -f "$staged" "$DIR/lazyforge"; }; then
	priv rm -f "$staged"
	die "could not install to $DIR/lazyforge"
fi

echo "installed lazyforge $tag to $DIR/lazyforge"
case ":$PATH:" in
*":$DIR:"*) ;;
*) echo "add $DIR to your PATH to run lazyforge" ;;
esac
