# gh-mirror 通用 Docker 部署

放弃特定面板/运维通道，改用标准 Docker Compose 在任意装有 Docker 的主机上一键部署。
本目录提供：`docker-compose.yml` + `nginx/gh-mirror.compose.conf` + `.env.example`。

> 仓库自带 `Dockerfile`（多阶段 → distroless）与 `install.sh MODE=docker`。
> 本方案在其之上补上**可移植的 compose 编排 + 反向代理/TLS 配置**，无需登录特定面板即可部署。

## 前置
- 一台 Linux 主机（amd64 / arm64），已装 Docker 与 docker compose。
- 域名 A 记录指向主机公网 IP，安全组/防火墙放行 **22 / 80 / 443**。
- （Let's Encrypt 申请证书时 80 端口必须可达。）

## 快速开始
```bash
git clone https://github.com/yufeng8/gh-mirror.git
cd gh-mirror
cp .env.example .env
# 编辑 .env：设置 ADMIN_PASS 与 ADMIN_SECRET（openssl rand -hex 32）
docker compose up -d
```
- 默认从本仓库源码构建（使用根目录 `Dockerfile`）。
- 想改用预构建镜像：编辑 `docker-compose.yml`，注释掉 `build: .` 并取消 `image: ghcr.io/yufeng8/gh-mirror:latest` 注释。

## 反向代理与 TLS
- 默认 `nginx` 服务挂载 `nginx/gh-mirror.compose.conf`（HTTP 80，**`merge_slashes off` 必选**），反代到 `gh-mirror:8080`。
- 启用 HTTPS：
  1. 用 certbot 申请证书（webroot 写入共享卷 `gm-certs`）：
     ```bash
     docker compose run --rm --entrypoint "" nginx \
       certbot certonly --webroot -w /var/www/certbot -d <你的域名>
     ```
  2. 将 `nginx/gh-mirror.compose.conf` 替换为带 443 的版本（参考 `nginx/gh-mirror.conf` 的 https 片段，并把 `127.0.0.1:8080` 改为 `gh-mirror:8080`、`<DOMAIN>` 替换为实际域名），再挂载为 `default.conf`：
     编辑 `docker-compose.yml` 中 nginx 的 volumes，将
     `./nginx/gh-mirror.compose.conf:/etc/nginx/conf.d/default.conf:ro`
     改为
     `./nginx/gh-mirror.https.conf:/etc/nginx/conf.d/default.conf:ro`
  3. `docker compose up -d nginx` 重新加载。

## 验证
```bash
curl -fsS http://<主机IP或域名>/healthz   # 应返回 ok
```
后台：`https://<域名>/admin`（账号见 `.env`，首次需登录添加 token，无 token 的请求返回 404）。

## 常用运维
| 项 | 命令 |
|----|------|
| 日志 | `docker logs -f gh-mirror` |
| 重启 | `docker compose restart` |
| 升级 | 改 `image` 标签或重新 `build`，再 `docker compose up -d` |
| 缓存清理 | 命名卷 `gm-cache`；可加 cron `docker run --rm -v gm-cache:/cache busybox find /cache -mtime +7 -delete` |
| 卸载 | `docker compose down`；数据在 `gm-data` / `gm-cache` 卷，删卷即清空 |

## 已知坑（来自项目大脑 knowledge/）
- **双斜杠**：代理路径 `/g/<token>/https://...` 的 `//` 必须保留，nginx 需 `merge_slashes off` + `proxy_pass ...$request_uri`，否则解析失败 404。
- **GHCR 可见性**：`ghcr.io/yufeng8/gh-mirror` 包须设为 **Public**，否则匿名 `docker pull` 报 401；私有则需在 `gh-mirror` 服务加 `docker login ghcr.io`（带 `read:packages` 的令牌）。
- **缓存持久化**：`gm-cache` / `gm-data` 为命名卷，重建容器不会丢缓存与 token。
- **模板内嵌**：页面模板由 Go `go:embed` 编译进二进制，运行时无需挂载 `templates/` 目录。
