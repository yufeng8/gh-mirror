package main

import (
	_ "embed"
	"net/http"
)

// faviconICO 是站点图标（16/32/48 三尺寸 ICO，由 logo.svg 渲染生成，见仓库根目录 favicon.ico）。
// 现代浏览器优先使用 htmlHead 里的内联 SVG data-URI favicon（零额外请求）；
// 本路由作为 alternate icon 兜底——老旧浏览器、保存书签 / 桌面快捷方式等场景。
//
//go:embed favicon.ico
var faviconICO []byte

// handleFavicon 返回站点图标 ICO（GET /favicon.ico）。
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconICO)
}
