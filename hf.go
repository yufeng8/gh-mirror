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
// token 鉴权模式下为 /hf/<用户名>/<token>/... 形式（用户名段反查用户表 + token 归属校验，与 /g /offline 一致）；
// 开放模式（auth.mode=open）保持 /hf/<owner>/<repo>/... 原形式（无鉴权，用户名段无意义）。
func (s *Server) handleHF(w http.ResponseWriter, r *http.Request) {
	rawURI := r.RequestURI
	if !strings.HasPrefix(rawURI, hfPrefix) {
		s.render404(w, r)
		return
	}
	rest := rawURI[len(hfPrefix):]
	sw := &statsWriter{ResponseWriter: w}

	if s.cfg.Auth.Mode == "token" {
		// 第一段为用户名段：反查用户表，不存在一律 404（不暴露用户是否存在）
		idx := strings.IndexByte(rest, '/')
		if idx < 0 {
			s.render404(sw, r)
			return
		}
		user, userOK := s.userByDockerSegment(rest[:idx])
		if !userOK {
			s.render404(sw, r)
			return
		}
		rest = rest[idx+1:]
		// 第二段为 token：必须存在且归属该用户（空 owner 归默认管理员）
		idx = strings.IndexByte(rest, '/')
		if idx < 0 {
			s.render404(sw, r)
			return
		}
		token := rest[:idx]
		t, exists := s.store.Get(token)
		if !exists || (t.Owner != user && !(t.Owner == "" && user == s.adminUser)) {
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
