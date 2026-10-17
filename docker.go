package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Docker 镜像加速：纯标准库实现的 Registry API v2 缓存反向代理。
// 策略：
//   - /v2/ 与 /v2/<name>/... 透明转发到上游 Registry，层(blob)写透磁盘缓存；
//   - manifest 内存缓存；/token 认证代理 + token 内存缓存；
//   - 重写上游 Www-Authenticate 的 realm 指向本机 /token，使客户端始终与本机交互。

// registryMapping 描述一个上游 Registry。authURL 为完整的 token 端点（scheme 去掉）。
type registryMapping struct {
	domain   string // 路径域名前缀，用于 /v2/<domain>/... 多 registry 路由；Docker Hub 为 ""
	upstream string // 上游 Registry API 主机
	authURL  string // 完整 token 端点（如 auth.docker.io/token、quay.io/v2/auth）；空表示匿名
	service  string // 认证 service 参数
}

// dockerHub 为默认（对应 docker registry-mirror 模式）。
var dockerHub = registryMapping{
	domain:   "",
	upstream: "registry-1.docker.io",
	authURL:  "auth.docker.io/token",
	service:  "registry.docker.io",
}

// extraRegistries 支持 /v2/<domain>/... 形式的多 registry 加速。
var extraRegistries = []registryMapping{
	{domain: "ghcr.io", upstream: "ghcr.io", authURL: "ghcr.io/token", service: "ghcr.io"},
	{domain: "quay.io", upstream: "quay.io", authURL: "quay.io/v2/auth", service: "quay.io"},
	{domain: "gcr.io", upstream: "gcr.io", authURL: "gcr.io/v2/auth", service: "gcr.io"},
	{domain: "registry.k8s.io", upstream: "registry.k8s.io", authURL: "", service: ""},
}

// matchRegistry 依据 /v2/ 之后的路径选择上游；命中域名前缀则走对应 registry，否则默认 Docker Hub。
func matchRegistry(rest string) (registryMapping, string) {
	for _, m := range extraRegistries {
		if m.domain != "" && strings.HasPrefix(rest, m.domain+"/") {
			return m, strings.TrimPrefix(rest, m.domain+"/")
		}
	}
	return dockerHub, rest
}

// dockerRepoName 从仓库路径提取镜像名（去掉 /manifests|/blobs|/tags 之后的部分）。
func dockerRepoName(repoPath string) string {
	for _, sep := range []string{"/manifests/", "/blobs/", "/tags/list"} {
		if i := strings.Index(repoPath, sep); i >= 0 {
			return repoPath[:i]
		}
	}
	return ""
}

// resolveTokenHost 依据 token 请求的 service 参数选择上游认证端点。
func resolveTokenHost(service string) string {
	switch service {
	case "", "registry.docker.io", "docker.io":
		return dockerHub.authURL
	case "ghcr.io":
		return "ghcr.io/token"
	case "quay.io":
		return "quay.io/v2/auth"
	case "gcr.io":
		return "gcr.io/v2/auth"
	default:
		return service + "/v2/auth"
	}
}

// ---------- 内存缓存（manifest / token） ----------

type memItem struct {
	data        []byte
	contentType string
	headers     map[string]string
	expiresAt   time.Time
}

type memCache struct {
	mu sync.Mutex
	m  map[string]*memItem
}

func newMemCache() *memCache { return &memCache{m: map[string]*memItem{}} }

func (c *memCache) Get(key string) *memItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	if it, ok := c.m[key]; ok {
		if time.Now().Before(it.expiresAt) {
			return it
		}
		delete(c.m, key)
	}
	return nil
}

func (c *memCache) Set(key string, data []byte, ct string, headers map[string]string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = &memItem{data: data, contentType: ct, headers: headers, expiresAt: time.Now().Add(ttl)}
}

var (
	dockerManifestCache = newMemCache()
	dockerTokenCache    = newMemCache()
)

