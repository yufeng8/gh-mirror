package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Server 持有缓存、回源超时与管理后台相关配置。
type Server struct {
	cache        *Cache
	timeout      time.Duration
	store        *Store
	adminPath    string
	adminUser    string
	adminPass    string
	secret       []byte
	cookieSecure bool
}

func main() {
	listen := getenv("LISTEN", "127.0.0.1:8080")
	cacheDir := getenv("CACHE_DIR", "./cache")
	ttlMin := getenvInt("CACHE_TTL_MINUTES", 1440)
	maxMB := getenvInt("MAX_CACHE_MB", 200)
	timeoutSec := getenvInt("UPSTREAM_TIMEOUT", 60)

	adminPath := getenv("ADMIN_PATH", "/admin")
	if !strings.HasPrefix(adminPath, "/") {
		adminPath = "/" + adminPath
	}
	if adminPath == "/" {
		adminPath = "/admin"
	}
	adminUser := getenv("ADMIN_USER", "admin")
	adminPass := getenv("ADMIN_PASS", "admin")
	secretStr := getenv("ADMIN_SECRET", "")
	var secret []byte
	if secretStr == "" {
		secret = genSecret()
	} else {
		secret = []byte(secretStr)
	}
	cookieSecure := getenv("COOKIE_SECURE", "true") == "true" || getenv("COOKIE_SECURE", "true") == "1"

	tokensFile := getenv("TOKENS_FILE", "./tokens.json")
	store := NewStore(tokensFile)

	cache := NewCache(cacheDir, time.Duration(ttlMin)*time.Minute, maxMB)
	s := &Server{
		cache:        cache,
		timeout:      time.Duration(timeoutSec) * time.Second,
		store:        store,
		adminPath:    adminPath,
		adminUser:    adminUser,
		adminPass:    adminPass,
		secret:       secret,
		cookieSecure: cookieSecure,
	}

	log.Printf("github-mirror 启动 listen=%s adminPath=%s tokens=%d ttl=%dm useJsdelivr=%v",
		listen, adminPath, len(store.All()), ttlMin, useJsdelivr)
	// 定时将内存中的 token 统计落盘，避免每次请求都写文件
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			store.Save()
		}
	}()
	// 直接以处理函数作为 Handler，避免 net/http.ServeMux 对 // 路径做规范化重定向
	log.Fatal(http.ListenAndServe(listen, http.HandlerFunc(s.handle)))
}

// genSecret 生成随机会话签名密钥。
func genSecret() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return []byte(hex.EncodeToString(b))
}

// handle 是统一入口：首页 / 健康检查 / 管理后台 / token 代理 / 其余 404。
// 注意：必须用原始 RequestURI 解析路径，否则 https:// 中的双斜杠会被丢失/重定向。
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	rawURI := r.RequestURI
	path := rawURI
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case path == "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	case path == "/":
		s.renderHome(w, r)
	case strings.HasPrefix(path, s.adminPath):
		s.handleAdmin(w, r)
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
