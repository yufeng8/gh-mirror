# gh-mirror v1.4.5 — MCP 接入重做：让 AI Agent 真正用得起来

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。

v1.4.5 集中重做 **MCP（Model Context Protocol）接入层**。起因是对线上两个真实端点（超管 `/ai/admin/mcp`、普通用户 `/ai/fengk/mcp`）做了完整协议实测，发现「能连上」与「Agent 能正确用」之间有一段距离。本版同时修复了一个链接转换缺陷。

## ✨ 本版亮点

### MCP：从「能连通」到「会用」

- 🐛 **修复 `search_images` 只有 ★ 没有镜像名**：它自己解析了一份 Docker Hub 响应并读 `name`，真实字段是 `repo_name` —— 结果只能输出 `★21411`。现在复用 `/api/search` 的规范化逻辑，输出 **名称 / ★ / 拉取数 / 官方 / 上游更新时间 / 描述**，并新增 `limit`（默认 10）
- 🐛 **修复 `serverInfo.version` 恒为 `0.2.0`**：改为读取构建期注入的真实版本号
- 🐛 **修复非法 `role` 静默生效**：`create_token` 传 `role:"root"` 会被静默按 `user` 创建，现在直接报错并说明可选值
- 📖 **新增 `instructions`（协议 ≥ 2025-06-18）**：一段给模型的使用指引——推荐流程、破坏性操作提示、结果解析方式。这是提升 Agent 正确率最有效的一处投入
- 🎯 **工具描述全面重写**：12 个工具统一「何时用 + 返回什么 + 是否破坏性 + 权限要求」；破坏性工具显式标注 ⚠
- 🔒 **`tools/list` 按角色裁剪**：普通用户不再看到 4 个必然被拒的管理工具（`set_service` / `get_cache_stats` / `set_cache_limits` / `clear_cache`），结果附 `note` 说明隐藏了几个
- 🧩 **参数约束**：关键参数加 `enum`（不会再把 `github` 传给 `set_service`）、整数参数带默认值
- 🧭 **错误可自纠**：未知工具会列出全部可用工具名；参数缺失给出可执行提示（缺 `q` → 提示传 `{"q":"nginx"}`）
- 📦 **结构化输出**：`list_tokens` / `list_tags` / `search_images` / `get_services` / `get_cache_stats` / `health` 统一为「一行摘要 + ```json 代码块」

### 协议兼容（Streamable HTTP）

- **响应按 `Accept` 协商**：只要求 `text/event-stream` 的客户端现在拿到规范事件流；同时接受两者时仍返回单个 JSON（兼容性最好）
- `GET` → `405` + JSON 说明；`DELETE` → `200`（无状态、无需终止会话）
- `resources/list` / `resources/templates/list` / `prompts/list` 返回空列表；`logging/setLevel` 被接受
- `401` 等错误改为 JSON 并附接入 hint
- 支持 JSON-RPC 批量

### 链接转换修复

- 🐛 **无 `https://` 的 GitHub / HuggingFace 链接不再被误判成 Docker 镜像**（后台说明里明确支持这种写法，但此前会生成完全错误的 `/offline/...` 地址）

## 🔧 兼容性

- **无破坏性变更**：端点、鉴权方式、MCP 地址、双入口、配置与数据卷格式均不变
- `tools/call` 仍返回 `content[0].text`，只是内容改为「摘要 + JSON」，LLM 读取更稳
- 老客户端会忽略 `instructions` 等新增字段
- `tools/list` 按角色裁剪是**收敛**而非破坏：服务端鉴权仍是权威

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
  ghcr.io/yufeng8/gh-mirror:v1.4.5
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

### MCP 接入（推荐首选：Streamable HTTP）

```json
{
  "mcpServers": {
    "gh-mirror": {
      "type": "http",
      "url": "https://<你的域名>/ai/<用户名>/mcp",
      "headers": { "Authorization": "Bearer <高级Token>" }
    }
  }
}
```

> ⚠️ **高级 Token 等同于账号凭证**：持有者可管理 Token、开关服务、清空缓存。请勿提交进仓库或公开分享。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
