#!/usr/bin/env python3
"""把 TTF 轉成 LovyanGFX 可用的抗鋸齒 VLW 字型（C 陣列），只收錄需要的字元以節省空間。
用法：python3 fontgen.py  → 產生 ../aimon/fonts.h
"""
import os
import struct
from PIL import ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
INTER = os.path.join(HERE, "fonts", "Inter-Variable.ttf")   # Inter（SIL OFL），最接近 Apple SF 的開源字型
ASCII = "".join(chr(c) for c in range(0x20, 0x7F)) + "·"
FONTS = [
    # (C 名稱, 像素大小, 字重, 字元)
    ("F_NUM",   58, 600, "0123456789:-. "),
    ("F_CLOCK", 62, 250, "0123456789:"),           # 螢幕保護的大時鐘（細字）
    ("F_UNIT",  24, 600, "%"),
    ("F_NAME",  33, 800, ASCII),
    ("F_LABEL", 18, 600, ASCII),
    ("F_SUB",   15, 500, ASCII),
    ("F_TINY",  13, 600, ASCII),
]


def vlw(size, weight, chars):
    font = ImageFont.truetype(INTER, size)
    font.set_variation_by_axes([min(32, max(14, size)), weight])
    ascent, descent = font.getmetrics()
    glyphs = []
    for ch in sorted(set(chars), key=ord):
        mask, (ox, oy) = font.getmask2(ch, mode="L")
        w, h = mask.size
        bitmap = bytes(mask) if w and h else b""
        adv = int(round(font.getlength(ch)))
        dy = ascent - oy             # 基線到字形頂端
        glyphs.append((ord(ch), h, w, adv, dy, ox, bitmap))
    out = bytearray(struct.pack(">6i", len(glyphs), 11, size, 0, ascent, descent))
    for u, h, w, adv, dy, dx, _ in glyphs:
        out += struct.pack(">7i", u, h, w, adv, dy, dx, 0)
    for g in glyphs:
        out += g[6]
    return bytes(out)


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    dst = os.path.join(here, "..", "aimon", "fonts.h")
    with open(dst, "w") as f:
        f.write("#pragma once\n// 由 tools/fontgen.py 產生（Inter, SIL Open Font License）\n#include <stdint.h>\n\n")
        for name, size, weight, chars in FONTS:
            data = vlw(size, weight, chars)
            f.write("static const uint8_t %s[%d] = {\n" % (name, len(data)))
            for i in range(0, len(data), 24):
                f.write("  " + ",".join("0x%02x" % b for b in data[i:i + 24]) + ",\n")
            f.write("};\n\n")
            print("%-8s %3dpx w%d %6d bytes" % (name, size, weight, len(data)))


if __name__ == "__main__":
    main()
