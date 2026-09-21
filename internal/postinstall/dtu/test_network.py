"""Isolated native-API and credential boundary tests; no host DBus or accounts."""

import contextlib
import copy
import importlib.util
import io
import json
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "dtu_network", Path(__file__).with_name("network.py")
)
dtu = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dtu)
ITEM = "a" * 26
SECRET = "synthetic-secret-no-output"
REAL_CREDENTIALS = dtu.credentials


class DBusTypes:
    Dictionary = staticmethod(lambda value, **kwargs: value)
    Array = staticmethod(lambda value, **kwargs: value)
    ByteArray = bytes
    Boolean = bool
    UInt32 = int


class Connection:
    def __init__(self, repo, settings):
        self.repo = repo
        self.saved = copy.deepcopy(settings)
        self.secret = self.saved.get("802-1x", {}).pop("password", None)

    def GetSettings(self):
        return copy.deepcopy(self.saved)

    def GetSecrets(self, setting):
        return {setting: {"password": self.secret}}

    def Delete(self):
        self.repo.writes += 1
        for path, connection in list(self.repo.connections.items()):
            if connection is self:
                del self.repo.connections[path]

    def Update(self, settings):
        self.repo.writes += 1
        self.saved = copy.deepcopy(settings)
        self.secret = self.saved.get("802-1x", {}).pop("password", self.secret)


class Settings:
    def __init__(self):
        self.connections = {}
        self.writes = 0

    def ListConnections(self):
        return list(self.connections)

    def AddConnection(self, settings):
        self.writes += 1
        path = "/settings/" + str(self.writes)
        self.connections[path] = Connection(self, settings)
        return path


def fixture():
    network = dtu.Network.__new__(dtu.Network)
    network.user = "testuser"
    network.uuid = "fixture-uuid"
    network.dbus = DBusTypes
    network.settings = Settings()
    network.interface = lambda path, _: network.settings.connections[path]
    network.active = lambda: False
    return network


class Credentials(unittest.TestCase):
    def test_identity_normalization_and_realm_refusal(self):
        for source in ("s123456", "s123456@dtu.dk", "  s123456  "):
            self.assertEqual(dtu.identity(source), "s123456@dtu.dk")
        for source in ("", "@dtu.dk", "x@example.org", "x@dtu.dk@dtu.dk", "x\nfoo"):
            with self.subTest(source=source), self.assertRaises(dtu.SetupError):
                dtu.identity(source)

    def test_private_op_fields_and_validation(self):
        good = [
            {"label": "username", "value": "s123456"},
            {"label": "password", "value": SECRET},
        ]
        for fields in (
            good,
            good[:1],
            good + good[:1],
            {},
            None,
            [{"label": "username", "value": "x@other.org"}, good[1]],
            [good[0], {"label": "password", "value": ""}],
        ):
            result = subprocess.CompletedProcess(
                [], 0, json.dumps(fields).encode(), b""
            )
            with (
                self.subTest(fields=type(fields)),
                patch.object(dtu.subprocess, "run", return_value=result) as run,
            ):
                if fields == good:
                    self.assertEqual(dtu.onepassword(ITEM), ("s123456@dtu.dk", SECRET))
                else:
                    with self.assertRaises(dtu.SetupError) as error:
                        dtu.onepassword(ITEM)
                    self.assertNotIn(SECRET, str(error.exception))
                self.assertNotIn(SECRET, str(run.call_args))
                # Both output streams must stay private, including native errors.
                self.assertTrue(run.call_args.kwargs["capture_output"])
                self.assertIn("--reveal", run.call_args.args[0])
        with patch.object(dtu.subprocess, "run") as run:
            with self.assertRaises(dtu.SetupError):
                dtu.onepassword("title or vault")
            run.assert_not_called()

    def test_op_failure_and_timeout_do_not_expose_native_output(self):
        result = subprocess.CompletedProcess([], 1, SECRET.encode(), SECRET.encode())
        for options in (
            {"return_value": result},
            {"side_effect": subprocess.TimeoutExpired([SECRET], 1)},
            {"side_effect": FileNotFoundError(SECRET)},
        ):
            with (
                patch.object(dtu.subprocess, "run", **options),
                self.assertRaises(dtu.SetupError) as error,
            ):
                dtu.onepassword(ITEM)
            self.assertNotIn(SECRET, str(error.exception))

    def test_manual_and_cancellation(self):
        with (
            patch.object(dtu.sys.stdin, "isatty", return_value=True),
            patch("builtins.input", side_effect=["m", "s123456"]),
            patch.object(dtu.getpass, "getpass", return_value=SECRET),
        ):
            self.assertEqual(dtu.credentials(""), ("s123456@dtu.dk", SECRET))
        with (
            patch.object(dtu.sys.stdin, "isatty", return_value=False),
            self.assertRaises(dtu.SetupError),
        ):
            dtu.credentials("")


