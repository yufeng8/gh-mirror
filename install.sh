#!/usr/bin/env bash
#
# gh-mirror 一键安装脚本（从 GitHub Release / 源码 拉取，支持原生与 Docker）
#
# 用法（公开仓库，远程服务器上一行执行）:
#   bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
#
# 私有仓库：需带 GITHUB_TOKEN（具备 repo 读 + read:packages 权限的 PAT）：
#   GITHUB_TOKEN=ghp_xxx bash -c "$(curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
#     https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)"
#
# 非交互 / 一行搞定（原生模式）:
#   DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='你的密码' \
#     bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
#
# Docker 模式（默认从 GHCR 拉取现成镜像；加 DOCKER_BUILD=1 可改为从 GitHub 源码现场构建）:
#   MODE=docker DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='你的密码' \
#     bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
#   指定镜像标签:       MODE=docker VERSION=v1.0.0 ...
#   改用源码现场构建镜像: MODE=docker DOCKER_BUILD=1 ...
#
# 可选环境变量:
#   MODE         native(默认) | docker
#   VERSION      原生模式: 指定 Release 标签(如 v1.0.0)，默认取最新；
#                Docker 模式: 指定镜像标签(默认 latest)；若 DOCKER_BUILD=1 则为源码引用(分支/标签，默认 main)
#   DOCKER_BUILD 仅 Docker 模式: 设为 1 时从 GitHub 源码构建镜像（而非拉取 GHCR 现成镜像）
#   GITHUB_TOKEN 私有仓库必填: 具备 repo(读) + read:packages 权限的 PAT，用于拉取脚本/Release/登录 GHCR
#   GHCR_USER    GHCR 登录用户名（默认 yufeng8），填与 GITHUB_TOKEN 对应账号一致即可
#   GH_PROXY     下载加速前缀，例如 https://ghproxy.com/（原生二进制下载用，国内服务器可用）
#   SKIP_NGINX=1 跳过 nginx + TLS 配置，只安装服务（自行反代时）
#   ADMIN_USER / ADMIN_PATH  如需自定义后台账号/路径
#
set -euo pipefail

REPO="yufeng8/gh-mirror"
APP_USER="ghmirror"
APP_DIR="/opt/gh-mirror"
CACHE_DIR="/var/cache/gh-mirror"
TOKENS_DIR="/var/lib/gh-mirror"
SERVICE_NAME="gh-mirror"
ENV_FILE="/etc/gh-mirror.env"
STATE_FILE="/etc/gh-mirror.state"

# ---------- 参数 ----------
DOMAIN="${DOMAIN:-}"
EMAIL="${EMAIL:-}"
ADMIN_PASS="${ADMIN_PASS:-}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PATH="${ADMIN_PATH:-/admin}"
VERSION="${VERSION:-}"
GH_PROXY="${GH_PROXY:-}"
SKIP_NGINX="${SKIP_NGINX:-0}"
MODE="${MODE:-native}"
case "${MODE}" in
  native|n) MODE="native" ;;
  docker|d) MODE="docker" ;;
  *) echo "错误: MODE 仅支持 native / docker（当前: ${MODE}）" >&2; exit 1 ;;
esac
DOCKER_BUILD="${DOCKER_BUILD:-0}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
GHCR_USER="${GHCR_USER:-yufeng8}"

# GitHub 鉴权封装：有 GITHUB_TOKEN 时自动带 Authorization 头
gh_curl() {
  if [[ -n "${GITHUB_TOKEN}" ]]; then
    curl -H "Authorization: Bearer ${GITHUB_TOKEN}" "$@"
  else
    curl "$@"
  fi
}

