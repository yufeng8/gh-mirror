package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gh-mirror/config"
)

// newProxyTestServer 构造带用户与 token 的测试 Server（用户 admin/bob + 3 个不同归属 token）。
func newProxyTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Listen:          "127.0.0.1:18099",
		CacheDir:        filepath.Join(dir, "cache"),
		CacheTTLMinutes: 1440,
		MaxCacheMB:      200,
		TokensFile:      filepath.Join(dir, "tokens.json"),
		Admin:           config.AdminConfig{Path: "/admin"},
	}
	s := &Server{
		cfg:       cfg,
		cache:     NewCache(cfg.CacheDir, time.Duration(cfg.CacheTTLMinutes)*time.Minute, cfg.MaxCacheMB),
		store:     NewStore(cfg.TokensFile),
		users:     NewUserStore(filepath.Join(dir, "users.json")),
		adminUser: "admin",
		timeout:   30 * time.Second,
	}
	if err := s.users.Add("admin", "pass1234", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := s.users.Add("bobby", "pass1234", RoleNormal); err != nil {
		t.Fatal(err)
	}
	s.store.Add("tokBob", "", RoleUser, "bobby")   // 归属 bobby
	s.store.Add("tokAlice", "", RoleUser, "alice") // 归属不存在的用户
	s.store.Add("tokAdmin", "", RoleUser, "")      // 空 owner 归默认管理员
	return s
}

// TestProxyUserSeg 验证 /g/<用户名>/<token>/ 解析与归属校验：
// 鉴权通过的请求会进入域名白名单（example.com → 403），鉴权失败一律 404。
func TestProxyUserSeg(t *testing.T) {
	s := newProxyTestServer(t)
	cases := []struct {
		name string
		uri  string
		want int
	}{
		{"新格式+token归属匹配", "/g/bobby/tokBob/https://example.com/x", http.StatusForbidden},
		{"空owner token 归默认管理员", "/g/admin/tokAdmin/https://example.com/x", http.StatusForbidden},
		{"token 不归属该用户", "/g/admin/tokBob/https://example.com/x", http.StatusNotFound},
		{"用户名段不存在", "/g/nobody/tokBob/https://example.com/x", http.StatusNotFound},
		{"token 归属不存在用户", "/g/bobby/tokAlice/https://example.com/x", http.StatusNotFound},
		{"缺 token 段", "/g/bobby/", http.StatusNotFound},
		{"旧格式(第一段是token不是用户名)", "/g/tokBob/https://github.com/o/r", http.StatusNotFound},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.uri, nil)
		rec := httptest.NewRecorder()
		s.handleProxy(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: %s -> %d, want %d", c.name, c.uri, rec.Code, c.want)
		}
	}
}