class Profiles(unittest.TestCase):
    def setUp(self):
        self.network = fixture()
        self.output = io.StringIO()
        self.enterContext(contextlib.redirect_stdout(self.output))
        self.enterContext(
            patch.object(dtu, "CA_PATH", str(Path(__file__).with_name("ca.pem")))
        )
        self.creds = self.enterContext(
            patch.object(dtu, "credentials", return_value=("s123456@dtu.dk", SECRET))
        )
        self.enterContext(patch.object(dtu.sys.stdin, "isatty", return_value=False))

    def configure(self, replace=False):
        self.network.configure(self.network.observe()[1], ITEM, replace)

    def test_existing_profiles_require_approval_before_credentials_or_writes(self):
        for ssid in (b"eduroam", b"DTUsecure"):
            self.network = fixture()
            legacy = {
                "connection": {
                    "uuid": "legacy",
                    "id": "custom name",
                    "type": "802-11-wireless",
                },
                "802-11-wireless": {"ssid": ssid},
            }
            self.network.settings.AddConnection(legacy)
            self.assertEqual(
                self.network.status()["existing"],
                [{"uuid": "legacy", "ssid": ssid.decode()}],
            )
            with self.assertRaises(dtu.SetupError):
                self.configure()
            self.creds.assert_not_called()
            self.assertEqual(self.network.settings.writes, 1)
            self.assertEqual(
                next(iter(self.network.settings.connections.values())).saved, legacy
            )

    def test_approved_replacement_preserves_unrelated_profiles(self):
        home = {
            "connection": {"uuid": "home", "type": "802-11-wireless"},
            "802-11-wireless": {"ssid": b"home"},
        }
        self.network.settings.AddConnection(home)
        for ssid in (b"eduroam", b"DTUsecure"):
            self.network.settings.AddConnection(
                {
                    "connection": {"uuid": ssid.decode(), "type": "802-11-wireless"},
                    "802-11-wireless": {"ssid": ssid},
                }
            )
        self.configure(replace=True)
        self.assertTrue(self.network.status()["configured"])
        self.assertEqual(len(self.network.settings.connections), 2)
        self.assertEqual(self.network.settings.connections["/settings/1"].saved, home)
        owned = next(
            c
            for c in self.network.settings.connections.values()
            if c.saved["connection"]["uuid"] == self.network.uuid
        )
        self.assertEqual(owned.secret, SECRET)
        self.assertTrue(owned.saved["connection"]["autoconnect"])
        self.creds.reset_mock()
        writes = self.network.settings.writes
        with self.assertRaises(dtu.SetupError):
            self.configure()
        self.creds.assert_not_called()
        self.assertEqual(self.network.settings.writes, writes)
        self.assertNotIn(SECRET, self.output.getvalue())

    def test_canceled_credentials_preserve_approved_replacement_targets(self):
        self.configure()
        before = self.network.observe()[1]
        writes = self.network.settings.writes
        self.creds.side_effect = dtu.SetupError("canceled")
        with self.assertRaises(dtu.SetupError):
            self.configure(replace=True)
        self.assertEqual(self.network.observe()[1], before)
        self.assertEqual(self.network.settings.writes, writes)

    def test_failed_recreation_reports_partial_deletion_and_can_retry(self):
        self.configure()
        with (
            patch.object(
                self.network.settings, "AddConnection", side_effect=RuntimeError(SECRET)
            ),
            self.assertRaisesRegex(dtu.SetupError, "after deleting") as error,
        ):
            self.configure(replace=True)
        self.assertNotIn(SECRET, str(error.exception))
        self.assertFalse(self.network.settings.connections)
        self.configure()
        self.assertTrue(self.network.status()["configured"])

    def test_inspection_never_fetches_secrets(self):
        self.configure()
        self.creds.reset_mock()
        writes = self.network.settings.writes
        with patch.object(
            Connection, "GetSecrets", side_effect=AssertionError("secret inspection")
        ):
            self.network.status()
            self.network.observe()
        self.creds.assert_not_called()
        self.assertEqual(self.network.settings.writes, writes)

    def test_manual_and_onepassword_credentials_are_saved_and_verified(self):
        # Exercise the real credential selection through the full setup path.
        for method in ("manual", "onepassword"):
            self.network = fixture()
            fields = [
                {"label": "username", "value": "s123456"},
                {"label": "password", "value": SECRET},
            ]
            result = subprocess.CompletedProcess(
                [], 0, json.dumps(fields).encode(), b""
            )
            with (
                patch.object(dtu, "credentials", REAL_CREDENTIALS),
                patch.object(dtu.sys.stdin, "isatty", return_value=True),
                patch(
                    "builtins.input",
                    side_effect=["m", "s123456", "n"] if method == "manual" else ["n"],
                ),
                patch.object(dtu.getpass, "getpass", return_value=SECRET),
                patch.object(dtu.subprocess, "run", return_value=result) as run,
            ):
                self.network.configure(
                    self.network.observe()[1], "" if method == "manual" else ITEM
                )
            connection = next(iter(self.network.settings.connections.values()))
            self.assertEqual(connection.saved["802-1x"]["identity"], "s123456@dtu.dk")
            self.assertEqual(connection.saved["802-1x"]["password-flags"], 0)
            self.assertEqual(connection.secret, SECRET)
            self.assertEqual(run.call_count, 0 if method == "manual" else 1)
            self.assertNotIn(SECRET, self.output.getvalue())

    def test_missing_or_unreadable_saved_password_stops_before_connection(self):
        for options in (
            {"return_value": {}},
            {"return_value": {"802-1x": {"password": "wrong"}}},
            {"side_effect": RuntimeError(SECRET)},
        ):
            self.network = fixture()
            with (
                patch.object(Connection, "GetSecrets", **options),
                patch.object(dtu.subprocess, "run") as run,
                self.assertRaises(dtu.SetupError) as error,
            ):
                self.configure()
            run.assert_not_called()
            self.assertNotIn(SECRET, str(error.exception))
            self.assertEqual(len(self.network.settings.connections), 1)

    def test_unknown_ownership_blocks_before_credentials(self):
        for meta in (
            {"uuid": "foreign", "id": dtu.PROFILE_ID},
            {"uuid": self.network.uuid, "id": dtu.PROFILE_ID},
        ):
            self.network.settings = Settings()
            self.network.settings.AddConnection({"connection": meta})
            with self.assertRaises(dtu.SetupError):
                self.configure()
            self.creds.assert_not_called()
            self.assertEqual(self.network.settings.writes, 1)

    def test_drift_and_canceled_authorization_preserve_existing_state(self):
        with self.assertRaises(dtu.SetupError):
            self.network.configure("0" * 64, ITEM)
        self.creds.assert_not_called()
        self.creds.side_effect = dtu.SetupError("canceled")
        with self.assertRaises(dtu.SetupError):
            self.configure()
        self.assertEqual(self.network.settings.writes, 0)

        def drift(_):
            self.network.settings.AddConnection(
                self.network.desired("other@dtu.dk", SECRET)
            )
            return "s123456@dtu.dk", SECRET

        self.creds.side_effect = drift
        with self.assertRaises(dtu.SetupError):
            self.configure()
        self.assertEqual(self.network.settings.writes, 1)

    def test_security_drift_needs_approved_recreation(self):
        self.configure()
        for key, value in (
            ("domain-match", ""),
            ("ca-cert", b""),
            ("system-ca-certs", True),
            ("phase1-auth-flags", 1),
            ("password-flags", 1),
            ("identity", "foreign@example.org"),
        ):
            connection = next(iter(self.network.settings.connections.values()))
            connection.saved["802-1x"][key] = value
            self.assertFalse(self.network.status()["configured"], key)
            with self.assertRaises(dtu.SetupError):
                self.configure()
            self.configure(replace=True)
            self.assertTrue(self.network.status()["configured"], key)
            self.assertEqual(len(self.network.settings.connections), 1)

    def test_connect_requires_confirmation_but_autoconnect_does_not(self):
        with (
            patch.object(dtu.sys.stdin, "isatty", return_value=True),
            patch("builtins.input", return_value="n"),
            patch.object(dtu.subprocess, "run") as run,
        ):
            self.configure()
            run.assert_not_called()
        for success in (False, True):
            active = [False]
            self.network.active = lambda active=active: active[0]

            def connect(*args, active=active, success=success, **kwargs):
                active[0] = success
                return subprocess.CompletedProcess([], 0 if success else 1)

            with (
                patch.object(dtu.sys.stdin, "isatty", return_value=True),
                patch("builtins.input", return_value="y"),
                patch.object(self.network, "eduroam_visible", return_value=True),
                patch.object(dtu.subprocess, "run", side_effect=connect),
            ):
                if success:
                    self.configure(replace=True)
                else:
                    with self.assertRaises(dtu.SetupError):
                        self.configure(replace=True)
            connection = next(iter(self.network.settings.connections.values()))
            self.assertTrue(connection.saved["connection"]["autoconnect"])

    def test_connection_timeout_retains_profile_and_autoconnect(self):
        with (
            patch.object(dtu.sys.stdin, "isatty", return_value=True),
            patch("builtins.input", return_value="y"),
            patch.object(self.network, "eduroam_visible", return_value=True),
            patch.object(
                dtu.subprocess,
                "run",
                side_effect=subprocess.TimeoutExpired([SECRET], 75),
            ),
            self.assertRaisesRegex(dtu.SetupError, "connection timed out") as error,
        ):
            self.configure()
        connection = next(iter(self.network.settings.connections.values()))
        self.assertEqual(connection.secret, SECRET)
        self.assertTrue(connection.saved["connection"]["autoconnect"])
        self.assertNotIn(SECRET, str(error.exception))

    def test_off_campus_setup_succeeds_without_attempting_activation(self):
        for terminal in (False, True):
            self.network = fixture()
            self.output.seek(0)
            self.output.truncate()
            with (
                patch.object(dtu.sys.stdin, "isatty", return_value=terminal),
                patch("builtins.input", return_value="y"),
                patch.object(
                    dtu.subprocess,
                    "run",
                    return_value=subprocess.CompletedProcess([], 0, b"Home\n", b""),
                ) as run,
            ):
                self.configure()
            connection = next(iter(self.network.settings.connections.values()))
            self.assertTrue(connection.saved["connection"]["autoconnect"])
            self.assertEqual(connection.secret, SECRET)
            self.assertEqual(run.call_count, int(terminal))
            if terminal:
                self.assertIn("eduroam was not found nearby", self.output.getvalue())
                self.assertIn("list", run.call_args.args[0])
            self.assertIn("Connection has not been tested", self.output.getvalue())
            self.assertNotIn("Connected to DTU", self.output.getvalue())

    def test_scan_matches_exact_ssid_and_keeps_native_output_private(self):
        for output, expected in (
            (b"eduroam\n", True),
            (b"Home\neduroam\n", True),
            (b"", False),
            (b"eduroam-guest\n", False),
        ):
            with patch.object(
                dtu.subprocess,
                "run",
                return_value=subprocess.CompletedProcess([], 0, output, b""),
            ):
                self.assertEqual(self.network.eduroam_visible(), expected)
        for options in (
            {"return_value": subprocess.CompletedProcess([], 1, b"", SECRET.encode())},
            {"side_effect": subprocess.TimeoutExpired([SECRET], 25)},
            {"side_effect": FileNotFoundError(SECRET)},
        ):
            with (
                patch.object(dtu.subprocess, "run", **options),
                self.assertRaises(dtu.SetupError) as error,
            ):
                self.network.eduroam_visible()
            self.assertNotIn(SECRET, str(error.exception))
            self.assertIn("could not be checked", str(error.exception))

    def test_success_accepts_native_omission_of_default_autoconnect(self):
        update = Connection.Update

        def native_update(connection, settings):
            update(connection, settings)
            if connection.saved["connection"].get("autoconnect"):
                connection.saved["connection"].pop("autoconnect")

        self.network.active = lambda: True
        with (
            patch.object(Connection, "Update", native_update),
            patch.object(dtu.sys.stdin, "isatty", return_value=True),
            patch("builtins.input", return_value="y"),
            patch.object(
                dtu.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)
            ),
        ):
            self.configure()
        self.assertIn("automatic connection enabled", self.output.getvalue())


if __name__ == "__main__":
    unittest.main()
