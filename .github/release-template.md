# gh-mirror v1.4.1 — 修复用户 Token 详情页 · 详情改弹窗 · MCP 主推 Streamable HTTP

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.4.1 是 v1.4.0 的修复与完善版：修掉「Token 详情」页空白的问题，把查看他人 Token 改为**当前页弹窗**（不跳转），并把 MCP 接入在文档与后台统一改为**主推 Streamable HTTP**。

## 🐛 修复：用户 Token 详情页空白

现象：用户管理页显示某用户「Token 数 = 1」，点进去详情页表格却是空的。

根因：`userTokensBody` 模板漏了 `flash` 占位符（只有 5 个 `%s`，handler 传了 6 个参数），`fmt.Sprintf` 参数整体错位——标题被塞进 topbarRight（显示成「Token 详情 · 健康检查」）、卡片标题变成用户名、`<tbody>` 里塞进「返回用户管理」按钮，真正的 Token 行从未渲染。

修复：补上占位符并修正参数顺序，详情页可正常展示该用户的 Token。

## ✨ 改进：Token 详情改为弹窗（不跳转页面）

- 用户列表的 **Token 数**改为文字按钮，点击在**当前页弹窗**展示
- 弹窗内：完整 Token（带**复制**）、备注、类型、命中、流量、创建时间、最近使用
- 仅「安装时创建的超级管理员」可查看；**只读**，不能修改或删除
- 新增 `GET /admin/users/tokens.json?user=<用户名>` 供前端拉取（仅安装时超管，403/400/404 明确报错）
- 弹窗宽度 1040px，7 列完整不被裁；原 `/admin/users/tokens` 页面保留（深链兼容）

## 🚀 MCP：主推 Streamable HTTP（`"type": "http"`）

`/ai/<用户名>/mcp` 一直兼容 MCP **Streamable HTTP**（2025-03-26 规范）——无状态、`POST` 返回单个 JSON、`GET` 405 属规范允许。本次把它在**所有入口提为首选**：

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

- `docs/mcp.md`：方式一改为「Streamable HTTP（远程网关 · 推荐首选）」，新增客户端配置、`Accept` 示例与「协议行为」说明
- `README.md` / `README_EN.md`：配置示例放首位，stdio 明确标注「备选（仅同机）」
- 后台 `/admin/mcp`：方式一标题标注**推荐首选**，新增可一键复制的 `mcpServers` JSON（Token 段随选择器联动）
- `docs/agent-prompt.md` 与首页特性卡片同步

> 客户端若把 `type` 写作 `"streamable-http"` 语义相同；`url` 末尾带不带 `/` 均可。

## ✅ 兼容性

- 无破坏性变更：端点、MCP 地址、双入口、配置与数据卷格式均不变
- `settings.json` / `users.json` 自动兼容旧格式
- 原 `/admin/users/tokens` 深链页面保留

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
  ghcr.io/yufeng8/gh-mirror:v1.4.1
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> `ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
