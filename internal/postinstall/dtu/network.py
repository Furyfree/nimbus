"""Linux NetworkManager setup adapted from GÉANT CAT's DTU Linux installer.

Copyright GÉANT Association on behalf of the GÉANT projects; see
licenses/GEANT-CAT.txt (https://cat.eduroam.org/copyright.php).
Nimbus modifications: fixed reviewed DTU profile,
private credential input, explicitly approved replacement and offline status.
Source: https://github.com/Furyfree/nimbus/tree/main/internal/postinstall/dtu
Upstream: https://github.com/GEANT/CAT


 * **************************************************************************
 * Contributions to this work were made on behalf of the GÉANT project,
 * a project that has received funding from the European Union’s Framework
 * Programme 7 under Grant Agreements No. 238875 (GN3)
 * and No. 605243 (GN3plus), Horizon 2020 research and innovation programme
 * under Grant Agreements No. 691567 (GN4-1) and No. 731122 (GN4-2).
 * On behalf of the aforementioned projects, GEANT Association is
 * the sole owner of the copyright in all material which was developed
 * by a member of the GÉANT project.
 * GÉANT Vereniging (Association) is registered with the Chamber of
 * Commerce in Amsterdam with registration number 40535155 and operates
 * in the UK as a branch of GÉANT Vereniging.
 *
 * Registered office: Hoekenrode 3, 1102BR Amsterdam, The Netherlands.
 * UK branch address: City House, 126-130 Hills Road, Cambridge CB2 1PQ, UK
 *
 * License: see the web/copyright.inc.php file in the file structure or
 *          <base_url>/copyright.php after deploying the software

Authors:
    Tomasz Wolniewicz <twoln@umk.pl>
    Michał Gasewicz <genn@umk.pl> (Network Manager support)

Contributors:
    Steffen Klemer https://github.com/sklemer1
    ikreb7 https://github.com/ikreb7
    Dimitri Papadopoulos Orfanos https://github.com/DimitriPapadopoulos
    sdasda7777 https://github.com/sdasda7777
    Matt Jolly http://gitlab.com/Matt.Jolly
    Benoit LE TEXIER benoit.le-texier@imt-atlantique.fr
    jmontes https://github.com/m4r4d0n4

Many thanks for multiple code fixes, feature ideas, styling remarks
much of the code provided by them in the form of pull requests
has been incorporated into the final form of this script.

This script is the main body of the CAT Linux installer.
In the generation process configuration settings are added
as well as messages which are getting translated into the language
selected by the user.

The script runs under python3.


"""

import argparse
import getpass
import hashlib
import json
import os
import pwd
import re
import subprocess
import sys
import uuid

CA_PATH = "/etc/NetworkManager/certs/dtu-eduroam.pem"
CA_SHA256 = "4044ec3c69ea71dade30be85294f22d6a3659cfb9c2b90986cebe3623775a7c1"
SERVERS = "ait-pisepsn03.win.dtu.dk;ait-pisepsn04.win.dtu.dk"
SERVICE = "org.freedesktop.NetworkManager"
CONNECTION = SERVICE + ".Settings.Connection"
PROFILE_ID = "Nimbus DTU eduroam"


class SetupError(Exception):
    """Messages safe for the terminal; never include native secret output."""


def identity(value):
    value = value.strip()
    if not re.fullmatch(r"[A-Za-z0-9._-]+(?:@dtu\.dk)?", value):
        raise SetupError("Use your DTU username, optionally ending in @dtu.dk.")
    return value if "@" in value else value + "@dtu.dk"


