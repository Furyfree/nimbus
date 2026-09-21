"""Verify production-generated DNF commands using signed, synthetic RPMs."""

import os
import shutil
import subprocess
import tempfile
from pathlib import Path


def run(*args, success=True):
    result = subprocess.run(args, text=True, capture_output=True, check=False)
    if success and result.returncode:
        raise AssertionError(f"{args}: {result.stdout}\n{result.stderr}")
    return result


def verify(root, payload):
    os.environ["GNUPGHOME"] = str(root / "gnupg")
    Path(os.environ["GNUPGHOME"]).mkdir(mode=0o700)
    run(
        "gpg",
        "--batch",
        "--passphrase",
        "",
        "--quick-generate-key",
        "Nimbus fixture <test@example.invalid>",
        "rsa2048",
        "sign",
        "1d",
    )
    key = next(
        line.split(":")[9]
        for line in run("gpg", "--with-colons", "--list-keys").stdout.splitlines()
        if line.startswith("fpr:")
    )
    keyfile = root / "key.asc"
    keyfile.write_text(run("gpg", "--armor", "--export", key).stdout)
    run("rpmkeys", "--import", str(keyfile))
    for path in Path("/etc/yum.repos.d").glob("*.repo"):
        path.unlink()
    for repo in ("a", "b", "disabled"):
        (root / repo).mkdir()
        Path(f"/etc/yum.repos.d/{repo}.repo").write_text(
            f"[nimbus-{repo}]\nname=Fixture {repo}\n"
            f"baseurl=file://{root}/{repo}\nenabled={int(repo != 'disabled')}\n"
            f"gpgcheck=1\ngpgkey=file://{keyfile}\n"
            f"priority={1 if repo == 'b' else 99}\n"
        )

    def build(name, version, repo, requires=""):
        spec = root / "fixture.spec"
        spec.write_text(
            f"Name: {name}\nVersion: {version}\nRelease: 1\n"
            f"Summary: Source fixture\nLicense: MIT\nBuildArch: noarch\n"
            f"{requires}\n%description\nFixture only.\n%files\n"
        )
        run("rpmbuild", "--define", f"_topdir {root}/build", "-bb", str(spec))
        rpm = root / f"build/RPMS/noarch/{name}-{version}-1.noarch.rpm"
        run(
            "rpmsign",
            "--define",
            "_openpgp_sign gpg",
            "--define",
            f"_openpgp_sign_id {key}",
            "--addsign",
            str(rpm),
        )
        shutil.copy2(rpm, root / repo)

    def refresh():
        for repo in ("a", "b", "disabled"):
            run("createrepo_c", str(root / repo))
        run("dnf5", "--refresh", "makecache")

    def observed(name, version, source):
        actual = run(
            "dnf5",
            "repoquery",
            "--installed",
            name,
            "--queryformat",
            "%{version}|%{from_repo}",
        ).stdout.strip()
        assert actual == f"{version}|nimbus-{source}", actual

    build("nimbus-source-dependency", "1", "b")
    build("nimbus-source-probe", "1", "a", "Requires: nimbus-source-dependency")
    build("nimbus-source-probe", "3", "b")
    build("nimbus-source-probe", "99", "disabled")
    build("nimbus-source-editor", "1", "a")
    build("nimbus-source-editor", "2", "b")
    refresh()
    run("dnf5", "-y", *payload["install"])
    observed("nimbus-source-probe", "1", "a")
    observed("nimbus-source-editor", "2", "b")
    observed("nimbus-source-dependency", "1", "b")
    print(
        "PASS mixed source install, stronger competing priority, cross-source dependency"
    )

    build("nimbus-source-probe", "2", "a", "Requires: nimbus-source-dependency")
    build("nimbus-source-editor", "3", "b")
    refresh()
    run("dnf5", "-y", "--setopt=cacheonly=metadata", *payload["upgrade"])
    observed("nimbus-source-probe", "2", "a")
    observed("nimbus-source-editor", "3", "b")
    print(
        "PASS constrained upgrade, orphan installed package, disabled source unchanged"
    )

    run(
        "dnf5",
        "-y",
        "do",
        "--action=upgrade",
        "--from-repo=nimbus-b",
        "nimbus-source-probe",
    )
    observed("nimbus-source-probe", "3", "b")
    run("dnf5", "-y", *payload["repair"])
    observed("nimbus-source-probe", "2", "a")
    print("PASS wrong-source downgrade correction")

    build("nimbus-source-probe", "2", "b")
    refresh()
    run("dnf5", "-y", "reinstall", "--from-repo=nimbus-b", "nimbus-source-probe")
    observed("nimbus-source-probe", "2", "b")
    run("dnf5", "-y", *payload["reinstall"])
    observed("nimbus-source-probe", "2", "a")
    print("PASS same-version source correction")

    run("dnf5", "-y", "remove", "--no-autoremove", "nimbus-source-probe")
    for path in (root / "a").glob("nimbus-source-probe-*.rpm"):
        path.unlink()
    refresh()
    failed = run("dnf5", "-y", *payload["install"], success=False)
    assert failed.returncode != 0, failed.stdout
    assert run("rpm", "-q", "nimbus-source-probe", success=False).returncode != 0
    print("PASS unavailable declared source refuses competing package")


if not Path("/.dockerenv").exists():
    raise SystemExit("This fixture may run only inside its disposable container")
with tempfile.TemporaryDirectory(prefix="nimbus-sources-") as directory:
    # The Go native-test harness supplies command vectors through exec.
    verify(Path(directory), globals()["payload"])