# 令牌权限自检：通过 GitHub API 的 X-OAuth-Scopes 响应头，检查是否含所需 scope。
# 用法: check_token_scopes "read:packages"  或  "repo,read:packages"
# 仅当 GITHUB_TOKEN 已设置时生效；无法读取 scope 头时（如细粒度 PAT）跳过以免误报。
check_token_scopes() {
  [[ -z "${GITHUB_TOKEN}" ]] && return 0
  local need="$1" header scopes code req missing
  header="$(gh_curl -s -o /dev/null -D - -w 'XHTTP_STATUS:%{http_code}' \
    "https://api.github.com/user" 2>/dev/null || true)"
  code="$(printf '%s\n' "${header}" | tr -d '\r' | sed -n 's/^XHTTP_STATUS://p')"
  scopes="$(printf '%s\n' "${header}" | tr -d '\r' \
    | awk -F': ' 'tolower($1)=="x-oauth-scopes"{print $2}')"
  if [[ "${code}" != "200" ]]; then
    echo "    ⚠ 令牌校验失败 (HTTP ${code:-未知})：可能无效或已过期，私有仓库/镜像将无法正常访问。" >&2
    return 0
  fi
  echo "    令牌授权范围: ${scopes:-（空）}"
  if [[ -z "${scopes}" ]]; then
    echo "    （无法读取授权范围，跳过 scope 校验；若后续拉取报 401，请核对令牌权限）"
    return 0
  fi
  missing=""
  local IFS=','; for req in ${need}; do
    printf ',%s,' "${scopes}" | grep -qi ",${req}," || missing="${missing} ${req}"
  done
  if [[ -n "${missing}" ]]; then
    echo "    ⚠ 提示: 令牌缺少以下权限:${missing}" >&2
    echo "      将导致无法访问对应资源。请重新生成 PAT 并勾选这些 scope 后重跑本脚本。" >&2
  fi
}

# ---------- 前置检查 ----------
if [[ "$(uname -s)" != "Linux" ]]; then
  echo "错误: 当前仅支持 Linux 服务器" >&2
  exit 1
fi
if [[ $EUID -ne 0 ]]; then
  echo "错误: 请使用 root 运行 (sudo bash install.sh)" >&2
  exit 1
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  aarch64|arm64) GOARCH="arm64" ;;
  x86_64|amd64)  GOARCH="amd64" ;;
  *) echo "错误: 不支持的架构 ${ARCH}" >&2; exit 1 ;;
esac

# ---------- 交互读取（缺省才问）----------
if [[ "${SKIP_NGINX}" != "1" ]]; then
  [[ -z "${DOMAIN}" ]] && read -r -p "部署域名 (如 mirror.example.com): " DOMAIN
  [[ -z "${EMAIL}" ]] && read -r -p "证书邮箱 (Let's Encrypt 必填): " EMAIL
fi
[[ -z "${ADMIN_PASS}" ]] && { read -r -s -p "管理员密码 [admin]: " ADMIN_PASS; echo; }
ADMIN_PASS="${ADMIN_PASS:-admin}"
[[ -z "${DOMAIN}" ]] && DOMAIN="mirror.example.com"

ADMIN_SECRET="$(openssl rand -hex 32)"

# 记录部署状态，供 uninstall.sh 使用
mkdir -p /etc/gh-mirror
printf 'MODE=%s\nDOMAIN=%s\n' "${MODE}" "${DOMAIN}" > "${STATE_FILE}"

