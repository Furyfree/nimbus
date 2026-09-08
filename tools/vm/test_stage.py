#!/usr/bin/env python3
"""Candidate staging regressions; no SSH, host mutation, or engine execution."""

import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("stage", Path(__file__).with_name("stage.py"))
stage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stage)


class StageTests(unittest.TestCase):
    def test_snapshot_keeps_dirty_source_and_identity_without_private_git_state(self):
        with tempfile.TemporaryDirectory() as work:
            source = Path(work) / "source"
            source.mkdir()
            stage.run("git", "-c", "init.templateDir=", "init", "--quiet", str(source))
            stage.run("git", "remote", "add", "origin",
                      "https://github.com/Furyfree/nimbus.git", cwd=source)
            (source / "nimbus.toml").write_text("old\n")
            (source / ".gitignore").write_text("*.secret\n")
            stage.run("git", "add", ".", cwd=source)
            stage.run("git", "-c", "user.name=Fixture", "-c",
                      "user.email=fixture@example.invalid", "commit", "--quiet",
                      "-m", "fixture", cwd=source)
            (source / "nimbus.toml").write_text("uncommitted\n")
            (source / "internal").mkdir()
            (source / "internal/new.go").write_text("package example\n")
            (source / "internal/token.secret").write_text("private\n")
            (source / "private.txt").write_text("private\n")
            (source / ".git/private").write_text("private\n")
            dest = Path(work) / "candidate"
            head = stage.snapshot(source, dest)
            self.assertEqual(stage.run("git", "rev-parse", "HEAD", cwd=dest), head)
            self.assertEqual((dest / "nimbus.toml").read_text(), "uncommitted\n")
            self.assertTrue((dest / "internal/new.go").exists())
            self.assertIn("M nimbus.toml", stage.run("git", "status", "--short", cwd=dest))
            for rel in (".git/private", "private.txt", "internal/token.secret"):
                self.assertFalse((dest / rel).exists())
            self.assertEqual((source / ".git/private").read_text(), "private\n")

    def archive(self, extra=None, wrong_hash=False):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz") as archive:
            files = {
                "nimbus": b"candidate binary",
                "checkout/nimbus.toml": b"schema = 1\n",
                "candidate.json": json.dumps({"binary_sha256": "bad" if wrong_hash else
                    hashlib.sha256(b"candidate binary").hexdigest()}).encode(),
            }
            for name, data in files.items():
                info = tarfile.TarInfo(name)
                info.size, info.mode = len(data), 0o700
                archive.addfile(info, io.BytesIO(data))
            if extra:
                archive.addfile(extra)
        output.seek(0)
        return output

    def test_receive_uses_fresh_directory_and_preserves_stable_paths(self):
        with tempfile.TemporaryDirectory() as work:
            home = Path(work)
            stable = home / ".local/share/nimbus"
            stable.mkdir(parents=True)
            (stable / "keep").write_text("stable")
            first = stage.receive(self.archive(), home)
            second = stage.receive(self.archive(), home)
            self.assertNotEqual(first, second)
            self.assertEqual(first.stat().st_mode & 0o777, 0o700)
            self.assertEqual((stable / "keep").read_text(), "stable")

    def test_rejects_escape_links_duplicates_and_corruption(self):
        for name, kind in [("../outside", tarfile.REGTYPE),
                           ("/tmp/outside", tarfile.REGTYPE),
                           ("checkout/link", tarfile.SYMTYPE),
                           ("checkout/hardlink", tarfile.LNKTYPE),
                           ("nimbus", tarfile.REGTYPE)]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as work:
                info = tarfile.TarInfo(name)
                info.type, info.linkname = kind, "/tmp/outside"
                with self.assertRaises(ValueError):
                    stage.receive(self.archive(info), Path(work))
                self.assertEqual(list(Path(work).iterdir()), [])
        with tempfile.TemporaryDirectory() as work:
            with self.assertRaises(ValueError):
                stage.receive(self.archive(wrong_hash=True), Path(work))
            self.assertEqual(list(Path(work).iterdir()), [])

    def test_receiver_runs_as_ssh_inline_python_without_file_global(self):
        with tempfile.TemporaryDirectory() as work:
            result = subprocess.run(
                ["python3", "-I", "-B", "-c", Path(stage.__file__).read_text(), "--receive"],
                input=self.archive().read(), env={"HOME": work, "PATH": "/usr/bin:/bin"},
                capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(b"No candidate command has been executed", result.stdout)


if __name__ == "__main__":
    unittest.main()
