package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
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
	case sub == "service":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doToggleService(w, r)
	case sub == "cache":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doSetCache(w, r)
	case sub == "cache/clear":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doClearCache(w, r)
	case sub == "api/convert":
		if !s.isAuthed(r) {
			s.writeJSON(w, http.StatusUnauthorized, convertResp{OK: false, Hint: "未登录或会话已过期"})
			return
		}
		s.doConvertAPI(w, r)
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
	role := strings.TrimSpace(r.FormValue("role"))
	if tok == "" {
		tok = GenToken()
	}
	if !s.store.Has(tok) {
		s.store.Add(tok, note, role)
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
	// 只 Sprintf body 片段（loginBody 无 CSS 的 %），再用 pageApp() 全幅包裹（分屏登录）
	body := fmt.Sprintf(loginBody, s.adminPath, html.EscapeString(errMsg))
	_, _ = w.Write([]byte(pageApp("登录 - gh-mirror", "gh-mirror 管理后台登录", body)))
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
	var totalHits, totalBytes int64
	for _, t := range tokens {
		totalHits += t.Hits
		totalBytes += t.Bytes
		lastUsed := "—"
		if t.LastUsed > 0 {
			lastUsed = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
		}
		roleTxt, roleCls := "普通", "badge"
		if normRole(t.Role) == RoleAdmin {
			roleTxt, roleCls = "管理员", "badge on"
		}
		rows.WriteString(fmt.Sprintf(
			`<tr><td><code>%s</code></td><td>%s</td><td><span class="%s">%s</span></td><td>%s</td><td class="num">%d</td><td class="num">%s</td><td>%s</td><td>
<form method="post" action="%s/del" class="del-form">
<input type="hidden" name="csrf" value="%s">
<input type="hidden" name="token" value="%s">
<button type="submit" class="danger" onclick="return confirm('确定删除该 token？')">删除</button>
</form></td></tr>`,
			html.EscapeString(t.Token), html.EscapeString(t.Note), roleCls, roleTxt,
			time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"),
			t.Hits, formatBytes(t.Bytes), lastUsed,
			s.adminPath, csrf, html.EscapeString(t.Token)))
	}
	if len(tokens) == 0 {
		rows.WriteString(`<tr><td colspan="8" class="muted">暂无 token，请在下方添加</td></tr>`)
	}
	// 自动拼接当前访问地址：经反代时按 X-Forwarded-Proto/TLS 判定 https，否则 http；host 取请求 Host
	base := baseOf(r)
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
	if !s.hfEnabled() {
		hfEx = "HuggingFace 加速未开启（可在下方「服务管理」开启）"
	}
	dockerEx := "Docker 加速未开启（可在下方「服务管理」开启）"
	if s.dockerEnabled() {
		dockerEx = "离线 tar：" + base + "/offline/library/nginx:1.25 ；或 registry 加速：daemon.json 加 \"registry-mirrors\": [\"" + base + "\"] 后 systemctl restart docker"
	}
	var ex strings.Builder
	ex.WriteString(`<p class="muted">代理示例（已自动填入当前访问地址，复制即可用）：</p>`)
	ex.WriteString(`<p><strong>GitHub 资源</strong>（需 token）：<br><code>` + ghEx + `</code></p>`)
	ex.WriteString(`<p><strong>HuggingFace 权重</strong>（需 token）：<br><code>` + hfEx + `</code></p>`)
	ex.WriteString(`<p><strong>Docker 镜像</strong>（无需 token）：<br><code>` + dockerEx + `</code></p>`)
	example := ex.String()
	// Docker 开启时在后台顶部强提醒：/v2、/offline 端点无 Token 鉴权，公网暴露有滥用/带宽风险
	warnHTML := ""
	if s.dockerEnabled() {
		warnHTML = `<div class="alert danger"><strong>⚠️ 安全提醒：Docker 加速/离线镜像已开启</strong>（<code>DOCKER_ENABLED=true</code>）。端点 <code>/v2</code> 与 <code>/offline</code> <strong>不使用 Token 鉴权</strong>（Docker registry 协议无法携带 Token）。<br>· 若网关经公网/CDN 暴露，<strong>任何人都能使用你的镜像加速、拉取离线 tar</strong>，耗费上游带宽。<br>· 置于 CDN 后：<code>[acl] ip_allow</code> <strong>无法按真实客户端 IP 限制</strong>（源站只见 CDN 回源 IP，X-Forwarded-For 可伪造）。<br><strong>降低风险（任选）：</strong>1) 不需要公网 Docker 加速就设 <code>DOCKER_ENABLED=false</code>；2) 公网反代挡掉裸路径 <code>location ^~ /v2/ { return 404; }</code>、<code>location ^~ /offline/ { return 404; }</code>，/offline 改用带秘密前缀的地址；3) registry 用「不走 CDN 的难猜子域名」直连源站 + 云安全组按来源 IP 放行；4) 在 CDN（EdgeOne）对 <code>/v2</code>、<code>/offline</code> 配速率限制与带宽封顶（CDN 看得到真实客户端 IP）。详见 <code>docs/docker-deploy.md</code>「Docker 加速安全」。</div>`
	}
	// 侧栏导航（仅真实存在的入口）；品牌 Logo 点击回首页（顶栏不再重复放品牌）
	// 「文档」指向仓库 README 的「文档」小节——README 里汇总了全部部署/使用文档入口
	aside := `<aside><a class="brand" href="/" title="返回首页">` + logoSVG + `<span>gh-mirror</span></a><nav class="nav">` +
		`<a class="navitem active" href="` + s.adminPath + `"><span class="ic">▦</span>概览</a>` +
		`<a class="navitem" href="#services"><span class="ic">⚙️</span>服务管理</a>` +
		`<a class="navitem" href="#cache"><span class="ic">💾</span>缓存管理</a>` +
		`<a class="navitem" href="#tokens"><span class="ic">🔑</span>Token 管理</a>` +
		`<span class="navsep"></span>` +
		`<a class="navitem" href="` + repoURL + `/#文档" target="_blank" rel="noopener"><span class="ic">📄</span>文档</a>` +
		`<a class="navitem" href="` + repoURL + `" target="_blank" rel="noopener"><span class="ic">✦</span>GitHub</a>` +
		`<a class="navitem" href="` + s.adminPath + `/logout"><span class="ic">⎋</span>退出登录</a></nav></aside>`
	// 顶栏右侧：健康检查 CTA + 主题切换
	topbarRight := `<a class="btn outline" href="` + base + `/healthz" target="_blank" rel="noopener">健康检查</a>` +
		`<button id="themeToggle" class="btn outline" type="button" onclick="toggleTheme()" aria-label="切换深色 / 浅色模式">🌙 深色</button>`
	// 服务状态徽标（GitHub 恒开；Docker/HF 按开关）
	svcBadge := func(on bool, name, onTxt, offTxt string) string {
		if on {
			return `<span class="badge on">● ` + name + ` · ` + onTxt + `</span>`
		}
		return `<span class="badge off">○ ` + name + ` · ` + offTxt + `</span>`
	}
	badges := svcBadge(true, "GitHub 代理", "已开启", "未开启") +
		svcBadge(s.dockerEnabled(), "Docker 加速", "已开启", "未开启 · 可配置") +
		svcBadge(s.hfEnabled(), "HuggingFace", "已开启", "未开启 · 可配置")
	// 概览统计卡（网关真实指标，不伪造主机监控）
	stats := statCard("Token 数", fmt.Sprintf("%d", len(tokens)), "个") +
		statCard("总命中", fmt.Sprintf("%d", totalHits), "次") +
		statCard("总流量", formatBytes(totalBytes), "") +
		statCard("缓存占用", formatBytes(s.cache.UsageBytes()), "上限 "+formatBytes(s.cache.MaxBytes())) +
		statCard("运行时长", fmtDuration(time.Since(processStart)), "")
	// 链接转换器：识别与生成统一在服务端 buildAccelURL（与 MCP convert_link 同一实现）
	conv := `<div id="gmconv" data-api="` + s.adminPath + `/api/convert" data-csrf="` + csrf + `" hidden></div>` +
		`<div class="conv-row"><input id="gmconv-in" type="text" autocomplete="off" placeholder="粘贴 GitHub / HuggingFace 原始链接，或 Docker 镜像名（如 library/nginx:1.25）">` +
		`<button type="button" class="btn" onclick="gmConvert()">获取加速链接</button></div>` +
		`<div class="conv-row" style="margin-top:10px"><input id="gmconv-out" type="text" readonly placeholder="加速链接将显示在这里">` +
		`<button type="button" id="gmconv-copy" class="btn outline" onclick="gmCopyConv()">复制</button>` +
		`<button type="button" class="btn outline" onclick="gmDownloadConv()">下载</button>` +
		`<button type="button" class="btn outline" onclick="gmOpenConv()">访问</button></div>` +
		`<p class="muted small" style="margin-top:10px">基于当前访问地址与首个 token 生成；GitHub / HuggingFace 链接需 token，Docker 镜像名给出 <code>/offline</code> 离线 tar 地址。</p>`
	// 服务管理三卡：GitHub 恒开；Docker / HuggingFace 即时开关（持锁内存 + settings.json 持久化）
	svcCard := func(tag, name, desc, usage string, on bool, svc string) string {
		// 三张卡的底部标识统一宽高，视觉一致；GitHub 恒开用「默认开启」标识代替按钮
		state, foot := `<span class="badge on">● 已开启</span>`, `<span class="svccard-tag">默认开启</span>`
		if svc != "" {
			form := `<form method="post" action="` + s.adminPath + `/service" class="svc-toggle">` +
				`<input type="hidden" name="csrf" value="` + csrf + `">` +
				`<input type="hidden" name="name" value="` + svc + `">`
			if on {
				state = `<span class="badge on">● 已开启</span>`
				foot = form + `<input type="hidden" name="on" value="0"><button type="submit" class="btn svccard-btn">关闭</button></form>`
			} else {
				state = `<span class="badge off">○ 未开启</span>`
				foot = form + `<input type="hidden" name="on" value="1"><button type="submit" class="btn svccard-btn">开启</button></form>`
			}
		}
		return `<div class="svccard"><div class="svccard-head"><span class="tag">` + tag + `</span><h3>` + name + `</h3>` + state + `</div>` +
			`<p>` + desc + `</p><p><code>` + usage + `</code></p>` +
			`<div class="svccard-foot">` + foot + `</div></div>`
	}
	services := `<div class="svcgrid">` +
		svcCard("GH", "GitHub 资源加速", "覆盖 blob / raw、Releases、仓库打包 archive 与短链，支持 Range 断点续传。始终可用。", "/g/&lt;token&gt;/https://github.com/owner/repo", true, "") +
		svcCard("DK", "Docker 镜像加速", "registry-mirror 回源 Docker Hub / GHCR，并可产出可 docker load 的离线 tar。开启后请阅读顶部安全提醒。", "daemon.json → registry-mirrors；/offline/&lt;image&gt;", s.dockerEnabled(), "docker") +
		svcCard("HF", "HuggingFace 权重", "加速模型与 LFS 大文件下载，token 模式下需带 token。", "/hf/&lt;token&gt;/owner/repo/resolve/main/file", s.hfEnabled(), "hf") +
		`</div><p class="muted small" style="margin-top:12px">开关<strong>即时生效、无需重启</strong>，并持久化到 <code>gm-data/settings.json</code>（重启后保持）。后台设置优先于环境变量 <code>DOCKER_ENABLED</code> / <code>HF_ENABLED</code>。</p>`
	// 缓存管理：已用总量 / 单文件上限 / 总上限 / 时效，均热改；超限与过期自动清理；可一键清除
	maxFileMB := int(s.cache.MaxBytes() / (1024 * 1024))
	maxTotalMB := int(s.cache.MaxTotalBytes() / (1024 * 1024))
	ttlMin := int(s.cache.TTL() / time.Minute)
	totalTxt, ttlTxt := "不限", "永不过期"
	if maxTotalMB > 0 {
		totalTxt = formatBytes(s.cache.MaxTotalBytes())
	}
	if ttlMin > 0 {
		ttlTxt = fmt.Sprintf("%d 分钟", ttlMin)
	}
	cacheMgmt := `<div class="stats">` +
		statCard("缓存已用", formatBytes(s.cache.UsageBytes()), "总上限 "+totalTxt) +
		statCard("单文件上限", fmt.Sprintf("%d MB", maxFileMB), "超过则不缓存") +
		statCard("缓存时效", ttlTxt, "到期自动失效") +
		statCard("缓存目录", html.EscapeString(s.cfg.CacheDir), "") +
		`</div>
<form method="post" action="` + s.adminPath + `/cache" class="row" style="margin-top:16px;align-items:flex-end">
  <input type="hidden" name="csrf" value="` + csrf + `">
  <label class="fld">单文件上限(MB)<input name="maxFileMB" type="number" min="0" value="` + strconv.Itoa(maxFileMB) + `"></label>
  <label class="fld">总上限(MB，0=不限)<input name="maxTotalMB" type="number" min="0" value="` + strconv.Itoa(maxTotalMB) + `"></label>
  <label class="fld">时效(分钟，0=永不过期)<input name="ttlMinutes" type="number" min="0" value="` + strconv.Itoa(ttlMin) + `"></label>
  <button type="submit" class="btn">保存设置</button>
</form>
<form method="post" action="` + s.adminPath + `/cache/clear" class="svc-toggle" style="margin-top:12px" onsubmit="return confirm('确定清除全部缓存？不影响 Token 与设置。')">
  <input type="hidden" name="csrf" value="` + csrf + `">
  <button type="submit" class="btn outline">清除缓存</button>
</form>
<p class="muted small" style="margin-top:12px">单文件上限：单个响应超过该大小则只透传、不缓存。总上限：超出后<strong>按最旧优先</strong>自动清理。时效：到期文件由后台周期任务自动删除。设置即时生效并持久化，优先于环境变量 <code>MAX_CACHE_MB</code> / <code>CACHE_TTL_MINUTES</code>。</p>`
	// 新增 Token 表单
	addForm := `<form method="post" action="` + s.adminPath + `/add" class="row">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<input name="token" placeholder="留空则自动生成">` +
		`<input name="note" placeholder="备注（可选）">` +
		`<select name="role" title="角色：管理员可用于 MCP 接入" aria-label="Token 角色">` +
		`<option value="user">普通用户</option><option value="admin">管理员（MCP）</option></select>` +
		`<button type="submit" class="btn">添加</button></form>` +
		`<p class="muted small" style="margin-top:8px">角色说明：普通 token 仅用于代理访问；<strong>管理员 token</strong> 可用于 <code>MCP</code> 接入（<code>/mcp</code> 或 <code>gh-mirror mcp</code>），请勿随意外发。</p>`
	// 页脚链接
	footLinks := `<a href="` + s.adminPath + `/logout">退出登录</a> · <a href="/">返回首页</a> · <a href="` + repoURL + `" target="_blank" rel="noopener">GitHub</a>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, warn, badges, stats, converter, services, cacheMgmt, examples, rows, addForm, footLinks
	// 只 Sprintf body 片段（无 CSS 的 %），再用 pageApp() 全幅包裹
	body := fmt.Sprintf(dashboardBody, aside, topbarRight, warnHTML, badges, stats, conv, services, cacheMgmt, example, rows.String(), addForm, footLinks)
	_, _ = w.Write([]byte(pageApp("管理后台 - gh-mirror", "gh-mirror 管理后台", body)))
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

// statCard 生成一张概览统计卡（标题 / 数值 / 可选副标题）。
func statCard(k, v, sub string) string {
	out := `<div class="stat"><p class="k">` + k + `</p><p class="v">` + v + `</p>`
	if sub != "" {
		out += `<p class="s">` + sub + `</p>`
	}
	return out + `</div>`
}

// baseOf 推断当前访问的基地址（经反代时按 X-Forwarded-Proto / TLS 判定 https）。
func baseOf(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// doToggleService 运行时切换 Docker / HuggingFace 开关（登录 + CSRF，即时生效并持久化）。
func (s *Server) doToggleService(w http.ResponseWriter, r *http.Request) {
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
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	if name != "docker" && name != "hf" {
		http.Error(w, "未知服务（仅支持 docker / hf）", http.StatusBadRequest)
		return
	}
	on := r.FormValue("on") == "1" || strings.EqualFold(r.FormValue("on"), "true") || strings.EqualFold(r.FormValue("on"), "on")
	if err := s.setService(name, on); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("服务开关已调整: %s -> %v", name, on)
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

// doSetCache 运行时调整缓存参数（单文件上限 / 总上限 / 时效），即时生效并持久化。
func (s *Server) doSetCache(w http.ResponseWriter, r *http.Request) {
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
	maxFileMB := atoiOr(r.FormValue("maxFileMB"), -1)
	maxTotalMB := atoiOr(r.FormValue("maxTotalMB"), -1)
	ttlMinutes := atoiOr(r.FormValue("ttlMinutes"), -1)
	if maxFileMB < -1 || maxTotalMB < -1 || ttlMinutes < -1 {
		http.Error(w, "参数不合法（应为非负整数）", http.StatusBadRequest)
		return
	}
	_ = s.setCacheLimits(maxFileMB, maxTotalMB, ttlMinutes)
	log.Printf("缓存参数已调整: 单文件=%dMB 总计=%dMB 时效=%d分钟",
		int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute))
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

// doClearCache 清空缓存目录内的所有文件（不影响 Token 与设置）。
func (s *Server) doClearCache(w http.ResponseWriter, r *http.Request) {
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
	files, bytes := s.cache.Clear()
	log.Printf("缓存已清除: %d 个文件，%s", files, formatBytes(bytes))
	http.Redirect(w, r, s.adminPath, http.StatusFound)
}

// convertResp 是链接转换 API（POST /admin/api/convert）的 JSON 响应。
type convertResp struct {
	OK   bool   `json:"ok"`
	URL  string `json:"url,omitempty"`
	Hint string `json:"hint,omitempty"`
}

// doConvertAPI 链接转换（需登录；只读、无副作用）。生成逻辑与 MCP convert_link 共用 buildAccelURL。
func (s *Server) doConvertAPI(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	raw := strings.TrimSpace(r.FormValue("url"))
	tk := ""
	if toks := s.store.All(); len(toks) > 0 {
		tk = toks[0].Token
	}
	res := s.buildAccelURL(baseOf(r), raw, tk)
	if res.URL == "" {
		hint := res.Hint
		if hint == "" {
			hint = "无法识别该输入"
		}
		s.writeJSON(w, http.StatusOK, convertResp{OK: false, Hint: hint})
		return
	}
	s.writeJSON(w, http.StatusOK, convertResp{OK: true, URL: res.URL})
}

// writeJSON 输出 JSON 响应。
func (s *Server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

// accelResult 描述一次链接转换的结果：URL 为空时 Hint 说明原因。
type accelResult struct {
	Kind string // github / hf / docker / ""
	URL  string // 加速链接（成功时非空）
	Hint string // 失败或降级时的说明
}

var (
	reURLMatch    = regexp.MustCompile(`^https?://([^/]+)(/.*)?$`)
	reHFBlob      = regexp.MustCompile(`^([^/]+/[^/]+)/blob/(.+)$`)
	reDockerImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*(?:/[a-zA-Z0-9._-]+)*(?::[A-Za-z0-9_.-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`)
)

// buildAccelURL 把用户粘贴的原始链接 / Docker 镜像名转换为本站加速链接。
// 这是后台「链接转换」与 MCP convert_link 的唯一实现（一个实现、两个消费者）。
// base 为当前访问基地址，tk 为要嵌入的 token（HF / GitHub 需要）。
func (s *Server) buildAccelURL(base, raw, tk string) accelResult {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return accelResult{Hint: "请输入 GitHub / HuggingFace 链接，或 Docker 镜像名"}
	}
	if m := reURLMatch.FindStringSubmatch(raw); m != nil {
		host := strings.ToLower(m[1])
		rest := strings.TrimLeft(m[2], "/")
		switch {
		case isGitHubHost(host):
			if tk == "" {
				return accelResult{Kind: "github", Hint: "请先在「Token 管理」添加一个 token"}
			}
			return accelResult{Kind: "github", URL: base + "/g/" + tk + "/" + raw}
		case isHFHost(host):
			if !s.hfEnabled() {
				return accelResult{Kind: "hf", Hint: "HuggingFace 加速未开启，请先在「服务管理」开启"}
			}
			if tk == "" {
				return accelResult{Kind: "hf", Hint: "请先在「Token 管理」添加一个 token"}
			}
			// /<owner>/<repo>/blob/<rev>/<file> 统一规范为 /resolve/ 形式
			if mm := reHFBlob.FindStringSubmatch(rest); mm != nil {
				rest = mm[1] + "/resolve/" + mm[2]
			}
			return accelResult{Kind: "hf", URL: base + "/hf/" + tk + "/" + rest}
		default:
			return accelResult{Hint: "暂不支持该域名（支持 github.com / huggingface.co，或 Docker 镜像名如 library/nginx:1.25）"}
		}
	}
	if reDockerImage.MatchString(raw) {
		if !s.dockerEnabled() {
			return accelResult{Kind: "docker", Hint: "Docker 加速未开启，请先在「服务管理」开启"}
		}
		return accelResult{Kind: "docker", URL: base + "/offline/" + raw}
	}
	return accelResult{Hint: "无法识别：支持 github.com / huggingface.co 链接，或 Docker 镜像名如 library/nginx:1.25"}
}

// isGitHubHost 判断主机是否属于 GitHub 资源域（含 raw / objects / codeload / gist / api）。
func isGitHubHost(h string) bool {
	return h == "github.com" || h == "gist.github.com" || h == "api.github.com" ||
		h == "raw.githubusercontent.com" || h == "objects.githubusercontent.com" ||
		h == "codeload.github.com" ||
		strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".githubusercontent.com")
}

// isHFHost 判断主机是否属于 HuggingFace 域。
func isHFHost(h string) bool {
	return h == "huggingface.co" || strings.HasSuffix(h, ".huggingface.co")
}

// atoiOr 解析整数，空串或解析失败时返回默认值 def。
func atoiOr(v string, def int) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// fmtDuration 把时长格式化为中文可读形式（用于后台"运行时长"）。
func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return "刚刚启动"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d 天 %d 小时", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d 小时 %d 分", hours, mins)
	default:
		return fmt.Sprintf("%d 分钟", mins)
	}
}
