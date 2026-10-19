# gh-mirror v1.3.5 — 注册三态 · 设置弹窗 · Token 可见性收紧

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.3.5 收尾后台账号体系的几处不一致：注册模式合并为无歧义的三态、设置区移回用户管理并改为弹窗表格、Token 可见性收紧为「只看自己的」，用户列表补上邮箱 / 最后登录 / Token 数。

## ✨ 本版亮点

- 🔐 **注册三态（取代两个独立开关）**：`关闭注册` / `仅邀请码注册` / `开放注册` 一个下拉搞定；旧 `settings.json` 自动迁移（关闭→关闭、开启+仅邀请码→仅邀请码、开启→开放），并回写旧字段便于回滚。
  **修复了一处真实矛盾**：此前 `/user/register` 不校验注册开关，关闭后仍能打开注册页（提交才被拒）；现已统一为「关闭时直接跳回登录页并提示」。
- ⚙️ **设置改为弹窗表格**：「用户注册与邮箱」移回「用户管理」页，只显示「⚙️ 设置」按钮与状态摘要（注册模式 / 联系邮箱 / SMTP）；点击弹出居中弹窗，内部为两列表格：注册模式、图形验证码、首页隐藏登录、联系邮箱、SMTP（主机/端口/账号/密码/发件人/TLS），底部保存/取消。SMTP 密码不回显、留空保持不变。服务管理页只保留「邀请码管理」。
- 🛡️ **Token 可见性收紧**：任何角色（含超级管理员）在 **Token 管理页、概览统计、侧栏计数、MCP `list_tokens`、链接转换**中都只看/只用自己的 Token；删除/编辑同样只能操作自己的。管理员（非安装时超管）在任何页面都无法查看他人 Token。
- 📋 **用户列表增强**：列变为 `用户名 / 邮箱 / 角色 / 创建时间 / 最后登录 / Token 数 / 操作`。
  - 邮箱为空显示「—」；最后登录在登录成功时写入 `users.json`（从未登录显示「—」）；
  - Token 数点击进入该用户的 **只读 Token 详情页**（仅安装时超管）：可查看/复制完整 Token、备注、类型、命中、流量、创建/最近使用，但不能修改或删除。

## ⚠️ 升级提示（两处行为变化）

1. **注册设置变成三态**：原「开启注册 + 仅邀请码」会自动换算为三态之一，无需手动改；只是后台界面从两个勾选框变为一个下拉。
2. **超管在 Token 管理页也只看到自己的 Token**（此前可见全部）。需要查看他人的 Token 时，请走「用户管理 → 该用户 Token 数」进入只读详情页。

## ✅ 兼容性

- 端点、MCP 地址（`/ai/<用户名>/mcp`）、双入口（`/admin` + `/user`）、配置与数据卷格式均不变；无破坏性变更。
- `settings.json` / `users.json` 均自动兼容旧格式（新增字段缺省时按 old 值推导），升级只需替换镜像 / 二进制。

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
  ghcr.io/yufeng8/gh-mirror:v1.3.5
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> 另注意：`ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
