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
	case sub == "tokens":
		if s.isAuthed(r) {
			s.renderTokensPage(w, r)
			return
		}
		http.Redirect(w, r, s.adminPath, http.StatusFound)
	case sub == "tokens/edit":
		if s.isAuthed(r) {
			s.doEditToken(w, r)
			return
		}
		http.Redirect(w, r, s.adminPath, http.StatusFound)
	case sub == "mcp":
		if s.isAuthed(r) {
			s.renderMCPPage(w, r)
			return
		}
		http.Redirect(w, r, s.adminPath, http.StatusFound)
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
		http.Redirect(w, r, s.adminPath+"/tokens?err=csrf", http.StatusFound)
		return
	}
	tok := strings.TrimSpace(r.FormValue("token"))
	note := strings.TrimSpace(r.FormValue("note"))
	role := strings.TrimSpace(r.FormValue("role"))
	if tok == "" {
		tok = GenToken()
	}
	code := "token_added"
	if s.store.Has(tok) {
		code = "token_exists"
	} else {
		// 新 token 归属当前登录用户（多用户预留：每位用户各自管理自己的 token）
		s.store.Add(tok, note, role, s.adminUser)
	}
	// 跳回 Token 管理页（该页才是操作上下文），并带上一次性反馈码
	http.Redirect(w, r, s.adminPath+"/tokens?ok="+code, http.StatusFound)
}

