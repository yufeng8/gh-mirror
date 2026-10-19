package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// MCP（Model Context Protocol）服务端：让 AI Agent 通过标准协议调用网关能力。
// 协议为 JSON-RPC 2.0，标准库手写，零第三方依赖。
// 两种传输共用同一 dispatcher：HTTP POST /mcp 与 `gh-mirror mcp` stdio 子命令。
// stdio 模式下所有日志必须写 stderr，stdout 只允许协议 JSON。

const (
	mcpDefaultProtocol = "2024-11-05" // initialize 缺省协议版本（优先回显客户端版本）
	mcpServerName      = "gh-mirror"
	aiPrefix           = "/ai/"       // AI 接入区前缀：/ai/<用户名>/mcp 为 MCP 端点（多用户隔离）
)

// ---- JSON-RPC 2.0 ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

func okResp(id json.RawMessage, result any) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Result: result}
}

func errResp(id json.RawMessage, code int, msg string) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

// ---- 工具 ----

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpCallResult struct {
	Content []mcpToolContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// owner 为调用方（鉴权用高级 Token）的归属用户名，供 create_token 等需要归属的操作使用。
	handler func(s *Server, base string, args map[string]any, owner string) mcpCallResult
}

// mcpOwner 返回鉴权 token 的归属用户名（空 owner 归默认管理员）。
func (s *Server) mcpOwner(tok string) string {
	if t, ok := s.store.Get(tok); ok && t.Owner != "" {
		return t.Owner
	}
	return s.adminUser
}

// mcpUserRole 返回 MCP 调用者（owner 用户名）的用户角色；用户不存在时按默认管理员兜底为 super。
func (s *Server) mcpUserRole(owner string) string {
	if u, ok := s.users.Get(owner); ok {
		return u.Role
	}
	if owner == s.adminUser {
		return RoleSuper
	}
	return RoleNormal
}

// mcpIsSuper 判断 MCP 调用者是否为超级管理员（实例级操作的工具用，与网页端 requireSuper 对齐）。
func (s *Server) mcpIsSuper(owner string) bool {
	return s.mcpUserRole(owner) == RoleSuper
}

func textResult(text string) mcpCallResult {
	return mcpCallResult{Content: []mcpToolContent{{Type: "text", Text: text}}}
}

func errorResult(text string) mcpCallResult {
	return mcpCallResult{Content: []mcpToolContent{{Type: "text", Text: text}}, IsError: true}
}

// jsonResult 输出「一行摘要 + ```json 代码块」。AI Agent 解析 JSON 比解析散文稳定得多，
// 摘要保留人类可读性，兼顾「给人看」与「给模型读」。
func jsonResult(summary string, data any) mcpCallResult {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return errorResult("结果序列化失败: " + err.Error())
	}
	return textResult(summary + "\n\n```json\n" + string(b) + "\n```")
}

// mcpServerVersion 返回对外声明的服务端版本（与后台「当前版本」一致，由构建期注入）。
func mcpServerVersion() string {
	if strings.TrimSpace(version) == "" {
		return "dev"
	}
	return version
}

// mcpProtoAtLeast 判断协议版本是否 >= 给定版本（按 "YYYY-MM-DD" 字典序比较即可）。
func mcpProtoAtLeast(v, min string) bool {
	v = strings.TrimSpace(v)
	return len(v) >= len(min) && v >= min
}

// mcpInstructions 是 initialize 的 instructions 字段（MCP 2025-06-18 起）：
// 客户端会把它展示给用户/模型，写清楚用法能显著降低误调用。
const mcpInstructions = `gh-mirror 自托管加速网关的 MCP 接入点（Streamable HTTP，无状态；每次调用用 Authorization: Bearer <高级Token> 鉴权，Token 必须属于路径中的用户）。

推荐流程：
1) convert_link：把 GitHub / HuggingFace 链接或 Docker 镜像名换成本站加速链接。支持省略 https:// 的写法（如 github.com/owner/repo/releases/download/v1/a.zip），链接中的 Token 取调用者自己的第一个 Token。
2) search_images / list_tags：在 Docker Hub 找镜像与版本号，把结果（如 library/nginx:1.25）交给 convert_link 生成 /offline/ 离线 tar 地址。
3) list_tokens / create_token / delete_token：管理自己的 Token。role=user 仅用于代理加速；role=admin 为高级 Token，才能作为本 MCP 的接入凭证。
4) health：诊断实例是否存活。get_services / set_service / get_cache_stats / set_cache_limits / clear_cache：服务开关与缓存管理。

注意：
- 所有结果都是文本；结构化数据放在 json 代码块中（形如 json 围栏），解析时取该代码块。
- 调用失败时 isError=true，错误文本已说明原因与修正方式。
- set_service / set_cache_limits / clear_cache / delete_token 属破坏性操作，即时生效且不可撤销。
- 服务开关与缓存管理仅超级管理员可用；普通用户的 tools/list 不含这些工具。`

