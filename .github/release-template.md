# gh-mirror v1.4.0 — 注册三态 · 设置弹窗 · Token 可见性收紧 · 后台视觉统一

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.4.0 是后台账号体系与界面的一次整合版本：注册模式合并为无歧义的三态、设置改为弹窗表格、Token 默认只看自己的、用户列表补上邮箱/最后登录/Token 数，并对整个后台做了一轮统一美化。

## ✨ 本版亮点

- 🔐 **注册三态（取代两个独立开关）**：`关闭注册` / `仅邀请码注册` / `开放注册` 一个下拉搞定；旧 `settings.json` 自动迁移（关闭→关闭、开启+仅邀请码→仅邀请码、开启→开放），并回写旧字段便于回滚。
  **修复一处真实矛盾**：此前 `/user/register` 不校验注册开关，关闭后仍能打开注册页（提交才被拒）；现在关闭时直接跳回登录页并提示「注册未开放」。
- ⚙️ **设置改为弹窗表格**：「用户注册与邮箱」放在「用户管理」页顶部，默认只显示「⚙️ 设置」按钮 + 状态摘要（注册模式 / 联系邮箱 / SMTP）；点击弹出居中弹窗，内部两列表格：注册模式、图形验证码、首页隐藏登录、联系邮箱、SMTP（主机/端口/账号/密码/发件人/TLS），**底部保存/取消固定**，内容超高时带可见滚动条（自绘，不依赖系统 overlay）。
- 🛡️ **Token 可见性收紧**：任何角色（含超级管理员）在 **Token（令牌）管理、概览统计、侧栏计数、MCP `list_tokens`、链接转换**中都只看/只用自己的 Token；删除/编辑同样只能操作自己的。管理员（非安装时超管）在任何页面都无法查看他人 Token。
- 📋 **用户列表增强**：列为 `用户名 / 邮箱 / 角色 / 创建时间 / 最后登录 / Token 数 / 操作`。
  - 邮箱为空显示「—」；最后登录在登录成功时写入 `users.json`（从未登录显示「—」）；
  - Token 数点击进入该用户的 **只读 Token 详情页**（仅安装时超管）：可查看/复制完整 Token、备注、类型、命中、流量、创建/最近使用，但不能修改或删除。
- 🎨 **后台统一美化**（借鉴 shadcn 设计语言，纯 CSS）
  - 设计令牌：`--radius .625rem`、`--shadow-sm/md`、`--ring-soft`；统一细滚动条与 focus ring
  - 按钮统一 36px 高 / 圆角 / hover 层次；登录、注册、重置密码按钮改为 44px + flex 垂直居中 + 1rem 字号
  - 侧栏当前项加左侧主色指示条；卡片/表头/行 hover 统一
  - 「Token 管理」页面标题与侧栏菜单改为「**令牌管理**」；Token 表列宽优化（访问次数收窄、流量加宽、操作按钮不换行）
  - 登录页「忘记密码」展开时位置完全固定（不再左右/上下漂移）；用户入口不再显示「超管密码无法在线找回」提示
  - 小屏顶部汉堡按钮靠右；首页登录按钮支持后台隐藏（仅隐藏入口，`/admin`、`/user` 仍可直达）

## ⚠️ 升级提示（三处行为变化）

1. **注册设置变成三态**：原「开启注册 + 仅邀请码」自动换算，无需手动改；后台界面从两个勾选框变成一个下拉。
2. **超管在令牌管理页也只看到自己的 Token**（此前可见全部）。查看他人请走「用户管理 → 该用户 Token 数」（只读详情）。
3. **后台界面有较大调整**：设置入口在用户管理页顶部（弹窗表单）、Token 菜单与标题改为「令牌管理」、登录卡片改为顶部锚定。

## ✅ 兼容性

- 端点、MCP 地址（`/ai/<用户名>/mcp`）、双入口（`/admin` + `/user`）、配置与数据卷格式均不变；无破坏性变更。
- `settings.json` / `users.json` 均自动兼容旧格式（新增字段缺省时按旧值推导），升级只需替换镜像 / 二进制。

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
  ghcr.io/yufeng8/gh-mirror:v1.4.0
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> 另注意：`ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
