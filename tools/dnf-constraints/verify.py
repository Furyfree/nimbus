"""Exercise generated Nimbus policy against synthetic RPMs, inside Docker."""

from pathlib import Path
import os
import shutil
import subprocess
import tempfile


def run(*args):
    result = subprocess.run(args, text=True, capture_output=True, check=False)
    if result.returncode:
        raise AssertionError(f"{args}: {result.stdout}\n{result.stderr}")
    return result.stdout


def verify(root):
    repo = root / "repo"
    repo.mkdir()
    versions = {
        "hyprland": ["0.55.3", "0.56.0", "0.56.1", "0.56.2~rc1",
                     "0.56.3rc1", "0.56.4^git1", "0.57~rc1", "0.57.0"],
        "noctalia": ["4.9.0", "5.0.0", "5.8.1", "5.9.0~rc1", "6.0.0"],
        "hyprland-devel": ["0.56.1"],
        "hyprland-plugin": ["1"],
    }

    def build(name, version, release="1.fc44", epoch=0):
        spec = root / "fixture.spec"
        requires = "Requires: hyprland >= 0.57\n" if name == "hyprland-plugin" else ""
        spec.write_text(
            f"Name: {name}\nEpoch: {epoch}\nVersion: {version}\n"
            f"Release: {release}\nSummary: Native constraint fixture\n"
            f"License: MIT\nBuildArch: noarch\n{requires}"
            "%description\nFixture only.\n%files\n"
        )
        run("rpmbuild", "-bb", str(spec), "--define", f"_topdir {root}/build",
            "--define", f"_rpmdir {repo}")

    for name, releases in versions.items():
        for version in releases:
            build(name, version)
    build("hyprland", "0.56.1", "2.fc44")
    build("hyprland", "0.56.9", epoch=1)
    run("createrepo_c", str(repo))

    host = root / "host"
    dnf = host / "etc/dnf"
    (dnf / "libdnf5.conf.d").mkdir(parents=True)
    lock = dnf / "versionlock.toml"
    config = dnf / "libdnf5.conf.d/20-nimbus.conf"
    shutil.copyfile("/policy/versionlock.toml", lock)
    shutil.copyfile("/policy/20-nimbus.conf", config)
    # Trust is disabled only for our unsigned, locally built empty test RPMs.
    args = ["dnf5", "--installroot", str(host), "--releasever=44",
            "--disable-repo=*", "--repofrompath", f"fixture,file://{repo}",
            "--setopt=fixture.gpgcheck=0", "--assumeno"]

    def preview(*command):
        result = subprocess.run([*args, *command], text=True, capture_output=True,
                                env={**os.environ, "LC_ALL": "C"}, check=False)
        return result.stdout + result.stderr

    def contains(output, *expected):
        for text in expected:
            assert text in output, f"Missing {text!r}:\n{output}"

    contains(preview("install", "hyprland", "noctalia", "hyprland-devel"),
             "0:0.56.1-2.fc44", "0:5.8.1-1.fc44", "hyprland-devel",
             "Installing:         3 packages")
    for version in ["0.55.3", "0.56.2~rc1", "0.56.3rc1", "0.56.4^git1",
                    "0.57~rc1", "0.57.0", "0.56.9"]:
        contains(preview("install", f"hyprland-{version}-1.fc44"),
                 "Failed to resolve the transaction", "excluded")
    for version in ["4.9.0", "5.9.0~rc1", "6.0.0"]:
        contains(preview("install", f"noctalia-{version}-1.fc44"),
                 "Failed to resolve the transaction", "excluded")
    contains(preview("install", "hyprland-plugin"),
             "Failed to resolve the transaction", "hyprland >= 0.57")

    # Populate only the isolated RPM database; there are no payloads or scripts.
    run("rpm", "--root", str(host), "--initdb")
    run("rpm", "--root", str(host), "--justdb", "--nodeps", "-i",
        str(repo / "noarch/hyprland-0.56.0-1.fc44.noarch.rpm"),
        str(repo / "noarch/noctalia-5.0.0-1.fc44.noarch.rpm"))
    contains(preview("upgrade"), "Upgrading:", "0:0.56.1-2.fc44", "0:5.8.1-1.fc44")
    run("rpm", "--root", str(host), "--justdb", "--nodeps", "-U",
        str(repo / "noarch/hyprland-0.56.1-1.fc44.noarch.rpm"))
    contains(preview("upgrade"), "0:0.56.1-2.fc44")
    run("rpm", "--root", str(host), "--justdb", "--nodeps", "-U",
        str(repo / "noarch/hyprland-0.57.0-1.fc44.noarch.rpm"))
    output = preview("upgrade", "hyprland")
    contains(output, "Nothing to do.")
    assert "Downgrading:" not in output, output

    lock.unlink()
    config.unlink()
    contains(preview("install", "noctalia-6.0.0-1.fc44"), "0:6.0.0-1.fc44")
    print("PASS: families, prereleases, epoch, subpackages, dependency conflict, "
          "patch and packaging upgrades, no implicit downgrade, policy removal")


if __name__ == "__main__":
    with tempfile.TemporaryDirectory(prefix="nimbus-dnf-") as temporary:
        verify(Path(temporary))
