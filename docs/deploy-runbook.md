# gh-mirror 目标机部署 Runbook（EdgeOne 前置 · 最终验证版）

> 目标：把 `yufeng8/gh-mirror`（`feat/hubproxy`）部署为多服务加速网关（Docker 镜像加速 / GitHub 代理 / HuggingFace 权重 / 离线 tar / 镜像搜索）。
> 本文按**已验证通过的架构**编写，命令均可直接复制。示例域名用 `git.yufeng.site`，换成你的即可。

## 架构（重要：域名前置腾讯 EdgeOne）
```
用户(HTTPS) →[EdgeOne: 持公网证书 + 终止TLS + 关缓存/关URL规范化]→ 回源 HTTP:80 →[源站面板 nginx: git.yufeng.site 仅80反代]→ gh-mirror:8080
```
- **源站不签自己的证书**，只做 80 反代；公网 HTTPS 由 EdgeOne 提供。
- EdgeOne 前置是本次最大坑（见文末故障排查）：会导致源站 LE 报 521、并可能压掉 gh-mirror 的双斜杠路径。

## 镜像
- 用：`ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy`（多架构 amd64/arm64，含 HF，**已修登录 400 bug**）。
- 不要用 `:latest`（跟踪 main，**无 HF**）；不要用 `:v0.1.0`（带登录 bug）。

---

## 1. 部署网关（Docker）
```bash
# 拉镜像（若 401：到 GHCR Packages 设置把包改为 Public）
docker pull ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy

# 生成密码与签名密钥（记好）
ADMIN_SECRET=$(openssl rand -hex 32); ADMIN_PASS=$(openssl rand -base64 18)

# 启动（保留同名数据卷则 token 不丢；覆盖旧容器时卷沿用）
docker rm -f gh-mirror 2>/dev/null
docker run -d --name gh-mirror --restart unless-stopped \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS="$ADMIN_PASS" \
  -e ADMIN_SECRET="$ADMIN_SECRET" \
  -e ADMIN_PATH=/admin -e LISTEN=0.0.0.0:8080 -e COOKIE_SECURE=true \
  -e HF_ENABLED=true \
  -p 127.0.0.1:8080:8080 \
  -v gm-cache:/var/cache/gh-mirror -v gm-data:/var/lib/gh-mirror \
  ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy

docker logs --tail 20 gh-mirror
curl -fsS http://127.0.0.1:8080/healthz     # 期望 ok
```
- `COOKIE_SECURE=true`（对外是 https）。若暂不走 TLS 直连测试可设 `false`。
- 要开 HuggingFace 代理才有 `/hf/`：`HF_ENABLED=true`。
- 本机自检：`curl -fsS http://127.0.0.1:8080/healthz` → `ok` 再往下配反代。

## 2. EdgeOne 配置（公网入口）
- **回源/源站地址**：`源站公网IPv4:80`，**回源协议 HTTP**。
- **回源 Host**：固定 `git.yufeng.site`（源站 nginx 靠 server_name 匹配）。
- **TLS 证书**：交给 EdgeOne（自动签发或上传）。
- **务必关闭**：缓存（不缓存/跟随源站）、URL 规范化/重写、智能压缩；**不要强制 80 跳转**。
- 大文件/流式（`/offline/` 产 tar）经 CDN 可能限速/超时：必要时对该路径关缓存直通，或直连源站测。

## 3. 源站 nginx 反代（面板里 git.yufeng.site 站点，仅 80，不签证书）
面板建/改 `git.yufeng.site` 站点 → 反代目标 `http://127.0.0.1:8080`，`location /` 用：
```nginx
location / {
    merge_slashes off;                         # 必须：保留 /g/<token>/https:// 的双斜杠
    proxy_pass http://127.0.0.1:8080$request_uri;   # 必须带 $request_uri，不能写成 ...:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;  # 真实客户端经 EdgeOne 是 https
    proxy_buffering off; proxy_request_buffering off;   # 大文件/流式
    proxy_read_timeout 600s; proxy_send_timeout 600s; client_max_body_size 0;
}
```
- **不要**再加 http→https 跳转（EdgeOne 已终止 TLS；源站 80 直接反代，否则回源回环）。
- `nginx -t && nginx -s reload`（或面板重载）。
- 源站回源连通自测：`echo ok > <git.yufeng.site站点webroot>/acme-test.txt`，外网 `curl -4 http://git.yufeng.site/acme-test.txt` → `ok`（不再 521）。

