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

# 3. 本地改代码，随时自测（比等 CI 更快）
go vet ./...
go test ./...      # 注意：当前仓库暂无单测，go test 会“空跑通过”
go build ./...

# 4. 提交并推送分支
git add -A && git commit -m "feat: 简要说明"
git push -u origin feat/你的功能

# 5. 开 PR 到 main
gh pr create --base main --title "feat: ..." --body "..."
```

## 合并门禁（main 分支保护）

PR 合入 `main` 前必须满足：

- ✅ **CI 必过**：`ci.yml` 会在 PR 上自动运行 `go vet` / `go test` / `go build`，对应保护规则里的必过检查 `CI`。
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

## 本地自测提醒

当前 `go test` 为**空跑通过**（仓库暂无单元测试），因此 CI 变绿 ≠ 行为正确。在补充单测之前，请务必在本地手动验证功能后再提 PR。
