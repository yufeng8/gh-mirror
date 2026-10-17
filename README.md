<p align="center">
  <img src="social-preview-zh.svg" alt="gh-mirror · 自托管多服务加速网关" width="100%">
</p>

<p align="center">
  <b>一个单二进制、零第三方依赖的自托管加速网关：Docker 镜像 · GitHub 资源 · HuggingFace 权重，一站加速。</b><br>
  内置 Token 鉴权、磁盘缓存、镜像搜索与 <b>MCP 服务端</b>，让 AI Agent 也能直接接管运维。
</p>

<p align="center">
  <strong>中文</strong> | <a href="README_EN.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml"><img src="https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml/badge.svg" alt="build"></a>
  <a href="https://github.com/yufeng8/gh-mirror/releases"><img src="https://img.shields.io/github/v/release/yufeng8/gh-mirror?include_prereleases&label=release" alt="release"></a>
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/deps-0-blueviolet" alt="zero dependencies">
  <img src="https://img.shields.io/badge/protocols-Docker%20%7C%20GitHub%20%7C%20HuggingFace-2ea44f" alt="protocols">
  <a href="docs/mcp.md"><img src="https://img.shields.io/badge/MCP-AI%20Agent%20ready-6f42c1" alt="MCP"></a>
  <img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="license">
</p>

<p align="center">
  <a href="#-界面一览">界面一览</a> ·
  <a href="#-功能亮点">功能亮点</a> ·
  <a href="#-mcp让-ai-agent-直接接管网关">MCP 接入</a> ·
  <a href="#-一键交给-ai-agent-部署">AI 部署</a> ·
  <a href="#-快速开始">快速开始</a> ·
  <a href="#-配置">配置</a>
</p>

---

## 🖼️ 界面一览

> 下面都是本机真实运行截图（桌面 1280 宽 + 移动端 393 宽），非设计稿。

<table>
  <tr>
    <td width="50%"><img src="docs/images/home-hero.png" alt="首页"><br><sub><b>首页</b>：能力概览 + 一键交接给 AI Agent</sub></td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/images/admin-overview-dark.png">
        <img src="docs/images/admin-overview-light.png" alt="管理后台概览">
      </picture>
      <br><sub><b>管理后台</b>：服务状态 / 统计 / 链接转换　<i>（主题支持切换深浅）</i></sub>
    </td>
  </tr>
  <tr>
    <td><img src="docs/images/admin-services.png" alt="服务管理"><br><sub><b>服务管理</b>：开关即时生效并持久化</sub></td>
    <td><img src="docs/images/admin-tokens.png" alt="Token 管理"><br><sub><b>Token 管理</b>：独立页面，类型 / 复制 / 行内编辑</sub></td>
  </tr>
</table>

---

## 🎯 功能亮点

### 📦 三类加速，一套网关

| 服务 | 加速端点 | 鉴权方式 | 亮点 |
|------|----------|----------|------|
| 🐳 **Docker 镜像** | `/v2/<用户名>[/<自定义路径>]/…` | 路径前缀（协议无法带 Token） | 多源回源（Docker Hub / GHCR / Quay / GCR / k8s）、Manifest+Token 内存缓存、Layer 磁盘缓存 |
| 🐙 **GitHub 资源** | `/g/<用户名>/<token>/https://github.com/…` | 用户名段 + Token | 覆盖 blob/raw、archive、releases、API，支持 `Range` 续传 |
| 🤗 **HuggingFace 权重** | `/hf/<用户名>/<token>/<owner>/<repo>/resolve/<rev>/<file>` | 用户名段 + Token | 自动跟随 302 到 CDN，模型 / LFS 大文件流式 + 缓存 |
| 📦 **离线镜像 tar** | `/offline/<用户名>/<token>/<image>` | 用户名段 + Token | 不需要本地 Docker，直接产出 `docker load` 可用的 tar |

> **多用户隔离**：`<用户名>` 为后台登录用户名，`<token>` 必须是该用户名自己的 Token（归属校验，无效或错配一律 404）；三类加速端点格式完全统一。

### 👥 多用户与账号体系

