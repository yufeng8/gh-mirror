package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const adminSessionCookie = "ghm_admin_session"
const userSessionCookie = "ghm_user_session"
const sessionMaxAge = 7 * 24 * 3600

// isUserEntry 判断请求是否属于普通用户入口。
func (s *Server) isUserEntry(r *http.Request) bool {
	return s.userPath != "" && strings.HasPrefix(r.URL.Path, s.userPath)
}

// entryBase 返回当前请求所属入口的 base 路径（超管入口返回 adminPath，普通入口返回 userPath）。
func (s *Server) entryBase(r *http.Request) string {
	if s.isUserEntry(r) {
		return s.userPath
	}
	return s.adminPath
}

// sessionCookieFor 根据请求路径选择对应入口的 session cookie（双入口强隔离：cookie 名不同、签名隔离，互换无效）。
func (s *Server) sessionCookieFor(r *http.Request) string {
	if s.isUserEntry(r) {
		return userSessionCookie
	}
	return adminSessionCookie
}

// parseSession 校验会话 Cookie 的签名、过期时间与用户是否仍存在，
// 返回 (sid, 用户名, 角色, ok)。会话格式：sid.<base64url(user)>.<exp>.<sig>（4 段）。
// 签名混入 cookie 名（domain separation），超管/普通入口 cookie 不可互换。
func (s *Server) parseSession(r *http.Request) (sid, user, role string, ok bool) {
	cookieName := s.sessionCookieFor(r)
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", "", "", false
	}
	parts := strings.SplitN(c.Value, ".", 5)
	if len(parts) != 5 {
		return "", "", "", false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(cookieName + ":" + parts[0] + "." + parts[1] + "." + parts[2] + "." + parts[3]))
	sig := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(parts[4])) != 1 {
		return "", "", "", false
	}
	exp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return "", "", "", false
	}
	if time.Now().Unix() >= exp {
		return "", "", "", false
	}
	ub, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", "", false
	}
	username := string(ub)
	u, exists := s.users.Get(username)
	if !exists {
		return "", "", "", false
	}
	// 密码版本校验：改密后旧会话全部失效（找回密码 / 重置密码后强制重新登录）
	if len(u.PassHash) < 8 || subtle.ConstantTimeCompare([]byte(u.PassHash[:8]), []byte(parts[2])) != 1 {
		return "", "", "", false
	}
	return parts[0], username, u.Role, true
}

// isAuthed 校验请求是否携带有效会话 Cookie。
func (s *Server) isAuthed(r *http.Request) bool {
	_, _, _, ok := s.parseSession(r)
	return ok
}

// currentUser 从会话解析当前用户名与角色。
func (s *Server) currentUser(r *http.Request) (username, role string, ok bool) {
	_, username, role, ok = s.parseSession(r)
	return
}

// requireSuper 判断当前会话是否为超级管理员。
func (s *Server) requireSuper(r *http.Request) bool {
	_, _, role, ok := s.parseSession(r)
	return ok && role == RoleSuper
}

// requireRootSuper 判断当前会话是否为「安装时创建的超级管理员」（种子账号 adminUser）。
// 只有它能管理用户与角色：后续被提升为 super 的「管理员」不具备该权限，
// 避免出现「管理员把超级管理员降级 / 互相提权」的越权。
func (s *Server) requireRootSuper(r *http.Request) bool {
	username, role, ok := s.currentUser(r)
	return ok && role == RoleSuper && username == s.adminUser
}

// signSession 生成带随机 sid、用户名、密码版本、过期时间与 HMAC 签名的会话值。
// cookieName 参与签名（domain separation），超管/普通入口的会话值不可互换。
// 密码版本（PassHash 前 8 位）使改密后旧会话全部失效。
func (s *Server) signSession(user, cookieName string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	sid := hex.EncodeToString(buf)
	exp := time.Now().Add(sessionMaxAge * time.Second).Unix()
	passVer := ""
	if u, ok := s.users.Get(user); ok && len(u.PassHash) >= 8 {
		passVer = u.PassHash[:8]
	}
	payload := sid + "." + base64.RawURLEncoding.EncodeToString([]byte(user)) + "." + passVer + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(cookieName + ":" + payload))
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
			s.doAdminLogin(w, r)
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
	case sub == "users":
		if s.requireRootSuper(r) {
			s.renderUsersPage(w, r)
			return
		}
		http.Redirect(w, r, s.adminPath, http.StatusFound)
	case sub == "users/add":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doAddUser(w, r)
	case sub == "users/del":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doDelUser(w, r)
	case sub == "users/role":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doSetUserRole(w, r)
	case sub == "users/pass":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doResetUserPass(w, r)
	case sub == "users/tokens":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.renderUserTokensPage(w, r)
	case sub == "users/tokens.json":
		s.renderUserTokensJSON(w, r)
	case sub == "services":
		if s.requireSuper(r) {
			s.renderServicesPage(w, r)
			return
		}
		http.Redirect(w, r, s.adminPath, http.StatusFound)
	case sub == "service":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doToggleService(w, r)
	case sub == "cache":
		// GET 渲染缓存管理页；POST 保存设置（同一路径按方法分发）；仅超管
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		if r.Method == http.MethodPost {
			s.doSetCache(w, r)
			return
		}
		s.renderCachePage(w, r)
	case sub == "cache/clear":
		if !s.requireSuper(r) {
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
	case sub == "docker-path":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doSetUserDockerPath(w, r)
	case sub == "offline/del":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doOfflineDel(w, r)
	case sub == "offline/batch-del":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doOfflineBatchDel(w, r)
	case sub == "offline/repull":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doOfflineRepull(w, r)
	case sub == "offline/repull-latest":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doOfflineRepullLatest(w, r)
	case sub == "offline/autopurge":
		if !s.requireSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doOfflineAutoPurge(w, r)
	case sub == "portal-settings":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doSetPortalSettings(w, r)
	case sub == "invite/generate":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doInviteGenerate(w, r)
	case sub == "invite/add":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doInviteAddCustom(w, r)
	case sub == "invite/batch":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doInviteBatch(w, r)
	case sub == "invite/export":
		if !s.requireRootSuper(r) {
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.doInviteExport(w, r)
	default:
		s.render404(w, r)
	}
}

// handleUser 处理普通用户入口路由（/user/*）：概览 / Token / MCP / 链接转换，仅普通视图（无超管功能）。
func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, s.userPath), "/")
	switch {
	case sub == "" || sub == "login":
		if r.Method == http.MethodPost {
			s.doUserLogin(w, r)
			return
		}
		if s.isAuthed(r) {
			s.renderDashboard(w, r)
			return
		}
		s.renderLogin(w, r, "")
	case sub == "register":
		if r.Method == http.MethodPost {
			s.doRegister(w, r)
			return
		}
		if s.isAuthed(r) {
			http.Redirect(w, r, s.userPath, http.StatusFound)
			return
		}
		s.renderRegisterPage(w, r)
	case sub == "send-code":
		s.doSendCode(w, r)
	case sub == "reset-password":
		s.doResetPassword(w, r)
	case sub == "logout":
		s.doLogout(w, r)
	case sub == "add":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.userPath, http.StatusFound)
			return
		}
		s.doAddToken(w, r)
	case sub == "del":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.userPath, http.StatusFound)
			return
		}
		s.doDelToken(w, r)
	case sub == "tokens":
		if s.isAuthed(r) {
			s.renderTokensPage(w, r)
			return
		}
		http.Redirect(w, r, s.userPath, http.StatusFound)
	case sub == "tokens/edit":
		if s.isAuthed(r) {
			s.doEditToken(w, r)
			return
		}
		http.Redirect(w, r, s.userPath, http.StatusFound)
	case sub == "mcp":
		if s.isAuthed(r) {
			s.renderMCPPage(w, r)
			return
		}
		http.Redirect(w, r, s.userPath, http.StatusFound)
	case sub == "api/convert":
		if !s.isAuthed(r) {
			s.writeJSON(w, http.StatusUnauthorized, convertResp{OK: false, Hint: "未登录或会话已过期"})
			return
		}
		s.doConvertAPI(w, r)
	case sub == "docker-path":
		if !s.isAuthed(r) {
			http.Redirect(w, r, s.userPath, http.StatusFound)
			return
		}
		s.doSetUserDockerPath(w, r)
	default:
		s.render404(w, r)
	}
}

// doAdminLogin 超管入口登录：仅接受超级管理员角色（普通用户登录被拒）。
func (s *Server) doAdminLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if s.adminCaptchaEnabled() && !s.captcha.Verify(r.FormValue("captchaId"), r.FormValue("captcha")) {
		s.renderLogin(w, r, "图形验证码错误或已过期")
		return
	}
	user := r.FormValue("user")
	pass := r.FormValue("pass")
	if s.users.Verify(user, pass) {
		if u, exists := s.users.Get(user); exists && u.Role == RoleSuper {
			if err := s.users.SetLastLogin(user); err != nil {
				log.Printf("更新最后登录时间失败(%s): %v", user, err)
			}
			s.setSessionCookie(w, adminSessionCookie, user)
			http.Redirect(w, r, s.adminPath, http.StatusFound)
			return
		}
		s.renderLogin(w, r, "此入口仅限超级管理员登录")
		return
	}
	s.renderLogin(w, r, "账号或密码错误")
}

// doUserLogin 普通入口登录：接受所有用户（super 也可登录体验普通视图）。
func (s *Server) doUserLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if s.userCaptchaEnabled() && !s.captcha.Verify(r.FormValue("captchaId"), r.FormValue("captcha")) {
		s.renderLogin(w, r, "图形验证码错误或已过期")
		return
	}
	user := r.FormValue("user")
	pass := r.FormValue("pass")
	if s.users.Verify(user, pass) {
		if err := s.users.SetLastLogin(user); err != nil {
			log.Printf("更新最后登录时间失败(%s): %v", user, err)
		}
		s.setSessionCookie(w, userSessionCookie, user)
		http.Redirect(w, r, s.userPath, http.StatusFound)
		return
	}
	s.renderLogin(w, r, "账号或密码错误")
}

// setSessionCookie 写入指定入口的会话 cookie（cookie 名参与签名，实现双入口会话隔离）。
func (s *Server) setSessionCookie(w http.ResponseWriter, cookieName, user string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.signSession(user, cookieName),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cookieSecure,
		MaxAge:   sessionMaxAge,
	})
}

func (s *Server) doLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   s.sessionCookieFor(r),
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, s.entryBase(r), http.StatusFound)
}

func (s *Server) doAddToken(w http.ResponseWriter, r *http.Request) {
	sid, user, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.entryBase(r)+"/tokens?err=csrf", http.StatusFound)
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
		// 新 token 归属当前登录用户（多用户：每位用户各自管理自己的 token）
		s.store.Add(tok, note, role, user)
	}
	// 跳回 Token 管理页（该页才是操作上下文），并带上一次性反馈码
	http.Redirect(w, r, s.entryBase(r)+"/tokens?ok="+code, http.StatusFound)
}

func (s *Server) doDelToken(w http.ResponseWriter, r *http.Request) {
	sid, user, role, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.entryBase(r)+"/tokens?err=csrf", http.StatusFound)
		return
	}
	// 兼容单个删除（行内表单）与批量删除（表格复选框批量表单）：读取全部 token 值
	toks := r.PostForm["token"]
	if len(toks) == 0 {
		if v := r.FormValue("token"); v != "" {
			toks = []string{v}
		}
	}
	deleted := 0
	for _, tok := range toks {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// 归属校验：只能删自己的；无权时按不存在处理，不泄露他人 token 是否存在
		if t, exists := s.store.Get(tok); exists && s.tokenVisible(t, user, role) {
			s.store.Delete(tok)
			deleted++
		}
	}
	code := "token_missing"
	switch {
	case deleted == 1:
		code = "token_deleted"
	case deleted > 1:
		code = "tokens_deleted"
	}
	http.Redirect(w, r, s.entryBase(r)+"/tokens?ok="+code+"&n="+strconv.Itoa(deleted), http.StatusFound)
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 注册关闭后从注册页跳回：给出明确提示（而非静默回到登录页）
	if errMsg == "" && r.URL.Query().Get("err") == "reg_closed" {
		errMsg = "注册未开放"
	}
	// 登录页身份按入口区分：/user 面向普通用户（用户后台），/admin 面向超管（管理后台）
	isUser := s.isUserEntry(r)
	sub, desc, forgot := "登录 gh-mirror 管理后台", "gh-mirror 管理后台登录", adminForgotHTML()
	if isUser {
		sub, desc, forgot = "登录 gh-mirror 用户后台", "gh-mirror 用户后台登录", userForgotHTML(s.userPath, s.contactEmail())
	}
	// 注册入口：仅普通入口且注册开关开启时显示
	regEntry := ""
	if isUser && s.regEnabled() {
		txt := "注册新账号"
		if s.inviteOnlyEnabled() {
			txt = "注册新账号（需邀请码）"
		}
		regEntry = `<p class="muted note">还没有账号？<a href="` + s.userPath + `/register">` + txt + `</a></p>`
	}
	// 图形验证码：按入口开关显隐（用户入口 userCaptcha / 管理入口 adminCaptcha）
	captchaBlock := ""
	if (isUser && s.userCaptchaEnabled()) || (!isUser && s.adminCaptchaEnabled()) {
		captchaBlock = s.captchaHTML()
	}
	// 只 Sprintf body 片段（loginBody 无 CSS 的 %），再用 pageApp() 全幅包裹（分屏登录）
	// 动词顺序：sub（身份） / action（入口地址） / err / captcha / forgot（忘记密码块） / regEntry（注册入口）
	body := fmt.Sprintf(loginBody, sub, s.entryBase(r), html.EscapeString(errMsg), captchaBlock, forgot, regEntry)
	_, _ = w.Write([]byte(pageApp("登录 - gh-mirror", desc, body)))
}

