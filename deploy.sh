#!/usr/bin/env bash
#
# GitHub 镜像加速服务 - 一键部署脚本 (Ubuntu 24.04)
# 用法: 在服务器上克隆/上传本项目后，root 身份运行:  bash deploy.sh
# 前置: 域名(如 mirror.example.com)的 A 记录已指向本机公网 IP，且 80/443 已放行。
#
set -euo pipefail

# ---------- 可配置项 ----------
DEFAULT_DOMAIN="mirror.example.com"
APP_USER="ghmirror"
APP_DIR="/opt/github-mirror"
CACHE_DIR="/var/cache/github-mirror"
TOKENS_DIR="/var/lib/github-mirror"
SERVICE_NAME="github-mirror"
GO_VERSION="1.22.5"

# ---------- 交互读取参数 ----------
read -r -p "部署域名 [${DEFAULT_DOMAIN}]: " DOMAIN
DOMAIN="${DOMAIN:-$DEFAULT_DOMAIN}"

read -r -p "证书申请邮箱 (Let's Encrypt 必填): " EMAIL
while [[ -z "${EMAIL}" ]]; do
  read -r -p "邮箱不能为空，请输入: " EMAIL
done

read -r -p "部署方式 [native=原生systemd / docker=容器] (默认 native): " MODE
MODE="${MODE:-native}"
case "${MODE}" in
  native|n) MODE="native" ;;
  docker|d) MODE="docker" ;;
  *) echo "错误: 未知模式 ${MODE}" >&2; exit 1 ;;
esac

# ---------- 管理员与密钥配置 ----------
read -r -s -p "管理员密码 [admin]: " ADMIN_PASS; echo
ADMIN_PASS="${ADMIN_PASS:-admin}"
ADMIN_USER="admin"
ADMIN_PATH="/admin"
ADMIN_SECRET="$(openssl rand -hex 32)"
COOKIE_SECURE="true"
TOKENS_FILE="${TOKENS_DIR}/tokens.json"

# 写入环境文件（systemd 加载 / 调试参考）
mkdir -p /etc/github-mirror
cat > /etc/github-mirror.env <<EOF
ADMIN_USER=${ADMIN_USER}
ADMIN_PASS=${ADMIN_PASS}
ADMIN_PATH=${ADMIN_PATH}
ADMIN_SECRET=${ADMIN_SECRET}
COOKIE_SECURE=${COOKIE_SECURE}
TOKENS_FILE=${TOKENS_FILE}
LISTEN=127.0.0.1:8080
CACHE_DIR=${CACHE_DIR}
CACHE_TTL_MINUTES=1440
MAX_CACHE_MB=200
UPSTREAM_TIMEOUT=60
EOF
chmod 0600 /etc/github-mirror.env
echo "==> 管理员账号: ${ADMIN_USER}  密码: ${ADMIN_PASS}  (已写入 /etc/github-mirror.env)"

echo "==> 部署参数: 域名=${DOMAIN}  方式=${MODE}"

# ---------- 环境检查 ----------
if [[ $EUID -ne 0 ]]; then
  echo "错误: 请使用 root 运行 (sudo bash deploy.sh)" >&2
  exit 1
fi
command -v apt-get >/dev/null || { echo "错误: 非 Debian/Ubuntu 系统" >&2; exit 1; }

# ---------- 安装系统依赖 (两种模式共用) ----------
echo "==> 安装 nginx / certbot / ufw / curl / cron"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y nginx certbot python3-certbot-nginx ufw curl cron openssl

# 检测目标架构 (供 Go / Docker 构建使用)
ARCH="$(uname -m)"
case "${ARCH}" in
  aarch64|arm64) GOARCH="arm64" ;;
  x86_64|amd64)  GOARCH="amd64" ;;
  *) echo "错误: 不支持的架构 ${ARCH}" >&2; exit 1 ;;
esac

# ---------- 按模式准备运行实体 ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ ! -f "${SCRIPT_DIR}/go.mod" ]]; then
  echo "错误: 未找到 go.mod，请在项目根目录运行本脚本" >&2
  exit 1
fi

if [[ "${MODE}" == "native" ]]; then
  # ===== 原生 systemd 模式 =====
  echo "==> [native] 安装 Go ${GO_VERSION}"
  if ! command -v go >/dev/null; then
    GO_TAR="go${GO_VERSION}.linux-${GOARCH}.tar.gz"
    curl -fsSL "https://go.dev/dl/${GO_TAR}" -o "/tmp/${GO_TAR}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TAR}"
    rm -f "/tmp/${GO_TAR}"
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
  fi
  go version

  echo "==> [native] 编译"
  cd "${SCRIPT_DIR}"
  export GOOS=linux GOARCH="${GOARCH}" CGO_ENABLED=0
  go build -o github-mirror .

  echo "==> [native] 创建用户与目录"
  id -u "${APP_USER}" >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin "${APP_USER}"
  mkdir -p "${APP_DIR}" "${CACHE_DIR}" "${TOKENS_DIR}"
  cp -f github-mirror "${APP_DIR}/github-mirror"
  chmod +x "${APP_DIR}/github-mirror"
  chown -R "${APP_USER}:${APP_USER}" "${CACHE_DIR}" "${TOKENS_DIR}"
  chown -R root:root "${APP_DIR}"

  echo "==> [native] 安装 systemd 服务"
  cp -f github-mirror.service "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
  systemctl enable --now "${SERVICE_NAME}"
  sleep 1
  systemctl is-active --quiet "${SERVICE_NAME}" && echo "    服务已启动" || {
    echo "    服务启动失败，查看: journalctl -u ${SERVICE_NAME}" >&2; exit 1; }

