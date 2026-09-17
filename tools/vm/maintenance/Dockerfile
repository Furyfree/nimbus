FROM registry.fedoraproject.org/fedora:44
RUN dnf5 -y install rpm-build rpm-sign gnupg2 createrepo_c git sudo shadow-utils util-linux-script && dnf5 clean all
ENTRYPOINT ["/bin/bash", "/input/test.sh"]
