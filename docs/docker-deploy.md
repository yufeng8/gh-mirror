# gh-mirror 通用 Docker 部署

放弃特定面板/运维通道，改用标准 Docker Compose 在任意装有 Docker 的主机上部署。
本目录提供：`docker-compose.yml` + 覆盖层 `docker-compose.publish.yml` / `docker-compose.hostcert.yml`
+ `nginx/gh-mirror.compose.conf`（HTTP）+ `nginx/gh-mirror.https.conf`（HTTPS 自申请证书）+
`nginx/gh-mirror.hostcert.conf`（HTTPS 复用宿主机证书）+ `.env.example`。

> 仓库自带 `Dockerfile`（多阶段 → distroless）与 `install.sh MODE=docker`。
> 本方案在其之上补上**可移植、可裁剪的 compose 编排 + 反向代理/TLS 配置**，无需登录特定面板即可部署。

## 设计：一切按宿主环境裁剪
网关核心（`gh-mirror`）始终启动；反向代理、证书来源、对外端口等都做成**可选项/覆盖层**，
由部署方或 AI Agent 按机器实际情况选择，不在 compose 里写死（部署机往往已有 nginx / certbot / KPanel 等）。

### 环境适配与冲突排查表
| 可能的冲突/耦合点 | 本方案的处理 |
|---|---|
| 主机 80/443 已被占用 | 内置 nginx 改为 `profiles: ["proxy"]`，默认不启用、不占端口 |
| 网关对外端口 | 通过覆盖层 `docker-compose.publish.yml` 暴露，默认仅 `127.0.0.1:8080` |
| 宿主机已有 certbot / Let's Encrypt 证书 | 内置 nginx 可复用：覆盖层 `docker-compose.hostcert.yml` + `nginx/gh-mirror.hostcert.conf` |
| 容器名 `gh-mirror` / `gh-mirror-nginx` 撞现有容器 | `GH_MIRROR_CONTAINER_NAME` / `NGINX_CONTAINER_NAME` 可改 |
| 纯 HTTP（无 TLS）后台登录失败 | `COOKIE_SECURE` 可配，HTTP 部署设为 `false` |
| 已有 `install.sh MODE=docker` 部署（host 网络） | 两者端口/容器名不同，避免同时运行；或改容器名错开 |
| 构建需拉取 Docker Hub 基础镜像 | 出网受限时可改用预构建镜像 `ghcr.io/yufeng8/gh-mirror:latest`（见下） |
| 域名前置 CDN（EdgeOne / Cloudflare） | 见下方「域名前置 CDN 的正确接法」：让 CDN 持证书、源站只做 80 反代，并关 CDN 缓存/URL 规范化 |

### 三种启用方式（按环境选一）
```bash
# 场景 A：机器已有 nginx / KPanel —— 只跑网关，暴露给既有反代转发
docker compose -f docker-compose.yml -f docker-compose.publish.yml up -d --build

# 场景 B1：用内置 nginx 自己申请证书（webroot + 容器内 certbot）
docker compose --profile proxy up -d --build

# 场景 B2：用内置 nginx 但复用宿主机已有证书（宿主机已跑 certbot）
#   先把 nginx/gh-mirror.hostcert.conf 里的 <DOMAIN> 换成实际域名
docker compose -f docker-compose.yml -f docker-compose.hostcert.yml --profile proxy up -d --build
```
> 所有命令都在仓库根目录执行；`--build` 首次需要，之后升级去掉了更快。

### 域名前置 CDN（EdgeOne / Cloudflare）的正确接法
若域名前面挡着 CDN（如腾讯 EdgeOne、Cloudflare），公网 TLS 在 CDN 边缘终止、再由 CDN 回源到源站。此时：
- **让 CDN 持公网证书并终止 TLS，源站只做 80 反代 → `127.0.0.1:8080`，源站不签自己的证书**。在 CDN 后面给源站签 LE，常因回源/IPv6 失败报 **521**，别纠缠（源站 certbot 根本不会被 LE 访问到）。
- CDN 配置：回源=源站公网 IP:80、协议 HTTP、回源 Host 固定为你的域名；**务必关闭缓存与 URL 规范化/重写**（否则会压掉 gh-mirror 的 `/g/<用户名>/<token>/https://` 双斜杠、缓存动态内容 → 404）；不要强制 80 跳转。
- 源站反代段：`merge_slashes off;` + `proxy_pass http://127.0.0.1:8080$request_uri;` + `proxy_set_header X-Forwarded-Proto https;`，且**不要**再加 http→https 跳转（避免回源回环）。
- 确需源站自己持证时，用 **DNS-01** 验证（绕开 80/回源/IPv6）。
- 安全组：源站 80 入站对 CDN 回源/公网开放；443 可不开放。
- 大文件/流式（`/offline/` 产 tar）经 CDN 可能被限速/超时，必要时该路径直连源站或对 CDN 关缓存直通。

