# Fedora package query

This image queries Fedora 44, RPM Fusion Free, RPM Fusion Nonfree, and Terra
package metadata. It changes no host package or repository configuration; it
leaves a Docker image and build cache that
`docker image rm nimbus-fedora-packages` removes.

~~~sh
just package-search ripgrep
just package-search "NVIDIA driver"
~~~

For other DNF5 queries, build the image and run DNF5 directly:

~~~sh
docker build --platform linux/amd64 \
  -t nimbus-fedora-packages tools/package-query
docker run --rm nimbus-fedora-packages search ripgrep
docker run --rm nimbus-fedora-packages info ripgrep
docker run --rm nimbus-fedora-packages repoquery --info ripgrep
docker run --rm nimbus-fedora-packages provides /usr/bin/rg
~~~

Each query uses a temporary container. Results are research input. Accepted
packages become bare names in profiles, components, or the machine manifest,
or `<repository>:<name>` references when the source is not Fedora.
