# github-mirror — GitHub 资源镜像加速服务（带 Token 鉴权与管理后台）

[![Release](https://img.shields.io/github/v/release/zzidcn/github-mirror)](https://github.com/zzidcn/github-mirror/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fzzidcn%2Fgithub-mirror-blue?logo=docker&logoColor=white)](https://github.com/zzidcn/github-mirror/pkgs/container/github-mirror)
[![docker workflow](https://github.com/zzidcn/github-mirror/actions/workflows/docker.yml/badge.svg)](https://github.com/zzidcn/github-mirror/actions/workflows/docker.yml)
[![build](https://github.com/zzidcn/github-mirror/actions/workflows/build.yml/badge.svg)](https://github.com/zzidcn/github-mirror/actions/workflows/build.yml)

> 本项目是一个 GitHub 资源镜像加速服务的开源实现（文档示例部署域名为 `mirror.example.com`，请替换为你自己的域名）。

在域名后拼接 **`/g/<token>/` + 完整 GitHub 地址** 即可加速下载，浏览器与命令行（curl/wget）通用。
后端用 Go 标准库实现的流式反向代理，自带磁盘缓存与 `Range` 断点续传，并内置 Web 管理后台与 Token 鉴权。

> 适用前提：服务器需能顺畅访问 GitHub。
> 若服务器在国内且直连 GitHub 很慢，纯自建代理无意义；此时只能依赖 jsDelivr 等上游（功能受限）。

## 访问规则（核心变更）

所有代理请求都**必须携带有效 token**，格式为：

```
https://mirror.example.com/g/<token>/<完整 GitHub 地址>
```

| 场景 | 访问地址 |
|------|----------|
| 仓库文件（blob/raw 页面） | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/blob/main/path/file.txt` |
| Raw 文件 | `https://mirror.example.com/g/<token>/https://raw.githubusercontent.com/owner/repo/main/file.txt` |
| Releases 二进制 | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip` |
| 仓库打包 | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/archive/refs/heads/main.zip` |
| 短链 | `https://mirror.example.com/g/<token>/gh/owner/repo/raw/main/file.txt` |

**未携带有效 token（或 token 不在后台）的请求一律返回 404。** 之前的「直接拼接 GitHub 地址」写法已不再可用。

命令行：
```bash
curl -L -o app.zip "https://mirror.example.com/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
```

响应头 `X-Cache: HIT` 表示命中本地缓存，`MISS` 表示回源。

## 管理后台

- 默认路径 `/admin`（可用环境变量 `ADMIN_PATH` 自定义）。
- 默认账号 / 密码：`admin` / `admin`（部署时可设置，密钥与凭据写入 `/etc/github-mirror.env`）。
- 登录后凭**签名 Session Cookie**（HMAC 校验 + 7 天过期）保持会话。
- 后台能力：
  - 查看 / 新增 / 删除 Token（新增时可留空 token 自动生成随机值，并填写备注）。
  - 页面直接给出该实例的代理示例地址。

首页（`/`）为公开展示代理使用说明，不暴露后台路径以外的内容。

## 关于 jsDelivr `gh`

`cdn.jsdelivr.net/gh/用户/仓库@分支/路径` 能加速，但**仅限公开仓库源码文件**、单文件约 20MB、
且吃不下任意 GitHub 地址（releases / archive / API 不行）。本项目默认直连回源覆盖全场景；
如需用 jsDelivr 边缘缓存省服务器带宽，可设 `USE_JSDELIVR=1`（仅对 blob/raw 生效）。

## 本地构建

```bash
# 在 Windows 上交叉编译到 Linux（目标架构如 arm64 / amd64）
set GOOS=linux
set GOARCH=arm64
go build -o github-mirror .
```

## 一键部署（Ubuntu 24.04）

把整个项目目录上传到服务器，然后以 root 运行：

```bash
sudo bash deploy.sh
```

脚本会交互询问**域名**、**证书邮箱**、**部署方式**，以及**管理员密码**（默认 `admin`），并自动生成会话密钥；随后自动完成安装。

- `native`（默认）：安装 Go → 编译 → 建用户/目录（含 token 数据目录）→ 注册 systemd 服务 → 配置 nginx → 防火墙 → 申请证书。
- `docker`：安装 Docker → 构建镜像（按架构）→ 以 `--network=host` + 缓存卷 + 数据卷启动容器 → 配置 nginx → 防火墙 → 申请证书。

> 容器模式用 host 网络规避网桥 NAT 开销、缓存与 token 数据挂卷避免写入 overlay 层，性能与原生基本持平。
> 资源受限的小内存实例（如 1GB）建议选 `native`（省去 Docker 守护进程约 100–200MB 内存）。

前置要求：域名 A 记录已指向本机公网 IP，且 80/443 已放行。

## 一键安装（从 GitHub Release，无需源码）

适合全新服务器、不想上传项目源码的场景：脚本自动连 GitHub 拉取最新预编译二进制并完成服务与反代配置。

```bash
# 交互式（会询问域名 / 邮箱 / 密码）
bash <(curl -fsSL https://raw.githubusercontent.com/zzidcn/github-mirror/main/install.sh)

# 非交互 / 一行搞定
DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='你的密码' \
  bash <(curl -fsSL https://raw.githubusercontent.com/zzidcn/github-mirror/main/install.sh)
```

可选环境变量：

| 变量 | 说明 |
|------|------|
| `MODE` | `native`(默认) / `docker` |
| `VERSION` | 原生模式指定版本标签（如 `v1.0.0`），默认取最新；Docker 模式指定镜像标签（默认 `latest`），`DOCKER_BUILD=1` 时则为源码分支/标签（默认 `main`） |
| `DOCKER_BUILD` | 仅 Docker 模式：设为 `1` 时从 GitHub 源码现场构建镜像（而非拉取 GHCR 现成镜像） |
| `GITHUB_TOKEN` | **私有仓库必填**：具备 `repo`(读) + `read:packages` 权限的 PAT，用于拉取脚本/Release、登录 GHCR 拉取私有镜像 |
| `GHCR_USER` | GHCR 登录用户名（默认 `zzidcn`），填与 `GITHUB_TOKEN` 对应账号一致即可 |
| `GH_PROXY` | 下载加速前缀（如 `https://ghproxy.com/`），国内服务器可用 |
| `SKIP_NGINX=1` | 只装服务、不配置 nginx/TLS（自行反代时） |
| `ADMIN_USER` / `ADMIN_PATH` | 自定义后台账号 / 路径 |

脚本流程（原生模式）：检测架构 → 下载 `github-mirror-linux-<arch>` → 建用户/目录 → 写 `/etc/github-mirror.env` → 注册 systemd 服务 →（默认）装 nginx + 申请证书 + 放通防火墙 → 配置缓存定时清理 → 自检。

> **私有仓库**：公开仓库可直接用上面的 `curl ... | bash` 一行安装；若仓库为私有，`raw.githubusercontent.com` 与 Release 都需要鉴权，请改用以下带令牌的方式（令牌会留在服务器上，请确保 PAT 仅授予必要权限）：
>
> ```bash
> GITHUB_TOKEN=ghp_xxx bash -c "$(curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
>   https://raw.githubusercontent.com/zzidcn/github-mirror/main/install.sh)"
> ```
>
> 该命令用令牌拉取脚本并以相同令牌运行；脚本内部会：带 `Authorization` 头查询/下载 Release（原生模式）、用 `docker login ghcr.io` 登录拉取私有镜像（Docker 模式）。
>
> **PAT 所需权限（scope）**：
> - `repo`（内容读取）：私有仓库下脚本拉取、`gh_curl` 查询/下载 Release，以及 `DOCKER_BUILD=1` 源码克隆都需要它。
> - `read:packages`：登录 GHCR 拉取**私有**镜像必需（公开镜像可匿名拉取，无需此项）。
>
> **脚本会做权限自检**：运行时会调用 GitHub API 读取令牌的 `X-OAuth-Scopes`，提前提示是否缺少上述 scope；若 `docker login` 失败，会明确点出「最可能缺 `read:packages`」并打印原始错误，而不是笼统报 401。权限不足时请重新生成 PAT 勾选对应 scope 后重跑。

### Docker 模式

若服务器想用 Docker，加 `MODE=docker` 即可（脚本会自动安装 Docker、从 GHCR 拉取现成镜像运行容器，仍由 nginx 反代；私有仓库记得带 `GITHUB_TOKEN`）：

```bash
MODE=docker DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='你的密码' \
  bash <(curl -fsSL https://raw.githubusercontent.com/zzidcn/github-mirror/main/install.sh)
```

### 卸载

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/zzidcn/github-mirror/main/uninstall.sh)
# 连数据(token/缓存/用户)一起删: 加 --purge；跳过确认: 加 -y
```

### Docker 镜像（GHCR 自动构建）

仓库已配置 GitHub Actions（`.github/workflows/docker.yml`）：推送 `main` 自动打 `latest`，打 `v*` 标签（如 `v1.0.0`）自动打同名标签，并推送**多架构镜像（amd64/arm64）**到 GitHub Container Registry：

```
ghcr.io/zzidcn/github-mirror:latest
ghcr.io/zzidcn/github-mirror:v1.0.0
```

`install.sh` 的 `MODE=docker` **默认直接拉取该镜像**，无需在服务器上编译源码：

```bash
MODE=docker DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='xxx' \
  bash <(curl -fsSL https://raw.githubusercontent.com/zzidcn/github-mirror/main/install.sh)
# 指定版本: MODE=docker VERSION=v1.0.0 ...
# 改用源码现场构建(兜底): MODE=docker DOCKER_BUILD=1 ...
```

> 注意：GHCR 镜像默认随仓库**私有**，匿名服务器 `docker pull` 会 401。需把该 Package 设为 **Public**：
> 仓库页面 → Packages → 对应包 → Package settings → Change visibility → Public；
> 或用 CLI：`gh api --method PATCH "user/packages/container/github-mirror/visibility" -f visibility=public`。

## 手动部署（Ubuntu 24.04）

1. **域名解析**：`mirror.example.com` A 记录指向服务器公网 IP；安全组放行 80/443。

2. **准备目录与用户**：
   ```bash
   sudo useradd -r -s /usr/sbin/nologin ghmirror
   sudo mkdir -p /opt/github-mirror /var/cache/github-mirror /var/lib/github-mirror
   sudo chown -R ghmirror:ghmirror /var/cache/github-mirror /var/lib/github-mirror
   ```

3. **上传二进制**：把 `github-mirror` 传到 `/opt/github-mirror/github-mirror` 并 `chmod +x`，属主 `ghmirror`。

4. **写入配置**（`/etc/github-mirror.env`，权限 600）：
   ```bash
   sudo tee /etc/github-mirror.env >/dev/null <<'EOF'
   ADMIN_USER=admin
   ADMIN_PASS=admin
   ADMIN_PATH=/admin
   ADMIN_SECRET=$(openssl rand -hex 32)
   COOKIE_SECURE=true
   TOKENS_FILE=/var/lib/github-mirror/tokens.json
   LISTEN=127.0.0.1:8080
   CACHE_DIR=/var/cache/github-mirror
   CACHE_TTL_MINUTES=1440
   MAX_CACHE_MB=200
   UPSTREAM_TIMEOUT=60
   EOF
   sudo chmod 600 /etc/github-mirror.env
   ```

5. **注册服务**：
   ```bash
   sudo cp github-mirror.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now github-mirror
   sudo systemctl status github-mirror
   ```

6. **nginx**：
   ```bash
   sudo cp nginx/github-mirror.conf /etc/nginx/sites-available/
   sudo ln -s /etc/nginx/sites-available/github-mirror.conf /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

7. **HTTPS（Let's Encrypt）**：
   ```bash
   sudo apt install -y certbot python3-certbot-nginx
   sudo certbot --nginx -d mirror.example.com
   ```

8. **登录后台添加 Token**：访问 `https://mirror.example.com/admin`，用 admin/admin 登录后新增 token，之后即可用 `/g/<token>/...` 访问。

## 缓存清理

缓存按 TTL（默认 1 天）过期，但文件不会自动删除。`deploy.sh` 会在部署时**自动配置**定时清理：

- 原生模式：写入 `/etc/cron.d/github-mirror`，每天 04:10 删除 `${CACHE_DIR}` 中 7 天前的缓存文件，以及 1 天前的 `.tmp` 残留。
- 容器模式：同样写入 `/etc/cron.d/github-mirror`，每天 04:10 启动一次性 `busybox` 容器挂载命名卷 `github-mirror-cache` 清理 7 天前的缓存。

如需手动调整，直接编辑 `/etc/cron.d/github-mirror` 即可。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `LISTEN` | `127.0.0.1:8080` | 监听地址（仅本机，由 nginx 反代） |
| `CACHE_DIR` | `./cache` | 缓存目录 |
| `CACHE_TTL_MINUTES` | `1440` | 缓存有效期（分钟） |
| `MAX_CACHE_MB` | `200` | 单文件超过此大小不缓存 |
| `UPSTREAM_TIMEOUT` | `60` | 回源超时（秒） |
| `USE_JSDELIVR` | `0` | 设为 `1` 让 blob/raw 走 jsDelivr 边缘缓存 |
| `ADMIN_PATH` | `/admin` | 管理后台路径（可自定义，必须以 `/` 开头） |
| `ADMIN_USER` | `admin` | 管理员账号 |
| `ADMIN_PASS` | `admin` | 管理员密码 |
| `ADMIN_SECRET` | 随机 | 会话签名密钥（建议通过环境变量固定，重启不致失效） |
| `COOKIE_SECURE` | `true` | 会话 Cookie 是否带 Secure 标记（HTTPS 下保持 true） |
| `TOKENS_FILE` | `./tokens.json` | Token 持久化文件路径 |

## 安全说明

- 仅允许回源到 GitHub 相关域名（白名单见 `rewrite.go`），不会被当作开放代理滥用。
- 代理请求必须携带后台下发的 token，无效 token 返回 404；**Token 列表等同于访问凭证，请妥善保管 `tokens.json` 与 `ADMIN_SECRET`**。
- 管理后台登录使用恒定时间比较，Session Cookie 经 HMAC 签名且 HttpOnly + SameSite=Strict。
- 如需进一步加固，可在 nginx 层对 `/admin` 增加访问限制或 Basic Auth，并定期更换管理员密码。