## 前置
- 一台 Linux 主机（amd64 / arm64），已装 Docker 与 docker compose。
- 场景 A：无需公网 80/443，由既有反代与证书体系负责对外。
- 场景 B1：域名 A 记录指向主机公网 IP，安全组/防火墙放行 80/443（申请证书需要）。
- 场景 B2：宿主机已有 `/etc/letsencrypt/live/<DOMAIN>/` 证书；仍需放行 80/443 供内置 nginx 监听。

## 快速开始
```bash
git clone https://github.com/yufeng8/gh-mirror.git
cd gh-mirror
cp .env.example .env
# 编辑 .env：设置 ADMIN_PASS 与 ADMIN_SECRET（openssl rand -hex 32）
#   纯 HTTP 部署把 COOKIE_SECURE 改为 false；要开 HuggingFace 代理设 HF_ENABLED=true
# 再按上面“三种启用方式”选一条执行
```
- 默认从本仓库源码构建（使用根目录 `Dockerfile`）。
- 源码构建需要宿主机 Go ≥ 1.24（`go.mod` 要求）。若本机 Go 版本更低，Go 会自动下载对应工具链（需网络）；建议直接用下方预构建镜像，免编译。
- 改用预构建镜像：见下方「快路径：预构建镜像（不编译）」，用 `ghcr.io/yufeng8/gh-mirror:<TAG>`（**含 HF**）。注意 `:latest` 跟踪 `main`、**不含 HF**，别用 `latest`。

## 快路径：预构建镜像（不编译）
镜像：`ghcr.io/yufeng8/gh-mirror:<TAG>`（多架构 amd64/arm64，含 HF/hubproxy，由同名 tag 触发 CI 构建）。