- 🔀 **双入口强隔离**：超管入口 `/admin` 与普通用户入口 `/user`（`USER_PATH` 可配、可隐藏）分离，各自独立会话 Cookie；普通用户登录超管入口会被拒绝。
- ✉️ **自主注册 / 找回密码**：超管配置 SMTP 后，可开放「邮箱验证码注册」；忘记密码可邮箱验证码自助重置（仅普通用户，超管密码只能服务器后台修改）。
- 🎟️ **邀请码注册**：可选「仅邀请码注册」，支持批量生成 / 自定义 / 导出（占位符格式）/ 备注 / 有效期 / 使用追踪。
- 🔐 **图形验证码**：登录 / 注册 / 管理员登录页均支持，用户入口与管理入口开关分别可控。
- 👤 **角色与归属**：超级管理员 / 普通用户两级角色；Token、Docker 自定义路径按用户隔离。

### 💡 为什么值得自托管

- 🚀 **单二进制、零依赖**：`CGO_ENABLED=0` 静态编译，多架构镜像，扔进 1 核 512M 的机器就能跑。
- 🔒 **访问可控**：Token 鉴权（默认开启）+ IP（CIDR）/ 仓库（通配符）黑白名单 + 令牌桶限流（IPv6 按 `/64` 归并）。
- 💾 **真磁盘缓存**：Layer / 文件落盘，命中直接返回 `X-Cache: HIT`；缓存总量、单文件上限、TTL 都能在后台热改，超限与磁盘余量不足自动按最旧优先清理。
- 🤖 **AI Agent 友好**：内置 MCP 服务端（`POST /ai/<用户名>/mcp` 与 `gh-mirror mcp` stdio），12 个工具覆盖 Token、服务开关、缓存、链接转换、镜像搜索。
- 🎛️ **后台可视化**：服务状态、统计、链接转换、Token 管理、MCP 接入、缓存管理全在网页里，开关即时生效并持久化到 `gm-data/settings.json`。

---

## 🤖 MCP：让 AI Agent 直接接管网关

内置 MCP（Model Context Protocol）服务端，Agent 不必猜接口，直接用标准协议操作网关：

```bash
# 方式一：HTTP（推荐，配高级 Token）
curl -sX POST https://mirror.example.com/ai/<用户名>/mcp \
  -H "Authorization: Bearer <高级Token>" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 方式二：stdio（本地或跳板机；token 必填）
gh-mirror mcp --token <高级Token>
```

| 类别 | 工具 |
|------|------|
| 健康与信息 | `health` |
| Token 管理 | `list_tokens` · `create_token` · `delete_token` |
| 服务开关 | `get_services` · `set_service` |
| 缓存 | `get_cache_stats` · `set_cache_limits` · `clear_cache` |
| 链接与镜像 | `convert_link` · `search_images` · `list_tags` |

> **多用户权限**：MCP 入口要求高级 Token（按 Token 归属定位调用用户）。Token 管理按归属隔离——普通用户只能查看 / 创建 / 删除自己的 Token；服务开关与缓存管理仅超级管理员可调（与网页端一致）；`convert_link` 生成带自己用户名段的加速链接。
>
> 详见 [docs/mcp.md](docs/mcp.md)。管理后台的「MCP 接入」页会按当前高级 Token 自动生成可复制的接入命令。

## 🤖 一键交给 AI Agent 部署

把下面这段提示词**整体复制**给 CodeBuddy / Cursor / Claude 等 AI Agent，它会自动完成部署与全链路验证：先读仓库文档选型（不强占 80/443、兼容已有反代 / CDN），再部署，最后逐项回报 `✅/❌`。

<details>
<summary><b>📋 点开复制完整部署提示词</b>（与 <a href="docs/agent-prompt.md">docs/agent-prompt.md</a> 保持一致，随版本更新）</summary>

