package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gh-mirror/config"
)

// newTestServer 构造一个用于 MCP 测试的 Server（临时数据目录 + 一个 admin token）。
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Listen:          "127.0.0.1:18099",
		CacheDir:        filepath.Join(dir, "cache"),
		CacheTTLMinutes: 1440,
		MaxCacheMB:      200,
		TokensFile:      filepath.Join(dir, "tokens.json"),
		Admin:           config.AdminConfig{Path: "/admin"},
		Docker:          config.DockerConfig{Enabled: false},
		HF:              config.HFConfig{Enabled: false},
	}
	s := &Server{
		cfg:       cfg,
		cache:     NewCache(cfg.CacheDir, time.Duration(cfg.CacheTTLMinutes)*time.Minute, cfg.MaxCacheMB),
		store:     NewStore(cfg.TokensFile),
		users:     NewUserStore(filepath.Join(dir, "users.json")),
		adminUser: "admin",
		timeout:   30 * time.Second,
	}
	s.store.Add("adm", "test", RoleAdmin, "")
	s.store.Add("usr", "test", RoleUser, "")
	// 用户表：admin（超级管理员，对应 adminUser）+ bobby（普通用户），供多用户权限测试
	if err := s.users.Add("admin", "pass1234", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := s.users.Add("bobby", "pass1234", RoleNormal); err != nil {
		t.Fatal(err)
	}
	return s
}

// mcpCall 发送一个 JSON-RPC 请求并返回全部响应（owner 传默认管理员 admin）。
func mcpCall(t *testing.T, s *Server, body string) []*rpcResp {
	t.Helper()
	return mcpCallAs(t, s, "admin", body)
}

// mcpCallAs 以指定 owner（模拟不同用户的高级 Token 调用者）发送 JSON-RPC 请求。
func mcpCallAs(t *testing.T, s *Server, owner, body string) []*rpcResp {
	t.Helper()
	var out []*rpcResp
	s.serveMCPRPC("http://127.0.0.1:18099", owner, []byte(body), func(r *rpcResp) { out = append(out, r) })
	return out
}

// mcpFirstText 取首个响应的 result.content[0].text。
func mcpFirstText(t *testing.T, r *rpcResp) string {
	t.Helper()
	if r == nil || r.Error != nil {
		t.Fatalf("响应异常: %+v", r)
	}
	b, _ := json.Marshal(r.Result)
	var cr mcpCallResult
	if err := json.Unmarshal(b, &cr); err != nil {
		t.Fatalf("解析 result 失败: %v (%s)", err, string(b))
	}
	if len(cr.Content) == 0 {
		t.Fatal("result.content 为空")
	}
	return cr.Content[0].Text
}

// TestMCPInitialize 验证 initialize 回显客户端协议版本与 serverInfo。
func TestMCPInitialize(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if len(out) != 1 {
		t.Fatalf("期望 1 个响应，实际 %d", len(out))
	}
	b, _ := json.Marshal(out[0].Result)
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.ProtocolVersion != "2025-06-18" {
		t.Fatalf("应回显客户端协议版本，实际 %q", res.ProtocolVersion)
	}
	if res.ServerInfo.Name != mcpServerName {
		t.Fatalf("serverInfo.name 应为 %q，实际 %q", mcpServerName, res.ServerInfo.Name)
	}
}

// TestMCPToolsList 验证工具清单数量，且不泄露内部 handler 字段。
func TestMCPToolsList(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if len(out) != 1 {
		t.Fatalf("期望 1 个响应，实际 %d", len(out))
	}
	b, _ := json.Marshal(out[0].Result)
	if strings.Contains(string(b), "handler") {
		t.Fatal("tools/list 不应泄露内部 handler 字段")
	}
	var res struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Tools) != len(mcpRegistry) {
		t.Fatalf("工具数应为 %d，实际 %d", len(mcpRegistry), len(res.Tools))
	}
	var hasConvert, hasHealth bool
	for _, tl := range res.Tools {
		if tl.Name == "convert_link" {
			hasConvert = true
		}
		if tl.Name == "health" {
			hasHealth = true
		}
	}
	if !hasConvert || !hasHealth {
		t.Fatal("工具清单缺少 convert_link 或 health")
	}
}

