#!/usr/bin/env bash
#
# github-mirror 卸载脚本
# 用法(本机 root 运行):  bash uninstall.sh
#   --purge   同时删除数据（tokens.json / 缓存 / 系统用户）
#   -y/--yes  跳过确认提示
#
set -euo pipefail

APP_USER="ghmirror"
APP_DIR="/opt/github-mirror"
CACHE_DIR="/var/cache/github-mirror"
TOKENS_DIR="/var/lib/github-mirror"
SERVICE_NAME="github-mirror"
ENV_FILE="/etc/github-mirror.env"
STATE_FILE="/etc/github-mirror.state"

PURGE=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    -y|--yes) ASSUME_YES=1 ;;
    *) echo "未知参数: $arg" >&2 ;;
  esac
done

# 读取安装时记录的状态
MODE="native"
DOMAIN=""
if [[ -f "${STATE_FILE}" ]]; then
  MODE="$(grep -oP '^MODE=\K.*' "${STATE_FILE}" || true)"
  DOMAIN="$(grep -oP '^DOMAIN=\K.*' "${STATE_FILE}" || true)"
fi
MODE="${MODE:-native}"

echo "将卸载 github-mirror (模式: ${MODE}${DOMAIN:+, 域名: ${DOMAIN}}${PURGE:+, 含数据删除})"
if [[ "${ASSUME_YES}" != "1" ]]; then
  read -r -p "确认卸载? [y/N] " ans
  case "$ans" in
    y|Y|yes) ;;
    *) echo "已取消"; exit 0 ;;
  esac
fi

# 1) 原生 systemd 服务
if [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]]; then
  echo "==> 停止并禁用 systemd 服务"
  systemctl disable --now "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  systemctl daemon-reload
fi

# 2) Docker 容器 / 镜像 / 卷
if command -v docker >/dev/null && \
   docker ps -a --filter "name=^${SERVICE_NAME}$" --format '{{.Names}}' | grep -q "${SERVICE_NAME}"; then
  echo "==> 停止并移除 Docker 容器"
  IMG="$(docker inspect -f '{{.Config.Image}}' "${SERVICE_NAME}" 2>/dev/null || true)"
  docker rm -f "${SERVICE_NAME}" 2>/dev/null || true
  if [[ "${PURGE}" == "1" ]]; then
    [[ -n "${IMG}" ]] && docker rmi "${IMG}" 2>/dev/null || true
    docker rmi "${SERVICE_NAME}" 2>/dev/null || true
    docker volume rm "${SERVICE_NAME}-cache" "${SERVICE_NAME}-data" 2>/dev/null || true
  fi
fi

# 3) nginx 站点配置
if [[ -n "${DOMAIN}" ]] && [[ -f "/etc/nginx/sites-available/${DOMAIN}.conf" ]]; then
  echo "==> 移除 nginx 站点配置"
  rm -f "/etc/nginx/sites-available/${DOMAIN}.conf" "/etc/nginx/sites-enabled/${DOMAIN}.conf"
  if command -v nginx >/dev/null && nginx -t >/dev/null 2>&1; then
    systemctl reload nginx 2>/dev/null || true
  fi
fi

# 4) 定时清理
echo "==> 移除定时清理"
rm -f /etc/cron.d/github-mirror

# 5) 程序目录与配置
echo "==> 移除程序目录与配置"
rm -rf "${APP_DIR}"
rm -f "${ENV_FILE}" "${STATE_FILE}"

# 6) 数据（默认保留，--purge 才删）
if [[ "${PURGE}" == "1" ]]; then
  echo "==> 删除数据: ${CACHE_DIR} ${TOKENS_DIR}"
  rm -rf "${CACHE_DIR}" "${TOKENS_DIR}"
  if id -u "${APP_USER}" >/dev/null 2>&1; then
    userdel "${APP_USER}" 2>/dev/null || true
  fi
else
  echo "==> 已保留数据目录: ${CACHE_DIR} ${TOKENS_DIR}（用 --purge 可一并删除）"
  echo "    若不再需要该用户，可手动执行: userdel ${APP_USER}"
fi

echo "卸载完成。"
