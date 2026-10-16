# github-mirror — 自托管多服务加速网关（Docker / GitHub / HuggingFace）

[![build](https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml/badge.svg)](https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml)
[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#license)

> 一个单二进制、完全自托管的多服务加速网关：**Docker 镜像**、**GitHub 资源**、**HuggingFace 模型**三类加速一站搞定，
> 内置 Token 鉴权、访问控制（IP/仓库黑白名单）、智能限流、SOCKS5 出站与镜像搜索。
> 文档示例域名为 `mirror.example.com`，请替换为你自己的域名。

设计参考了 [sky22333/hubproxy](https://github.com/sky22333/hubproxy) 的多服务架构，并以 Go **标准库**重写、无任何第三方依赖，便于审计与离线构建。

---

## 特性

- **Docker 镜像加速**：兼容 Registry API v2，内置 Docker Hub / GHCR / Quay / GCR / registry.k8s.io 多源；Manifest/Token 内存缓存、Layer 写透磁盘缓存、流式传输、`Range` 断点续传。
- **GitHub 资源加速**：`/g/<token>/` + 完整 GitHub 地址，覆盖 blob/raw、archive、releases、API，自带磁盘缓存。
- **HuggingFace 加速**：`/hf/<token>/<owner>/<repo>/resolve/<rev>/<file>`，自动跟随 302 到 CDN，模型/LFS 流式 + 缓存。
- **离线镜像 tar**：`/offline/<image>` 无需本地 Docker，把镜像打包成 `docker load` 可用的 tar（流式 + 同镜像并发防抖）。
- **镜像搜索**：Docker Hub 搜索 / 标签浏览，服务端渲染轻量页面（`/search`、`/tags`）与 JSON API（`/api/search`、`/api/tags`）。
- **安全管控**：Token 鉴权（默认）+ 可选开放模式；IP（CIDR）/ 仓库（通配符）黑白名单；按真实客户端 IP 的令牌桶限流（IPv6 归并 `/64`）。
- **统一配置**：`config.toml` + 环境变量覆盖，平滑兼容旧的环境变量部署。
- **可选 SOCKS5 出站**：为 GitHub/HF/Docker 回源统一走 SOCKS5 代理。

---

## 快速开始

### 方式一：Docker（推荐）

```bash
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e LISTEN=0.0.0.0:8080 \
  -e CACHE_DIR=/data/cache \
  -e TOKENS_FILE=/data/tokens.json \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='change-me' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e DOCKER_ENABLED=true \
  -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:latest
```

### 方式二：docker-compose

```bash
ADMIN_SECRET="$(openssl rand -hex 32)" docker compose up -d
```

编排文件见 [`docker-compose.yml`](docker-compose.yml)：默认挂载数据卷 `gm-data`（缓存 + token），按需在 `environment` 中开启 `DOCKER_ENABLED` / `HF_ENABLED` / `RATELIMIT_ENABLED` / `UPSTREAM_SOCKS5`。

### 方式三：本地构建

```bash
go build -o gh-mirror .
./gh-mirror   # 读取 ./config.toml（缺失则用默认值 + 环境变量）
```

> 生产建议置于 nginx + HTTPS 之后，`LISTEN` 保持 `127.0.0.1:8080` 由反代转发。一键部署/安装脚本见文末「部署脚本」。

---

## 配置

优先级：**环境变量 > `config.toml` > 内置默认值**。`config.toml` 缺失时自动退化到默认值 + 环境变量（容器部署可完全用环境变量）。

### config.toml 样例

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

### 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `LISTEN` | `127.0.0.1:8080` | 监听地址 |
| `CACHE_DIR` | `./cache` | 缓存目录 |
| `CACHE_TTL_MINUTES` | `1440` | 缓存有效期（分钟） |
| `MAX_CACHE_MB` | `200` | 单文件超过此大小不缓存 |
| `UPSTREAM_TIMEOUT` | `60` | 回源超时（秒） |
| `USE_JSDELIVER` | `0` | `1` 让 GitHub blob/raw 走 jsDelivr 边缘缓存 |
| `TOKENS_FILE` | `./tokens.json` | Token 持久化文件 |
| `ADMIN_PATH` | `/admin` | 管理后台路径 |
| `ADMIN_USER` | `admin` | 管理员账号 |
| `ADMIN_PASS` | `admin` | 管理员密码 |
| `ADMIN_SECRET` | 随机 | 会话签名密钥（建议固定，重启不致失效） |
| `COOKIE_SECURE` | `true` | 会话 Cookie 是否带 Secure |
| `DOCKER_ENABLED` | `false` | 开启 Docker 镜像加速 |
| `HF_ENABLED` | `false` | 开启 HuggingFace 加速 |
| `RATELIMIT_ENABLED` | `false` | 开启按 IP 限流 |
| `UPSTREAM_SOCKS5` | 空 | SOCKS5 出站代理，如 `socks5://user:pass@host:1080` |

---

## 各服务用法

### GitHub 资源加速（`/g/<token>/`）

```
https://mirror.example.com/g/<token>/<完整 GitHub 地址>
```

| 场景 | 访问地址 |
|------|----------|
| 仓库文件 | `.../g/<token>/https://github.com/owner/repo/blob/main/path/file.txt` |
| Raw 文件 | `.../g/<token>/https://raw.githubusercontent.com/owner/repo/main/file.txt` |
| Releases | `.../g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip` |
| 仓库打包 | `.../g/<token>/https://github.com/owner/repo/archive/refs/heads/main.zip` |
| 短链 | `.../g/<token>/gh/owner/repo/raw/main/file.txt` |

```bash
curl -L -o app.zip "https://mirror.example.com/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
```

响应头 `X-Cache: HIT` 命中缓存，`MISS` 回源。

### Docker 镜像加速（`/v2/`、`/token`）

开启 `[docker] enabled=true`（或 `DOCKER_ENABLED=true`）后，在 Docker 守护进程配置镜像加速器：

```json
// /etc/docker/daemon.json
{ "registry-mirrors": ["https://mirror.example.com"] }
```

```bash
sudo systemctl restart docker
docker pull nginx:latest   # 自动走加速器
```

多 Registry（GHCR/Quay/GCR/k8s 等）用域名前缀形式：

```
https://mirror.example.com/v2/ghcr.io/<owner>/<repo>/manifests/<tag>
```

### HuggingFace 加速（`/hf/`）

开启 `[hf] enabled=true` 后：

```bash
# token 模式（默认）
curl -L -o model.bin "https://mirror.example.com/hf/<token>/<owner>/<repo>/resolve/main/model.bin"
# 开放模式（[auth] mode=open）可省略 <token>
curl -L -o model.bin "https://mirror.example.com/hf/<owner>/<repo>/resolve/main/model.bin"
```

### 镜像搜索 / 标签浏览

- 页面：`https://mirror.example.com/search?q=nginx`、`https://mirror.example.com/tags?repo=library/nginx`
- JSON：`/api/search?q=nginx`、`/api/tags?repo=library/nginx`

### 离线镜像打包（`/offline/`）

开启 `[docker] enabled=true` 后，按需把镜像打包成 `docker load` 可用的 tar，**无需本地 Docker**：

```bash
curl -L -o nginx.tar "https://mirror.example.com/offline/library/nginx:1.25"
docker load -i nginx.tar
```

- 自动处理多架构镜像（默认选 `linux/amd64`，回退 `linux/arm64`）。
- 遵守 `[acl]` 仓库黑白名单；同一镜像并发打包会返回 `429`（防抖）。
- 说明：输出为经典 `docker-save` 格式；首次使用请用真实 `docker load` 校验兼容性。

---

## 管理后台

- 默认路径 `/admin`，默认账号/密码 `admin`/`admin`（务必修改）。
- 登录后基于签名 Session Cookie（HMAC + HttpOnly + SameSite=Strict）保持会话。
- 能力：查看 / 新增 / 删除 Token，页面直接给出本实例代理示例地址。

---

## 安全说明

- **域名白名单**：仅允许回源到 GitHub/HuggingFace 相关域名（见 `rewrite.go`），不会被当作开放代理滥用（防 SSRF）。
- **Token 鉴权**：默认所有代理请求必须携带后台下发 token，无效返回 404；`tokens.json` 与 `ADMIN_SECRET` 等同于凭证，请妥善保管。
- **访问控制**：`[acl]` 支持 IP（CIDR）与仓库（通配符）黑白名单，入口前置，命中即返回 403。
- **限流**：`[ratelimit]` 按真实客户端 IP 令牌桶，超限返回 429。
- **真实 IP**：从 `X-Forwarded-For`/`X-Real-IP`/`RemoteAddr` 取客户端 IP。**这些头可被伪造，请仅在受信反代之后启用**；直连场景可在反代层覆盖这些头。
- 可进一步在 nginx 层对 `/admin` 增加 IP 限制或 Basic Auth。

---

## 生产建议

- **反代 + HTTPS**：nginx 反代到 `127.0.0.1:8080`，用 Let's Encrypt 签证书；`COOKIE_SECURE=true`。
- **数据落盘**：`CACHE_DIR` 与 `TOKENS_FILE` 指向持久化卷/目录，避免写入容器 overlay 层。
- **缓存清理**：缓存按 TTL 过期但不自动删除，建议配置定时任务清理（见「部署脚本」自动生成的 `/etc/cron.d/github-mirror`）。
- **固定 `ADMIN_SECRET`**：避免重启后会话全部失效。
- **内存**：单二进制常驻内存很低；Docker 模式注意守护进程开销，小内存实例可选原生部署。

---

## 部署脚本

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

## 目录结构

```
main.go        # 入口、配置加载、service group 路由、入口中间件
config/        # config.go（TOML 解析 + env 覆盖）+ config.toml 样例
proxy.go       # GitHub 代理 + 共用 serveTarget/回源/缓存
docker.go      # Docker Registry v2 缓存反向代理
hf.go          # HuggingFace 加速
search.go      # 镜像搜索/标签 API + 服务端渲染页面
offline.go     # 离线镜像 tar 打包
clientip.go    # 真实客户端 IP + IPv6 /64 归并
ratelimit.go   # 令牌桶限流
acl.go         # IP/仓库黑白名单
socks5.go      # 最小 SOCKS5 拨号器
cache.go       # 磁盘缓存
store.go       # Token 持久化 + 统计
rewrite.go     # 域名白名单 + URL 改写
admin.go/templates.go  # 管理后台
```

## License

MIT（沿用上游项目协议）。
