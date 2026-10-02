#!/usr/bin/env bash
# End-to-end test of install.sh against a local fake release server.
# TEST_INSTALL_REUSE_DIST=1 skips `make release` and reuses an existing dist/.
# shellcheck disable=SC2329 # case_* functions are invoked by name from run_case
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=v0.0.0-test
BAD_VERSION=v0.0.0-bad
NOLINE_VERSION=v0.0.0-noline
REPO=deadstyle/lazyforge

TMP=$(mktemp -d)
PIDS=()
cleanup() {
  for pid in ${PIDS[@]+"${PIDS[@]}"}; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$TMP"
}
trap cleanup EXIT

# Tools install.sh needs, plus a directory that is not the install dir.
BASE_PATH="/usr/bin:/bin:$(dirname "$(command -v curl)")"

free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'
}

# start_server DIR prints the base URL of a server rooted at DIR.
start_server() {
  local port
  port=$(free_port)
  python3 -m http.server "$port" --bind 127.0.0.1 --directory "$1" >/dev/null 2>&1 &
  PIDS+=("$!")
  for _ in $(seq 50); do
    curl -s -o /dev/null "http://127.0.0.1:$port/" && break
    sleep 0.1
  done
  echo "http://127.0.0.1:$port"
}

# run_install NAME=VALUE... runs install.sh with a clean PATH; sets OUT and RC.
run_install() {
  RC=0
  OUT=$(env -i HOME="$TMP/home" PATH="$BASE_PATH" "$@" bash "$ROOT/install.sh" 2>&1) || RC=$?
}

sha_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

HOST_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64 | amd64) HOST_ARCH=amd64 ;;
  *) HOST_ARCH=arm64 ;;
esac

BUILD_OK=1
if [[ "${TEST_INSTALL_REUSE_DIST:-}" != 1 ]]; then
  mkdir -p "$ROOT/dist" && touch "$ROOT/dist/stale-from-previous-build"
  make -C "$ROOT" release VERSION="$VERSION" >"$TMP/build.log" 2>&1 || BUILD_OK=0
fi

