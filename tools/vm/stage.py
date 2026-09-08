#!/usr/bin/env python3
"""Build and stage a private, unpublished candidate without replacing Nimbus."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile


def run(*args, cwd=None, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def snapshot(source, target):
    """Copy reviewed source paths, preserving HEAD and uncommitted definitions."""
    source = source.resolve(strict=True)
    if Path(run("git", "rev-parse", "--show-toplevel", cwd=source)) != source:
        raise ValueError("source must be a Git checkout root")
    origin = run("git", "remote", "get-url", "origin", cwd=source)
    if origin.lower().removesuffix(".git").rstrip("/") not in (
        "https://github.com/furyfree/nimbus", "git@github.com:furyfree/nimbus"
    ):
        raise ValueError("source origin must be the Nimbus repository")
    head = run("git", "rev-parse", "HEAD", cwd=source)
    target.mkdir(mode=0o700)
    # A fresh shallow clone carries identity, not local hooks, credentials,
    # reflogs, remote state, or links back into the developer's checkout.
    run("git", "-c", "init.templateDir=", "init", "--quiet", str(target))
    run("git", "-c", "protocol.file.allow=always", "fetch", "--quiet",
        "--depth=1", "--no-tags", source.as_uri(), head, cwd=target)
    run("git", "update-ref", "refs/heads/candidate", head, cwd=target)
    run("git", "symbolic-ref", "HEAD", "refs/heads/candidate", cwd=target)
    run("git", "read-tree", "HEAD", cwd=target)
    run("git", "remote", "add", "origin",
        "https://github.com/Furyfree/nimbus.git", cwd=target)
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached"], cwd=source
    ).split(b"\0")
    # Include new implementation and definition files, but never arbitrary
    # untracked files, build output, or ignored runtime state at the root.
    paths += subprocess.check_output(
        ["git", "ls-files", "-z", "--others", "--exclude-standard", "--",
         "nimbus.toml", "machines", "profiles", "components", "system",
         "cmd", "internal", "tools", "docs"], cwd=source
    ).split(b"\0")
    for raw in sorted(set(paths) - {b""}):
        rel = Path(os.fsdecode(raw))
        if rel.is_absolute() or ".." in rel.parts or ".git" in rel.parts:
            raise ValueError("unsafe source path")
        path = source / rel
        if not path.exists() and not path.is_symlink():
            continue  # Preserve a tracked deletion.
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"candidate source must be a regular file: {rel}")
        if path.resolve(strict=True) != path:
            raise ValueError(f"symlink ancestor in candidate source: {rel}")
        dest = target / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, dest)
        dest.chmod(0o755 if path.stat().st_mode & 0o111 else 0o644)
    if run("git", "rev-parse", "HEAD", cwd=source) != head:
        raise ValueError("source HEAD changed while preparing candidate")
    return head


def build(source, destination):
    destination.mkdir(mode=0o700)
    checkout = destination / "checkout"
    head = snapshot(source, checkout)
    env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
    binary = destination / "nimbus"
    subprocess.run(["go", "build", "-trimpath", "-ldflags",
                    "-s -w -X github.com/Furyfree/nimbus/internal/version.Engine="
                    f"0.0.0-dev.{head[:12]}", "-o", str(binary), "./cmd/nimbus"],
                   cwd=checkout, env=env, check=True)
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    (destination / "candidate.json").write_text(json.dumps({
        "commit": head, "binary_sha256": digest,
        "note": "Unpublished candidate; checkout includes local changes."
    }, indent=2) + "\n")
    return destination


def receive(stream, home):
    """Extract only regular files into a new private directory, never a target."""
    home = home.resolve(strict=True)
    if home.stat().st_uid != os.getuid():
        raise ValueError("candidate home has a foreign owner")
    destination = Path(tempfile.mkdtemp(prefix=".nimbus-candidate-", dir=home))
    try:
        with tarfile.open(fileobj=stream, mode="r|gz") as archive:
            seen = set()
            total = 0
            for member in archive:
                rel = PurePosixPath(member.name)
                if (rel.is_absolute() or ".." in rel.parts or not rel.parts
                        or rel.parts[0] not in {"checkout", "nimbus", "candidate.json"}
                        or member.name in seen or not member.isfile()):
                    raise ValueError(f"unsafe candidate member: {member.name}")
                seen.add(member.name)
                total += member.size
                if total > 512 * 1024 * 1024:
                    raise ValueError("candidate exceeds 512 MiB")
                path = destination.joinpath(*rel.parts)
                path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with path.open("xb") as output, archive.extractfile(member) as data:
                    shutil.copyfileobj(data, output)
                path.chmod(0o700 if member.mode & 0o111 else 0o600)
        metadata = json.loads((destination / "candidate.json").read_text())
        binary = destination / "nimbus"
        if hashlib.sha256(binary.read_bytes()).hexdigest() != metadata["binary_sha256"]:
            raise ValueError("candidate binary checksum mismatch")
        print(f"Candidate staged at {destination}")
        prefix = shlex.quote(str(binary))
        checkout = shlex.quote(str(destination / "checkout"))
        print(f"Inspect: {prefix} validate --checkout {checkout}")
        print(f"Plan:    {prefix} sync --plan --checkout {checkout} --machine vm")
        print("No candidate command has been executed. Stable Nimbus is unchanged.")
        return destination
    except BaseException:
        # This function owns exactly this newly allocated directory.
        shutil.rmtree(destination)
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path)
    parser.add_argument("--output", type=Path, help="build locally into a new directory only")
    parser.add_argument("--host", default="pby@127.0.0.1")
    parser.add_argument("--port", type=int, default=2222)
    parser.add_argument("--receive", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.receive:
        receive(sys.stdin.buffer, Path.home())
        return
    source = args.source or Path(__file__).resolve().parents[2]
    if not re.fullmatch(r"[A-Za-z0-9_.@:-]+", args.host) or args.host.startswith("-"):
        parser.error("invalid SSH destination")
    if not 1 <= args.port <= 65535:
        parser.error("invalid SSH port")
    if args.output:
        build(source, args.output.absolute())
        print(f"Candidate built locally at {args.output.absolute()}")
        return
    with tempfile.TemporaryDirectory(prefix="nimbus-stage-") as work:
        destination = build(source, Path(work) / "candidate")
        archive = Path(work) / "candidate.tar.gz"
        with tarfile.open(archive, "w:gz") as output:
            for path in sorted(destination.rglob("*")):
                if path.is_file():
                    output.add(path, arcname=str(path.relative_to(destination)), recursive=False)
        receiver = Path(__file__).read_text()
        command = "python3 -I -B -c " + shlex.quote(receiver) + " --receive"
        with archive.open("rb") as data:
            subprocess.run(["ssh", "-o", "BatchMode=yes", "-p", str(args.port),
                            args.host, command], stdin=data, check=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as exc:
        sys.exit(f"vm-stage: {exc}")