## 4. 防火墙 / 安全组（源站）
- 源站 **80 入站**对 EdgeOne 回源/公网开放；**443 可不开放**（EdgeOne→源站只走 80）。

---

## 5. 登录与加 token
- 后台：`https://git.yufeng.site/admin`（`ADMIN_PATH` 默认 `/admin`）。
- 账号 `ADMIN_USER`（默认 `admin`），密码 `ADMIN_PASS`（第 1 步那个）。
- 忘密码：`docker inspect gh-mirror --format '{{range .Config.Env}}{{println .}}{{end}}' | grep ADMIN_`。
- **登录后第一件事：添加 token**；之后代理请求都带它（不带 token 的代理请求返回 404，属设计）。
- `v0.1.2-hubproxy` 已修复"点登录跳 `/%!s(MISSING)` 报 400"的问题；若仍复现，确认镜像是 `v0.1.2-hubproxy`。

## 6. 端到端验证（token 记作 `<TK>`）
```bash
B=https://git.yufeng.site
curl -fsS $B/healthz                                            # ok
# GitHub 代理（双斜杠必须保留）
curl -fsSI "$B/g/<TK>/https://raw.githubusercontent.com/octocat/Hello-World/master/README"
# 搜索 / tags
curl -fsS "$B/search?q=nginx" | head;  curl -fsS "$B/tags?image=library/nginx" | head
# HuggingFace 代理（需 HF_ENABLED=true 且主机出网）
curl -fsSL "$B/hf/<repo>/<path>" -o part.bin -w "hf:%{http_code} size:%{size_download}\n"
# 离线 tar（P5 关键）：产出后本机 docker load 能载入
curl -fsS "$B/offline/library/nginx:1.25" -o nginx.tar && docker load -i nginx.tar && docker images | grep nginx
```
**Docker 加速（registry-mirror）**：
```bash
# /etc/docker/daemon.json 加：
#   { "registry-mirrors": ["https://git.yufeng.site"] }
systemctl restart docker
docker pull nginx            # 再看网关日志是否命中缓存
docker logs -f gh-mirror
```
> 若 docker 报 mirror 相关错误（EdgeOne/https 兼容问题），可临时改用 `http://127.0.0.1:8080` 在源站本机验证加速链路。

## 7. 限流 / ACL / SOCKS5（需 config.toml，无 env 映射）
在部署目录放 `config.toml` 并重启容器加载。示例片段（按需开）：
```toml
[ratelimit]
enabled = true
period_seconds = 60
quota = 120

[acl]
ip_allow = []        # 例：["1.2.3.4"]
ip_block = []
repo_allow = []
repo_block = []

[upstream]
socks5 = ""          # 例："socks5://127.0.0.1:1080"
```
> 先最小可用，再逐个开；改 `config.toml` 后 `docker restart gh-mirror`。

## 8. 如何更新（升级镜像）+ 常用运维