// mcpToolNames 返回全部工具名（用于「未知工具」错误提示，帮助模型自纠）。
func mcpToolNames() string {
	names := make([]string, 0, len(mcpRegistry))
	for _, t := range mcpRegistry {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

// mcpAdminOnlyTools 列出仅超级管理员可用的工具（服务开关与缓存管理）。
var mcpAdminOnlyTools = map[string]bool{
	"set_service": true, "get_cache_stats": true,
	"set_cache_limits": true, "clear_cache": true,
}

// mcpVisibleTools 按调用者角色返回可见工具与被隐藏的数量：普通用户看不到仅超管可用的
// 管理工具，避免 Agent 把轮次浪费在必然被拒的调用上；tools/list 实时返回，权限变更后
// 重新连接即可拿到最新清单。
func mcpVisibleTools(s *Server, owner string) ([]mcpTool, int) {
	if s.mcpIsSuper(owner) {
		return mcpRegistry, 0
	}
	out := make([]mcpTool, 0, len(mcpRegistry))
	hidden := 0
	for _, t := range mcpRegistry {
		if mcpAdminOnlyTools[t.Name] {
			hidden++
			continue
		}
		out = append(out, t)
	}
	return out, hidden
}

func objSchema(props map[string]any, required []string) map[string]any {
	sch := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sch["required"] = required
	}
	return sch
}

func propStr(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func propInt(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func propBool(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// propEnum 枚举字符串参数：给出 enum 后，模型不会盲猜取值（如把 github 传给 set_service）。
func propEnum(desc string, values ...string) map[string]any {
	p := propStr(desc)
	p["enum"] = values
	return p
}

// propDefaultInt 带默认值的整数参数，帮助模型省略时选对。
func propDefaultInt(desc string, def int) map[string]any {
	p := propInt(desc)
	p["default"] = def
	return p
}

func argStr(args map[string]any, k string) string {
	if v, ok := args[k].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(args map[string]any, k string, def int) int {
	switch v := args[k].(type) {
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func argBool(args map[string]any, k string, def bool) bool {
	switch v := args[k].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		if s == "1" || s == "true" || s == "on" {
			return true
		}
		if s == "0" || s == "false" || s == "off" {
			return false
		}
	}
	return def
}

// mcpRegistry 是全部工具的注册表。handler 为非导出字段，tools/list 输出时不会泄露。
var mcpRegistry = []mcpTool{
	{
		Name:        "health",
		Description: "健康检查：确认网关实例是否存活。返回：JSON（status/uptime/listen/admin_path/base_url）。排查「连接失败」「无响应」类问题时先调它。无副作用。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			return jsonResult("ok：网关实例存活。", map[string]any{
				"status":     "ok",
				"uptime":     fmtDuration(time.Since(processStart)),
				"listen":     s.cfg.Listen,
				"admin_path": s.adminPath,
				"base_url":   base,
				"user":       owner,
			})
		},
	},
	{
		Name:        "list_tokens",
		Description: "列出调用者自己的 Token（明文、类型、命中数、流量、创建与最近使用时间）。返回：JSON 数组。任何角色都只返回自己名下的 Token。用于确认可用 Token 或排查「链接里的 token 是否还有效」。无副作用。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			toks := s.tokensForUser(s.store.All(), owner, s.mcpUserRole(owner))
			type row struct {
				Token     string `json:"token"`
				Note      string `json:"note"`
				Role      string `json:"role"`
				Hits      int64  `json:"hits"`
				Bytes     int64  `json:"bytes"`
				CreatedAt string `json:"created_at"`
				LastUsed  string `json:"last_used"`
			}
			rows := make([]row, 0, len(toks))
			for _, t := range toks {
				lu := "—"
				if t.LastUsed > 0 {
					lu = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
				}
				note := t.Note
				if note == "" {
					note = "—"
				}
				rows = append(rows, row{t.Token, note, normRole(t.Role), t.Hits, t.Bytes,
					time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"), lu})
			}
			if len(rows) == 0 {
				return textResult("你名下还没有 Token。可调 create_token 创建一个（role=user 用于代理加速，role=admin 才能作为 MCP 凭证）。")
			}
			return jsonResult(fmt.Sprintf("共 %d 个 Token（role=admin 为高级 Token，可用于 MCP 接入）：", len(rows)), rows)
		},
	},
	{
		Name:        "create_token",
		Description: "创建一个新 Token。role=user 为普通 Token（仅用于拼加速链接）；role=admin 为高级 Token（可用于 MCP 接入，权限更高）。返回：新 Token 明文。示例：{\"note\":\"CI 构建\",\"role\":\"user\"}。无副作用。",
		InputSchema: objSchema(map[string]any{
			"token": propStr("自定义 token 值；留空自动生成 32 位十六进制"),
			"note":  propStr("备注，用于区分用途，如 CI / 个人笔记本"),
			"role":  propEnum("角色：user=普通（仅代理），admin=高级（可用于 MCP）", "user", "admin"),
		}, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			tok := argStr(args, "token")
			if tok == "" {
				tok = GenToken()
			}
			if s.store.Has(tok) {
				return errorResult("该 token 已存在，请换一个 token 值或留空让它自动生成。")
			}
			roleArg := strings.ToLower(argStr(args, "role"))
			if roleArg != "" && roleArg != "user" && roleArg != "admin" {
				return errorResult("role 仅支持 user 或 admin；不确定时用 user。")
			}
			role := normRole(roleArg)
			s.store.Add(tok, argStr(args, "note"), role, owner)
			log.Printf("MCP 创建 token: role=%s", role)
			return textResult(fmt.Sprintf("已创建 Token（角色 %s），请妥善保存，之后不会再次完整展示：\n%s", role, tok))
		},
	},
	{
		Name:        "delete_token",
		Description: "删除指定 Token。⚠ 破坏性：删除后所有使用该 Token 的加速链接立即失效，且不可恢复。权限：仅能删除自己的 Token（超级管理员可删除全部）。返回：删除结果。",
		InputSchema: objSchema(map[string]any{"token": propStr("要删除的 Token 明文；先用 list_tokens 确认")}, []string{"token"}),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			tok := argStr(args, "token")
			if tok == "" {
				return errorResult("缺少参数 token：先用 list_tokens 查看可删除的 Token。")
			}
			t, exists := s.store.Get(tok)
			if !exists {
				return errorResult("token 不存在：可能已删除或拼写有误，可用 list_tokens 核对。")
			}
			if !s.tokenVisible(t, owner, s.mcpUserRole(owner)) {
				return errorResult("无权删除该 token（仅能删除自己的；如需删除他人 Token 请联系超级管理员）。")
			}
			s.store.Delete(tok)
			log.Printf("MCP 删除 token")
			return textResult("已删除 token: " + tok)
		},
	},
	{
		Name:        "get_services",
		Description: "查询三个加速服务的启用状态。返回：JSON（github 恒为 true；docker / hf 为实际开关）。排查「某个资源加速不可用」时先调它。无副作用。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			return jsonResult("当前服务开关状态：", map[string]any{
				"github": true, "docker": s.dockerEnabled(), "hf": s.hfEnabled(),
			})
		},
	},
	{
		Name:        "set_service",
		Description: "开启或关闭 Docker / HuggingFace 加速。⚠ 破坏性：即时生效并持久化到 settings.json，会立刻影响全部用户，且不可撤销。权限：仅超级管理员。返回：切换后的状态。示例：{\"name\":\"docker\",\"enabled\":true}",
		InputSchema: objSchema(map[string]any{
			"name":    propEnum("服务名：docker（离线包与 registry 加速）或 hf（HuggingFace 权重加速）；github 恒开无需设置", "docker", "hf"),
			"enabled": propBool("true 开启 / false 关闭"),
		}, []string{"name", "enabled"}),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			if !s.mcpIsSuper(owner) {
				return errorResult("需要超级管理员权限（服务开关仅超级管理员可操作）。可用 get_services 查看当前状态。")
			}
			name := strings.ToLower(argStr(args, "name"))
			if name != "docker" && name != "hf" {
				return errorResult("name 仅支持 docker 或 hf（github 恒为开启，无需设置）。")
			}
			raw, ok := args["enabled"]
			if !ok {
				return errorResult("缺少参数 enabled：true 开启、false 关闭。")
			}
			enabled := argBool(args, "enabled", false)
			if _, isBool := raw.(bool); !isBool {
				if _, isStr := raw.(string); !isStr {
					return errorResult("enabled 必须是布尔值 true 或 false。")
				}
			}
			if err := s.setService(name, enabled); err != nil {
				return errorResult(err.Error())
			}
			state := "已关闭"
			if enabled {
				state = "已开启"
			}
			log.Printf("MCP 服务开关: %s -> %v", name, enabled)
			return textResult(fmt.Sprintf("%s %s（即时生效，已持久化到 settings.json）", name, state))
		},
	},
	{
		Name:        "get_cache_stats",
		Description: "查询缓存占用与限额（已用容量、单文件上限、总上限、时效、缓存目录）。返回：JSON。权限：仅超级管理员。排查「下载慢」「反复回源」时有用。无副作用。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			if !s.mcpIsSuper(owner) {
				return errorResult("需要超级管理员权限（缓存管理仅超级管理员可操作）。")
			}
			maxTotal := s.cache.MaxTotalBytes()
			totalTxt := "不限"
			if maxTotal > 0 {
				totalTxt = formatBytes(maxTotal)
			}
			ttl := s.cache.TTL()
			ttlTxt := "永不过期"
			if ttl > 0 {
				ttlTxt = fmt.Sprintf("%d 分钟", int(ttl/time.Minute))
			}
			return jsonResult("当前缓存状态：", map[string]any{
				"已用":         formatBytes(s.cache.UsageBytes()),
				"单文件上限":     fmt.Sprintf("%d MB", int(s.cache.MaxBytes()/(1024*1024))),
				"总上限":       totalTxt,
				"时效":        ttlTxt,
				"缓存目录":      s.cfg.CacheDir,
				"自动失效周期":   fmt.Sprintf("%d 分钟", int(cacheSweepInterval/time.Minute)),
			})
		},
	},
	{
		Name:        "set_cache_limits",
		Description: "调整缓存限额与时效。⚠ 破坏性：即时生效并持久化；上限过小会导致频繁回源下载。权限：仅超级管理员。未提供的项保持不变，至少传一个参数。返回：调整后的值。",
		InputSchema: objSchema(map[string]any{
			"maxFileMB":  propInt("单文件缓存上限 MB，0 = 不限制"),
			"maxTotalMB": propInt("缓存总上限 MB，0 = 不限（超限按最旧优先清理）"),
			"ttlMinutes": propInt("缓存时效分钟，0 = 永不过期"),
		}, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			if !s.mcpIsSuper(owner) {
				return errorResult("需要超级管理员权限（缓存管理仅超级管理员可操作）。")
			}
			mf := argInt(args, "maxFileMB", -1)
			mt := argInt(args, "maxTotalMB", -1)
			tt := argInt(args, "ttlMinutes", -1)
			if mf < 0 && mt < 0 && tt < 0 {
				return errorResult("至少提供一个参数：maxFileMB（单文件 MB）/ maxTotalMB（总量 MB）/ ttlMinutes（时效分钟）。")
			}
			_ = s.setCacheLimits(mf, mt, tt)
			log.Printf("MCP 缓存参数调整: 单文件=%dMB 总计=%dMB 时效=%d分钟",
				int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute))
			return textResult("缓存参数已更新并持久化：" + fmt.Sprintf("单文件=%dMB 总计=%dMB 时效=%d分钟",
				int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute)))
		},
	},
	{
		Name:        "clear_cache",
		Description: "清空缓存目录内的所有文件。⚠ 破坏性：缓存不可恢复，之后所有请求都会重新回源下载（可能变慢）。权限：仅超级管理员。不影响 Token 与服务设置。返回：清除的文件数与体积。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			if !s.mcpIsSuper(owner) {
				return errorResult("需要超级管理员权限（缓存管理仅超级管理员可操作）。")
			}
			files, bytes := s.cache.Clear()
			log.Printf("MCP 清除缓存: %d 个文件，%s", files, formatBytes(bytes))
			return textResult(fmt.Sprintf("已清除缓存：%d 个文件，%s", files, formatBytes(bytes)))
		},
	},
	{
		Name:        "convert_link",
		Description: "把 GitHub / HuggingFace 链接或 Docker 镜像名转换为本站加速链接（与后台「链接转换」同一实现）。支持省略 https:// 的写法（如 github.com/owner/repo/releases/download/v1/a.zip）。返回：加速链接 URL（其中的 Token 取调用者自己的第一个 Token）。示例：{\"url\":\"library/nginx:1.25\"}。无副作用。",
		InputSchema: objSchema(map[string]any{
			"url": propStr("GitHub / HuggingFace 原始链接，或 Docker 镜像名如 library/nginx:1.25"),
		}, []string{"url"}),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			raw := argStr(args, "url")
			if raw == "" {
				return errorResult("缺少参数 url：传 GitHub / HuggingFace 链接，或 Docker 镜像名（如 library/nginx:1.25）。")
			}
			// 取调用者自己的 token（按归属过滤），避免生成他人 token 的链接
			tk := ""
			if toks := s.tokensForUser(s.store.All(), owner, s.mcpUserRole(owner)); len(toks) > 0 {
				tk = toks[0].Token
			}
			res := s.buildAccelURL(base, raw, tk, owner)
			if res.URL == "" {
				// 把后台文案改写成 MCP 语境，告诉模型下一步该调什么
				hint := strings.ReplaceAll(res.Hint, "请先在「Token 管理」添加一个 token",
					"你名下还没有可用 Token：先调用 create_token 创建一个（role=user 即可用于生成加速链接）")
				hint = strings.ReplaceAll(hint, "请先在「服务管理」开启", "服务未开启，需超级管理员调用 set_service 开启（docker 或 hf）")
				return errorResult(hint)
			}
			return textResult(res.URL)
		},
	},
	{
		Name:        "search_images",
		Description: "在 Docker Hub 搜索镜像，用于挑选要加速的镜像名。返回：JSON（query/total/returned/results[]，每项含 name、stars、pulls、official、updated、description）。示例：{\"q\":\"nginx\",\"limit\":5}。无副作用。",
		InputSchema: objSchema(map[string]any{
			"q":     propStr("搜索关键词，如 nginx、redis、bitnami/postgresql"),
			"page":  propDefaultInt("结果页码，从 1 开始", 1),
			"limit": propDefaultInt("最多返回条数（1-25）", 10),
		}, []string{"q"}),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			q := argStr(args, "q")
			if q == "" {
				return errorResult("缺少参数 q：请传搜索关键词，例如 {\"q\":\"nginx\"}")
			}
			page := argInt(args, "page", 1)
			if page < 1 {
				page = 1
			}
			limit := argInt(args, "limit", 10)
			if limit < 1 {
				limit = 1
			}
			if limit > 25 {
				limit = 25
			}
			target := fmt.Sprintf("%s/v2/search/repositories/?query=%s&page=%d&page_size=%d",
				hubAPI, url.QueryEscape(q), page, limit)
			body, _, status, err := hubGetCtx(context.Background(), target)
			if err != nil {
				return errorResult("上游请求失败（本实例需能访问 Docker Hub）: " + err.Error())
			}
			if status != http.StatusOK {
				return errorResult(fmt.Sprintf("Docker Hub 返回 HTTP %d，可稍后重试或换一个关键词", status))
			}
			var parsed hubSearchResp
			if err := json.Unmarshal(body, &parsed); err != nil {
				return errorResult("上游返回无法解析: " + err.Error())
			}
			// 复用 /api/search 的规范化逻辑：name 取 repo_name（否则为空）、updated 取 last_updated
			items := normalizeSearchResults(parsed)
			if len(items) > limit {
				items = items[:limit]
			}
			if len(items) == 0 {
				return textResult(fmt.Sprintf("关键词 %q 没有匹配到镜像。可换个关键词，或确认镜像名后用 convert_link 直接生成加速链接。", q))
			}
			return jsonResult(fmt.Sprintf("关键词 %q 共命中 %d 个镜像，返回前 %d 个：", q, parsed.Count, len(items)),
				map[string]any{
					"query": q, "total": parsed.Count, "returned": len(items),
					"results": items,
				})
		},
	},
	{
		Name:        "list_tags",
		Description: "列出 Docker Hub 上某镜像仓库的全部标签（版本）与压缩后大小，用于确定要加速的版本号。返回：JSON（repo/total/tags[]）。示例：{\"repo\":\"library/nginx\"}。无副作用。",
		InputSchema: objSchema(map[string]any{"repo": propStr("仓库名（不含 tag），如 library/nginx、bitnami/postgresql")}, []string{"repo"}),
		handler: func(s *Server, base string, args map[string]any, owner string) mcpCallResult {
			repo := argStr(args, "repo")
			if repo == "" {
				return errorResult("缺少参数 repo：例如 {\"repo\":\"library/nginx\"}")
			}
			if !validRepo(repo) {
				return errorResult("repo 不合法：只允许字母数字与 . _ - 和 / 分隔，例如 library/nginx")
			}
			target := fmt.Sprintf("%s/v2/repositories/%s/tags?page_size=50", hubAPI, repo)
			body, _, status, err := hubGetCtx(context.Background(), target)
			if err != nil {
				return errorResult("上游请求失败（本实例需能访问 Docker Hub）: " + err.Error())
			}
			if status != http.StatusOK {
				return errorResult(fmt.Sprintf("Docker Hub 返回 HTTP %d，仓库可能不存在或无标签", status))
			}
			var parsed struct {
				Results []struct {
					Name     string `json:"name"`
					FullSize int64  `json:"full_size"`
				} `json:"results"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return errorResult("上游返回无法解析: " + err.Error())
			}
			if len(parsed.Results) == 0 {
				return textResult(fmt.Sprintf("%s 没有标签返回，仓库名可能拼写错误。", repo))
			}
			tags := make([]map[string]any, 0, len(parsed.Results))
			for _, r := range parsed.Results {
				tags = append(tags, map[string]any{
					"tag": r.Name,
					"size": humanSize(r.FullSize),
					"image": repo + ":" + r.Name,
				})
			}
			return jsonResult(fmt.Sprintf("%s 共 %d 个标签（已按大小降序取前 %d 个，可直接用 image 字段去 convert_link）：", repo, len(tags), len(tags)),
				map[string]any{"repo": repo, "total": len(tags), "tags": tags})
		},
	},
}

// mcpDispatch 分发一个 JSON-RPC 请求。返回 nil 表示通知类方法（不回响应）。
func mcpDispatch(s *Server, base string, owner string, req rpcReq) *rpcResp {
	switch req.Method {
	case "initialize":
		ver := mcpDefaultProtocol
		if len(req.Params) > 0 {
			var p struct{ ProtocolVersion string `json:"protocolVersion"` }
			if err := json.Unmarshal(req.Params, &p); err == nil && p.ProtocolVersion != "" {
				ver = p.ProtocolVersion
			}
		}
		result := map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": mcpServerName, "version": mcpServerVersion()},
		}
		// instructions 字段自 MCP 2025-06-18 起支持：客户端会展示给用户/模型，
		// 是让 AI Agent 正确使用本服务端最有效的一处投入。
		if mcpProtoAtLeast(ver, "2025-06-18") {
			result["instructions"] = mcpInstructions
		}
		res := okResp(req.ID, result)
		return &res
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		res := okResp(req.ID, map[string]any{})
		return &res
	case "tools/list":
		list, hidden := mcpVisibleTools(s, owner)
		result := map[string]any{"tools": list}
		if hidden > 0 {
			result["note"] = fmt.Sprintf("当前账号为普通用户，%d 个仅超级管理员可用的工具（服务开关 / 缓存管理）已隐藏。", hidden)
		}
		res := okResp(req.ID, result)
		return &res
	case "resources/list":
		// 本服务端不提供 resources；显式返回空列表，避免客户端探测时报错
		res := okResp(req.ID, map[string]any{"resources": []any{}})
		return &res
	case "resources/templates/list":
		res := okResp(req.ID, map[string]any{"resourceTemplates": []any{}})
		return &res
	case "prompts/list":
		res := okResp(req.ID, map[string]any{"prompts": []any{}})
		return &res
	case "logging/setLevel":
		// 接受但无需实现（日志固定写 stderr）
		res := okResp(req.ID, map[string]any{})
		return &res
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				res := errResp(req.ID, rpcInvalidParams, "params 解析失败: "+err.Error())
				return &res
			}
		}
		if p.Name == "" {
			res := errResp(req.ID, rpcInvalidParams, "缺少工具名 name。可用工具："+mcpToolNames())
			return &res
		}
		for _, t := range mcpRegistry {
			if t.Name == p.Name {
				res := okResp(req.ID, t.handler(s, base, p.Arguments, owner))
				return &res
			}
		}
		// 未知工具时列出可用工具名，模型可自行纠正
		res := errResp(req.ID, rpcInvalidParams, "未知工具 "+p.Name+"，可用工具："+mcpToolNames())
		return &res
	default:
		res := errResp(req.ID, rpcMethodNotFound,
			"不支持的方法 "+req.Method+"，本服务端支持：initialize、ping、tools/list、tools/call、resources/list、prompts/list")
		return &res
	}
}

// serveMCPRPC 解析请求体（单个对象或 JSON 数组批量），逐个分发并通过 emit 回写。
func (s *Server) serveMCPRPC(base string, owner string, body []byte, emit func(*rpcResp)) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		e := errResp(nil, rpcInvalidRequest, "空请求")
		emit(&e)
		return
	}
	if strings.HasPrefix(trimmed, "[") {
		var reqs []rpcReq
		if err := json.Unmarshal([]byte(trimmed), &reqs); err != nil {
			e := errResp(nil, rpcParseError, "JSON 解析失败: "+err.Error())
			emit(&e)
			return
		}
		if len(reqs) == 0 {
			e := errResp(nil, rpcInvalidRequest, "空批量请求")
			emit(&e)
			return
		}
		for _, req := range reqs {
			s.emitMCPOne(base, owner, req, emit)
		}
		return
	}
	var req rpcReq
	if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
		e := errResp(nil, rpcParseError, "JSON 解析失败: "+err.Error())
		emit(&e)
		return
	}
	s.emitMCPOne(base, owner, req, emit)
}

// emitMCPOne 分发单个请求；对工具处理中的 panic 兜底，避免中断整个传输。
func (s *Server) emitMCPOne(base string, owner string, req rpcReq, emit func(*rpcResp)) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("MCP 处理异常: %v", rec)
			e := errResp(req.ID, rpcInternalError, "内部错误")
			emit(&e)
		}
	}()
	if resp := mcpDispatch(s, base, owner, req); resp != nil {
		emit(resp)
	}
}

// mcpBearerToken 从 Authorization 头提取 Bearer token。
func mcpBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// aiMCPUser 从 /ai/<用户名>/mcp 路径解析并校验用户段，返回归属用户名。
// 不匹配新格式（缺 /ai/ 前缀、第二段不是 mcp、用户不存在）时返回 ok=false。
func (s *Server) aiMCPUser(path string) (string, bool) {
	rest := strings.TrimPrefix(path, aiPrefix)
	if rest == path || rest == "" {
		return "", false
	}
	parts := strings.SplitN(rest, "/", 2)
	seg := strings.TrimSpace(parts[0])
	if seg == "" || len(parts) < 2 || strings.Trim(parts[1], "/") != "mcp" {
		return "", false
	}
	user, ok := s.userByDockerSegment(seg)
	if !ok {
		return "", false
	}
	return user, true
}

// mcpAccepts 判断客户端 Accept 头是否接受指定媒体类型。
func mcpAccepts(header, media string) bool {
	return strings.Contains(strings.ToLower(header), media)
}

// mcpWriteError 输出 JSON 格式的 HTTP 错误（带接入 hint，便于 Agent / 人快速定位）。
func (s *Server) mcpWriteError(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	b, _ := json.Marshal(map[string]any{
		"error": detail,
		"hint":  "端点 POST /ai/<用户名>/mcp；鉴权头 Authorization: Bearer <高级Token>（Token 需归属该用户）",
	})
	_, _ = w.Write(b)
}

// handleMCP 处理 /ai/<用户名>/mcp（Streamable HTTP，无状态）。
// 多用户隔离（与 /g /hf /offline 同模型）：路径用户名段反查用户表，
// Bearer 高级 Token 必须归属该用户名，owner 以路径用户为准。
//
// 响应格式按客户端 Accept 协商（规范 2025-03-26 起）：
//   - 同时接受 application/json 与 text/event-stream（绝大多数客户端）→ 返回单个 JSON
//   - 只接受 text/event-stream（部分客户端强依赖）→ 返回 text/event-stream 事件流
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 无状态实现不提供服务端主动推送流，按规范返回 405 并说明正确用法
		s.mcpWriteError(w, http.StatusMethodNotAllowed,
			"本 MCP 服务端为无状态实现，不提供 SSE 通知流；请用 POST 发送 JSON-RPC（tools/list、tools/call）。")
		return
	case http.MethodDelete:
		// 无会话可终止：返回 200 让客户端静默收尾（规范允许 405，这里选择更友好）
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","detail":"无状态服务端，无需终止会话"}`))
		return
	case http.MethodPost:
	default:
		s.mcpWriteError(w, http.StatusMethodNotAllowed, "仅支持 POST（JSON-RPC 2.0 over HTTP）")
		return
	}
	path := r.URL.Path
	if !strings.HasPrefix(path, aiPrefix) {
		// 旧端点 /mcp 已废弃：给出明确引导，便于更新 Agent 客户端配置
		s.mcpWriteError(w, http.StatusUnauthorized,
			"MCP 端点已改为 /ai/<用户名>/mcp 形式（需带用户名段），请更新客户端配置。")
		return
	}
	user, ok := s.aiMCPUser(path)
	if !ok {
		s.render404(w, r)
		return
	}
	tok := mcpBearerToken(r)
	if !s.store.IsAdmin(tok) {
		s.mcpWriteError(w, http.StatusUnauthorized, "未授权：需要高级 Token（Authorization: Bearer <token>）")
		return
	}
	// token 必须归属路径里的用户名（空 owner 归默认管理员）
	t, exists := s.store.Get(tok)
	if !exists || (t.Owner != user && !(t.Owner == "" && user == s.adminUser)) {
		s.mcpWriteError(w, http.StatusUnauthorized, "未授权：该 Token 不属于路径中的用户")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		resp := errResp(nil, rpcParseError, "读取请求失败")
		s.writeMCPJSON(w, &resp)
		return
	}
	// 仅要求 text/event-stream 时按 SSE 返回；两种都接受时优先 JSON（兼容性最好）
	if mcpAccepts(r.Header.Get("Accept"), "text/event-stream") &&
		!mcpAccepts(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		sent := 0
		s.serveMCPRPC(baseOf(r), user, body, func(resp *rpcResp) {
			b, _ := json.Marshal(resp)
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			sent++
			if flusher != nil {
				flusher.Flush()
			}
		})
		if sent == 0 {
			// 纯通知类请求也要结束响应流，否则客户端会一直等待
			_, _ = fmt.Fprint(w, ": no response (notification)\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		return
	}
	s.serveMCPRPC(baseOf(r), user, body, func(resp *rpcResp) { s.writeMCPJSON(w, resp) })
}

func (s *Server) writeMCPJSON(w http.ResponseWriter, resp *rpcResp) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, _ := json.Marshal(resp)
	_, _ = w.Write(b)
}