// dockerClient 用于上游请求：不设整体超时（大层流式），由 ResponseHeaderTimeout 约束握手等待。
var dockerClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// handleDocker 是 Docker 加速入口，由 main.handle 在 [docker].enabled=true 时按前缀分发进来。
func (s *Server) handleDocker(w http.ResponseWriter, r *http.Request) {
	// 自定义路径（可选）：设置后 /v2 请求必须带该前缀，剥掉前缀再按原有逻辑处理。
	// registry 协议无法携带 Token，这条前缀相当于给 /v2 加了一道「只有知道路径的人能用」的保护，
	// 也为将来多用户（每个用户一条路径）留出扩展位。
	if p := s.dockerPath(); p != "" {
		prefix := "/v2/" + p
		isToken := r.URL.Path == "/token" || strings.HasPrefix(r.URL.Path, "/token/")
		if !isToken {
			switch {
			case r.URL.Path == prefix:
				r.URL.Path = "/v2/"
			case strings.HasPrefix(r.URL.Path, prefix+"/"):
				r.URL.Path = "/v2/" + strings.TrimPrefix(r.URL.Path, prefix+"/")
			default:
				// 未带自定义路径 → 一律 404，不暴露端点是否存在
				s.render404(w, r)
				return
			}
		}
	}
	switch {
	case r.URL.Path == "/token" || strings.HasPrefix(r.URL.Path, "/token/"):
		s.proxyDockerToken(w, r)
	case r.URL.Path == "/v2/" || strings.HasPrefix(r.URL.Path, "/v2/"):
		s.proxyDockerV2(w, r)
	default:
		s.render404(w, r)
	}
}

