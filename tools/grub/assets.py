"""Generate or check the Nimbus theme's solid-color GRUB box slices."""

import argparse
from pathlib import Path
import struct
import zlib


def png(width, color):
    def chunk(kind, data):
        payload = kind + data
        return struct.pack(">I", len(data)) + payload + struct.pack(
            ">I", zlib.crc32(payload)
        )

    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, 1, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(b"\x00" + bytes.fromhex(color) * width))
        + chunk(b"IEND", b"")
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="check without writing")
    args = parser.parse_args()
    theme = Path(__file__).resolve().parents[2] / "system" / "grub"
    for name, width, color in (
        ("selection_c.png", 1, "264a66"),
        ("scrollbar_frame_c.png", 4, "1b2633"),
        ("scrollbar_thumb_c.png", 4, "a7b5c5"),
    ):
        path = theme / name
        content = png(width, color)
        if args.check:
            if not path.is_file() or path.read_bytes() != content:
                parser.exit(1, f"{path}: regenerate with tools/grub/assets.py\n")
        else:
            path.write_bytes(content)


if __name__ == "__main__":
    main()
