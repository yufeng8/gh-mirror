# MCP 接入指南（AI Agent 友好）

gh-mirror 内置 **MCP（Model Context Protocol）服务端**：AI Agent 可通过标准 MCP 协议直接管理网关——查/建/删 Token、开关 Docker/HuggingFace 加速、查看与调整缓存、把原链接转成加速链接、健康检查、镜像搜索与标签查询。

- 协议：**JSON-RPC 2.0**，标准库手写，**零第三方依赖**
- 传输：**HTTP `POST /mcp`** 与 **stdio 子命令 `gh-mirror mcp`**，两者共用同一套工具实现
- 方法：`initialize` / `ping` / `tools/list` / `tools/call`（通知类 `notifications/initialized` 不回响应）

## 1. 准备：签发一个 admin 角色的 Token

MCP 鉴权要求 **admin 角色** 的 token（与普通代理用的 user token 区分）。

在管理后台 →「Token 管理」→「新增 Token」，**角色选择「管理员（MCP）」**即可。旧 token 若没有角色字段，加载时按 `user` 处理（不影响原有代理）。

> admin token 具备运维能力，**请勿随意外发**；它同时不会泄露 `ADMIN_SECRET`，也不能修改超级管理员账号。

## 2. 接入方式一：HTTP（远程 / 已部署的网关）

```bash
curl -sS -X POST https://<你的域名>/mcp \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

- 仅支持 `POST`（`GET` 返回 405）
- 单次请求/响应，**不做 SSE 流式**
- 支持 JSON-RPC 批量（请求体为数组）
- 未带 token / token 非 admin → `401`

## 3. 接入方式二：stdio（本地 Agent，推荐桌面端）

二进制里直接运行子命令，无需经过 HTTP：

```bash
gh-mirror mcp --token <admin-token>            # 或设置环境变量 GM_MCP_TOKEN
gh-mirror mcp --token <admin-token> --base https://<你的域名>   # 指定生成链接用的基地址
```

- 逐行读 JSON-RPC、逐行写响应；**stdout 只输出协议 JSON，所有日志写 stderr**
- `--base` 缺省时由 `LISTEN` 推导（如 `http://127.0.0.1:8080`）
- 未传 token 时以退出码 2 结束并提示用法

### 客户端配置示例

Claude Desktop / CodeBuddy / 其它支持 `mcpServers` 的客户端：

```json
{
  "mcpServers": {
    "gh-mirror": {
      "command": "gh-mirror",
      "args": ["mcp", "--token", "<admin-token>", "--base", "https://<你的域名>"]
    }
  }
}
```

若网关在远程主机上，用 HTTP 方式配置即可（客户端间差异较大，以其文档为准）。

## 4. 工具清单

| 工具 | 参数 | 说明 |
|------|------|------|
| `health` | — | 服务状态与运行时长 |
| `list_tokens` | — | 列出全部 token（角色、命中、流量、时间） |
| `create_token` | `token?`, `note?`, `role?` | 创建 token（role: `user` / `admin`） |
| `delete_token` | `token` | 删除 token |
| `get_services` | — | GitHub / Docker / HF 启用状态 |
| `set_service` | `name`(`docker`\|`hf`), `enabled` | **即时生效**开关并持久化 |
| `get_cache_stats` | — | 已用 / 单文件上限 / 总上限 / 时效 / 目录 |
| `set_cache_limits` | `maxFileMB?`, `maxTotalMB?`, `ttlMinutes?` | 热改缓存参数并持久化 |
| `clear_cache` | — | 清空缓存（不影响 token 与设置） |
| `convert_link` | `url` | 原链接 / 镜像名 → 加速链接（与后台「链接转换」同一实现） |
| `search_images` | `q`, `page?` | Docker Hub 镜像搜索 |
| `list_tags` | `repo` | Docker Hub 标签列表 |

`tools/call` 返回结构：`{"content":[{"type":"text","text":"…"}],"isError":false}`；失败时 `isError:true` 且 `text` 为原因。

## 5. 调用示例

```bash
# 列出工具
curl -sS -X POST https://<域名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 查服务状态
curl -sS -X POST https://<域名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_services","arguments":{}}}'

# 开启 HuggingFace 加速（即时生效）
curl -sS -X POST https://<域名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"hf","enabled":true}}}'

# 把 GitHub 链接转成加速链接
curl -sS -X POST https://<域名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"convert_link","arguments":{"url":"https://github.com/owner/repo/releases/download/v1.0/app.zip"}}}'

# stdio 方式
echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | gh-mirror mcp --token <tok>
```

## 6. 安全建议

1. **只给需要的工具权限**：admin token 能建/删 token 与改设置，按信任范围签发。
2. **不要暴露在公共网络无保护**：`/mcp` 会经过现有限流与 IP ACL 中间件；若网关经公网暴露，建议在 CDN（EdgeOne）对 `/mcp` 配置速率限制。
3. **stdio 模式更安全**：不经网络，token 只在本地进程与配置文件中（注意配置文件的读取权限）。
4. **定期轮换**：在后台删除旧 admin token、签发新的。
5. **审计**：写操作（建/删 token、开关服务、改缓存、清缓存）都会写入服务日志。
6. MCP **不提供**超级管理员账号密码的读写，也不泄露 `ADMIN_SECRET`。

## 7. 故障排查

| 现象 | 原因 / 处理 |
|------|-------------|
| `401` | 未带 `Authorization: Bearer`，或 token 不是 admin 角色 |
| `405` | 用了 `GET`，`/mcp` 只支持 `POST` |
| `-32601 不支持的方法` | 方法名拼错；支持 `initialize`/`ping`/`tools/list`/`tools/call` |
| `-32602 未知工具` | 工具名不存在，用 `tools/list` 核对 |
| stdio 无响应 | 确认 token 有效；查看 **stderr** 日志（stdout 只有协议 JSON） |
| `convert_link` 提示未开启 | Docker / HF 未开启，先 `set_service` 或去后台「服务管理」开启 |
| `search_images` 失败 | 上游 Docker Hub 不可达或返回非 200（受出网环境影响） |
