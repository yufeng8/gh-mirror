# gh-mirror v1.3.3 — 反幻觉提示词强化 · 首页提示词取自 GitHub · MCP stdio 修正

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.3.3 聚焦**让 AI Agent 部署不再产生幻觉配置**、**首页提示词始终同步 GitHub**，并修正 MCP stdio 的说明与一处地址推导缺陷。无破坏性变更。

## ✨ 本版亮点

- 🛡️ **AI Agent 提示词升级为「白名单 + 反幻觉总规则」**（`docs/agent-prompt.md`，内嵌首页 `#agent`；README 中 / 英副本同步）：
  之前只点名禁止 `GM_*`，Agent 换个前缀就能绕过（实测已冒出 `GHW-MODE` / `GHW-DB` / `GHW-DATA-DIR` / `GHW-USER-MAX-CONC`）。现新增最高优先级的【第零步：反幻觉总规则】——**只用白名单**、不确定就先 grep 查证、**绝不发明任何 `GM_*` / `GHW_*` / `GH_*` 前缀变量**、不发明「看起来合理」的开关（并发 / 线程 / 模式 / 数据库等变量根本不存在，限流用 `RATELIMIT_ENABLED`、内存与 CPU 限制用 docker 层 `mem_limit`）、**只读真实存在的文档**（点名仓库没有 `COMPOSE.md` / `RELEASES.md` / `INSTALL.md` / `releases.atom`）。
- 🔗 **首页提示词改为直接从 GitHub 取最新**：不再内嵌展示（旧版要等发版打 tag 才更新）。首页 `#agent` 现在是引导卡片 + 「📋 复制最新提示词（从 GitHub 拉取）」按钮，前端从 jsDelivr → raw.githubusercontent.com 取仓库最新版并写入剪贴板；离线 / 内网实例自动回退镜像内置版本；管理员自定义过（`AGENT_PROMPT_FILE` / `gm-data/agent-prompt.md`）则优先用管理员版本。服务端不代拉 GitHub（不暴露实例 IP、不被限流、不拖慢首屏）。
- 🔧 **MCP stdio 说明修正 + 一处 bug 修复**：澄清 `gh-mirror mcp` 操作的是**本机数据目录**、**不是远程网关的遥控器**（远程部署请用 HTTP `POST /ai/<用户名>/mcp`）；Docker 镜像无 shell，不能 `docker exec` 运行 stdio。并修复 `defaultBase` 对 `LISTEN=0.0.0.0:8080` 推导出 `http://0.0.0.0:8080`（不可访问地址）导致 `convert_link` 生成坏链接的缺陷，现已归一为 `127.0.0.1`。
- 📝 **示例与文档同步**：`.env.example`（根 + `deploy-docker`）补全 `LISTEN` / `CACHE_DIR` / `TOKENS_FILE` / `DOCKER_ENABLED` 等真实变量，并把禁令升级为「任何非白名单前缀」；`docs/docker-deploy.md` 反面教材加入 `GHW_*` 并新增「给 AI Agent / 自动部署」指引（含不要验证 gh-mirror 不代理的端点）；`docker-compose.standalone.yml` 示例镜像同步到 v1.3.2 起的最新正式版。

## ⚠️ 为什么做这些

第三方 AI Agent 自动部署时反复臆造环境变量：`GM_*` 之后又冒出 `GHW-MODE` / `GHW-DB` / `GHW-DATA-DIR` / `GHW-USER-MAX-CONC`（程序一个都不读，所谓「1GB 内存调低并发」完全不生效），还会去读仓库里根本不存在的文档、枚举验证 gh-mirror 不代理的端点（`/hf/*/discussions`、`/hf/*/pulls/*/diff` 必然 404）并误判安装失败。v1.3.3 用「白名单 + 先查证再使用」从源头堵住，并让首页提示词始终等于仓库最新版，不必等发版。

## ✅ 兼容性

- 端点格式、MCP 地址（`/ai/<用户名>/mcp`）、双入口（`/admin` + `/user`）、配置与数据卷格式均与 v1.3.2 一致。
- 无破坏性变更；从 v1.3.2 升级只需替换镜像 / 二进制。

## 🚀 快速开始

```bash
# 单容器 + 既有反代（推荐：单文件、真实 env、只绑 127.0.0.1:8080）
cp .env.example .env   # 设 ADMIN_PASS / ADMIN_SECRET
docker compose -f docker-compose.standalone.yml up -d

# 或 docker run
docker run -d --name gh-mirror \
  -p 127.0.0.1:8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  ghcr.io/yufeng8/gh-mirror:v1.3.3
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
