# 贡献指南 / Contributing

感谢你关注 **gh-mirror**！无论是新功能、Bug 修复还是文档改进，都欢迎参与。本仓库通过**分支保护**保证 `main` 始终可发布，请按以下约定协作。

## 分支模型

| 分支 | 用途 | 能否直接推送 |
|------|------|------------|
| `main` | 稳定、可发布的代码；只接收已验证的合并 | 受保护，需经 PR + CI 通过（+ review） |
| `feat/xxx` | 新功能开发 | ✅ |
| `fix/xxx` | Bug 修复 | ✅ |
| `docs/xxx` | 文档改动 | ✅ |
| `refactor/xxx` | 重构（不改外部行为） | ✅ |
| `test/xxx` | 补充测试 | ✅ |
| `chore/xxx` | 构建 / 依赖等杂项 | ✅ |

> 分支从 `main` 切出，命名用小写 + 连字符，例如 `feat/token-rotation`、`fix/404-on-missing-file`。

## 开发流程

```bash
# 1. 切到最新 main 并拉取
git switch main && git pull

# 2. 开一个功能分支
git switch -c feat/你的功能

# 3. 本地自测（比等 CI 更快）
go vet ./...
go test ./...      # 13 个测试文件 / 67 个 Test，覆盖缓存、用户、邀请码、验证码、MCP、代理用户名段等
go build ./...
# 交叉编译（发版前必做）
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null .

# 4. 提交并推送分支
git add -A && git commit -m "feat: 简要说明"
git push -u origin feat/你的功能

# 5. 开 PR 到 main
gh pr create --base main --title "feat: ..." --body "..."
```

## 合并门禁（main 分支保护）

PR 合入 `main` 前必须满足：

- ✅ **CI 必过**：`ci.yml` / `build.yml` 会自动运行 `go vet` / `go test` / `go build`（并做多平台交叉编译），对应保护规则里的必过检查。
- ✅ **至少 1 个 review**（如为唯一维护者，可由 admin 直接合并以绕过该要求）。
- 🔓 admin 可 bypass 所有限制，但**日常开发请勿滥用**——分支隔离的意义是让坏代码留在特性分支、不污染 `main`。

## 提交信息约定

推荐使用 [Conventional Commits](https://www.conventionalcommits.org/) 风格，便于生成 changelog：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 仅文档
- `refactor:` 重构（无行为变化）
- `test:` 测试相关
- `chore:` 构建 / 依赖 / 杂项

示例：`fix: 修复缺失文件时误报 404 的问题`

## 项目约定（新增功能前必读）

### 🚫 零第三方依赖（铁律）

后端只允许使用 **Go 标准库**——不引入任何外部 module。这是本项目的第一原则：

- 便于审计（安全敏感项目，依赖面越小越好）
- `CGO_ENABLED=0` 静态编译、离线可构建
- 需要"轮子"时（图形验证码、SMTP、OCI 解析、MCP 协议等）用标准库手写，参考现有实现：`captcha.go`（点阵渲染）、`mail.go`（SMTP）、`offline.go`（tar/OCI）、`mcp.go`（JSON-RPC）

前端同理：管理后台与首页为**服务端渲染的内联 HTML/CSS/JS**（`templates.go`），不引入前端框架与 CDN 资源。

### 🔐 多用户与凭证安全

- 所有代理端点按「用户名段 + Token 归属」校验（`/g` `/hf` `/offline`）或「用户名段 + 高级 Token」（`/ai/<用户名>/mcp`）；实例级操作（服务开关、缓存管理）仅超级管理员。
- `tokens.json` / `users.json` / `invites.json` / `settings.json` 含凭证（密码哈希、SMTP 密码、Token），**已在 `.gitignore` 中忽略，切勿提交**；也不要写进日志、测试快照或 PR 描述。
- 测试中的敏感值用假数据（参考 `mcp_test.go` / `users_test.go` 的 `newTestServer`）。

### 📝 改功能必须同步文档

以下文件与代码行为强相关，修改功能时**同 PR 内一并更新**：

- `README.md` / `README_EN.md`：端点格式、配置项、环境变量表、目录结构
- `docs/mcp.md`：MCP 工具与权限口径
- `.github/release-template.md`：下次发版的说明（含破坏性变更与迁移方式）
- 首页/后台页面文案在 `templates.go` / `admin.go`，注意与 README 保持一致

### 💥 破坏性变更需谨慎

端点格式、配置项、持久化文件结构的变更属于破坏性变更。参考 v1.3.0 的处理方式：

1. 旧格式一律明确失败（404 / 401 + 引导文案），不做"智能兼容"
2. 发版说明写明**迁移方式**（用户该怎么做）
3. 后台「用法示例」/「链接转换」自动生成新格式，降低用户迁移成本

### 🧱 代码风格

- 单文件单职责，参考现有分层：`proxy.go` / `hf.go` / `offline.go`（服务）、`store.go` / `users.go` / `invite.go`（数据层）、`admin.go` / `templates.go`（后台）、`settings.go`（运行时设置）
- 持久化统一走「内存 + JSON 原子写」模式（见 `store.go` / `users.go`）
- 新功能配套单测（表驱动用例优先），涉及路径解析的必须有归属校验用例

## 本地自测提醒

`go test` 现有 **67 个 Test**（13 个文件），覆盖缓存失效、用户校验、邀请码生命周期、图形验证码、MCP 权限边界、代理用户名段归属等核心路径。但 CI 变绿 ≠ 端到端正确——涉及 HTTP 行为的改动，建议本地起实例（`go run .` + `curl`）验证后再提 PR；UI 改动建议在桌面 1280 与移动 393 两个宽度下各看一眼。
