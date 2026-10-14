package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 镜像搜索 + 标签浏览：代理 Docker Hub 公共 API，并提供服务端渲染的轻量 Web 页面。
// 说明：P5 离线打包暂缓，页面中相关入口以“开发中”占位提示。

const hubAPI = "https://hub.docker.com"

var searchTmpl = template.Must(template.New("search").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>镜像搜索 · github-mirror</title>` + pageCSS + `</head><body>
<header class="bar"><a class="brand" href="/search">github-mirror</a>
<nav><a href="/search">镜像搜索</a><a href="/admin">管理后台</a></nav></header>
<main>
<form class="sbox" action="/search" method="get">
<input name="q" value="{{.Query}}" placeholder="搜索 Docker 镜像，如 nginx / redis" autofocus>
<button type="submit">搜索</button></form>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
{{if .Results}}
<p class="meta">约 {{.Count}} 个结果</p>
<ul class="list">
{{range .Results}}
<li><div class="row"><a class="name" href="/tags?repo={{.Name}}">{{.Name}}</a>
{{if .IsOfficial}}<span class="tag off">官方</span>{{end}}
<span class="star">★ {{.StarCount}}</span></div>
<div class="desc">{{.Description}}</div></li>
{{end}}
</ul>
{{else if .Query}}<p class="meta">未找到与 “{{.Query}}” 相关的镜像。</p>{{end}}
</main></body></html>`))

var tagsTmpl = template.Must(template.New("tags").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Repo}} 标签 · github-mirror</title>` + pageCSS + `</head><body>
<header class="bar"><a class="brand" href="/search">github-mirror</a>
<nav><a href="/search">镜像搜索</a><a href="/admin">管理后台</a></nav></header>
<main>
<p><a href="/search">← 返回搜索</a></p>
<h2>{{.Repo}}</h2>
<p class="hint">拉取：<code>docker pull {{.Repo}}:&lt;tag&gt;</code>（Docker Hub 走 registry-mirror 加速）</p>
<p class="hint dim">离线打包功能开发中，敬请期待。</p>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
{{if .Results}}
<ul class="tags">
{{range .Results}}
<li><span class="tname">{{.Name}}</span><span class="tsize">{{.Size}}</span><span class="tdate">{{.LastUpdated}}</span></li>
{{end}}
</ul>
{{else if .Loaded}}<p class="meta">暂无标签。</p>{{end}}
</main></body></html>`))

const pageCSS = `<style>
*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;background:#0f1115;color:#e6e8eb}
.bar{display:flex;justify-content:space-between;align-items:center;padding:14px 22px;background:#171a21;border-bottom:1px solid #262b36}
.brand{color:#fff;font-weight:700;text-decoration:none;font-size:18px}.bar nav a{color:#9aa4b2;margin-left:16px;text-decoration:none}.bar nav a:hover{color:#fff}
main{max-width:760px;margin:0 auto;padding:24px 18px}
.sbox{display:flex;gap:10px;margin:8px 0 20px}.sbox input{flex:1;padding:12px 14px;border-radius:10px;border:1px solid #2a3040;background:#12151c;color:#e6e8eb;font-size:15px}
.sbox button{padding:12px 20px;border:none;border-radius:10px;background:#3b82f6;color:#fff;font-size:15px;cursor:hover}.sbox button:hover{background:#2f6fe0}
.meta{color:#8b94a3}.hint{color:#9aa4b2}.hint.dim{color:#6b7280}.err{color:#f87171}
.list{list-style:none;padding:0;margin:0}.list li{padding:14px 0;border-bottom:1px solid #232936}
.row{display:flex;align-items:center;gap:10px}.name{color:#7dd3fc;font-weight:600;text-decoration:none;font-size:16px}.name:hover{text-decoration:underline}
.tag{font-size:12px;padding:2px 8px;border-radius:999px}.tag.off{background:#133b2a;color:#4ade80}
.star{color:#fbbf24;font-size:13px}.desc{color:#9aa4b2;margin-top:6px;font-size:14px;line-height:1.5}
h2{color:#fff}.tags{list-style:none;padding:0;margin:0;columns:2}
.tags li{display:flex;justify-content:space-between;gap:10px;padding:8px 0;border-bottom:1px solid #232936;break-inside:avoid}
.tname{color:#e6e8eb;font-family:ui-monospace,monospace}.tsize,.tdate{color:#6b7280;font-size:12px}
code{background:#12151c;padding:2px 6px;border-radius:6px;color:#7dd3fc}
</style>`

type hubSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
	IsOfficial  bool   `json:"is_official"`
}

type hubSearchResp struct {
	Count   int               `json:"count"`
	Results []hubSearchResult `json:"results"`
}

type hubTagResult struct {
	Name        string `json:"name"`
	FullSize    int64  `json:"full_size"`
	LastUpdated string `json:"last_updated"`
}

type hubTagsResp struct {
	Count   int            `json:"count"`
	Results []hubTagResult `json:"results"`
}

// hubGet 请求 Docker Hub 公共 API（带超时，限长读取）。
func hubGet(r *http.Request, target string) ([]byte, string, int, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "github-mirror/1.0")
	resp, err := dockerClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	return body, ct, resp.StatusCode, err
}

// validRepo 校验仓库名，防止路径注入与 URL 路径穿越。
func validRepo(repo string) bool {
	if repo == "" || len(repo) > 200 {
		return false
	}
	if strings.Contains(repo, "..") || strings.HasPrefix(repo, "/") ||
		strings.HasSuffix(repo, "/") || strings.Contains(repo, "//") {
		return false
	}
	return strings.IndexFunc(repo, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '.' || r == '-' || r == '_' || r == '/':
			return false
		default:
			return true
		}
	}) < 0
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// handleSearchAPI /api/search?q=&page= 代理 Docker Hub 搜索 JSON。
func (s *Server) handleSearchAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	target := fmt.Sprintf("%s/v2/search/repositories/?query=%s&page=%s&page_size=25",
		hubAPI, url.QueryEscape(q), url.QueryEscape(page))
	body, ct, status, err := hubGet(r, target)
	if err != nil {
		http.Error(w, "上游请求失败", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// handleTagsAPI /api/tags?repo=owner/name 代理 Docker Hub 标签 JSON。
func (s *Server) handleTagsAPI(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !validRepo(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	target := fmt.Sprintf("%s/v2/repositories/%s/tags?page_size=50", hubAPI, repo)
	body, ct, status, err := hubGet(r, target)
	if err != nil {
		http.Error(w, "上游请求失败", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// handleSearchPage /search?q= 服务端渲染搜索结果页。
func (s *Server) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Query": q}
	if q != "" {
		target := fmt.Sprintf("%s/v2/search/repositories/?query=%s&page_size=25", hubAPI, url.QueryEscape(q))
		if body, _, status, err := hubGet(r, target); err == nil && status == http.StatusOK {
			var sr hubSearchResp
			if json.Unmarshal(body, &sr) == nil {
				data["Results"] = sr.Results
				data["Count"] = sr.Count
			} else {
				data["Error"] = "搜索结果解析失败"
			}
		} else {
			data["Error"] = "搜索服务暂时不可用"
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = searchTmpl.Execute(w, data)
}

// handleTagsPage /tags?repo=owner/name 服务端渲染标签列表页。
func (s *Server) handleTagsPage(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if !validRepo(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	data := map[string]any{"Repo": repo}
	target := fmt.Sprintf("%s/v2/repositories/%s/tags?page_size=50", hubAPI, repo)
	if body, _, status, err := hubGet(r, target); err == nil && status == http.StatusOK {
		var tr hubTagsResp
		if json.Unmarshal(body, &tr) == nil {
			type tagView struct {
				Name        string
				Size        string
				LastUpdated string
			}
			views := make([]tagView, 0, len(tr.Results))
			for _, t := range tr.Results {
				views = append(views, tagView{Name: t.Name, Size: humanSize(t.FullSize), LastUpdated: t.LastUpdated})
			}
			data["Results"] = views
			data["Loaded"] = true
		} else {
			data["Error"] = "标签解析失败"
		}
	} else {
		data["Error"] = "标签服务暂时不可用"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tagsTmpl.Execute(w, data)
}