# ---------- 原生模式安装 ----------
install_native() {
  [[ -n "${GITHUB_TOKEN}" ]] && check_token_scopes "repo"
  mkdir -p "${APP_DIR}"
  if [[ -z "${VERSION}" ]]; then
    echo "==> 查询最新版本..."
    VERSION="$(gh_curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep -oP '"tag_name":\s*"\K[^"]+')" || VERSION=""
    if [[ -z "${VERSION}" ]]; then
      echo "错误: 无法获取最新版本（GitHub API 限流或被墙），可用 VERSION=v1.0.0 指定" >&2
      exit 1
    fi
  fi
  echo "==> 安装版本: ${VERSION}  (架构 linux/${GOARCH})"

  ASSET="gh-mirror-linux-${GOARCH}"
  URL="${GH_PROXY}https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
  echo "==> 下载 ${URL}"
  gh_curl -fSL "${URL}" -o "${APP_DIR}/gh-mirror"
  chmod +x "${APP_DIR}/gh-mirror"
  echo "    二进制已就绪: ${APP_DIR}/gh-mirror"

  id -u "${APP_USER}" >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin "${APP_USER}"
  mkdir -p "${APP_DIR}" "${CACHE_DIR}" "${TOKENS_DIR}"

  mkdir -p /etc/gh-mirror
  cat > "${ENV_FILE}" <<EOF
ADMIN_USER=${ADMIN_USER}
ADMIN_PASS=${ADMIN_PASS}
ADMIN_PATH=${ADMIN_PATH}
ADMIN_SECRET=${ADMIN_SECRET}
COOKIE_SECURE=true
TOKENS_FILE=${TOKENS_DIR}/tokens.json
LISTEN=127.0.0.1:8080
CACHE_DIR=${CACHE_DIR}
CACHE_TTL_MINUTES=1440
MAX_CACHE_MB=200
UPSTREAM_TIMEOUT=60
EOF
  chmod 0600 "${ENV_FILE}"
  echo "==> 管理员账号: ${ADMIN_USER}  密码: ${ADMIN_PASS}  (已写入 ${ENV_FILE})"

  cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=GitHub Mirror Proxy (${DOMAIN})
After=network.target

[Service]
Type=simple
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${APP_DIR}/gh-mirror
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

  chown -R "${APP_USER}:${APP_USER}" "${CACHE_DIR}" "${TOKENS_DIR}"
  chown -R root:root "${APP_DIR}"

  systemctl daemon-reload
  systemctl enable --now "${SERVICE_NAME}"
  sleep 1
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    echo "    服务已启动"
  else
    echo "    服务启动失败，查看: journalctl -u ${SERVICE_NAME}" >&2
    exit 1
  fi
}

# ---------- Docker 模式安装 ----------
install_docker() {
  if ! command -v docker >/dev/null; then
    echo "==> 安装 Docker"
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
  fi
  docker version --format '{{.Server.Version}}' >/dev/null || {
    echo "错误: Docker 未正常启动" >&2; exit 1; }

  # 私有仓库：用 GITHUB_TOKEN 登录 GHCR（需 read:packages 权限）
  if [[ -n "${GITHUB_TOKEN}" ]]; then
    check_token_scopes "read:packages$([[ "${DOCKER_BUILD}" == "1" ]] && echo ',repo')"
    echo "==> [docker] 使用 GITHUB_TOKEN 登录 GHCR (用户 ${GHCR_USER})"
    rm -f /tmp/docker_login.err
    if ! printf '%s' "${GITHUB_TOKEN}" | docker login ghcr.io -u "${GHCR_USER}" --password-stdin 2>/tmp/docker_login.err; then
      echo "    ❌ GHCR 登录失败。" >&2
      echo "      最可能原因: 令牌缺少 read:packages 权限（GHCR 鉴权必需）。" >&2
      echo "      其他可能: 令牌无效/过期，或 GHCR_USER(${GHCR_USER}) 与令牌所属账号不一致。" >&2
      if [[ -s /tmp/docker_login.err ]]; then
        echo "      原始错误: $(grep -iE 'error|denied|unauthorized' /tmp/docker_login.err | head -1 | sed 's/^[[:space:]]*//')" >&2
      fi
      echo "      若镜像已设为 Public，可忽略并继续匿名拉取；否则请补全权限后用同一令牌重跑。" >&2
    fi
  else
    echo "    未提供 GITHUB_TOKEN：将以匿名方式拉取（仅限公开镜像；私有镜像会 401）。"
  fi

  IMAGE="ghcr.io/${REPO,,}"
  if [[ "${DOCKER_BUILD}" == "1" ]]; then
    # 兜底：从 GitHub 源码现场构建（私有仓库需 GITHUB_TOKEN 才能克隆）
    GIT_INSTEADOF_KEY=""
    if [[ -n "${GITHUB_TOKEN}" ]]; then
      GIT_INSTEADOF_KEY="url.https://${GITHUB_TOKEN}@github.com/.insteadOf"
      git config --global "${GIT_INSTEADOF_KEY}" "https://github.com/"
      trap 'git config --global --unset "'"${GIT_INSTEADOF_KEY}"'" 2>/dev/null || true' EXIT
    fi
    BUILD_REF="${VERSION:-main}"
    DOCKER_CONTEXT="https://github.com/${REPO}.git#${BUILD_REF}"
    echo "==> [docker] 从源码构建 ${DOCKER_CONTEXT} (GOARCH=${GOARCH})"
    docker build --build-arg "GOARCH=${GOARCH}" -t "${SERVICE_NAME}" "${DOCKER_CONTEXT}"
    IMAGE_REF="${SERVICE_NAME}"
    [[ -n "${GIT_INSTEADOF_KEY}" ]] && git config --global --unset "${GIT_INSTEADOF_KEY}" 2>/dev/null || true
  else
    # 默认：直接拉取 GHCR 现成镜像
    TAG="${VERSION:-latest}"
    echo "==> [docker] 拉取镜像 ${IMAGE}:${TAG}"
    docker pull "${IMAGE}:${TAG}"
    IMAGE_REF="${IMAGE}:${TAG}"
  fi

  echo "==> [docker] 启动容器 (host 网络 + 缓存/数据命名卷)"
  docker rm -f "${SERVICE_NAME}" >/dev/null 2>&1 || true
  docker run -d --name "${SERVICE_NAME}" \
    --network=host --restart=unless-stopped \
    -e ADMIN_USER="${ADMIN_USER}" \
    -e ADMIN_PASS="${ADMIN_PASS}" \
    -e ADMIN_PATH="${ADMIN_PATH}" \
    -e ADMIN_SECRET="${ADMIN_SECRET}" \
    -e COOKIE_SECURE="true" \
    -e TOKENS_FILE=/var/lib/gh-mirror/tokens.json \
    -e LISTEN=127.0.0.1:8080 \
    -e CACHE_DIR=/var/cache/gh-mirror \
    -e CACHE_TTL_MINUTES=1440 \
    -e MAX_CACHE_MB=200 \
    -e UPSTREAM_TIMEOUT=60 \
    -v "${SERVICE_NAME}-cache:/var/cache/gh-mirror" \
    -v "${SERVICE_NAME}-data:/var/lib/gh-mirror" \
    "${IMAGE_REF}"

  sleep 2
  if docker ps --filter "name=^${SERVICE_NAME}$" --format '{{.Names}}' | grep -q "${SERVICE_NAME}"; then
    echo "    容器已启动"
  else
    echo "    容器启动失败，查看: docker logs ${SERVICE_NAME}" >&2
    exit 1
  fi
}

# ---------- 缓存定时清理 ----------
setup_cron() {
  if [[ "${MODE}" == "docker" ]]; then
    cat > /etc/cron.d/gh-mirror <<EOF
# GitHub 镜像缓存清理 (Docker 命名卷 ${SERVICE_NAME}-cache)
10 4 * * * root docker run --rm -v ${SERVICE_NAME}-cache:/cache busybox:latest find /cache -type f -mtime +7 -delete
EOF
  else
    cat > /etc/cron.d/gh-mirror <<EOF
# GitHub 镜像缓存清理（保留 7 天，临时文件 1 天）
10 4 * * * root find ${CACHE_DIR} -type f -mtime +7 -delete
10 4 * * * root find ${CACHE_DIR} -type f -name '*.tmp' -mtime +1 -delete
EOF
  fi
  chmod 0644 /etc/cron.d/gh-mirror
  systemctl enable --now cron 2>/dev/null || service cron start 2>/dev/null || true
}

# ---------- 模式分发 ----------
if [[ "${MODE}" == "docker" ]]; then
  install_docker
else
  install_native
fi

# ---------- 缓存定时清理（两种模式共用）----------
echo "==> 配置缓存定时清理"
setup_cron

# ---------- 可选：nginx + TLS（两种模式均反代 127.0.0.1:8080）----------
if [[ "${SKIP_NGINX}" != "1" ]]; then
  if command -v apt-get >/dev/null; then
    echo "==> 安装 nginx / certbot / ufw"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y nginx certbot python3-certbot-nginx ufw curl cron openssl

    echo "==> 写入 nginx 配置"
    cat > "/etc/nginx/sites-available/${DOMAIN}.conf" <<'NGINX'
server {
    listen 80;
    server_name DOMAIN_PLACEHOLDER;

    # 关键：禁止合并双斜杠，否则 /https://... 会被改成 /https:/... 导致解析失败
    merge_slashes off;

    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8080$request_uri;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINX
    sed -i "s/DOMAIN_PLACEHOLDER/${DOMAIN}/" "/etc/nginx/sites-available/${DOMAIN}.conf"
    ln -sf "/etc/nginx/sites-available/${DOMAIN}.conf" "/etc/nginx/sites-enabled/${DOMAIN}.conf"
    rm -f /etc/nginx/sites-enabled/default
    nginx -t
    systemctl reload nginx

    echo "==> 配置防火墙 (22/80/443)"
    ufw allow 22/tcp
    ufw allow 80/tcp
    ufw allow 443/tcp
    ufw --force enable || true

    echo "==> 申请 Let's Encrypt 证书"
    certbot --nginx -d "${DOMAIN}" --non-interactive --agree-tos -m "${EMAIL}" --redirect
  else
    echo "警告: 未检测到 apt-get，已跳过 nginx/TLS 配置。请手动反代 127.0.0.1:8080。" >&2
  fi
fi

# ---------- 自检 ----------
echo "==> 自检"
sleep 2
if curl -fsS -o /dev/null -w "本地健康检查 HTTP %{http_code}\n" "http://127.0.0.1:8080/healthz"; then
  echo "部署成功! (模式: ${MODE})"
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
  echo "自检失败，请检查服务状态与 nginx -t" >&2
fi
