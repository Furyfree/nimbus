FROM registry.fedoraproject.org/fedora:44
RUN dnf -y install NetworkManager NetworkManager-wifi python3 python3-dbus python3-gobject-base dbus-daemon && dnf clean all
COPY internal/postinstall/dtu/network.py internal/postinstall/dtu/test_network.py /test/
COPY internal/postinstall/dtu/ca.pem /etc/NetworkManager/certs/dtu-eduroam.pem
COPY tools/vm/dtu/native.py /test/native.py
CMD ["python3", "-I", "-B", "/test/native.py"]
