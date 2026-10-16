import os, re

# cairosvg 依赖 libcairo 原生库。
# Windows 上若未全局安装，可设置环境变量 CAIRO_BIN_DIR 指向含 libcairo-2.dll 的目录
# （如某些应用的 ImageViewer 模块目录）；非 Windows 通常系统自带，无需设置。
_cairo = os.environ.get("CAIRO_BIN_DIR")
if _cairo and os.path.isdir(_cairo) and _cairo not in os.environ.get("PATH", ""):
    os.environ["PATH"] = _cairo + os.pathsep + os.environ.get("PATH", "")

from PIL import Image, ImageDraw, ImageFont
import cairosvg

# 资源根：默认脚本所在目录（logos/ 与本脚本同级）；可用 COVER_ROOT 覆盖。
D = os.environ.get("COVER_ROOT", os.path.dirname(os.path.abspath(__file__)))
# 输出目录：默认当前工作目录；可用 COVER_OUT 覆盖（避免写入仓库目录）。
OUT = os.environ.get("COVER_OUT", os.getcwd())
W, H = 1280, 640
CYAN = (34, 211, 238)
GREEN = (52, 211, 153)
LIGHT = (203, 213, 225)
MUTE = (148, 163, 184)
WHITE = (238, 242, 247)


def hex2rgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


NODES = {
    #  name:       (circle fill, stroke, logo color, center)
    "docker":      ("#dbeafe", "#2496ED", "#0369a1", (770, 200)),
    "huggingface": ("#000000", "#eab308", "#FFD21E", (980, 120)),
    "github":      ("#f3f4f6", "#6b7280", "#111827", (1190, 200)),
}


def get_path(fn):
    return re.search(r'<path d="([^"]+)"', open(fn, encoding="utf-8").read()).group(1)


def render_logo(fn, color, size):
    s = open(fn, encoding="utf-8").read()
    s = s.replace('<path d=', f'<path fill="{color}" d=', 1)
    tmp = os.path.join(OUT, "_tmp.svg")
    out = os.path.join(OUT, "_tmp.png")
    open(tmp, "w", encoding="utf-8").write(s)
    cairosvg.svg2png(url=tmp, write_to=out, output_width=size, output_height=size)
    img = Image.open(out).convert("RGBA")
    os.remove(tmp)
    os.remove(out)
    return img


def render_project_logo(size):
    out = os.path.join(OUT, "_plogo.png")
    cairosvg.svg2png(url=os.path.join(D, "logos", "project_logo.svg"), write_to=out,
                     output_width=size, output_height=size)
    img = Image.open(out).convert("RGBA")
    os.remove(out)
    return img


def project_logo_svg(x, y, size):
    s = open(os.path.join(D, "logos", "project_logo.svg"), encoding="utf-8").read()
    inner = s.split(">", 1)[1].rsplit("</svg>", 1)[0]
    sc = size / 64
    return f'<g transform="translate({round(x, 2)},{round(y, 2)}) scale({sc})">{inner}</g>\n'


def font(sz, bold=False):
    if os.name == "nt":
        cands = (["C:/Windows/Fonts/msyhbd.ttc", "C:/Windows/Fonts/msyh.ttc"] if bold
                 else ["C:/Windows/Fonts/msyh.ttc"])
    else:
        base = "Bold" if bold else "Regular"
        cands = [
            f"/usr/share/fonts/truetype/noto/NotoSansCJK-{base}.ttc",
            f"/usr/share/fonts/opentype/noto/NotoSansCJK-{base}.ttc",
            "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
            "/System/Library/Fonts/PingFang.ttc",
        ]
    for p in cands:
        if os.path.isfile(p):
            try:
                return ImageFont.truetype(p, sz)
            except Exception:
                continue
    return ImageFont.load_default()


# ---------------- PNG ----------------
img = Image.new("RGBA", (W, H), (10, 14, 26, 255))
dr = ImageDraw.Draw(img)
top, bot = (10, 14, 26), (14, 23, 48)
for y in range(H):
    t = y / (H - 1)
    dr.line([(0, y), (W, y)], fill=tuple(int(top[i] + (bot[i] - top[i]) * t) for i in range(3)) + (255,))
grid = (22, 33, 58, 55)
for x in range(0, W, 80):
    dr.line([(x, 0), (x, H)], fill=grid)
for y in range(0, H, 80):
    dr.line([(0, y), (W, y)], fill=grid)
glow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
gd = ImageDraw.Draw(glow)
for r in range(330, 0, -8):
    gd.ellipse([980 - r, 340 - r, 980 + r, 340 + r], fill=CYAN + (int(70 * (1 - r / 330)),))
img = Image.alpha_composite(img, glow)
dr = ImageDraw.Draw(img)