// adminForgotHTML 超管入口的忘记密码说明：超管密码只能服务器后台修改（不提供邮箱重置）。
func adminForgotHTML() string {
	return `<details class="forgot">
        <summary>忘记密码？</summary>
        <div class="forgot-body">
          <p class="forgot-title">在服务器上重置密码</p>
          <p class="muted small">超级管理员密码是部署时设置的 <code>ADMIN_PASS</code>（环境变量 / <code>.env</code>），只能在服务器后台修改，无法在线找回：</p>
          <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code># docker run 部署：以新密码重建容器（token / 缓存在 gm-data、gm-cache 卷，不丢）
docker rm -f gh-mirror
docker run -d --name gh-mirror -e ADMIN_PASS='新密码' -e ADMIN_SECRET='你的密钥' -p 127.0.0.1:8080:8080 -v gm-data:/var/lib/gh-mirror ghcr.io/yufeng8/gh-mirror:latest

# docker compose 部署：改 .env 后重建
sed -i 's#^ADMIN_PASS=.*#ADMIN_PASS=新密码#' .env &amp;&amp; docker compose up -d</code></pre></div>
          <p class="muted small">镜像 tag 请用 <strong>:latest</strong>（或你当前在用的版本）。重置仅影响登录，不影响已创建的 Token 与缓存。</p>
        </div>
      </details>`
}

// userForgotHTML 普通入口的忘记密码：邮箱验证码自主重置（仅普通用户）+ 联系超管（展示联系邮箱）。
func userForgotHTML(userPath, contactEmail string) string {
	return `<details class="forgot">
        <summary>忘记密码？</summary>
        <div class="forgot-body">
          <p class="forgot-title">通过邮箱找回（仅普通用户）</p>
          <form method="post" action="` + userPath + `/reset-password" autocomplete="off">
            <label for="resetEmail">注册邮箱</label>
            <input id="resetEmail" name="email" type="email" required>
            <div class="pw" style="margin-top:8px">
              <input name="code" placeholder="6 位验证码" required>
              <button type="button" class="btn outline" onclick="gmSendCode('` + userPath + `/send-code','reset','resetEmail')">发送验证码</button>
            </div>
            <label for="resetPass" style="margin-top:8px">新密码</label>
            <input id="resetPass" name="pass" type="password" placeholder="至少 4 位" required>
            <button type="submit" class="btn btn-login" style="margin-top:12px">重置密码</button>
          </form>
          <p class="muted small" style="margin-top:12px">或联系超级管理员重置：<strong>` + html.EscapeString(contactEmail) + `</strong></p>
        </div>
      </details>`
}

