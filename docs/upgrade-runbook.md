# gh-mirror 目标机升级 Runbook → v0.1.2-hubproxy（给 AI Agent 执行）

> 目标：把已在运行的 gh-mirror 升级到 `ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy`
> （三页 UI 现代化重构 + favicon + 登录「忘记密码」面板 + 后台统计卡/服务徽标/健康检查/运行时长 + Docker 安全强提醒 + `%!s(MISSING)` 修复）。
> **保留数据与既有反代**，对外仅瞬断（容器重建 <1s）。

---

## 0. 架构回顾（升级时不要破坏）
```
用户(HTTPS) →[腾讯 EdgeOne：持公网证书 + 终止TLS + 关缓存/关URL规范化 + 不强制跳80]
            → 回源 HTTP:80 →[源站面板 nginx：git.yufeng.site 仅做 80 反代]
            → gh-mirror:8080
```
- 源站 **不签自己的证书**；反代段必须保留：`merge_slashes off;` + `proxy_pass http://127.0.0.1:8080$request_uri;` + `proxy_set_header X-Forwarded-Proto https;`，且**不要再加 http→https 跳转**。
- 本次**只升级 gh-mirror 容器/镜像**；EdgeOne 设置与源站 nginx 反代**保持不变**。

---

## 1. 升级前自检（记录现状，便于回滚）
```bash
docker ps --filter name=gh-mirror --format '{{.Names}} {{.Image}} {{.Status}}'
docker inspect gh-mirror --format '当前镜像={{.Config.Image}}'
docker volume ls | grep -E 'gm-data|gm-cache'          # 确认数据卷存在
# 记录旧镜像 tag 以便回滚
docker inspect gh-mirror --format '{{.Config.Image}}' | tee /tmp/gm_old_image.txt
# 备份 token 数据（gm-data 卷内 tokens.json，双保险）
mkdir -p /root/gm-backup-$(date +%F)
docker run --rm -v gm-data:/data -v /root/gm-backup-$(date +%F):/b alpine:3 sh -c 'cp -a /data/. /b/ 2>/dev/null; ls -la /b'
```
- 若上面 `docker inspect` 报容器不存在，说明不是容器部署 → 先与负责人确认部署方式，不要盲目操作。

---

## 2. 拉取并校验新镜像
```bash
docker pull ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy
docker images | grep v0.1.2-hubproxy
```
> 若 `docker pull` 报 `401/denied`：GHCR 包非 Public → 到 GitHub Packages 设置把 `gh-mirror` 包改为 Public，或联系负责人。**不要用 `:latest`**（跟踪 main、不含 HF）。

---

## 3. 升级（按现网部署方式二选一）

### 方式 A｜docker run 部署（一键脚本：自动沿用旧 env 与卷）
> 自动从旧容器提取**我们自己的**环境变量（ADMIN_*/LISTEN/COOKIE_SECURE/HF_ENABLED/DOCKER_ENABLED 等，**不会**误带 PATH 等镜像默认项），保留 `gm-data`/`gm-cache` 卷。
```bash
cat > /root/gm-upgrade.sh <<'EOF'
#!/usr/bin/env bash
set -e
OLD=gh-mirror
NEW=ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy
HOSTPORT=${HOSTPORT:-127.0.0.1:8080}        # 若旧容器发布端口不同，改这里

getv(){ docker inspect "$OLD" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
        | awk -F= -v k="$1" '$1==k{print substr($0,length(k)+2)}'; }

[ -f /tmp/gm_old_image.txt ] || docker inspect "$OLD" --format '{{.Config.Image}}' | tee /tmp/gm_old_image.txt
echo "==> 拉取 $NEW"; docker pull "$NEW"

args=(run -d --name gh-mirror --restart unless-stopped)
for k in ADMIN_USER ADMIN_PASS ADMIN_SECRET ADMIN_PATH LISTEN COOKIE_SECURE \
         HF_ENABLED DOCKER_ENABLED UPSTREAM_SOCKS5; do
  v="$(getv "$k")"; [ -n "$v" ] && args+=(-e "$k=$v") && echo "   沿用 $k"
done
args+=(-p "$HOSTPORT" -v gm-cache:/var/cache/gh-mirror -v gm-data:/var/lib/gh-mirror "$NEW")

echo "==> 重建容器"; docker rm -f "$OLD"; docker "${args[@]}"
sleep 1; docker logs --tail 20 gh-mirror
echo "==> healthz: $(curl -fsS http://127.0.0.1:8080/healthz)"
EOF
bash /root/gm-upgrade.sh
```

