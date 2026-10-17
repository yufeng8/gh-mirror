package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gh-mirror/config"
)

// Server 持有缓存、回源超时、配置与管理后台相关状态。
// svcMu 保护 dockerOn / hfOn：这两个开关可在管理后台运行时热切换（-race 安全）。
type Server struct {
	cfg          *config.Config
	cache        *Cache
	offlineCache *OfflineCache
	timeout      time.Duration
	store        *Store
	users        *UserStore
	adminPath    string
	userPath     string // 普通用户入口路径（默认 /user），与 adminPath 强隔离
	adminUser    string // 初始超级管理员用户名（启动种子；运行态当前用户从 session 取）
	secret       []byte
	cookieSecure bool
	limiter      *RateLimiter
	acl          *AccessController
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	svcMu        sync.RWMutex
	dockerOn     bool
	hfOn         bool
	dockerPrefix string // /v2 自定义路径前缀（空=未启用），持 svcMu 保护；读取用 dockerPath()
	offlineAutoPurge   bool // 离线包磁盘满自动清理开关
	offlinePurgeFreeGB int  // 触发阈值（磁盘可用低于该 GB 数时按最旧优先清理）
	offlineMaxFileMB   int  // 离线包单文件上限（MB，0=不限；超过则不落盘缓存）
	contactEmailStr    string // 超管联系邮箱（忘记密码引导 / 用户紧急联系展示）
	smtpCfg            smtpConfig // SMTP 发信配置（持 svcMu）
	regOpen            bool       // 用户自主注册开关（持 svcMu）
	userCaptcha        bool       // 用户入口图形验证码开关（持 svcMu）
	adminCaptcha       bool       // 管理入口图形验证码开关（持 svcMu）
	inviteOnly         bool       // 仅邀请码注册开关（持 svcMu）
	codes              *CodeStore    // 邮箱验证码存储（内存）
	captcha            *CaptchaStore // 图形验证码存储（内存）
	invites            *InviteStore  // 邀请码存储（invites.json）
}

// processStart 记录进程启动时间，用于后台展示运行时长。
var processStart = time.Now()

// version 是当前程序版本号，构建时由 -ldflags "-X main.version=<tag>" 注入；
// 本地 go build / go run 时为 "dev"。
var version = "dev"

// cacheSweepInterval 是缓存自动失效（过期清理 + 超限清理）的执行周期。
const cacheSweepInterval = 10 * time.Minute

// dockerEnabled 返回 Docker 加速当前是否开启（运行时开关，非 cfg 静态值）。
func (s *Server) dockerEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.dockerOn
}

// hfEnabled 返回 HuggingFace 加速当前是否开启（运行时开关，非 cfg 静态值）。
func (s *Server) hfEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.hfOn
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
	userPath := cfg.Admin.UserPath
	if !strings.HasPrefix(userPath, "/") {
		userPath = "/" + userPath
	}
	if userPath == "/" {
		userPath = "/user"
	}
	// 双入口路径冲突校验：入口地址不能撞已有代理/接口路径，也不能互相冲突
	if err := validateEntryPaths(adminPath, userPath); err != nil {
		log.Fatalf("入口路径配置冲突: %v", err)
	}
	adminUser := cfg.Admin.User
	adminPass := cfg.Admin.Pass
	secretStr := cfg.Admin.Secret
	var secret []byte
	if secretStr == "" {
		secret = genSecret()
		log.Printf("⚠️ 安全提醒：未设置 ADMIN_SECRET，已随机生成会话密钥——重启后所有登录会话失效，多实例间也不一致。生产环境请显式设置固定值（如 openssl rand -hex 32）。")
	} else {
		secret = []byte(secretStr)
	}
	// 安全默认值告警（按既定策略：只提醒、不阻断启动）
	if adminPass == "admin" {
		log.Printf("⚠️ 安全提醒：管理后台正在使用默认弱口令 admin/admin。公网部署请务必通过 ADMIN_PASS（或 config.toml 的 admin.pass）设置强口令。")
	}

	store := NewStore(cfg.TokensFile)
	// 用户表与 tokens.json 同目录：ADMIN_USER/ADMIN_PASS 作为「初始超级管理员」种子迁移进用户表，
	// 之后账号管理完全走 users.json（ADMIN_PASS 只在首次启动时用于创建种子账号）。
	users := NewUserStore(filepath.Join(filepath.Dir(cfg.TokensFile), "users.json"))
	invites := NewInviteStore(filepath.Join(filepath.Dir(cfg.TokensFile), "invites.json"))
	if !users.Has(adminUser) {
		if err := users.Add(adminUser, adminPass, RoleSuper); err != nil {
			log.Printf("⚠️ 初始化超级管理员账号失败: %v（ADMIN_USER 需 4-32 位字母数字._-）", err)
		} else {
			log.Printf("已初始化超级管理员账号: %s（来自 ADMIN_USER；后续可在「用户管理」中增删）", adminUser)
		}
	}
	cache := NewCache(cfg.CacheDir, time.Duration(cfg.CacheTTLMinutes)*time.Minute, cfg.MaxCacheMB)
	// 离线镜像 tar 内容寻址缓存（与普通响应缓存独立：不纳入响应缓存的自动清理）
	offlineCache := NewOfflineCache(filepath.Join(cfg.CacheDir, "offline-cache"))
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
		offlineCache: offlineCache,
		timeout:      time.Duration(cfg.UpstreamTimeoutSec) * time.Second,
		store:        store,
		users:        users,
		adminPath:    adminPath,
		userPath:     userPath,
		adminUser:    adminUser,
		secret:       secret,
		cookieSecure: cfg.Admin.CookieSecure,
		limiter:      limiter,
		acl:          aclCtl,
		dial:         dial,
		dockerOn:     cfg.Docker.Enabled,
		hfOn:         cfg.HF.Enabled,
		codes:        NewCodeStore(),
		captcha:      NewCaptchaStore(),
		invites:      invites,
	}
	// 运行时设置（gm-data/settings.json）优先级最高：覆盖 config.toml / 环境变量的开关与缓存参数
	loadSettings(s, cfg)
	// MCP stdio 子命令：gh-mirror mcp [--token <admin-token>] [--base <url>]
	// 以 stdio 传输供本地 Agent 接入（stdout 仅协议 JSON，日志走 stderr）
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCPStdio(s)
		return
	}
	// 缓存自动失效：周期清理过期文件，并在超总上限时按最旧优先清理
	go func() {
		ticker := time.NewTicker(cacheSweepInterval)
		for range ticker.C {
			expired, over, low := cache.Evict()
			if expired > 0 || over > 0 || low > 0 {
				log.Printf("缓存自动失效: 过期 %d 个、超限 %d 个、磁盘余量 %d 个，当前占用 %s（磁盘可用 %s）",
					expired, over, low, formatBytes(cache.UsageBytes()), formatBytes(cache.DiskFree()))
			}
			// 离线包磁盘满自动清理（开关 + 阈值由超管在缓存管理页设置，独立于普通响应缓存）
			if b := s.offlinePurgeBytes(); b > 0 {
				if n := s.offlineCache.PurgeUntil(b); n > 0 {
					log.Printf("离线包磁盘满自动清理: 删除 %d 个，当前占用 %s（磁盘可用 %s）",
						n, formatBytes(s.offlineCache.TotalBytes()), formatBytes(diskFree(cfg.CacheDir)))
				}
			}
		}
	}()

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

