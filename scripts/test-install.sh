#!/usr/bin/env bash
# End-to-end test of install.sh against a local fake release server, and of
# publish-github-release.sh against a fake GitHub API.
# TEST_INSTALL_REUSE_DIST=1 skips `make release` and reuses an existing dist/, which must
# have been built with VERSION=$TEST_INSTALL_VERSION (default v0.0.0-test).
# shellcheck disable=SC2329 # case_* functions are invoked by name from run_case
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=${TEST_INSTALL_VERSION:-v0.0.0-test}
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
BASE_PATH="/usr/bin:/bin:$(dirname "$(command -v curl)"):$(dirname "$(command -v python3)")"

free_port() {
  python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'
}

# start_server DIR sets SERVER_URL to the base URL of a server rooted at DIR. It runs in the
# current shell, not a subshell, so its PID lands in PIDS and cleanup stops it.
start_server() {
  local port
  port=$(free_port)
  python3 -m http.server "$port" --bind 127.0.0.1 --directory "$1" >/dev/null 2>&1 &
  PIDS+=("$!")
  for _ in $(seq 50); do
    curl -s -o /dev/null "http://127.0.0.1:$port/" && break
    sleep 0.1
  done
  SERVER_URL="http://127.0.0.1:$port"
}

# run_install NAME=VALUE... runs install.sh with a clean PATH; sets OUT and RC.
run_install() {
  RC=0
  OUT=$(env -i HOME="$TMP/home" PATH="$BASE_PATH" "$@" bash "$ROOT/install.sh" 2>&1) || RC=$?
}

# run_install_stderr NAME=VALUE... is run_install, but sets ERR to stderr alone.
run_install_stderr() {
  RC=0
  ERR=$(env -i HOME="$TMP/home" PATH="$BASE_PATH" "$@" bash "$ROOT/install.sh" 2>&1 >/dev/null) || RC=$?
}

# run_publish TAG DIR NAME=VALUE... runs publish-github-release.sh with a clean environment; sets OUT and RC.
run_publish() {
  local tag=$1 dir=$2
  shift 2
  RC=0
  OUT=$(env -i HOME="$TMP/home" PATH="$BASE_PATH" "$@" bash "$ROOT/scripts/publish-github-release.sh" "$tag" "$dir" 2>&1) || RC=$?
}