elif [[ "${MODE}" == "docker" ]]; then
  # ===== Docker 容器模式 =====
  echo "==> [docker] 安装 Docker"
  if ! command -v docker >/dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
  fi
  docker version --format '{{.Server.Version}}' >/dev/null || {
    echo "错误: Docker 未正常启动" >&2; exit 1; }

  echo "==> [docker] 构建镜像 (GOARCH=${GOARCH})"
  cd "${SCRIPT_DIR}"
  docker build --build-arg "GOARCH=${GOARCH}" -t "${SERVICE_NAME}" .

  echo "==> [docker] 启动容器 (host 网络 + 缓存卷 + 数据卷)"
  docker rm -f "${SERVICE_NAME}" >/dev/null 2>&1 || true
  # --network=host: 容器直接用宿主机网络，规避网桥 NAT 开销
  # 缓存挂命名卷避免写入容器 overlay 层；token 数据挂命名卷持久化
  docker run -d --name "${SERVICE_NAME}" \
    --network=host --restart=unless-stopped \
    -e ADMIN_USER="${ADMIN_USER}" \
    -e ADMIN_PASS="${ADMIN_PASS}" \
    -e ADMIN_PATH="${ADMIN_PATH}" \
    -e ADMIN_SECRET="${ADMIN_SECRET}" \
    -e COOKIE_SECURE="${COOKIE_SECURE}" \
    -e TOKENS_FILE=/var/lib/github-mirror/tokens.json \
    -e LISTEN=127.0.0.1:8080 \
    -e CACHE_DIR=/var/cache/github-mirror \
    -e CACHE_TTL_MINUTES=1440 \
    -e MAX_CACHE_MB=200 \
    -e UPSTREAM_TIMEOUT=60 \
    -v "${SERVICE_NAME}-cache:/var/cache/github-mirror" \
    -v "${SERVICE_NAME}-data:/var/lib/github-mirror" \
    "${SERVICE_NAME}"

  sleep 2
  if docker ps --filter "name=^${SERVICE_NAME}$" --format '{{.Names}}' | grep -q "${SERVICE_NAME}"; then
    echo "    容器已启动"
  else
    echo "    容器启动失败，查看: docker logs ${SERVICE_NAME}" >&2; exit 1
  fi
fi

# ---------- 配置缓存自动清理 (两种模式均写入 /etc/cron.d，每天 04:10 清理 7 天前的缓存) ----------
echo "==> 配置缓存定时清理"
CRON_FILE="/etc/cron.d/github-mirror"
if [[ "${MODE}" == "docker" ]]; then
  cat > "${CRON_FILE}" <<EOF
# GitHub 镜像缓存清理 (Docker 命名卷 ${SERVICE_NAME}-cache)
10 4 * * * root docker run --rm -v ${SERVICE_NAME}-cache:/cache busybox:latest find /cache -type f -mtime +7 -delete
EOF
else
  cat > "${CRON_FILE}" <<EOF
# GitHub 镜像缓存清理 (宿主机目录 ${CACHE_DIR})
10 4 * * * root find ${CACHE_DIR} -type f -mtime +7 -delete
10 4 * * * root find ${CACHE_DIR} -type f -name '*.tmp' -mtime +1 -delete
EOF
fi
chmod 0644 "${CRON_FILE}"
systemctl enable --now cron 2>/dev/null || service cron start 2>/dev/null || true

# ---------- 安装 nginx 配置 (两种模式共用，均反代 127.0.0.1:8080) ----------
echo "==> 配置 nginx"
cp -f nginx/github-mirror.conf "/etc/nginx/sites-available/${DOMAIN}.conf"
sed -i "s/server_name _;/server_name ${DOMAIN};/" "/etc/nginx/sites-available/${DOMAIN}.conf"
ln -sf "/etc/nginx/sites-available/${DOMAIN}.conf" "/etc/nginx/sites-enabled/${DOMAIN}.conf"
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx

# ---------- 防火墙 ----------
echo "==> 配置防火墙 (放行 22/80/443)"
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable || true

# ---------- 申请 HTTPS 证书 ----------
echo "==> 申请 Let's Encrypt 证书"
certbot --nginx -d "${DOMAIN}" --non-interactive --agree-tos -m "${EMAIL}" --redirect

# ---------- 自检 ----------
echo "==> 自检"
sleep 2
if curl -fsS -o /dev/null -w "HTTP %{http_code}\n" "https://${DOMAIN}/healthz"; then
  echo "部署成功!"
  echo "  首页/说明:    https://${DOMAIN}/"
  echo "  管理后台:      https://${DOMAIN}${ADMIN_PATH}  (账号 ${ADMIN_USER} / 你设置的密码)"
  echo "  代理示例:      https://${DOMAIN}/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
  echo "  首次使用请先登录后台添加 token，无 token 的请求会返回 404。"
  if [[ "${MODE}" == "docker" ]]; then
    echo "  容器管理: docker logs -f ${SERVICE_NAME} / docker restart ${SERVICE_NAME}"
  else
    echo "  服务管理: journalctl -u ${SERVICE_NAME} / systemctl restart ${SERVICE_NAME}"
  fi
else
  echo "自检失败，请检查: systemctl status nginx / journalctl -u ${SERVICE_NAME} / docker logs ${SERVICE_NAME} / nginx -t"
fi