func (s *Server) doDelToken(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/tokens?err=csrf", http.StatusFound)
		return
	}
	tok := r.FormValue("token")
	code := "token_missing"
	if tok != "" && s.store.Has(tok) {
		s.store.Delete(tok)
		code = "token_deleted"
	}
	http.Redirect(w, r, s.adminPath+"/tokens?ok="+code, http.StatusFound)
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
	var totalHits, totalBytes int64
	for _, t := range tokens {
		totalHits += t.Hits
		totalBytes += t.Bytes
	}
	// 自动拼接当前访问地址：经反代时按 X-Forwarded-Proto/TLS 判定 https，否则 http；host 取请求 Host
	base := baseOf(r)
	tk := ""
	if len(tokens) > 0 {
		tk = tokens[0].Token
	}
	// 用法示例：token 包在 .tslot 中，切换右上角选择器即可联动替换（示例 + 链接转换共用）
	ghEx, hfEx := "请先到「Token 管理」创建一个 token", "请先到「Token 管理」创建一个 token"
	if tk != "" {
		ghEx = base + "/g/" + tokSlot(tk) + "/https://github.com/yufeng8/gh-mirror/blob/main/README.md"
		hfEx = base + "/hf/" + tokSlot(tk) + "/openai-community/gpt2/resolve/main/config.json"
	}
	if !s.hfEnabled() {
		hfEx = "HuggingFace 加速未开启（可在「服务管理」开启）"
	}
	dockerEx := "Docker 加速未开启（可在「服务管理」开启）"
	if s.dockerEnabled() {
		// 离线包必须带 token：没有 token 时给引导文案，不能拼出字面 "token" 占位（否则像是已经固定好了）
		offlineEx := "请先到「Token 管理」创建一个 token（离线包必须带 token）"
		if tk != "" {
			offlineEx = base + "/offline/" + tokSlot(tk) + "/library/nginx:1.25"
		}
		// 拉取路径固定为 <用户名>[/<自定义路径>]，因此 registry-mirrors 不再适用，统一用带路径的 pull
		dockerEx = "离线 tar（需 token）：" + offlineEx +
			" ；registry 加速（无需 token，按带路径方式 pull）：<code>docker pull " + base + "/" +
			html.EscapeString(s.dockerV2Prefix()) + "/library/nginx:1.25</code>"
	}
	var ex strings.Builder
	ex.WriteString(`<div class="row" style="justify-content:space-between;align-items:flex-start;gap:10px;margin-bottom:12px">`)
	ex.WriteString(`<p class="muted" style="margin:0;flex:1;min-width:220px">已自动填入当前访问地址；下方示例与「链接转换」都会使用这里选定的 Token。</p>`)
	ex.WriteString(s.tokenSelector(tokens, "示例使用的 Token"))
	ex.WriteString(`</div>`)
	ex.WriteString(`<p><strong>GitHub 资源</strong>（需 token）：<br><code>` + ghEx + `</code></p>`)
	ex.WriteString(`<p><strong>HuggingFace 权重</strong>（需 token）：<br><code>` + hfEx + `</code></p>`)
	ex.WriteString(`<p><strong>Docker 镜像</strong>（仅离线 tar 需 token；registry 加速走 <code>/v2</code> 由 Docker 客户端直连、无法带 token）：<br><code>` + dockerEx + `</code></p>`)
	example := ex.String()
	// 操作反馈条（保存 / 创建 / 开关等）；Docker 风险说明改由弹窗承载，不再常驻页面
	flashHTML := s.flashHTML(r)
	// 侧栏与顶栏（各页面共用组件；小屏侧栏为抽屉）
	aside := s.consoleAside(r)
	topbarRight := s.topbarRight(base)
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
	// 概览只放与 token 流量相关的指标；缓存细节在下方「缓存管理」里展示，避免重复
	stats := statCard("Token 数", fmt.Sprintf("%d", len(tokens)), "个") +
		statCard("总命中", fmt.Sprintf("%d", totalHits), "次") +
		statCard("总流量", formatBytes(totalBytes), "") +
		statCard("运行时长", fmtDuration(time.Since(processStart)), "")
	// 链接转换器：识别与生成统一在服务端 buildAccelURL（与 MCP convert_link 同一实现）
	conv := `<div id="gmconv" data-api="` + s.adminPath + `/api/convert" data-csrf="` + csrf + `" hidden></div>` +
		`<div class="conv-row"><input id="gmconv-in" type="text" autocomplete="off" placeholder="粘贴原始链接或镜像名">` +
		`<button type="button" class="btn" onclick="gmConvert()">获取加速链接</button></div>` +
		`<div class="conv-row" style="margin-top:10px"><input id="gmconv-out" type="text" readonly placeholder="加速链接将显示在这里">` +
		`<button type="button" id="gmconv-copy" class="btn outline" onclick="gmCopyConv()">复制</button>` +
		`<button type="button" class="btn outline" onclick="gmDownloadConv()">下载</button>` +
		`<button type="button" class="btn outline" onclick="gmOpenConv()">访问</button></div>` +
		`<p class="muted small" style="margin-top:10px">基于当前访问地址与<strong>上方所选 Token</strong>生成。支持 GitHub / HuggingFace 链接（如 <code>github.com/owner/repo/releases/download/v1/a.zip</code>）与 Docker 镜像名（如 <code>library/nginx:1.25</code>，给出<strong>需带 token</strong> 的 <code>/offline/&lt;token&gt;/&lt;镜像名&gt;</code> 离线 tar 地址）。registry 加速走 <code>/v2</code>，由 Docker 客户端直连，协议本身无法携带 token。</p>`
	// 链接转换卡片的 Token 快捷切换器（与「用法示例」的切换器联动，共用 .gmtok）
	convSel := s.tokenSelector(tokens, "链接转换使用的 Token")
	// 服务管理三卡：GitHub 恒开；Docker / HuggingFace 即时开关（持锁内存 + settings.json 持久化）
	svcCard := func(tag, name, desc, usage string, on bool, svc string) string {
		// 三张卡的底部标识统一宽高，视觉一致；GitHub 恒开用「默认开启」标识代替按钮
		state, foot := `<span class="badge on">● 已开启</span>`, `<span class="svccard-tag">默认开启</span>`
		if svc != "" {
			form := `<form method="post" action="` + s.adminPath + `/service" class="svc-toggle"` +
				` id="gmSvcForm-` + svc + `">` +
				`<input type="hidden" name="csrf" value="` + csrf + `">` +
				`<input type="hidden" name="name" value="` + svc + `">`
			switch {
			case svc == "docker" && on:
				// 已开启：提供「自定义路径」（弹窗内改前缀，提交时 on=1 幂等，仅更新路径）+「关闭」
				state = `<span class="badge on">● 已开启</span>`
				off := `<form method="post" action="` + s.adminPath + `/service" class="svc-toggle" id="gmSvcForm-docker-off">` +
					`<input type="hidden" name="csrf" value="` + csrf + `">` +
					`<input type="hidden" name="name" value="docker">` +
					`<input type="hidden" name="on" value="0">`
				foot = form + `<input type="hidden" name="on" value="1">` +
					`<button type="button" class="btn outline svccard-btn" onclick="gmOpenModal('gmDockerModal','gmSvcForm-docker','保存路径')">自定义路径</button></form>` +
					off + `<button type="submit" class="btn svccard-btn is-off">关闭</button></form>`
			case on:
				// 关闭按钮用另一套配色，和「开启」形成状态区分
				state = `<span class="badge on">● 已开启</span>`
				foot = form + `<input type="hidden" name="on" value="0"><button type="submit" class="btn svccard-btn is-off">关闭</button></form>`
			case svc == "docker":
				// 开启 Docker 前先弹安全确认（风险说明 + 可选自定义路径），确认后才真正开启
				state = `<span class="badge off">○ 未开启</span>`
				foot = form + `<input type="hidden" name="on" value="1">` +
					`<button type="button" class="btn svccard-btn" onclick="gmOpenModal('gmDockerModal','gmSvcForm-docker','我已知晓，确认开启')">开启</button></form>`
			default:
				state = `<span class="badge off">○ 未开启</span>`
				foot = form + `<input type="hidden" name="on" value="1"><button type="submit" class="btn svccard-btn">开启</button></form>`
			}
		}
		return `<div class="svccard"><div class="svccard-head">` + svcTagHTML(tag) + `<h3>` + name + `</h3>` + state + `</div>` +
			`<p>` + desc + `</p><p><code>` + usage + `</code></p>` +
			`<div class="svccard-foot">` + foot + `</div></div>`
	}
	// Docker 卡片的用法说明：路径固定带 <用户名>[/<自定义路径>]
	ownerSeg := s.dockerUserSegment()
	dockerUsage := "docker pull &lt;域名&gt;/" + html.EscapeString(ownerSeg) +
		"/library/nginx:1.25（用户名段必带）；/offline/&lt;token&gt;/&lt;image&gt;"
	if c := s.dockerPath(); c != "" {
		dockerUsage = "docker pull &lt;域名&gt;/" + html.EscapeString(ownerSeg) + "/" + html.EscapeString(c) +
			"/library/nginx:1.25；/offline/&lt;token&gt;/&lt;image&gt;"
	}
	services := `<div class="svcgrid">` +
		svcCard("GH", "GitHub 资源加速", "覆盖 blob / raw、Releases、仓库打包 archive 与短链，支持 Range 断点续传。始终可用。", "/g/&lt;token&gt;/https://github.com/owner/repo", true, "") +
		svcCard("DK", "Docker 镜像加速", "registry-mirror 回源 Docker Hub / GHCR，并可产出可 docker load 的离线 tar。开启前请先阅读安全说明。", dockerUsage, s.dockerEnabled(), "docker") +
		svcCard("HF", "HuggingFace 权重", "加速模型与 LFS 大文件下载，token 模式下需带 token。", "/hf/&lt;token&gt;/owner/repo/resolve/main/file", s.hfEnabled(), "hf") +
		`</div><p class="muted small" style="margin-top:12px">开关<strong>即时生效、无需重启</strong>，并持久化到 <code>gm-data/settings.json</code>（重启后保持）。后台设置优先于环境变量 <code>DOCKER_ENABLED</code> / <code>HF_ENABLED</code>。</p>` + dockerModalHTML(s.dockerPath(), s.dockerUserSegment())
	// 缓存管理：已用总量 / 单文件上限 / 总上限 / 时效，均热改；超限与过期自动清理；可一键清除
	maxFileMB := int(s.cache.MaxBytes() / (1024 * 1024))
	maxTotalMB := int(s.cache.MaxTotalBytes() / (1024 * 1024))
	ttlMin := int(s.cache.TTL() / time.Minute)
	totalTxt := "不限"
	if maxTotalMB > 0 {
		totalTxt = formatBytes(s.cache.MaxTotalBytes())
	}
	// 磁盘可用空间：低于 1 GiB 时缓存也会按最旧优先自动清理
	freeBytes := s.cache.DiskFree()
	diskTxt, diskSub := "—", "探测不可用"
	if freeBytes > 0 {
		diskTxt = formatBytes(freeBytes)
		diskSub = "充裕"
		if freeBytes < diskFreeMin {
			diskSub = "偏低，缓存会自动清理"
		}
	}
	cacheMgmt := `<div class="stats">` +
		statCard("缓存已用", formatBytes(s.cache.UsageBytes()), "总上限 "+totalTxt) +
		statCard("单文件上限", fmt.Sprintf("%d MB", maxFileMB), "超过则不缓存") +
		statCard("磁盘可用", diskTxt, diskSub) +
		statCardCls("缓存目录", html.EscapeString(s.cfg.CacheDir), "服务端本地路径", "sm") +
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
<p class="muted small" style="margin-top:12px">单文件上限：单个响应超过该大小则只透传、不缓存。总上限：超出后<strong>按最旧优先</strong>自动清理。<strong>磁盘可用空间低于 1 GB 时，即使没到总上限也会按最旧优先清理</strong>（缓存是临时的，避免写满磁盘拖垮整机）。时效：到期文件由后台周期任务自动删除。设置即时生效并持久化，优先于环境变量 <code>MAX_CACHE_MB</code> / <code>CACHE_TTL_MINUTES</code>。</p>`
	// Token 区块已独立成页，这里只放引导卡
	tokensGuide := `<p class="muted">Token 的创建、复制、删除与角色管理已独立成页，便于统一维护。</p>` +
		`<div class="row" style="margin-top:12px"><a class="btn" href="` + s.adminPath + `/tokens">前往 Token 管理` +
		`（` + strconv.Itoa(len(tokens)) + `）</a></div>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, badges, stats, convSel, converter, services, cacheMgmt, examples, tokensGuide
	// 只 Sprintf body 片段（无 CSS 的 %），再用 pageApp() 全幅包裹
	body := fmt.Sprintf(dashboardBody, aside, topbarRight, flashHTML, badges, stats, convSel, conv, services, cacheMgmt, example, tokensGuide)
	_, _ = w.Write([]byte(pageApp("管理后台 - gh-mirror", "gh-mirror 管理后台", body)))
}

