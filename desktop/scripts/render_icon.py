#!/usr/bin/env python3
"""Draw the classroom-notify tray icon and pack ico/icns."""

import math
import struct
import zlib
from pathlib import Path

PINE = (20, 53, 42, 255)
CREAM = (244, 239, 228, 255)
EMBER = (228, 87, 46, 255)


def sd_round_box(px, py, left, top, right, bottom, radius):
    cx = (left + right) / 2
    cy = (top + bottom) / 2
    hx = (right - left) / 2
    hy = (bottom - top) / 2
    dx = abs(px - cx) - hx + radius
    dy = abs(py - cy) - hy + radius
    outside = math.hypot(max(dx, 0), max(dy, 0))
    inside = min(max(dx, dy), 0)
    return outside + inside - radius


def sample(x, y, size):
    nx = (x + 0.5) / size
    ny = (y + 0.5) / size
    bg = sd_round_box(nx, ny, 0.06, 0.06, 0.94, 0.94, 0.18)
    card = sd_round_box(nx, ny, 0.24, 0.20, 0.76, 0.78, 0.08)
    dot = math.hypot(nx - 0.68, ny - 0.32) - 0.085
    color = (0, 0, 0, 0)
    if bg < 0.6 / size:
        color = cover(color, PINE, smooth(bg * size))
    if card < 0.6 / size:
        color = cover(color, CREAM, smooth(card * size))
    if dot < 0.6 / size:
        color = cover(color, EMBER, smooth(dot * size))
    return color


def smooth(dist):
    return max(0.0, min(1.0, 0.5 - dist))


def cover(bottom, top, alpha):
    a = top[3] / 255 * alpha
    if a <= 0:
        return bottom
    out_a = a + bottom[3] / 255 * (1 - a)
    if out_a <= 0:
        return (0, 0, 0, 0)
    def ch(i):
        return int(round((top[i] * a + bottom[i] * (bottom[3] / 255) * (1 - a)) / out_a))
    return (ch(0), ch(1), ch(2), int(round(out_a * 255)))


def render(size):
    raw = bytearray()
    for y in range(size):
        raw.append(0)
        for x in range(size):
            acc = [0, 0, 0, 0]
            for sy in (0.25, 0.75):
                for sx in (0.25, 0.75):
                    px = sample(x + sx - 0.5, y + sy - 0.5, size)
                    for i in range(4):
                        acc[i] += px[i]
            raw.extend(int(round(v / 4)) for v in acc)
    return png_bytes(size, size, bytes(raw))


def chunk(tag, data):
    return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)


def png_bytes(w, h, raw):
    ihdr = struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")


def write_ico(path, images):
    count = len(images)
    header = struct.pack("<HHH", 0, 1, count)
    entries = b""
    payload = b""
    offset = 6 + 16 * count
    for size, png in images:
        width = 0 if size >= 256 else size
        height = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", width, height, 0, 0, 1, 32, len(png), offset)
        payload += png
        offset += len(png)
    path.write_bytes(header + entries + payload)


def write_icns(path, images):
    types = {128: b"ic07", 256: b"ic08", 512: b"ic09", 1024: b"ic10"}
    body = b""
    for size, png in images:
        if size not in types:
            continue
        chunk_data = types[size] + struct.pack(">I", len(png) + 8) + png
        body += chunk_data
    path.write_bytes(b"icns" + struct.pack(">I", len(body) + 8) + body)


def main():
    out = Path(__file__).resolve().parents[1] / "src-tauri" / "icons"
    out.mkdir(parents=True, exist_ok=True)
    pngs = {}
    for size in (16, 32, 48, 64, 128, 256, 512, 1024):
        pngs[size] = render(size)
    (out / "32x32.png").write_bytes(pngs[32])
    (out / "128x128.png").write_bytes(pngs[128])
    (out / "128x128@2x.png").write_bytes(pngs[256])
    (out / "icon.png").write_bytes(pngs[512])
    (out / "source.png").write_bytes(pngs[1024])
    write_ico(out / "icon.ico", [(s, pngs[s]) for s in (16, 32, 48, 64, 128, 256)])
    write_icns(out / "icon.icns", [(s, pngs[s]) for s in (128, 256, 512, 1024)])
    print(f"wrote icons to {out}")


if __name__ == "__main__":
    main()
