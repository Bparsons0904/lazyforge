#!/usr/bin/env bash
# Usage: publish-github-release.sh <tag> <asset-dir>. Mirrors a Forgejo release to GitHub. The release
# is published only after every asset uploads, so GitHub's "latest" never points at a partial release.
set -euo pipefail

tag="${1:?usage: publish-github-release.sh <tag> <asset-dir>}"
dir="${2:?usage: publish-github-release.sh <tag> <asset-dir>}"
token="${GITHUB_RELEASE_TOKEN:?GITHUB_RELEASE_TOKEN is not set (Forgejo secret GH_RELEASE_TOKEN)}"
repo="${GH_RELEASE_REPO:-Bparsons0904/lazyforge}"
api="${GH_RELEASE_API_URL:-https://api.github.com}"
uploads="${GH_RELEASE_UPLOADS_URL:-https://uploads.github.com}"
wait_s="${GH_RELEASE_TAG_WAIT_SECONDS:-300}"
poll_s="${GH_RELEASE_TAG_POLL_SECONDS:-5}"
auth="Authorization: Bearer $token"
accept="Accept: application/vnd.github+json"

shopt -s nullglob
assets=("$dir"/*)
[ "${#assets[@]}" -gt 0 ] || { echo "publish-github-release.sh: no files in $dir" >&2; exit 1; }

# github-push.yml pushes the tag in a race with this run, so wait for it rather than creating it.
deadline=$((SECONDS + wait_s))
until [ "$(curl -s -o /dev/null -w '%{http_code}' -H "$auth" -H "$accept" "$api/repos/$repo/git/ref/tags/$tag")" = 200 ]; do
	[ "$SECONDS" -lt "$deadline" ] || { echo "publish-github-release.sh: tag $tag never appeared in $repo" >&2; exit 1; }
	sleep "$poll_s"
done

# Lists drafts too: GET releases/tags/<tag> returns 404 for a draft, which would let a rerun collide with it.
exists=$(curl -fsS -H "$auth" -H "$accept" "$api/repos/$repo/releases?per_page=100" |
	python3 -c 'import json, sys
tag = sys.argv[1]
print("yes" if any(r["tag_name"] == tag for r in json.load(sys.stdin)) else "no")' "$tag")
if [ "$exists" = yes ]; then
	echo "publish-github-release.sh: release $tag already exists in $repo" >&2
	exit 1
fi

id=$(curl -fsS -X POST -H "$auth" -H "$accept" -H "Content-Type: application/json" \
	-d "{\"tag_name\":\"$tag\",\"name\":\"$tag\",\"draft\":true}" "$api/repos/$repo/releases" |
	python3 -c 'import json, sys; print(json.load(sys.stdin)["id"])')

for f in "${assets[@]}"; do
	curl -fsS -o /dev/null -X POST -H "$auth" -H "$accept" -H "Content-Type: application/octet-stream" \
		--data-binary "@$f" "$uploads/repos/$repo/releases/$id/assets?name=$(basename "$f")"
done

curl -fsS -o /dev/null -X PATCH -H "$auth" -H "$accept" -H "Content-Type: application/json" \
	-d '{"draft":false}' "$api/repos/$repo/releases/$id"
