# 產生 firmware/aimon/logos.h：Claude / ChatGPT / Gemini 官方 LOGO 的小圖（今日統計頁的品牌圖示）
# LOGO 取自設定視窗 windows/ui.js 的 LOGOS（官方 SVG），轉成黑底的 RGB565（螢幕底色是純黑，不用透明度）。
# 需要：pip install resvg-py pillow
# 用法：python firmware/tools/logogen.py
import io, os, re
import resvg_py
from PIL import Image

ROOT = os.path.join(os.path.dirname(__file__), "..", "..")
UI_JS = os.path.join(ROOT, "windows", "ui.js")
OUT = os.path.join(ROOT, "firmware", "aimon", "logos.h")
SIZES = {240: (18, 32), 466: (36, 62)}   # 螢幕寬度 → 小圖（今日統計）、大圖（主畫面取代名稱，設計 32 px）邊長，AMOLED 要偶數
BRANDS = ["claude", "chatgpt", "gemini"]


def logos():
    js = open(UI_JS, encoding="utf-8").read()
    out = {}
    for b in BRANDS:
        m = re.search(r"\b%s:\s*`(<svg.*?</svg>)`" % b, js, re.S)
        if not m:
            raise SystemExit("ui.js 找不到 %s 的 LOGO" % b)
        svg = m.group(1)
        if "xmlns" not in svg:
            svg = svg.replace("<svg ", '<svg xmlns="http://www.w3.org/2000/svg" ', 1)
        out[b] = svg
    return out


def rgb565(svg, n):
    png = bytes(resvg_py.svg_to_bytes(svg_string=svg, width=n, height=n))
    img = Image.open(io.BytesIO(png)).convert("RGBA")
    bg = Image.new("RGBA", img.size, (0, 0, 0, 255))
    img = Image.alpha_composite(bg, img).convert("RGB")
    d = img.tobytes()
    return [((d[i] >> 3) << 11) | ((d[i + 1] >> 2) << 5) | (d[i + 2] >> 3) for i in range(0, len(d), 3)]


def main():
    svgs = logos()
    with open(OUT, "w", encoding="utf-8", newline="\n") as f:
        f.write("#pragma once\n// 由 firmware/tools/logogen.py 產生（來源：windows/ui.js 的官方 LOGO），不要手動改\n")
        f.write("// 黑底 RGB565，順序跟 BRANDS 一樣：claude, chatgpt, gemini\n")
        for w, (n, nb) in SIZES.items():
            f.write("\n#if LCD_W == %d\nstatic const int LOGO_N = %d;\nstatic const int LOGOB_N = %d;\n" % (w, n, nb))
            for prefix, size in (("LOGO", n), ("LOGOB", nb)):
                for b in BRANDS:
                    px = rgb565(svgs[b], size)
                    f.write("static const uint16_t %s_%s[%d] = {\n" % (prefix, b.upper(), size * size))
                    for i in range(0, len(px), 16):
                        f.write("  " + ",".join("0x%04X" % v for v in px[i:i + 16]) + ",\n")
                    f.write("};\n")
            f.write("#endif\n")
        f.write("\nstatic const uint16_t* const LOGOS[] = {LOGO_CLAUDE, LOGO_CHATGPT, LOGO_GEMINI};\n")
        f.write("static const uint16_t* const LOGOS_BIG[] = {LOGOB_CLAUDE, LOGOB_CHATGPT, LOGOB_GEMINI};\n")
    print("wrote", os.path.normpath(OUT))


if __name__ == "__main__":
    main()
