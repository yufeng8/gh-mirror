package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpFirstResult 取首个响应的 result（map 形态），便于断言 initialize / tools/list 等结构。
func mcpFirstResult(t *testing.T, r *rpcResp) map[string]any {
	t.Helper()
	if r == nil || r.Error != nil {
		t.Fatalf("响应异常: %+v", r)
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("result 不是对象: %+v", r.Result)
	}
	return m
}

// TestMCPInitializeInstructions：协议 2025-06-18 起 initialize 应返回 instructions 与真实版本号。
func TestMCPInitializeInstructions(t *testing.T) {
	s := newTestServer(t)
	got := mcpFirstResult(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)[0])
	ins, _ := got["instructions"].(string)
	if !strings.Contains(ins, "convert_link") || !strings.Contains(ins, "create_token") {
		t.Fatalf("instructions 未给出关键工具指引: %q", ins)
	}
	si, _ := got["serverInfo"].(map[string]any)
	if si["name"] != mcpServerName {
		t.Fatalf("serverInfo.name = %v", si["name"])
	}
	if v, _ := si["version"].(string); v == "" || v == "0.2.0" {
		t.Fatalf("serverInfo.version 应为构建期版本，实际 %q", si["version"])
	}
	// 旧协议版本不返回 instructions（老客户端会忽略未知字段，这里保证仅新版本携带）
	old := mcpFirstResult(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)[0])
	if _, ok := old["instructions"]; ok {
		t.Fatal("2024-11-05 不应返回 instructions")
	}
}

// TestMCPToolSchemaForAgents：工具描述要含「返回什么」，敏感参数要有 enum/默认值，便于模型正确选择与传参。
func TestMCPToolSchemaForAgents(t *testing.T) {
	s := newTestServer(t)
	resp := mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)[0]
	if resp.Error != nil {
		t.Fatalf("tools/list 出错: %+v", resp.Error)
	}
	rm, _ := resp.Result.(map[string]any)
	tools, _ := rm["tools"].([]mcpTool)
	if len(tools) != 12 {
		t.Fatalf("工具数 = %d, want 12", len(tools))
	}
	byName := map[string]map[string]any{}
	byDesc := map[string]string{}
	for _, m := range tools {
		if len(m.Description) < 20 {
			t.Fatalf("工具 %s 描述过短: %q", m.Name, m.Description)
		}
		byName[m.Name] = m.InputSchema
		byDesc[m.Name] = m.Description
	}
	// 破坏性工具必须在描述里明确警告
	for _, n := range []string{"delete_token", "set_service", "set_cache_limits", "clear_cache"} {
		if !strings.Contains(byDesc[n], "破坏性") {
			t.Fatalf("工具 %s 描述缺少破坏性警告: %q", n, byDesc[n])
		}
	}
	// 权限要求要写明
	for _, n := range []string{"set_service", "get_cache_stats", "clear_cache"} {
		if !strings.Contains(byDesc[n], "超级管理员") {
			t.Fatalf("工具 %s 描述缺少权限说明: %q", n, byDesc[n])
		}
	}
	// set_service.name 应为枚举，避免模型传入 github
	props, _ := byName["set_service"]["properties"].(map[string]any)
	nameProp, _ := props["name"].(map[string]any)
	enum, _ := nameProp["enum"].([]string)
	if len(enum) != 2 {
		t.Fatalf("set_service.name 缺少 enum 约束: %+v", nameProp)
	}
	// create_token.role 也应为枚举
	props2, _ := byName["create_token"]["properties"].(map[string]any)
	roleProp, _ := props2["role"].(map[string]any)
	if _, ok := roleProp["enum"]; !ok {
		t.Fatalf("create_token.role 缺少 enum: %+v", roleProp)
	}
	// search_images 支持 limit 且有默认值
	props3, _ := byName["search_images"]["properties"].(map[string]any)
	lim, _ := props3["limit"].(map[string]any)
	if lim["type"] != "integer" || lim["default"] == nil {
		t.Fatalf("search_images.limit 定义不完整: %+v", lim)
	}
	// 必填项保留
	req, _ := byName["convert_link"]["required"].([]string)
	if len(req) != 1 || req[0] != "url" {
		t.Fatalf("convert_link required = %v", req)
	}
}

// TestMCPUnknownToolListsAvailable：未知工具错误要列出可用工具，模型才能自纠。
func TestMCPUnknownToolListsAvailable(t *testing.T) {
	s := newTestServer(t)
	r := mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"convert","arguments":{}}}`)
	if len(r) != 1 || r[0].Error == nil {
		t.Fatalf("期望错误响应: %+v", r)
	}
	msg := r[0].Error.Message
	if !strings.Contains(msg, "convert_link") || !strings.Contains(msg, "health") {
		t.Fatalf("错误未列出可用工具: %q", msg)
	}
}

// TestMCPEmptyCapabilitiesMethods：客户端探测 resources/prompts 时应得到空列表而不是报错。
func TestMCPEmptyCapabilitiesMethods(t *testing.T) {
	s := newTestServer(t)
	res := mcpFirstResult(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list","params":{}}`)[0])
	if arr, ok := res["resources"].([]any); !ok || len(arr) != 0 {
		t.Fatalf("resources/list = %+v", res["resources"])
	}
	res2 := mcpFirstResult(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":2,"method":"prompts/list","params":{}}`)[0])
	if arr, ok := res2["prompts"].([]any); !ok || len(arr) != 0 {
		t.Fatalf("prompts/list = %+v", res2["prompts"])
	}
	// logging/setLevel 应被接受
	if r := mcpCall(t, s, `{"jsonrpc":"2.0","id":3,"method":"logging/setLevel","params":{"level":"debug"}}`); len(r) != 1 || r[0].Error != nil {
		t.Fatalf("logging/setLevel 应被接受: %+v", r)
	}
}

