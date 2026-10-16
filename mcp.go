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
	mcpServerVersion   = "0.2.0"
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
	handler     func(s *Server, base string, args map[string]any) mcpCallResult
}

func textResult(text string) mcpCallResult {
	return mcpCallResult{Content: []mcpToolContent{{Type: "text", Text: text}}}
}

func errorResult(text string) mcpCallResult {
	return mcpCallResult{Content: []mcpToolContent{{Type: "text", Text: text}}, IsError: true}
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
		Name: "health", Description: "健康检查：返回服务状态与运行时长。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			return textResult(fmt.Sprintf("ok\n运行时长: %s\n监听: %s\n后台路径: %s",
				fmtDuration(time.Since(processStart)), s.cfg.Listen, s.adminPath))
		},
	},
	{
		Name: "list_tokens", Description: "列出全部 Token（含角色、命中数、流量、创建与最近使用时间）。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			toks := s.store.All()
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
				rows = append(rows, row{t.Token, t.Note, normRole(t.Role), t.Hits, t.Bytes,
					time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"), lu})
			}
			b, _ := json.MarshalIndent(rows, "", "  ")
			return textResult(fmt.Sprintf("共 %d 个 token：\n%s", len(rows), string(b)))
		},
	},
	{
		Name: "create_token", Description: "创建新 Token。role 可为 user（默认，仅代理）或 admin（可用于 MCP 接入）。",
		InputSchema: objSchema(map[string]any{
			"token": propStr("指定 token 值，留空则自动生成"),
			"note":  propStr("备注说明"),
			"role":  propStr("角色：user 或 admin，默认 user"),
		}, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			tok := argStr(args, "token")
			if tok == "" {
				tok = GenToken()
			}
			if s.store.Has(tok) {
				return errorResult("该 token 已存在")
			}
			role := normRole(argStr(args, "role"))
			s.store.Add(tok, argStr(args, "note"), role)
			log.Printf("MCP 创建 token: role=%s", role)
			return textResult(fmt.Sprintf("已创建 token（角色 %s）：\n%s", role, tok))
		},
	},
	{
		Name: "delete_token", Description: "删除指定 Token。",
		InputSchema: objSchema(map[string]any{"token": propStr("要删除的 token")}, []string{"token"}),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			tok := argStr(args, "token")
			if !s.store.Has(tok) {
				return errorResult("token 不存在")
			}
			s.store.Delete(tok)
			log.Printf("MCP 删除 token")
			return textResult("已删除 token: " + tok)
		},
	},
	{
		Name: "get_services", Description: "查询三个服务的启用状态（GitHub 恒开；Docker / HuggingFace 可开关）。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			b, _ := json.MarshalIndent(map[string]any{
				"github": true, "docker": s.dockerEnabled(), "hf": s.hfEnabled(),
			}, "", "  ")
			return textResult(string(b))
		},
	},
	{
		Name: "set_service", Description: "开启或关闭 Docker / HuggingFace 加速（即时生效、无需重启，并持久化）。",
		InputSchema: objSchema(map[string]any{
			"name":    propStr("服务名：docker 或 hf"),
			"enabled": propBool("true 开启 / false 关闭"),
		}, []string{"name", "enabled"}),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			name := strings.ToLower(argStr(args, "name"))
			if name != "docker" && name != "hf" {
				return errorResult("name 仅支持 docker 或 hf")
			}
			enabled := argBool(args, "enabled", false)
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
		Name: "get_cache_stats", Description: "查询缓存：已用容量、单文件上限、总上限、时效与缓存目录。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
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
			b, _ := json.MarshalIndent(map[string]any{
				"已用":      formatBytes(s.cache.UsageBytes()),
				"单文件上限":   fmt.Sprintf("%d MB", int(s.cache.MaxBytes()/(1024*1024))),
				"总上限":     totalTxt,
				"时效":      ttlTxt,
				"缓存目录":    s.cfg.CacheDir,
				"自动失效周期": fmt.Sprintf("%d 分钟", int(cacheSweepInterval/time.Minute)),
			}, "", "  ")
			return textResult(string(b))
		},
	},
	{
		Name: "set_cache_limits", Description: "调整缓存参数（即时生效并持久化）。未提供某项则保持不变。",
		InputSchema: objSchema(map[string]any{
			"maxFileMB":  propInt("单文件缓存上限 MB，0 = 不限制"),
			"maxTotalMB": propInt("缓存总上限 MB，0 = 不限（超限按最旧优先清理）"),
			"ttlMinutes": propInt("缓存时效分钟，0 = 永不过期"),
		}, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			mf := argInt(args, "maxFileMB", -1)
			mt := argInt(args, "maxTotalMB", -1)
			tt := argInt(args, "ttlMinutes", -1)
			if mf < 0 && mt < 0 && tt < 0 {
				return errorResult("至少提供一个参数：maxFileMB / maxTotalMB / ttlMinutes")
			}
			_ = s.setCacheLimits(mf, mt, tt)
			log.Printf("MCP 缓存参数调整: 单文件=%dMB 总计=%dMB 时效=%d分钟",
				int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute))
			return textResult("缓存参数已更新并持久化：" + fmt.Sprintf("单文件=%dMB 总计=%dMB 时效=%d分钟",
				int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute)))
		},
	},
	{
		Name: "clear_cache", Description: "清空缓存目录内的所有文件（不影响 Token 与设置）。",
		InputSchema: objSchema(nil, nil),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			files, bytes := s.cache.Clear()
			log.Printf("MCP 清除缓存: %d 个文件，%s", files, formatBytes(bytes))
			return textResult(fmt.Sprintf("已清除缓存：%d 个文件，%s", files, formatBytes(bytes)))
		},
	},
	{
		Name: "convert_link", Description: "把原始链接或 Docker 镜像名转换为本站加速链接（与后台「链接转换」同一实现）。",
		InputSchema: objSchema(map[string]any{
			"url": propStr("GitHub / HuggingFace 原始链接，或 Docker 镜像名如 library/nginx:1.25"),
		}, []string{"url"}),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			raw := argStr(args, "url")
			if raw == "" {
				return errorResult("缺少参数 url")
			}
			tk := ""
			if toks := s.store.All(); len(toks) > 0 {
				tk = toks[0].Token
			}
			res := s.buildAccelURL(base, raw, tk)
			if res.URL == "" {
				return errorResult(res.Hint)
			}
			return textResult(res.URL)
		},
	},
	{
		Name: "search_images", Description: "在 Docker Hub 搜索镜像（等价 /api/search）。",
		InputSchema: objSchema(map[string]any{
			"q":    propStr("搜索关键词，如 nginx"),
			"page": propStr("页码，默认 1"),
		}, []string{"q"}),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			q := argStr(args, "q")
			page := argStr(args, "page")
			if page == "" {
				page = "1"
			}
			target := fmt.Sprintf("%s/v2/search/repositories/?query=%s&page=%s&page_size=25",
				hubAPI, url.QueryEscape(q), url.QueryEscape(page))
			body, _, status, err := hubGetCtx(context.Background(), target)
			if err != nil {
				return errorResult("上游请求失败: " + err.Error())
			}
			if status != http.StatusOK {
				return errorResult(fmt.Sprintf("上游返回 HTTP %d", status))
			}
			var parsed struct {
				Count   int `json:"count"`
				Results []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					StarCount   int    `json:"star_count"`
				} `json:"results"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return textResult(string(body))
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("关键词 %q 命中 %d 个：\n", q, parsed.Count))
			for i, r := range parsed.Results {
				if i >= 25 {
					break
				}
				desc := r.Description
				if len(desc) > 80 {
					desc = desc[:80] + "…"
				}
				sb.WriteString(fmt.Sprintf("- %s  ★%d  %s\n", r.Name, r.StarCount, desc))
			}
			return textResult(sb.String())
		},
	},
	{
		Name: "list_tags", Description: "列出 Docker Hub 上某仓库的标签（等价 /api/tags）。",
		InputSchema: objSchema(map[string]any{"repo": propStr("仓库名，如 library/nginx")}, []string{"repo"}),
		handler: func(s *Server, base string, args map[string]any) mcpCallResult {
			repo := argStr(args, "repo")
			if !validRepo(repo) {
				return errorResult("repo 不合法")
			}
			target := fmt.Sprintf("%s/v2/repositories/%s/tags?page_size=50", hubAPI, repo)
			body, _, status, err := hubGetCtx(context.Background(), target)
			if err != nil {
				return errorResult("上游请求失败: " + err.Error())
			}
			if status != http.StatusOK {
				return errorResult(fmt.Sprintf("上游返回 HTTP %d", status))
			}
			var parsed struct {
				Results []struct {
					Name     string `json:"name"`
					FullSize int64  `json:"full_size"`
				} `json:"results"`
			}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return textResult(string(body))
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("%s 的标签（%d 个）：\n", repo, len(parsed.Results)))
			for i, r := range parsed.Results {
				if i >= 50 {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s  %s\n", r.Name, humanSize(r.FullSize)))
			}
			return textResult(sb.String())
		},
	},
}

// mcpDispatch 分发一个 JSON-RPC 请求。返回 nil 表示通知类方法（不回响应）。
func mcpDispatch(s *Server, base string, req rpcReq) *rpcResp {
	switch req.Method {
	case "initialize":
		ver := mcpDefaultProtocol
		if len(req.Params) > 0 {
			var p struct{ ProtocolVersion string `json:"protocolVersion"` }
			if err := json.Unmarshal(req.Params, &p); err == nil && p.ProtocolVersion != "" {
				ver = p.ProtocolVersion
			}
		}
		res := okResp(req.ID, map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": mcpServerName, "version": mcpServerVersion},
		})
		return &res
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		res := okResp(req.ID, map[string]any{})
		return &res
	case "tools/list":
		res := okResp(req.ID, map[string]any{"tools": mcpRegistry})
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
			res := errResp(req.ID, rpcInvalidParams, "缺少工具名 name")
			return &res
		}
		for _, t := range mcpRegistry {
			if t.Name == p.Name {
				res := okResp(req.ID, t.handler(s, base, p.Arguments))
				return &res
			}
		}
		res := errResp(req.ID, rpcInvalidParams, "未知工具: "+p.Name)
		return &res
	default:
		res := errResp(req.ID, rpcMethodNotFound, "不支持的方法: "+req.Method)
		return &res
	}
}

// serveMCPRPC 解析请求体（单个对象或 JSON 数组批量），逐个分发并通过 emit 回写。
func (s *Server) serveMCPRPC(base string, body []byte, emit func(*rpcResp)) {
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
			s.emitMCPOne(base, req, emit)
		}
		return
	}
	var req rpcReq
	if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
		e := errResp(nil, rpcParseError, "JSON 解析失败: "+err.Error())
		emit(&e)
		return
	}
	s.emitMCPOne(base, req, emit)
}

// emitMCPOne 分发单个请求；对工具处理中的 panic 兜底，避免中断整个传输。
func (s *Server) emitMCPOne(base string, req rpcReq, emit func(*rpcResp)) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("MCP 处理异常: %v", rec)
			e := errResp(req.ID, rpcInternalError, "内部错误")
			emit(&e)
		}
	}()
	if resp := mcpDispatch(s, base, req); resp != nil {
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

// handleMCP 处理 POST /mcp（Streamable HTTP：单次请求/响应，不做 SSE 流式）。
// 鉴权：Bearer token 必须是 admin 角色（与普通代理用的 user token 区分）。
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
		return
	}
	if !s.store.IsAdmin(mcpBearerToken(r)) {
		http.Error(w, "未授权：需要 admin 角色的 token（Authorization: Bearer <token>）", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		resp := errResp(nil, rpcParseError, "读取请求失败")
		s.writeMCPJSON(w, &resp)
		return
	}
	s.serveMCPRPC(baseOf(r), body, func(resp *rpcResp) { s.writeMCPJSON(w, resp) })
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
			log.Printf("MCP stdio: token 无效或非 admin 角色")
			resp := errResp(nil, rpcInvalidRequest, "未授权：token 无效或不是 admin 角色")
			writeMCPLine(out, &resp)
			out.Flush()
			continue
		}
		s.serveMCPRPC(base, []byte(line), func(resp *rpcResp) { writeMCPLine(out, resp) })
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
	token := fs.String("token", "", "admin 角色的 token（也可设 GM_MCP_TOKEN 环境变量）")
	base := fs.String("base", "", "生成加速链接用的基地址，默认由 LISTEN 推导")
	_ = fs.Parse(os.Args[2:])
	if *token == "" {
		*token = os.Getenv("GM_MCP_TOKEN")
	}
	if *base == "" {
		*base = defaultBase(s.cfg.Listen)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "缺少 admin token：请使用 --token <token> 或设置 GM_MCP_TOKEN")
		fmt.Fprintln(os.Stderr, "提示：在管理后台创建 token 时把角色设为「管理员(admin)」。")
		os.Exit(2)
	}
	// stdout 仅供协议使用
	log.SetOutput(os.Stderr)
	if err := serveMCPStdio(s, *token, *base); err != nil {
		log.Printf("MCP stdio 结束: %v", err)
	}
}

// defaultBase 由监听地址推导默认基地址（stdio 模式下没有 HTTP 请求可参照）。
func defaultBase(listen string) string {
	host := listen
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return "http://" + host
}
