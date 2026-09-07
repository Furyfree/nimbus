#!/usr/bin/env bash
# Export a tagged source tree and its Go dependencies for the COPR recipe.
set -euo pipefail

fail() { printf 'release source: %s\n' "$*" >&2; exit 1; }
[ "$#" -eq 2 ] || fail 'usage: source.sh vMAJOR.MINOR.PATCH /absolute/new-output-directory'
tag="$1"
output="$2"
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail 'expected a stable version tag such as v0.1.0'
[[ "$output" = /* ]] || fail 'output directory must be absolute'
if [ -e "$output" ] || [ -L "$output" ]; then
  fail 'output directory already exists'
fi
commit="$(git rev-parse --verify "refs/tags/${tag}^{commit}")" || fail 'tag does not exist'
git merge-base --is-ancestor "$commit" refs/remotes/origin/main || fail 'tag must belong to origin/main'
root="$(git rev-parse --show-toplevel)"
case "$(realpath -m "$output")/" in "$root/"*) fail 'output must be outside the checkout' ;; esac
version="${tag#v}"
epoch="$(git show -s --format=%ct "$commit")"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
source_dir="$work/nimbus-$version"
mkdir "$source_dir"
git archive --format=tar "$commit" | tar -xf - -C "$source_dir"
cd "$source_dir"
[ -f LICENSE ] || fail 'source license is missing'
[ ! -e vendor ] || fail 'vendor must be generated from go.mod and go.sum'
sha256sum go.mod go.sum > "$work/module-checksums"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOFLAGS=''
export GOCACHE="$work/go-cache"
go mod download
go mod verify
go mod vendor
sha256sum --check "$work/module-checksums"
{
  printf 'Tag: %s\nCommit: %s\nToolchain: %s\n\nModules:\n' "$tag" "$commit" "$(go env GOVERSION)"
  go list -m -mod=readonly all
} > "$work/RELEASE-SOURCE.txt"
# Freeze the payload before tests or compilation can create local artifacts.
archive="nimbus-$version-vendor.tar.gz"
(cd "$work" && tar --sort=name --mtime="@$epoch" --owner=0 --group=0 \
  --numeric-owner --format=gnu -cf - "nimbus-$version" | gzip -n > "$archive")
# The same offline build and tests used by the RPM recipe. Build output and
# caches stay outside the source tree.
export GOPROXY=off GOSUMDB=off
go test -mod=vendor ./...
go build -mod=vendor -trimpath -buildvcs=false \
  -ldflags "-X github.com/Furyfree/nimbus/internal/version.Engine=$version" \
  -o "$work/nimbus" ./cmd/nimbus
"$work/nimbus" version
"$work/nimbus" validate --checkout .
cd "$work"
sha256sum "$archive" RELEASE-SOURCE.txt > SHA256SUMS
mkdir -m 700 "$output"
cp -- "$archive" RELEASE-SOURCE.txt SHA256SUMS "$output/"
printf 'Release source ready: %s\n' "$output/$archive"
