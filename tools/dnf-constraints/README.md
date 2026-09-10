# Native constraint test

Build the disposable Fedora 44 image, then opt into the native solver test:

~~~sh
docker build -t nimbus-dnf-test tools/dnf-constraints
NIMBUS_DNF_TEST_IMAGE=nimbus-dnf-test go test ./internal/plan \
  -run '^TestNativeDNFConstraints$' -count=1 -v
~~~

The test renders policy from the VM manifest through the Go implementation,
then mounts it read-only into a network-disabled container. Synthetic, empty
RPMs exercise range boundaries, prereleases, epoch changes, similarly named
subpackages, solver conflicts, patch and packaging upgrades, downgrade refusal,
and removal of the generated policy. Only the fixture repository disables
signature checking. No host package, repository, selector, or receipt changes.

Ordinary `go test` skips this opt-in test. Remove the test image with
`docker image rm nimbus-dnf-test` when finished. A passing solver test does not
prove desktop ABI compatibility or replace the pending Fedora VM apply drill.
