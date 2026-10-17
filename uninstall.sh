#!/usr/bin/env bash
#
# gh-mirror 卸载脚本
# 用法(本机 root 运行):  bash uninstall.sh
#   --purge   同时删除数据（tokens.json / users.json / invites.json / settings.json / 缓存）
#   -y/--yes  跳过确认提示
#
# 清理范围（覆盖三种部署方式：install.sh 原生 / install.sh 与 deploy.sh 的 docker 模式 /
# docker-compose 模式）：
#   1) systemd 服务（原生模式）
#   2) Docker 容器 / 镜像 / 命名卷（两套命名：脚本用 gh-mirror-*，compose 用 gm-*）/ 网络
#   3) nginx 站点配置
#   4) 定时清理 cron（该 cron 只是兜底——gh-mirror 自身每 10 分钟自动清理缓存）
#   5) 程序目录 / 配置目录 / 环境文件
#   6) 数据（默认保留，--purge 才删）
#
set -euo pipefail

APP_USER="ghmirror"
APP_DIR="/opt/gh-mirror"
CACHE_DIR="/var/cache/gh-mirror"
TOKENS_DIR="/var/lib/gh-mirror"
SERVICE_NAME="gh-mirror"
ETC_DIR="/etc/gh-mirror"
ENV_FILE="/etc/gh-mirror.env"
STATE_FILE="/etc/gh-mirror.state"

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

echo "将卸载 gh-mirror (模式: ${MODE}${DOMAIN:+, 域名: ${DOMAIN}}${PURGE:+, 含数据删除})"
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

# 2) Docker 容器 / 镜像 / 卷 / 网络
if command -v docker >/dev/null && \
   docker ps -a --filter "name=^${SERVICE_NAME}$" --format '{{.Names}}' | grep -q "${SERVICE_NAME}"; then
  echo "==> 停止并移除 Docker 容器"
  IMG="$(docker inspect -f '{{.Config.Image}}' "${SERVICE_NAME}" 2>/dev/null || true)"
  docker rm -f "${SERVICE_NAME}" 2>/dev/null || true
  if [[ "${PURGE}" == "1" ]]; then
    [[ -n "${IMG}" ]] && docker rmi "${IMG}" 2>/dev/null || true
    docker rmi "${SERVICE_NAME}" 2>/dev/null || true
    # 命名卷两套命名：install.sh / deploy.sh 用 gh-mirror-*，docker-compose 用 gm-*
    # （gm-certs 是 compose 的证书卷；一并清理避免残留用户数据与证书）
    docker volume rm \
      "${SERVICE_NAME}-cache" "${SERVICE_NAME}-data" \
      gm-cache gm-data gm-certs 2>/dev/null || true
    docker network rm gm-net 2>/dev/null || true
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

# 4) 定时清理（兜底 cron；gh-mirror 自身已每 10 分钟按 TTL/超限/磁盘余量自动清理）
echo "==> 移除定时清理"
rm -f /etc/cron.d/gh-mirror

# 5) 程序目录与配置（/etc/gh-mirror 为 install.sh 创建的配置目录，整个删除避免空目录残留）
echo "==> 移除程序目录与配置"
rm -rf "${APP_DIR}" "${ETC_DIR}"
rm -f "${ENV_FILE}" "${STATE_FILE}"

# 6) 数据（默认保留，--purge 才删）
if [[ "${PURGE}" == "1" ]]; then
  echo "==> 删除数据: ${CACHE_DIR} ${TOKENS_DIR}"
  echo "    （含 tokens.json / users.json / invites.json / settings.json 与缓存文件）"
  rm -rf "${CACHE_DIR}" "${TOKENS_DIR}"
  if id -u "${APP_USER}" >/dev/null 2>&1; then
    userdel "${APP_USER}" 2>/dev/null || true
  fi
  # 证书与防火墙规则不自动删除：证书可能被同机其它站点共用，防火墙规则涉及主机安全，
  # 如需清理请手动执行：
  #   certbot delete --cert-name <域名>
  #   ufw status numbered   # 按需 delete 80/443 规则
  if [[ -n "${DOMAIN}" ]]; then
    echo "    提示: TLS 证书未自动删除（可能被同机其它站点共用），如需清理请手动执行:"
    echo "          certbot delete --cert-name ${DOMAIN}"
  fi
else
  echo "==> 已保留数据目录: ${CACHE_DIR} ${TOKENS_DIR}（用 --purge 可一并删除）"
  echo "    若不再需要该用户，可手动执行: userdel ${APP_USER}"
fi

echo "卸载完成。"
