FROM registry.fedoraproject.org/fedora:44

RUN dnf5 -y install \
      https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-44.noarch.rpm \
      https://download1.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-44.noarch.rpm \
    && dnf5 clean all

ENTRYPOINT ["dnf5"]
CMD ["--help"]