// tokSlot 把示例里的 token 包成可被前端替换的片段（切换 Token 选择器时更新）。
// data-orig 记住服务端渲染时的值：前端切到空/失效 token 时回退到它，避免露出字面 "token" 占位。
func tokSlot(tk string) string {
	if tk == "" {
		return `<span class="tslot" data-orig="">token</span>`
	}
	esc := html.EscapeString(tk)
	return `<span class="tslot" data-orig="` + esc + `">` + esc + `</span>`
}

// shortTok 取 token 前 8 位用于展示，避免长串占满下拉框。
func shortTok(tk string) string {
	if len(tk) <= 8 {
		return tk
	}
	return tk[:8]
}

// tokenSelector 生成 Token 快捷下拉框：按列表序号 + 备注 + token 前 8 位展示，便于查找切换。
// 用 class="gmtok" 而非 id，使同一页面可放多个实例（链接转换 / 用法示例）并保持联动。
func (s *Server) tokenSelector(tokens []*Token, label string) string {
	var sb strings.Builder
	sb.WriteString(`<div class="tsel"><label>` + html.EscapeString(label) + `</label><select class="gmtok" onchange="gmApplyToken(this.value)" aria-label="` + html.EscapeString(label) + `">`)
	if len(tokens) == 0 {
		sb.WriteString(`<option value="">暂无 token</option>`)
	} else {
		for i, t := range tokens {
			note := t.Note
			if note == "" {
				note = "（无备注）"
			}
			sb.WriteString(fmt.Sprintf(`<option value="%s">%d. %s · %s…</option>`,
				html.EscapeString(t.Token), i+1, html.EscapeString(note), html.EscapeString(shortTok(t.Token))))
		}
	}
	sb.WriteString(`</select></div>`)
	return sb.String()
}

