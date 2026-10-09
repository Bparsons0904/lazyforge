#!/usr/bin/env bash
set -euo pipefail

BASE="${LAZYFORGE_BASE_URL:-https://git.bobparsons.dev}"
GH_API="${LAZYFORGE_GITHUB_API_URL:-https://api.github.com}"
GH_WEB="${LAZYFORGE_GITHUB_URL:-https://github.com}"
DIR="${LAZYFORGE_INSTALL_DIR:-$HOME/.local/bin}"
REPO="deadstyle/lazyforge"
GH_REPO="Bparsons0904/lazyforge"
pinned="${LAZYFORGE_VERSION:-}"

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

# fetch_from <latest-url> <download-base> <dir> looks up the tag (unless pinned) and downloads the
# archive and checksums.txt into dir. It returns non-zero on any failure, so the caller can try the
# next host. It sets tag and asset.
fetch_from() {
	local latest=$1 base=$2 dir=$3
	mkdir -p "$dir" || return 1
	if [ -n "$pinned" ]; then
		tag=$pinned
	else
		tag=$(curl -fsSL --connect-timeout 10 "$latest" |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1) || return 1
		[ -n "$tag" ] || return 1
	fi
	asset="lazyforge_${tag}_${os}_${arch}.tar.gz"
	curl -fsSL --connect-timeout 10 -o "$dir/$asset" "$base/$tag/$asset" || return 1
	curl -fsSL --connect-timeout 10 -o "$dir/checksums.txt" "$base/$tag/checksums.txt" || return 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

src="$tmp/forgejo"
if ! fetch_from "$BASE/api/v1/repos/$REPO/releases/latest" "$BASE/$REPO/releases/download" "$src"; then
	echo "install.sh: $BASE unreachable or incomplete, falling back to GitHub" >&2
	src="$tmp/github"
	fetch_from "$GH_API/repos/$GH_REPO/releases/latest" "$GH_WEB/$GH_REPO/releases/download" "$src" ||
		die "could not download lazyforge from $BASE or GitHub"
fi

# Exact filename match so a prefix of another asset's name can't be picked.
line=$(awk -v f="$asset" '$2 == f' "$src/checksums.txt")
[ -n "$line" ] || die "no checksum for $asset"
if command -v sha256sum >/dev/null 2>&1; then
	sum() { sha256sum -c -; }
else
	sum() { shasum -a 256 -c -; }
fi
(cd "$src" && echo "$line" | sum >/dev/null 2>&1) || die "checksum mismatch for $asset"

tar -xzf "$src/$asset" -C "$src" lazyforge

# bash 3.2 (macOS) treats an empty array as unset under set -u, so use a function.
priv() { "$@"; }
mkdir -p "$DIR" 2>/dev/null || true
if [ ! -d "$DIR" ] || [ ! -w "$DIR" ]; then
	command -v sudo >/dev/null 2>&1 || die "$DIR is not writable and sudo is not available"
	priv() { sudo "$@"; }
	priv mkdir -p "$DIR"
fi
staged="$DIR/.lazyforge.tmp.$$"
if ! { priv cp "$src/lazyforge" "$staged" && priv chmod 755 "$staged" && priv mv -f "$staged" "$DIR/lazyforge"; }; then
	priv rm -f "$staged"
	die "could not install to $DIR/lazyforge"
fi

echo "installed lazyforge $tag to $DIR/lazyforge"
case ":$PATH:" in
*":$DIR:"*) ;;
*) echo "add $DIR to your PATH to run lazyforge" ;;
esac
