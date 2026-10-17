package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gh-mirror/config"
)

// newHFTestServer 构造指定 auth 模式的 HF 测试 Server（用户 admin/bobby + 不同归属 token）。
func newHFTestServer(t *testing.T, authMode string) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Listen:          "127.0.0.1:18099",
		CacheDir:        filepath.Join(dir, "cache"),
		CacheTTLMinutes: 1440,
		MaxCacheMB:      200,
		TokensFile:      filepath.Join(dir, "tokens.json"),
		Admin:           config.AdminConfig{Path: "/admin"},
		Auth:            config.AuthConfig{Mode: authMode},
		Docker:          config.DockerConfig{Enabled: false},
		HF:              config.HFConfig{Enabled: true},
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
	s.store.Add("tokBob", "", RoleUser, "bobby") // 归属 bobby
	s.store.Add("tokAdmin", "", RoleUser, "")    // 空 owner 归默认管理员
	return s
}

// TestHFUserSegTokenMode 验证 token 模式下 /hf/<用户名>/<token>/ 解析与归属校验。
func TestHFUserSegTokenMode(t *testing.T) {
	s := newHFTestServer(t, "token")
	cases := []struct {
		name string
		uri  string
		want int
	}{
		{"新格式+token归属匹配", "/hf/bobby/tokBob/https://example.com/x", http.StatusForbidden},
		{"空owner token 归默认管理员", "/hf/admin/tokAdmin/https://example.com/x", http.StatusForbidden},
		{"token 不归属该用户", "/hf/admin/tokBob/https://example.com/x", http.StatusNotFound},
		{"用户名段不存在", "/hf/nobody/tokBob/https://example.com/x", http.StatusNotFound},
		{"缺 token 段", "/hf/bobby/", http.StatusNotFound},
		{"旧格式(第一段是token不是用户名)", "/hf/tokBob/openai/gpt2", http.StatusNotFound},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.uri, nil)
		rec := httptest.NewRecorder()
		s.handleHF(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: %s -> %d, want %d", c.name, c.uri, rec.Code, c.want)
		}
	}
}

// TestHFOpenModeNoUserSeg 验证开放模式保持原格式（无用户名段/token，直接按 HF 路径解析）。
func TestHFOpenModeNoUserSeg(t *testing.T) {
	s := newHFTestServer(t, "open")
	// 开放模式：完整 URL 形式透传，example.com 被域名白名单拒（403 = 未做鉴权直接进入转发）
	req := httptest.NewRequest(http.MethodGet, "/hf/https://example.com/x", nil)
	rec := httptest.NewRecorder()
	s.handleHF(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("开放模式透传 example.com -> %d, want 403", rec.Code)
	}
}