// renderRegisterPage 渲染用户自主注册页（/user/register，仅注册模式非关闭时可达）。
func (s *Server) renderRegisterPage(w http.ResponseWriter, r *http.Request) {
	// 注册关闭时直接回到登录页，避免「能打开页面但提交被拒」的矛盾
	if !s.regEnabled() {
		http.Redirect(w, r, s.userPath+"?err=reg_closed", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 图形验证码默认隐藏：点「发送验证码」时才展开并要求输入（发码接口会校验）；
	// 这样注册表单首屏更简洁，也避免用户误以为注册必须填两个验证码。
	captchaBlock := ""
	if s.userCaptchaEnabled() {
		captchaBlock = `<div id="gmRegCaptcha" hidden>` + s.captchaHTML() + `</div>`
	}
	inviteBlock := ""
	if s.inviteOnlyEnabled() {
		inviteBlock = `<label for="invite">邀请码</label>` +
			`<input id="invite" name="invite" placeholder="请输入邀请码（注册必填）" required autocomplete="off">`
	}
	body := fmt.Sprintf(registerBody, s.flashHTML(r), s.userPath, inviteBlock, s.userPath, captchaBlock, s.userPath)
	_, _ = w.Write([]byte(pageApp("注册 - gh-mirror", "注册 gh-mirror 用户后台", body)))
}

// contactEmail 返回超管配置的联系邮箱（未配置返回空）。
func (s *Server) contactEmail() string {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.contactEmailStr
}

func (s *Server) renderDashboard(w http.ResponseWriter, r *http.Request) {
	sid, user, role, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	tokens := s.tokensForUser(s.store.All(), user, role)
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
		ghEx = base + "/g/" + dockerSegment(user) + "/" + tokSlot(tk) + "/https://github.com/yufeng8/gh-mirror/blob/main/README.md"
		hfEx = base + "/hf/" + dockerSegment(user) + "/" + tokSlot(tk) + "/openai-community/gpt2/resolve/main/config.json"
	}
	if !s.hfEnabled() {
		hfEx = "HuggingFace 加速未开启（须超级管理员在「服务管理」开启）"
	}
	dockerEx := "Docker 加速未开启（须超级管理员在「服务管理」开启）"
	if s.dockerEnabled() {
		// 离线包必须带 token：没有 token 时给引导文案，不能拼出字面 "token" 占位（否则像是已经固定好了）
		offlineEx := "请先到「Token 管理」创建一个 token（离线包必须带 token）"
		if tk != "" {
			offlineEx = base + "/offline/" + dockerSegment(user) + "/" + tokSlot(tk) + "/library/nginx:1.25"
		}
		// 拉取路径固定为 <用户名>[/<自定义路径>]，因此 registry-mirrors 不再适用，统一用带路径的 pull
		dockerEx = "离线 tar（需 token）：" + offlineEx +
			" ；registry 加速（无需 token，按带路径方式 pull）：<code>docker pull " + base + "/" +
			html.EscapeString(s.dockerV2PrefixFor(user)) + "/library/nginx:1.25</code>"
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
	// Docker 自定义路径设置（受服务状态门控；双入口都显示当前用户自己的路径）
	dockerPathCard := s.dockerPathCard(csrf, s.entryBase(r), user)
	// 概览统计卡（网关真实指标，不伪造主机监控）
	// 概览只放与 token 流量相关的指标；缓存细节在下方「缓存管理」里展示，避免重复
	stats := statCardTip("Token 数", fmt.Sprintf("%d", len(tokens)), "个",
		"当前账号可见的 Token 数量。普通用户只看到自己的；超级管理员也只看到自己的。") +
		statCardTip("总命中", fmt.Sprintf("%d", totalHits), "次",
			"经本网关代理成功的请求次数（GitHub /g/ 与 HuggingFace /hf/ 路径）。"+
				"响应状态码 ≥ 400 的请求不计入；用别人 Token 的流量记在那个 Token 上。") +
		statCardTip("总流量", formatBytes(totalBytes), "",
			"上述成功响应实际写给客户端的字节数（按 Token 分别累计后求和）。") +
		statCardTip("运行时长", fmtDuration(time.Since(processStart)), "",
			"当前进程已运行时长，实例重启后重新计时。")
	// 链接转换器：识别与生成统一在服务端 buildAccelURL（与 MCP convert_link 同一实现）
	conv := `<div id="gmconv" data-api="` + s.entryBase(r) + `/api/convert" data-csrf="` + csrf + `" hidden></div>` +
		`<div class="conv-row"><input id="gmconv-in" class="conv-in" type="text" autocomplete="off" placeholder="粘贴原始链接或镜像名">` +
		`<button type="button" class="btn outline" onclick="gmPickImage('gmconv-in')">🔍 搜索镜像</button>` +
		`<button type="button" class="btn" onclick="gmConvert()">获取加速链接</button></div>` +
		`<div class="conv-row" style="margin-top:8px"><input id="gmconv-out" class="conv-out" type="text" readonly placeholder="加速链接将显示在这里">` +
		`<button type="button" id="gmconv-copy" class="btn outline" onclick="gmCopyConv()">复制</button>` +
		`<button type="button" class="btn outline" onclick="gmDownloadConv()">下载</button>` +
		`<button type="button" class="btn outline" onclick="gmOpenConv()">访问</button></div>` +
		`<details class="conv-help"><summary>使用说明与示例</summary><div class="conv-help-body">` +
		`<p style="margin:0 0 6px">基于当前访问地址与<strong>上方所选 Token</strong>生成。支持 GitHub / HuggingFace 链接（如 <code>github.com/owner/repo/releases/download/v1/a.zip</code>）与 Docker 镜像名（如 <code>library/nginx:1.25</code>）。</p>` +
		`<p style="margin:0">Docker 镜像会给出<strong>需带用户名段与 token</strong> 的 <code>/offline/&lt;用户名&gt;/&lt;token&gt;/&lt;镜像名&gt;</code> 离线 tar 地址（用户名段取当前登录用户）；registry 加速走 <code>/v2/&lt;用户名&gt;/…</code>，由 Docker 客户端直连，协议本身无法携带 token。</p>` +
		`</div></details>` +
		s.imagePickerHTML()
	// 链接转换卡片的 Token 快捷切换器（与「用法示例」的切换器联动，共用 .gmtok）
	convSel := s.tokenSelector(tokens, "链接转换使用的 Token")
	// Token / 服务管理 / 缓存管理均已独立成页，这里只放引导卡
	tokensGuide := `<p class="muted">Token 的创建、复制、删除与角色管理已独立成页，便于统一维护。</p>` +
		`<div class="row" style="margin-top:12px"><a class="btn" href="` + s.entryBase(r) + `/tokens">前往 Token 管理` +
		`（` + strconv.Itoa(len(tokens)) + `）</a></div>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, badges, dockerPathCard, stats, convSel, converter, examples, tokensGuide
	// 只 Sprintf body 片段（无 CSS 的 %），再用 pageApp() 全幅包裹
	body := fmt.Sprintf(dashboardBody, aside, topbarRight, flashHTML, badges, dockerPathCard, stats, convSel, conv, example, tokensGuide)
	_, _ = w.Write([]byte(pageApp("管理后台 - gh-mirror", "gh-mirror 管理后台", body)))
}

// dockerPathCard 渲染概览页「我的 Docker 自定义路径」设置（受 Docker 服务状态门控）。
func (s *Server) dockerPathCard(csrf, base, user string) string {
	if !s.dockerEnabled() {
		return `<p class="muted" style="margin-top:12px">Docker 加速未开启。如需设置自定义拉取路径，请联系超级管理员开启对应服务。</p>`
	}
	cur := html.EscapeString(s.userDockerPath(user))
	return `<form method="post" action="` + base + `/docker-path" class="row" style="margin-top:12px;align-items:flex-end">
  <input type="hidden" name="csrf" value="` + csrf + `">
  <label class="fld">我的 Docker 自定义路径<input name="dockerPath" value="` + cur + `" placeholder="可选，如 team-a" style="width:320px;max-width:100%"></label>
  <button type="submit" class="btn">保存</button>
</form>
<p class="muted small" style="margin-top:6px">你的 Docker 拉取命令：<code>docker pull 域名/` + html.EscapeString(dockerSegment(user)) + `[/自定义路径]/镜像名</code>（docker 客户端会自动补 <code>/v2/</code> 前缀，服务端完整路径为 <code>/v2/` + html.EscapeString(dockerSegment(user)) + `[/自定义路径]/…</code>）。registry 拉取<strong>不校验 Token</strong>，路径本身就是凭证；留空则仅用用户名段。自定义路径仅支持字母、数字、-、_、/（最长 64 字符）。</p>`
}

// renderServicesPage 渲染服务管理独立页（/admin/services）：GitHub / Docker / HuggingFace 三卡开关。
func (s *Server) renderServicesPage(w http.ResponseWriter, r *http.Request) {
	sid, user, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	base := baseOf(r)
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
	// Docker 卡片的用法说明：路径固定带 <用户名>[/<该用户的自定义路径>]
	ownerSeg := dockerSegment(user)
	dockerUsage := "docker pull &lt;域名&gt;/" + html.EscapeString(ownerSeg) +
		"/library/nginx:1.25（用户名段必带）；/offline/&lt;用户名&gt;/&lt;token&gt;/&lt;image&gt;"
	if c := s.userDockerPath(user); c != "" {
		dockerUsage = "docker pull &lt;域名&gt;/" + html.EscapeString(ownerSeg) + "/" + html.EscapeString(c) +
			"/library/nginx:1.25；/offline/&lt;用户名&gt;/&lt;token&gt;/&lt;image&gt;"
	}
	services := `<div class="svcgrid">` +
		svcCard("GH", "GitHub 资源加速", "覆盖 blob / raw、Releases、仓库打包 archive 与短链，支持 Range 断点续传。始终可用。", "/g/&lt;token&gt;/https://github.com/owner/repo", true, "") +
		svcCard("DK", "Docker 镜像加速", "registry-mirror 回源 Docker Hub / GHCR，并可产出可 docker load 的离线 tar。开启前请先阅读安全说明。", dockerUsage, s.dockerEnabled(), "docker") +
		svcCard("HF", "HuggingFace 权重", "加速模型与 LFS 大文件下载，token 模式下需带 token。", "/hf/&lt;token&gt;/owner/repo/resolve/main/file", s.hfEnabled(), "hf") +
		`</div><p class="muted small" style="margin-top:12px">开关<strong>即时生效、无需重启</strong>，并持久化到 <code>gm-data/settings.json</code>（重启后保持）。后台设置优先于环境变量 <code>DOCKER_ENABLED</code> / <code>HF_ENABLED</code>。</p>` + dockerModalHTML(s.userDockerPath(user), dockerSegment(user))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, services
	// 邀请码管理属用户体系，仅「安装时创建的超级管理员」可见与操作（设置区已移回用户管理页）
	invHTML := ""
	if s.requireRootSuper(r) {
		invHTML = s.inviteSectionHTML(r, csrf)
	}
	body := fmt.Sprintf(servicesBody, s.consoleAside(r), s.topbarRight(base), s.flashHTML(r), services, invHTML)
	_, _ = w.Write([]byte(pageApp("服务管理 - gh-mirror", "gh-mirror 服务管理", body)))
}

// renderCachePage 渲染缓存管理独立页（/admin/cache）：已用 / 上限 / 时效热改 + 一键清除。
func (s *Server) renderCachePage(w http.ResponseWriter, r *http.Request) {
	sid, user, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	base := baseOf(r)
	// 缓存管理：已用总量 / 单文件上限 / 总上限 / 时效，均热改；超限与过期自动清理；可一键清除
	maxFileMB := int(s.cache.MaxBytes() / (1024 * 1024))
	maxTotalMB := int(s.cache.MaxTotalBytes() / (1024 * 1024))
	ttlMin := int(s.cache.TTL() / time.Minute)
	totalTxt := "不限"
	if maxTotalMB > 0 {
		totalTxt = formatBytes(s.cache.MaxTotalBytes())
	}
	// 顶部统计卡：磁盘可用统一在下方「离线包缓存」区展示（避免重复）
	cacheMgmt := `<div class="stats">` +
		statCard("缓存已用", formatBytes(s.cache.UsageBytes()), "总上限 "+totalTxt) +
		statCard("单文件上限", fmt.Sprintf("%d MB", maxFileMB), "超过则不缓存") +
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
<p class="muted small" style="margin-top:12px">单文件上限：单个响应超过该大小则只透传、不缓存。总上限：超出后<strong>按最旧优先</strong>自动清理。<strong>磁盘可用空间低于 1 GB 时，即使没到总上限也会按最旧优先清理</strong>（缓存是临时的，避免写满磁盘拖垮整机）。时效：到期文件由后台周期任务自动删除。<strong>离线包缓存</strong>单独管理（见下方「离线包缓存」区：自动清理阈值 + 单文件上限），不受本区参数影响。设置即时生效并持久化，优先于环境变量 <code>MAX_CACHE_MB</code> / <code>CACHE_TTL_MINUTES</code>。</p>`
	// 离线包区块：分页参数 + 当前用户（复制下载链接用）
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	size := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && (v == 10 || v == 20 || v == 50) {
		size = v
	}
	offlineSection := s.offlineCacheSection(csrf, base, user, page, size)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, cacheMgmt, offlineSection
	body := fmt.Sprintf(cacheBody, s.consoleAside(r), s.topbarRight(base), s.flashHTML(r), cacheMgmt, offlineSection)
	_, _ = w.Write([]byte(pageApp("缓存管理 - gh-mirror", "gh-mirror 缓存管理", body)))
}

// imagePickerHTML 渲染「选择镜像」弹窗（常用镜像 + Docker Hub 搜索）。
// 离线包缓存页与总览页的链接转换共用；前端 gmPickImage(targetId) 决定选中后填入哪个输入框。
func (s *Server) imagePickerHTML() string {
	return `<div class="modal" id="gmImageModal" hidden>
  <div class="modal-mask" onclick="gmCloseModal()"></div>
  <div class="modal-box" style="max-width:720px">
    <div class="modal-head"><h3>选择镜像</h3><button type="button" class="modal-x" onclick="gmCloseModal()" aria-label="关闭">✕</button></div>
    <div class="modal-body">
      <div class="row" style="gap:8px;flex-wrap:nowrap">
        <input id="gmImageQuery" placeholder="搜索 Docker Hub，如 nginx / redis / postgres" style="flex:1;min-width:0" onkeydown="if(event.key==='Enter'){event.preventDefault();gmSearchImage()}">
        <button type="button" class="btn" onclick="gmSearchImage()">搜索</button>
      </div>
      <p class="muted small" style="margin:14px 0 6px">常用镜像（点击填入）：</p>
      <div class="row" id="gmImageCommon" style="gap:8px"></div>
      <p class="muted small" style="margin:14px 0 6px">搜索结果（最多 10 条，点击填入）：</p>
      <div id="gmImageResults"><p class="muted small">输入关键词后点「搜索」。</p></div>
    </div>
    <div class="modal-foot"><button type="button" class="btn outline" onclick="gmCloseModal()">关闭</button></div>
  </div>
</div>`
}

// offlineCacheSection 渲染缓存管理页的「离线包缓存」区块：自动清理设置 + 列表 + 多选/反选 + 分页。
// proxyBase 为代理地址（复制下载链接用）；user 为当前超管（链接带其用户名段与 token）。
func (s *Server) offlineCacheSection(csrf, proxyBase, user string, page, size int) string {
	pkgs := s.offlineCache.List()
	total := len(pkgs)
	totalPages := (total + size - 1) / size
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * size
	end := start + size
	if end > total {
		end = total
	}
	pagePkgs := pkgs[start:end]

	// 复制下载链接用的 token：当前超管自己的 token（offline 要求 token 归属该用户名段）
	tok := ""
	for _, t := range s.store.All() {
		if t.Owner == user || (t.Owner == "" && user == s.adminUser) {
			tok = t.Token
			break
		}
	}

	// 自动清理设置
	autoPurgeChecked := ""
	if s.offlineAutoPurge {
		autoPurgeChecked = " checked"
	}
	autoPurgeForm := `<form method="post" action="` + s.adminPath + `/offline/autopurge" class="row" style="margin-top:14px;align-items:flex-end">
  <input type="hidden" name="csrf" value="` + csrf + `">
  <label class="fld" style="flex-direction:row;gap:8px;align-items:center"><input type="checkbox" name="autoPurge" value="1"` + autoPurgeChecked + `> 磁盘满自动清理</label>
  <label class="fld">磁盘可用低于(GB)<input name="purgeFreeGB" type="number" min="0" value="` + strconv.Itoa(s.offlinePurgeFreeGB) + `"></label>
  <label class="fld">单文件上限(MB，0=不限)<input name="maxFileMB" type="number" min="0" value="` + strconv.Itoa(s.offlineMaxFileMB) + `"></label>
  <button type="submit" class="btn">保存</button>
</form>
<p class="muted small" style="margin-top:8px"><strong>磁盘满自动清理</strong>：开启后，磁盘可用空间低于阈值时，按<strong>最旧优先</strong>自动删除离线缓存包，直到恢复到阈值以上（默认关闭，避免误删有意保留的包）。<strong>单文件上限</strong>：按镜像 manifest 预估大小，超过该值的离线包<strong>不落盘缓存</strong>（仍可实时打包流式下载），0 = 不限制。</p>`

	// 列表行
	var rows strings.Builder
	for _, p := range pagePkgs {
		stateCls, stateTxt := "badge off", "✕ 失败"
		switch p.State {
		case OfflineReady:
			stateCls, stateTxt = "badge on", "● 已就绪"
		case OfflineBuilding:
			stateCls, stateTxt = "badge off", "◐ 打包中"
		}
		mtime := "—"
		if p.Mtime > 0 {
			mtime = time.Unix(p.Mtime, 0).Format("2006-01-02 15:04")
		}
		sizeTxt := formatBytes(p.Size)
		if p.State != OfflineReady {
			sizeTxt = "—"
		}
		dlLink := ""
		if tok != "" {
			dlLink = proxyBase + "/offline/" + dockerSegment(user) + "/" + tok + "/" + p.Image
		}
		img := html.EscapeString(p.Image)
		dg := html.EscapeString(p.Digest)
		// 版本号（tag / digest）与上游镜像构建时间；未知时只显示版本
		refTxt := strings.TrimSpace(p.Ref)
		if refTxt == "" {
			refTxt = "latest"
		}
		if strings.HasPrefix(refTxt, "sha256:") {
			refTxt = "digest " + shortTok(refTxt)
		}
		refMeta := ` · 版本 ` + html.EscapeString(refTxt)
		if p.Upstream > 0 {
			refMeta += ` · 上游更新 ` + relTime(p.Upstream)
		}
		copyBtn := `<button type="button" class="btn outline" title="复制下载链接" onclick="gmCopyText(this, &quot;` + html.EscapeString(dlLink) + `&quot;)"` + func() string { if tok == "" { return " disabled" }; return "" }() + `>复制链接</button>`
		rows.WriteString(`<tr class="tok-row">` +
			`<td data-label="选择"><input type="checkbox" name="digest" value="` + dg + `" aria-label="选择 ` + img + `"></td>` +
			`<td data-label="镜像"><code>` + img + `</code><div class="muted small">` + dg + refMeta + `</div></td>` +
			`<td data-label="状态"><span class="` + stateCls + `">` + stateTxt + `</span></td>` +
			`<td data-label="大小" class="num">` + sizeTxt + `</td>` +
			`<td data-label="缓存时间">` + mtime + `</td>` +
			`<td data-label="操作"><span class="ops">` + copyBtn +
			`<form method="post" action="` + s.adminPath + `/offline/repull" class="del-form"><input type="hidden" name="csrf" value="` + csrf + `"><input type="hidden" name="image" value="` + img + `"><button type="submit" class="btn outline">重新拉取</button></form>` +
			`<form method="post" action="` + s.adminPath + `/offline/repull-latest" class="del-form"><input type="hidden" name="csrf" value="` + csrf + `"><input type="hidden" name="image" value="` + img + `"><button type="submit" class="btn outline">拉取最新</button></form>` +
			`<form method="post" action="` + s.adminPath + `/offline/del" class="del-form"><input type="hidden" name="csrf" value="` + csrf + `"><input type="hidden" name="digest" value="` + dg + `"><button type="submit" class="danger" onclick="return confirm('确定删除该离线缓存包？')">删除</button></form>` +
			`</span></td></tr>`)
	}
	if total == 0 {
		rows.WriteString(`<tr><td colspan="6" class="muted">暂无离线缓存包。用户通过 /offline 下载镜像后会自动缓存，或点击下方「拉取」主动缓存。</td></tr>`)
	}

	// 分页条：始终渲染（空列表也能看到每页条数与页码，避免误以为没有分页）
	sizeBar := `<div class="row" style="margin-left:auto;gap:8px;align-items:center;flex-wrap:wrap">` +
		`<span class="muted small">共 ` + strconv.Itoa(total) + ` 个 · 第 ` + strconv.Itoa(page) + `/` + strconv.Itoa(totalPages) + ` 页</span>` +
		`<label class="muted small" style="display:inline-flex;align-items:center;gap:6px">每页` +
		`<select onchange="gmSetCachePageSize(this.value)" aria-label="每页显示离线包数" style="width:auto;height:32px;padding:0 28px 0 10px">` +
		selOpt(size, 10) + selOpt(size, 20) + selOpt(size, 50) +
		`</select> 条</label>` +
		pageLink(s.adminPath+"/cache", page-1, size, totalPages, "上一页") +
		pageLink(s.adminPath+"/cache", page+1, size, totalPages, "下一页") +
		`</div>`

	// 批量操作条
	pager := `<div class="row" style="margin-top:14px;gap:8px;align-items:center;flex-wrap:wrap">` +
		`<button type="button" class="btn outline" onclick="var c=document.querySelectorAll('input[name=digest]');for(var i=0;i<c.length;i++)c[i].checked=true">全选</button>` +
		`<button type="button" class="btn outline" onclick="var c=document.querySelectorAll('input[name=digest]');for(var i=0;i<c.length;i++)c[i].checked=!c[i].checked">反选</button>` +
		`<button type="submit" class="danger" onclick="return confirm('确定删除选中的离线缓存包？')">删除选中</button>` +
		`</div>`

	// 拉取新镜像表单（主动缓存）+ 镜像选择面板（常用镜像 / Docker Hub 搜索）
	imagePicker := s.imagePickerHTML()
	pullForm := `<form method="post" action="` + s.adminPath + `/offline/repull" class="row" style="margin-top:12px;gap:10px">
  <input type="hidden" name="csrf" value="` + csrf + `">
  <input id="gmOfflineImage" name="image" placeholder="镜像名，如 library/nginx:1.25 或 nginx:latest" required style="width:min(460px,100%)">
  <button type="button" class="btn outline" onclick="gmPickImage()">🔍 选择镜像</button>
  <button type="submit" class="btn">拉取并缓存</button>
</form>
<p class="muted small" style="margin-top:6px">主动拉取镜像到离线缓存（后台异步打包，稍后刷新可见）；用户下载时也会自动缓存。点「选择镜像」可从常用镜像或 Docker Hub 搜索结果快速填入。</p>` + imagePicker

	return `<section class="card">
  <div class="card-head"><h2>离线包缓存</h2><span class="muted small">内容寻址（manifest digest）· 全局共享 · 仅超管管理</span></div>
  <div class="stats">` +
		statCard("离线包数", strconv.Itoa(total), "个") +
		statCard("占用", formatBytes(s.offlineCache.TotalBytes()), "全局共享") +
		statCard("磁盘可用", formatBytes(diskFree(s.cfg.CacheDir)), "") +
		`</div>` +
		autoPurgeForm +
		`<div class="card-head" style="display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-top:22px">` +
		`<h2 style="margin:0">已缓存离线包</h2>` + sizeBar + `</div>` +
		`<form method="post" action="` + s.adminPath + `/offline/batch-del">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<div class="table-wrap"><table class="tok-table">` +
		`<thead><tr><th><input type="checkbox" onclick="var c=document.querySelectorAll('input[name=digest]');for(var i=0;i<c.length;i++)c[i].checked=this.checked" aria-label="全选"></th><th>镜像</th><th>状态</th><th class="num">大小</th><th>缓存时间</th><th>操作</th></tr></thead>` +
		`<tbody>` + rows.String() + `</tbody></table></div>` +
		pager +
		`</form>` +
		pullForm +
		`</section>`
}

// relTime 把 unix 时间转为「N 分钟前 / N 小时前 / N 天前」（0 或未来时间返回空串）。
func relTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	d := time.Since(time.Unix(ts, 0))
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes())+1)
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

// selOpt 生成分页「每页条数」下拉的选项（含选中态）。
func selOpt(cur, v int) string {
	sel := ""
	if cur == v {
		sel = " selected"
	}
	return `<option value="` + strconv.Itoa(v) + `"` + sel + `>` + strconv.Itoa(v) + `</option>`
}

