package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const tokenPrefix = "/g/"

// statsWriter 包装 ResponseWriter，统计本次响应写入的字节数与状态码。
type statsWriter struct {
	http.ResponseWriter
	bytes int64
	code  int
}

func (w *statsWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statsWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statsWriter) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}

// handleProxy 处理 /g/<token>/<github-url>：校验 token 后回源或命中缓存。
// 无效 token 或无法解析的目标一律返回 404。
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	// 使用原始 RequestURI，保留 GitHub 地址中的双斜杠（https://）
	rawURI := r.RequestURI
	if !strings.HasPrefix(rawURI, tokenPrefix) {
		s.render404(w, r)
		return
	}
	rest := rawURI[len(tokenPrefix):]
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		s.render404(w, r)
		return
	}
	token := rest[:idx]
	if !s.store.Has(token) {
		s.render404(w, r)
		return
	}
	// 用计数包装器统计流量；仅对成功的代理响应累加统计
	sw := &statsWriter{ResponseWriter: w}
	defer func() {
		if sw.status() < 400 {
			s.store.RecordHit(token, sw.bytes)
		}
	}()

	// remain 形如 /https://github.com/owner/repo/...?query
	remain := rest[idx:]
	target, ok := resolveTarget(remain)
	if !ok {
		s.render404(sw, r)
		return
	}
	tu, err := url.Parse(target)
	if err != nil || tu.Scheme == "" || tu.Host == "" {
		s.render404(sw, r)
		return
	}
	dest := rewrite(tu)
	if dest == nil {
		http.Error(sw, "仅支持 GitHub 相关域名: "+tu.Host, http.StatusForbidden)
		return
	}
	if ok, reason := s.acl.CheckRepo(githubRepoName(tu)); !ok {
		http.Error(sw, "仓库访问被限制: "+reason, http.StatusForbidden)
		return
	}

	s.serveTarget(sw, r, dest)
}

// serveTarget 在缓存/并发锁之下回源或返回缓存；GitHub 与 HuggingFace 等 handler 共用。
func (s *Server) serveTarget(w http.ResponseWriter, r *http.Request, dest *url.URL) {
	key := cacheKey(r.Method, dest.String())
	if r.Method == http.MethodGet {
		if p, meta, hit := s.cache.Get(key); hit {
			serveCached(w, r, p, meta)
			return
		}
	}
	// 同一 URL 串行回源，避免并发惊群
	lk := s.cache.lock(key)
	lk.Lock()
	defer lk.Unlock()
	if r.Method == http.MethodGet {
		if p, meta, hit := s.cache.Get(key); hit {
			serveCached(w, r, p, meta)
			return
		}
	}
	s.fetch(w, r, dest, key)
}

// githubRepoName 从 GitHub URL 提取 owner/repo（用于仓库级 ACL）。
func githubRepoName(u *url.URL) string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}

// fetch 真正回源并流式返回，成功的完整 GET 响应会顺带写入缓存。
func (s *Server) fetch(w http.ResponseWriter, r *http.Request, dest *url.URL, key string) {
	req, err := http.NewRequest(r.Method, dest.String(), nil)
	if err != nil {
		http.Error(w, "构造请求失败", http.StatusInternalServerError)
		return
	}
	// 透传对下载有意义的请求头
	for _, h := range []string{"Range", "Accept", "Accept-Language", "User-Agent", "Referer", "Cookie"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "github-mirror/1.0 (+https://github.com/zzidcn/github-mirror)")
	}
	// 强制 identity，便于缓存原始字节（不透明压缩）
	req.Header.Set("Accept-Encoding", "identity")

	client := &http.Client{
		Timeout: s.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向次数过多")
			}
			return nil
		},
	}
	if s.dial != nil {
		client.Transport = transportWithDial(s.dial)
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "回源失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 上游错误（4xx/5xx）直接透传，方便排查
	if resp.StatusCode >= 400 {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	canCache := r.Method == http.MethodGet &&
		resp.StatusCode == http.StatusOK &&
		resp.ContentLength >= 0 &&
		resp.ContentLength <= s.cache.maxBytes

	var cf *os.File
	if canCache {
		meta := CacheMeta{
			ContentType: resp.Header.Get("Content-Type"),
			Disposition: resp.Header.Get("Content-Disposition"),
		}
		if f, e := s.cache.Begin(key, meta); e == nil {
			cf = f
		}
	}

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)

	if cf != nil {
		// 同时写给客户端与缓存文件；客户端中途断开则丢弃缓存
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

// serveCached 从磁盘缓存直接返回，并支持 Range 断点续传。
func serveCached(w http.ResponseWriter, r *http.Request, p string, meta CacheMeta) {
	f, err := os.Open(p)
	if err != nil {
		http.Error(w, "缓存读取失败", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "缓存读取失败", http.StatusInternalServerError)
		return
	}
	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	}
	if meta.Disposition != "" {
		w.Header().Set("Content-Disposition", meta.Disposition)
	}
	w.Header().Set("X-Cache", "HIT")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// copyHeaders 复制上游响应头，剔除逐跳（hop-by-hop）头。
func copyHeaders(dst, src http.Header) {
	skip := map[string]bool{
		"connection":        true,
		"transfer-encoding": true,
		"keep-alive":        true,
		"proxy-connection":  true,
		"te":                true,
		"trailer":           true,
		"content-encoding":  true,
		"x-cache":           true, // 保留我们自己的 MISS/HIT 标记，不被上游覆盖
	}
	for k, vv := range src {
		if skip[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
