package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config 是 gh-mirror 的统一配置：默认值 → config.toml → 环境变量（覆盖，保持旧部署兼容）。
type Config struct {
	Listen             string
	CacheDir           string
	CacheTTLMinutes    int
	MaxCacheMB         int
	UpstreamTimeoutSec int
	UseJsdelivr        bool
	TokensFile         string

	Admin     AdminConfig
	Auth      AuthConfig
	Docker    DockerConfig
	HF        HFConfig
	RateLimit RateLimitConfig
	ACL       ACLConfig
	Upstream  UpstreamConfig
}

type AdminConfig struct {
	Path         string
	User         string
	Pass         string
	Secret       string
	CookieSecure bool
}

// AuthConfig.Mode: "token"=强制令牌(默认)；"open"=开放代理(配合 acl 使用)。
type AuthConfig struct {
	Mode string
}

type DockerConfig struct {
	Enabled         bool
	Upstreams       []string
	CacheTTLMinutes int
}

type HFConfig struct {
	Enabled         bool
	CacheTTLMinutes int
}

type RateLimitConfig struct {
	Enabled       bool
	PeriodSeconds int
	Quota         int
	IPv6Prefix    int
}

type ACLConfig struct {
	IPAllow   []string
	IPBlock   []string
	RepoAllow []string
	RepoBlock []string
}

type UpstreamConfig struct {
	Socks5 string
}

// Load 读取并合并配置。
func Load(path string) *Config {
	c := defaultConfig()
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			c.applyTOML(string(b))
		}
	}
	c.applyEnv()
	return c
}

func defaultConfig() *Config {
	return &Config{
		Listen:             "127.0.0.1:8080",
		CacheDir:           "./cache",
		CacheTTLMinutes:    1440,
		MaxCacheMB:         200,
		UpstreamTimeoutSec: 60,
		TokensFile:         "./tokens.json",
		Admin:              AdminConfig{Path: "/admin", User: "admin", Pass: "admin", CookieSecure: true},
		Auth:               AuthConfig{Mode: "token"},
		Docker:             DockerConfig{Enabled: false, Upstreams: []string{"https://registry-1.docker.io"}, CacheTTLMinutes: 1440},
		HF:                 HFConfig{Enabled: false, CacheTTLMinutes: 1440},
		RateLimit:          RateLimitConfig{Enabled: false, PeriodSeconds: 3600, Quota: 1000, IPv6Prefix: 64},
		Upstream:           UpstreamConfig{Socks5: ""},
	}
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

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v == "1" || v == "true"
}

// applyEnv 用环境变量覆盖，兼容旧版部署脚本。
func (c *Config) applyEnv() {
	c.Listen = getenv("LISTEN", c.Listen)
	c.CacheDir = getenv("CACHE_DIR", c.CacheDir)
	c.CacheTTLMinutes = getenvInt("CACHE_TTL_MINUTES", c.CacheTTLMinutes)
	c.MaxCacheMB = getenvInt("MAX_CACHE_MB", c.MaxCacheMB)
	c.UpstreamTimeoutSec = getenvInt("UPSTREAM_TIMEOUT", c.UpstreamTimeoutSec)
	c.UseJsdelivr = getenvBool("USE_JSDELIVR", c.UseJsdelivr)
	c.TokensFile = getenv("TOKENS_FILE", c.TokensFile)
	c.Admin.Path = getenv("ADMIN_PATH", c.Admin.Path)
	c.Admin.User = getenv("ADMIN_USER", c.Admin.User)
	c.Admin.Pass = getenv("ADMIN_PASS", c.Admin.Pass)
	c.Admin.Secret = getenv("ADMIN_SECRET", c.Admin.Secret)
	if os.Getenv("COOKIE_SECURE") != "" {
		c.Admin.CookieSecure = getenvBool("COOKIE_SECURE", c.Admin.CookieSecure)
	}
	// 多服务开关与出站代理（便于容器/一键部署直接开启，等价于 config.toml）
	c.Docker.Enabled = getenvBool("DOCKER_ENABLED", c.Docker.Enabled)
	c.HF.Enabled = getenvBool("HF_ENABLED", c.HF.Enabled)
	c.RateLimit.Enabled = getenvBool("RATELIMIT_ENABLED", c.RateLimit.Enabled)
	if v := os.Getenv("UPSTREAM_SOCKS5"); v != "" {
		c.Upstream.Socks5 = v
	}
}