```text
你是部署助手。请在这台 Linux 服务器上部署 gh-mirror（自托管多服务加速网关：Docker 镜像加速 / GitHub 资源加速 / HuggingFace 权重加速 + 镜像搜索 + MCP 接入），并对外通过域名 https://<你的域名> 提供服务。

【第一步：确认对外域名】
本提示词里的 <你的域名> 是通用占位符，不是任何具体站点。开始动手前先问用户：「对外使用的域名是什么？」；若用户没有提供，不要自行猜测或沿用示例域名，先问清再继续。下文所有 <你的域名> 都替换成用户确认的域名。

【硬性要求】
1. 先读仓库文档，并严格按其中的「环境适配与冲突排查表」选型，不要盲目执行：
   git clone https://github.com/yufeng8/gh-mirror.git
   - docs/docker-deploy.md：通用 Docker 部署、域名前置 CDN（EdgeOne/Cloudflare）的正确接法、Docker 加速安全
   - docs/deploy-runbook.md：先探测 → 选型 → 执行 → 端到端验证的完整流程
2. 优先使用预构建镜像，不要编译：ghcr.io/yufeng8/gh-mirror:<最新正式版 tag>（多架构、含全部特性）。
   不要写死版本号：动手前先从 GitHub Releases（https://github.com/yufeng8/gh-mirror/releases）确认当前最新正式版 tag，再拼进镜像名。
   :latest 跟随最新正式版，但为了升级可控，请显式使用具体 tag。
3. 数据持久化用命名卷：gm-data（token 与设置）、gm-cache（缓存）。重建容器不得丢数据。
4. ADMIN_PASS 与 ADMIN_SECRET 现场生成强随机值并妥善记录（勿写进任何提交或日志）。
   ADMIN_PATH 默认 /admin，LISTEN 默认 0.0.0.0:8080。
5. 部署方式按机器实际情况二选一，不要强占主机 80/443：
   A. 机器已有 nginx / KPanel 等 Web 服务：只跑网关并绑定 127.0.0.1:8080，由既有反代转发；
   B. 干净主机：用仓库自带 nginx（--profile proxy）或复用宿主机证书。
6. 反代段必须这样配（否则代理路径会 404）：
   merge_slashes off;
   proxy_pass http://127.0.0.1:8080$request_uri;   # 必须带 $request_uri
   proxy_set_header Host $host;
   proxy_set_header X-Forwarded-Proto https;        # 经 CDN 时固定 https
   且不要再叠加 http→https 跳转（会形成回环）。
7. 若域名前置 CDN（EdgeOne/Cloudflare）：让 CDN 持公网证书并终止 TLS，源站只做 80 反代；
   CDN 侧关闭缓存与 URL 规范化/重写；不要在 CDN 后面的源站用 certbot（会报 521，重申请无用）；
   确需源站自己持证时用 DNS-01 验证。
8. 防火墙 / 云安全组放行源站 80 入站；对外 HTTPS 由 CDN 或主机提供。

【完成后必须逐项验证并回报结果】
- curl -fsS http://127.0.0.1:8080/healthz                → ok
- curl -fsS -H 'Host: <你的域名>' http://127.0.0.1/healthz → ok（源站反代段通）
- curl -fsS https://<你的域名>/healthz                     → ok（全链路通）
- 浏览器 https://<你的域名>/ 能打开（多服务宣传页）
- https://<你的域名>/admin 能登录（ADMIN_USER / ADMIN_PASS）
- 在后台添加 token 后：curl -fsSI "https://<你的域名>/g/<ADMIN_USER>/<ADMIN_TOKEN>/https://raw.githubusercontent.com/octocat/Hello-World/master/README" → 200
- 镜像搜索：curl -fsS "https://<你的域名>/search?q=nginx" 能返回结果
- 若开启 Docker 加速：docker pull <你的域名>/<ADMIN_USER>/library/nginx:1.25 能正常拉取并命中缓存（路径必须带用户名段；后台可再加自定义路径）；
  curl -fsS "https://<你的域名>/offline/<ADMIN_USER>/<ADMIN_TOKEN>/library/nginx:1.25" -o nginx.tar 且本机 docker load -i nginx.tar 能载入（离线包路径必须带用户名段与 token）

【遇到错误】
不要盲目重试。先读 docs/docker-deploy.md 的「已知坑 / 故障排查」，仍无法解决时，
把关键报错与相关配置原样贴出来分析。

【汇报格式】
部署方式（A/B）、镜像 tag、验证结果逐项 ✅/❌、以及任何偏离上述要求的改动及原因。
```

