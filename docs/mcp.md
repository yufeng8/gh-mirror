# MCP 接入指南（AI Agent 友好）

gh-mirror 内置 **MCP（Model Context Protocol）服务端**：AI Agent 可通过标准 MCP 协议直接管理网关——查/建/删 Token、开关 Docker/HuggingFace 加速、查看与调整缓存、把原链接转成加速链接、健康检查、镜像搜索与标签查询。

- 协议：**JSON-RPC 2.0**，标准库手写，**零第三方依赖**
- 传输：**Streamable HTTP（`"type": "http"`，主推）`POST /ai/<用户名>/mcp`** 与 **stdio 子命令 `gh-mirror mcp`**（仅同机，备选），两者共用同一套工具实现
- 方法：`initialize` / `ping` / `tools/list` / `tools/call`（通知类 `notifications/initialized` 不回响应）

## 1. 准备：签发一个高级 Token

MCP 鉴权要求 **高级 Token**（内部角色值 `admin`，与普通代理用的 `user` token 区分）。

在管理后台 →「Token 管理」→「新增 Token」，**类型选择「高级 Token（可用于 MCP）」**即可。

> **多用户说明**：每个 Token 归属其创建用户。MCP 接入请使用**归属你自己的**高级 Token——后台「MCP 接入」页会按当前用户自动列出可用 Token 并生成可复制的接入命令，复制即用。

> 高级 Token 具备运维能力，**请勿随意外发**；它同时不会泄露 `ADMIN_SECRET`，也不能修改超级管理员账号。

## 2. 接入方式一：Streamable HTTP（远程网关 · **推荐首选**）

MCP 官方规范 **2025-03-26** 引入的 **Streamable HTTP** 传输（客户端配置里的 `"type": "http"`）。
这是 gh-mirror **主推**的接入方式：**远程 / 已部署的网关一律用它**——无需本地二进制、无需同机数据。

### 客户端配置（复制即用）

Claude Desktop / CodeBuddy / Cursor / Cline 等支持 `mcpServers` 的客户端：

```json
{
  "mcpServers": {
    "gh-mirror": {
      "type": "http",
      "url": "https://<你的域名>/ai/<用户名>/mcp",
      "headers": {
        "Authorization": "Bearer <高级Token>"
      }
    }
  }
}
```

> 部分客户端把 `type` 写作 `"streamable-http"`，语义相同；`url` 末尾带不带 `/` 均可。

### 等价 curl 验证

```bash
curl -sS -X POST https://<你的域名>/ai/<用户名>/mcp \
  -H 'Authorization: Bearer <高级Token>' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

### 协议行为（与 Streamable HTTP 规范对齐）

- **无状态**：不返回 `Mcp-Session-Id`，每个请求独立鉴权（规范允许无状态服务器）
- 请求用 `POST`，响应为**单个 JSON**（`Content-Type: application/json`）——这是规范允许的形式，**不强制 SSE 流**
- `GET` 返回 `405`（服务器不提供主动推送流，规范允许）；不支持 `DELETE`
- 支持 JSON-RPC 批量（请求体为数组）
- 路径里的 `<用户名>` 为后台登录用户名；`<高级Token>` **必须归属该用户**
- 用户名不存在 → `404`；未带 token / token 非高级 / token 不归属该用户 → `401`
- 旧端点 `POST /mcp` 已废弃（返回 `401` 并提示新格式）

## 3. 接入方式二：stdio（本地 Agent，推荐桌面端）

> **适用前提（先读这个）**：stdio 是在**运行这条命令的机器上**直接初始化网关并操作**本机数据**
> （`TOKENS_FILE` / 设置 / 缓存），token 也用同一份本地数据校验——它**不是**远程网关的遥控器：
> - MCP 客户端与网关**同一台机器**（或挂载同一数据目录）→ stdio 可用；
> - 网关部署在**远程服务器**（尤其 Docker 部署）→ 请用上面的 HTTP 方式；
>   本地桌面没有网关数据，token 校验不会通过，工具操作的也只是本地空数据。
> - Docker 镜像是极简无 shell（distroless），**无法 `docker exec` 运行 stdio**；
>   确需在网关主机上用 stdio 时：从 Releases 下载二进制，以与网关相同的
>   `TOKENS_FILE` / 数据目录运行——或直接用 HTTP（更简单）。

二进制里直接运行子命令，无需经过 HTTP：

```bash
gh-mirror mcp --token <高级Token>            # 或设置环境变量 GM_MCP_TOKEN
gh-mirror mcp --token <高级Token> --base http://127.0.0.1:8080   # 指定生成链接用的基地址
```

- 逐行读 JSON-RPC、逐行写响应；**stdout 只输出协议 JSON，所有日志写 stderr**
- `--base` **仅影响 `convert_link` 生成链接的前缀**，不改变工具操作的数据（始终是本机数据目录）；
  缺省由 `LISTEN` 推导（`:端口` 与 `0.0.0.0:端口` 会归一为 `http://127.0.0.1:端口`）