// tokenRows 渲染 Token 表格行（复制 / 编辑 / 删除）。
// 每个 td 带 data-label，窄屏下由 CSS 切换为卡片式布局；每行后跟一条隐藏的编辑行（行内表单）。
func (s *Server) tokenRows(tokens []*Token, csrf string) string {
	var rows strings.Builder
	for _, t := range tokens {
		lastUsed := "—"
		if t.LastUsed > 0 {
			lastUsed = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
		}
		// 角色文案用「普通 / 高级」：避免与后续多用户系统里的"管理员"角色混淆（admin 为内部值，保持兼容）
		roleTxt, roleCls := "普通", "badge"
		if normRole(t.Role) == RoleAdmin {
			roleTxt, roleCls = "高级", "badge role-admin"
		}
		esc := html.EscapeString(t.Token)
		// 列表里只显示缩写（完整值放 title 并用「复制」按钮拿走），避免 32 位 token 把表格撑宽
		shown := esc
		if len(t.Token) > 10 {
			shown = html.EscapeString(shortTok(t.Token)) + "…"
		}
		rows.WriteString(fmt.Sprintf(
			`<tr class="tok-row"><td data-label="Token"><span class="tok-cell"><code title="完整 Token：%s">%s</code>`+
				`<button type="button" class="btn outline" title="复制 token" onclick="gmCopyText(this,&quot;%s&quot;)">复制</button></span></td>`+
				`<td data-label="备注">%s</td>`+
				`<td data-label="类型"><span class="%s">%s</span></td>`+
				`<td data-label="创建时间">%s</td>`+
				`<td data-label="访问次数" class="num">%d</td>`+
				`<td data-label="流量" class="num">%s</td>`+
				`<td data-label="最近使用">%s</td>`+
				`<td data-label="操作"><span class="ops">`+
				`<button type="button" class="btn outline edit-toggle" onclick="gmToggleEdit(this)" aria-expanded="false">编辑</button>`+
				`<form method="post" action="%s/del" class="del-form">`+
				`<input type="hidden" name="csrf" value="%s">`+
				`<input type="hidden" name="token" value="%s">`+
				`<button type="submit" class="danger" onclick="return confirm('确定删除该 token？')">删除</button>`+
				`</form></span></td></tr>
<tr class="edit-row" hidden><td colspan="8">
<form method="post" action="%s/tokens/edit" class="edit-form">
<input type="hidden" name="csrf" value="%s">
<input type="hidden" name="token" value="%s">
<label class="fld">备注<input name="note" value="%s" placeholder="用于区分用途，如 CI / 个人笔记本"></label>
<label class="fld">类型<select name="role" aria-label="Token 类型"><option value="user"%s>普通 Token（仅代理）</option><option value="admin"%s>高级 Token（可用于 MCP）</option></select></label>
<button type="submit" class="btn">保存</button>
<button type="button" class="btn outline" onclick="gmToggleEdit(this)">取消</button>
<span class="muted small">Token 值不可修改，避免已发出的链接失效。</span>
</form>
</td></tr>`,
			esc, shown, esc, html.EscapeString(t.Note), roleCls, roleTxt,
			time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"),
			t.Hits, formatBytes(t.Bytes), lastUsed,
			s.adminPath, csrf, esc,
			s.adminPath, csrf, esc, html.EscapeString(t.Note),
			selAttr(normRole(t.Role) == RoleUser), selAttr(normRole(t.Role) == RoleAdmin)))
	}
	if len(tokens) == 0 {
		rows.WriteString(`<tr><td colspan="8" class="muted">暂无 token，请在上方创建</td></tr>`)
	}
	return rows.String()
}