</details>

> 提示词内含「**先问清对外域名再动手**」的硬性约束与反代/CDN 正确接法，避免 Agent 猜测域名或配出 404。服务跑起来后，也可以把 [docs/mcp.md](docs/mcp.md) 里的 MCP 接入命令交给 Agent，让它直接接管 Token、服务开关与缓存。

---

## ✨ 特性

<p align="center"><img src="docs/images/home-features.png" alt="通用特性"></p>

- 🐳 **Docker 镜像加速**：兼容 Registry API v2，内置 Docker Hub / GHCR / Quay / GCR / registry.k8s.io 多源；Manifest/Token 内存缓存、Layer 写透磁盘缓存、流式传输、`Range` 断点续传。
- 🐙 **GitHub 资源加速**：`/g/<用户名>/<token>/` + 完整 GitHub 地址，覆盖 blob/raw、archive、releases、API，自带磁盘缓存。
- 🤗 **HuggingFace 加速**：`/hf/<用户名>/<token>/<owner>/<repo>/resolve/<rev>/<file>`，自动跟随 302 到 CDN，模型/LFS 流式 + 缓存。
- 📦 **离线镜像 tar**：`/offline/<用户名>/<token>/<image>` 无需本地 Docker，把镜像打包成 `docker load` 可用的 tar（流式 + 同镜像并发防抖；**需用户名段与 token**，避免公开暴露被白嫖流量）。
- 🔍 **镜像搜索**：Docker Hub 搜索 / 标签浏览，服务端渲染轻量页面（`/search`、`/tags`）与 JSON API（`/api/search`、`/api/tags`）。
- 🛡️ **安全管控**：Token 鉴权（默认）+ 可选开放模式；IP（CIDR）/ 仓库（通配符）黑白名单；按真实客户端 IP 的令牌桶限流（IPv6 归并 `/64`）。
- ⚙️ **统一配置**：`config.toml` + 环境变量覆盖，平滑兼容旧的环境变量部署。
- 🧦 **可选 SOCKS5 出站**：为 GitHub/HF/Docker 回源统一走 SOCKS5 代理。
- 🤖 **MCP 接入（AI Agent 友好）**：内置 MCP 服务端，AI Agent 可通过标准协议调用网关能力——管理 Token、开关 Docker/HuggingFace 加速、查看与调整缓存、链接转换、健康检查、镜像搜索与标签。支持 `POST /ai/<用户名>/mcp` 与 `gh-mirror mcp` stdio 两种接入（需归属自己的高级 Token），详见 `docs/mcp.md`。
- 🎛️ **后台可视化运维**：服务状态徽标、概览统计（Token 数 / 命中 / 流量 / 缓存占用 / 运行时长）、服务开关即时生效、缓存上限与时效可调 + 自动失效、链接转换工具。