- 未传 token 时以退出码 2 结束并提示用法

### 客户端配置示例（MCP 客户端与网关同机）

Claude Desktop / CodeBuddy / 其它支持 `mcpServers` 的客户端：

```json
{
  "mcpServers": {
    "gh-mirror": {
      "command": "gh-mirror",
      "args": ["mcp", "--token", "<高级Token>"]
    }
  }
}
```

网关在远程主机上时，用 HTTP 方式配置即可（客户端间差异较大，以其文档为准）。

## 4. 工具清单

| 工具 | 参数 | 说明 |
|------|------|------|
| `health` | — | 服务状态与运行时长 |
| `list_tokens` | — | 列出你可查看的 token（普通用户仅自己的，超级管理员全部） |
| `create_token` | `token?`, `note?`, `role?` | 创建 token（归属当前调用用户；role: `user` / `admin`） |
| `delete_token` | `token` | 删除 token（仅自己的；超级管理员可删全部） |
| `get_services` | — | GitHub / Docker / HF 启用状态 |
| `set_service` | `name`(`docker`\|`hf`), `enabled` | **即时生效**开关并持久化（**仅超级管理员**） |
| `get_cache_stats` | — | 已用 / 单文件上限 / 总上限 / 时效 / 目录（**仅超级管理员**） |
| `set_cache_limits` | `maxFileMB?`, `maxTotalMB?`, `ttlMinutes?` | 热改缓存参数并持久化（**仅超级管理员**） |
| `clear_cache` | — | 清空缓存（**仅超级管理员**；不影响 token 与设置） |
| `convert_link` | `url` | 原链接 / 镜像名 → 加速链接（与后台「链接转换」同一实现，生成带你自己用户名段的 `/g/<用户名>/<token>/`、`/hf/<用户名>/<token>/`、`/offline/<用户名>/<token>/` 链接） |
| `search_images` | `q`, `page?` | Docker Hub 镜像搜索 |
| `list_tags` | `repo` | Docker Hub 标签列表 |

`tools/call` 返回结构：`{"content":[{"type":"text","text":"…"}],"isError":false}`；失败时 `isError:true` 且 `text` 为原因。

> **多用户权限模型**：MCP 入口要求高级 Token，按 Token 归属定位调用用户。Token 管理按归属隔离（普通用户只能管理自己的）；服务开关与缓存管理为实例级操作，仅超级管理员可调（与网页端 `/admin` 权限一致）；`convert_link` 使用调用者自己的 Token 生成带其用户名段的加速链接。

## 5. 调用示例

```bash
# 列出工具
curl -sS -X POST https://<域名>/ai/<用户名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 查服务状态
curl -sS -X POST https://<域名>/ai/<用户名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_services","arguments":{}}}'

# 开启 HuggingFace 加速（即时生效）
curl -sS -X POST https://<域名>/ai/<用户名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"hf","enabled":true}}}'

# 把 GitHub 链接转成加速链接
curl -sS -X POST https://<域名>/ai/<用户名>/mcp -H 'Authorization: Bearer <tok>' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"convert_link","arguments":{"url":"https://github.com/owner/repo/releases/download/v1.0/app.zip"}}}'

# stdio 方式
echo '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | gh-mirror mcp --token <tok>
```

## 6. 安全建议

1. **按归属与角色授权**：普通用户的高级 Token 只能管理自己的 Token、转换链接、查询服务状态；服务开关与缓存管理仅超级管理员的高级 Token 可调。请按信任范围签发。
2. **不要暴露在公共网络无保护**：`/ai/` 会经过现有限流与 IP ACL 中间件；若网关经公网暴露，建议在 CDN（EdgeOne）对 `/ai/` 配置速率限制。
3. **stdio 模式更安全**：不经网络，token 只在本地进程与配置文件中（注意配置文件的读取权限）。
4. **定期轮换**：在后台删除旧高级 Token、签发新的。
5. **审计**：写操作（建/删 token、开关服务、改缓存、清缓存）都会写入服务日志。
6. MCP **不提供**超级管理员账号密码的读写，也不泄露 `ADMIN_SECRET`。

## 7. 故障排查

| 现象 | 原因 / 处理 |
|------|-------------|
| `401` | 未带 `Authorization: Bearer`，或 token 不是高级 Token |
| `405` | 用了 `GET`，`/ai/<用户名>/mcp` 只支持 `POST` |
| `-32601 不支持的方法` | 方法名拼错；支持 `initialize`/`ping`/`tools/list`/`tools/call` |
| `-32602 未知工具` | 工具名不存在，用 `tools/list` 核对 |
| stdio 无响应 | 确认 token 有效；查看 **stderr** 日志（stdout 只有协议 JSON） |
| `convert_link` 提示未开启 | Docker / HF 未开启，先 `set_service` 或去后台「服务管理」开启 |
| `search_images` 失败 | 上游 Docker Hub 不可达或返回非 200（受出网环境影响） |