# new_fake_api LOG [NAME=VALUE...] starts the fake GitHub API with the given knobs, logging
# each request to LOG. It sets API_URL and PUB_ENV, the env every publish case shares.
new_fake_api() {
  local port log=$1
  shift
  port=$(free_port)
  : >"$log"
  env "$@" python3 "$TMP/fake_github_api.py" "$port" "$log" >/dev/null 2>&1 &
  PIDS+=("$!")
  API_URL="http://127.0.0.1:$port"
  for _ in $(seq 50); do
    curl -s -o /dev/null "$API_URL/" && break
    sleep 0.1
  done
  PUB_ENV=(GH_RELEASE_API_URL="$API_URL" GH_RELEASE_UPLOADS_URL="$API_URL/uploads" \
    GH_RELEASE_TAG_WAIT_SECONDS=1 GH_RELEASE_TAG_POLL_SECONDS=0.2)
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

start_server "$SRV_FULL"
URL_FULL=$SERVER_URL
start_server "$SRV_NOAPI"
URL_NOAPI=$SERVER_URL

# Forgejo trees for the host-mixing cases. ARCH404 has latest for VERSION and no archive for it,
# with zeroed checksums, so a mixed install would fail verification. CSUM404 has a corrupt archive
# for VERSION and no checksums.txt, so a mixed install would fail verification.
SRV_ARCH404="$TMP/srv-arch404"
SRV_CSUM404="$TMP/srv-csum404"
ARCH404_DL="$SRV_ARCH404/$REPO/releases/download/$VERSION"
CSUM404_DL="$SRV_CSUM404/$REPO/releases/download/$VERSION"
mkdir -p "$ARCH404_DL" "$CSUM404_DL" "$SRV_ARCH404/api/v1/repos/$REPO/releases" "$SRV_CSUM404/api/v1/repos/$REPO/releases"
printf '{"tag_name":"%s"}' "$VERSION" | tee "$SRV_ARCH404/api/v1/repos/$REPO/releases/latest" >"$SRV_CSUM404/api/v1/repos/$REPO/releases/latest"
if [[ -f "$ROOT/dist/checksums.txt" ]]; then
  awk '{ print sprintf("%064d", 0) "  " $2 }' "$ROOT/dist/checksums.txt" >"$ARCH404_DL/checksums.txt"
fi
for f in "$ROOT"/dist/*.tar.gz "$ROOT"/dist/*.zip; do
  [[ -e "$f" ]] || continue
  echo corrupt >"$CSUM404_DL/$(basename "$f")"
done
start_server "$SRV_ARCH404"
URL_ARCH404=$SERVER_URL
start_server "$SRV_CSUM404"
URL_CSUM404=$SERVER_URL

# NOTAG answers releases/latest with 200 and no tag_name, and has no assets, so only a fallback installs.
SRV_NOTAG="$TMP/srv-notag"
mkdir -p "$SRV_NOTAG/api/v1/repos/$REPO/releases"
printf '{"name":"x"}' >"$SRV_NOTAG/api/v1/repos/$REPO/releases/latest"
start_server "$SRV_NOTAG"
URL_NOTAG=$SERVER_URL

# Forgejo URL nothing listens on.
FORGEJO_DOWN="http://127.0.0.1:$(free_port)"

# GitHub tree behind a logging server: the API root serves latest, the web root serves the good
# release. Every request is appended to GH_LOG, so "never asked GitHub" is checkable.
GH_TREE="$TMP/github"
GH_LOG="$TMP/github.log"
GH_DL="$GH_TREE/web/Bparsons0904/lazyforge/releases/download/$VERSION"
mkdir -p "$GH_DL" "$GH_TREE/api/repos/Bparsons0904/lazyforge/releases"
cp "$ROOT"/dist/* "$GH_DL/" 2>/dev/null || true
printf '{"tag_name":"%s"}' "$VERSION" >"$GH_TREE/api/repos/Bparsons0904/lazyforge/releases/latest"
cat >"$TMP/github_server.py" <<'PY'
import functools
import http.server
import sys

port, root, log = int(sys.argv[1]), sys.argv[2], sys.argv[3]


class Handler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, fmt, *args):
        with open(log, "a") as f:
            f.write(self.path + "\n")


http.server.ThreadingHTTPServer(
    ("127.0.0.1", port), functools.partial(Handler, directory=root)
).serve_forever()
PY
GH_PORT=$(free_port)
python3 "$TMP/github_server.py" "$GH_PORT" "$GH_TREE" "$GH_LOG" >/dev/null 2>&1 &
PIDS+=("$!")
for _ in $(seq 50); do
  curl -s -o /dev/null "http://127.0.0.1:$GH_PORT/" && break
  sleep 0.1
done
GH_ENV=(LAZYFORGE_GITHUB_API_URL="http://127.0.0.1:$GH_PORT/api" LAZYFORGE_GITHUB_URL="http://127.0.0.1:$GH_PORT/web")

# Fake GitHub API for publish-github-release.sh: release list, ref, create, upload, patch.
cat >"$TMP/fake_github_api.py" <<'PY'
import http.server
import json
import os
import sys

port, log = int(sys.argv[1]), sys.argv[2]
REPO = "Bparsons0904/lazyforge"
RELEASE_ID = 7
state = {"tag": ""}


def record(line):
    with open(log, "a") as f:
        f.write(line + "\n")


def release(tag, draft):
    return {
        "url": f"https://api.github.com/repos/{REPO}/releases/{RELEASE_ID}",
        "assets_url": f"https://api.github.com/repos/{REPO}/releases/{RELEASE_ID}/assets",
        "upload_url": f"https://uploads.github.com/repos/{REPO}/releases/{RELEASE_ID}/assets{{?name,label}}",
        "id": RELEASE_ID,
        "author": {"login": "github-actions[bot]", "id": 41898282},
        "tag_name": tag,
        "name": tag,
        "draft": draft,
        "prerelease": False,
        "assets": [],
    }


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, status, payload):
        body = json.dumps(payload, indent=2).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_body(self):
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def do_GET(self):
        if self.path == "/":
            return self.reply(200, {})
        if self.path.startswith(f"/repos/{REPO}/git/ref/tags/"):
            record(f"ref-get {self.path}")
            status = int(os.environ.get("FAKE_REF_STATUS", "200"))
            if status != 200:
                return self.reply(status, {"message": "Not Found"})
            tag = self.path.rsplit("/", 1)[1]
            return self.reply(200, {"ref": f"refs/tags/{tag}", "object": {"sha": "0" * 40, "type": "commit"}})
        if self.path.startswith(f"/repos/{REPO}/releases?"):
            record(f"releases-list {self.path}")
            releases = []
            if os.environ.get("FAKE_EXISTING_TAG"):
                draft = os.environ.get("FAKE_EXISTING_DRAFT") == "true"
                releases.append(release(os.environ["FAKE_EXISTING_TAG"], draft))
            return self.reply(200, releases)
        record(f"unhandled GET {self.path}")
        self.reply(404, {"message": "Not Found"})

    def do_POST(self):
        data = self.read_body()
        if self.path == f"/repos/{REPO}/releases":
            payload = json.loads(data)
            state["tag"] = payload["tag_name"]
            record(f"release-create {self.path} draft={json.dumps(payload.get('draft', False))}")
            return self.reply(201, release(state["tag"], payload.get("draft", False)))
        if self.path.startswith(f"/uploads/repos/{REPO}/releases/{RELEASE_ID}/assets?name="):
            record(f"asset-upload {self.path}")
            status = int(os.environ.get("FAKE_UPLOAD_STATUS", "201"))
            return self.reply(status, {"id": 1} if status < 400 else {"message": "upload failed"})
        record(f"unhandled POST {self.path}")
        self.reply(404, {"message": "Not Found"})

    def do_PATCH(self):
        payload = json.loads(self.read_body())
        record(f"release-patch {self.path} draft={json.dumps(payload.get('draft'))}")
        self.reply(200, release(state["tag"], payload.get("draft", False)))


http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

PUBLISH_ASSETS="$TMP/assets"
mkdir -p "$PUBLISH_ASSETS"
printf 'archive' >"$PUBLISH_ASSETS/lazyforge_${VERSION}_linux_amd64.tar.gz"
printf 'sums' >"$PUBLISH_ASSETS/checksums.txt"

case_release_artifacts() {
  [[ $BUILD_OK == 1 ]] || { cat "$TMP/build.log"; return 1; }
  local want got
  want=$(printf '%s\n' checksums.txt \
    "lazyforge_${VERSION}_darwin_amd64.tar.gz" "lazyforge_${VERSION}_darwin_arm64.tar.gz" \
    "lazyforge_${VERSION}_linux_amd64.tar.gz" "lazyforge_${VERSION}_linux_arm64.tar.gz" \
    "lazyforge_${VERSION}_windows_amd64.zip" "lazyforge_${VERSION}_windows_arm64.zip" | sort)
  got=$(find "$ROOT/dist" -type f -exec basename {} \; | sort)
  [[ "$got" == "$want" ]] || { echo "dist contents: $got"; return 1; }
  [[ $(wc -l <"$ROOT/dist/checksums.txt") -eq 6 ]] || { echo "checksums.txt should list 6 archives"; return 1; }
  if command -v sha256sum >/dev/null; then
    (cd "$ROOT/dist" && sha256sum -c checksums.txt) || return 1
  else
    (cd "$ROOT/dist" && shasum -a 256 -c checksums.txt) || return 1
  fi
  mkdir -p "$TMP/host"
  tar -xzf "$ROOT/dist/lazyforge_${VERSION}_${HOST_OS}_${HOST_ARCH}.tar.gz" -C "$TMP/host" lazyforge || return 1
  "$TMP/host/lazyforge" --version | grep -qF "$VERSION"
}

case_windows_zips() {
  [[ $BUILD_OK == 1 ]] || { cat "$TMP/build.log"; return 1; }
  local arch name entries
  for arch in amd64 arm64; do
    name="lazyforge_${VERSION}_windows_${arch}.zip"
    [[ -f "$ROOT/dist/$name" ]] || { echo "missing $name"; return 1; }
    entries=$(python3 -m zipfile -l "$ROOT/dist/$name" | awk 'NR > 1 { print $1 }')
    [[ "$entries" == lazyforge.exe ]] || { echo "$name entries: $entries"; return 1; }
    awk -v n="$name" '$2 == n { found = 1 } END { exit !found }' "$ROOT/dist/checksums.txt" \
      || { echo "checksums.txt has no line for $name"; return 1; }
  done
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

case_forgejo_down_falls_back() {
  local dir="$TMP/down"
  run_install_stderr LAZYFORGE_BASE_URL="$FORGEJO_DOWN" LAZYFORGE_INSTALL_DIR="$dir" "${GH_ENV[@]}"
  echo "$ERR"
  [[ $RC == 0 ]] || return 1
  grep -q 'falling back to GitHub' <<<"$ERR" || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_forgejo_up_never_asks_github() {
  local dir="$TMP/healthy"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  [[ ! -s "$GH_LOG" ]] || { cat "$GH_LOG"; return 1; }
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_checksum_mismatch_never_falls_back() {
  local dir="$TMP/mismatch"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$BAD_VERSION" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC != 0 ]] && grep -qiE 'checksum|mismatch|FAILED' <<<"$OUT" || return 1
  [[ ! -s "$GH_LOG" ]] || { cat "$GH_LOG"; return 1; }
  [[ ! -e "$dir/lazyforge" ]]
}

case_pinned_noline_never_falls_back() {
  local dir="$TMP/pinned-noline"
  run_install LAZYFORGE_BASE_URL="$URL_FULL" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$NOLINE_VERSION" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC != 0 ]] || return 1
  [[ ! -s "$GH_LOG" ]] || { cat "$GH_LOG"; return 1; }
  [[ ! -e "$dir/lazyforge" ]]
}

case_archive_missing_on_forgejo_installs_from_github() {
  local dir="$TMP/arch404"
  run_install LAZYFORGE_BASE_URL="$URL_ARCH404" LAZYFORGE_INSTALL_DIR="$dir" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  grep -q "download/$VERSION/lazyforge_${VERSION}_${HOST_OS}_${HOST_ARCH}.tar.gz" "$GH_LOG" || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_checksums_missing_on_forgejo_installs_from_github() {
  local dir="$TMP/csum404"
  run_install LAZYFORGE_BASE_URL="$URL_CSUM404" LAZYFORGE_INSTALL_DIR="$dir" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  [[ -s "$GH_LOG" ]] || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_latest_without_tag_on_forgejo_installs_from_github() {
  local dir="$TMP/notag"
  run_install LAZYFORGE_BASE_URL="$URL_NOTAG" LAZYFORGE_INSTALL_DIR="$dir" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  [[ -s "$GH_LOG" ]] || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_pinned_version_with_forgejo_down_installs_from_github() {
  local dir="$TMP/pinned-down"
  run_install LAZYFORGE_BASE_URL="$FORGEJO_DOWN" LAZYFORGE_INSTALL_DIR="$dir" LAZYFORGE_VERSION="$VERSION" "${GH_ENV[@]}"
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  if grep -q 'releases/latest' "$GH_LOG"; then
    cat "$GH_LOG"
    return 1
  fi
  grep -q "download/$VERSION/" "$GH_LOG" || return 1
  "$dir/lazyforge" --version | grep -qF "$VERSION"
}

case_publish_happy_path() {
  local log="$TMP/publish-happy.log"
  new_fake_api "$log"
  run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=test-token
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  cat "$log"
  [[ $(awk '{ print $1 }' "$log" | uniq | paste -sd, -) == ref-get,releases-list,release-create,asset-upload,release-patch ]] || return 1
  grep -q '^release-create .* draft=true$' "$log" || return 1
  grep -q '^release-patch .* draft=false$' "$log" || return 1
  [[ $(grep -c '^asset-upload ' "$log") == 2 ]] || return 1
  [[ $(grep '^asset-upload ' "$log" | sed 's/.*name=//' | sort | paste -sd, -) == "checksums.txt,lazyforge_${VERSION}_linux_amd64.tar.gz" ]]
}

case_publish_waits_then_fails_on_missing_tag() {
  local log="$TMP/publish-wait.log"
  new_fake_api "$log" FAKE_REF_STATUS=404
  run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=test-token
  echo "$OUT"
  [[ $RC != 0 ]] || return 1
  grep -qF "$VERSION" <<<"$OUT" || return 1
  if grep -q '^release-create' "$log"; then
    cat "$log"
    return 1
  fi
  [[ $(grep -c '^ref-get' "$log") -ge 2 ]]
}

case_publish_refuses_existing_release() {
  local draft log
  for draft in false true; do
    log="$TMP/publish-exists-$draft.log"
    new_fake_api "$log" FAKE_EXISTING_TAG="$VERSION" FAKE_EXISTING_DRAFT="$draft"
    run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=test-token
    echo "draft=$draft: $OUT"
    [[ $RC != 0 ]] || return 1
    grep -q 'already exists' <<<"$OUT" || return 1
    if grep -q '^release-create' "$log"; then
      cat "$log"
      return 1
    fi
  done
}

case_publish_upload_failure_stays_draft() {
  local log="$TMP/publish-500.log"
  new_fake_api "$log" FAKE_UPLOAD_STATUS=500
  run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=test-token
  echo "$OUT"
  [[ $RC != 0 ]] || return 1
  grep -q '^asset-upload' "$log" || return 1
  if grep -q '^release-patch .*draft=false' "$log"; then
    cat "$log"
    return 1
  fi
}

case_publish_requires_token() {
  local log="$TMP/publish-token.log" token
  new_fake_api "$log"
  for token in unset empty; do
    if [[ $token == unset ]]; then
      run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}"
    else
      run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=
    fi
    echo "token $token: $OUT"
    [[ $RC != 0 ]] || return 1
    grep -q 'RELEASE_TOKEN' <<<"$OUT" || return 1
  done
  [[ ! -s "$log" ]]
}

case_publish_ignores_legacy_overrides() {
  local log="$TMP/publish-env.log"
  new_fake_api "$log"
  run_publish "$VERSION" "$PUBLISH_ASSETS" "${PUB_ENV[@]}" GITHUB_RELEASE_TOKEN=test-token \
    GITHUB_API_URL=http://127.0.0.1:1 GITHUB_UPLOADS_URL=http://127.0.0.1:1 GITHUB_REPO=wrong/repo
  echo "$OUT"
  [[ $RC == 0 ]] || return 1
  grep -q 'wrong/repo' "$log" && return 1
  grep -q '/repos/Bparsons0904/lazyforge/' "$log"
}

FAILED=0
run_case() {
  : >"$GH_LOG"
  if "case_$1" >"$TMP/case.log" 2>&1; then
    echo "PASS $1"
  else
    echo "FAIL $1"
    sed 's/^/    /' "$TMP/case.log"
    FAILED=1
  fi
}

for name in release_artifacts windows_zips fresh_install reinstall corrupt_keeps_existing \
  corrupt_fresh_installs_nothing missing_checksum_line_installs_nothing pinned_version_skips_latest \
  path_hint_when_off_path no_path_hint_when_on_path \
  forgejo_down_falls_back forgejo_up_never_asks_github checksum_mismatch_never_falls_back \
  pinned_noline_never_falls_back archive_missing_on_forgejo_installs_from_github \
  checksums_missing_on_forgejo_installs_from_github latest_without_tag_on_forgejo_installs_from_github pinned_version_with_forgejo_down_installs_from_github \
  publish_happy_path publish_waits_then_fails_on_missing_tag publish_refuses_existing_release \
  publish_upload_failure_stays_draft publish_requires_token publish_ignores_legacy_overrides; do
  run_case "$name"
done
exit "$FAILED"