> 文档示例域名为 `mirror.example.com`，请替换为你自己的域名。
> 设计参考了 [sky22333/hubproxy](https://github.com/sky22333/hubproxy) 的多服务架构，并以 Go **标准库**重写、无任何第三方依赖，便于审计与离线构建。

---

## 🚀 快速开始

### 🐳 方式一：Docker（推荐）

```bash
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e LISTEN=0.0.0.0:8080 \
  -e CACHE_DIR=/data/cache \
  -e TOKENS_FILE=/data/tokens.json \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='change-me' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  -e DOCKER_ENABLED=true \
  -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:v1.2.0
```

启动后：**超级管理员**访问 `http://<域名>/admin` 登录（仅 super 账号可登录）；**普通用户**访问 `http://<域名>/user` 登录（`USER_PATH` 可改，注意不要与 `/g` `/hf` `/offline` `/v2` `/ai` `/mcp` `/search` `/healthz` 等保留路径冲突，冲突时启动即报错）。

### 🧩 方式二：docker-compose

```bash
ADMIN_SECRET="$(openssl rand -hex 32)" docker compose up -d
```

编排文件见 [`docker-compose.yml`](docker-compose.yml)：默认挂载数据卷 `gm-data`（缓存 + token），按需在 `environment` 中开启 `DOCKER_ENABLED` / `HF_ENABLED` / `RATELIMIT_ENABLED` / `UPSTREAM_SOCKS5`。

### 🛠️ 方式三：本地构建

```bash
go build -o gh-mirror .
./gh-mirror   # 读取 ./config.toml（缺失则用默认值 + 环境变量）
```

> 生产建议置于 nginx + HTTPS 之后，`LISTEN` 保持 `127.0.0.1:8080` 由反代转发。一键部署/安装脚本见文末「部署脚本」。

---

## ⚙️ 配置

优先级：**环境变量 > `config.toml` > 内置默认值**。`config.toml` 缺失时自动退化到默认值 + 环境变量（容器部署可完全用环境变量）。

### 📝 config.toml 样例

```toml
listen = "127.0.0.1:8080"
cache_dir = "./cache"
cache_ttl_minutes = 1440
max_cache_mb = 200
upstream_timeout = 60
use_jsdelivr = false
tokens_file = "./tokens.json"

[admin]
path = "/admin"
user_path = "/user"    # 普通用户入口路径（与超管入口及保留路径冲突会启动报错）
user = "admin"
pass = "admin"
secret = ""            # 会话签名密钥，建议固定
cookie_secure = true   # HTTPS 下保持 true

[auth]
mode = "token"         # token=强制令牌(默认)；open=开放代理(配合 [acl] 使用)

[docker]
enabled = false
upstreams = ["https://registry-1.docker.io"]
cache_ttl_minutes = 1440

[hf]
enabled = false
cache_ttl_minutes = 1440

[ratelimit]
enabled = false
period_seconds = 3600  # 统计周期
quota = 1000           # 每周期每 IP 配额
ipv6_prefix = 64       # IPv6 归并前缀

[acl]
ip_allow = []          # 如 ["10.0.0.0/8"]，非空时仅允许名单内 IP
ip_block = []            # 如 ["1.2.3.4"]
repo_allow = []          # 如 ["library/*","grafana"]，支持通配符
repo_block = []          # 如 ["evil/*"]

[upstream]
socks5 = ""            # 如 "socks5://user:pass@127.0.0.1:1080"
```

### 🌱 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `LISTEN` | `127.0.0.1:8080` | 监听地址 |
| `CACHE_DIR` | `./cache` | 缓存目录 |
| `CACHE_TTL_MINUTES` | `1440` | 缓存有效期（分钟） |
| `MAX_CACHE_MB` | `200` | 单文件超过此大小不缓存 |
| `UPSTREAM_TIMEOUT` | `60` | 回源超时（秒） |
| `USE_JSDELIVER` | `0` | `1` 让 GitHub blob/raw 走 jsDelivr 边缘缓存 |
| `TOKENS_FILE` | `./tokens.json` | Token 持久化文件 |
| `ADMIN_PATH` | `/admin` | 管理后台（超管）路径 |
| `USER_PATH` | `/user` | 普通用户入口路径（与 `ADMIN_PATH` 及 `/g` `/hf` `/offline` `/v2` 等保留路径冲突会启动报错） |
| `ADMIN_USER` | `admin` | 管理员账号 |
| `ADMIN_PASS` | `admin` | 管理员密码 |
| `ADMIN_SECRET` | 随机 | 会话签名密钥（建议固定，重启不致失效） |
| `COOKIE_SECURE` | `true` | 会话 Cookie 是否带 Secure |
| `DOCKER_ENABLED` | `false` | 开启 Docker 镜像加速 |
| `HF_ENABLED` | `false` | 开启 HuggingFace 加速 |
| `RATELIMIT_ENABLED` | `false` | 开启按 IP 限流 |
| `UPSTREAM_SOCKS5` | 空 | SOCKS5 出站代理，如 `socks5://user:pass@host:1080` |

---

## 🧭 各服务用法

<p align="center"><img src="docs/images/home-usage.png" alt="基本使用：三种服务的命令直接复制"></p>

### 🐙 GitHub 资源加速（`/g/<用户名>/<token>/`）

```
https://mirror.example.com/g/<用户名>/<token>/<完整 GitHub 地址>
```

| 场景 | 访问地址 |
|------|----------|
| 仓库文件 | `.../g/<用户名>/<token>/https://github.com/owner/repo/blob/main/path/file.txt` |
| Raw 文件 | `.../g/<用户名>/<token>/https://raw.githubusercontent.com/owner/repo/main/file.txt` |
| Releases | `.../g/<用户名>/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip` |
| 仓库打包 | `.../g/<用户名>/<token>/https://github.com/owner/repo/archive/refs/heads/main.zip` |
| 短链 | `.../g/<用户名>/<token>/gh/owner/repo/raw/main/file.txt` |

```bash
curl -L -o app.zip "https://mirror.example.com/g/<用户名>/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
```

`<用户名>` 为后台登录用户名，`<token>` 必须是该用户自己的 Token（归属校验，无效或错配一律 404）。响应头 `X-Cache: HIT` 命中缓存，`MISS` 回源。

### 🐳 Docker 镜像加速（`/v2/<用户名>[/<自定义路径>]/…`、`/token`）

<p align="center"><img src="docs/images/admin-docker-modal.png" alt="开启 Docker 前的安全确认与自定义路径" width="52%"><br>
<sub>开启前会弹出安全确认：说明风险、并可设置「用户名 + 自定义路径」前缀</sub></p>

开启 `[docker] enabled=true`（或 `DOCKER_ENABLED=true`）后，`/v2` 的拉取路径**固定带用户名段**（管理后台可再填一段自定义路径）：

```bash
# 用户名段 = 后台登录用户名；自定义路径在「服务管理 → Docker 镜像加速」里可选填写
docker pull mirror.example.com/admin/library/nginx:1.25
```

> 因为路径带前缀，`registry-mirrors` **不再适用**（docker daemon 不支持带路径的 mirror）。`docker pull` 命令里**不用手写 `/v2/`**（客户端自动补全），服务端完整路径为 `/v2/<用户名>[/<自定义路径>]/…`。
>
> **鉴权模型**：registry 拉取走 Docker 协议透传，**不校验 Token**——「用户名段 + 自定义路径」本身就是凭证（只有知道路径才能用）；只有 **`/offline` 离线包**才做 `<用户名>/<token>` 双重校验。

多 Registry（GHCR/Quay/GCR/k8s 等）用域名前缀形式（用户名段在最前）：

```
https://mirror.example.com/v2/<用户名>/ghcr.io/<owner>/<repo>/manifests/<tag>
```

### 🤗 HuggingFace 加速（`/hf/<用户名>/<token>/`）

开启 `[hf] enabled=true` 后：

```bash
# token 模式（默认，用户名段 + token）
curl -L -o model.bin "https://mirror.example.com/hf/<用户名>/<token>/<owner>/<repo>/resolve/main/model.bin"
# 开放模式（[auth] mode=open）可省略用户名段与 token
curl -L -o model.bin "https://mirror.example.com/hf/<owner>/<repo>/resolve/main/model.bin"
```

### 🔍 镜像搜索 / 标签浏览

- 页面：`https://mirror.example.com/search?q=nginx`、`https://mirror.example.com/tags?repo=library/nginx`
- JSON：`/api/search?q=nginx`、`/api/tags?repo=library/nginx`

### 📦 离线镜像打包（`/offline/`）

开启 `[docker] enabled=true` 后，按需把镜像打包成 `docker load` 可用的 tar，**无需本地 Docker**：

```bash
curl -L -o nginx.tar "https://mirror.example.com/offline/<用户名>/<token>/library/nginx:1.25"
docker load -i nginx.tar
```

- 自动处理多架构镜像（默认选 `linux/amd64`，回退 `linux/arm64`）。
- 遵守 `[acl]` 仓库黑白名单；同一镜像并发打包会返回 `429`（防抖）。
- 说明：输出为经典 `docker-save` 格式；首次使用请用真实 `docker load` 校验兼容性。

---

## 🎛️ 管理后台

**双入口**：超管入口 `/admin`（仅超级管理员可登录）+ 普通用户入口 `/user`（`USER_PATH` 可配、可隐藏），各自独立会话 Cookie，普通用户登录超管入口会被拒绝。账号密码由 `ADMIN_USER` / `ADMIN_PASS` 环境变量设置（务必修改默认值）；登录后基于签名 Session Cookie（HMAC + HttpOnly + SameSite=Strict）保持会话。

**超管能力**（`/admin`）：服务管理（Docker / HuggingFace 开关 + Docker 自定义路径）、缓存管理（上限 / 时效 / 清除 + 离线包文件管理 + 磁盘满自动清理）、用户管理（创建 / 删除 / 改角色 / 重置密码 + SMTP 与注册开关 + 邀请码管理）、Token 全量管理、MCP 接入。

**普通用户能力**（`/user`）：管理自己的 Token、设置自己的 Docker 自定义路径（Docker 开启时）、MCP 接入；服务未开启时对应入口提示「请联系超级管理员开启」。

后台适配移动端（393px）：抽屉式导航、Token 卡片式列表、点击「编辑」就地展开表单。

---

## 🛡️ 安全说明

- **域名白名单**：仅允许回源到 GitHub/HuggingFace 相关域名（见 `rewrite.go`），不会被当作开放代理滥用（防 SSRF）。
- **Token 鉴权**：默认所有代理请求必须携带后台下发 token，无效返回 404；`tokens.json` 与 `ADMIN_SECRET` 等同于凭证，请妥善保管。
- **访问控制**：`[acl]` 支持 IP（CIDR）与仓库（通配符）黑白名单，入口前置，命中即返回 403。
- **限流**：`[ratelimit]` 按真实客户端 IP 令牌桶，超限返回 429。
- **真实 IP**：从 `X-Forwarded-For`/`X-Real-IP`/`RemoteAddr` 取客户端 IP。**这些头可被伪造，请仅在受信反代之后启用**；直连场景可在反代层覆盖这些头。
- 可进一步在 nginx 层对 `/admin` 增加 IP 限制或 Basic Auth。

---

## 🏭 生产建议

- **反代 + HTTPS**：nginx 反代到 `127.0.0.1:8080`，用 Let's Encrypt 签证书；`COOKIE_SECURE=true`。
- **数据落盘**：`CACHE_DIR` 与 `TOKENS_FILE` 指向持久化卷/目录，避免写入容器 overlay 层。
- **缓存清理**：缓存由后台周期任务**自动失效**——每 10 分钟按 TTL 过期、超总上限、磁盘可用不足（<1 GB）三级策略自动清理，无需额外定时任务；部署脚本另生成 cron 每天清理 7 天前的缓存文件兜底。
- **固定 `ADMIN_SECRET`**：避免重启后会话全部失效。
- **内存**：单二进制常驻内存很低；Docker 模式注意守护进程开销，小内存实例可选原生部署。

---

## 📜 部署脚本

保留原有的一键部署 / 安装 / 卸载脚本（Ubuntu 24.04，含 nginx + TLS + systemd）：

```bash
# 一键部署（上传源码后，root 运行）
sudo bash deploy.sh

# 一键安装（从 Release 拉预编译二进制，无需源码）
DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='你的密码' \
  bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)

# 卸载
bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/uninstall.sh)
```

> 一键脚本（`deploy.sh` / `install.sh` / `uninstall.sh`）与 GHCR 镜像均指向本 fork：`yufeng8/gh-mirror`（镜像 `ghcr.io/yufeng8/gh-mirror`）。

---

## 🗂️ 目录结构

```
main.go        # 入口、配置加载、service group 路由、入口中间件
config/        # config.go（TOML 解析 + env 覆盖）+ config.toml 样例
proxy.go       # GitHub 代理（/g/<用户名>/<token>/）+ 共用 serveTarget/回源/缓存
docker.go      # Docker Registry v2 缓存反向代理
hf.go          # HuggingFace 加速（/hf/<用户名>/<token>/）
search.go      # 镜像搜索/标签 API + 服务端渲染页面
offline.go     # 离线镜像 tar 打包（内容寻址缓存）
offlinecache.go # 离线包缓存数据层（清单 JSON / 任务状态 / 磁盘清理钩子）
clientip.go    # 真实客户端 IP + IPv6 /64 归并
ratelimit.go   # 令牌桶限流
acl.go         # IP/仓库黑白名单
socks5.go      # 最小 SOCKS5 拨号器
cache.go       # 磁盘缓存（TTL / 单文件上限 / 总上限 均可运行时热改 + 自动失效）
store.go       # Token 持久化 + 统计（owner 归属）
users.go       # 用户持久化（users.json，角色 + 邮箱 + Docker 路径）
mail.go        # SMTP 发信 + 邮箱验证码
captcha.go     # 图形验证码（点阵渲染 + 校验）
invite.go      # 邀请码数据层（生成 / 导出 / 有效期 / 使用追踪）
mcp.go         # MCP 服务端（HTTP / stdio，多用户权限边界）
rewrite.go     # 域名白名单 + URL 改写
admin.go/templates.go  # 管理后台（双入口 /admin + /user）
settings.go    # 运行时设置持久化（开关 + 缓存 + SMTP/注册/验证码，gm-data/settings.json）
*_test.go      # 单元测试（缓存 / 用户 / 邀请码 / 验证码 / MCP / 代理用户名段等）
```

## 📚 文档

- `docs/docker-deploy.md`：通用 Docker 部署、环境适配与冲突排查表、**域名前置 CDN（EdgeOne/Cloudflare）正确接法**、**Docker 加速安全**
- `docs/deploy-runbook.md`：目标机实测部署 Runbook（先探测 → 选型 → 执行 → 端到端验证 → 更新/回滚）
- `docs/upgrade-runbook.md`：升级 / 回滚镜像的一键脚本
- `docs/agent-prompt.md`：**给 AI Agent 的部署提示词**（首页「一键交给 AI Agent 部署」直接渲染此文件，随版本更新；可用 `AGENT_PROMPT_FILE` 或 `gm-data/agent-prompt.md` 覆盖）
- `docs/mcp.md`：**MCP 接入指南**（HTTP `POST /ai/<用户名>/mcp` 与 `gh-mirror mcp` stdio 两种接入、工具清单、鉴权、安全建议、故障排查）

## 🙏 致谢 / Acknowledgements

本项目**部分思路参考自 [sky22333/hubproxy](https://github.com/sky22333/hubproxy)（MIT License · © 2025 sky22333）**——一个同样支持 Docker 镜像加速、GitHub 加速与离线镜像的多功能加速服务。

gh-mirror 受其思路启发，以 Go **独立实现**，并扩展了 Web 管理后台、Token 鉴权、磁盘缓存、镜像搜索、链接转换与 MCP 接入等能力。若你的场景更适合原项目，也推荐尝试 hubproxy。

本项目与 hubproxy **无任何关联（not affiliated）**。如有对其代码的复用，均遵循其 MIT 许可并保留相应声明。

### 🔗 生态推荐：[KPanel](https://github.com/kejilion/KPanel)

如果你还想在同一台机器上把「主机运维 + 项目自动化部署」一起管起来，可以看看 **[kejilion/KPanel](https://github.com/kejilion/KPanel)**：

- **服务器 AI Agent**：以自然语言驱动真实主机上的操作，不必在面板、SSH 与命令行之间来回切换。
- **小主机友好**：**1 GB 内存的小主机也能正常运行**，适合低配 VPS、家用小主机与轻量生产环境。
- **自动化部署项目**：把部署流程交给 Agent 执行，减少手工步骤与人为失误。
- **围绕同一台真实主机协同**：系统、网站、Docker、文件、集群与 AI 助手在同一台机器上打通，而不是各管一摊。

和 gh-mirror 的定位刚好互补：gh-mirror 负责让「上游资源访问更快」（内置 MCP，Agent 可直接管理 Token、服务开关与缓存），KPanel 负责「主机与项目本身的运维编排」。两者都主张让 AI Agent 真正落地到一台真实服务器上干活。

> 仅为生态推荐，与本项目无关联（not affiliated），也不构成任何担保；请自行评估后使用。

## 📄 License

MIT（沿用上游项目协议）。