// serveMCPStdio 以 stdio 传输运行 MCP：逐行读 JSON-RPC、逐行写响应。
// 日志一律写 stderr，stdout 只输出协议 JSON。
func serveMCPStdio(s *Server, token, base string) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if !s.store.IsAdmin(token) {
			log.Printf("MCP stdio: token 无效或不是高级 Token")
			resp := errResp(nil, rpcInvalidRequest, "未授权：token 无效或不是高级 Token")
			writeMCPLine(out, &resp)
			out.Flush()
			continue
		}
		s.serveMCPRPC(base, s.mcpOwner(token), []byte(line), func(resp *rpcResp) { writeMCPLine(out, resp) })
		out.Flush()
	}
	return in.Err()
}

func writeMCPLine(w io.Writer, resp *rpcResp) {
	b, _ := json.Marshal(resp)
	_, _ = w.Write(append(b, '\n'))
}

// runMCPStdio 解析 `gh-mirror mcp` 子命令参数并以 stdio 传输运行。
func runMCPStdio(s *Server) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	token := fs.String("token", "", "高级 Token（也可设 GM_MCP_TOKEN 环境变量）")
	base := fs.String("base", "", "生成加速链接用的基地址，默认由 LISTEN 推导")
	_ = fs.Parse(os.Args[2:])
	if *token == "" {
		*token = os.Getenv("GM_MCP_TOKEN")
	}
	if *base == "" {
		*base = defaultBase(s.cfg.Listen)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "缺少高级 Token：请使用 --token <token> 或设置 GM_MCP_TOKEN")
		fmt.Fprintln(os.Stderr, "提示：在管理后台创建 token 时把类型选为「高级 Token（可用于 MCP）」（内部值 admin）。")
		os.Exit(2)
	}
	// stdout 仅供协议使用
	log.SetOutput(os.Stderr)
	if err := serveMCPStdio(s, *token, *base); err != nil {
		log.Printf("MCP stdio 结束: %v", err)
	}
}

// defaultBase 由监听地址推导默认基地址（stdio 模式下没有 HTTP 请求可参照）。
// 0.0.0.0 / 缺省主机段归一为 127.0.0.1——0.0.0.0 不是可访问地址，
// 直接拼接会生成 http://0.0.0.0:8080 这类无效链接（容器部署 LISTEN 常为 0.0.0.0:8080）。
func defaultBase(listen string) string {
	host := listen
	if i := strings.LastIndex(host, ":"); i >= 0 {
		if h := host[:i]; h == "" || h == "0.0.0.0" {
			host = "127.0.0.1" + host[i:]
		}
	}
	return "http://" + host
}
