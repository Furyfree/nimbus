# The complete local gate.
check: fmt-check vet test
    git diff --check HEAD
    if command -v markdownlint >/dev/null 2>&1; then markdownlint '*.md' 'docs/**/*.md' 'tools/**/*.md'; else echo 'markdownlint not installed: skipped'; fi

fmt-check:
    test -z "$(gofmt -l .)" || { gofmt -l .; echo 'gofmt: files need formatting'; exit 1; }

vet:
    go vet ./...

test:
    go test ./...

# Validate the definitions in this checkout.
validate:
    go run ./cmd/nimbus validate --checkout .

package-search query:
    docker build --platform linux/amd64 -t nimbus-fedora-packages tools/package-query
    docker run --rm nimbus-fedora-packages search --all {{quote(query)}}