// TestMCPCallHealth 验证 tools/call 调用 health。
func TestMCPCallHealth(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"health","arguments":{}}}`)
	if len(out) != 1 {
		t.Fatalf("期望 1 个响应，实际 %d", len(out))
	}
	text := mcpFirstText(t, out[0])
	if !strings.Contains(text, "ok") {
		t.Fatalf("health 应返回 ok，实际: %s", text)
	}
}

// TestMCPCallSetService 验证服务开关即时生效并持久化。
func TestMCPCallSetService(t *testing.T) {
	s := newTestServer(t)
	if s.hfEnabled() {
		t.Fatal("初始 hf 应为关闭")
	}
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"hf","enabled":true}}}`)
	text := mcpFirstText(t, out[0])
	if !strings.Contains(text, "已开启") {
		t.Fatalf("应返回已开启，实际: %s", text)
	}
	if !s.hfEnabled() {
		t.Fatal("hf 应立即变为开启（即时生效）")
	}
	// 持久化校验
	st := s.currentSettings()
	if !st.HF {
		t.Fatal("settings 应记录 hf=true（已持久化）")
	}
}

// TestMCPCallConvertLink 验证链接转换（与后台转换器同一实现，GitHub 输出带用户名段）。
func TestMCPCallConvertLink(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"convert_link","arguments":{"url":"https://github.com/o/r/releases/download/v1/a.zip"}}}`)
	text := mcpFirstText(t, out[0])
	if !strings.Contains(text, "/g/admin/") || !strings.Contains(text, "github.com/o/r") {
		t.Fatalf("转换结果异常: %s", text)
	}
}

// TestMCPPermissionBoundaries 验证 MCP 多用户权限边界：
// 实例级操作（服务开关/缓存）仅超级管理员；token 列表/删除按归属隔离。
func TestMCPPermissionBoundaries(t *testing.T) {
	s := newTestServer(t)
	s.store.Add("tokBobby", "", RoleUser, "bobby") // 归属 bobby

	call := func(owner, body string) string {
		out := mcpCallAs(t, s, owner, body)
		if len(out) != 1 {
			t.Fatalf("期望 1 个响应，实际 %d", len(out))
		}
		return mcpFirstText(t, out[0])
	}
	setSvc := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"hf","enabled":true}}}`
	clearC := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"clear_cache","arguments":{}}}`
	listT := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_tokens","arguments":{}}}`
	delT := func(tok string) string {
		return `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_token","arguments":{"token":"` + tok + `"}}}`
	}

	// 1. 普通用户（bobby）调实例级工具应被拒
	if txt := call("bobby", setSvc); !strings.Contains(txt, "超级管理员") {
		t.Fatalf("normal 调 set_service 应被拒，实际: %s", txt)
	}
	if txt := call("bobby", clearC); !strings.Contains(txt, "超级管理员") {
		t.Fatalf("normal 调 clear_cache 应被拒，实际: %s", txt)
	}
	// 2. 普通用户 list_tokens 只见自己的（不见 adm/usr——它们归 admin）
	if txt := call("bobby", listT); strings.Contains(txt, "tokBobby") && !strings.Contains(txt, "\"adm\"") {
		if strings.Contains(txt, "\"token\": \"adm\"") {
			t.Fatalf("normal 不应看到他人 token: %s", txt)
		}
	} else if !strings.Contains(txt, "tokBobby") {
		t.Fatalf("normal 应看到自己的 token: %s", txt)
	}
	// 3. 普通用户删除他人 token 应被拒
	if txt := call("bobby", delT("adm")); !strings.Contains(txt, "无权删除") {
		t.Fatalf("normal 删他人 token 应被拒，实际: %s", txt)
	}
	// 4. 普通用户删除自己的 token 应成功
	if txt := call("bobby", delT("tokBobby")); !strings.Contains(txt, "已删除") {
		t.Fatalf("normal 删自己的 token 应成功，实际: %s", txt)
	}
	// 5. 超级管理员（admin）调实例级工具应成功
	if txt := call("admin", setSvc); !strings.Contains(txt, "已开启") {
		t.Fatalf("super 调 set_service 应成功，实际: %s", txt)
	}
}

