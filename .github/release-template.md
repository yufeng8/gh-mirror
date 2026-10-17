# gh-mirror v1.2.0 — 多用户与账号体系正式版

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像**、**GitHub 资源**、**HuggingFace 权重**三类加速一站搞定。v1.2.0 起支持**多用户账号体系**——超级管理员与普通用户分入口隔离，并可开放邮箱验证码 / 邀请码自主注册。

## ✨ 本版亮点

- 👥 **多用户 + 双入口强隔离**：超管入口 `/admin` 与普通用户入口 `/user` 分离（`USER_PATH` 可配、可隐藏），各自独立 session cookie；普通用户登录超管入口被拒。入口路径与保留代理路径冲突时启动即报错（fail-fast）。
- 🔐 **图形验证码**：登录 / 注册 / 管理员登录页均支持图形验证码（零依赖点阵渲染），用户入口与管理入口**开关分别可控**。
- 📦 **离线包内容寻址缓存**：按 manifest digest 全局共享命中（源更新自动拉新），后台「缓存文件管理」支持复制下载链接、删除、重新拉取、拉取最新、多选反选、分页，并可选「磁盘满自动清理」（开关 + 阈值）。
- ✉️ **用户自主注册 / 找回密码**：邮箱验证码注册（用户名严格校验：≥4 位、保留字、路径规范、唯一）；邮箱验证码找回密码（仅普通用户，超管密码只能服务器后台改）；SMTP 设置与注册开关由超管在用户管理页统一配置。
- 🎟️ **邀请码注册**：可选「仅邀请码注册」，批量生成 / 自定义邀请码、占位符自定义导出、备注 / 有效期 / 使用追踪（激活时间）。
- 🛡️ **权限收紧**：服务管理、缓存管理、用户管理均仅超管可见可操作；侧栏特权项带盾牌标识。
- 🎨 **UI 完善**：首页导航登录/注册入口 + 小屏抽屉；绿色「已开启」徽标呼吸光晕；移动端卡片式列表。

## ⚠️ 破坏性变更（从 v1.1.0 升级必读）

1. **双入口**：新增普通用户入口 `/user`（默认，`USER_PATH` 可配）。普通用户请从 `/user` 登录；`/admin` 仅超级管理员可登录。
2. **session 升级**：会话值绑定密码摘要，本次升级后所有用户需**重新登录**；改密后旧会话自动失效。
3. **用户名最小长度 2→4 位**（已有短用户名仍可登录，仅新建受限）。
4. **权限变更**：服务管理、缓存管理两个页面由「所有登录用户可见」收紧为**仅超管**。
5. 新增环境变量：`USER_PATH`（默认 `/user`）；入口与 `/g` `/hf` `/offline` `/v2` `/search` `/mcp` `/healthz` 等保留路径冲突会启动报错。

## 🚀 快速开始

```bash
# 方式一：Docker（推荐）
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='强口令' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  -e DOCKER_ENABLED=true -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:v1.2.0

# 方式二：下载本页二进制
# 各平台 gh-mirror-<os>-<arch>，`chmod +x` 后直接运行
```

启动后：超级管理员访问 `http://<域名>/admin` 登录；普通用户访问 `http://<域名>/user`。如需开放自主注册，请先在「用户管理 → 用户注册与邮箱」配置 SMTP 并开启注册开关。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)

---