// reservedPaths 是已被代理/接口占用的顶层路径，入口地址（ADMIN_PATH/USER_PATH）不得与其冲突。
var reservedPaths = []string{
	"/healthz",
	"/search",
	"/tags",
	"/api",
	"/v2",
	"/token",
	"/offline",
	"/hf",
	"/g",
	"/mcp",
}

// validateEntryPaths 校验超管/普通入口路径：不能互相冲突，也不能撞已有代理/接口路径。
// 冲突（含前缀重叠）会导致请求被错误路由到后台，fail-fast 避免部署后才发现。
func validateEntryPaths(adminPath, userPath string) error {
	if pathConflicts(adminPath, userPath) {
		return fmt.Errorf("ADMIN_PATH(%s) 与 USER_PATH(%s) 冲突，请使用不同且无前缀关系的路径", adminPath, userPath)
	}
	for _, p := range []string{adminPath, userPath} {
		for _, r := range reservedPaths {
			if pathConflicts(p, r) {
				return fmt.Errorf("入口路径 %s 与保留路径 %s 冲突，请改用其它入口路径", p, r)
			}
		}
	}
	return nil
}

// pathConflicts 判断两个路径是否存在前缀重叠（含相等）。
func pathConflicts(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
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
	// P4 入口中间件：限流 + IP 访问控制（健康检查/首页/后台/普通入口豁免）
	if !(path == "/healthz" || path == "/" || strings.HasPrefix(path, s.adminPath) || strings.HasPrefix(path, s.userPath)) {
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
	case path == "/captcha":
		s.handleCaptcha(w, r)
	case path == "/captcha/new":
		s.handleCaptchaNew(w, r)
	case path == "/favicon.ico":
		s.handleFavicon(w, r)
	case path == "/":
		s.renderHome(w, r)
	case strings.HasPrefix(path, s.adminPath):
		s.handleAdmin(w, r)
	case strings.HasPrefix(path, s.userPath):
		s.handleUser(w, r)
	case path == "/search":
		s.handleSearchPage(w, r)
	case path == "/tags":
		s.handleTagsPage(w, r)
	case path == "/api/search":
		s.handleSearchAPI(w, r)
	case path == "/api/tags":
		s.handleTagsAPI(w, r)
	case s.dockerEnabled() && (path == "/v2" || strings.HasPrefix(path, "/v2/") || path == "/token" || strings.HasPrefix(path, "/token/")):
		s.handleDocker(w, r)
	case s.dockerEnabled() && (path == "/offline" || strings.HasPrefix(path, "/offline/")):
		s.handleOffline(w, r)
	case s.hfEnabled() && strings.HasPrefix(path, hfPrefix):
		s.handleHF(w, r)
	case path == "/mcp" || strings.HasPrefix(path, "/mcp/") || strings.HasPrefix(path, aiPrefix):
		// MCP 接入（JSON-RPC 2.0）：新端点 /ai/<用户名>/mcp；旧 /mcp 走 handleMCP 返回引导提示
		// 限流与 ACL 中间件对 MCP 同样生效
		s.handleMCP(w, r)
	case strings.HasPrefix(path, tokenPrefix):
		s.handleProxy(w, r)
	default:
		s.render404(w, r)
	}
}

func (s *Server) renderHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(s.homeHTML(r.Host)))
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
