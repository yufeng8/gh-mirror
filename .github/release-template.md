# gh-mirror v1.4.3 — 镜像搜索详情 · 注册验证码交互 · 后台体验细化

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.4.3 是一轮「细节补齐」：镜像搜索结果补齐关键信息、离线包显示版本与上游更新时间、注册页图形验证码改为按需展开并加 60 秒冷却，以及链接转换与侧栏的一批体验优化。

## ✨ 本版亮点

- 🔍 **镜像搜索结果补全详情**
  - 修复此前只有 `21411 ★` 的问题：`/api/search` 现在输出规范化字段 `name / description / stars / pulls / official / updated`
  - 每条结果显示：**镜像名**（如 `library/nginx`）、官方标记、★ 星级、**拉取数**（千分位）、**上游更新时间**（N 天前）、描述
- 📦 **离线包显示版本与上游更新时间**
  - 镜像名下方新增 `版本 7.2`（取拉取时的 tag；digest 引用显示缩写）与 `上游更新 3 天前`（取镜像 config 的 `created`）
  - 用户下载与后台重拉两条路径都会记录并持久化
  - ⚠️ 升级前已缓存的包没有该时间，重新拉取或新拉取后出现
- 🔐 **注册页图形验证码改为「发码时展开」+ 60 秒冷却**
  - 图形码默认隐藏，点「发送验证码」才展开并要求输入（避免首屏同时出现两个验证码）
  - 发码接口在开启用户入口验证码时会校验图形码（防刷）
  - 发送成功后按钮进入 **60 秒倒计时**，失败自动刷新图形码
- 🚀 **链接转换更好用**：输入框收窄至 460px，新增「🔍 搜索镜像」按钮（复用镜像选择面板），选中即填入
- 👋 **侧栏展示当前用户**：底部新增用户名 + 「👋 欢迎你！」卡片（退出登录与 Star 引导之间）

## 🔧 其他改进

- 镜像选择面板的常用镜像、搜索结果与拉取输入框宽度统一（460px），链接转换与离线包两处一致
- 「我的 Docker 自定义路径」输入框由 130px 加宽到 320px，长路径不再被截断
- 发码按钮在找回密码场景也能正确定位（不再依赖注册页专属 id）

## ✅ 兼容性

- 无破坏性变更：端点、MCP 地址、双入口、配置与数据卷格式均不变
- MCP 主推接入方式仍是 **Streamable HTTP**（`"type": "http"` + `headers.Authorization`）
- `offline-cache.json` 新增 `upstream` 字段，旧文件自动兼容（缺失视为未知）

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
  ghcr.io/yufeng8/gh-mirror:v1.4.3
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> `ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
