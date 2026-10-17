# gh-mirror v1.1.0 — 首个正式版

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像**、**GitHub 资源**、**HuggingFace 权重**三类加速一站搞定，内置 Token 鉴权、磁盘缓存、镜像搜索与 **MCP 服务端**（AI Agent 友好）。

## ✨ 本版亮点

- 🤖 **MCP 接入**：内置 MCP 服务端，AI Agent 用标准协议直接管理网关——`POST /mcp`（HTTP）与 `gh-mirror mcp`（stdio）两种传输，12 个工具覆盖 Token、服务开关、缓存、链接转换、镜像搜索
- 🎛️ **管理后台**：概览 / 服务管理 / 缓存管理 / Token 管理 / **MCP 接入**五个页面；小屏抽屉导航、Token 独立页（类型 / 一键复制 / 行内编辑）、操作反馈提示
- 🐳 **Docker 路径安全**：`/v2` 强制 `<用户名>[/<自定义路径>]` 前缀（为多用户预留）、`/offline` 离线包强制 Token
- 💾 **磁盘缓存 + 余量保护**：流式代理 + 磁盘缓存（Range 续传）；磁盘可用低于 1 GB 自动按最旧优先清理
- 🔒 **访问可控**：Token 鉴权、IP/仓库黑白名单、令牌桶限流、SOCKS5 出站

## ⚠️ 破坏性变更（从预发布升级必读）

1. **Docker 拉取路径变更**：`/v2/...` → `/v2/<用户名>[/<自定义路径>]/...`，`registry-mirrors` 不再生效（docker daemon 不支持带路径 mirror），改用带路径 pull：
   ```bash
   docker pull <域名>/<用户名>/library/nginx:1.25
   ```
2. **离线包强制 Token**：`/offline/<镜像>` → `/offline/<token>/<镜像>`，缺失或无效一律 404
3. **Token 字段**：新增 `owner`（归属用户，多用户预留，旧数据自动归属默认管理员）；后台展示的「角色」改称「类型」（内部值 `admin` / `user` 不变，MCP 仍要求高级 Token）

## 🚀 快速开始

```bash
# 方式一：Docker（推荐）
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e DOCKER_ENABLED=true -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:v1.1.0

# 方式二：下载本页二进制
# 各平台 gh-mirror-<os>-<arch>，`chmod +x` 后直接运行
```

启动后访问 `http://<域名>/admin` 登录后台，创建 Token 后即可使用；MCP 接入命令见后台「MCP 接入」页。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)

---