### 8.1 如何更新（推荐：env 文件 + 启动脚本，升级只改一个 tag）
首次把配置固化成两个文件（一次性，之后升级/改密码都靠它们）：
```bash
mkdir -p /opt/gh-mirror && cd /opt/gh-mirror
cat > gm.env <<'EOF'
ADMIN_USER=admin
ADMIN_PASS=你的ADMIN_PASS
ADMIN_SECRET=你的ADMIN_SECRET
ADMIN_PATH=/admin
LISTEN=0.0.0.0:8080
COOKIE_SECURE=true
HF_ENABLED=true
EOF
cat > run.sh <<'EOF'
#!/usr/bin/env bash
set -e
IMAGE="${1:-ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy}"
docker pull "$IMAGE"
docker rm -f gh-mirror 2>/dev/null || true
docker run -d --name gh-mirror --restart unless-stopped \
  --env-file /opt/gh-mirror/gm.env \
  -p 127.0.0.1:8080:8080 \
  -v gm-cache:/var/cache/gh-mirror -v gm-data:/var/lib/gh-mirror \
  "$IMAGE"
sleep 1; docker logs --tail 20 gh-mirror
curl -fsS http://127.0.0.1:8080/healthz
EOF
chmod +x run.sh
./run.sh            # 用默认 tag 起一次
```
**以后升级**（token/缓存在 `gm-data`/`gm-cache` 卷，不丢；源站 nginx 反代不用动）：
```bash
cd /opt/gh-mirror
./run.sh ghcr.io/yufeng8/gh-mirror:<新tag>     # 例如 v0.1.2-hubproxy
```
**回滚**（新镜像有问题，用上一个 tag 再跑一次即可）：
```bash
./run.sh ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy
```
> 注意：`gm.env` 只放**我们自己的变量**；不要把 `docker inspect` 导出的镜像默认变量（`PATH` 等）塞进去，否则可能破坏容器。改 `ADMIN_PASS`/`ADMIN_SECRET`/开关只需编辑 `gm.env` 再 `./run.sh`。
> 若用 compose 部署则更简单：改 `docker-compose.yml` 的 `image:` tag，再 `docker compose ... up -d`（带上与首次相同的 `-f/--profile`）。

### 8.2 常用运维
```bash
docker logs -f gh-mirror                 # 看日志
docker restart gh-mirror                 # 重启（不换镜像）
docker run --rm -v gm-cache:/cache busybox find /cache -mtime +7 -delete   # 清 7 天前缓存
docker rm -f gh-mirror                   # 卸载容器（卷保留）
docker volume rm gm-data gm-cache        # 彻底清数据（token/缓存都没）
```

## 9. 故障排查
| 现象 | 原因 / 处理 |
|------|-------------|
| 点登录跳 `/%!s(MISSING)` 报 400 | 旧镜像 `v0.1.0` 的登录 bug；换 `v0.1.2-hubproxy` |
| 经域名 502/521 | EdgeOne 回源地址/端口/Host 不对，或源站 80 未对 EdgeOne 放行 |
| `/g/<token>/https://...` 变 404 | EdgeOne 缓存/URL 规范化没关，或源站缺 `merge_slashes off`/`$request_uri` |
| 后台登录无反应/反复跳登录 | 缺 `X-Forwarded-Proto https`，或 `COOKIE_SECURE` 与访问协议不符 |
| 源站签 LE 报 521 | EdgeOne 前置所致；按本架构**源站不签证书**，证书交给 EdgeOne；确需源站持证用 DNS-01 |
| `/hf/` 失败 | 未设 `HF_ENABLED=true`，或主机无出网 |
| `docker pull` 镜像 401 | GHCR 包非 Public；改为 Public 或用源码构建 |
| 80 外网不通 | 云安全组/运营商封 80；用 `Test-NetConnection <域名> -Port 80` 外网探测，注意 AAAA/IPv6 |

## 附录：UI 坏掉时用 curl 登录 + 加 token（兜底）
```bash
curl -sS -c gm.txt -d "user=admin" -d "pass='你的ADMIN_PASS'" "https://git.yufeng.site/admin" -o /dev/null -w "login:%{http_code}\n"
SID=$(awk '/ghm_session/{print $NF}' gm.txt | cut -d. -f1)
CSRF=$(printf 'csrf:%s' "$SID" | openssl dgst -sha256 -hmac '你的ADMIN_SECRET' | awk '{print $NF}')
curl -sS -b gm.txt --data-urlencode "csrf=$CSRF" --data-urlencode "token=e2e-test-001" --data-urlencode "note=target" \
  "https://git.yufeng.site/admin/add" -o /dev/null -w "add:%{http_code}\n"
# 之后用 token=e2e-test-001 测代理
```