# Good tree: latest endpoint plus the release assets. Bad tree: same assets
# under another tag, archives corrupted but checksums.txt left as the good hashes.
SRV_FULL="$TMP/srv-full"
SRV_NOAPI="$TMP/srv-noapi"
GOOD_DL="$SRV_FULL/$REPO/releases/download/$VERSION"
BAD_DL="$SRV_FULL/$REPO/releases/download/$BAD_VERSION"
NOLINE_DL="$SRV_FULL/$REPO/releases/download/$NOLINE_VERSION"
mkdir -p "$GOOD_DL" "$BAD_DL" "$NOLINE_DL" "$SRV_FULL/api/v1/repos/$REPO/releases" "$SRV_NOAPI/$REPO/releases"
cp "$ROOT"/dist/* "$GOOD_DL/" 2>/dev/null || true
for f in "$ROOT"/dist/*.tar.gz; do
  [[ -e "$f" ]] || continue
  name=$(basename "$f")
  { cat "$f"; echo corrupt; } >"$BAD_DL/${name//$VERSION/$BAD_VERSION}"
  cp "$f" "$NOLINE_DL/${name//$VERSION/$NOLINE_VERSION}"
done
if [[ -f "$ROOT/dist/checksums.txt" ]]; then
  sed "s/$VERSION/$BAD_VERSION/" "$ROOT/dist/checksums.txt" >"$BAD_DL/checksums.txt"
fi
: >"$NOLINE_DL/checksums.txt"
printf '{"tag_name":"%s"}' "$VERSION" >"$SRV_FULL/api/v1/repos/$REPO/releases/latest"
ln -s "$SRV_FULL/$REPO/releases/download" "$SRV_NOAPI/$REPO/releases/download"

URL_FULL=$(start_server "$SRV_FULL")
URL_NOAPI=$(start_server "$SRV_NOAPI")

case_release_artifacts() {
  [[ $BUILD_OK == 1 ]] || { cat "$TMP/build.log"; return 1; }
  local want got
  want=$(printf '%s\n' checksums.txt \
    "lazyforge_${VERSION}_darwin_amd64.tar.gz" "lazyforge_${VERSION}_darwin_arm64.tar.gz" \
    "lazyforge_${VERSION}_linux_amd64.tar.gz" "lazyforge_${VERSION}_linux_arm64.tar.gz" | sort)
  got=$(find "$ROOT/dist" -type f -exec basename {} \; | sort)
  [[ "$got" == "$want" ]] || { echo "dist contents: $got"; return 1; }
  [[ $(wc -l <"$ROOT/dist/checksums.txt") -eq 4 ]] || { echo "checksums.txt should list 4 archives"; return 1; }
  if command -v sha256sum >/dev/null; then
    (cd "$ROOT/dist" && sha256sum -c checksums.txt) || return 1
  else
    (cd "$ROOT/dist" && shasum -a 256 -c checksums.txt) || return 1
  fi
  mkdir -p "$TMP/host"
  tar -xzf "$ROOT/dist/lazyforge_${VERSION}_${HOST_OS}_${HOST_ARCH}.tar.gz" -C "$TMP/host" lazyforge || return 1
  "$TMP/host/lazyforge" --version | grep -qF "$VERSION"
}

case_fresh_install() {
  local dir="$TMP/fresh"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  [[ -x "$dir/lazyforge" ]] || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION" || return 1
  grep -qF "$VERSION" <<<"$OUT" && grep -qF "$dir/lazyforge" <<<"$OUT"
}

case_reinstall() {
  local dir="$TMP/again"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir"
  [[ $RC == 0 ]] || return 1
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  [[ "$(ls -A "$dir")" == lazyforge ]] || { ls -A "$dir"; return 1; }
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_corrupt_keeps_existing() {
  local dir="$TMP/keep" before
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir"
  [[ $RC == 0 ]] || return 1
  before=$(sha_of "$dir/lazyforge")
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$BAD_VERSION"
  echo "$OUT"
  [[ $RC != 0 ]] && grep -qiE 'checksum|mismatch|FAILED' <<<"$OUT" || return 1
  [[ "$(sha_of "$dir/lazyforge")" == "$before" ]] || return 1
  [[ "$(ls -A "$dir")" == lazyforge ]]
}

case_corrupt_fresh_installs_nothing() {
  local dir="$TMP/nothing"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$BAD_VERSION"
  echo "$OUT"
  [[ $RC != 0 ]] && grep -qiE 'checksum|mismatch|FAILED' <<<"$OUT" || return 1
  [[ ! -e "$dir/lazyforge" ]]
}

case_missing_checksum_line_installs_nothing() {
  local dir="$TMP/noline"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$NOLINE_VERSION"
  echo "$OUT"
  [[ $RC != 0 ]] && grep -q 'no checksum' <<<"$OUT" || return 1
  [[ ! -e "$dir/lazyforge" ]]
}

case_pinned_version_skips_latest() {
  local dir="$TMP/pinned"
  run_install LAZYFORGE_BASE_URL="$URL_NOAPI" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$VERSION"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_path_hint_when_off_path() {
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$TMP/offpath"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  grep -q PATH <<<"$OUT"
}

case_no_path_hint_when_on_path() {
  local dir="$TMP/onpath"
  BASE_PATH="$dir:$BASE_PATH" run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  ! grep -q PATH <<<"$OUT"
}

FAILED=0
run_case() {
  if "case_$1" >"$TMP/case.log" 2>&1; then
    echo "PASS $1"
  else
    echo "FAIL $1"
    sed 's/^/    /' "$TMP/case.log"
    FAILED=1
  fi
}

for name in release_artifacts fresh_install reinstall corrupt_keeps_existing \
  corrupt_fresh_installs_nothing missing_checksum_line_installs_nothing pinned_version_skips_latest \
  path_hint_when_off_path no_path_hint_when_on_path; do
  run_case "$name"
done
exit "$FAILED"