// TestMCPPathUserSeg 验证 /ai/<用户名>/mcp 端点：用户名段反查 + 高级 Token 归属校验。
func TestMCPPathUserSeg(t *testing.T) {
	s := newTestServer(t)
	s.store.Add("tokBobby", "", RoleAdmin, "bobby") // bobby 自己的高级 Token

	call := func(path, tok string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		return rec.Code
	}

	// 1. 用户名段 + 归属自己的高级 Token → 200
	if c := call("/ai/admin/mcp", "adm"); c != http.StatusOK {
		t.Errorf("admin 路径 + 归属 token -> %d, want 200", c)
	}
	// 2. 普通用户 bobby 用自己的高级 Token → 200
	if c := call("/ai/bobby/mcp", "tokBobby"); c != http.StatusOK {
		t.Errorf("bobby 路径 + 自己的高级 token -> %d, want 200", c)
	}
	// 3. Token 不归属路径用户（adm 归 admin，用在 bobby 路径）→ 401
	if c := call("/ai/bobby/mcp", "adm"); c != http.StatusUnauthorized {
		t.Errorf("跨用户 token -> %d, want 401", c)
	}
	// 4. 用户名段不存在 → 404
	if c := call("/ai/nobody/mcp", "adm"); c != http.StatusNotFound {
		t.Errorf("用户名不存在 -> %d, want 404", c)
	}
	// 5. 旧端点 /mcp 已废弃 → 401（引导用新格式）
	if c := call("/mcp", "adm"); c != http.StatusUnauthorized {
		t.Errorf("旧端点 /mcp -> %d, want 401", c)
	}
	// 6. 路径第二段不是 mcp → 404
	if c := call("/ai/admin/other", "adm"); c != http.StatusNotFound {
		t.Errorf("路径第二段非 mcp -> %d, want 404", c)
	}
	// 7. GET 方法 → 405
	req := httptest.NewRequest(http.MethodGet, "/ai/admin/mcp", nil)
	rec := httptest.NewRecorder()
	s.handleMCP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET -> %d, want 405", rec.Code)
	}
}

// TestMCPUnknownMethod 与未知工具的报错。
func TestMCPUnknownMethod(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","id":5,"method":"foo/bar"}`)
	if len(out) != 1 || out[0].Error == nil || out[0].Error.Code != rpcMethodNotFound {
		t.Fatalf("未知方法应返回 -32601，实际: %+v", out)
	}
	out = mcpCall(t, s, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope"}}`)
	if len(out) != 1 || out[0].Error == nil || out[0].Error.Code != rpcInvalidParams {
		t.Fatalf("未知工具应返回 -32602，实际: %+v", out)
	}
}

// TestMCPNotificationNoResponse 验证通知类方法不回响应（stdio 下不得产生输出）。
func TestMCPNotificationNoResponse(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(out) != 0 {
		t.Fatalf("通知类不应产生响应，实际 %d 个", len(out))
	}
}

// TestMCPBatch 验证 JSON-RPC 批量请求。
func TestMCPBatch(t *testing.T) {
	s := newTestServer(t)
	out := mcpCall(t, s, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_services","arguments":{}}}]`)
	if len(out) != 2 {
		t.Fatalf("批量应返回 2 个响应，实际 %d", len(out))
	}
}

// TestStoreIsAdmin 验证 admin / user 角色区分（MCP 鉴权依赖）。
func TestStoreIsAdmin(t *testing.T) {
	s := newTestServer(t)
	if !s.store.IsAdmin("adm") {
		t.Fatal("adm 应为 admin 角色")
	}
	if s.store.IsAdmin("usr") {
		t.Fatal("usr 不应具备 admin 角色")
	}
	if s.store.IsAdmin("nope") {
		t.Fatal("不存在的 token 不应通过 admin 校验")
	}
}
