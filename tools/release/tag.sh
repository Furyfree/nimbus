#!/usr/bin/env bash
# Tag the clean, current main commit and push only that tag.
set -euo pipefail

fail() { printf 'release tag: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 1 ] || fail 'usage: tag.sh vMAJOR.MINOR.PATCH'
tag="$1"
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail 'expected a stable version tag such as v0.1.0'
[ "$(git branch --show-current)" = main ] || fail 'switch to main before tagging'
[ -z "$(git status --porcelain)" ] || fail 'working tree must be clean before tagging'
commit="$(git rev-parse HEAD)"
remote_main="$(git ls-remote --exit-code origin refs/heads/main)"
[ "${remote_main%%[[:space:]]*}" = "$commit" ] || fail 'main differs from origin/main; update and review it before tagging'
remote_tag="$(git ls-remote origin "refs/tags/$tag")"
[ -z "$remote_tag" ] || fail 'tag already exists on origin; use a new version'

if git show-ref --verify --quiet "refs/tags/$tag"; then
  # A failed push leaves its local tag intact for a retry, without moving it.
  [ "$(git cat-file -t "refs/tags/$tag")" = tag ] || fail 'existing local tag is not annotated'
  [ "$(git rev-parse "refs/tags/${tag}^{commit}")" = "$commit" ] || fail 'existing local tag points at another commit'
else
  git tag -a "$tag" "$commit" -m "Nimbus $tag"
fi
if ! git push --no-follow-tags origin "refs/tags/$tag"; then
  fail "push failed; the local $tag tag was preserved. Retry after fixing the push failure"
fi
printf 'Tagged %s as %s. Start draft preparation with: just release %s\n' "$commit" "$tag" "$tag"
