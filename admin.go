package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookie = "ghm_session"
const sessionMaxAge = 7 * 24 * 3600

// parseSession 校验会话 Cookie 的签名与过期时间，返回会话内的随机 sid。
func (s *Server) parseSession(r *http.Request) (sid string, ok bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	parts := strings.SplitN(c.Value, ".", 3)
	if len(parts) != 3 {
		return "", false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(parts[2])) != 1 {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", false
	}
	if time.Now().Unix() >= exp {
		return "", false
	}
	return parts[0], true
}

// isAuthed 校验请求是否携带有效会话 Cookie。
func (s *Server) isAuthed(r *http.Request) bool {
	_, ok := s.parseSession(r)
	return ok
}

// signSession 生成带随机 sid、过期时间与 HMAC 签名的会话值。
func (s *Server) signSession() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	sid := hex.EncodeToString(buf)
	exp := time.Now().Add(sessionMaxAge * time.Second).Unix()
	payload := sid + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

// csrfToken 由 sid 确定性派生，无需额外存储（SameSite=Strict 已限定同源）。
func (s *Server) csrfToken(sid string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("csrf:" + sid))
	return hex.EncodeToString(mac.Sum(nil))
}

// validCSRF 恒定时间比较提交值与期望值。
func (s *Server) validCSRF(sid, submitted string) bool {
	if submitted == "" {
		return false
	}
	expected := s.csrfToken(sid)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(submitted)) == 1
}

// handleAdmin 处理后台路由（登录/登出/增删 token）。
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, s.adminPath), "/")
	switch {
	case sub == "" || sub == "login":
		if r.Method == http.MethodPost {
			s.doLogin(w, r)
			return
		}
		if s.isAuthed(r) {
			s.renderDashboard(w, r)
			return
		}
		s.renderLogin(w, r, "")
	case sub == "logout":
		s.doLogout(w, r)
	case sub == "add":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doAddToken(w, r)
	case sub == "del":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doDelToken(w, r)
	default:
		s.render404(w, r)
	}
}

func (s *Server) doLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	user := r.FormValue("user")
	pass := r.FormValue("pass")
	if subtle.ConstantTimeCompare([]byte(user), []byte(s.adminUser)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(s.adminPass)) == 1 {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    s.signSession(),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   s.cookieSecure,
			MaxAge:   sessionMaxAge,
		})
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	s.renderLogin(w, r, "账号或密码错误")
}

func (s *Server) doLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

func (s *Server) doAddToken(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Error(w, "CSRF 校验失败", http.StatusForbidden)
		return
	}
	tok := strings.TrimSpace(r.FormValue("token"))
	note := strings.TrimSpace(r.FormValue("note"))
	if tok == "" {
		tok = GenToken()
	}
	if !s.store.Has(tok) {
		s.store.Add(tok, note)
	}
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

func (s *Server) doDelToken(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Error(w, "CSRF 校验失败", http.StatusForbidden)
		return
	}
	tok := r.FormValue("token")
	if tok != "" {
		s.store.Delete(tok)
	}
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 只对 body 片段做 Sprintf（body 内无 CSS 的 %），再用 page() 包裹，避免 CSS 中的 % 被误当作格式化动词
	body := fmt.Sprintf(loginBody, s.adminPath, html.EscapeString(errMsg))
	_, _ = w.Write([]byte(page("管理员登录 - gh-mirror", "管理员登录", body)))
}

func (s *Server) renderDashboard(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	tokens := s.store.All()
	var rows strings.Builder
	for _, t := range tokens {
		lastUsed := "—"
		if t.LastUsed > 0 {
			lastUsed = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
		}
		rows.WriteString(fmt.Sprintf(
			`<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td class="num">%d</td><td class="num">%s</td><td>%s</td><td>
<form method="post" action="%s/del" class="del-form">
<input type="hidden" name="csrf" value="%s">
<input type="hidden" name="token" value="%s">
<button type="submit" class="danger" onclick="return confirm('确定删除该 token？')">删除</button>
</form></td></tr>`,
			html.EscapeString(t.Token), html.EscapeString(t.Note),
			time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"),
			t.Hits, formatBytes(t.Bytes), lastUsed,
			s.adminPath, csrf, html.EscapeString(t.Token)))
	}
	if len(tokens) == 0 {
		rows.WriteString(`<tr><td colspan="7" class="muted">暂无 token，请在下方添加</td></tr>`)
	}
	// 自动拼接当前访问地址：经反代时按 X-Forwarded-Proto/TLS 判定 https，否则 http；host 取请求 Host
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	base := scheme + "://" + html.EscapeString(r.Host)
	tk := ""
	if len(tokens) > 0 {
		tk = tokens[0].Token
	}
	// 三个服务的代理示例：GitHub/HuggingFace 需 token；Docker 无需 token；HF/Docker 按开关显示
	ghEx, hfEx := "请先在下方添加一个 token", "请先在下方添加一个 token"
	if tk != "" {
		ghEx = base + "/g/" + tk + "/https://github.com/yufeng8/gh-mirror/blob/main/README.md"
		hfEx = base + "/hf/" + tk + "/openai-community/gpt2/resolve/main/config.json"
	}
	if !s.cfg.HF.Enabled {
		hfEx = "HuggingFace 加速未开启（设 HF_ENABLED=true）"
	}
	dockerEx := "Docker 加速未开启（设 DOCKER_ENABLED=true）"
	if s.cfg.Docker.Enabled {
		dockerEx = "离线 tar：" + base + "/offline/library/nginx:1.25 ；或 registry 加速：daemon.json 加 \"registry-mirrors\": [\"" + base + "\"] 后 systemctl restart docker"
	}
	var ex strings.Builder
	ex.WriteString(`<p class="muted">代理示例（已自动填入当前访问地址，复制即可用）：</p>`)
	ex.WriteString(`<p><strong>GitHub 资源</strong>（需 token）：<br><code>` + ghEx + `</code></p>`)
	ex.WriteString(`<p><strong>HuggingFace 权重</strong>（需 token）：<br><code>` + hfEx + `</code></p>`)
	ex.WriteString(`<p><strong>Docker 镜像</strong>（无需 token）：<br><code>` + dockerEx + `</code></p>`)
	example := ex.String()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: example, rows, add-action, add-form-csrf, logout-href
	// 只对 body 片段做 Sprintf（body 内无 CSS 的 %），再用 page() 包裹，避免 CSS 中的 % 被误当作格式化动词
	body := fmt.Sprintf(dashboardBody, example, rows.String(), s.adminPath, csrf, s.adminPath)
	_, _ = w.Write([]byte(page("管理后台 - gh-mirror", "管理后台", body)))
}

// formatBytes 把字节数格式化为易读单位。
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div := float64(unit)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, u := range units {
		nf := float64(n) / div
		if nf < unit {
			return fmt.Sprintf("%.1f %s", nf, u)
		}
		div *= unit
	}
	return fmt.Sprintf("%.1f PB", float64(n)/div)
}
