# gh-mirror v1.3.0 — 全端点多用户隔离

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.3.0 将所有加速端点统一为**「功能路径 + 用户名段 + Token」**的多用户隔离模型，并修复 MCP 的多用户权限缺陷。

## ✨ 本版亮点

- 🔐 **全端点统一用户名段**：GitHub / HuggingFace / 离线包 / MCP 四类端点格式统一为 `/<功能>/<用户名>/<token>/` 与 `/ai/<用户名>/mcp`；用户名段反查用户表 + Token 归属双重校验（Token 必须是该用户名自己的，空 owner 仅归默认管理员）。
- 👥 **MCP 多用户隔离修复**：`list_tokens` 按归属过滤（不再泄露全部 Token）；`delete_token` 只能删自己的；`set_service` / `set_cache_limits` / `clear_cache` / `get_cache_stats` 收紧为**仅超级管理员**（与网页端一致）；`convert_link` 生成调用者自己的用户名段链接。
- 💾 **离线包单文件上限（新）**：按镜像 manifest 预估大小，超过设定值的离线包**不落盘缓存**（仍可实时打包流式下载）；与「磁盘满自动清理」同表单设置并持久化。
- 🎛️ **后台优化**：侧栏特权项归组（概览 → Token 管理 → MCP 接入 → 用户管理🛡️ → 缓存管理🛡️ → 服务管理🛡️）；缓存管理页去除重复的「磁盘可用」卡；Docker 拉取路径表述澄清（registry 拉取消**不校验 Token**，路径即凭证）。
- 🖼️ **文档与封面刷新**：README 全量同步新路径、缓存清理机制改正为「后台每 10 分钟三级自动清理」；社交封面重排为 logo + 项目名同行布局，英文版完整翻译。

## ⚠️ 破坏性变更（从 v1.2.0 升级必读）

1. **GitHub / HuggingFace 加速链接必须带用户名段**
   - `/g/<token>/<URL>` → `/g/<用户名>/<token>/<URL>`
   - `/hf/<token>/…` → `/hf/<用户名>/<token>/…`（**开放模式 `auth.mode=open` 保持原格式不变**）
   - **旧格式一律 404**。迁移：登录后台 →「用法示例」或「链接转换」重新复制带用户名段的新链接。
   - `<用户名>` 为后台登录用户名；`<token>` 必须是该用户名自己的 Token。

2. **MCP 端点改为 `/ai/<用户名>/mcp`**
   - `POST /mcp` → `POST /ai/<用户名>/mcp`
   - 旧端点返回 `401` 并提示 `MCP 端点已改为 /ai/<用户名>/mcp 形式（需带用户名段），请更新客户端配置`
   - 迁移：更新 AI Agent 客户端（Claude Desktop / Cursor 等）里的 URL；stdio 方式（`gh-mirror mcp --token`）不变，身份由 Token 归属确定。

3. **（v1.2.0 引入，升级时一并确认）双入口**
   - 超管入口 `/admin`；普通用户入口 `/user`（`USER_PATH` 可配、可隐藏），各自独立会话 Cookie。
   - 两个入口都不要与 `/g` `/hf` `/offline` `/v2` `/ai` `/mcp` `/search` `/healthz` 等保留路径冲突，冲突时启动即报错。

## 🚀 快速开始

```bash
# 方式一：Docker（推荐）
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  -e DOCKER_ENABLED=true -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:v1.3.0

# 方式二：下载本页二进制
# 各平台 gh-mirror-<os>-<arch>，`chmod +x` 后直接运行
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

## 📋 端点速查（v1.3.0）

| 服务 | 端点 | 鉴权 |
|---|---|---|
| Docker 拉取 | `/v2/<用户名>[/<自定义路径>]/…` | 路径即凭证（协议无法带 Token） |
| GitHub 资源 | `/g/<用户名>/<token>/<URL>` | 用户名段 + Token 归属 |
| HuggingFace 权重 | `/hf/<用户名>/<token>/<owner>/<repo>/…` | 用户名段 + Token 归属（开放模式免） |
| 离线镜像 tar | `/offline/<用户名>/<token>/<image>` | 用户名段 + Token 归属 |
| MCP 接入 | `POST /ai/<用户名>/mcp` | 用户名段 + 高级 Token 归属 |

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)

---
