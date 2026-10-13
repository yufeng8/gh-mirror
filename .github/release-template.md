# GitHub Mirror 镜像加速服务

轻量、自托管的 GitHub 资源镜像代理：通过带 Token 的 URL 加速 `raw`、`releases`、`archive` 等资源的访问，自带用量统计与管理后台。

## ✨ 功能特性

- **Token 鉴权**：每个访问地址携带独立 Token，未授权一律 404
- **用量统计**：按 Token 记录访问次数、流量与最近使用时间，持久化到 `tokens.json`
- **管理后台**：Web 界面增删 Token、查看统计，卡片式精致 UI
- **智能缓存**：GET 请求带缓存减少回源；并发回源串行化避免惊群
- **多协议重写**：`raw.githubusercontent.com` / `github.com` / releases / archive 统一经 `github-mirror` 重写
- **一键部署**：提供 `deploy.sh`、`Dockerfile`、`systemd` 服务文件与 `nginx` 反代配置

## 🚀 快速开始

1. 下载对应平台的二进制（`github-mirror-<os>-<arch>`）并放到服务器
2. 准备 `tokens.json`（或用管理后台添加），并设置环境变量：
   - `ADMIN_PASS`：后台登录密码
   - `LISTEN`：监听地址（默认 `:8080`）
   - `TOKENS_FILE`：Token 文件路径
   - `CACHE_DIR`：缓存目录
3. 启动后访问 `http://<你的域名>/admin` 登录后台，添加第一个 Token
4. 客户端使用方式：
   - Git：`git config --global http."https://github.com/".proxy http://<你的域名>/g/<token>`
   - 或直接把资源 URL 的 `https://github.com` 替换为 `https://<你的域名>/g/<token>/https://github.com`

## 🔗 相关链接

- 源码：https://github.com/zzidcn/github-mirror
- 部署文档：见仓库 `README.md` 与 `deploy.sh`

---
