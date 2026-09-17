"""Disposable container only: real NM persistence with no agent or a non-saving agent."""
import importlib.util
import contextlib
import io
import os
from pathlib import Path
import subprocess
import sys
import time
from unittest.mock import patch


def load():
    spec = importlib.util.spec_from_file_location("dtu", "/test/network.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def agent():
    import dbus
    import dbus.service
    from dbus.mainloop.glib import DBusGMainLoop
    from gi.repository import GLib
    DBusGMainLoop(set_as_default=True)
    bus = dbus.SystemBus()
    interface = "org.freedesktop.NetworkManager.SecretAgent"

    class Agent(dbus.service.Object):
        @dbus.service.method(interface, in_signature="a{sa{sv}}osasu", out_signature="a{sa{sv}}")
        def GetSecrets(self, connection, path, setting, hints, flags):
            raise dbus.exceptions.DBusException(
                "No stored secrets", name="org.freedesktop.NetworkManager.SecretAgent.NoSecrets")

        @dbus.service.method(interface, in_signature="os", out_signature="")
        def CancelGetSecrets(self, path, setting):
            pass

        @dbus.service.method(interface, in_signature="a{sa{sv}}o", out_signature="")
        def SaveSecrets(self, connection, path):
            pass  # Mirrors Noctalia 5.1.0: SaveSecrets accepts but stores nothing.

        @dbus.service.method(interface, in_signature="a{sa{sv}}o", out_signature="")
        def DeleteSecrets(self, connection, path):
            pass

    service = Agent(bus, "/org/freedesktop/NetworkManager/SecretAgent")
    manager = dbus.Interface(bus.get_object("org.freedesktop.NetworkManager",
                            "/org/freedesktop/NetworkManager/AgentManager"),
                            "org.freedesktop.NetworkManager.AgentManager")
    manager.Register("nimbus-dtu-fixture")
    Path("/tmp/agent-ready").touch()
    GLib.MainLoop().run()
    return service


def wait_for(predicate):
    last_error = None
    for _ in range(100):
        try:
            if predicate():
                return
        except Exception as error:
            last_error = error
        time.sleep(0.1)
    raise RuntimeError("fixture service did not become ready") from last_error


def main():
    if not Path("/run/.containerenv").exists() and not Path("/.dockerenv").exists():
        raise SystemExit("This integration fixture requires a disposable container.")
    if "--agent" in sys.argv:
        agent()
        return
    os.makedirs("/run/dbus", exist_ok=True)
    Path("/tmp/test-bus.conf").write_text("""<busconfig>
<type>system</type><listen>unix:path=/run/dbus/system_bus_socket</listen>
<policy context="default"><allow user="*"/><allow own="*"/>
<allow send_destination="*"/><allow receive_sender="*"/></policy>
</busconfig>""")
    subprocess.run(["dbus-daemon", "--config-file=/tmp/test-bus.conf", "--fork", "--nopidfile"], check=True)
    def start_nm():
        with open("/tmp/networkmanager.log", "ab") as log:
            process = subprocess.Popen(["NetworkManager", "--no-daemon"], stdout=log, stderr=log)
        wait_for(lambda: dtu.Network().status())
        return process

    dtu = load()
    nm = start_nm()
    secret_agent = None
    password = "synthetic-test-password"
    try:
        network = dtu.Network()
        legacy_path = network.settings.AddConnection({
            "connection": {"id": "Existing eduroam", "type": "802-11-wireless"},
            "802-11-wireless": {"ssid": network.dbus.ByteArray(b"eduroam")},
            "ipv4": {"method": "auto"}, "ipv6": {"method": "auto"}})
        home_path = network.settings.AddConnection({
            "connection": {"id": "Home", "type": "802-11-wireless"},
            "802-11-wireless": {"ssid": network.dbus.ByteArray(b"home")},
            "ipv4": {"method": "auto"}, "ipv6": {"method": "auto"}})
        existing = set(network.settings.ListConnections())
        dtu.credentials = lambda _: (_ for _ in ()).throw(AssertionError("credentials before approval"))
        try:
            network.configure(network.observe()[1], "")
            raise AssertionError("unapproved replacement allowed")
        except dtu.SetupError:
            pass
        assert existing == set(network.settings.ListConnections())
        for no_op_agent in (False, True):
            if no_op_agent:
                secret_agent = subprocess.Popen([sys.executable, "-I", "-B", __file__, "--agent"])
                wait_for(lambda: Path("/tmp/agent-ready").exists())
            dtu.credentials = lambda _: ("s123456@dtu.dk", password)
            if no_op_agent:
                output = io.StringIO()
                with patch.object(sys.stdin, "isatty", return_value=True), \
                     patch("builtins.input", return_value="y"), \
                     contextlib.redirect_stdout(output):
                    network.configure(network.observe()[1], "", replace_existing=True)
                assert "eduroam was not found nearby" in output.getvalue()
                assert "Connection has not been tested" in output.getvalue()
            else:
                network.configure(network.observe()[1], "", replace_existing=True)
            assert network.status()["configured"], "NM normalized settings were rejected"
            assert not network.status()["connected"], "unexpected connection"
            current, _ = network.observe()
            assert current[1]["connection"].get("autoconnect", True)
            network.verify_password(current[0], password)
            paths = set(network.settings.ListConnections())
            assert existing - {legacy_path} <= paths
            assert legacy_path not in paths and len(paths - (existing - {legacy_path})) == 1
            dtu.credentials = lambda _: (_ for _ in ()).throw(AssertionError("credentials on unapproved rerun"))
            try:
                network.configure(network.observe()[1], "")
                raise AssertionError("rerun replaced without approval")
            except dtu.SetupError:
                pass
            assert paths == set(network.settings.ListConnections())
        secret_agent.terminate()
        secret_agent.wait(timeout=10)
        secret_agent = None
        # A subsequent secret-free preference update must retain the password.
        current, _ = network.observe()
        settings = current[1]
        settings["connection"]["autoconnect"] = True
        network.interface(current[0], dtu.CONNECTION).Update(settings)
        network.verify_password(current[0], password)
        keyfiles = list(Path("/etc/NetworkManager/system-connections").glob("*"))
        saved = [path for path in keyfiles if password.encode() in path.read_bytes()]
        assert len(saved) == 1, "password not persisted in exactly one native keyfile"
        keyfile = saved[0]
        assert keyfile.stat().st_uid == 0 and keyfile.stat().st_gid == 0
        assert keyfile.stat().st_mode & 0o777 == 0o600, "native keyfile is not root-only"
        nm.terminate()
        nm.wait(timeout=10)
        nm = start_nm()
        network = dtu.Network()
        current, _ = network.observe()
        assert network.status()["configured"]
        assert current[1]["connection"].get("autoconnect", True)
        network.verify_password(current[0], password)
        network.interface(current[0], dtu.CONNECTION).Delete()
        assert not keyfile.exists(), "native deletion retained the credential file"
        assert len(list(Path("/etc/NetworkManager/system-connections").glob("*"))) == 1
        print("PASS: saved credentials with absent/non-saving agents, off-campus setup succeeds with autoconnect, preserved unrelated profiles, root:root 0600 keyfile, password survives Update and daemon restart, native removal deletes password file")
    finally:
        if secret_agent is not None:
            secret_agent.terminate()
            secret_agent.wait(timeout=10)
        nm.terminate()
        nm.wait(timeout=10)


if __name__ == "__main__":
    main()
