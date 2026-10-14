package main

import (
	"net/http"
	"net/url"
	"strings"
)

const hfPrefix = "/hf/"

// buildHFTarget 把 /hf/ 之后的剩余路径解析为 HuggingFace 地址：
//   - <owner>/<repo>/resolve/<rev>/<file> -> https://huggingface.co/<owner>/<repo>/resolve/<rev>/<file>
//   - https://huggingface.co/...          -> 原样透传
func buildHFTarget(rest string) string {
	rest = strings.TrimPrefix(rest, "/")
	if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
		return rest
	}
	return "https://huggingface.co/" + rest
}

// hfRepoName 提取 owner/repo；完整 URL 形式返回空（跳过仓库级 ACL）。
func hfRepoName(rest string) string {
	rest = strings.TrimPrefix(rest, "/")
	if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}

// handleHF HuggingFace 模型/LFS 加速入口，由 main.handle 在 [hf].enabled=true 时按 /hf/ 前缀分发。
// 复用与 GitHub 相同的 resolve/rewrite/serveTarget/磁盘缓存/流式管线（域名白名单见 rewrite.go）。
// token 鉴权模式下沿用 /hf/<token>/... 形式，保持与 GitHub 一致的安全模型。
func (s *Server) handleHF(w http.ResponseWriter, r *http.Request) {
	rawURI := r.RequestURI
	if !strings.HasPrefix(rawURI, hfPrefix) {
		s.render404(w, r)
		return
	}
	rest := rawURI[len(hfPrefix):]
	sw := &statsWriter{ResponseWriter: w}

	if s.cfg.Auth.Mode == "token" {
		idx := strings.IndexByte(rest, '/')
		if idx < 0 {
			s.render404(sw, r)
			return
		}
		token := rest[:idx]
		if !s.store.Has(token) {
			s.render404(sw, r)
			return
		}
		defer func() {
			if sw.status() < 400 {
				s.store.RecordHit(token, sw.bytes)
			}
		}()
		rest = rest[idx:]
	}
	if ok, reason := s.acl.CheckRepo(hfRepoName(rest)); !ok {
		http.Error(sw, "仓库访问被限制: "+reason, http.StatusForbidden)
		return
	}

	target := buildHFTarget(rest)
	tu, err := url.Parse(target)
	if err != nil || tu.Scheme == "" || tu.Host == "" {
		s.render404(sw, r)
		return
	}
	dest := rewrite(tu)
	if dest == nil {
		http.Error(sw, "仅支持 HuggingFace 相关域名: "+tu.Host, http.StatusForbidden)
		return
	}
	s.serveTarget(sw, r, dest)
}
