#!/usr/bin/env bash
# Usage: publish-release.sh <tag>. Creates a draft and publishes it only after every asset uploads,
# so "latest" never points at a release with missing assets. Needs FORGEJO_TOKEN, FORGEJO_URL and FORGEJO_REPO in the environment.
set -euo pipefail

tag="${1:?usage: publish-release.sh <tag>}"
api="${FORGEJO_URL:?}/api/v1/repos/${FORGEJO_REPO:?}"
auth="Authorization: token ${FORGEJO_TOKEN:?}"

id=$(curl -fsS -X POST -H "$auth" -H "Content-Type: application/json" \
	-d "{\"tag_name\":\"$tag\",\"name\":\"$tag\",\"draft\":true}" "$api/releases" |
	sed -n 's/^{[[:space:]]*"id"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')
[ -n "$id" ] || { echo "publish-release.sh: could not read release id" >&2; exit 1; }

for f in dist/*; do
	curl -fsS -o /dev/null -X POST -H "$auth" -F "attachment=@$f" "$api/releases/$id/assets?name=$(basename "$f")"
done

curl -fsS -o /dev/null -X PATCH -H "$auth" -H "Content-Type: application/json" -d '{"draft":false}' "$api/releases/$id"