> **`<TAG>` 填什么**：取 [Releases](https://github.com/yufeng8/gh-mirror/releases) 里的**最新正式版**（形如 `vX.Y.Z-hubproxy`），不要写死旧版本号，也不要用 `:latest`。
```bash
docker pull ghcr.io/yufeng8/gh-mirror:<TAG>      # 401 → 包非 Public，去 Packages 设置改 Public
```
- **方式 C1｜最快，无需 clone**（场景 A，背后已有反代）：
  ```bash
  ADMIN_SECRET=$(openssl rand -hex 32); ADMIN_PASS=$(openssl rand -base64 18)
  docker run -d --name gh-mirror --restart unless-stopped \
    -e ADMIN_USER=admin -e ADMIN_PASS="$ADMIN_PASS" -e ADMIN_SECRET="$ADMIN_SECRET" \
    -e LISTEN=0.0.0.0:8080 -e COOKIE_SECURE=false -p 127.0.0.1:8080:8080 \
    -v gm-cache:/var/cache/gh-mirror -v gm-data:/var/lib/gh-mirror \
    ghcr.io/yufeng8/gh-mirror:<TAG>
  # 要开 HF 加 -e HF_ENABLED=true；随后让既有反代转发到 127.0.0.1:8080
  ```
- **方式 C2｜compose + 预构建镜像**：clone 后，注释 `build: .` 并把 `image:` 指向 `v0.1.0`，再按场景执行（去掉 `--build`）：
  ```bash
  sed -i 's/^    build: \./    # build: ./' docker-compose.yml
  sed -i 's#^    # image: ghcr.io/yufeng8/gh-mirror:latest#    image: ghcr.io/yufeng8/gh-mirror:<TAG>#' docker-compose.yml
  docker compose -f docker-compose.yml -f docker-compose.publish.yml up -d   # 场景 A
  # docker compose --profile proxy up -d                                     # 场景 B
  ```

## 反向代理与 TLS
- **场景 A（既有反代）**：把域名转发到 `127.0.0.1:8080`（或你设的 `GM_BIND_ADDR:GM_HTTP_PORT`），
  转发写法参考 `nginx/gh-mirror.compose.conf` 的 `location /`：`merge_slashes off` + `proxy_pass ...$request_uri` + 透传 `Host`/`X-Forwarded-*`。
  TLS 与证书续期完全交给宿主机现有体系。
- **场景 B1（内置 nginx，自申请证书）**：
  ```bash
  DOMAIN=git.yufeng.site
  # 1) 把 nginx/gh-mirror.https.conf 中的 <DOMAIN> 替换为实际域名
  # 2) 编辑 docker-compose.yml，将 nginx 挂载改为：
  #      ./nginx/gh-mirror.https.conf:/etc/nginx/conf.d/default.conf:ro
  # 3) 申请证书（gm-certs 卷对 nginx 可写，certbot 可直接写）
  docker compose --profile proxy run --rm --entrypoint "" nginx \
    certbot certonly --webroot -w /var/www/certbot -d $DOMAIN
  # 4) 重新加载
  docker compose --profile proxy up -d nginx
  ```
- **场景 B2（内置 nginx，复用宿主机证书）**：用 `docker-compose.hostcert.yml` 覆盖层 +
  `nginx/gh-mirror.hostcert.conf`（已含 80→443 跳转）。证书由宿主机 certbot 续期，容器只负责服务。

## 验证
```bash
# 场景 A：
curl -fsS http://127.0.0.1:8080/healthz      # 或经既有反代访问域名
# 场景 B1/B2：
curl -fsS http://<主机IP>/healthz            # 应返回 ok
curl -fsS https://<域名>/healthz             # B1/B2 配好证书后
```
后台：`http(s)://<域名或IP>/admin`（账号见 `.env`，首次需登录添加 token，无 token 的请求返回 404）。

## 常用运维
| 项 | 命令 |
|----|------|
| 日志 | `docker logs -f gh-mirror` |
| 重启 | `docker compose restart`（叠加/带 profile 时把同样的 `-f .../--profile proxy` 带上） |
| 升级 | 改 `image` 标签或重新 `build`，再执行对应启用命令 |
| 缓存清理 | 命名卷 `gm-cache`；可加 cron `docker run --rm -v gm-cache:/cache busybox find /cache -mtime +7 -delete` |
| 卸载 | `docker compose down`（带同样的 `-f .../--profile proxy`）；数据在 `gm-data` / `gm-cache` 卷，删卷即清空 |

> 提示：用了覆盖层/profile 后，`down`/`restart` 等命令要**带上与 `up` 时相同的 `-f` 和 `--profile` 参数**，
> 否则 compose 找不到对应服务定义。

## Docker 加速安全（重要，与 DOCKER_ENABLED 相关）
Docker 加速有两个端点，鉴权方式不同：

| 端点 | 用途 | Token 鉴权 |
|------|------|-----------|
| `/v2/<用户名>[/<自定义路径>]/…` | registry 加速（带路径 pull-through） | **不支持 Token**：Docker Registry 协议无法携带我们的 `/g/<用户名>/<token>/`；改用**强制路径前缀**（用户名段必带 + 可选自定义路径）作为保护位 |
| `/offline/<用户名>/<token>/<镜像名>` | 离线 tar（`docker load` 可用） | **必须带用户名段与 Token**：用户名不存在、token 缺失或不匹配一律返回 404 |

> `/v2` 的路径前缀**固定为 `<用户名>[/<自定义路径>]`**：用户名段取 `ADMIN_USER`（小写化），不可省略；自定义路径在管理后台可填。`GET /v2/` 探测端点会放行（由上游返回 401 挑战），否则 docker 客户端会判定该 registry 不支持 v2。

**风险**：`DOCKER_ENABLED=true` 且网关公网可达时，知道完整路径的人仍可使用你的 `/v2` 镜像加速（路径属"混淆"而非鉴权），耗费你的上游带宽与配额。离线包 `/offline` 已强制 token，不会被人白嫖。

**为什么 CDN 下 IP ACL 靠不住**：源站只看到 CDN 回源 IP；`ClientIP` 取 `X-Forwarded-For` 首段**可被伪造**，取 `RemoteAddr` 又是 CDN＝放行所有人。所以 `[acl] ip_allow` 在 CDN 后**无法按真实客户端 IP 限制**。

**降低风险（任选，按场景）：**
1. **不需要公网 Docker 加速** → `DOCKER_ENABLED=false`（最省心；GitHub/HF/搜索仍 token 保护，可正常公网）。
2. **开放当公共 mirror + 边缘防滥用** → registry-mirror 用公网 `https://<域名>`；在 CDN（EdgeOne 看得到真实客户端 IP）对 `/v2` 配**速率限制、并发/带宽封顶、IP 黑名单、地域限制**；并盯 `gm-cache` 体积与上游出网账单。
3. **远程机器有固定公网 IP（推荐生产自用）** → 用**不走 CDN 的难猜子域名**（如 `m7f3k9mirror.<域名>`，A 记录直连源站），源站 nginx 给该域名反代到 `gh-mirror:8080`（`merge_slashes off` + `proxy_pass ...$request_uri`），云**安全组只放行那几台机器公网 IP** 访问源站 80；registry-mirror 指向该子域名。
4. **给 /v2 加「用户名 + 自定义路径」前缀（推荐，管理后台可配）** → `/v2` 的路径**固定为 `<用户名>[/<自定义路径>]`**：用户名段取 `ADMIN_USER`（自动小写），**强制带上、不可省略**；自定义路径在后台开启 Docker 的弹窗里可选填写（如 `team-a/9f3k2`）：
   ```bash
   # 只用用户名段
   docker pull <域名>/<用户名>/library/nginx:1.25
   # 用户名段 + 自定义路径
   docker pull <域名>/<用户名>/team-a/9f3k2/library/nginx:1.25
   ```
   未带前缀或前缀不匹配一律 404（不暴露端点是否存在）。**注意**：路径带前缀后 `registry-mirrors` 不再生效（docker daemon 不支持带路径的 mirror），必须改用上面的 pull 方式；自定义路径可含 `/`、最长 64 字符，仅支持字母、数字、`-`、`_`、`/`，可随时在后台修改。用户名段的存在让每个用户天然拥有独立路径，便于后续多用户扩展。
5. **离线包只给可信用户** → `/offline` 已强制用户名段与 token（`/offline/<用户名>/<token>/<镜像名>`，token 必须归属该用户名）：为不同使用者签发不同 token，可随时在后台删除以吊销；需要更细的控制再叠加反代 IP 白名单或 CDN 速率限制。
   ```bash
   # 离线包示例（token 来自管理后台「Token 管理」）
   curl -L -o nginx.tar "https://<域名>/offline/<用户名>/<token>/library/nginx:1.25"
   docker load -i nginx.tar
   ```

> `/offline` 的 token 就是普通的代理 token，泄露后可在后台删除轮换；`/v2` 因协议限制无法带 token，只能靠开关、网络边界与边缘限速控制。开启 `DOCKER_ENABLED` 时，**管理后台会弹出安全确认窗**（说明风险与降险方案），确认后才真正开启。

## 已知坑（来自项目大脑 knowledge/）
- **双斜杠**：代理路径 `/g/<用户名>/<token>/https://...` 的 `//` 必须保留，nginx 需 `merge_slashes off` + `proxy_pass ...$request_uri`，否则解析失败 404。
- **端口冲突**：场景 B 的内置 nginx 会占用主机 80/443。若部署机已有其它 Web 服务，请改用场景 A（不启用 proxy profile）。
- **域名前置 CDN（EdgeOne/Cloudflare）**：会压掉 `/g/<用户名>/<token>/https://` 的双斜杠、缓存动态内容致 404，并让源站 LE 报 521。按上面「域名前置 CDN 的正确接法」处理：CDN 持证书 + 源站只做 80 反代 + 关 CDN 缓存/URL 规范化。
- **`/v2` 无 Token 鉴权**：registry-mirror 端点因协议限制无法带 token，CDN 后 `ip_allow` 也挡不住真实客户端。详见「Docker 加速安全」：按需 `DOCKER_ENABLED=false` / 边缘限速带宽封顶 / 难猜子域名+安全组。**`/offline` 离线包已强制用户名段与 token**（`/offline/<用户名>/<token>/<镜像>`）。
- **HTTP 后台登录失败**：纯 HTTP 部署若忘把 `COOKIE_SECURE=false`，浏览器不会下发 Secure cookie，表现为登录无反应/反复跳登录。
- **GHCR 可见性**：`ghcr.io/yufeng8/gh-mirror` 包须设为 **Public**，否则匿名 `docker pull` 报 401；私有则需在 `gh-mirror` 服务加 `docker login ghcr.io`（带 `read:packages` 的令牌）。
- **缓存持久化**：`gm-cache` / `gm-data` 为命名卷，重建容器不会丢缓存与 token。
- **模板内嵌**：页面模板由 Go `go:embed` 编译进二进制，运行时无需挂载 `templates/` 目录。
- **HF 默认关**：二进制含 HF 代码，但 `HF_ENABLED` 未设时默认关闭；要开放 HuggingFace 代理就写进 `.env`。
