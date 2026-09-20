"""Render native themes inside the disposable Fedora preview container only."""

import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def grub(out, width, height):
    work = out / f"grub-{width}"
    work.mkdir()
    esp = work / "esp/EFI/BOOT"
    esp.mkdir(parents=True)
    config = work / "grub.cfg"
    config.write_text(f"""set root=(memdisk)
set prefix=(memdisk)/boot/grub
insmod all_video
insmod gfxterm
insmod gfxmenu
insmod png
loadfont $prefix/themes/nimbus/mono-16.pf2
loadfont $prefix/themes/nimbus/mono-20.pf2
loadfont $prefix/themes/nimbus/mono-24.pf2
set gfxmode={width}x{height}
terminal_output gfxterm
set theme=$prefix/themes/nimbus/theme.txt
export theme
set timeout=15
menuentry 'Fedora Linux' {{ echo 'Preview only'; sleep 60; }}
menuentry 'Windows Boot Manager (on /dev/nvme0n1p1)' {{ echo 'Preview only'; sleep 60; }}
submenu 'Previous kernels' {{
""" + "".join(f"menuentry 'Fedora Linux (7.2.{i}-200.fc44.x86_64) 44 (Forty Four)' {{ sleep 60; }}\n"
              for i in range(8, 0, -1)) + """menuentry 'Fedora Linux (0-rescue) 44 (Forty Four)' { sleep 60; }
}
menuentry 'UEFI Firmware Settings' { echo 'Preview only'; sleep 60; }
""")
    theme = Path("/work/system/root/boot/grub2/themes/nimbus")
    run("grub2-mkstandalone", "-O", "x86_64-efi", "--locales=", "--fonts=",
        "-o", str(esp / "BOOTX64.EFI"), f"boot/grub/grub.cfg={config}",
        *(f"boot/grub/themes/nimbus/{p.name}={p}" for p in sorted(theme.iterdir())
          if p.is_file()))
    shutil.copyfile("/usr/share/edk2/ovmf/OVMF_VARS.fd", work / "vars.fd")
    qmp = work / "qmp.sock"
    with (work / "qemu.log").open("w") as log:
        vm = subprocess.Popen([
            "qemu-system-x86_64", "-machine", "q35,accel=tcg", "-m", "256",
            "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/edk2/ovmf/OVMF_CODE.fd",
            "-drive", f"if=pflash,format=raw,file={work}/vars.fd",
            "-drive", f"format=raw,file=fat:rw:{work}/esp", "-net", "none",
            "-display", "none", "-vga", "std", "-qmp", f"unix:{qmp},server=on,wait=off",
        ], stdout=log, stderr=log)
        try:
            for _ in range(100):
                if qmp.exists():
                    break
                if vm.poll() is not None:
                    raise RuntimeError("QEMU exited; inspect qemu.log")
                time.sleep(.1)
            with socket.socket(socket.AF_UNIX) as conn:
                conn.settimeout(15)
                conn.connect(str(qmp))
                stream = conn.makefile("rwb")
                stream.readline()

                def command(name, arguments=None):
                    request = {"execute": name}
                    if arguments:
                        request["arguments"] = arguments
                    stream.write(json.dumps(request).encode() + b"\n")
                    stream.flush()
                    while True:
                        response = json.loads(stream.readline())
                        if "error" in response:
                            raise RuntimeError(response)
                        if "return" in response:
                            return

                command("qmp_capabilities")
                time.sleep(10)
                ppm = work / "screen.ppm"
                command("screendump", {"filename": str(ppm)})
                run("magick", str(ppm), str(out / f"grub-{width}.png"))
                # Open the Previous kernels submenu and scroll to its end.
                for key in ["down", "down", "ret"] + ["down"] * 8:
                    command("send-key", {"keys": [{"type": "qcode", "data": key}]})
                    time.sleep(.3)
                command("screendump", {"filename": str(ppm)})
                run("magick", str(ppm), str(out / f"grub-{width}-scroll.png"))
                command("quit")
        finally:
            if vm.poll() is None:
                vm.terminate()
            vm.wait(timeout=10)


def plymouth(out):
    shutil.copytree("/work/system/root/usr/share/plymouth/themes/nimbus",
                    "/usr/share/plymouth/themes/nimbus", dirs_exist_ok=True)
    Path("/etc/plymouth/plymouthd.conf").write_text("[Daemon]\nTheme=nimbus\n")
    env = os.environ | {"DISPLAY": ":99", "GDK_BACKEND": "x11"}
    with (out / "xvfb.log").open("w") as log:
        display = subprocess.Popen(["Xvfb", ":99", "-screen", "0", "1280x720x24",
                                    "-nolisten", "tcp"], stdout=log, stderr=log)
        ask = None
        try:
            time.sleep(1)
            run("plymouthd", "--debug", f"--debug-file={out}/plymouth.log",
                "--no-boot-log", "--kernel-command-line=quiet splash",
                "--graphical-boot", env=env)
            run("plymouth", "show-splash", env=env)
            time.sleep(2)

            def capture(name):
                run("magick", "import", "-window", "root", str(out / name), env=env)

            capture("plymouth-boot.png")
            ask = subprocess.Popen(["plymouth", "ask-for-password",
                                    "--prompt=Please enter passphrase for disk WDS500G1X0E-00AFY0 (luks-0000):"],
                                   stdout=subprocess.DEVNULL, env=env)
            time.sleep(1)
            run("xdotool", "type", "sample", env=env)
            time.sleep(.3)
            capture("plymouth-unlock.png")
            run("xdotool", "type", "x" * 50, env=env)
            run("plymouth", "display-message", "--text=Incorrect passphrase. Try again.", env=env)
            time.sleep(.3)
            capture("plymouth-long-input.png")
            run("plymouth", "hide-message", "--text=Incorrect passphrase. Try again.", env=env)
            run("xdotool", "key", "BackSpace", "BackSpace", env=env)
            run("plymouth", "quit", env=env)
        finally:
            if ask and ask.poll() is None:
                ask.terminate()
                ask.wait(timeout=5)
            display.terminate()
            display.wait(timeout=5)


def main():
    if not Path("/.dockerenv").exists() or Path("/host").exists():
        raise SystemExit("Run only in the disposable preview container; never on the workstation")
    out = Path("/work/output")
    out.mkdir(exist_ok=True)
    for size in (16, 20, 24):
        run("grub2-mkfont", "-s", str(size), "-o",
            f"/work/system/root/boot/grub2/themes/nimbus/mono-{size}.pf2",
            "/usr/share/fonts/dejavu-sans-mono-fonts/DejaVuSansMono.ttf")
    license_text = Path("/usr/share/licenses/dejavu-sans-mono-fonts/LICENSE").read_text()
    Path("/work/system/root/usr/share/licenses/nimbus-boot-theme/FONT-LICENSE").write_text(
        "\n".join(line.rstrip() for line in license_text.splitlines()) + "\n")
    grub(out, 1280, 720)
    grub(out, 800, 600)
    plymouth(out)


if __name__ == "__main__":
    main()