def center(txt, x, y, f, fill):
    dr.text((x, y), txt, font=f, fill=fill, anchor="mm")


dr.text((70, 134), "SELF-HOSTED MULTI-SERVICE ACCELERATION GATEWAY", font=font(17, True), fill=CYAN)
dr.text((64, 158), "gh-mirror", font=font(98, True), fill=WHITE)
for i in range(226):
    t = i / 225
    dr.line([(72 + i, 280), (72 + i, 285)],
            fill=tuple(int(CYAN[j] + (GREEN[j] - CYAN[j]) * t) for j in range(3)))
dr.text((70, 316), "Docker images · GitHub repos · HuggingFace weights", font=font(28), fill=LIGHT)
dr.text((70, 354), "— One gateway to accelerate them all", font=font(28), fill=LIGHT)


def chip(x, y, label, color):
    dr.rounded_rectangle([x, y, x + 180, y + 38], radius=19, fill=(17, 28, 51), outline=(34, 48, 77))
    dr.ellipse([x + 16, y + 13, x + 26, y + 23], fill=color)
    dr.text((x + 38, y + 9), label, font=font(16), fill=LIGHT)


for x, y, label, col in [
    (70, 412, "Image Mirror", CYAN), (262, 412, "Offline tar", GREEN), (454, 412, "Image Search", (167, 139, 250)),
    (70, 466, "Rate Limit · ACL", (251, 191, 36)), (262, 466, "Token Auth", (244, 114, 182)), (454, 466, "SOCKS5 Egress", (56, 189, 248)),
]:
    chip(x, y, label, col)
dr.text((70, 548), "Pure Go stdlib · Zero third-party deps · Single binary · Offline build", font=font(18), fill=MUTE)

# topology lines
for p in [[(800, 226), (948, 318)], [(1180, 226), (1012, 318)], [(980, 154), (980, 268)], [(980, 414), (980, 484)]]:
    dr.line(p, fill=CYAN + (140,), width=2)
for px, py in [(800, 226), (1180, 226), (980, 154), (980, 484)]:
    dr.ellipse([px - 4, py - 4, px + 4, py + 4], fill=CYAN)

# node rings + real logos
for name, (fill_hex, stroke_hex, logo_hex, (nx, ny)) in NODES.items():
    dr.ellipse([nx - 46, ny - 46, nx + 46, ny + 46], fill=hex2rgb(fill_hex), outline=hex2rgb(stroke_hex), width=3)
    logo = render_logo(os.path.join(D, "logos", name + ".svg"), logo_hex, 58)
    img.paste(logo, (nx - 29, ny - 29), logo)
    center({"docker": "Docker", "huggingface": "HuggingFace", "github": "GitHub"}[name], nx, ny + 70, font(18), LIGHT)

# gateway + cache
dr.ellipse([888, 248, 1072, 432], outline=CYAN + (90,), width=2)
hex_pts = [(980, 270), (1040, 305), (1040, 375), (980, 410), (920, 375), (920, 305)]
dr.polygon(hex_pts, fill=(14, 74, 80))
dr.polygon(hex_pts, outline=(255, 255, 255), width=2)
plogo = render_project_logo(88)
img.paste(plogo, (980 - 44, 336 - 44), plogo)
center("gh-mirror", 980, 436, font(18), LIGHT)
for ry in (496, 512, 528):
    dr.ellipse([924, ry - 14, 1036, ry + 14], fill=(15, 29, 54), outline=(45, 212, 191), width=2)
center("Disk Cache", 980, 568, font(16), MUTE)

img.convert("RGB").save(os.path.join(OUT, "cover_en.png"))
print("PNG done")

# ---------------- SVG (inline real logos) ----------------
def node_svg(name, d, fill, stroke, logo, cx, cy):
    S, half = 58, 29
    sc = S / 24
    tx, ty = round(cx - half, 2), round(cy - half, 2)
    return (f'  <circle cx="{cx}" cy="{cy}" r="46" fill="{fill}" stroke="{stroke}" stroke-width="3"/>\n'
            f'  <g transform="translate({tx},{ty}) scale({sc})"><path d="{d}" fill="{logo}"/></g>\n')


svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="640" viewBox="0 0 1280 640" font-family="'Segoe UI','Microsoft YaHei','PingFang SC','Noto Sans CJK SC',sans-serif">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#0a0e1a"/><stop offset="1" stop-color="#0e1730"/>
    </linearGradient>
    <radialGradient id="glow" cx="0.5" cy="0.5" r="0.5">
      <stop offset="0" stop-color="#22d3ee" stop-opacity="0.35"/><stop offset="1" stop-color="#22d3ee" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="gw" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#22d3ee"/><stop offset="1" stop-color="#34d399"/>
    </linearGradient>
    <linearGradient id="title" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0" stop-color="#e2e8f0"/><stop offset="1" stop-color="#ffffff"/>
    </linearGradient>
    <pattern id="grid" width="80" height="80" patternUnits="userSpaceOnUse">
      <path d="M 80 0 L 0 0 0 80" fill="none" stroke="#16213a" stroke-width="1"/>
    </pattern>
  </defs>
  <rect width="1280" height="640" fill="url(#bg)"/>
  <rect width="1280" height="640" fill="url(#grid)" opacity="0.5"/>
  <circle cx="980" cy="340" r="330" fill="url(#glow)"/>

  <text x="70" y="150" font-size="17" letter-spacing="3" fill="#7dd3fc" font-weight="600">SELF-HOSTED MULTI-SERVICE ACCELERATION GATEWAY</text>
  <text x="66" y="252" font-size="98" font-weight="800" fill="url(#title)">gh-mirror</text>
  <rect x="72" y="278" width="226" height="5" rx="2.5" fill="url(#gw)"/>
  <text x="70" y="334" font-size="28" fill="#cbd5e1">Docker images · GitHub repos · HuggingFace weights</text>
  <text x="70" y="372" font-size="28" fill="#cbd5e1">— One gateway to accelerate them all</text>
  <g font-size="16" fill="#cbd5e1">
    <rect x="70" y="412" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="92" cy="431" r="5" fill="#22d3ee"/><text x="108" y="437">Image Mirror</text>
    <rect x="262" y="412" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="284" cy="431" r="5" fill="#34d399"/><text x="300" y="437">Offline tar</text>
    <rect x="454" y="412" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="476" cy="431" r="5" fill="#a78bfa"/><text x="492" y="437">Image Search</text>
    <rect x="70" y="466" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="92" cy="485" r="5" fill="#fbbf24"/><text x="108" y="491">Rate Limit · ACL</text>
    <rect x="262" y="466" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="284" cy="485" r="5" fill="#f472b6"/><text x="300" y="491">Token Auth</text>
    <rect x="454" y="466" width="180" height="38" rx="19" fill="#111c33" stroke="#22304d"/><circle cx="476" cy="485" r="5" fill="#38bdf8"/><text x="492" y="491">SOCKS5 Egress</text>
  </g>
  <text x="70" y="560" font-size="18" fill="#94a3b8">Pure Go stdlib · Zero third-party deps · Single binary · Offline build</text>

  <g stroke="#22d3ee" stroke-opacity="0.55" stroke-width="2" fill="none">
    <path d="M 800 226 L 948 318"/><path d="M 1180 226 L 1012 318"/>
    <path d="M 980 154 L 980 268"/><path d="M 980 414 L 980 484"/>
  </g>
  <g fill="#22d3ee"><circle cx="800" cy="226" r="4"/><circle cx="1180" cy="226" r="4"/><circle cx="980" cy="154" r="4"/><circle cx="980" cy="484" r="4"/></g>

{node_svg("docker", get_path(os.path.join(D,"logos","docker.svg")), "#dbeafe", "#2496ED", "#0369a1", 770, 200)}
  <text x="770" y="270" text-anchor="middle" font-size="18" fill="#cbd5e1">Docker</text>
{node_svg("huggingface", get_path(os.path.join(D,"logos","huggingface.svg")), "#000000", "#eab308", "#FFD21E", 980, 120)}
  <text x="980" y="190" text-anchor="middle" font-size="18" fill="#cbd5e1">HuggingFace</text>
{node_svg("github", get_path(os.path.join(D,"logos","github.svg")), "#f3f4f6", "#6b7280", "#111827", 1190, 200)}
  <text x="1190" y="270" text-anchor="middle" font-size="18" fill="#cbd5e1">GitHub</text>

  <circle cx="980" cy="340" r="92" fill="none" stroke="url(#gw)" stroke-opacity="0.3" stroke-width="2"/>
  <polygon points="980,270 1040.6,305 1040.6,375 980,410 919.4,375 919.4,305" fill="#0e4a50" stroke="#ffffff" stroke-opacity="0.85" stroke-width="2"/>
{project_logo_svg(936, 292, 88)}
  <text x="980" y="436" text-anchor="middle" font-size="18" fill="#cbd5e1">gh-mirror</text>
  <g stroke="url(#cache)" stroke-width="2"><ellipse cx="980" cy="496" rx="56" ry="14" fill="#0f1d36"/><ellipse cx="980" cy="512" rx="56" ry="14" fill="#0f1d36"/><ellipse cx="980" cy="528" rx="56" ry="14" fill="#0f1d36"/></g>
  <text x="980" y="568" text-anchor="middle" font-size="16" fill="#94a3b8">Disk Cache</text>
</svg>
'''
with open(os.path.join(OUT, "cover_en.svg"), "w", encoding="utf-8") as f:
    f.write(svg)
print("SVG done")