// pageLink 生成分页链接（禁用态输出占位）。
func pageLink(base string, page, size, totalPages int, label string) string {
	if page < 1 || page > totalPages {
		return `<span class="btn outline" style="opacity:.4;pointer-events:none">` + label + `</span>`
	}
	return `<a class="btn outline" href="` + base + `?page=` + strconv.Itoa(page) + `&size=` + strconv.Itoa(size) + `">` + label + `</a>`
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
func (s *Server) tokenRows(tokens []*Token, csrf, base string) string {
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
			`<tr class="tok-row"><td data-label="选择" class="sel-cell">`+
				`<input type="checkbox" class="tok-sel" form="gmBatchForm" name="token" value="%s" aria-label="选择该 Token"></td>`+
				`<td data-label="Token"><span class="tok-cell"><code title="完整 Token：%s">%s</code>`+
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
<tr class="edit-row" hidden><td colspan="9">
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
			esc, esc, shown, esc, html.EscapeString(t.Note), roleCls, roleTxt,
			time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"),
			t.Hits, formatBytes(t.Bytes), lastUsed,
			base, csrf, esc,
			base, csrf, esc, html.EscapeString(t.Note),
			selAttr(normRole(t.Role) == RoleUser), selAttr(normRole(t.Role) == RoleAdmin)))
	}
	if len(tokens) == 0 {
		rows.WriteString(`<tr><td colspan="9" class="muted">暂无 token，请在上方创建</td></tr>`)
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
func (s *Server) tokenAddForm(csrf, base string) string {
	return `<form method="post" action="` + base + `/add" class="row">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<input name="token" placeholder="留空则自动生成">` +
		`<input name="note" placeholder="备注（可选）">` +
		`<select name="role" title="Token 类型：高级 Token 可用于 MCP 接入" aria-label="Token 类型">` +
		`<option value="user">普通 Token</option><option value="admin">高级 Token（可用于 MCP）</option></select>` +
		`<button type="submit" class="btn">添加</button></form>` +
		`<p class="muted small" style="margin-top:8px">说明：<strong>普通 Token</strong>仅用于代理访问；<strong>高级 Token</strong> 另可用于 <code>MCP</code> 接入（<code>/mcp</code> 或 <code>gh-mirror mcp</code>），请勿随意外发。</p>`
}

// tokenFoot Token 页面页脚链接（各带适配图标，便于快速识别）。
func (s *Server) tokenFoot(base string) string {
	return `<a href="` + base + `/logout">` + logoutSVG + `退出登录</a>` +
		`<span class="foot-sep">·</span>` +
		`<a href="/">` + homeSVG + `返回首页</a>` +
		`<span class="foot-sep">·</span>` +
		`<a href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + `GitHub</a>`
}

// renderTokensPage 渲染 Token 管理独立页面（/admin/tokens）。
func (s *Server) renderTokensPage(w http.ResponseWriter, r *http.Request) {
	sid, user, role, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	entry := s.entryBase(r)
	all := s.tokensForUser(s.store.All(), user, role)

	// 每页条数：10（默认）/ 20 / 50；页码从 1 开始
	size := 10
	switch r.URL.Query().Get("size") {
	case "20":
		size = 20
	case "50":
		size = 50
	}
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	total := len(all)
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > total {
		end = total
	}
	pageTokens := all[start:end]

	// 列表工具条：批量删除（行复选框通过 form 属性关联到该表单）+ 每页条数
	sizeSel := func(sz int) string {
		if size == sz {
			return " selected"
		}
		return ""
	}
	toolbar := `<form id="gmBatchForm" method="post" action="` + entry + `/del" style="display:contents">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<button type="submit" id="gmBatchDel" class="btn danger" disabled onclick="return gmConfirmBatchDel()">删除选中</button>` +
		`</form>` +
		`<label class="muted small" style="margin-left:auto;display:inline-flex;align-items:center;gap:6px">每页` +
		`<select onchange="gmSetTokenPageSize(this.value)" aria-label="每页显示条数" style="width:auto;height:32px;padding:0 28px 0 10px">` +
		`<option value="10"` + sizeSel(10) + `>10</option>` +
		`<option value="20"` + sizeSel(20) + `>20</option>` +
		`<option value="50"` + sizeSel(50) + `>50</option>` +
		`</select> 条</label>`

	pageURL := func(p int) string {
		return entry + "/tokens?size=" + strconv.Itoa(size) + "&page=" + strconv.Itoa(p)
	}
	pager := `<div class="row" style="margin-top:14px;gap:8px;align-items:center">` +
		`<span class="muted small">共 ` + strconv.Itoa(total) + ` 个 · 第 ` + strconv.Itoa(page) + `/` + strconv.Itoa(pages) + ` 页</span>`
	if page > 1 {
		pager += `<a class="btn outline" href="` + pageURL(page-1) + `">上一页</a>`
	}
	if page < pages {
		pager += `<a class="btn outline" href="` + pageURL(page+1) + `">下一页</a>`
	}
	pager += `</div>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, toolbar, rows, pager, addForm, foot
	body := fmt.Sprintf(tokensBody, s.consoleAside(r), s.topbarRight(baseOf(r)), s.flashHTML(r),
		toolbar, s.tokenRows(pageTokens, csrf, entry), pager, s.tokenAddForm(csrf, entry), s.tokenFoot(entry))
	_, _ = w.Write([]byte(pageApp("令牌管理 - gh-mirror", "gh-mirror 令牌管理", body)))
}

// tokenVisible 判断某 token 是否属于当前用户（空 owner 的旧数据归属默认管理员 adminUser）。
// 任何角色（含 super）在常规页面都只看自己的 token——查看他人 token 仅限
// 「用户管理 → 某用户 Token 详情」（仅安装时超管，只读）。
func (s *Server) tokenVisible(t *Token, user, role string) bool {
	_ = role
	return t.Owner == user || (t.Owner == "" && user == s.adminUser)
}

// tokensForUser 返回属于当前用户的 token 列表（任何角色都只返回自己的）。
func (s *Server) tokensForUser(tokens []*Token, user, role string) []*Token {
	out := make([]*Token, 0, len(tokens))
	for _, t := range tokens {
		if s.tokenVisible(t, user, role) {
			out = append(out, t)
		}
	}
	return out
}

// adminTokensFor 过滤出 role=admin（高级 Token）的 token（调用方需先按用户过滤）。
func adminTokensFor(tokens []*Token) []*Token {
	var out []*Token
	for _, t := range tokens {
		if normRole(t.Role) == RoleAdmin {
			out = append(out, t)
		}
	}
	return out
}

// mcpTokenSelector 生成 MCP 页的 Token 选择器（只列「归属当前用户」的 admin token，默认选中第一个）。
// 用 .gmtok class 复用全局联动：切换后同步本页所有 .tslot（HTTP/stdio 示例里的 token）。
func (s *Server) mcpTokenSelector(tokens []*Token) string {
	adm := adminTokensFor(tokens)
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
	sid, user, role, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	_ = sid
	tokens := s.tokensForUser(s.store.All(), user, role)
	adm := adminTokensFor(tokens)
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
			s.mcpTokenSelector(tokens)
	} else {
		status = `<div class="flash err"><strong style="flex:0 0 auto">!</strong><span>未就绪：当前用户 <code>` + html.EscapeString(user) + `</code> 名下还没有「高级 Token」。MCP 鉴权要求 admin 角色（高级 Token），请先到「Token 管理」创建一个。</span></div>` +
			`<div class="row" style="margin-top:12px"><a class="btn" href="` + s.entryBase(r) + `/tokens">去创建高级 Token</a></div>`
	}

	// 两种接入方式的示例命令（token 段为 .tslot，随选择器联动）
	httpEx, stdioEx := "", ""
	if tk != "" {
		httpEx = `<p class="muted"><strong>推荐首选</strong>：MCP 规范 2025-03-26 的 <strong>Streamable HTTP</strong>（客户端配置 <code>"type": "http"</code>），远程网关无需本地二进制。endpoint：<code>` + html.EscapeString(base) + `/ai/` + html.EscapeString(dockerSegment(user)) + `/mcp</code>（鉴权用 <code>Authorization: Bearer &lt;token&gt;</code>，Token 必须归属该用户）</p>` +
			`<p class="muted small" style="margin:12px 0 4px">客户端配置（复制到 Claude Desktop / CodeBuddy / Cursor / Cline 等）：</p>` +
			`<div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">json</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>{
  "mcpServers": {
    "gh-mirror": {
      "type": "http",
      "url": "` + html.EscapeString(base) + `/ai/` + html.EscapeString(dockerSegment(user)) + `/mcp",
      "headers": { "Authorization": "Bearer ` + tokSlot(tk) + `" }
    }
  }
}</code></pre></div>` +
			`<p class="muted small" style="margin:12px 0 4px">等价 curl 验证：</p>` +
			`<div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -sX POST ` + html.EscapeString(base) + `/ai/` + html.EscapeString(dockerSegment(user)) + `/mcp \
  -H "Authorization: Bearer ` + tokSlot(tk) + `" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'</code></pre></div>` +
			`<p class="muted small" style="margin-top:10px">协议行为：无状态；POST 返回单个 JSON（规范允许，不强制 SSE 流）；<code>GET</code> 返回 405 属预期。</p>`
		stdioEx = `<p class="muted"><strong>仅限与网关数据同机</strong>：stdio 直接操作本机数据目录（<code>TOKENS_FILE</code> 等）并用其校验 token，<strong>不是</strong>远程网关的遥控器——远程部署请用上方 HTTP 方式；Docker 镜像无 shell，不能 <code>docker exec</code> 运行。日志写 stderr，stdout 只输出协议 JSON。</p>` +
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
// renderUsersPage 渲染用户管理页（仅超级管理员可访问）。
// 支持按用户名/邮箱搜索（q）与分页（size=10/20/30/50、page）。
func (s *Server) renderUsersPage(w http.ResponseWriter, r *http.Request) {
	sid, user, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	csrf := s.csrfToken(sid)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	all := s.users.All()
	users := all
	if q != "" {
		lq := strings.ToLower(q)
		users = make([]*User, 0, len(all))
		for _, u := range all {
			if strings.Contains(strings.ToLower(u.Username), lq) || strings.Contains(strings.ToLower(u.Email), lq) {
				users = append(users, u)
			}
		}
	}
	// 每页条数：10（默认）/ 20 / 30 / 50
	size := 10
	switch r.URL.Query().Get("size") {
	case "20":
		size = 20
	case "30":
		size = 30
	case "50":
		size = 50
	}
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	total := len(users)
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > total {
		end = total
	}
	pageUsers := users[start:end]

	// 每个用户的 Token 数（一次遍历；空 owner 归默认管理员，兼容旧数据）
	counts := map[string]int{}
	for _, t := range s.store.All() {
		o := t.Owner
		if o == "" {
			o = s.adminUser
		}
		counts[o]++
	}
	// 把各用户的 Token 只读数据内嵌进页面：弹窗直接本地渲染，不再额外请求。
	// 这样不受反代/CDN 对 .json 路径的处理、ADMIN_PATH 非默认等因素影响。
	tokenMap := map[string][]userTokenJSON{}
	for _, u := range all {
		if counts[u.Username] > 0 {
			tokenMap[u.Username] = s.userTokenData(u.Username)
		}
	}
	tb, err := json.Marshal(tokenMap)
	if err != nil {
		tb = []byte("{}")
	}
	// 防止数据中出现 </script 提前闭合脚本标签
	tokensJSON := strings.ReplaceAll(string(tb), "</", "<\\/")

	// 工具条：用户名/邮箱搜索 + 分页（每页 10/20/30/50 + 上一页/下一页）
	usersBase := s.adminPath + "/users"
	sizeSel := func(sz int) string {
		if size == sz {
			return " selected"
		}
		return ""
	}
	pageURL := func(p int) string {
		u := usersBase + "?size=" + strconv.Itoa(size) + "&page=" + strconv.Itoa(p)
		if q != "" {
			u += "&q=" + url.QueryEscape(q)
		}
		return u
	}
	nav := ""
	if page > 1 {
		nav += `<a class="btn outline" href="` + pageURL(page-1) + `">上一页</a>`
	}
	if page < pages {
		nav += `<a class="btn outline" href="` + pageURL(page+1) + `">下一页</a>`
	}
	clearLink := ""
	if q != "" {
		clearLink = `<a class="btn outline" href="` + usersBase + `?size=` + strconv.Itoa(size) + `">清除</a>`
	}
	userTools := `<form method="get" action="` + usersBase + `" class="row" style="gap:8px;align-items:center;margin-left:auto;flex-wrap:wrap">` +
		`<input type="hidden" name="size" value="` + strconv.Itoa(size) + `">` +
		`<input name="q" value="` + html.EscapeString(q) + `" placeholder="搜索用户名 / 邮箱" aria-label="搜索用户名或邮箱" style="width:200px">` +
		`<button type="submit" class="btn outline">搜索</button>` + clearLink + `</form>` +
		`<div class="row" style="gap:8px;align-items:center;flex-wrap:wrap">` +
		`<span class="muted small">共 ` + strconv.Itoa(total) + ` 个 · 第 ` + strconv.Itoa(page) + `/` + strconv.Itoa(pages) + ` 页</span>` +
		`<label class="muted small" style="display:inline-flex;align-items:center;gap:6px">每页` +
		`<select onchange="gmSetUserPageSize(this.value)" aria-label="每页显示用户数" style="width:auto;height:32px;padding:0 28px 0 10px">` +
		`<option value="10"` + sizeSel(10) + `>10</option>` +
		`<option value="20"` + sizeSel(20) + `>20</option>` +
		`<option value="30"` + sizeSel(30) + `>30</option>` +
		`<option value="50"` + sizeSel(50) + `>50</option>` +
		`</select> 条</label>` + nav + `</div>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, topbarRight, flash, settings, userTools, rows, addForm, tokensData
	body := fmt.Sprintf(usersBody, s.consoleAside(r), s.topbarRight(baseOf(r)), s.flashHTML(r),
		s.portalSettingsHTML(csrf), userTools, s.userRows(pageUsers, counts, csrf, user), s.userAddForm(csrf), tokensJSON)
	_, _ = w.Write([]byte(pageApp("用户管理 - gh-mirror", "gh-mirror 用户管理", body)))
}

// portalSettingsHTML 渲染用户管理页底部的「用户注册与邮箱」设置区（按钮 + 弹窗表格表单，仅超管）。
func (s *Server) portalSettingsHTML(csrf string) string {
	cfg := s.smtpSettings()
	port := ""
	if cfg.Port > 0 {
		port = strconv.Itoa(cfg.Port)
	}
	mode := s.regMode()
	// 状态摘要：注册模式 / 联系邮箱 / SMTP
	regTxt := "关闭注册"
	switch mode {
	case RegInvite:
		regTxt = "仅邀请码注册"
	case RegOpen:
		regTxt = "开放注册"
	}
	mailState := "未设置"
	if v := s.contactEmail(); v != "" {
		mailState = html.EscapeString(v)
	}
	smtpState := "未配置"
	if cfg.Host != "" {
		smtpState = html.EscapeString(cfg.Host)
		if cfg.Port > 0 {
			smtpState += ":" + port
		}
	}
	warnHTML := ""
	if mode != RegClosed && cfg.Host == "" {
		warnHTML = ` · <strong style="color:#d97706">SMTP 未配置，邮箱验证码无法发送</strong>`
	}
	sel := func(m string) string {
		if mode == m {
			return " selected"
		}
		return ""
	}
	chk := func(on bool) string {
		if on {
			return " checked"
		}
		return ""
	}
	return `<section class="card">
  <div class="card-head"><h2>用户注册与邮箱</h2><span class="muted small">注册模式 / 图形验证码 / 联系邮箱 / SMTP</span></div>
  <div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-top:8px">
    <button type="button" class="btn" onclick="gmOpenModal('gmPortalModal','portalSettingsForm','保存')">⚙️ 设置</button>
    <span class="muted small">当前：` + regTxt + ` · 联系邮箱 ` + mailState + ` · SMTP ` + smtpState + warnHTML + `</span>
  </div>
  <div class="modal" id="gmPortalModal" hidden>
    <div class="modal-mask" onclick="gmCloseModal()"></div>
    <div class="modal-box" role="dialog" aria-modal="true" aria-labelledby="gmPortalModalTitle">
      <div class="modal-head"><h3 id="gmPortalModalTitle">用户注册与邮箱</h3><button type="button" class="modal-x" onclick="gmCloseModal()" aria-label="关闭">✕</button></div>
      <form id="portalSettingsForm" method="post" action="` + s.adminPath + `/portal-settings">
        <input type="hidden" name="csrf" value="` + csrf + `">
        <input type="hidden" name="scope" value="all">
        <div class="modal-body">
          <table class="kv-table">
            <tr><th>注册模式</th><td>
              <select name="regMode">
                <option value="closed"` + sel(RegClosed) + `>关闭注册</option>
                <option value="invite"` + sel(RegInvite) + `>仅邀请码注册</option>
                <option value="open"` + sel(RegOpen) + `>开放注册</option>
              </select>
              <p class="muted small" style="margin:6px 0 0">「仅邀请码」需在服务管理页的邀请码管理中生成邀请码；关闭后注册页与注册入口均不可用。</p>
            </td></tr>
            <tr><th>图形验证码</th><td>
              <div class="kv-checks">
                <label><input type="checkbox" name="userCaptcha" value="1"` + chk(s.userCaptchaEnabled()) + `> 用户入口（登录 / 注册）</label>
                <label><input type="checkbox" name="adminCaptcha" value="1"` + chk(s.adminCaptchaEnabled()) + `> 管理入口登录</label>
              </div>
            </td></tr>
            <tr><th>首页入口</th><td>
              <div class="kv-checks">
                <label><input type="checkbox" name="hideLogin" value="1"` + chk(s.hideLoginEnabled()) + `> 隐藏首页登录 / 注册按钮</label>
              </div>
              <p class="kv-hint">隐藏后首页不再显示入口，但 <code>/admin</code>、<code>/user</code> 仍可直接访问。</p>
            </td></tr>
            <tr><th>联系邮箱</th><td>
              <input name="contactEmail" type="email" value="` + html.EscapeString(s.contactEmail()) + `" placeholder="用于用户紧急联系与找回密码引导">
            </td></tr>
            <tr class="kv-group"><th colspan="2">SMTP 发信设置</th></tr>
            <tr><th>SMTP 主机</th><td><input name="smtpHost" value="` + html.EscapeString(cfg.Host) + `" placeholder="smtp.example.com"></td></tr>
            <tr><th>端口</th><td><input name="smtpPort" type="number" min="1" value="` + port + `" placeholder="465" style="max-width:140px"></td></tr>
            <tr><th>账号</th><td><input name="smtpUser" value="` + html.EscapeString(cfg.Username) + `" autocomplete="off"></td></tr>
            <tr><th>密码</th><td><input name="smtpPass" type="password" value="" placeholder="已设置则留空保持不变" autocomplete="new-password"></td></tr>
            <tr><th>发件人</th><td><input name="smtpFrom" type="email" value="` + html.EscapeString(cfg.From) + `" placeholder="no-reply@example.com"></td></tr>
            <tr><th>加密</th><td><label class="kv-check"><input type="checkbox" name="smtpTLS" value="1"` + chk(cfg.TLS) + `> 使用 TLS</label></td></tr>
          </table>
        </div>
        <div class="modal-foot">
          <button type="button" class="btn outline" onclick="gmCloseModal()">取消</button>
          <button type="submit" class="btn">保存</button>
        </div>
      </form>
    </div>
  </div>
  <p class="muted small" style="margin-top:10px">SMTP 凭据保存在 <code>gm-data/settings.json</code>（文件权限 0600）。注册模式：关闭 = 无自助注册；仅邀请码 = 需有效邀请码；开放 = 邮箱验证码自助注册（需先配置 SMTP）。超级管理员密码无法通过邮箱找回。</p>
</section>`
}

// inviteSectionHTML 渲染服务管理页底部的「邀请码管理」区（仅超管）：生成/自定义/导出/删除/备注/有效期 + 使用追踪。
func (s *Server) inviteSectionHTML(r *http.Request, csrf string) string {
	all := s.invites.List()
	// 每页条数：10（默认）/ 20 / 50；参数用 inv_size/inv_page，避免与服务管理页其它查询冲突
	size := 10
	switch r.URL.Query().Get("inv_size") {
	case "20":
		size = 20
	case "50":
		size = 50
	}
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("inv_page")); err == nil && v > 0 {
		page = v
	}
	total := len(all)
	pages := (total + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > total {
		end = total
	}
	pageInvites := all[start:end]
	sizeSel := func(sz int) string {
		if size == sz {
			return " selected"
		}
		return ""
	}
	var rows strings.Builder
	for _, inv := range pageInvites {
		expireTxt := "永久"
		if inv.ExpireAt > 0 {
			expireTxt = time.Unix(inv.ExpireAt, 0).Format("2006-01-02 15:04")
		}
		stateCls, stateTxt, usedTxt := "badge on", "● 未使用", "—"
		if inv.Used {
			stateCls, stateTxt = "badge off", "○ 已使用"
			usedTxt = html.EscapeString(inv.UsedBy) + " · " + time.Unix(inv.UsedAt, 0).Format("2006-01-02 15:04")
		}
		note := inv.Note
		if note == "" {
			note = "—"
		}
		rows.WriteString(`<tr class="tok-row">` +
			`<td data-label="选择"><input type="checkbox" name="code" value="` + html.EscapeString(inv.Code) + `" aria-label="选择邀请码"></td>` +
			`<td data-label="邀请码"><code>` + html.EscapeString(inv.Code) + `</code></td>` +
			`<td data-label="备注">` + html.EscapeString(note) + `</td>` +
			`<td data-label="有效期">` + expireTxt + `</td>` +
			`<td data-label="状态"><span class="` + stateCls + `">` + stateTxt + `</span></td>` +
			`<td data-label="使用者/激活时间">` + usedTxt + `</td>` +
			`<td data-label="创建时间">` + time.Unix(inv.CreatedAt, 0).Format("2006-01-02 15:04") + `</td></tr>`)
	}
	if len(pageInvites) == 0 {
		rows.WriteString(`<tr><td colspan="7" class="muted">暂无邀请码</td></tr>`)
	}
	// 分页工具条（与 Token 列表一致：每页 10/20/50 + 上一页/下一页）
	pageURL := func(p int) string {
		return s.adminPath + "/services?inv_size=" + strconv.Itoa(size) + "&inv_page=" + strconv.Itoa(p)
	}
	pagerNav := ""
	if page > 1 {
		pagerNav += `<a class="btn outline" href="` + pageURL(page-1) + `">上一页</a>`
	}
	if page < pages {
		pagerNav += `<a class="btn outline" href="` + pageURL(page+1) + `">下一页</a>`
	}
	// 页码 + 每页条数 + 上一页/下一页 放在同一行（翻页紧邻页码，避免跑到表格下方）
	sizeBar := `<div class="row" style="margin:0 0 10px;gap:10px;align-items:center;flex-wrap:wrap">` +
		`<span class="muted small">共 ` + strconv.Itoa(total) + ` 个 · 第 ` + strconv.Itoa(page) + `/` + strconv.Itoa(pages) + ` 页</span>` +
		`<label class="muted small" style="display:inline-flex;align-items:center;gap:6px">每页` +
		`<select onchange="gmSetInvitePageSize(this.value)" aria-label="每页显示邀请码数" style="width:auto;height:32px;padding:0 28px 0 10px">` +
		`<option value="10"` + sizeSel(10) + `>10</option>` +
		`<option value="20"` + sizeSel(20) + `>20</option>` +
		`<option value="50"` + sizeSel(50) + `>50</option>` +
		`</select> 条</label>` + pagerNav + `</div>`
	return `<section class="card">
  <div class="card-head"><h2>邀请码管理</h2><span class="muted small">批量生成 / 自定义 / 导出 / 备注 / 有效期</span></div>

  <div style="margin-top:14px">
    <h3 style="margin:0 0 8px;font-size:.95rem">批量生成</h3>
    <form method="post" action="` + s.adminPath + `/invite/generate" class="row" style="gap:10px;align-items:flex-end;flex-wrap:wrap">
      <input type="hidden" name="csrf" value="` + csrf + `">
      <label class="fld">数量<input name="count" type="number" min="1" max="10000" value="10" style="width:80px"></label>
      <label class="fld">备注<input name="note" placeholder="批量备注（可选）" style="width:170px"></label>
      <label class="fld">有效期(天,0=永久)<input name="days" type="number" min="0" value="0" style="width:80px"></label>
      <label class="fld">导出格式<input name="format" value="{code}" placeholder="{code}" style="width:140px"></label>
      <button type="submit" class="btn">批量生成并下载</button>
    </form>
  </div>

  <div style="margin-top:16px;padding-top:14px;border-top:1px solid var(--border)">
    <h3 style="margin:0 0 8px;font-size:.95rem">自定义邀请码</h3>
    <form method="post" action="` + s.adminPath + `/invite/add" class="row" style="gap:10px;align-items:flex-end;flex-wrap:wrap">
      <input type="hidden" name="csrf" value="` + csrf + `">
      <label class="fld">邀请码<input name="code" placeholder="6-64 位字母数字" style="width:190px"></label>
      <label class="fld">备注<input name="note" placeholder="备注（可选）" style="width:170px"></label>
      <label class="fld">有效期(天,0=永久)<input name="days" type="number" min="0" value="0" style="width:80px"></label>
      <button type="submit" class="btn outline">添加</button>
    </form>
  </div>
  <form method="post" action="` + s.adminPath + `/invite/batch" style="margin-top:16px">
    <input type="hidden" name="csrf" value="` + csrf + `">
    ` + sizeBar + `
    <div class="table-wrap"><table class="tok-table">
      <thead><tr><th><input type="checkbox" onclick="var c=document.querySelectorAll('input[name=code]');for(var i=0;i<c.length;i++)c[i].checked=this.checked" aria-label="全选"></th><th>邀请码</th><th>备注</th><th>有效期</th><th>状态</th><th>使用者/激活时间</th><th>创建时间</th></tr></thead>
      <tbody>` + rows.String() + `</tbody>
    </table></div>
    <div class="row" style="margin-top:12px;gap:8px;align-items:center;flex-wrap:wrap">
      <button type="button" class="btn outline" onclick="var c=document.querySelectorAll('input[name=code]');for(var i=0;i<c.length;i++)c[i].checked=true">全选</button>
      <button type="button" class="btn outline" onclick="var c=document.querySelectorAll('input[name=code]');for(var i=0;i<c.length;i++)c[i].checked=!c[i].checked">反选</button>
      <button type="submit" class="danger" name="op" value="delete" onclick="return confirm('确定删除选中的邀请码？')">删除选中</button>
      <input name="note" placeholder="给选中批量打备注">
      <button type="submit" class="btn outline" name="op" value="note">批量备注</button>
      <input name="days" type="number" min="0" placeholder="有效期(天)" style="width:150px">
      <button type="submit" class="btn outline" name="op" value="expire">批量设有效期</button>
    </div>
  </form>
  <div style="margin-top:16px;padding-top:14px;border-top:1px solid var(--border)">
    <h3 style="margin:0 0 8px;font-size:.95rem">导出</h3>
    <form method="post" action="` + s.adminPath + `/invite/export">
      <input type="hidden" name="csrf" value="` + csrf + `">
      <div class="row" style="gap:10px;align-items:flex-end;flex-wrap:wrap">
        <label class="fld">导出格式<input name="format" value="{code}" style="width:240px"></label>
        <label class="fld">导出时批量打备注<input name="note" placeholder="可选（应用到全部）" style="width:240px"></label>
        <button type="submit" class="btn">导出全部为文本</button>
      </div>
      <p class="muted small" style="margin:8px 0 0">
        占位符：<code>{code}</code> 邀请码 · <code>{note}</code> 备注 · <code>{expire}</code> 有效期（永久 / 具体时间）·
        <code>{used}</code> 未使用 / 已使用 · <code>{used_by}</code> 使用者 · <code>{used_at}</code> 激活时间 ·
        <code>{created_at}</code> 创建时间（空值输出空字符串）。<br>
        示例：<code>{code}</code> → <code>ABCD1234</code>；
        <code>{code} — {note}（{expire}）</code> → <code>ABCD1234 — 邀请张三（永久）</code>；
        CSV：<code>{code},{used},{used_by},{used_at}</code>
      </p>
    </form>
  </div>
  <p class="muted small" style="margin-top:10px">邀请码为一次性：用户注册使用后标记为「已使用」并记录使用者与激活时间；填写「导出时批量打备注」会先给选中码统一打备注再导出。</p>
</section>`
}

// toggleRole 返回与当前角色相反的角色（用于「设为管理员/设为普通」切换按钮）。
func toggleRole(role string) string {
	if role == RoleSuper {
		return RoleNormal
	}
	return RoleSuper
}

// userRows 渲染用户列表（当前登录用户标注「我」；改角色/重置密码/删除为行内操作）。
// 安装时创建的超级管理员（s.adminUser）唯一，不可降级/删除，也不显示操作按钮；
// 其他 super 角色显示为「管理员」（可被唯一超管调整）。
func (s *Server) userRows(users []*User, counts map[string]int, csrf, current string) string {
	var rows strings.Builder
	for _, u := range users {
		roleTxt, roleCls := "普通用户", "badge"
		isRoot := u.Username == s.adminUser
		if isRoot {
			roleTxt, roleCls = "超级管理员（唯一）", "badge role-admin"
		} else if u.Role == RoleSuper {
			roleTxt, roleCls = "管理员", "badge role-admin"
		}
		me := ""
		if u.Username == current {
			me = ` <span class="muted small">（我）</span>`
		}
		esc := html.EscapeString(u.Username)
		// 邮箱（手工创建的用户可能没有）
		emailCell := `<span class="muted">—</span>`
		if u.Email != "" {
			emailCell = html.EscapeString(u.Email)
		}
		// 最后登录（从未登录显示 —）
		lastLogin := `<span class="muted">—</span>`
		if u.LastLoginAt > 0 {
			lastLogin = time.Unix(u.LastLoginAt, 0).Format("2006-01-02 15:04")
		}
		// Token 数：点击在原页弹出只读详情（不跳转页面）
		tokenCell := `<span class="muted">0</span>`
		if n := counts[u.Username]; n > 0 {
			tokenCell = `<button type="button" class="tok-count" title="查看该用户的 Token（只读）" onclick="gmShowUserTokens('` + esc + `')">` + strconv.Itoa(n) + `</button>`
		}
		ops := `<span class="muted small">安装时创建，不可降级 / 删除</span>`
		if !isRoot {
			btnTxt, roleVal := "设为管理员", RoleSuper
			if u.Role == RoleSuper {
				btnTxt, roleVal = "设为普通用户", RoleNormal
			}
			ops = fmt.Sprintf(
				`<form method="post" action="%s/users/role" class="del-form"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="username" value="%s"><input type="hidden" name="role" value="%s"><button type="submit" class="btn outline" onclick="return confirm('确定%s该用户？')">%s</button></form>`+
					`<form method="post" action="%s/users/pass" class="del-form"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="username" value="%s"><input type="password" name="pass" placeholder="新密码" required><button type="submit" class="btn outline">重置密码</button></form>`+
					`<form method="post" action="%s/users/del" class="del-form"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="username" value="%s"><button type="submit" class="danger" onclick="return confirm('确定删除该用户？其 Token 不会被删除。')">删除</button></form>`,
				s.adminPath, csrf, esc, roleVal, btnTxt, btnTxt,
				s.adminPath, csrf, esc,
				s.adminPath, csrf, esc)
		}
		rows.WriteString(fmt.Sprintf(
			`<tr class="tok-row"><td data-label="用户名"><code>%s</code>%s</td>`+
				`<td data-label="邮箱">%s</td>`+
				`<td data-label="角色"><span class="%s">%s</span></td>`+
				`<td data-label="创建时间">%s</td>`+
				`<td data-label="最后登录">%s</td>`+
				`<td data-label="Token 数">%s</td>`+
				`<td data-label="操作"><span class="ops">%s</span></td></tr>`,
			esc, me, emailCell, roleCls, roleTxt,
			time.Unix(u.CreatedAt, 0).Format("2006-01-02 15:04"), lastLogin, tokenCell, ops))
	}
	if len(users) == 0 {
		rows.WriteString(`<tr><td colspan="7" class="muted">暂无用户</td></tr>`)
	}
	return rows.String()
}

// renderUserTokensPage 渲染某用户的 Token 只读详情页（/admin/users/tokens，仅安装时超管）。
func (s *Server) renderUserTokensPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("user"))
	if target == "" {
		http.Redirect(w, r, s.adminPath+"/users", http.StatusFound)
		return
	}
	if _, exists := s.users.Get(target); !exists {
		http.Redirect(w, r, s.adminPath+"/users?err=bad_input", http.StatusFound)
		return
	}
	rows := s.userTokenRows(s.store.All(), target)
	back := `<a class="btn outline" href="` + s.adminPath + `/users">← 返回用户管理</a>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// verbs: aside, username, topbarRight, flash, back, rows（须与模板占位符顺序一致）
	body := fmt.Sprintf(userTokensBody, s.consoleAside(r), html.EscapeString(target), s.topbarRight(baseOf(r)), s.flashHTML(r),
		back, rows)
	_, _ = w.Write([]byte(pageApp("Token 详情 · "+target+" - gh-mirror", "该用户的 Token 详情（只读）", body)))
}

// userTokenJSON 是某用户 Token 的只读快照（JSON API 与页面内嵌数据共用）。
type userTokenJSON struct {
	Token    string `json:"token"`
	Note     string `json:"note"`
	Role     string `json:"role"`
	Hits     int64  `json:"hits"`
	Bytes    string `json:"bytes"`
	Created  string `json:"created"`
	LastUsed string `json:"lastUsed"`
}

// userTokenData 汇总某用户的 Token 只读数据（空 owner 归默认管理员，兼容旧数据）。
func (s *Server) userTokenData(target string) []userTokenJSON {
	out := make([]userTokenJSON, 0, 8)
	for _, t := range s.store.All() {
		if !(t.Owner == target || (t.Owner == "" && target == s.adminUser)) {
			continue
		}
		roleTxt := "普通"
		if normRole(t.Role) == RoleAdmin {
			roleTxt = "高级（MCP）"
		}
		lastUsed := "—"
		if t.LastUsed > 0 {
			lastUsed = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
		}
		note := t.Note
		if note == "" {
			note = "—"
		}
		out = append(out, userTokenJSON{
			Token: t.Token, Note: note, Role: roleTxt, Hits: t.Hits, Bytes: formatBytes(t.Bytes),
			Created: time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"), LastUsed: lastUsed,
		})
	}
	return out
}

// renderUserTokensJSON 返回某用户的 Token 列表（JSON，供调试 / 外部集成使用，仅安装时超管）。
// 用户管理页的弹窗不依赖它——数据已在页面渲染时内嵌（避免多一次请求失败的风险）。
func (s *Server) renderUserTokensJSON(w http.ResponseWriter, r *http.Request) {
	if !s.requireRootSuper(r) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "err": "无权访问"})
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("user"))
	if target == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "err": "缺少用户名参数"})
		return
	}
	if _, exists := s.users.Get(target); !exists {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "err": "用户不存在"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": target, "tokens": s.userTokenData(target)})
}

