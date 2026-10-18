# gh-mirror v1.3.2 — 部署脚手架与 AI Agent 提示词完善

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.3.2 在 v1.3.1 基础上**聚焦部署体验与「纠正第三方 AI Agent 幻觉配置」**，无代码逻辑变更、无破坏性变更。

## ✨ 本版亮点

- 🐳 **新增 `docker-compose.standalone.yml`（单容器最小正确配置）**：只暴露 `127.0.0.1:8080`、使用真实环境变量（`ADMIN_*` / `USER_PATH` / `CACHE_*` / `LISTEN` / `COOKIE_SECURE`）、不内置 nginx、不占主机 80/443，配合根目录 `.env.example` 即可 `docker compose -f docker-compose.standalone.yml up -d`。专为「机器已有 nginx / KPanel / CDN 反代」场景，也供 AI Agent / 自动部署直接选用，免于自己补端口时连带编出无效变量。
- 📝 **`.env.example` 补全真实变量并加警示**（根 + `deploy-docker`）：补齐 `USER_PATH` 等可选项；顶部明确「主服务只认这些变量」，点名 `GM_DATA_DIR` / `GM_PROXY_*` / `GM_ALLOWED_NETWORKS` / `GM_ADMIN_PASSWORD` 等一律不被读取（唯一 `GM_` 开头的是 `GM_MCP_TOKEN`，仅用于 `gh-mirror mcp` 子命令）。
- 📄 **`docs/docker-deploy.md` 新增「最小单文件 compose」小节**：推荐 `standalone.yml`，并附 `GM_*` 幻觉配置的反面教材说明。
- 🤖 **AI Agent 提示词同步**：`docs/agent-prompt.md`（内嵌首页 #agent 区块）+ README 中/英副本 + 首页 #deploy 示例——A 场景明确指向 `standalone.yml`，新增「严禁臆造 GM_ 前缀变量」硬约束。

## ⚠️ 为什么做这些

某些第三方 AI Agent 自动部署时，会臆造出一大堆 `GM_DATA_DIR`、`GM_PROXY_MAX_*`、`GM_PROXY_MAX_PROXY_*`、`GM_ALLOWED_NETWORKS`、`GM_ADMIN_PASSWORD` 等环境变量——这些**程序一概不读取**（等于无效配置，还可能把明文密码写进 compose）。v1.3.2 通过「给一份开箱即用的正确配置 + 在文档 / 提示词 / 示例里明确真实变量清单与禁令」从源头降低这类幻觉。

## ✅ 兼容性

- 端点格式、MCP 地址（`/ai/<用户名>/mcp`）、双入口（`/admin` + `/user`）均与 v1.3.1 完全一致。
- 无破坏性变更；从 v1.3.1 升级只需替换镜像 / 二进制，配置与数据卷不变。

## 🚀 快速开始

```bash
# 方式一：单容器 + 既有反代（推荐，已含正确 env）
cp .env.example .env   # 设 ADMIN_PASS / ADMIN_SECRET
docker compose -f docker-compose.standalone.yml up -d

# 方式二：docker run（与 v1.3.1 相同）
docker run -d --name gh-mirror \
  -p 127.0.0.1:8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  ghcr.io/yufeng8/gh-mirror:v1.3.2
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
