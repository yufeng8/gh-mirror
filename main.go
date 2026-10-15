package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gh-mirror/config"
)

// Server 持有缓存、回源超时、配置与管理后台相关状态。
type Server struct {
	cfg          *config.Config
	cache        *Cache
	timeout      time.Duration
	store        *Store
	adminPath    string
	adminUser    string
	adminPass    string
	secret       []byte
	cookieSecure bool
	limiter      *RateLimiter
	acl          *AccessController
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
}

func main() {
	cfg := config.Load("config.toml")

	adminPath := cfg.Admin.Path
	if !strings.HasPrefix(adminPath, "/") {
		adminPath = "/" + adminPath
	}
	if adminPath == "/" {
		adminPath = "/admin"
	}
	adminUser := cfg.Admin.User
	adminPass := cfg.Admin.Pass
	secretStr := cfg.Admin.Secret
	var secret []byte
	if secretStr == "" {
		secret = genSecret()
	} else {
		secret = []byte(secretStr)
	}

	store := NewStore(cfg.TokensFile)
	cache := NewCache(cfg.CacheDir, time.Duration(cfg.CacheTTLMinutes)*time.Minute, cfg.MaxCacheMB)
	useJsdelivr = cfg.UseJsdelivr

	// P4：访问控制、限流、SOCKS5 出站（默认关闭，由 config.toml 开启）
	aclCtl := NewAccessController(cfg.ACL.IPAllow, cfg.ACL.IPBlock, cfg.ACL.RepoAllow, cfg.ACL.RepoBlock)
	var limiter *RateLimiter
	if cfg.RateLimit.Enabled {
		limiter = NewRateLimiter(cfg.RateLimit.PeriodSeconds, cfg.RateLimit.Quota)
	}
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	if cfg.Upstream.Socks5 != "" {
		dial = newSocks5Dialer(cfg.Upstream.Socks5).dialContext
		if tr, ok := dockerClient.Transport.(*http.Transport); ok {
			tr.DialContext = dial
		}
		log.Printf("SOCKS5 出站代理已启用: %s", cfg.Upstream.Socks5)
	}

	s := &Server{
		cfg:          cfg,
		cache:        cache,
		timeout:      time.Duration(cfg.UpstreamTimeoutSec) * time.Second,
		store:        store,
		adminPath:    adminPath,
		adminUser:    adminUser,
		adminPass:    adminPass,
		secret:       secret,
		cookieSecure: cfg.Admin.CookieSecure,
		limiter:      limiter,
		acl:          aclCtl,
		dial:         dial,
	}

	log.Printf("github-mirror 启动 listen=%s adminPath=%s tokens=%d ttl=%dm useJsdelivr=%v authMode=%s",
		cfg.Listen, adminPath, len(store.All()), cfg.CacheTTLMinutes, useJsdelivr, cfg.Auth.Mode)
	// 定时将内存中的 token 统计落盘，避免每次请求都写文件
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			store.Save()
		}
	}()
	// 直接以处理函数作为 Handler，避免 net/http.ServeMux 对 // 路径做规范化重定向
	log.Fatal(http.ListenAndServe(cfg.Listen, http.HandlerFunc(s.handle)))
}

// genSecret 生成随机会话签名密钥。
func genSecret() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return []byte(hex.EncodeToString(b))
}

// handle 是统一入口：首页 / 健康检查 / 管理后台 / 搜索 / Docker 加速 / 离线打包 / HuggingFace 加速 / token 代理 / 其余 404。
// 注意：必须用原始 RequestURI 解析路径，否则 https:// 中的双斜杠会被丢失/重定向。
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	rawURI := r.RequestURI
	path := rawURI
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	// P4 入口中间件：限流 + IP 访问控制（健康检查/首页/后台豁免）
	if !(path == "/healthz" || path == "/" || strings.HasPrefix(path, s.adminPath)) {
		if s.limiter != nil && !s.limiter.Allow(ClientIP(r)) {
			http.Error(w, "请求频率过快，请稍后重试", http.StatusTooManyRequests)
			return
		}
		if ok, reason := s.acl.CheckIP(ClientIP(r)); !ok {
			http.Error(w, "访问被拒绝: "+reason, http.StatusForbidden)
			return
		}
	}
	switch {
	case path == "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	case path == "/":
		s.renderHome(w, r)
	case strings.HasPrefix(path, s.adminPath):
		s.handleAdmin(w, r)
	case path == "/search":
		s.handleSearchPage(w, r)
	case path == "/tags":
		s.handleTagsPage(w, r)
	case path == "/api/search":
		s.handleSearchAPI(w, r)
	case path == "/api/tags":
		s.handleTagsAPI(w, r)
	case s.cfg.Docker.Enabled && (path == "/v2" || strings.HasPrefix(path, "/v2/") || path == "/token" || strings.HasPrefix(path, "/token/")):
		s.handleDocker(w, r)
	case s.cfg.Docker.Enabled && (path == "/offline" || strings.HasPrefix(path, "/offline/")):
		s.handleOffline(w, r)
	case s.cfg.HF.Enabled && strings.HasPrefix(path, hfPrefix):
		s.handleHF(w, r)
	case strings.HasPrefix(path, tokenPrefix):
		s.handleProxy(w, r)
	default:
		s.render404(w, r)
	}
}

func (s *Server) renderHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(homeHTML(r.Host)))
}

func (s *Server) render404(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundHTML))
}

// getenv / getenvInt 保留以兼容可能的脚本调用；新代码请统一使用 config 包。
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
