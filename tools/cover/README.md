# gh-mirror 社交预览封面生成器

生成 GitHub 社交预览用的 1280×640 封面（PNG + SVG），含中英文两个版本。

- `build_cover.py`    → 中文版 `cover.svg` / `cover.png`
- `build_cover_en.py` → 英文版 `cover_en.svg` / `cover_en.png`

## 依赖
```bash
pip install -r requirements.txt
```
- `cairosvg` 需要原生 `libcairo` 库。
  - Linux：系统一般自带；缺失时 `apt install libcairo2`（Debian/Ubuntu）。
  - Windows：若未全局安装，设置环境变量 `CAIRO_BIN_DIR` 指向含 `libcairo-2.dll` 的目录。

## 用法
```bash
# 在仓库根目录执行，输出到当前目录
python tools/cover/build_cover.py
python tools/cover/build_cover_en.py

# 指定输出目录（避免写入仓库）
COVER_OUT=/tmp/cover python tools/cover/build_cover.py
```
- 资源根默认是脚本所在目录（`logos/` 与本脚本同级）；可用 `COVER_ROOT` 覆盖。
- 输出默认是当前工作目录；可用 `COVER_OUT` 覆盖。

## 资源
- `logos/docker.svg`、`logos/github.svg`、`logos/huggingface.svg`：服务节点品牌图标。
- `logos/project_logo.svg`：项目官方 `logo.svg`（绿→青渐变圆角方块 + 白色同步箭头）。
- 封面中央网关六边形内嵌 `project_logo`，右侧三服务节点用真实品牌图标；SVG 内联图标保证矢量缩放无损。

## 约定
- `social-preview.*` 使用中文版封面；`assets/cover-zh.*` / `assets/cover-en.*` 存两份语言资产。
- 改图后请用脚本重出，**不要手改位图**，以保证 SVG 与 PNG 一致。
