FROM registry.fedoraproject.org/fedora:44

RUN dnf5 -y install \
      https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-44.noarch.rpm \
      https://download1.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-44.noarch.rpm \
    && dnf5 -y install --nogpgcheck \
      --repofrompath 'terra,https://repos.fyralabs.com/terra44' \
      terra-release terra-gpg-keys \
    && dnf5 clean all

# -y accepts repository key imports inside the disposable container.
ENTRYPOINT ["dnf5", "-y"]
CMD ["--help"]