def onepassword(item):
    if not re.fullmatch(r"[a-z0-9]{26}", item):
        raise SetupError(
            "Supply the 26-character 1Password item UUID, not a vault UUID or item title."
        )
    try:
        result = subprocess.run(
            [
                "op",
                "item",
                "get",
                item,
                "--format=json",
                "--fields=label=username,label=password",
                "--reveal",
            ],
            capture_output=True,
            timeout=120,
            check=False,
        )
    except FileNotFoundError:
        raise SetupError(
            "Install 1Password CLI and enable desktop CLI integration, or choose manual entry."
        ) from None
    except subprocess.TimeoutExpired:
        raise SetupError(
            "1Password authorization timed out; unlock 1Password and retry."
        ) from None
    if result.returncode:
        raise SetupError(
            "1Password could not read the item; check authorization, account and item UUID."
        )
    try:
        fields = json.loads(result.stdout)
        values = {}
        for field in fields:
            label = field.get("label")
            if label in ("username", "password"):
                if label in values:
                    raise ValueError("duplicate")
                values[label] = field.get("value")
        username, password = values["username"], values["password"]
        if (
            not isinstance(username, str)
            or not isinstance(password, str)
            or not password
        ):
            raise ValueError("empty")
    except (ValueError, TypeError, KeyError, AttributeError):
        raise SetupError(
            "The 1Password item must have nonempty username and password fields."
        ) from None
    return identity(username), password


def credentials(item):
    if item:
        return onepassword(item)
    if not sys.stdin.isatty():
        raise SetupError(
            "Manual credential selection requires a terminal; use --onepassword-item for 1Password."
        )
    method = input("Credentials: 1Password or manual [1/m]: ").strip().lower()
    if method in ("", "1", "1password"):
        return onepassword(input("1Password login item UUID: ").strip())
    if method not in ("m", "manual"):
        raise SetupError("Credential selection canceled; no connection was changed.")
    username = identity(input("DTU username (for example s123456): "))
    password = getpass.getpass("DTU password: ")
    if not password:
        raise SetupError("The DTU password cannot be empty.")
    return username, password


def plain(value):
    if isinstance(value, dict):
        return {str(k): plain(v) for k, v in value.items()}
    if isinstance(value, (list, tuple, bytes, bytearray)):
        return [plain(v) for v in value]
    if isinstance(value, (str, int, float, bool)):
        return value
    raise SetupError("NetworkManager returned an unsupported configuration value.")