// applyTOML 解析简化版 TOML：支持 [section]、key=value、字符串/整数/布尔/字符串数组。
func (c *Config) applyTOML(data string) {
	sections := map[string]map[string]string{"": {}}
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := sections[cur]; !ok {
				sections[cur] = map[string]string{}
			}
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		sections[cur][k] = v
	}
	if m, ok := sections[""]; ok {
		c.applyRoot(m)
	}
	if m, ok := sections["admin"]; ok {
		c.applyAdmin(m)
	}
	if m, ok := sections["auth"]; ok {
		setStr(m, "mode", &c.Auth.Mode)
	}
	if m, ok := sections["docker"]; ok {
		c.applyDocker(m)
	}
	if m, ok := sections["hf"]; ok {
		setBool(m, "enabled", &c.HF.Enabled)
		setInt(m, "cache_ttl_minutes", &c.HF.CacheTTLMinutes)
	}
	if m, ok := sections["ratelimit"]; ok {
		setBool(m, "enabled", &c.RateLimit.Enabled)
		setInt(m, "period_seconds", &c.RateLimit.PeriodSeconds)
		setInt(m, "quota", &c.RateLimit.Quota)
		setInt(m, "ipv6_prefix", &c.RateLimit.IPv6Prefix)
	}
	if m, ok := sections["acl"]; ok {
		setArr(m, "ip_allow", &c.ACL.IPAllow)
		setArr(m, "ip_block", &c.ACL.IPBlock)
		setArr(m, "repo_allow", &c.ACL.RepoAllow)
		setArr(m, "repo_block", &c.ACL.RepoBlock)
	}
	if m, ok := sections["upstream"]; ok {
		setStr(m, "socks5", &c.Upstream.Socks5)
	}
}

func (c *Config) applyRoot(m map[string]string) {
	setStr(m, "listen", &c.Listen)
	setStr(m, "cache_dir", &c.CacheDir)
	setInt(m, "cache_ttl_minutes", &c.CacheTTLMinutes)
	setInt(m, "max_cache_mb", &c.MaxCacheMB)
	setInt(m, "upstream_timeout", &c.UpstreamTimeoutSec)
	setBool(m, "use_jsdelivr", &c.UseJsdelivr)
	setStr(m, "tokens_file", &c.TokensFile)
}

func (c *Config) applyAdmin(m map[string]string) {
	setStr(m, "path", &c.Admin.Path)
	setStr(m, "user", &c.Admin.User)
	setStr(m, "pass", &c.Admin.Pass)
	setStr(m, "secret", &c.Admin.Secret)
	setBool(m, "cookie_secure", &c.Admin.CookieSecure)
}

func (c *Config) applyDocker(m map[string]string) {
	setBool(m, "enabled", &c.Docker.Enabled)
	setArr(m, "upstreams", &c.Docker.Upstreams)
	setInt(m, "cache_ttl_minutes", &c.Docker.CacheTTLMinutes)
}

func setStr(m map[string]string, k string, dst *string) {
	if v, ok := m[k]; ok {
		*dst = unquote(v)
	}
}

func setBool(m map[string]string, k string, dst *bool) {
	if v, ok := m[k]; ok {
		*dst = v == "true" || v == "1"
	}
}

func setInt(m map[string]string, k string, dst *int) {
	if v, ok := m[k]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			*dst = n
		}
	}
}

func setArr(m map[string]string, k string, dst *[]string) {
	if v, ok := m[k]; ok {
		*dst = parseArray(v)
	}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `\"`, `"`)
		s = strings.ReplaceAll(s, `\\`, `\`)
	}
	return s
}

func parseArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, unquote(p))
	}
	return out
}