// userTokenRows 渲染某用户 token 的只读行（可复制，不含编辑/删除表单）。
func (s *Server) userTokenRows(tokens []*Token, owner string) string {
	var rows strings.Builder
	for _, t := range tokens {
		if !(t.Owner == owner || (t.Owner == "" && owner == s.adminUser)) {
			continue
		}
		roleTxt := "普通"
		if normRole(t.Role) == RoleAdmin {
			roleTxt = "高级（MCP）"
		}
		lastUsed := "—"
		if t.LastUsed > 0 {
			lastUsed = time.Unix(t.LastUsed, 0).Format("2006-01-02 15:04")
		}
		note := t.Note
		if note == "" {
			note = "—"
		}
		rows.WriteString(fmt.Sprintf(
			`<tr class="tok-row">`+
				`<td data-label="Token"><code>%s</code> <button type="button" class="btn outline" onclick="gmCopyText(this,this.parentNode.querySelector('code').textContent)">复制</button></td>`+
				`<td data-label="备注">%s</td>`+
				`<td data-label="类型">%s</td>`+
				`<td data-label="命中">%d</td>`+
				`<td data-label="流量">%s</td>`+
				`<td data-label="创建时间">%s</td>`+
				`<td data-label="最近使用">%s</td></tr>`,
			html.EscapeString(t.Token), html.EscapeString(note), roleTxt, t.Hits, formatBytes(t.Bytes),
			time.Unix(t.CreatedAt, 0).Format("2006-01-02 15:04"), lastUsed))
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="7" class="muted">该用户暂无 Token</td></tr>`)
	}
	return rows.String()
}

// userAddForm 新增用户表单（用户名 + 密码 + 角色）。
func (s *Server) userAddForm(csrf string) string {
	return `<form method="post" action="` + s.adminPath + `/users/add" class="row">` +
		`<input type="hidden" name="csrf" value="` + csrf + `">` +
		`<input type="text" name="username" placeholder="用户名（4-32 位字母数字 . _ -）" required>` +
		`<input type="password" name="pass" placeholder="密码（至少 4 位）" required>` +
		`<select name="role" aria-label="角色"><option value="normal">普通用户</option><option value="super">管理员（不能管理用户）</option></select>` +
		`<button type="submit" class="btn">创建用户</button></form>`
}

// doAddUser 创建用户（仅 super）。
func (s *Server) doAddUser(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("pass")
	urole := r.FormValue("role")
	if err := s.users.Add(username, pass, urole); err != nil {
		log.Printf("创建用户失败: %v", err)
		http.Redirect(w, r, s.adminPath+"/users?err=user_add_failed", http.StatusFound)
		return
	}
	log.Printf("已创建用户: %s（角色 %s）", username, normUserRole(urole))
	http.Redirect(w, r, s.adminPath+"/users?ok=user_added", http.StatusFound)
}

// doDelUser 删除用户（仅 super；不允许删除最后一个 super）。
func (s *Server) doDelUser(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	username := r.FormValue("username")
	// 安装时创建的超级管理员不可删除（否则会失去唯一能管理角色的账号）
	if username == s.adminUser {
		log.Printf("拒绝删除超级管理员: %s", username)
		http.Redirect(w, r, s.adminPath+"/users?err=user_del_failed", http.StatusFound)
		return
	}
	if err := s.users.Delete(username); err != nil {
		log.Printf("删除用户失败: %v", err)
		http.Redirect(w, r, s.adminPath+"/users?err=user_del_failed", http.StatusFound)
		return
	}
	log.Printf("已删除用户: %s", username)
	http.Redirect(w, r, s.adminPath+"/users?ok=user_deleted", http.StatusFound)
}

// doSetUserRole 修改用户角色（仅 super；不允许降级最后一个 super）。
func (s *Server) doSetUserRole(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	username := r.FormValue("username")
	// 安装时创建的超级管理员不可降级（否则「管理员」就能把超级管理员变成普通用户）
	if username == s.adminUser {
		log.Printf("拒绝修改超级管理员角色: %s", username)
		http.Redirect(w, r, s.adminPath+"/users?err=user_role_failed", http.StatusFound)
		return
	}
	newRole := r.FormValue("role")
	if err := s.users.UpdateRole(username, newRole); err != nil {
		log.Printf("修改用户角色失败: %v", err)
		http.Redirect(w, r, s.adminPath+"/users?err=user_role_failed", http.StatusFound)
		return
	}
	log.Printf("用户角色已更新: %s -> %s", username, normUserRole(newRole))
	http.Redirect(w, r, s.adminPath+"/users?ok=user_role", http.StatusFound)
}

// doResetUserPass 重置用户密码（仅 super）。
func (s *Server) doResetUserPass(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	username := r.FormValue("username")
	pass := r.FormValue("pass")
	if err := s.users.ResetPassword(username, pass); err != nil {
		log.Printf("重置密码失败: %v", err)
		http.Redirect(w, r, s.adminPath+"/users?err=user_add_failed", http.StatusFound)
		return
	}
	log.Printf("已重置用户密码: %s", username)
	http.Redirect(w, r, s.adminPath+"/users?ok=user_pass", http.StatusFound)
}

func (s *Server) consoleAside(r *http.Request) string {
	// Token 计数按当前用户可见范围（super 全量、normal 仅自己）
	_, user, role, _ := s.parseSession(r)
	base := s.entryBase(r)
	isAdmin := !s.isUserEntry(r)
	n := len(s.tokensForUser(s.store.All(), user, role))
	tokensNav := "令牌管理"
	if n > 0 {
		tokensNav = fmt.Sprintf("令牌管理（%d）", n)
	}
	// 高亮互斥：各独立子页各自高亮，其余高亮概览
	overviewActive, tokensActive, mcpActive, usersActive, svcActive, cacheActive := " active", "", "", "", "", ""
	switch {
	case strings.HasSuffix(r.URL.Path, "/tokens"):
		overviewActive, tokensActive = "", " active"
	case strings.HasSuffix(r.URL.Path, "/mcp"):
		overviewActive, mcpActive = "", " active"
	case strings.HasSuffix(r.URL.Path, "/users"):
		overviewActive, usersActive = "", " active"
	case strings.HasSuffix(r.URL.Path, "/services"):
		overviewActive, svcActive = "", " active"
	case strings.HasSuffix(r.URL.Path, "/cache"):
		overviewActive, cacheActive = "", " active"
	}
	// 超管专属导航项（用户管理 / 缓存管理 / 服务管理）：仅超管入口 + super 角色可见
	// 三项文字右侧均加盾牌符号标识超管专属，且按特权分组排在普通功能之后
	svcNav, cacheNav, usersNav := "", "", ""
	if isAdmin && role == RoleSuper {
		svcNav = `<a class="navitem` + svcActive + `" href="` + base + `/services"><span class="ic">⚙️</span>服务管理<span class="priv" title="仅管理员可见">🛡️</span></a>`
		cacheNav = `<a class="navitem` + cacheActive + `" href="` + base + `/cache"><span class="ic">💾</span>缓存管理<span class="priv" title="仅管理员可见">🛡️</span></a>`
		// 用户管理仅「安装时创建的超级管理员」可见：管理员没有改角色/删用户的入口
		if user == s.adminUser {
			usersNav = `<a class="navitem` + usersActive + `" href="` + base + `/users"><span class="ic">👥</span>用户管理<span class="priv" title="仅安装时的超级管理员可见">🛡️</span></a>`
		}
	}
	return `<aside id="gmNav">` +
		`<a class="brand" href="/" title="返回首页">` + logoSVG + `<span>gh-mirror</span></a>` +
		`<nav class="nav">` +
		`<a class="navitem` + overviewActive + `" href="` + base + `"><span class="ic">▦</span>概览</a>` +
		`<a class="navitem` + tokensActive + `" href="` + base + `/tokens"><span class="ic">🔑</span>` + tokensNav + `</a>` +
		`<a class="navitem` + mcpActive + `" href="` + base + `/mcp"><span class="ic">🤖</span>MCP 接入</a>` +
		svcNav +
		cacheNav +
		usersNav +
		`<span class="navsep"></span>` +
		`<a class="navitem" href="` + repoURL + `/blob/main/README.md" target="_blank" rel="noopener"><span class="ic">📄</span>项目文档</a>` +
		`<a class="navitem" href="` + repoURL + `" target="_blank" rel="noopener"><span class="ic">` + githubSVG + `</span>GitHub</a>` +
		`<a class="navitem" href="` + base + `/logout"><span class="ic">` + logoutSVG + `</span>退出登录</a>` +
		`</nav>` + s.asideFoot(user) + `</aside>` +
		`<button class="nav-mask" type="button" aria-label="关闭导航菜单" onclick="toggleNav()"></button>`
}

// asideFoot 渲染侧栏底部信息区：当前用户（用户名 + 欢迎词）+ ⭐ Star 引导 + 版本号 + Releases。
// 版本号由构建期注入（见 main.go 的 version），此处只做静态展示，不联网拉取最新版本。
func (s *Server) asideFoot(user string) string {
	// 当前登录用户卡片（未登录时省略）
	userBlock := ""
	if user != "" {
		userBlock = `<div class="aside-user">` +
			`<div class="un"><code>` + html.EscapeString(user) + `</code></div>` +
			`<div class="hi">👋 欢迎你！</div>` +
			`</div>`
	}
	return `<div class="aside-foot">` + userBlock +
		`<a class="star-cta" href="` + repoURL + `" target="_blank" rel="noopener" title="在 GitHub 上 Star 本项目">⭐ Star 支持一下</a>` +
		`<div class="ver">` + tagSVG + `当前版本 <code>` + html.EscapeString(version) + `</code></div>` +
		`<a class="ver-link" href="` + repoURL + `/releases" target="_blank" rel="noopener">` + pkgSVG + `查看 Releases</a>` +
		`</div>`
}

// doEditToken 修改 Token 的备注与角色（登录 + CSRF；token 值本身不可改）。
func (s *Server) doEditToken(w http.ResponseWriter, r *http.Request) {
	sid, user, userRole, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.entryBase(r)+"/tokens?err=csrf", http.StatusFound)
		return
	}
	tok := strings.TrimSpace(r.FormValue("token"))
	note := strings.TrimSpace(r.FormValue("note"))
	tokenRole := normRole(r.FormValue("role"))
	// 归属校验：normal 只能编辑自己的；无权时按不存在处理，不泄露他人 token
	if t, exists := s.store.Get(tok); !exists || !s.tokenVisible(t, user, userRole) {
		http.Redirect(w, r, s.entryBase(r)+"/tokens?err=token_missing", http.StatusFound)
		return
	}
	if !s.store.Update(tok, note, tokenRole) {
		http.Redirect(w, r, s.entryBase(r)+"/tokens?err=token_missing", http.StatusFound)
		return
	}
	log.Printf("Token 已更新: 类型=%s", tokenRole)
	http.Redirect(w, r, s.entryBase(r)+"/tokens?ok=token_updated", http.StatusFound)
}

// flashOK / flashErr 把 POST 后重定向携带的 ok / err 码翻译成可读文案。
var flashOK = map[string]string{
	"token_added":   "Token 已创建，可立即用于代理访问",
	"token_deleted":  "Token 已删除",
	"tokens_deleted": "已删除选中的 Token",
	"token_updated": "Token 已保存",
	"token_exists":  "该 Token 已存在，未做改动",
	"service_on":    "服务已开启，即时生效（已持久化）",
	"service_off":   "服务已关闭",
	"cache_saved":   "缓存设置已保存，即时生效",
	"cache_cleared": "缓存已清除",
	"user_added":    "用户已创建",
	"user_deleted":  "用户已删除",
	"user_role":     "用户角色已更新",
	"user_pass":     "用户密码已重置",
	"docker_path":   "你的 Docker 自定义路径已保存",
	"offline_deleted":    "离线缓存包已删除",
	"offline_repulling":  "已开始后台重新拉取，稍后刷新页面查看进度",
	"offline_purge_saved": "离线包自动清理设置已保存",
	"portal_saved":       "注册与邮箱设置已保存",
	"password_reset":     "密码已重置，请用新密码登录",
	"invite_added":       "自定义邀请码已添加",
	"invite_deleted":     "邀请码已删除",
	"invite_noted":       "邀请码备注已更新",
	"invite_expired":     "邀请码有效期已更新",
}

var flashErr = map[string]string{
	"csrf":            "安全校验失败（会话可能已过期），请刷新页面后重试",
	"token_missing":   "Token 不存在或已被删除",
	"bad_input":       "输入不合法，请检查后重试",
	"svc_unknown":     "未知服务，仅支持 Docker / HuggingFace",
	"svc_failed":      "服务开关调整失败，请查看日志",
	"path_invalid":    "自定义路径不合法（仅支持字母、数字、-、_、/，最长 64 字符）",
	"user_add_failed": "创建用户失败（用户名需 4-32 位字母数字 . _ -，密码至少 4 位，且用户名不可重复）",
	"user_del_failed": "不能删除最后一个超级管理员",
	"user_role_failed": "不能降级最后一个超级管理员",
	"svc_off":       "Docker 加速未开启，请联系超级管理员开启对应服务后再设置",
	"bad_code":        "验证码错误或已过期",
	"bad_captcha":     "图形验证码错误或已过期",
	"reset_super":     "超级管理员密码无法通过邮箱重置，只能由服务器后台修改",
	"reset_failed":    "重置失败，请检查邮箱是否正确",
	"reg_closed":      "注册未开放",
	"username_invalid": "用户名不合法（4-32 位字母数字 . _ -，且不能使用保留字）",
	"username_taken":  "用户名已被占用，请换一个",
	"email_taken":     "该邮箱已被注册",
	"register_failed": "注册失败，请稍后重试",
	"invite_gen_failed":   "批量生成失败（数量需在 1-10000 之间）",
	"invite_add_failed":   "添加邀请码失败（需 6-64 位字母数字且不重复）",
	"invite_invalid":      "邀请码无效、已使用或已过期",
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

// statCardTip 同 statCard，但标题旁带一个问号按钮，点击/悬停显示说明气泡。
func statCardTip(k, v, sub, tip string) string {
	return statCardClsTip(k, v, sub, "", tip)
}

// statCardCls 同 statCard，但可给数值附加样式类（如 "sm" 用于缓存目录这类长文本）。
func statCardCls(k, v, sub, cls string) string {
	return statCardClsTip(k, v, sub, cls, "")
}

// statCardClsTip 完整的统计卡渲染：数值样式类 + 说明气泡（tip 为空则不渲染问号）。
func statCardClsTip(k, v, sub, cls, tip string) string {
	vcls := "v"
	if cls != "" {
		vcls = "v " + cls
	}
	head := k
	if strings.TrimSpace(tip) != "" {
		head += ` <button type="button" class="q" aria-label="查看说明">?<span class="q-tip" role="tooltip">` +
			html.EscapeString(tip) + `</span></button>`
	}
	out := `<div class="stat"><p class="k">` + head + `</p><p class="` + vcls + `">` + v + `</p>`
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
	sid, user, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/services?err=csrf", http.StatusFound)
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	if name != "docker" && name != "hf" {
		http.Redirect(w, r, s.adminPath+"/services?err=svc_unknown", http.StatusFound)
		return
	}
	on := r.FormValue("on") == "1" || strings.EqualFold(r.FormValue("on"), "true") || strings.EqualFold(r.FormValue("on"), "on")
	// 表单里存在 dockerPath 才更新自定义路径（只有 Docker 的开关表单会带该字段）；按当前用户存储
	if name == "docker" {
		if vals, ok := r.PostForm["dockerPath"]; ok {
			p := ""
			if len(vals) > 0 {
				p = vals[0]
			}
			if err := s.users.SetDockerPath(user, p); err != nil {
				http.Redirect(w, r, s.adminPath+"/services?err=path_invalid", http.StatusFound)
				return
			}
			log.Printf("Docker 自定义路径已更新（用户 %s）: %q", user, s.userDockerPath(user))
		}
	}
	if err := s.setService(name, on); err != nil {
		http.Redirect(w, r, s.adminPath+"/services?err=svc_failed", http.StatusFound)
		return
	}
	log.Printf("服务开关已调整: %s -> %v", name, on)
	code := "service_off"
	if on {
		code = "service_on"
	}
	http.Redirect(w, r, s.adminPath+"/services?ok="+code, http.StatusFound)
}

// doSetUserDockerPath 设置当前用户自己的 Docker 自定义路径（Docker 服务开启才可设，否则提示联系超管）。
func (s *Server) doSetUserDockerPath(w http.ResponseWriter, r *http.Request) {
	sid, user, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.entryBase(r), http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.entryBase(r)+"?err=csrf", http.StatusFound)
		return
	}
	// 门控：Docker 加速开启才可设置自己的路径
	if !s.dockerEnabled() {
		http.Redirect(w, r, s.entryBase(r)+"?err=svc_off", http.StatusFound)
		return
	}
	p := strings.TrimSpace(r.FormValue("dockerPath"))
	if err := s.users.SetDockerPath(user, p); err != nil {
		http.Redirect(w, r, s.entryBase(r)+"?err=path_invalid", http.StatusFound)
		return
	}
	log.Printf("用户 %s 已更新自己的 Docker 自定义路径: %q", user, s.userDockerPath(user))
	http.Redirect(w, r, s.entryBase(r)+"?ok=docker_path", http.StatusFound)
}

// imageLatest 把镜像名（含或不含 tag）替换为 latest tag。
func imageLatest(image string) string {
	_, repo, _ := parseImageRef(image)
	return repo + ":latest"
}

// doOfflineDel 删除单个离线缓存包（仅超管）。
func (s *Server) doOfflineDel(w http.ResponseWriter, r *http.Request) {
	sid, _, role, ok := s.parseSession(r)
	if !ok || role != RoleSuper {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	if digest := strings.TrimSpace(r.FormValue("digest")); digest != "" {
		_ = s.offlineCache.Remove(digest)
		log.Printf("离线缓存包已删除: %s", digest)
	}
	http.Redirect(w, r, s.adminPath+"/cache?ok=offline_deleted", http.StatusFound)
}

// doOfflineBatchDel 批量删除离线缓存包（仅超管；多选）。
func (s *Server) doOfflineBatchDel(w http.ResponseWriter, r *http.Request) {
	sid, _, role, ok := s.parseSession(r)
	if !ok || role != RoleSuper {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	for _, d := range r.PostForm["digest"] {
		if d = strings.TrimSpace(d); d != "" {
			_ = s.offlineCache.Remove(d)
		}
	}
	log.Printf("离线缓存包批量删除: %d 个", len(r.PostForm["digest"]))
	http.Redirect(w, r, s.adminPath+"/cache?ok=offline_deleted", http.StatusFound)
}

// doOfflineRepull 后台重新拉取指定版本（仅超管）。
func (s *Server) doOfflineRepull(w http.ResponseWriter, r *http.Request) {
	sid, _, role, ok := s.parseSession(r)
	if !ok || role != RoleSuper {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	image := strings.TrimSpace(r.FormValue("image"))
	if image == "" {
		http.Redirect(w, r, s.adminPath+"/cache?err=bad_input", http.StatusFound)
		return
	}
	s.repullOffline(image)
	http.Redirect(w, r, s.adminPath+"/cache?ok=offline_repulling", http.StatusFound)
}

// doOfflineRepullLatest 后台拉取最新版本（仅超管；强制 latest tag）。
func (s *Server) doOfflineRepullLatest(w http.ResponseWriter, r *http.Request) {
	sid, _, role, ok := s.parseSession(r)
	if !ok || role != RoleSuper {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	image := strings.TrimSpace(r.FormValue("image"))
	if image == "" {
		http.Redirect(w, r, s.adminPath+"/cache?err=bad_input", http.StatusFound)
		return
	}
	s.repullOffline(imageLatest(image))
	http.Redirect(w, r, s.adminPath+"/cache?ok=offline_repulling", http.StatusFound)
}

// doOfflineAutoPurge 保存离线包磁盘满自动清理开关与阈值（仅超管）。
func (s *Server) doOfflineAutoPurge(w http.ResponseWriter, r *http.Request) {
	sid, _, role, ok := s.parseSession(r)
	if !ok || role != RoleSuper {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	autoPurge := r.FormValue("autoPurge") == "1" || strings.EqualFold(r.FormValue("autoPurge"), "true") || strings.EqualFold(r.FormValue("autoPurge"), "on")
	purgeFreeGB := 0
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("purgeFreeGB"))); err == nil {
		purgeFreeGB = v
	}
	maxFileMB := 0
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("maxFileMB"))); err == nil {
		maxFileMB = v
	}
	s.setOfflinePurge(autoPurge, purgeFreeGB, maxFileMB)
	log.Printf("离线包缓存设置: autoPurge=%v purgeFreeGB=%d maxFileMB=%d", autoPurge, purgeFreeGB, maxFileMB)
	http.Redirect(w, r, s.adminPath+"/cache?ok=offline_purge_saved", http.StatusFound)
}

// formBool 读取布尔表单值：checkbox 的 "1" / "true" / "on" 均视为真。
func formBool(r *http.Request, name string) bool {
	v := r.FormValue(name)
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
}

// doSetPortalSettings 保存「用户注册与邮箱」设置（仅超管）。
// 页面上是两个独立表单（注册开关区 / SMTP 发信区）共用本端点，以隐藏域 scope 区分：
// 只更新提交来源对应的那一组，未提交的组保持现值——否则保存注册开关会把 SMTP 清空、
// 保存邮箱设置会把注册开关与联系邮箱重置（历史缺陷：两个表单全量覆盖互相吃掉对方）。
func (s *Server) doSetPortalSettings(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	scope := r.FormValue("scope")
	// scope 缺省时（旧版页面 / 手工 POST）按字段存在性推断来源，保持兼容
	portalForm := scope == "portal" || scope == "all" || (scope == "" && r.PostForm.Has("contactEmail"))
	smtpForm := scope == "smtp" || scope == "all" || (scope == "" && r.PostForm.Has("smtpHost"))

	oldSMTP := s.smtpSettings()
	cfg := oldSMTP
	if smtpForm {
		port := 0
		if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smtpPort"))); err == nil {
			port = v
		}
		cfg = smtpConfig{
			Host:     strings.TrimSpace(r.FormValue("smtpHost")),
			Port:     port,
			Username: strings.TrimSpace(r.FormValue("smtpUser")),
			Password: r.FormValue("smtpPass"),
			From:     strings.TrimSpace(r.FormValue("smtpFrom")),
			TLS:      r.FormValue("smtpTLS") == "1",
		}
		// 密码留空 = 保持不变（页面已不回显密码，防止重复提交时把已存凭据清掉）
		if cfg.Password == "" {
			cfg.Password = oldSMTP.Password
		}
	}
	regMode, contactEmail := s.regMode(), s.contactEmail()
	userCaptcha, adminCaptcha := s.userCaptchaEnabled(), s.adminCaptchaEnabled()
	hideLogin := s.hideLoginEnabled()
	if portalForm {
		regMode = normRegMode(r.FormValue("regMode"))
		contactEmail = r.FormValue("contactEmail")
		userCaptcha = formBool(r, "userCaptcha")
		adminCaptcha = formBool(r, "adminCaptcha")
		hideLogin = formBool(r, "hideLogin")
	}
	s.setPortalSettings(cfg, regMode, contactEmail, userCaptcha, adminCaptcha, hideLogin)
	log.Printf("门户设置已保存(scope=%s): regMode=%s userCaptcha=%v adminCaptcha=%v contactEmail=%q smtp=%s:%d tls=%v",
		scope, regMode, userCaptcha, adminCaptcha, contactEmail, cfg.Host, cfg.Port, cfg.TLS)
	http.Redirect(w, r, s.adminPath+"/users?ok=portal_saved", http.StatusFound)
}

// daysToExpire 把「有效期天数」转 unix 时间戳（<=0 返回 0 = 永久）。
func daysToExpire(days int) int64 {
	if days <= 0 {
		return 0
	}
	return time.Now().AddDate(0, 0, days).Unix()
}

// doInviteGenerate 批量生成邀请码并直接下载（按格式导出），仅超管。
func (s *Server) doInviteGenerate(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	count := 10
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("count"))); err == nil && v > 0 && v <= 10000 {
		count = v
	}
	note := strings.TrimSpace(r.FormValue("note"))
	days := 0
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("days"))); err == nil {
		days = v
	}
	format := strings.TrimSpace(r.FormValue("format"))
	if format == "" {
		format = "{code}"
	}
	codes, err := s.invites.Generate(count, note, daysToExpire(days))
	if err != nil {
		http.Redirect(w, r, s.adminPath+"/users?err=invite_gen_failed", http.StatusFound)
		return
	}
	log.Printf("批量生成邀请码 %d 条（备注=%q 有效期=%d天）", count, note, days)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="invite-codes.txt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(s.invites.Export(codes, format)))
}

// doInviteAddCustom 添加自定义邀请码，仅超管。
func (s *Server) doInviteAddCustom(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	note := strings.TrimSpace(r.FormValue("note"))
	days := 0
	if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("days"))); err == nil {
		days = v
	}
	if err := s.invites.AddCustom(code, note, daysToExpire(days)); err != nil {
		http.Redirect(w, r, s.adminPath+"/users?err=invite_add_failed", http.StatusFound)
		return
	}
	log.Printf("已添加自定义邀请码: %s", code)
	http.Redirect(w, r, s.adminPath+"/users?ok=invite_added", http.StatusFound)
}

// doInviteBatch 批量操作（delete/note/expire），仅超管。
func (s *Server) doInviteBatch(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	op := r.FormValue("op")
	codes := r.PostForm["code"]
	switch op {
	case "delete":
		s.invites.Delete(codes)
		http.Redirect(w, r, s.adminPath+"/users?ok=invite_deleted", http.StatusFound)
	case "note":
		s.invites.SetNote(codes, strings.TrimSpace(r.FormValue("note")))
		http.Redirect(w, r, s.adminPath+"/users?ok=invite_noted", http.StatusFound)
	case "expire":
		days := 0
		if v, err := strconv.Atoi(strings.TrimSpace(r.FormValue("days"))); err == nil {
			days = v
		}
		s.invites.SetExpire(codes, daysToExpire(days))
		http.Redirect(w, r, s.adminPath+"/users?ok=invite_expired", http.StatusFound)
	default:
		http.Redirect(w, r, s.adminPath+"/users?err=bad_input", http.StatusFound)
	}
}

// doInviteExport 导出邀请码（未指定 code 时导出全部；可选批量打备注），仅超管。
func (s *Server) doInviteExport(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok || !s.requireRootSuper(r) {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/users?err=csrf", http.StatusFound)
		return
	}
	format := strings.TrimSpace(r.FormValue("format"))
	if format == "" {
		format = "{code}"
	}
	note := strings.TrimSpace(r.FormValue("note"))
	codes := r.PostForm["code"]
	if len(codes) == 0 {
		for _, inv := range s.invites.List() {
			codes = append(codes, inv.Code)
		}
	}
	if note != "" {
		s.invites.SetNote(codes, note)
	}
	out := s.invites.Export(codes, format)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="invite-codes.txt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

// jsonResp 通用 JSON 响应（验证码发送等 AJAX 接口）。
type jsonResp struct {
	OK  bool   `json:"ok"`
	Err string `json:"err,omitempty"`
}

// doSendCode 发送邮箱验证码（注册 register / 找回密码 reset；未登录可访问，受 SMTP 与开关门控）。
func (s *Server) doSendCode(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	purpose := r.FormValue("purpose")
	cfg := s.smtpSettings()
	if !cfg.ready() {
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "SMTP 未配置，请联系超级管理员"})
		return
	}
	if !validEmail(email) {
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "邮箱格式不正确"})
		return
	}
	// 注册发码要求图形验证码（防刷）：前端在首次点击「发送验证码」时展开图形码行
	if purpose == "register" && s.userCaptchaEnabled() &&
		!s.captcha.Verify(r.FormValue("captchaId"), r.FormValue("captcha")) {
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "图形验证码错误或已过期，请重新输入"})
		return
	}
	if purpose == "register" && !s.regEnabled() {
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "注册未开放"})
		return
	}
	if purpose == "reset" {
		u, ok := s.users.ByEmail(email)
		if !ok || u.Role == RoleSuper {
			// 不泄露邮箱是否已注册
			s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "该邮箱无法自助找回，请联系超级管理员"})
			return
		}
	}
	code, ok := s.codes.Issue(purpose + ":" + email)
	if !ok {
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "发送过于频繁，请 60 秒后再试"})
		return
	}
	subject := "gh-mirror 验证码"
	body := "您的验证码是 " + code + "，10 分钟内有效。若非本人操作请忽略本邮件。"
	if err := sendMail(cfg, email, subject, body); err != nil {
		log.Printf("发送验证码邮件失败（%s）: %v", email, err)
		s.writeJSON(w, http.StatusOK, jsonResp{OK: false, Err: "邮件发送失败，请稍后再试或联系超级管理员"})
		return
	}
	s.writeJSON(w, http.StatusOK, jsonResp{OK: true})
}

// doRegister 处理用户自主注册（受注册开关与 SMTP 门控；验证码校验后创建 normal 账号并直接登录）。
func (s *Server) doRegister(w http.ResponseWriter, r *http.Request) {
	if !s.regEnabled() {
		http.Redirect(w, r, s.userPath+"/register?err=reg_closed", http.StatusFound)
		return
	}
	r.ParseForm()
	if s.userCaptchaEnabled() && !s.captcha.Verify(r.FormValue("captchaId"), r.FormValue("captcha")) {
		http.Redirect(w, r, s.userPath+"/register?err=bad_captcha", http.StatusFound)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	code := strings.TrimSpace(r.FormValue("code"))
	pass := r.FormValue("pass")
	if !s.codes.Verify("register:"+email, code) {
		http.Redirect(w, r, s.userPath+"/register?err=bad_code", http.StatusFound)
		return
	}
	if err := s.users.Register(username, email, pass); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "保留字"), strings.Contains(msg, "不合法"):
			http.Redirect(w, r, s.userPath+"/register?err=username_invalid", http.StatusFound)
		case strings.Contains(msg, "用户名已存在"):
			http.Redirect(w, r, s.userPath+"/register?err=username_taken", http.StatusFound)
		case strings.Contains(msg, "邮箱已被注册"):
			http.Redirect(w, r, s.userPath+"/register?err=email_taken", http.StatusFound)
		default:
			http.Redirect(w, r, s.userPath+"/register?err=register_failed", http.StatusFound)
		}
		return
	}
	// 仅邀请码模式：注册后消耗邀请码（失败则回滚删除刚创建的用户）
	if s.inviteOnlyEnabled() {
		if !s.invites.Claim(r.FormValue("invite"), username) {
			_ = s.users.Delete(username)
			http.Redirect(w, r, s.userPath+"/register?err=invite_invalid", http.StatusFound)
			return
		}
	}
	s.setSessionCookie(w, userSessionCookie, username)
	log.Printf("用户注册成功: %s (%s)", username, email)
	http.Redirect(w, r, s.userPath, http.StatusFound)
}

// doResetPassword 处理邮箱验证码重置密码（仅普通用户；super 账号拒绝）。
func (s *Server) doResetPassword(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	code := strings.TrimSpace(r.FormValue("code"))
	pass := r.FormValue("pass")
	if !s.codes.Verify("reset:"+email, code) {
		http.Redirect(w, r, s.userPath+"?err=bad_code", http.StatusFound)
		return
	}
	u, ok := s.users.ByEmail(email)
	if !ok {
		http.Redirect(w, r, s.userPath+"?err=reset_failed", http.StatusFound)
		return
	}
	if u.Role == RoleSuper {
		http.Redirect(w, r, s.userPath+"?err=reset_super", http.StatusFound)
		return
	}
	if err := s.users.ResetPassword(u.Username, pass); err != nil {
		http.Redirect(w, r, s.userPath+"?err=reset_failed", http.StatusFound)
		return
	}
	log.Printf("用户通过邮箱重置密码: %s", u.Username)
	http.Redirect(w, r, s.userPath+"?ok=password_reset", http.StatusFound)
}

// doSetCache 运行时调整缓存参数（单文件上限 / 总上限 / 时效），即时生效并持久化。
func (s *Server) doSetCache(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	maxFileMB := atoiOr(r.FormValue("maxFileMB"), -1)
	maxTotalMB := atoiOr(r.FormValue("maxTotalMB"), -1)
	ttlMinutes := atoiOr(r.FormValue("ttlMinutes"), -1)
	if maxFileMB < -1 || maxTotalMB < -1 || ttlMinutes < -1 {
		http.Redirect(w, r, s.adminPath+"/cache?err=bad_input", http.StatusFound)
		return
	}
	_ = s.setCacheLimits(maxFileMB, maxTotalMB, ttlMinutes)
	log.Printf("缓存参数已调整: 单文件=%dMB 总计=%dMB 时效=%d分钟",
		int(s.cache.MaxBytes()/(1024*1024)), int(s.cache.MaxTotalBytes()/(1024*1024)), int(s.cache.TTL()/time.Minute))
	http.Redirect(w, r, s.adminPath+"/cache?ok=cache_saved", http.StatusFound)
}

// doClearCache 清空缓存目录内的所有文件（不影响 Token 与设置）。
func (s *Server) doClearCache(w http.ResponseWriter, r *http.Request) {
	sid, _, _, ok := s.parseSession(r)
	if !ok {
		http.Redirect(w, r, s.adminPath, http.StatusFound)
		return
	}
	r.ParseForm()
	if !s.validCSRF(sid, r.FormValue("csrf")) {
		http.Redirect(w, r, s.adminPath+"/cache?err=csrf", http.StatusFound)
		return
	}
	files, bytes := s.cache.Clear()
	log.Printf("缓存已清除: %d 个文件，%s", files, formatBytes(bytes))
	http.Redirect(w, r, s.adminPath+"/cache?ok=cache_cleared", http.StatusFound)
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
	// 当前用户用于离线包路径的用户名段（多用户隔离）
	_, user, role, ok := s.parseSession(r)
	if !ok {
		s.writeJSON(w, http.StatusUnauthorized, convertResp{OK: false, Hint: "未登录或会话已过期"})
		return
	}
	// 只允许使用自己的 token（管理员 / 超管也不能借用他人 token 生成链接）
	own := s.tokensForUser(s.store.All(), user, role)
	tk := ""
	if req := strings.TrimSpace(r.FormValue("token")); req != "" {
		for _, t := range own {
			if t.Token == req {
				tk = req
				break
			}
		}
	}
	if tk == "" && len(own) > 0 {
		tk = own[0].Token
	}
	res := s.buildAccelURL(baseOf(r), raw, tk, user)
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
	reLooseHost   = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9.-]*\.[a-zA-Z]{2,})(/.*)?$`)
	reHFBlob      = regexp.MustCompile(`^([^/]+/[^/]+)/blob/(.+)$`)
	reDockerImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*(?:/[a-zA-Z0-9._-]+)*(?::[A-Za-z0-9_.-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`)
)

// buildAccelURL 把用户粘贴的原始链接 / Docker 镜像名转换为本站加速链接。
// user 为归属用户名：GitHub / HuggingFace / Docker 离线包路径都带用户名段
// （/g/<用户名>/<token>/、/hf/<用户名>/<token>/、/offline/<用户名>/<token>/<镜像>，多用户隔离）。
// 这是后台「链接转换」与 MCP convert_link 的唯一实现（一个实现、两个消费者）。
// base 为当前访问基地址，tk 为要嵌入的 token（HF / GitHub 需要）。
func (s *Server) buildAccelURL(base, raw, tk, user string) accelResult {
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
			return accelResult{Kind: "github", URL: base + "/g/" + dockerSegment(user) + "/" + tk + "/" + raw}
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
	// 从浏览器地址栏复制的链接常常省略 https://（如 github.com/owner/repo/...），
	// 而后台说明里就写着支持这种写法。若不在此补认，这类输入会落到下面的
	// Docker 镜像判断里，生成完全错误的 /offline/ 路径。
	// 只补认「已知主机名」：docker.io/library/nginx、ghcr.io/owner/app 含点但不是
	// GitHub / HuggingFace 域，仍按 Docker 镜像处理。
	if m := reLooseHost.FindStringSubmatch(raw); m != nil {
		host := strings.ToLower(m[1])
		if isGitHubHost(host) || isHFHost(host) {
			return s.buildAccelURL(base, "https://"+raw, tk, user)
		}
	}
	if reDockerImage.MatchString(raw) {
		if !s.dockerEnabled() {
			return accelResult{Kind: "docker", Hint: "Docker 加速未开启，请先在「服务管理」开启"}
		}
		if tk == "" {
			return accelResult{Kind: "docker", Hint: "离线 tar 需要 token，请先在「Token 管理」添加一个 token"}
		}
		// 离线包地址必须带用户名段与 token（/offline/<用户名>/<token>/<镜像>，多用户隔离）
		return accelResult{Kind: "docker", URL: base + "/offline/" + dockerSegment(user) + "/" + tk + "/" + raw}
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