func (s *Server) proxyDockerV2(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v2/")
	mapping, repoPath := matchRegistry(rest)
	if name := dockerRepoName(repoPath); name != "" {
		if ok, reason := s.acl.CheckRepo(name); !ok {
			http.Error(w, "镜像访问被限制: "+reason, http.StatusForbidden)
			return
		}
	}
	target := "https://" + mapping.upstream + "/v2/" + repoPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	isBlob := strings.Contains(r.URL.Path, "/blobs/")
	isManifest := strings.Contains(r.URL.Path, "/manifests/")

	// 缓存命中
	if r.Method == http.MethodGet {
		if isBlob {
			if p, meta, hit := s.cache.Get(cacheKey(http.MethodGet, target)); hit {
				serveCached(w, r, p, meta)
				return
			}
		} else if isManifest {
			if it := dockerManifestCache.Get(dockerManifestKey(r, target)); it != nil {
				writeMemItem(w, it)
				return
			}
		}
	}

	req, err := http.NewRequest(r.Method, target, nil)
	if err != nil {
		http.Error(w, "构造请求失败", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(req, r)

	resp, err := dockerClient.Do(req)
	if err != nil {
		http.Error(w, "回源失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	rewriteAuthRealm(resp.Header, clientBaseURL(r)+"/token")

	// 上游错误（4xx/5xx）直接透传，便于客户端识别
	if resp.StatusCode >= 400 {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// manifest：读入内存并缓存（JSON 体积小）
	if isManifest && r.Method == http.MethodGet && resp.StatusCode == http.StatusOK {
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			http.Error(w, "读取 manifest 失败", http.StatusBadGateway)
			return
		}
		ct := resp.Header.Get("Content-Type")
		hdr := map[string]string{"Docker-Content-Digest": resp.Header.Get("Docker-Content-Digest")}
		dockerManifestCache.Set(dockerManifestKey(r, target), body, ct, hdr, manifestTTL(r.URL.Path))
		copyHeaders(w.Header(), resp.Header)
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if d := resp.Header.Get("Docker-Content-Digest"); d != "" {
			w.Header().Set("Docker-Content-Digest", d)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	// blob：写透磁盘缓存（内容寻址，缓存极稳定）
	canCacheBlob := isBlob && r.Method == http.MethodGet &&
		resp.StatusCode == http.StatusOK &&
		resp.ContentLength >= 0 && resp.ContentLength <= s.cache.MaxBytes()

	key := cacheKey(http.MethodGet, target)
	var cf *os.File
	if canCacheBlob {
		meta := CacheMeta{ContentType: resp.Header.Get("Content-Type")}
		if f, e := s.cache.Begin(key, meta); e == nil {
			cf = f
		}
	}

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)

	if cf != nil {
		_, err = io.Copy(io.MultiWriter(w, cf), resp.Body)
		cf.Close()
		if err != nil {
			s.cache.Abort(key)
			return
		}
		s.cache.Commit(key)
	} else {
		_, _ = io.Copy(w, resp.Body)
	}
}

func (s *Server) proxyDockerToken(w http.ResponseWriter, r *http.Request) {
	authHost := resolveTokenHost(r.URL.Query().Get("service"))
	target := "https://" + authHost
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	key := cacheKey("token", r.URL.RawQuery)
	if it := dockerTokenCache.Get(key); it != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(it.data)))
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(it.data)
		return
	}

	req, err := http.NewRequest(r.Method, target, nil)
	if err != nil {
		http.Error(w, "构造认证请求失败", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(req, r)

	resp, err := dockerClient.Do(req)
	if err != nil {
		http.Error(w, "认证回源失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if rerr != nil {
		http.Error(w, "读取认证响应失败", http.StatusBadGateway)
		return
	}
	if resp.StatusCode == http.StatusOK {
		dockerTokenCache.Set(key, body, "application/json", nil, tokenTTL(body))
	}

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// ---------- 辅助 ----------

func dockerManifestKey(r *http.Request, target string) string {
	return cacheKey("manifest", target+"|"+r.Header.Get("Accept"))
}

func writeMemItem(w http.ResponseWriter, it *memItem) {
	if it.contentType != "" {
		w.Header().Set("Content-Type", it.contentType)
	}
	for k, v := range it.headers {
		if v != "" {
			w.Header().Set(k, v)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(it.data)))
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(it.data)
}

// copyRequestHeaders 透传对拉取有意义的请求头，强制 identity 以便缓存原始字节。
func copyRequestHeaders(dst, src *http.Request) {
	skip := map[string]bool{"host": true, "connection": true, "content-length": true, "accept-encoding": true}
	for k, vv := range src.Header {
		if skip[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			dst.Header.Add(k, v)
		}
	}
	dst.Header.Set("Accept-Encoding", "identity")
}

// rewriteAuthRealm 把上游 Www-Authenticate 的 realm 统一改写到本机 /token。
func rewriteAuthRealm(h http.Header, realm string) {
	www := h.Get("Www-Authenticate")
	if www == "" {
		return
	}
	for _, u := range []string{dockerHub.authURL, "https://ghcr.io/token", "https://quay.io/v2/auth", "https://gcr.io/v2/auth"} {
		if u != "" {
			www = strings.ReplaceAll(www, "https://"+u, realm)
		}
	}
	// 兜底：auth.docker.io 根域
	www = strings.ReplaceAll(www, "https://auth.docker.io", realm)
	h.Set("Www-Authenticate", www)
}

// clientBaseURL 推断客户端访问本机的 base URL（考虑反代与 TLS）。
func clientBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
		scheme = xfp
	}
	return scheme + "://" + r.Host
}

// manifestTTL：按引用类型给不同缓存时长（digest 长驻，latest 短，其余中等）。
func manifestTTL(path string) time.Duration {
	if strings.Contains(path, "/manifests/sha256:") || strings.Contains(path, "@sha256:") {
		return 24 * time.Hour
	}
	if strings.HasSuffix(path, "/latest") {
		return 10 * time.Minute
	}
	return 30 * time.Minute
}

// tokenTTL 从 token 响应中提取安全 TTL。
func tokenTTL(body []byte) time.Duration {
	var tr struct {
		ExpiresIn int `json:"expires_in"`
	}
	if json.Unmarshal(body, &tr) == nil && tr.ExpiresIn > 0 {
		safe := time.Duration(tr.ExpiresIn-300) * time.Second
		if safe > 5*time.Minute {
			return safe
		}
	}
	return 30 * time.Minute
}
