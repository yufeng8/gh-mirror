package main

import (
	"net/url"
	"os"
	"path"
	"strings"
)

// useJsdelivr 开启后，对仓库源码文件（blob/raw）走 cdn.jsdelivr.net 边缘缓存，
// 可进一步省服务器带宽，但仅支持公开仓库文件、单文件约 20MB 上限、且 ref 不能是 HEAD。
var useJsdelivr = os.Getenv("USE_JSDELIVR") == "1"

// allowedHosts 仅允许回源到这些 GitHub/HuggingFace 相关域名，防止被用作开放代理（SSRF）。
var allowedHosts = map[string]bool{
	"github.com":                    true,
	"www.github.com":                true,
	"raw.githubusercontent.com":     true,
	"gist.githubusercontent.com":    true,
	"codeload.github.com":           true,
	"objects.githubusercontent.com": true,
	"huggingface.co":                true,
	"www.huggingface.co":            true,
	"cdn-lfs.huggingface.co":        true,
	"api.github.com":                true,
	"raw.github.com":                true,
}

func isAllowed(host string) bool {
	return allowedHosts[strings.ToLower(strings.TrimSuffix(host, "."))]
}

// fileExt 正确处理 .tar.gz / .tgz 这类复合后缀。
func fileExt(s string) string {
	if strings.HasSuffix(s, ".tar.gz") {
		return ".tar.gz"
	}
	if strings.HasSuffix(s, ".tgz") {
		return ".tgz"
	}
	return path.Ext(s)
}

// rewrite 把 GitHub 的网页地址重写成真正可下载的端点：
//   - github.com/owner/repo/blob|raw/<ref>/<path> -> raw.githubusercontent.com
//   - github.com/owner/repo/archive/<ref>.{zip,tar.gz,...} -> codeload.github.com
//   - github.com/owner/repo/releases/download/... -> 直连 github.com（会 302 到 objects.githubusercontent.com）
//   - 其余 GitHub 域名（raw/codeload/objects/gist/api）-> 直接透传
//
// 返回 nil 表示域名不在白名单内。
func rewrite(u *url.URL) *url.URL {
	host := strings.ToLower(u.Host)
	if !isAllowed(host) {
		return nil
	}
	if host != "github.com" && host != "www.github.com" {
		return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 {
		return &url.URL{Scheme: "https", Host: "github.com", Path: u.Path, RawQuery: u.RawQuery}
	}
	owner, repo, kind := parts[0], parts[1], parts[2]

	switch kind {
	case "blob", "raw":
		if len(parts) < 5 {
			return &url.URL{Scheme: "https", Host: "github.com", Path: u.Path, RawQuery: u.RawQuery}
		}
		ref := parts[3]
		file := strings.Join(parts[4:], "/")
		if useJsdelivr {
			if ref == "HEAD" {
				ref = "main"
			}
			return &url.URL{Scheme: "https", Host: "cdn.jsdelivr.net", Path: "/gh/" + owner + "/" + repo + "@" + ref + "/" + file}
		}
		return &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: "/" + owner + "/" + repo + "/" + ref + "/" + file}
	case "archive":
		rest := strings.Join(parts[3:], "/")
		ext := fileExt(rest)
		base := strings.TrimSuffix(rest, ext)
		format := "zip"
		switch ext {
		case ".tar.gz", ".tgz":
			format = "tar.gz"
		case ".tar":
			format = "tar"
		}
		return &url.URL{Scheme: "https", Host: "codeload.github.com", Path: "/" + owner + "/" + repo + "/" + format + "/" + base}
	case "releases":
		return &url.URL{Scheme: "https", Host: "github.com", Path: u.Path, RawQuery: u.RawQuery}
	default:
		return &url.URL{Scheme: "https", Host: "github.com", Path: u.Path, RawQuery: u.RawQuery}
	}
}

// resolveTarget 把 token 之后的剩余路径解析为 GitHub 地址：
//   - /https://github.com/...          -> 完整 URL（最常用）
//   - /gh/owner/repo/...               -> github.com/owner/repo/...
func resolveTarget(uri string) (string, bool) {
	if strings.HasPrefix(uri, "/gh/") {
		return "https://github.com" + strings.TrimPrefix(uri, "/gh"), true
	}
	trimmed := strings.TrimPrefix(uri, "/")
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed, true
	}
	return "", false
}
