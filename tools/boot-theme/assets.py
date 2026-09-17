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
    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(data)) + chunk(b"IEND", b""))


def lock(x, y):
    shackle = (y <= 18 and abs(math.hypot(x - 24, y - 18) - 12) <= 1)
    sides = (18 <= y <= 29 and (abs(x - 12) <= 1 or abs(x - 36) <= 1))
    body = (5 <= x <= 43 and 28 <= y <= 61
            and not (7 < x < 41 and 30 < y < 59))
    return shackle or sides or body


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
        "lock.png": png(48, 64, lock, (238, 238, 238)),
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
