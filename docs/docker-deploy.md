# gh-mirror 通用 Docker 部署

放弃特定面板/运维通道，改用标准 Docker Compose 在任意装有 Docker 的主机上一键部署。
本目录提供：`docker-compose.yml` + `nginx/gh-mirror.compose.conf`（HTTP 反代）+ `nginx/gh-mirror.https.conf`（HTTPS 模板）+ `.env.example`。

> 仓库自带 `Dockerfile`（多阶段 → distroless）与 `install.sh MODE=docker`。
> 本方案在其之上补上**可移植的 compose 编排 + 反向代理/TLS 配置**，无需登录特定面板即可部署。

## 设计：网关与反代解耦
网关核心（`gh-mirror`）始终启动；反向代理（`nginx`）为**可选项**，不强行占用主机 80/443——
部署机往往已经运行自己的 nginx / KPanel 等。按实际情况二选一：

| 场景 | 适用 | 命令 | 对外端口 |
|------|------|------|----------|
| A. 自带反代 | 机器已有 nginx / KPanel | `docker compose up -d`（并暴露网关端口） | 由既有反代转发 |
| B. 内置反代 | 机器无其它 Web 服务 | `docker compose --profile proxy up -d` | 80 / 443 |

## 前置
- 一台 Linux 主机（amd64 / arm64），已装 Docker 与 docker compose。
- 场景 B 或需要对网关启用 HTTPS：域名 A 记录指向主机公网 IP，安全组/防火墙放行 **22 / 80 / 443**。
- （Let's Encrypt 申请证书时 80 端口必须可达。）

## 快速开始

### 场景 A：机器已有 nginx / KPanel
```bash
git clone -b feat/hubproxy https://github.com/yufeng8/gh-mirror.git
cd gh-mirror
cp .env.example .env
# 编辑 .env：设置 ADMIN_PASS 与 ADMIN_SECRET（openssl rand -hex 32）
docker compose up -d
# 取消 docker-compose.yml 中 gh-mirror 的 ports 注释，把网关暴露到本机：
#   ports:
#     - "127.0.0.1:8080:8080"
# 然后让既有反代把域名转发到 127.0.0.1:8080（转发写法见下方“反代要点”）
docker compose up -d gh-mirror
```
- 默认从本仓库源码构建（使用根目录 `Dockerfile`）。
- 想改用预构建镜像：编辑 `docker-compose.yml`，注释掉 `build: .` 并取消 `image: ghcr.io/yufeng8/gh-mirror:latest` 注释。
- 既有的 nginx 转发段可直接参考 `nginx/gh-mirror.compose.conf` 的 `location /`（注意 `merge_slashes off` + `proxy_pass ...$request_uri`）。

### 场景 B：用内置 nginx
```bash
git clone -b feat/hubproxy https://github.com/yufeng8/gh-mirror.git
cd gh-mirror
cp .env.example .env
# 编辑 .env：设置 ADMIN_PASS 与 ADMIN_SECRET（openssl rand -hex 32）
docker compose --profile proxy up -d --build
```
内置 nginx 监听 80（HTTP），反代到 `gh-mirror:8080`。

## 启用 HTTPS
### 场景 B（内置 nginx）
`gm-certs` 卷对 nginx 容器可写，certbot 可直接写入：
```bash
DOMAIN=git.yufeng.site
# 1) 把 nginx/gh-mirror.https.conf 中的 <DOMAIN> 替换为实际域名
# 2) 编辑 docker-compose.yml，将 nginx 挂载改为：
#      ./nginx/gh-mirror.https.conf:/etc/nginx/conf.d/default.conf:ro
# 3) 申请证书
docker compose --profile proxy run --rm --entrypoint "" nginx \
  certbot certonly --webroot -w /var/www/certbot -d $DOMAIN
# 4) 重新加载
docker compose --profile proxy up -d nginx
```

### 场景 A（既有 nginx）
在既有 nginx 上配置 443 + 反代到 `127.0.0.1:8080`，证书用 certbot 按你现有流程申请即可；
反代要点与内置配置一致：`merge_slashes off` + `proxy_pass ...$request_uri` + 透传 `Host`/`X-Forwarded-*`。

## 验证
```bash
# 场景 A（假设暴露到 127.0.0.1:8080，经反代访问域名或直连）：
curl -fsS http://127.0.0.1:8080/healthz
# 场景 B：
curl -fsS http://<主机IP>/healthz        # 应返回 ok
```
后台：`http://<域名或IP>/admin`（账号见 `.env`，首次需登录添加 token，无 token 的请求返回 404）。

## 常用运维
| 项 | 命令 |
|----|------|
| 日志 | `docker logs -f gh-mirror` |
| 重启 | `docker compose restart`（或 `docker compose --profile proxy restart`） |
| 升级 | 改 `image` 标签或重新 `build`，再 `docker compose up -d` |
| 缓存清理 | 命名卷 `gm-cache`；可加 cron `docker run --rm -v gm-cache:/cache busybox find /cache -mtime +7 -delete` |
| 卸载 | `docker compose down`（或 `docker compose --profile proxy down`）；数据在 `gm-data` / `gm-cache` 卷，删卷即清空 |

## 已知坑（来自项目大脑 knowledge/）
- **双斜杠**：代理路径 `/g/<token>/https://...` 的 `//` 必须保留，nginx 需 `merge_slashes off` + `proxy_pass ...$request_uri`，否则解析失败 404。
- **端口冲突**：场景 B 的内置 nginx 会占用主机 80/443。若部署机已有其它 Web 服务，请改用场景 A（不启用 proxy profile）。
- **GHCR 可见性**：`ghcr.io/yufeng8/gh-mirror` 包须设为 **Public**，否则匿名 `docker pull` 报 401；私有则需在 `gh-mirror` 服务加 `docker login ghcr.io`（带 `read:packages` 的令牌）。
- **缓存持久化**：`gm-cache` / `gm-data` 为命名卷，重建容器不会丢缓存与 token。
- **模板内嵌**：页面模板由 Go `go:embed` 编译进二进制，运行时无需挂载 `templates/` 目录。
- **HF 默认关**：二进制含 HF 代码，但 `HF_ENABLED` 未设时默认关闭；要开放 HuggingFace 代理就写进 `.env`。
