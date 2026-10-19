# gh-mirror v1.4.2 — 列表分页与搜索 · Token 批量删除 · 镜像选择面板 · 详情弹窗修复

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.4.2 集中打磨后台的**列表体验**：四个列表统一分页与每页条数、用户列表支持搜索、Token 支持勾选批量删除、离线包拉取新增「选择镜像」面板，并修复实际部署下 Token 详情弹窗的「网络错误」。

## ✨ 本版亮点

- 📄 **列表分页统一**：Token / 邀请码 / 离线包 每页 10/20/50，用户列表 10/20/30/50；页码、每页条数、上一页/下一页在同一行
- 🔍 **用户搜索**：按**用户名 / 邮箱**子串匹配（不区分大小写）；搜索与翻页互相保留参数，可一键「清除」
- ☑️ **Token 批量删除**：行复选框 + 表头全选 + 「删除选中（N）」，逐个做归属校验（只能删自己的）
- 🖼️ **离线包「选择镜像」面板**：常用镜像前 10（nginx / redis / postgres / mysql / mariadb / mongo / node / python / alpine / ubuntu）+ Docker Hub 实时搜索（前 10 条含 stars / 官方 / 描述），点击即填入
- 🐛 **修复 Token 详情弹窗「网络错误」**：此前通过 `fetch /admin/users/tokens.json` 拉数据，经反代（对 `.json` 有特殊规则）或自定义 `ADMIN_PATH` 时会失败。改为**服务端内嵌数据、弹窗本地渲染**，不再发额外请求

## 🔧 其他改进

- 离线包列表分页条此前只在「有数据」时渲染（0 个包时看不到分页）→ 现在**始终显示**，并移到「已缓存离线包」标题右侧
- 邀请码「上一页 / 下一页」从表格下方移到页码同一行，去掉重复分页行
- 用户列表的「Token 数」点击弹窗在各分页下仍准确（统计基于全部用户）

## ✅ 兼容性

- 无破坏性变更：端点、MCP 地址、双入口、配置与数据卷格式均不变
- 分页参数：Token / 用户 / 离线包用 `size` + `page`；邀请码用 `inv_size` + `inv_page`
- `settings.json` / `users.json` 自动兼容旧格式

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
  ghcr.io/yufeng8/gh-mirror:v1.4.2
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> `ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