def digest(value):
    return hashlib.sha256(
        json.dumps(plain(value), sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()


class Network:
    def __init__(self):
        import dbus

        self.dbus = dbus
        self.user = pwd.getpwuid(os.getuid()).pw_name
        self.uuid = str(uuid.uuid5(uuid.NAMESPACE_URL, "nimbus:dtu:" + self.user))
        self.bus = dbus.SystemBus()
        self.settings = self.interface(
            "/org/freedesktop/NetworkManager/Settings", SERVICE + ".Settings"
        )

    def interface(self, path, interface):
        return self.dbus.Interface(self.bus.get_object(SERVICE, path), interface)

    def observe(self):
        found = None
        existing = []
        snapshot = []
        for path in self.settings.ListConnections():
            connection = self.interface(path, CONNECTION)
            # Inspection deliberately excludes secrets; GetSecrets is setup-only.
            settings = connection.GetSettings()
            meta = settings.get("connection", {})
            ssid = bytes(settings.get("802-11-wireless", {}).get("ssid", []))
            if meta.get("type") == "802-11-wireless" and ssid in (
                b"eduroam",
                b"DTUsecure",
            ):
                if not meta.get("uuid"):
                    raise SetupError(
                        "A matching Wi-Fi profile has no UUID; review it manually."
                    )
                existing.append((str(path), str(meta["uuid"]), ssid.decode()))
                public = plain(settings)
                public["connection"].pop("timestamp", None)
                snapshot.append(public)
            if meta.get("uuid") != self.uuid:
                if meta.get("id") == PROFILE_ID:
                    raise SetupError(
                        "Another connection uses the Nimbus DTU name; review it manually before setup."
                    )
                continue
            if found is not None:
                raise SetupError(
                    "Multiple Nimbus DTU profiles found; review them manually."
                )
            permissions = list(meta.get("permissions", []))
            marker = settings.get("user", {}).get("data", {}).get("nimbus.dtu.owner")
            if (
                meta.get("id") != PROFILE_ID
                or marker != self.user
                or permissions != ["user:" + self.user + ":"]
            ):
                raise SetupError(
                    "The expected DTU profile has unfamiliar ownership; it will not be overwritten."
                )
            found = (str(path), settings)
        self.snapshots = {item["connection"]["uuid"]: item for item in snapshot}
        self.existing = sorted(existing, key=lambda item: item[1])
        snapshot.sort(key=lambda item: item["connection"]["uuid"])
        return found, digest(snapshot)

    def valid(self, settings):
        eap = settings.get("802-1x", {})
        wifi = settings.get("802-11-wireless", {})
        security = settings.get("802-11-wireless-security", {})
        try:
            correct_identity = identity(str(eap.get("identity", ""))) == eap.get(
                "identity"
            )
        except SetupError:
            correct_identity = False
        return (
            settings.get("connection", {}).get("type") == "802-11-wireless"
            and wifi.get("mode") == "infrastructure"
            and correct_identity
            and bytes(wifi.get("ssid", [])) == b"eduroam"
            and security.get("key-mgmt") == "wpa-eap"
            and list(security.get("proto", [])) == ["rsn"]
            and list(security.get("pairwise", [])) == ["ccmp"]
            and list(security.get("group", [])) == ["ccmp"]
            and list(eap.get("eap", [])) == ["peap"]
            and eap.get("phase2-auth") == "mschapv2"
            and not eap.get("phase2-autheap", "")
            and not eap.get("phase1-peapver", "")
            and not eap.get("phase1-peaplabel", "")
            and eap.get("domain-match") == SERVERS
            and eap.get("anonymous-identity") == "anonymous@dtu.dk"
            and bytes(eap.get("ca-cert", [])) == ("file://" + CA_PATH + "\0").encode()
            and not eap.get("system-ca-certs", False)
            and not eap.get("ca-path", "")
            and int(eap.get("phase1-auth-flags", 0)) == 0
            and int(eap.get("password-flags", 0)) == 0
        )

    def active(self):
        props = self.interface(
            "/org/freedesktop/NetworkManager", "org.freedesktop.DBus.Properties"
        )
        for path in props.Get(SERVICE, "ActiveConnections"):
            active = self.interface(path, "org.freedesktop.DBus.Properties")
            values = active.GetAll(SERVICE + ".Connection.Active")
            if values.get("Uuid") == self.uuid and int(values.get("State", 0)) == 2:
                return True
        return False

    def status(self):
        found, observed = self.observe()
        return {
            "observed": observed,
            "configured": bool(found and self.valid(found[1])),
            "connected": self.active() if found else False,
            "existing": [{"uuid": uid, "ssid": ssid} for _, uid, ssid in self.existing],
        }

    def desired(self, username, password):
        dbus = self.dbus
        # NetworkManager stores the password in its root-only native keyfile.
        # Noctalia's secret agent prompts but does not implement saved secrets.
        return dbus.Dictionary(
            {
                "connection": dbus.Dictionary(
                    {
                        "type": "802-11-wireless",
                        "uuid": self.uuid,
                        "id": PROFILE_ID,
                        "permissions": dbus.Array(
                            ["user:" + self.user + ":"], signature="s"
                        ),
                        "autoconnect": dbus.Boolean(False),
                    },
                    signature="sv",
                ),
                "user": dbus.Dictionary(
                    {
                        "data": dbus.Dictionary(
                            {"nimbus.dtu.owner": self.user}, signature="ss"
                        )
                    },
                    signature="sv",
                ),
                "802-11-wireless": dbus.Dictionary(
                    {
                        "ssid": dbus.ByteArray(b"eduroam"),
                        "mode": "infrastructure",
                        "security": "802-11-wireless-security",
                    },
                    signature="sv",
                ),
                "802-11-wireless-security": dbus.Dictionary(
                    {
                        "key-mgmt": "wpa-eap",
                        "proto": dbus.Array(["rsn"], signature="s"),
                        "pairwise": dbus.Array(["ccmp"], signature="s"),
                        "group": dbus.Array(["ccmp"], signature="s"),
                    },
                    signature="sv",
                ),
                "802-1x": dbus.Dictionary(
                    {
                        "eap": dbus.Array(["peap"], signature="s"),
                        "identity": username,
                        "ca-cert": dbus.ByteArray(
                            ("file://" + CA_PATH + "\0").encode()
                        ),
                        "domain-match": SERVERS,
                        "phase2-auth": "mschapv2",
                        "anonymous-identity": "anonymous@dtu.dk",
                        "password": password,
                        "password-flags": dbus.UInt32(0),
                    },
                    signature="sv",
                ),
                "ipv4": dbus.Dictionary({"method": "auto"}, signature="sv"),
                "ipv6": dbus.Dictionary({"method": "auto"}, signature="sv"),
            },
            signature="sa{sv}",
        )

    def configure(self, expected, item, replace_existing=False):
        found, observed = self.observe()
        if observed != expected:
            raise SetupError(
                "DTU connection changed after approval; inspect and retry."
            )
        if self.existing and not replace_existing:
            raise SetupError(
                "Existing eduroam/DTUsecure profiles were kept; explicit replacement approval is required."
            )
        with open(CA_PATH, "rb") as f:
            if hashlib.sha256(f.read(65537)).hexdigest() != CA_SHA256:
                raise SetupError(
                    "The reviewed DTU CA bundle is missing or changed; rerun certificate preparation."
                )
        username, password = credentials(item)
        # Authorization can take time. Reinspect the entire replacement set.
        _, observed = self.observe()
        if observed != expected:
            raise SetupError(
                "DTU connection changed during authorization; inspect and retry."
            )
        settings = self.desired(username, password)
        targets = list(self.existing)
        approved = dict(self.snapshots)
        removed = False
        try:
            # Each deletion is bound to the exact UUID/settings shown at approval.
            # Recheck after each operation; never expand the approved target set.
            for path, uid, _ in targets:
                self.observe()
                if self.snapshots != approved:
                    raise SetupError(
                        "DTU profiles changed during replacement; inspect and retry."
                    )
                self.interface(path, CONNECTION).Delete()
                removed = True
                approved.pop(uid)
                self.observe()
                if any(current_uid == uid for _, current_uid, _ in self.existing):
                    raise SetupError(
                        "Profile deletion could not be verified; inspect and retry."
                    )
            if self.existing:
                raise SetupError(
                    "Another DTU profile appeared; setup stopped before creating a duplicate."
                )
            path = self.settings.AddConnection(settings)
        except Exception:
            if removed:
                raise SetupError(
                    "Replacement stopped after deleting an approved profile. Old credentials cannot be restored by Nimbus; rerun setup or restore it through NetworkManager."
                ) from None
            raise SetupError(
                "NetworkManager could not replace/create the profile; inspect its permissions and retry."
            ) from None
        else:
            self.verify_password(path, password)
        finally:
            del password, settings
        if not self.status()["configured"]:
            raise SetupError(
                "Profile was written but configuration verification failed; inspect and retry."
            )
        # Enable autoconnect only after profile/password verification, including
        # off campus. Approval explains that NM may connect as soon as available.
        current, _ = self.observe()
        if not current or not self.valid(current[1]):
            raise SetupError(
                "Connection settings changed; automatic connection was not enabled."
            )
        settings = current[1]
        settings["connection"]["autoconnect"] = self.dbus.Boolean(True)
        self.interface(current[0], CONNECTION).Update(settings)
        current, _ = self.observe()
        if not current or not current[1]["connection"].get("autoconnect", True):
            raise SetupError(
                "Profile saved, but automatic connection could not be verified; check the network menu."
            )
        print(
            "DTU eduroam profile and saved password verified; automatic connection enabled."
        )
        print("NetworkManager owns password storage in its root-only connection file.")
        print(
            "Only explicitly approved eduroam/DTUsecure profiles were replaced; other Wi-Fi profiles were preserved."
        )
        if not sys.stdin.isatty():
            self.connection_deferred()
            return
        choice = input("Connect now? This may switch Wi-Fi [y/N]: ").strip().lower()
        if choice not in ("y", "yes"):
            self.connection_deferred()
            return
        if self.active():
            print(
                "Already connected to DTU eduroam; automatic connection enabled. Check Internet access and DTU services."
            )
            return
        if not self.eduroam_visible():
            print(
                "eduroam was not found nearby. Setup is complete; your profile and password are saved."
            )
            self.connection_deferred()
            return
        # Keep native errors private: DBus/nmcli errors can contain account data.
        try:
            result = subprocess.run(
                ["nmcli", "--wait", "60", "connection", "up", "uuid", self.uuid],
                capture_output=True,
                timeout=75,
                check=False,
            )
        except subprocess.TimeoutExpired:
            raise SetupError(
                "Profile and password saved, but connection timed out. Retry this profile in the network menu; no recreation is needed."
            ) from None
        if result.returncode or not self.active():
            raise SetupError(
                "Profile and password saved, but connection failed. Check Wi-Fi range and DTU credentials; retry this profile in the network menu. No recreation is needed."
            )
        print(
            "Connected to DTU eduroam; automatic connection enabled. Check Internet access and DTU services."
        )

    @staticmethod
    def connection_deferred():
        print(
            "NetworkManager will try to connect automatically when eduroam is available and Wi-Fi is enabled. No need to rerun setup."
        )
        print(
            "Connection has not been tested by this setup run; verify it when you are in range."
        )

    @staticmethod
    def eduroam_visible():
        # Only scan after explicit connection approval. A failed scan is unknown,
        # not evidence of absence. Never display other SSIDs or native errors.
        try:
            result = subprocess.run(
                [
                    "nmcli",
                    "--colors",
                    "no",
                    "--terse",
                    "--fields",
                    "SSID",
                    "device",
                    "wifi",
                    "list",
                    "--rescan",
                    "yes",
                ],
                capture_output=True,
                timeout=25,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            raise SetupError(
                "Profile saved, but available Wi-Fi networks could not be checked. Check Wi-Fi and NetworkManager, then retry in the network menu."
            ) from None
        if result.returncode:
            raise SetupError(
                "Profile saved, but available Wi-Fi networks could not be checked. Check Wi-Fi and NetworkManager, then retry in the network menu."
            )
        return b"eduroam" in result.stdout.splitlines()

    def verify_password(self, path, expected):
        # Only called for the profile just created in approved setup. Never
        # fetch passwords during status/planning or include native errors.
        try:
            secrets = self.interface(path, CONNECTION).GetSecrets("802-1x")
            matched = secrets.get("802-1x", {}).get("password") == expected
            del secrets
        except Exception:
            raise SetupError(
                "Profile created, but saved password verification failed. Inspect NetworkManager permissions and rerun setup."
            ) from None
        if not matched:
            raise SetupError(
                "Profile created, but NetworkManager did not retain the supplied password. Rerun setup; connection was not attempted."
            )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("inspect", "configure"))
    parser.add_argument("--expected")
    parser.add_argument(
        "--replace-existing",
        action="store_true",
        help="internal: CLI explicitly approved the observed replacement set",
    )
    parser.add_argument("--onepassword-item", default="")
    args = parser.parse_args()
    if sys.platform != "linux" or os.getuid() == 0:
        raise SetupError(
            "DTU setup requires Linux and your normal desktop user, not root."
        )
    network = Network()
    if args.action == "inspect":
        print(json.dumps(network.status(), sort_keys=True))
    else:
        if not args.expected or not re.fullmatch(r"[a-f0-9]{64}", args.expected):
            raise SetupError("Missing approved profile observation.")
        network.configure(args.expected, args.onepassword_item, args.replace_existing)


if __name__ == "__main__":
    try:
        main()
    except (KeyboardInterrupt, EOFError):
        print("DTU setup canceled; rerun to inspect partial setup.", file=sys.stderr)
        sys.exit(1)
    except SetupError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        # Native DBus and subprocess errors may contain credentials. Never
        # emit their text or a traceback into Nimbus diagnostics.
        print(
            "DTU setup failed. Check NetworkManager permissions and Python DBus support, then retry.",
            file=sys.stderr,
        )
        sys.exit(1)