### 方式 B｜compose 部署
```bash
cd <部署目录>                       # 含 docker-compose.yml 的目录
grep -n "image:\|build:" docker-compose.yml
# 若原先用 image：改 tag
sed -i -E 's#(image:[[:space:]]*).*gh-mirror.*#\1ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy#' docker-compose.yml
# 若原先用源码 build（无 image 行）：注释 build 并启用 image
# sed -i 's/^([[:space:]]*)build: \./\1# build: ./' docker-compose.yml
# 然后加一行：    image: ghcr.io/yufeng8/gh-mirror:v0.1.2-hubproxy

# 用与首次启动相同的 -f / --profile 组合
docker compose -f docker-compose.yml [-f docker-compose.publish.yml] [--profile proxy] up -d
docker logs --tail 20 gh-mirror
curl -fsS http://127.0.0.1:8080/healthz     # 期望 ok
```
> 注意：用过 `-f`/`--profile` 的，`up` 也要带同样的参数，否则找不到服务定义。`ADMIN_PASS`/`ADMIN_SECRET` 等在 `.env`，保持不变。

---

## 4. 验证（从内到外）
```bash
# 1) 网关本机
curl -fsS http://127.0.0.1:8080/healthz                     # ok
# 2) 源站反代（带 Host，不过 EdgeOne）
curl -fsS -H 'Host: git.yufeng.site' http://127.0.0.1/healthz   # ok
# 3) 经 EdgeOne 全链路
curl -fsS https://git.yufeng.site/healthz                   # ok
```
浏览器：
- 首页 `https://git.yufeng.site/` → 多服务宣传页（Docker/GitHub/HuggingFace + 终端窗口 + GitHub 引导）。
- 登录 `https://git.yufeng.site/admin` → 分屏登录页；点「**忘记密码？**」应能展开"在服务器上重置密码"面板。
- 登录后后台 → 顶部**统计卡**（Token 数/总命中/总流量/缓存占用/运行时长）、**服务状态徽标**、**健康检查**按钮；若开了 Docker，顶部有 ⚠️ 安全提醒。
- 代理抽查（双斜杠必须保留）：`curl -fsSI "https://git.yufeng.site/g/<token>/https://raw.githubusercontent.com/octocat/Hello-World/master/README"`。

---

## 5. 回滚（新镜像有问题时）
```bash
OLDIMG="$(cat /tmp/gm_old_image.txt)"     # 例如 ghcr.io/yufeng8/gh-mirror:v0.1.1-hubproxy
HOSTPORT=${HOSTPORT:-127.0.0.1:8080}
getv(){ docker inspect gh-mirror --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
        | awk -F= -v k="$1" '$1==k{print substr($0,length(k)+2)}'; }
args=(run -d --name gh-mirror --restart unless-stopped)
for k in ADMIN_USER ADMIN_PASS ADMIN_SECRET ADMIN_PATH LISTEN COOKIE_SECURE HF_ENABLED DOCKER_ENABLED UPSTREAM_SOCKS5; do
  v="$(getv "$k")"; [ -n "$v" ] && args+=(-e "$k=$v"); done
args+=(-p "$HOSTPORT" -v gm-cache:/var/cache/gh-mirror -v gm-data:/var/lib/gh-mirror "$OLDIMG")
docker rm -f gh-mirror; docker "${args[@]}"; curl -fsS http://127.0.0.1:8080/healthz
# compose 部署则把 image 改回旧 tag 再 up -d
```

---

## 6. 故障排查
| 现象 | 原因 / 处理 |
|------|-------------|
| 经域名 502/521 | EdgeOne 回源地址/端口/Host 不对，或源站 80 未对 EdgeOne 放行；确认回源=`源站IP:80`、Host=`git.yufeng.site`、不强制跳 80 |
| `/g/<token>/https://...` 变 404 | 源站反代缺 `merge_slashes off` 或 `proxy_pass ...$request_uri` 被改成 `.../;` |
| 后台能开但登录无反应/反复跳登录 | 缺 `X-Forwarded-Proto https`，或 `COOKIE_SECURE` 与访问协议不符（对外 https 应 true） |
| `docker pull` 镜像 401 | GHCR 包非 Public；改为 Public |
| 页面出现 `%!s(MISSING)` | 本版本已修复；若仍出现=镜像没换成功，确认 `docker inspect gh-mirror` 的 Image 为 `v0.1.2-hubproxy` |
| 端口不是 8080 | 方式 A 脚本按 `HOSTPORT` 变量调整；以旧容器实际发布端口为准 |
| 容器起不来 | `docker logs gh-mirror` 看报错；多为 env 缺失（ADMIN_PASS/ADMIN_SECRET）或端口/卷冲突 |

---

## 7. 负责人需确认的信息（升级前提供）
- 部署方式：`docker run` 还是 `compose`？compose 的启动 `-f`/`--profile` 组合是什么？
- 网关发布端口（默认 `127.0.0.1:8080`）？
- 需要开启的开关：`HF_ENABLED` / `DOCKER_ENABLED`（沿用旧容器设置即可，脚本会自动带）。
