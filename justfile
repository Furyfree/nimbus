# Run the available local checks. Extend with gofmt, go vet, and go test
# ./... once Go code exists.
check:
    git diff --check HEAD
    if command -v markdownlint >/dev/null 2>&1; then markdownlint '*.md' 'docs/**/*.md' 'tools/**/*.md'; fi

package-search query:
    docker build --platform linux/amd64 -t nimbus-fedora-packages tools/package-query
    docker run --rm nimbus-fedora-packages search --all {{quote(query)}}
