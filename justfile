# The complete local gate.
check: fmt-check vet test
    git diff --check HEAD
    if command -v markdownlint >/dev/null 2>&1; then markdownlint '*.md' 'docs/**/*.md' 'tools/**/*.md'; else echo 'markdownlint not installed: skipped'; fi
    if command -v shellcheck >/dev/null 2>&1; then shellcheck install.sh bootstrap tools/release/*.sh; else echo 'shellcheck not installed: skipped'; fi

fmt-check:
    test -z "$(gofmt -l .)" || { gofmt -l .; echo 'gofmt: files need formatting'; exit 1; }

vet:
    go vet ./...

test:
    go test ./...

# Validate the definitions in this checkout.
validate:
    go run ./cmd/nimbus validate --checkout .

# Build a static binary that runs on any x86_64 Linux, such as the test VM.
# -s -w drops the symbol table and DWARF debug data; panics still print
# their stack traces.
build:
    CGO_ENABLED=0 go build -ldflags='-s -w' -o nimbus ./cmd/nimbus

# Create and push an annotated version tag from clean, up-to-date main.
tag version:
    bash tools/release/tag.sh {{quote(version)}}

# Start manual draft-release preparation for an existing remote tag.
release tag:
    gh workflow run release.yml --repo Furyfree/nimbus --ref main -f {{quote("tag=" + tag)}}

# The .git directory goes along so the selector can read the origin; the VM
# has no git. A running binary cannot be overwritten, so the new one is moved
# into place.

# Push the build and definitions to the drill VM; run nimbus init there yourself.
vm-push host="pby@127.0.0.1" port="2222": build
    scp -P {{port}} nimbus {{host}}:.local/bin/nimbus.new
    ssh -p {{port}} {{host}} 'mv .local/bin/nimbus.new .local/bin/nimbus'
    tar cz nimbus.toml machines profiles components system install.sh bootstrap .git | ssh -p {{port}} {{host}} 'rm -rf .local/share/nimbus && mkdir -p .local/share/nimbus && tar xz -C .local/share/nimbus'

package-search query:
    docker build --platform linux/amd64 -t nimbus-fedora-packages tools/package-query
    docker run --rm nimbus-fedora-packages search --all {{quote(query)}}
