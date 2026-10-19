# gh-mirror v1.4.4 — 指标口径说明 + 链接转换区美化

一个单二进制、零第三方依赖的自托管加速网关：**Docker 镜像 · GitHub 资源 · HuggingFace 权重**三类加速一站搞定。v1.4.4 是两处体验打磨：**统计卡加问号说明**（把「总命中 / 总流量」这类指标的口径讲清楚），以及**链接转换区重排**（两行等宽、说明折叠）。

## ✨ 本版亮点

- ❓ **统计卡问号说明**：概览四张指标卡标题旁新增问号，**点击或悬停**即弹出深色说明气泡
  - **Token 数**：当前账号可见的 Token 数量（超管也仅自己的）
  - **总命中**：经 `/g/` 与 `/hf/` 代理**成功**的请求次数；状态码 ≥ 400 不计入；用别人 Token 的流量记在那个 Token 上
  - **总流量**：上述成功响应实际写给客户端的字节数（按 Token 分别累计后求和）
  - **运行时长**：进程运行时长，实例重启后重置
  - 纯 CSS 实现（`:hover` / `:focus`）：点击后保持显示，点别处自动消失，无需额外 JS
  - 缓存管理等其他页面的统计卡**不显示**问号，不受影响
- 🎨 **链接转换区重排**
  - 输入框与输出框**统一 560px**（此前输入 460px、输出铺满整行约 1040px）
  - 输出框改为等宽字体 + 浅底色，链接更易读、也明确是只读结果
  - 原占 3 行的说明文字收进 `<details>`「使用说明与示例」，**默认收起**（收起仅 53px，展开 182px）

## 🔧 其他改进

- 链接转换两行按钮均紧随输入框，不再有「一个窄一个宽」的错位感
- 说明内容一字未减（GitHub / HuggingFace 示例、`/offline` 与 `/v2` 规则），拆为两段更易读
- 小屏（≤600px）输入框仍铺满，移动端不受影响

## ✅ 兼容性

- 无破坏性变更：端点、MCP 地址、双入口、配置与数据卷格式均不变
- MCP 主推接入方式仍是 **Streamable HTTP**（`"type": "http"` + `headers.Authorization`）
- 说明气泡不改变任何统计口径：仍是「当前账号可见 Token」的成功代理请求与字节数

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
  ghcr.io/yufeng8/gh-mirror:v1.4.4
```

启动后：**超级管理员**访问 `http://<域名>/admin`；**普通用户**访问 `http://<域名>/user`。

> 纯 HTTP（无 TLS）部署请设 `COOKIE_SECURE=false`，否则浏览器不下发 Secure cookie，表现为登录无反应 / 反复跳回登录页。
> `ADMIN_PASS` 只是**首次启动的种子密码**，之后改该环境变量不会更新已存在用户的密码（详见 `docs/agent-prompt.md`）。

## 🔗 相关链接

- 文档：[README.md](https://github.com/yufeng8/gh-mirror/blob/main/README.md) · [docs/](https://github.com/yufeng8/gh-mirror/tree/main/docs) · [docs/mcp.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/mcp.md) · [docs/docker-deploy.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/docker-deploy.md)
- 镜像：[ghcr.io/yufeng8/gh-mirror](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
- AI Agent 部署提示词：[docs/agent-prompt.md](https://github.com/yufeng8/gh-mirror/blob/main/docs/agent-prompt.md)

---
