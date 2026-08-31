package-search query:
    docker build --platform linux/amd64 -t nimbus-fedora-packages tools/package-query
    docker run --rm nimbus-fedora-packages search --all {{quote(query)}}
