# gh-mirror v1.3.1 — 文档 / 脚本 / 资源同步与细节修复

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定，多用户隔离、Token 鉴权、磁盘缓存与镜像搜索，内置 MCP 服务端。v1.3.1 是 v1.3.0 之后的一批**文档、部署脚本与站点资源对齐**，全部为增量与修复，**无破坏性变更，升级零风险**。

## ✨ 本版亮点

- 🖼️ **站点图标补全**：新增 `logo.png`（256 透明位图）/ `favicon.ico`（16/32/48 多尺寸）仓库资产；站点新增 `GET /favicon.ico` 路由（ICO 兼容层）——现代浏览器仍走零请求的 SVG data-URI 主图标，老浏览器 / 保存书签 / 桌面快捷方式自动回落到 ICO。
- 📝 **文档全面对齐**：`README_EN.md` 按 v1.3.0 完整重写（原为 v1.0.0 旧结构，仅 GitHub 单服务）并加中英双语切换；修复 README 顶部锚点导航失效（带 U+FE0F 变体选择符的 emoji 使 GitHub 生成的 slug 含不可见字符，点击无反应，已换等价单码点 emoji 治本）；截图说明文案优化。
- 🔧 **部署脚本修复**：`deploy.sh` / `install.sh` 适配 v1.3.0（补 `USER_PATH`、代理示例改为 `/g/<用户名>/<token>/` 等新格式并补 HF / 离线 / MCP 示例、完成提示输出双入口；Docker 模式镜像 tag fallback 由 `latest` 改固定稳定版）；`uninstall.sh` 修复 compose 部署的 `gm-*` 卷/网络残留与 `/etc/gh-mirror` 目录残留。
- 🔐 **凭证安全**：`.gitignore` 补全含凭证的数据文件（`users.json` / `invites.json` / `settings.json`），防止密码哈希 / SMTP 密码被误提交；`CONTRIBUTING.md` 对齐最新架构（零依赖铁律、多用户与凭证安全、破坏性变更处理方式、文档同步清单等约定）。
- ✂️ **清理**：删除 README 中已不再引用的 4 张演示截图（`admin-conv-result` / `admin-usage` / `mobile-home` / `mobile-tokens`）；中文 README 同步「Star & Fork」板块。

## 🚀 升级说明（无破坏性变更）

- **Docker**：直接拉取 `ghcr.io/yufeng8/gh-mirror:v1.3.1` 替换旧容器即可；配置（`ADMIN_PATH` / `USER_PATH` / 数据卷）保持不变。
- **二进制**：下载本页对应平台二进制覆盖原文件，原 `tokens.json` / `users.json` 等数据文件保留。
- 端点格式、MCP 地址、双入口均与 v1.3.0 一致，无需重新生成链接或更新 Agent 客户端。

## 📋 端点速查（v1.3.0 起沿用）

| 服务 | 端点 | 鉴权 |
|---|---|---|
| Docker 拉取 | `/v2/<用户名>[/<自定义路径>]/…` | 路径即凭证（协议无法带 Token） |
| GitHub 资源 | `/g/<用户名>/<token>/<URL>` | 用户名段 + Token 归属 |
| HuggingFace 权重 | `/hf/<用户名>/<token>/<owner>/<repo>/…` | 用户名段 + Token 归属（开放模式免） |
| 离线镜像 tar | `/offline/<用户名>/<token>/<image>` | 用户名段 + Token 归属 |
| MCP 接入 | `POST /ai/<用户名>/mcp` | 用户名段 + 高级 Token 归属 |

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [README_EN.md](https://github.com/yufeng8/gh-mirror/blob/main/README_EN.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)

---