// TestMCPBadArgsExplain：参数缺失/非法时要给出可执行的修正提示，而不是静默用默认值。
func TestMCPBadArgsExplain(t *testing.T) {
	s := newTestServer(t)
	cases := []struct{ name, call, want string }{
		{"search_images 缺 q", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_images","arguments":{}}}`, "缺少参数 q"},
		{"create_token 非法 role", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_token","arguments":{"role":"root"}}}`, "role 仅支持"},
		{"set_service 非法 name", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"github","enabled":false}}}`, "name 仅支持"},
		{"set_service 缺 enabled", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"set_service","arguments":{"name":"docker"}}}`, "缺少参数 enabled"},
		{"convert_link 缺 url", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"convert_link","arguments":{}}}`, "缺少参数 url"},
		{"list_tags 缺 repo", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_tags","arguments":{}}}`, "缺少参数 repo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			txt := mcpFirstText(t, mcpCall(t, s, c.call)[0])
			if !strings.Contains(txt, c.want) {
				t.Fatalf("提示未包含 %q，实际: %s", c.want, txt)
			}
		})
	}
}

// TestMCPConvertLinkNoTokenHint：没有可用 Token 时，提示应指向 create_token（MCP 语境）。
func TestMCPConvertLinkNoTokenHint(t *testing.T) {
	s := newTestServer(t)
	s.store.Delete("adm") // 让调用者名下没有可用 token
	s.store.Delete("usr")
	txt := mcpFirstText(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"convert_link","arguments":{"url":"github.com/a/b/blob/main/README.md"}}}`)[0])
	if !strings.Contains(txt, "create_token") {
		t.Fatalf("应提示调用 create_token，实际: %s", txt)
	}
}

// TestMCPHTTPTransport 覆盖传输层：鉴权、GET/DELETE 语义、SSE 协商。
func TestMCPHTTPTransport(t *testing.T) {
	s := newTestServer(t)
	post := func(tok, accept, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/ai/admin/mcp", strings.NewReader(body))
		if tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
		r.Header.Set("Content-Type", "application/json")
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		s.handleMCP(w, r)
		return w
	}
	// 无 token / 普通 token → 401 JSON
	for _, tok := range []string{"", "usr"} {
		w := post(tok, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token=%q 期望 401，实际 %d", tok, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("错误响应应为 JSON，实际 %q", ct)
		}
	}
	// Accept 同时含 json 与 sse → 返回单个 JSON
	w := post("adm", "application/json, text/event-stream", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("双 Accept 应返回 JSON，实际 %q", ct)
	}
	// 只要求 sse → 返回事件流
	w2 := post("adm", "text/event-stream", `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if ct := w2.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("仅 sse Accept 应返回 text/event-stream，实际 %q", ct)
	}
	if body := w2.Body.String(); !strings.Contains(body, "event: message") || !strings.Contains(body, "data: ") {
		t.Fatalf("SSE 响应格式不正确: %q", body)
	}
	// 只含通知的请求也要结束响应流
	w3 := post("adm", "text/event-stream", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w3.Body.Len() == 0 {
		t.Fatal("纯通知请求也应写回响应流结束标记")
	}
	// GET → 405 JSON 说明；DELETE → 200
	gr := httptest.NewRequest(http.MethodGet, "/ai/admin/mcp", nil)
	gw := httptest.NewRecorder()
	s.handleMCP(gw, gr)
	if gw.Code != http.StatusMethodNotAllowed || !strings.Contains(gw.Body.String(), "POST") {
		t.Fatalf("GET 期望 405 + POST 指引，实际 %d %s", gw.Code, gw.Body.String())
	}
	dr := httptest.NewRequest(http.MethodDelete, "/ai/admin/mcp", nil)
	dw := httptest.NewRecorder()
	s.handleMCP(dw, dr)
	if dw.Code != http.StatusOK {
		t.Fatalf("DELETE 期望 200，实际 %d", dw.Code)
	}
}

// TestMCPListTokensJSON：list_tokens 改为 JSON 代码块输出，便于模型解析。
func TestMCPListTokensJSON(t *testing.T) {
	s := newTestServer(t)
	txt := mcpFirstText(t, mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_tokens","arguments":{}}}`)[0])
	if !strings.Contains(txt, "```json") {
		t.Fatalf("list_tokens 应输出 json 代码块: %s", txt)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(jsonBlock(t, txt)), &rows); err != nil {
		t.Fatalf("json 代码块解析失败: %v", err)
	}
	if len(rows) == 0 || rows[0]["token"] == "" {
		t.Fatalf("list_tokens JSON 内容异常: %+v", rows)
	}
}

// jsonBlock 提取文本中的第一个 json 代码块内容。
func jsonBlock(t *testing.T, txt string) string {
	t.Helper()
	const fence = "```json"
	i := strings.Index(txt, fence)
	if i < 0 {
		t.Fatalf("未找到 json 代码块: %s", txt)
	}
	rest := txt[i+len(fence):]
	j := strings.Index(rest, "```")
	if j < 0 {
		t.Fatalf("json 代码块未闭合: %s", txt)
	}
	return strings.TrimSpace(rest[:j])
}