// selAttr 返回选中属性（供编辑表单里的角色下拉回显）。
func selAttr(sel bool) string {
	if sel {
		return " selected"
	}
	return ""
}

// tokenAddForm Token 新增表单（角色选择供 MCP 使用）。
func (s *Server) tokenAddForm(csrf string) string {
	return `<form method="post" action="` + s.adminPath + `/add" class="row">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<input name="token" placeholder="留空则自动生成">` +
		`<input name="note" placeholder="备注（可选）">` +
		`<select name="role" title="Token 类型：高级 Token 可用于 MCP 接入" aria-label="Token 类型">` +
		`<option value="user">普通 Token</option><option value="admin">高级 Token（可用于 MCP）</option></select>` +
		`<button type="submit" class="btn">添加</button></form>` +
		`<p class="muted small" style="margin-top:8px">说明：<strong>普通 Token</strong>仅用于代理访问；<strong>高级 Token</strong> 另可用于 <code>MCP</code> 接入（<code>/mcp</code> 或 <code>gh-mirror mcp</code>），请勿随意外发。</p>`
}

// tokenFoot Token 页面页脚链接（各带适配图标，便于快速识别）。
func (s *Server) tokenFoot() string {
	return `<a href="` + s.adminPath + `/logout">` + logoutSVG + `退出登录</a>` +
		`<span class="foot-sep">·</span>` +
		`<a href="/">` + homeSVG + `返回首页</a>` +
		`<span class="foot-sep">·</span>` +
		`<a href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + `GitHub</a>`
}

// renderTokensPage 渲染 Token 管理独立页面（/admin/tokens）。
func (s *Server) renderTokensPage(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	tokens := s.store.All()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, rows, addForm, foot
	body := fmt.Sprintf(tokensBody, s.consoleAside(r), s.topbarRight(baseOf(r)), s.flashHTML(r),
		s.tokenRows(tokens, csrf), s.tokenAddForm(csrf), s.tokenFoot())
	_, _ = w.Write([]byte(pageApp("Token 管理 - gh-mirror", "gh-mirror Token 管理", body)))
}

// adminTokensFor 过滤出「归属某用户」且 role=admin（高级 Token）的 token。
// owner 为空视为归属默认管理员（兼容旧数据）——为多用户预留：每位用户只看到自己的高级 Token。
func adminTokensFor(tokens []*Token, user string) []*Token {
	var out []*Token
	for _, t := range tokens {
		if normRole(t.Role) == RoleAdmin && (t.Owner == user || t.Owner == "") {
			out = append(out, t)
		}
	}
	return out
}

// mcpTokenSelector 生成 MCP 页的 Token 选择器（只列「归属当前用户」的 admin token，默认选中第一个）。
// 用 .gmtok class 复用全局联动：切换后同步本页所有 .tslot（HTTP/stdio 示例里的 token）。
func (s *Server) mcpTokenSelector(tokens []*Token, user string) string {
	adm := adminTokensFor(tokens, user)
	var sb strings.Builder
	sb.WriteString(`<div class="tsel"><label>用于 MCP 接入的高级 Token</label><select class="gmtok" onchange="gmApplyToken(this.value)" aria-label="选择用于 MCP 接入的 Token">`)
	if len(adm) == 0 {
		sb.WriteString(`<option value="">暂无</option>`)
	} else {
		for i, t := range adm {
			note := t.Note
			if note == "" {
				note = "（无备注）"
			}
			sb.WriteString(fmt.Sprintf(`<option value="%s">%d. %s · %s…</option>`,
				html.EscapeString(t.Token), i+1, html.EscapeString(note), html.EscapeString(shortTok(t.Token))))
		}
	}
	sb.WriteString(`</select></div>`)
	return sb.String()
}

// mcpToolsTable 渲染 12 个工具的分类清单（与 mcp.go 的 mcpRegistry 一一对应，勿漏）。
func mcpToolsTable() string {
	type row struct{ cat, name, desc string }
	rows := []row{
		{"健康与信息", "health", "返回服务状态与运行时长"},
		{"Token 管理", "list_tokens", "列出全部 Token（类型 / 命中 / 流量 / 时间）"},
		{"Token 管理", "create_token", "创建新 Token（类型 user / admin）"},
		{"Token 管理", "delete_token", "删除指定 Token"},
		{"服务开关", "get_services", "查询 GitHub / Docker / HuggingFace 启用状态"},
		{"服务开关", "set_service", "开启或关闭 Docker / HuggingFace 加速"},
		{"缓存", "get_cache_stats", "查询缓存：已用 / 上限 / 时效 / 目录"},
		{"缓存", "set_cache_limits", "调整缓存参数（即时生效并持久化）"},
		{"缓存", "clear_cache", "清空缓存目录内所有文件"},
		{"链接与镜像", "convert_link", "把原始链接 / 镜像名转为加速链接"},
		{"链接与镜像", "search_images", "在 Docker Hub 搜索镜像（等价 /api/search）"},
		{"链接与镜像", "list_tags", "列出 Docker Hub 某仓库的标签（等价 /api/tags）"},
	}
	var sb strings.Builder
	for _, r := range rows {
		sb.WriteString(`<tr><td class="muted">` + r.cat + `</td><td><code>` + r.name + `</code></td><td>` + r.desc + `</td></tr>`)
	}
	return sb.String()
}

// renderMCPPage 渲染 MCP 接入介绍页（/admin/mcp）。
// 核心：根据当前是否有 admin 角色 token 动态给出「就绪状态」与可复制的接入配置。
func (s *Server) renderMCPPage(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	_ = sid
	tokens := s.store.All()
	user := s.adminUser
	adm := adminTokensFor(tokens, user)
	base := baseOf(r)
	tk := ""
	if len(adm) > 0 {
		tk = adm[0].Token
	}

	// 就绪状态卡：有归属当前用户的 admin token 则就绪并给出选择器；否则引导创建
	status := ""
	if len(adm) > 0 {
		status = `<div class="flash ok"><strong style="flex:0 0 auto">✓</strong><span>已就绪：当前用户 <code>` + html.EscapeString(user) + `</code> 名下共有 ` +
			strconv.Itoa(len(adm)) + ` 个「高级 Token」可用于 MCP 接入。切换下方选择器，下方命令里的 Token 会同步更新，复制即用。</span></div>` +
			s.mcpTokenSelector(tokens, user)
	} else {
		status = `<div class="flash err"><strong style="flex:0 0 auto">!</strong><span>未就绪：当前用户 <code>` + html.EscapeString(user) + `</code> 名下还没有「高级 Token」。MCP 鉴权要求 admin 角色（高级 Token），请先到「Token 管理」创建一个。</span></div>` +
			`<div class="row" style="margin-top:12px"><a class="btn" href="` + s.adminPath + `/tokens">去创建高级 Token</a></div>`
	}

	// 两种接入方式的示例命令（token 段为 .tslot，随选择器联动）
	httpEx, stdioEx := "", ""
	if tk != "" {
		httpEx = `<p class="muted">endpoint：<code>` + html.EscapeString(base) + `/mcp</code>（仅支持 <code>POST</code>，鉴权用 <code>Authorization: Bearer &lt;token&gt;</code>）</p>` +
			`<div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -sX POST ` + html.EscapeString(base) + `/mcp \
  -H "Authorization: Bearer ` + tokSlot(tk) + `" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'</code></pre></div>`
		stdioEx = `<p class="muted">在本地或跳板机运行 <code>gh-mirror</code> 二进制：日志写 stderr，stdout 只输出协议 JSON。</p>` +
			`<div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>gh-mirror mcp --token ` + tokSlot(tk) + `
# 或使用环境变量（不落命令行历史）
GM_MCP_TOKEN=` + tokSlot(tk) + ` gh-mirror mcp</code></pre></div>`
	} else {
		httpEx = `<p class="muted">创建「高级 Token」后，这里会自动生成可复制的接入命令。</p>`
		stdioEx = `<p class="muted">创建「高级 Token」后，这里会自动生成可复制的接入命令。</p>`
	}

	tools := `<table class="mcp-tools"><thead><tr><th>类别</th><th>工具</th><th>说明</th></tr></thead><tbody>` +
		mcpToolsTable() + `</tbody></table>` +
		`<p class="muted small" style="margin-top:12px">完整协议说明见 <a href="` + repoURL + `/blob/main/docs/mcp.md" target="_blank" rel="noopener">docs/mcp.md</a>。首页还提供「一键交给 AI Agent 部署」的完整提示词，复制给 CodeBuddy / Cursor / Claude 即可自动完成部署与验证。</p>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, status, httpEx, stdioEx, tools
	body := fmt.Sprintf(mcpBody, s.consoleAside(r), s.topbarRight(base), s.flashHTML(r), status, httpEx, stdioEx, tools)
	_, _ = w.Write([]byte(pageApp("MCP 接入 - gh-mirror", "gh-mirror MCP 接入指南", body)))
}

// consoleAside 渲染后台侧栏（各页面共用；小屏为抽屉，唤出入口见顶栏 ☰ 按钮）。
func (s *Server) consoleAside(r *http.Request) string {
	n := len(s.store.All())
	tokensNav := "Token 管理"
	if n > 0 {
		tokensNav = fmt.Sprintf("Token 管理（%d）", n)
	}
	// 高亮互斥：/admin/tokens 或 /admin/mcp 各自高亮，其余情况高亮概览
	overviewActive, tokensActive, mcpActive := " active", "", ""
	switch {
	case strings.HasSuffix(r.URL.Path, "/tokens"):
		overviewActive, tokensActive = "", " active"
	case strings.HasSuffix(r.URL.Path, "/mcp"):
		overviewActive, mcpActive = "", " active"
	}
	return `<aside id="gmNav">` +
		`<a class="brand" href="/" title="返回首页">` + logoSVG + `<span>gh-mirror</span></a>` +
		`<nav class="nav">` +
		`<a class="navitem` + overviewActive + `" href="` + s.adminPath + `"><span class="ic">▦</span>概览</a>` +
		`<a class="navitem` + tokensActive + `" href="` + s.adminPath + `/tokens"><span class="ic">🔑</span>` + tokensNav + `</a>` +
		`<a class="navitem` + mcpActive + `" href="` + s.adminPath + `/mcp"><span class="ic">🤖</span>MCP 接入</a>` +
		`<a class="navitem" href="` + s.adminPath + `#services"><span class="ic">⚙️</span>服务管理</a>` +
		`<a class="navitem" href="` + s.adminPath + `#cache"><span class="ic">💾</span>缓存管理</a>` +
		`<span class="navsep"></span>` +
		`<a class="navitem" href="` + repoURL + `/blob/main/README.md" target="_blank" rel="noopener"><span class="ic">📄</span>项目文档</a>` +
		`<a class="navitem" href="` + repoURL + `" target="_blank" rel="noopener"><span class="ic">` + githubSVG + `</span>GitHub</a>` +
		`<a class="navitem" href="` + s.adminPath + `/logout"><span class="ic">` + logoutSVG + `</span>退出登录</a>` +
		`</nav>` + s.asideFoot() + `</aside>` +
		`<button class="nav-mask" type="button" aria-label="关闭导航菜单" onclick="toggleNav()"></button>`
}

// asideFoot 渲染侧栏底部信息区：⭐ Star 引导 + 当前版本号 + Releases 链接。
// 版本号由构建期注入（见 main.go 的 version），此处只做静态展示，不联网拉取最新版本。
func (s *Server) asideFoot() string {
	return `<div class="aside-foot">` +
		`<a class="star-cta" href="` + repoURL + `" target="_blank" rel="noopener" title="在 GitHub 上 Star 本项目">⭐ Star 支持一下</a>` +
		`<div class="ver">` + tagSVG + `当前版本 <code>` + html.EscapeString(version) + `</code></div>` +
		`<a class="ver-link" href="` + repoURL + `/releases" target="_blank" rel="noopener">` + pkgSVG + `查看 Releases</a>` +
		`</div>`
}

// doEditToken 修改 Token 的备注与角色（登录 + CSRF；token 值本身不可改）。
func (s *Server) doEditToken(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/tokens?err=csrf", http.StatusFound)
		return
	}
	tok := strings.TrimSpace(r.FormValue("token"))
	note := strings.TrimSpace(r.FormValue("note"))
	role := normRole(r.FormValue("role"))
	if !s.store.Update(tok, note, role) {
		http.Redirect(w, r, s.adminPath+"/tokens?err=token_missing", http.StatusFound)
		return
	}
	log.Printf("Token 已更新: 类型=%s", role)
	http.Redirect(w, r, s.adminPath+"/tokens?ok=token_updated", http.StatusFound)
}

// flashOK / flashErr 把 POST 后重定向携带的 ok / err 码翻译成可读文案。
var flashOK = map[string]string{
	"token_added":   "Token 已创建，可立即用于代理访问",
	"token_deleted": "Token 已删除",
	"token_updated": "Token 已保存",
	"token_exists":  "该 Token 已存在，未做改动",
	"service_on":    "服务已开启，即时生效（已持久化）",
	"service_off":   "服务已关闭",
	"cache_saved":   "缓存设置已保存，即时生效",
	"cache_cleared": "缓存已清除",
}

var flashErr = map[string]string{
	"csrf":          "安全校验失败（会话可能已过期），请刷新页面后重试",
	"token_missing": "Token 不存在或已被删除",
	"bad_input":     "输入不合法，请检查后重试",
	"svc_unknown":   "未知服务，仅支持 Docker / HuggingFace",
	"svc_failed":    "服务开关调整失败，请查看日志",
	"path_invalid":  "自定义路径不合法（仅支持字母、数字、-、_、/，最长 64 字符）",
}

// flashHTML 渲染一次性操作反馈条（保存 / 创建 / 开关等操作后的即时提示）。
// 文案由重定向带上来的 ?ok= / ?err= 决定；前端会自动淡出并清理该查询参数。
func (s *Server) flashHTML(r *http.Request) string {
	q := r.URL.Query()
	msg, kind := "", ""
	if code := q.Get("ok"); code != "" {
		if m, ok := flashOK[code]; ok {
			msg, kind = m, "ok"
		}
	}
	if msg == "" {
		if code := q.Get("err"); code != "" {
			if m, ok := flashErr[code]; ok {
				msg, kind = m, "err"
			}
		}
	}
	if msg == "" {
		return ""
	}
	icon := "✓"
	if kind == "err" {
		icon = "!"
	}
	return `<div class="flash ` + kind + `" id="gmFlash" role="status" aria-live="polite">` +
		`<strong style="flex:0 0 auto">` + icon + `</strong><span>` + msg + `</span>` +
		`<button type="button" class="flash-x" onclick="gmDismissFlash()" aria-label="关闭提示">✕</button></div>`
}

// topbarRight 渲染后台顶栏右侧按钮（各页面共用）。
func (s *Server) topbarRight(base string) string {
	return `<a class="btn outline" href="` + base + `/healthz" target="_blank" rel="noopener">健康检查</a>` +
		`<button id="themeToggle" class="btn outline" type="button" onclick="toggleTheme()" aria-label="切换深色 / 浅色模式">🌙 深色</button>`
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
	return statCardCls(k, v, sub, "")
}

// statCardCls 同 statCard，但可给数值附加样式类（如 "sm" 用于缓存目录这类长文本）。
func statCardCls(k, v, sub, cls string) string {
	vcls := "v"
	if cls != "" {
		vcls = "v " + cls
	}
	out := `<div class="stat"><p class="k">` + k + `</p><p class="` + vcls + `">` + v + `</p>`
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
		http.Redirect(w, r, s.adminPath+"?err=csrf#services", http.StatusFound)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	if name != "docker" && name != "hf" {
		http.Redirect(w, r, s.adminPath+"?err=svc_unknown#services", http.StatusFound)
		return
	}
	on := r.FormValue("on") == "1" || strings.EqualFold(r.FormValue("on"), "true") || strings.EqualFold(r.FormValue("on"), "on")
	// 表单里存在 dockerPath 才更新自定义路径（只有 Docker 的开关表单会带该字段）
	if name == "docker" {
		if vals, ok := r.PostForm["dockerPath"]; ok {
			p := ""
			if len(vals) > 0 {
				p = vals[0]
			}
			if err := s.setDockerPath(p); err != nil {
				http.Redirect(w, r, s.adminPath+"?err=path_invalid#services", http.StatusFound)
				return
			}
			log.Printf("Docker 自定义路径已更新: %q", s.dockerPath())
		}
	}
	if err := s.setService(name, on); err != nil {
		http.Redirect(w, r, s.adminPath+"?err=svc_failed#services", http.StatusFound)
		return
	}
	log.Printf("服务开关已调整: %s -> %v", name, on)
	code := "service_off"
	if on {
		code = "service_on"
	}
	http.Redirect(w, r, s.adminPath+"?ok="+code+"#services", http.StatusFound)
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
		http.Redirect(w, r, s.adminPath+"?err=csrf#cache", http.StatusFound)
		return
	}
	maxFileMB := atoiOr(r.FormValue("maxFileMB"), -1)
	maxTotalMB := atoiOr(r.FormValue("maxTotalMB"), -1)
	ttlMinutes := atoiOr(r.FormValue("ttlMinutes"), -1)
	if maxFileMB < -1 || maxTotalMB < -1 || ttlMinutes < -1 {
		http.Redirect(w, r, s.adminPath+"?err=bad_input#cache", http.StatusFound)
		return
	}
	_ = s.setCacheLimits(maxFileMB, maxTotalMB, ttlMinutes)
	log.Printf("缓存参数已调整: 单文件=%dMB 总计=%dMB 时效=%d分钟",
		int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute))
	http.Redirect(w, r, s.adminPath+"?ok=cache_saved#cache", http.StatusFound)
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
		http.Redirect(w, r, s.adminPath+"?err=csrf#cache", http.StatusFound)
		return
	}
	files, bytes := s.cache.Clear()
	log.Printf("缓存已清除: %d 个文件，%s", files, formatBytes(bytes))
	http.Redirect(w, r, s.adminPath+"?ok=cache_cleared#cache", http.StatusFound)
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
	toks := s.store.All()
	// token 可由前端选择器指定；未指定或不存在时回落到第一个
	tk := ""
	if req := strings.TrimSpace(r.FormValue("token")); req != "" && s.store.Has(req) {
		tk = req
	} else if len(toks) > 0 {
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
		if tk == "" {
			return accelResult{Kind: "docker", Hint: "离线 tar 需要 token，请先在「Token 管理」添加一个 token"}
		}
		// 离线包地址必须带 token（/offline/<token>/<image>）
		return accelResult{Kind: "docker", URL: base + "/offline/" + tk + "/" + raw}
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

// fmtDuration 把时长格式化为紧凑形式（用于后台"运行时长"）：0天01:09
func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return "刚刚启动"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%d天%02d:%02d", days, hours, mins)
}
