# Native package source test

Build the disposable Fedora 44 image and run the opt-in test:

~~~sh
docker build -t nimbus-dnf-sources-test tools/dnf-sources
NIMBUS_DNF_SOURCE_TEST_IMAGE=nimbus-dnf-sources-test go test ./internal/plan \
  -run '^TestNativeDNFPackageSources$' -count=1 -v
~~~

The Go test sends production-generated commands to a network-disabled container.
The container builds empty fixture RPMs, signs them with an ephemeral test key
and retains native signature checking. It tests mixed source installation,
dependencies from another source, competing priorities, constrained upgrades,
disabled repositories, downgrade correction, same-version reinstall and refusal
when the declared source lacks a package. No host package or configuration is
changed. Ordinary tests skip this opt-in check.

Remove the image with `docker image rm nimbus-dnf-sources-test` when finished.
This solver test does not replace the pending graphical Fedora VM apply drill.
