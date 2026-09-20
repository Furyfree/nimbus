"""Generate reproducible original Paper Dark Plymouth artwork (no dependencies)."""

import argparse
import math
from pathlib import Path
import struct
import zlib


def png(width, height, inside, color):
    def chunk(kind, data):
        payload = kind + data
        return (struct.pack(">I", len(data)) + payload
                + struct.pack(">I", zlib.crc32(payload)))

    data = bytearray()
    for y in range(height):
        data.append(0)
        for x in range(width):
            coverage = sum(inside(x + (sx + .5) / 4, y + (sy + .5) / 4)
                           for sy in range(4) for sx in range(4))
            data.extend((*color, round(255 * coverage / 16)))
    # Level 0 stores the rows uncompressed so the bytes are identical across
    # zlib versions; the check runs on multiple distributions.
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(bytes(data), 0)) + chunk(b"IEND", b""))


def rounded(x, y, left, top, right, bottom, radius):
    if not (left <= x <= right and top <= y <= bottom):
        return False
    cx = min(max(x, left + radius), right - radius)
    cy = min(max(y, top + radius), bottom - radius)
    return math.hypot(x - cx, y - cy) <= radius


def lock(x, y):
    # A solid padlock: thin outlines vanish when Plymouth scales the icon to
    # about twenty pixels at 1024x768, a filled shape with a keyhole does not.
    arc = y <= 32 and 15 <= math.hypot(x - 42, y - 32) <= 28
    legs = 32 <= y <= 44 and (14 <= x <= 27 or 57 <= x <= 70)
    body = rounded(x, y, 2, 40, 82, 94, 10)
    keyhole = rounded(x, y, 36, 56, 48, 80, 6)
    return (arc or legs or body) and not keyhole


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    theme = (Path(__file__).resolve().parents[2]
             / "system/root/usr/share/plymouth/themes/nimbus")
    assets = {
        "pixel.png": png(1, 1, lambda x, y: True, (136, 136, 136)),
        "bullet.png": png(10, 10, lambda x, y: math.hypot(x - 5, y - 5) <= 4,
                          (238, 238, 238)),
        "lock.png": png(84, 96, lock, (238, 238, 238)),
    }
    for name, content in assets.items():
        path = theme / name
        if args.check:
            if not path.is_file() or path.read_bytes() != content:
                parser.exit(1, f"{path}: run tools/boot-theme/assets.py\n")
        else:
            path.write_bytes(content)


if __name__ == "__main__":
    main()
