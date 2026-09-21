#!/usr/bin/env python3
"""Build and exercise two signed candidate RPMs in a network-disabled container."""

import argparse
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

root = Path(__file__).resolve().parents[3]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", help="use an already built Fedora test image")
args = parser.parse_args()
image = args.image or "nimbus-maintenance-test"
if not args.image:
    subprocess.run(
        [
            "docker",
            "build",
            "--platform=linux/amd64",
            "-t",
            image,
            str(Path(__file__).parent),
        ],
        check=True,
    )
with tempfile.TemporaryDirectory(prefix="nimbus-maintenance-") as directory:
    work = Path(directory)
    env = os.environ | {"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"}
    for version, name in (("0.4.6", "nimbus-old"), ("0.4.7", "nimbus-new")):
        subprocess.run(
            [
                "go",
                "build",
                "-ldflags="
                + "-X github.com/Furyfree/nimbus/internal/version.Engine="
                + version,
                "-o",
                str(work / name),
                "./cmd/nimbus",
            ],
            cwd=root,
            env=env,
            check=True,
        )
    shutil.copyfile(Path(__file__).with_name("test.sh"), work / "test.sh")
    subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--network=none",
            "--platform=linux/amd64",
            "-v",
            str(work) + ":/input:ro,Z",
            "--entrypoint",
            "/bin/bash",
            image,
            "/input/test.sh",
        ],
        check=True,
    )
