# gh-mirror v1.3.4 — 角色权限收紧 · 后台设置整合与交互优化

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.3.4 聚焦**账号权限边界**与**后台设置体验**：只有安装时创建的超级管理员能管理用户与角色，后台设置区完成整合、改为可展开表单，并修掉一处会互相覆盖保存的缺陷。

## ✨ 本版亮点

- 🛡️ **角色权限收紧（行为变更，请留意）**：新增「唯一超管」概念——**只有安装时创建的超级管理员（`ADMIN_USER` 种子）能进入用户管理、增删用户、改角色、重置密码**，以及管理「用户注册与邮箱」「邀请码」。后续被设为「管理员」的账号**不再能**把超级管理员降级或互相提权；种子超管本身**不可被降级 / 删除**。服务管理、缓存管理、离线包等运维操作仍对管理员开放。
  > **升级提示**：若你的实例此前创建了多个 `super` 账号，升级后除 `ADMIN_USER` 之外的那些账号将**失去用户管理权限**（其余权限不受影响）。
- 🧩 **后台整合**：「用户注册与邮箱」（含 SMTP）与「邀请码管理」从「用户管理」页移到「服务管理」页；左侧导航顺序调整为「服务管理 → 缓存管理 → 用户管理」。
- ⚙️ **设置改为「按钮 + 表单」**：「用户注册与邮箱」不再平铺，默认只显示「⚙️ 设置」按钮与状态摘要（注册开 / 关、联系邮箱、SMTP 是否已配置），点开是一个完整表单（注册开关 / 图形验证码 / 首页隐藏登录按钮 / 联系邮箱 / SMTP），底部保存 / 取消；保存后自动展开并回显。SMTP 密码**不回显**、留空保持不变。
- 🐛 **修复设置互相覆盖**：此前注册区与 SMTP 区是两个表单却共用一个端点、全量覆盖——点「保存」会清空 SMTP 凭据，点「保存邮箱设置」会把注册开关与联系邮箱重置。现统一为分组 / 单一表单保存。
- 🎨 **细节优化**：小屏顶部导航汉堡按钮靠右；首页登录 / 注册按钮可在后台隐藏（`/admin`、`/user` 仍可直达，不会锁死后台）；邀请码管理改为分区布局（批量生成 / 自定义各占一行），导出格式补齐占位符说明与真实示例。

## ✅ 兼容性

- 端点、MCP 地址（`/ai/<用户名>/mcp`）、双入口（`/admin` + `/user`）、配置与数据卷格式均不变；无破坏性变更。
- 唯一的行为变更是**用户管理权限收紧**（见上）；升级只需替换镜像 / 二进制。

## 🚀 快速开始

```bash
# 单容器 + 既有反代（推荐：单文件、真实 env、只绑 127.0.0.1:8080）
cp .env.example .env   # 设 ADMIN_PASS / ADMIN_SECRET
docker compose -f docker-compose.standalone.yml up -d

# 或 docker run
docker run -d --name gh-mirror \
  -p 127.0.0.1:8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  ghcr.io/yufeng8/gh-mirror:v1.3.4
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
