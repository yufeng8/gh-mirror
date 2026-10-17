package main

import (
	_ "embed" // 启用 //go:embed（docs/agent-prompt.md）
	"html"
	"os"
	"path/filepath"
	"strings"
)

// repoURL 是本项目在 GitHub 上的地址，用于首页 Stars / Fork 引导与页脚。
const repoURL = "https://github.com/yufeng8/gh-mirror"

// logoSVG 是项目 Logo（内联 SVG，便于零额外请求渲染，契合私有化部署）。
// 注意：logoSVG 会被拼进 loginBody/dashboardBody 这两个 fmt.Sprintf 的格式串，
// 其中绝不能出现字面 %（渐变坐标用 0~1 小数而非 0%~100%），否则触发 %!s(MISSING)。
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="gh-mirror logo">
  <defs>
    <linearGradient id="gm" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#22c55e"/>
      <stop offset="1" stop-color="#0ea5e9"/>
    </linearGradient>
  </defs>
  <rect x="4" y="4" width="56" height="56" rx="14" fill="url(#gm)"/>
  <g fill="none" stroke="#ffffff" stroke-width="5" stroke-linecap="round">
    <path d="M21 32 A11 11 0 0 1 43 32"/>
    <path d="M43 32 A11 11 0 0 1 21 32"/>
  </g>
  <path d="M43 32 L37 30 L41 36 Z" fill="#ffffff"/>
  <path d="M21 32 L27 34 L23 28 Z" fill="#ffffff"/>
</svg>`

// dockerSVG / tagSVG / pkgSVG 为小尺寸内联图标（零外部请求）：
// Docker 鲸鱼（简化版：容器方块 + 船体 + 浪花）、版本标签、发布包。
// 容器方块按官方排布（上 2 / 中 3 / 下 5 的阶梯），下方为不对称鲸身（左宽右收）+ 右侧尾鳍，
// 用官方品牌蓝以增强 1× 尺寸下的辨识度。
const dockerSVG = `<svg viewBox="0 0 26.6 19" width="20" height="20" fill="#2496ED" aria-hidden="true"><rect x="11.24" y="2.9" width="2.33" height="2.33" rx=".4"/><rect x="14.45" y="2.9" width="2.33" height="2.33" rx=".4"/><rect x="8.03" y="6.11" width="2.33" height="2.33" rx=".4"/><rect x="11.24" y="6.11" width="2.33" height="2.33" rx=".4"/><rect x="14.45" y="6.11" width="2.33" height="2.33" rx=".4"/><rect x="4.82" y="9.32" width="2.33" height="2.33" rx=".4"/><rect x="8.03" y="9.32" width="2.33" height="2.33" rx=".4"/><rect x="11.24" y="9.32" width="2.33" height="2.33" rx=".4"/><rect x="14.45" y="9.32" width="2.33" height="2.33" rx=".4"/><rect x="17.66" y="9.32" width="2.33" height="2.33" rx=".4"/><path d="M19.6 11.9c0 4.3-3.3 6.9-7.5 6.9H7.2C3.9 18.8 1.1 16.4.2 13.4c-.1-.4.2-.8.7-.8h18.7z"/><path d="M19.6 12.1c1.2-1 2.6-1.6 4.1-1.5 1.4.1 2.4 1 2.5 2.1-.9 1.5-2.6 2.3-4.3 2.1l-2.3-.2z"/></svg>`

const tagSVG = `<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true"><path d="M2 7.2V3.4A1.4 1.4 0 0 1 3.4 2h3.8l6.4 6.4a1.4 1.4 0 0 1 0 2l-2.8 2.8a1.4 1.4 0 0 1-2 0z"/><circle cx="5.4" cy="5.4" r="1.1" fill="currentColor" stroke="none"/></svg>`

const pkgSVG = `<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true"><path d="M8 1.8 14 5v6L8 14.2 2 11V5z"/><path d="M2 5l6 3.2L14 5M8 8.2v6"/></svg>`

// homeSVG 是「返回首页」用的房子图标。
const homeSVG = `<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true"><path d="M2.6 7.1 8 2.6l5.4 4.5v5.6a.9.9 0 0 1-.9.9H3.5a.9.9 0 0 1-.9-.9z"/><path d="M6.5 13.6V9.7h3v3.9"/></svg>`

// 平台标签：logo + 短代号（图形负责形象、文字负责零歧义，且不像全名那样占宽）。
const tagGH = `<span class="tag tag-logo" title="GitHub">` + githubSVG + `<span class="tag-txt">GH</span></span>`

const tagDK = `<span class="tag tag-logo" title="Docker">` + dockerSVG + `<span class="tag-txt">DK</span></span>`

const tagHF = `<span class="tag tag-logo" title="HuggingFace"><span class="emo">🤗</span><span class="tag-txt">HF</span></span>`

// svcTagHTML 把服务代号渲染成平台 logo 标签（其它代号回退为文字标签）。
func svcTagHTML(code string) string {
	switch code {
	case "GH":
		return tagGH
	case "DK":
		return tagDK
	case "HF":
		return tagHF
	}
	return `<span class="tag">` + code + `</span>`
}

// dockerModalHTML 生成开启 Docker 加速前的安全确认弹窗（原先常驻后台顶部，改为按需弹出）。
// cur 是当前已保存的自定义路径（/v2 前缀），用于回填输入框；确认按钮由 gmOpenModal 决定是否显示。
func dockerModalHTML(cur, owner string) string {
	return `
<div class="modal" id="gmDockerModal" hidden>
  <div class="modal-mask" onclick="gmCloseModal()"></div>
  <div class="modal-box" role="dialog" aria-modal="true" aria-labelledby="gmDockerModalTitle">
    <div class="modal-head"><span class="badge warn">⚠️ 安全说明</span><h3 id="gmDockerModalTitle">开启 Docker 加速前请先确认风险</h3></div>
    <div class="modal-body">
      <div class="modal-field">
        <label for="gmDockerPath">拉取路径（用户名段固定，自定义路径可选）</label>
        <div class="path-edit">
          <span class="path-fixed" title="当前登录用户名，不可省略">` + html.EscapeString(owner) + `</span>
          <span class="path-sep">/</span>
          <input id="gmDockerPath" name="dockerPath" form="gmSvcForm-docker" value="` + html.EscapeString(cur) + `" data-owner="` + html.EscapeString(owner) + `" data-saved="` + html.EscapeString(cur) + `" placeholder="可选，如 team-a / 9f3k2" maxlength="64" autocomplete="off" spellcheck="false" oninput="gmDockerPathPreview(this.value)">
        </div>
        <p class="muted small" style="margin-top:8px">拉取路径固定为 <code>/v2/&lt;用户名&gt;/&lt;自定义路径&gt;/…</code>：<strong>用户名段（<code>` + html.EscapeString(owner) + `</code>）强制带上、不能省略</strong>，自定义路径留空则只用用户名段。这样每个用户天然有独立路径，也方便后续扩展多用户。</p>
        <p class="muted small">当前预览：<code id="gmDockerPathPreview">docker pull &lt;本服务域名&gt;/` + html.EscapeString(owner) + `/library/nginx:1.25</code></p>
        <p class="muted small">自定义路径仅支持字母、数字、<code>-</code>、<code>_</code>、<code>/</code>，最长 64 字符。<strong>注意：这种方式下 <code>registry-mirrors</code> 不再生效</strong>，必须按上面的带路径方式拉取。</p>
      </div>
      端点 <code>/v2</code> <strong>不使用 Token 鉴权</strong>（Docker registry 协议无法携带 Token），<code>&lt;用户名&gt;/&lt;自定义路径&gt;</code> 就是给它的「秘密前缀」式保护；<code>/offline</code> 离线包<strong>必须带 Token</strong>（<code>/offline/&lt;token&gt;/&lt;镜像名&gt;</code>，缺失或无效一律 404）。
      <ul>
        <li>若网关经<strong>公网 / CDN 暴露</strong>，任何人都能使用你的镜像加速、拉取离线 tar，<strong>耗费上游带宽</strong>。</li>
        <li>置于 CDN 后：<code>[acl] ip_allow</code> <strong>无法按真实客户端 IP 限制</strong>（源站只看到 CDN 回源 IP，X-Forwarded-For 可伪造）。</li>
      </ul>
      <p style="margin:12px 0 0"><strong>降低风险（任选其一即可）：</strong></p>
      <ol>
        <li>不需要公网 Docker 加速：保持关闭，或设 <code>DOCKER_ENABLED=false</code>。</li>
        <li>公网反代挡掉裸路径：<code>location ^~ /v2/ { return 404; }</code>；<code>/offline</code> 已强制 token 鉴权，可再叠加 IP 白名单或速率限制。</li>
        <li>registry 走<strong>不走 CDN 的难猜子域名</strong>直连源站，并在云安全组按来源 IP 放行。</li>
        <li>在 CDN（如 EdgeOne）对 <code>/v2</code>、<code>/offline</code> 配速率限制与带宽封顶（CDN 看得到真实客户端 IP）。</li>
      </ol>
      <p style="margin:12px 0 0">详见 <code>docs/docker-deploy.md</code> 的「Docker 加速安全」一节。</p>
    </div>
    <div class="modal-foot">
      <button type="button" class="btn outline" data-modal-cancel onclick="gmCloseModal()">取消</button>
      <button type="button" class="btn" data-modal-ok onclick="gmConfirmModal()">我已知晓，确认开启</button>
    </div>
  </div>
</div>`
}

// logoutSVG 是「退出登录」用的图标（门 + 外出的箭头，比符号 ⎋ 更直观）。
const logoutSVG = `<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6.5 2H4.2A1.7 1.7 0 0 0 2.5 3.7v8.6A1.7 1.7 0 0 0 4.2 14h2.3"/><path d="M10.6 11.2 13.8 8l-3.2-3.2"/><path d="M13.8 8H6.2"/></svg>`

// logoSVGAlt 是 logoSVG 的第二份副本，唯一差别是渐变 id（gm2）。
// 为什么必须存在：同一页面会出现两个 logo，若二者复用同一个 <linearGradient id="gm"> 就形成重复 ID；
// 当其中一个所在容器被 display:none（如登录页 ≤860px 隐藏左侧品牌区）时，另一个的
// fill="url(#gm)" 会解析到隐藏子树里的 paint server，导致该 logo 整体不渲染（移动端"看不到 logo"）。
// 因此同页第二处 logo 一律使用本副本。注意事项与 logoSVG 相同：不得出现字面 %。
const logoSVGAlt = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="gh-mirror logo">
  <defs>
    <linearGradient id="gm2" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#22c55e"/>
      <stop offset="1" stop-color="#0ea5e9"/>
    </linearGradient>
  </defs>
  <rect x="4" y="4" width="56" height="56" rx="14" fill="url(#gm2)"/>
  <g fill="none" stroke="#ffffff" stroke-width="5" stroke-linecap="round">
    <path d="M21 32 A11 11 0 0 1 43 32"/>
    <path d="M43 32 A11 11 0 0 1 21 32"/>
  </g>
  <path d="M43 32 L37 30 L41 36 Z" fill="#ffffff"/>
  <path d="M21 32 L27 34 L23 28 Z" fill="#ffffff"/>
</svg>`

// githubSVG 是 GitHub 标志（内联，用于顶栏/引导的明显位置）。
const githubSVG = `<svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-2.91-.88-2.91-2.82 0-.63.22-1.15.58-1.55-.06-.14-.25-.71.06-1.48 0 0 .58-.18 1.9.7a6.6 6.6 0 0 1 1.74-.23c.59 0 1.18.08 1.74.23 1.32-.89 1.9-.7 1.9-.7.31.77.12 1.34.06 1.48.36.4.58.92.58 1.55 0 1.95-1.14 2.62-2.92 2.82.3.26.56.76.56 1.54 0 1.11-.01 2.01-.01 2.29 0 .21.15.46.55.38A7.99 7.99 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>`

// faviconHref 是内联 SVG data-URI favicon（品牌标志，紧凑版），零额外文件/请求。
// '#' 必须编码为 %23，否则会被当作 fragment 分隔符。
const faviconHref = "data:image/svg+xml," +
	"<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'>" +
	"<defs><linearGradient id='g' x1='0' y1='0' x2='1' y2='1'>" +
	"<stop offset='0' stop-color='%2322c55e'/><stop offset='1' stop-color='%230ea5e9'/></linearGradient></defs>" +
	"<rect x='4' y='4' width='56' height='56' rx='14' fill='url(%23g)'/>" +
	"<g fill='none' stroke='%23ffffff' stroke-width='5' stroke-linecap='round'>" +
	"<path d='M21 32A11 11 0 0 1 43 32'/><path d='M43 32A11 11 0 0 1 21 32'/></g>" +
	"<path d='M43 32L37 30L41 36Z' fill='%23ffffff'/><path d='M21 32L27 34L23 28Z' fill='%23ffffff'/></svg>"

// pageStyle 复刻 shadcn 设计系统并扩展：CSS 变量主题、圆角卡片、渐变 Hero、
// 终端拟物窗口、Console 侧栏、分屏登录、统计卡、服务徽标、focus-visible、
// 移动优先响应式、深色模式。全部内联、无外部 CDN。
// 注意：pageStyle 位于 <head>、经字符串拼接 + w.Write 输出，不做 fmt 格式化，
// 其中的 %（如 width:100%）安全；只有 loginBody/dashboardBody 这类被
// fmt.Sprintf 的 body 片段才必须避免字面 %。
const pageStyle = `
<style>
  :root{
    --background:#ffffff; --foreground:#0a0a0a; --card:#ffffff;
    --muted:#f4f4f5; --muted-foreground:#71717a; --border:#e4e4e7;
    --primary:#16a34a; --primary-foreground:#ffffff; --ring:#16a34a;
    --danger:#dc2626; --danger-foreground:#ffffff; --radius:.75rem;
    --glass:rgba(255,255,255,.72);
  }
  html.dark{
    --background:#0a0a0a; --foreground:#ededed; --card:#18181b;
    --muted:#27272a; --muted-foreground:#a1a1aa; --border:#3f3f46;
    --primary:#22c55e; --primary-foreground:#052e16; --ring:#22c55e;
    --glass:rgba(10,10,10,.6);
  }
  *{box-sizing:border-box}
  html{-webkit-text-size-adjust:100%;scroll-behavior:smooth}
  body{margin:0;color:var(--foreground);background:var(--background);line-height:1.7;
    font-family:ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,"PingFang SC","Microsoft YaHei",sans-serif;
    -webkit-font-smoothing:antialiased;transition:background .2s ease,color .2s ease}
  .container{max-width:1000px;margin:0 auto;padding:0 20px 64px}
  .logo{width:46px;height:46px;flex:0 0 auto;border-radius:12px;overflow:hidden;box-shadow:0 1px 2px rgba(0,0,0,.08)}
  .mark{width:clamp(76px,7vw,108px);height:clamp(76px,7vw,108px);flex:0 0 auto;border-radius:18px;overflow:hidden;box-shadow:0 12px 30px rgba(22,163,74,.26)}
  .mark>svg,.logo>svg{display:block;width:100%;height:100%}
  /* 品牌 Logo：markup 中是裸 <svg>，必须用直接子选择器分场景显式设定宽高，
     否则 .brand 无自身宽高时 svg 会无约束放大（曾导致三页 Logo 过大）。 */
  .brand{text-decoration:none;color:var(--foreground)}
  .brand:hover{text-decoration:none}
  .brand>svg{display:block;flex:0 0 auto;width:30px;height:30px;border-radius:9px}
  .topnav .brand>svg{width:30px;height:30px}
  .auth .brand>svg{width:40px;height:40px;border-radius:11px}
  .auth .box .brand>svg{width:34px;height:34px;border-radius:10px}
  .console aside .brand>svg{width:30px;height:30px}
  .console .topbar .brand>svg{width:32px;height:32px}
  .muted{color:var(--muted-foreground)}
  code{background:var(--muted);padding:2px 6px;border-radius:6px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.86rem;word-break:normal;overflow-wrap:break-word}
  pre{background:#0b1020;color:#e6edf3;padding:14px 16px;border-radius:var(--radius);overflow:auto;font-size:.85rem;line-height:1.55;margin:0}
  pre code{background:transparent;color:inherit;padding:0}
  a{color:var(--primary);text-decoration:none}
  a:hover{text-decoration:underline}
  .btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;padding:9px 16px;border-radius:var(--radius);background:var(--primary);color:var(--primary-foreground);font-size:.95rem;font-weight:600;border:1px solid transparent;cursor:pointer;transition:filter .15s,transform .15s}
  .btn:hover{filter:brightness(1.03);text-decoration:none;transform:translateY(-1px)}
  .btn.outline{background:transparent;color:var(--foreground);border-color:var(--border)}
  .btn:focus-visible,button:focus-visible,input:focus-visible,a:focus-visible,summary:focus-visible{outline:2px solid var(--ring);outline-offset:2px}
  .row{display:flex;flex-wrap:wrap;gap:10px;align-items:center}
  .card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:22px 24px;box-shadow:0 1px 2px rgba(0,0,0,.04)}
  .card h2{font-size:1.1rem;margin:0 0 12px;font-weight:700}
  .alert{border:1px solid var(--border);border-left:4px solid var(--primary);background:var(--muted);border-radius:var(--radius);padding:14px 16px;font-size:.92rem}
  .alert.warn{border-left-color:#d97706;background:#fffbeb}
  .alert.danger{border-left-color:var(--danger);background:#fef2f2}
  html.dark .alert.warn{background:#3a2f12}
  html.dark .alert.danger{background:#3a1414}
  .err{color:var(--danger)}
  .num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
  .foot{margin-top:18px;font-size:.9rem;color:var(--muted-foreground);display:flex;align-items:center;gap:10px;flex-wrap:wrap}
  /* MCP 工具清单表 */
  .mcp-tools{width:100%;border-collapse:collapse;margin-top:6px}
  .mcp-tools th,.mcp-tools td{border:1px solid var(--border);padding:8px 12px;text-align:left;font-size:.88rem;vertical-align:top}
  .mcp-tools th{background:var(--muted);font-weight:700;white-space:nowrap}
  .mcp-tools td code{white-space:nowrap}
  .mcp-tools td.muted{color:var(--muted-foreground);white-space:nowrap}
  .foot a{display:inline-flex;align-items:center;gap:6px}
  /* 三个页脚图标统一 16px（三个 SVG 自带尺寸不同：15/14/18），保证视觉等大 */
  .foot a svg{flex:0 0 auto;width:16px;height:16px;opacity:.9}
  .foot .foot-sep{opacity:.5}
  /* 「关闭」按钮用另一套配色，与绿色实心的「开启」区分状态 */
  .svccard-btn.is-off{background:var(--card);color:#b45309;border:1px solid rgba(217,119,6,.65);box-shadow:none}
  .svccard-btn.is-off:hover{background:#fffbeb;border-color:#d97706}
  html.dark .svccard-btn.is-off{background:rgba(245,158,11,.12);color:#fbbf24;border-color:rgba(245,158,11,.55)}
  html.dark .svccard-btn.is-off:hover{background:rgba(245,158,11,.2)}
  .footer{margin-top:48px;padding-top:20px;border-top:1px solid var(--border);color:var(--muted-foreground);font-size:.85rem}
  details{border:1px solid var(--border);border-radius:var(--radius);padding:12px 16px;background:var(--muted);margin-bottom:14px}
  details summary{cursor:pointer;font-weight:600;list-style:none}
  details summary::-webkit-details-marker{display:none}
  details p{margin:10px 0 0;color:var(--muted-foreground);font-size:.88rem}
  input{padding:9px 11px;border:1px solid var(--border);border-radius:var(--radius);font-size:.95rem;outline:none;background:var(--background);color:var(--foreground)}
  button{padding:9px 16px;border:1px solid transparent;border-radius:var(--radius);background:var(--primary);color:var(--primary-foreground);font-size:.95rem;font-weight:600;cursor:pointer;transition:filter .15s}
  button:hover{filter:brightness(.95)}
  button.danger{background:var(--danger);color:var(--danger-foreground)}
  table{width:100%;border-collapse:collapse;margin-top:8px;font-size:.9rem;border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
  th,td{padding:10px 12px;text-align:left;border-bottom:1px solid var(--border)}
  thead th{background:var(--muted);font-weight:600}
  tbody tr:last-child td{border-bottom:0}
  .topnav{position:sticky;top:0;z-index:30;display:flex;align-items:center;gap:16px;padding:10px 20px;border-bottom:1px solid var(--border);background:var(--glass);backdrop-filter:saturate(180%) blur(12px);-webkit-backdrop-filter:saturate(180%) blur(12px)}
  .topnav .brand{display:flex;align-items:center;gap:10px;font-weight:800;white-space:nowrap}
  .topnav nav{display:flex;gap:18px}
  .topnav nav a{color:var(--muted-foreground);font-weight:500;font-size:.92rem}
  .topnav nav a:hover{color:var(--foreground);text-decoration:none}
  .topnav .spacer{margin-left:auto;display:flex;gap:8px;align-items:center}
  .topnav .gh{display:inline-flex;align-items:center;gap:7px;font-weight:600}
  .hero{display:flex;gap:26px;align-items:center;flex-wrap:wrap;padding:44px 0 10px}
  /* 文案块可伸缩：大屏下铺满剩余宽度（不再因过宽而换行、也不留大片空白）。
     用 .hero#top 限定首页 Home Hero，避免影响登录页的 .auth .hero */
  .hero#top>div{flex:1 1 420px;min-width:0}
  /* Logo 放大到与右侧文案块同高（上下齐平），视觉更饱满；
     高度撑满容器、宽度由 aspect-ratio 反推，保证是正方形 */
  .hero#top{align-items:stretch}
  .hero#top .mark{height:100%;width:auto;aspect-ratio:1/1;flex:0 0 auto;
    max-height:320px;border-radius:26px;box-shadow:0 18px 44px rgba(22,163,74,.28)}
  /* line-height 过小会让渐变 background-clip:text 裁掉 g 等字母的降部，故用 1.15 并补一点下内边距 */
  .hero h1{font-size:clamp(2.9rem,5.4vw,4.4rem);line-height:1.15;margin:0;padding-bottom:.08em;font-weight:800;letter-spacing:-.02em;color:var(--primary)}
  @supports(-webkit-background-clip:text){.hero h1{background:linear-gradient(120deg,var(--primary),#0284c7);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}}
  /* 深色主题用更亮的蓝端，保证渐变末端在深底上也可读 */
  html.dark .hero h1{background:linear-gradient(120deg,#22c55e,#0ea5e9);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}
  .hero .lede{font-size:clamp(1.35rem,2.1vw,1.85rem);font-weight:750;margin:8px 0 6px}
  /* 不设 max-width：ch 按字符"0"宽计算，中文下会出现突兀断点，交给容器自然换行即可 */
  .hero .sub{color:var(--muted-foreground);margin:0;font-size:clamp(1rem,1.15vw,1.1rem);line-height:1.65}
  /* scroll-margin-top 让锚点跳转/截图时标题不被 sticky 顶栏压住 */
  .section{margin-top:52px;scroll-margin-top:84px}
  .sec-head{display:flex;align-items:flex-start;gap:14px;margin-bottom:20px}
  .sec-num{font-size:2.2rem;font-weight:800;line-height:1;color:var(--primary);opacity:.85;font-variant-numeric:tabular-nums}
  .sec-en{display:block;font-size:.72rem;letter-spacing:.16em;text-transform:uppercase;color:var(--muted-foreground);font-weight:700}
  .sec-head h2{font-size:1.4rem;margin:0;font-weight:750}
  .grid-features{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px}
  .feature{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px;transition:transform .15s ease,box-shadow .15s ease}
  .feature:hover{transform:translateY(-3px);box-shadow:0 10px 28px rgba(0,0,0,.07)}
  .feature h3{margin:8px 0 6px;font-size:1rem;font-weight:650}
  .feature p{margin:0;color:var(--muted-foreground);font-size:.9rem}
  .tag{display:inline-flex;min-width:40px;justify-content:center;align-items:center;padding:2px 9px;border-radius:8px;background:var(--muted);color:var(--foreground);font-weight:800;font-size:.76rem;letter-spacing:.04em}
  /* 平台标签：logo + 平台名（图形负责形象、文字保证零歧义）；三枚固定同高，品牌色增强辨识 */
  .tag-logo{padding:3px 10px 3px 8px;height:26px;gap:6px;font-size:.78rem;font-weight:700;letter-spacing:0}
  .tag-logo svg{display:block;width:17px;height:17px}
  .tag-logo .emo{font-size:.95rem;line-height:1}
  .tag-txt{white-space:nowrap}
  /* minmax(0,1fr) + 卡片 min-width:0：否则内部的等宽命令文本会把列撑开、顶出视口 */
  .grid-svc{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
  .svc{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:18px;min-width:0;transition:transform .15s ease,box-shadow .15s ease}
  .svc:hover{transform:translateY(-3px);box-shadow:0 10px 28px rgba(0,0,0,.07)}
  .svc h3{margin:10px 0 4px;font-size:1.08rem}
  .svc p{color:var(--muted-foreground);font-size:.9rem;margin:0 0 4px}
  .term{border:1px solid var(--border);border-radius:12px;overflow:hidden;background:#0b1020;margin-top:12px}
  .term-bar{display:flex;align-items:center;gap:6px;padding:8px 12px;background:rgba(255,255,255,.05);border-bottom:1px solid rgba(255,255,255,.07)}
  .term-bar .dot{width:11px;height:11px;border-radius:50%;flex:0 0 auto}
  .term-bar .dot.r{background:#ff5f56}.term-bar .dot.y{background:#ffbd2e}.term-bar .dot.g{background:#27c93f}
  /* 文件名过长时截断，避免把右侧「复制」按钮挤到贴边 */
  .term-bar .title{color:#8b98a9;font-size:.76rem;margin-left:8px;font-family:ui-monospace,Menlo,Consolas,monospace;
    min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .copy-btn{margin-left:auto;flex:0 0 auto;white-space:nowrap;background:transparent;border:1px solid rgba(255,255,255,.2);color:#cbd5e1;padding:3px 11px;font-size:.76rem;border-radius:8px;cursor:pointer}
  .copy-btn:hover{background:rgba(255,255,255,.09)}
  .term pre{padding:14px 16px}
  /* 优先在空格/斜杠处换行，避免把 127.0.0.1 这类地址拦腰截断 */
  .term code{background:transparent;color:inherit;padding:0;word-break:normal;overflow-wrap:break-word;white-space:pre-wrap}
  /* 长提示词默认折叠为约 5 行，底部渐隐提示可展开 */
  .term.folded pre{max-height:calc(1.65em * 5 + 28px);overflow:hidden;position:relative}
  .term.folded pre::after{content:"";position:absolute;left:0;right:0;bottom:0;height:60px;background:linear-gradient(to bottom,rgba(11,16,32,0),#0b1020)}
  .fold-btn{background:transparent;border:1px solid rgba(255,255,255,.2);color:#cbd5e1;padding:3px 11px;font-size:.76rem;border-radius:8px;cursor:pointer}
  .fold-btn:hover{background:rgba(255,255,255,.09)}
  .badge-link{display:inline-flex;align-items:center;gap:6px;padding:5px 13px;border:1px solid var(--border);border-radius:999px;background:var(--muted);color:var(--foreground);font-size:.82rem;font-weight:600}
  .badge-link:hover{color:var(--primary);border-color:var(--primary);text-decoration:none}
  .console{display:flex;min-height:100vh}
  .console aside{width:232px;flex:0 0 auto;border-right:1px solid var(--border);padding:16px 14px;position:sticky;top:0;height:100vh;overflow-y:auto;background:var(--card)}
  .console aside .brand{display:flex;align-items:center;gap:10px;font-weight:800;padding:4px 8px 16px}
  /* align-content:start 必需：.nav 在 flex 列里被拉伸时，grid 行会被平均撑开导致导航项间距过大 */
  .console .nav{display:grid;gap:4px;align-content:start}
  .navitem{display:flex;align-items:center;gap:10px;padding:9px 10px;border-radius:10px;color:var(--muted-foreground);font-weight:500;font-size:.92rem}
  .navitem:hover{background:var(--muted);color:var(--foreground);text-decoration:none}
  .navitem .ic{display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;flex:0 0 auto}
  .navitem .ic svg{display:block}
  .navitem.active{background:var(--muted);color:var(--foreground);font-weight:650}
  .navitem .ic{width:18px;text-align:center;flex:0 0 auto}
  .console .main{flex:1;min-width:0;padding:22px 26px 56px}
  .console .topbar{display:flex;align-items:center;gap:12px;margin-bottom:22px}
  .console .topbar h1{font-size:1.28rem;margin:0;font-weight:750;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .console .topbar .spacer{margin-left:auto;display:flex;gap:8px;align-items:center}
  .console .card{margin-bottom:18px}
  .stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(128px,1fr));gap:12px}
  .stat{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:14px 16px}
  .stat .k{color:var(--muted-foreground);font-size:.8rem;margin:0}
  .stat .v{font-size:1.55rem;font-weight:750;margin:3px 0 0;font-variant-numeric:tabular-nums;line-height:1.1}
  /* 长文本型统计值（如缓存目录路径）：缩小字号并允许断行，避免撑破卡片 */
  .stat .v.sm{font-size:.98rem;font-weight:650;line-height:1.4;word-break:break-all;font-family:ui-monospace,Menlo,Consolas,monospace}
  .stat .s{color:var(--muted-foreground);font-size:.76rem;margin:3px 0 0}
  .badge{display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border-radius:999px;background:var(--muted);color:var(--muted-foreground);font-size:.8rem;font-weight:600;white-space:nowrap}
  .badge.on{background:rgba(34,197,94,.15);color:#15803d}
  .badge.warn{background:#fffbeb;color:#b45309}
  html.dark .badge.warn{background:rgba(245,158,11,.18);color:#fbbf24}
  .badge.off{background:rgba(245,158,11,.18);color:#b45309}
  html.dark .badge.on{color:#4ade80}
  html.dark .badge.off{color:#fbbf24}
  .auth{display:grid;grid-template-columns:1.05fr .95fr;min-height:100vh}
  /* 左侧品牌面板：多层渐变 + 双光晕 + 装饰环，内容垂直居中单列 */
  .auth .hero{position:relative;overflow:hidden;margin:0;padding:56px 48px;color:#fff;display:flex;flex-direction:column;justify-content:center;
    background:linear-gradient(155deg,#0b3d24 0%,#15803d 42%,#16a34a 68%,#0ea5e9 100%)}
  .auth .hero::before{content:"";position:absolute;pointer-events:none;width:520px;height:520px;right:-170px;top:-150px;border-radius:50%;
    background:radial-gradient(circle,rgba(56,189,248,.34),transparent 62%)}
  .auth .hero::after{content:"";position:absolute;pointer-events:none;width:430px;height:430px;left:-140px;bottom:-170px;border-radius:50%;
    background:radial-gradient(circle,rgba(255,255,255,.15),transparent 65%)}
  .auth .hero .deco{position:absolute;pointer-events:none;z-index:0;border-radius:50%;border:1px solid rgba(255,255,255,.16)}
  .auth .hero .deco.a{width:320px;height:320px;right:-100px;bottom:-80px}
  .auth .hero .deco.b{width:190px;height:190px;right:130px;bottom:70px;border-color:rgba(255,255,255,.12)}
  .auth .hero>*:not(.deco){position:relative;z-index:1;max-width:460px}
  .auth .hero h2{font-size:2.25rem;margin:22px 0 12px;font-weight:800;line-height:1.18;text-shadow:0 2px 18px rgba(0,0,0,.18)}
  .auth .hero p{color:rgba(255,255,255,.84);max-width:44ch;margin:0;line-height:1.75}
  .auth .hero .mcp{display:inline-flex;align-items:center;gap:7px;margin-top:20px;padding:6px 14px;border-radius:999px;
    background:rgba(255,255,255,.14);border:1px solid rgba(255,255,255,.26);color:#fff;font-size:.84rem;font-weight:600;
    transition:background .18s ease,transform .18s ease}
  .auth .hero .mcp:hover{background:rgba(255,255,255,.22);transform:translateY(-2px)}
  .auth .hero .pts{list-style:none;padding:0;margin:24px 0 0;display:grid;gap:10px}
  .auth .hero .pts li{display:flex;gap:10px;align-items:flex-start;padding:9px 14px;border-radius:12px;font-size:.92rem;
    background:rgba(255,255,255,.10);border:1px solid rgba(255,255,255,.18);transition:transform .18s ease,background .18s ease}
  .auth .hero .pts li:hover{transform:translateY(-2px);background:rgba(255,255,255,.16)}
  .auth .hero .pts li::before{content:"✓";font-weight:800;color:#bbf7d0}
  .auth .hero-foot{margin-top:30px;display:flex;gap:10px;flex-wrap:wrap}
  .auth .hero-foot a{display:inline-flex;align-items:center;gap:8px;padding:10px 17px;border-radius:999px;font-size:.88rem;font-weight:600;
    color:rgba(255,255,255,.92);background:rgba(255,255,255,.12);border:1px solid rgba(255,255,255,.22);
    -webkit-backdrop-filter:blur(8px);backdrop-filter:blur(8px);transition:background .18s ease,transform .18s ease,box-shadow .18s ease}
  .auth .hero-foot a:hover{color:#fff;background:rgba(255,255,255,.22);transform:translateY(-2px);box-shadow:0 6px 18px rgba(0,0,0,.18);text-decoration:none}
  .auth .brand{display:flex;align-items:center;gap:12px;font-weight:800;font-size:1.18rem}
  /* 左侧品牌区里的 brand 是 <a>，会继承 a{color:var(--primary)} 的绿字压在绿底上，必须显式改白 */
  .auth .hero .brand{color:#fff}
  /* 右侧登录区：水平+垂直居中，卡片表面 */
  .auth .panel{display:flex;align-items:center;justify-content:center;min-height:100vh;padding:44px 24px;background:var(--background)}
  .auth .box{width:100%;max-width:400px;background:var(--card);border:1px solid var(--border);border-radius:16px;padding:32px 30px;
    box-shadow:0 18px 48px rgba(0,0,0,.10)}
  html.dark .auth .box{box-shadow:0 0 0 1px rgba(255,255,255,.05)}
  .auth .box .brand{margin-bottom:14px;justify-content:center}
  .auth .box h1{font-size:1.65rem;margin:6px 0 4px;font-weight:800;letter-spacing:-.01em;text-align:center}
  .auth .box .sub{color:var(--muted-foreground);font-size:.92rem;margin:0;text-align:center}
  .auth label{display:block;font-size:.86rem;font-weight:600;margin:18px 0 7px}
  .auth .box input{width:100%;height:44px;border-radius:10px;background:var(--muted);padding:0 13px}
  .auth .box input:focus{background:var(--background);border-color:var(--ring);box-shadow:0 0 0 3px rgba(22,163,74,.16)}
  html.dark .auth .box input:focus{box-shadow:0 0 0 3px rgba(34,197,94,.20)}
  .auth .pw{display:flex;gap:8px}
  .auth .pw input{flex:1;min-width:0}
  .auth .pw button{height:44px;border-radius:10px;padding:0 14px}
  .auth .btn-login{display:block;width:min(220px,72%);margin:20px auto 0;padding:12px 16px;border-radius:10px}
  .auth .err{min-height:1.3em;margin:8px 0 0;font-size:.86rem}
  .auth .note{margin-top:18px;font-size:.82rem;text-align:center}
  .auth .back{margin-top:10px;font-size:.86rem;text-align:center}
  /* 「忘记密码?」默认纯文字弱链：须显式重置通用 details 的边框/底色/内边距，否则会残留一个框 */
  .forgot{margin:12px 0 0;border:0;background:transparent;padding:0;text-align:right}
  .forgot summary{cursor:pointer;font-weight:500;font-size:.82rem;list-style:none;color:var(--muted-foreground);display:inline-block;padding:2px 0}
  .forgot summary:hover{color:var(--primary)}
  .forgot summary:focus-visible{outline:2px solid var(--ring);outline-offset:2px;border-radius:4px}
  .forgot summary::-webkit-details-marker{display:none}
  .forgot[open]{text-align:left;margin-top:14px;border:1px solid var(--border);border-radius:12px;background:var(--muted);padding:12px 14px}
  .forgot-title{font-weight:700;margin:2px 0 8px}
  .small{font-size:.82rem}
  .svcgrid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px}
  .svccard{background:var(--muted);border:1px solid var(--border);border-radius:var(--radius);padding:16px;display:flex;flex-direction:column}
  /* 两列 grid：左列（平台标签 / 标题）固定，右列状态徽标垂直居中——
     避免加了平台名后三项挤在一行导致换行参差 */
  .svccard-head{display:grid;grid-template-columns:1fr auto;grid-template-rows:auto auto;align-items:center;gap:6px 10px}
  .svccard-head>.tag{grid-column:1;grid-row:1;justify-self:start}
  .svccard-head h3{grid-column:1;grid-row:2;margin:0;font-size:1.02rem;font-weight:700}
  .svccard-head>.badge{grid-column:2;grid-row:1 / span 2;justify-self:end;align-self:center}
  .svccard p{color:var(--muted-foreground);font-size:.88rem;margin:10px 0 0}
  .svccard code{font-size:.8rem}
  /* 底部标识统一宽高并贴卡片底部对齐（margin-top:auto），三张卡视觉一致 */
  .svccard-foot{margin-top:auto;padding-top:14px;display:flex;justify-content:flex-end;align-items:center;gap:8px}
  .svccard-btn{min-width:132px;height:36px;padding:0 18px;border-radius:9px}
  .svccard-tag{display:inline-flex;align-items:center;justify-content:center;min-width:132px;height:36px;padding:0 18px;border-radius:9px;
    font-size:.84rem;font-weight:600;color:var(--muted-foreground);background:var(--card);border:1px dashed var(--border)}
  .navsep{display:block;height:1px;margin:10px 8px;background:var(--border)}
  /* 小屏抽屉导航：遮罩 + 汉堡按钮 */
  /* 操作反馈条：保存 / 创建 / 开关等操作后的一次性提示，自动淡出 */
  .flash{display:flex;align-items:center;gap:10px;margin:0 0 16px;padding:11px 14px;border-radius:12px;
    border:1px solid var(--border);background:var(--card);font-size:.9rem;animation:flashIn .25s ease}
  .flash.ok{border-color:rgba(22,163,74,.4);background:rgba(22,163,74,.09);color:#15803d}
  .flash.err{border-color:rgba(220,38,38,.4);background:rgba(220,38,38,.09);color:#b91c1c}
  .flash.out{opacity:0;transform:translateY(-6px);transition:opacity .35s ease,transform .35s ease}
  .flash .flash-x{margin-left:auto;background:none;border:0;cursor:pointer;color:inherit;font-size:.85rem;padding:2px 7px;border-radius:6px;line-height:1}
  .flash .flash-x:hover{background:rgba(0,0,0,.07)}
  @keyframes flashIn{from{opacity:0;transform:translateY(-6px)}to{opacity:1;transform:none}}
  html.dark .flash.ok{color:#4ade80}
  html.dark .flash.err{color:#f87171}
  /* 弹窗（Docker 开启前安全确认等） */
  .modal{position:fixed;inset:0;z-index:60;display:flex;align-items:center;justify-content:center;padding:18px}
  .modal[hidden]{display:none}
  .modal-mask{position:absolute;inset:0;background:rgba(0,0,0,.5)}
  .modal-box{position:relative;z-index:1;width:100%;max-width:660px;max-height:86vh;display:flex;flex-direction:column;
    background:var(--background);border:1px solid var(--border);border-radius:16px;box-shadow:0 24px 60px rgba(0,0,0,.28);animation:modalIn .22s ease}
  @keyframes modalIn{from{opacity:0;transform:translateY(10px) scale(.98)}to{opacity:1;transform:none}}
  .modal-head{display:flex;align-items:center;gap:10px;padding:16px 20px;border-bottom:1px solid var(--border)}
  .modal-head h3{margin:0;font-size:1.02rem;font-weight:750}
  /* 底部多留 8px，避免最后一行文字被底栏的渐变/边框视觉压住 */
  .modal-body{padding:16px 20px 24px;overflow-y:auto;font-size:.88rem;line-height:1.75;color:var(--muted-foreground)}
  .modal-body strong{color:var(--foreground)}
  /* 弹窗内的表单字段（如 Docker 自定义路径） */
  .modal-field{margin:0 0 14px;padding:0 0 14px;border-bottom:1px dashed var(--border)}
  .modal-field label{display:block;font-size:.82rem;font-weight:700;color:var(--foreground);margin:0 0 6px}
  .modal-field input{width:100%;padding:9px 12px;border:1px solid var(--border);border-radius:10px;
    background:var(--background);color:var(--foreground);font-size:.9rem}
  .modal-field input:focus{outline:none;border-color:var(--ring);box-shadow:0 0 0 3px rgba(22,163,74,.16)}
  .modal-field p{margin:8px 0 0}
  /* 拉取路径编辑：固定的用户名段 + 自定义路径输入框 */
  .path-edit{display:flex;align-items:center;gap:6px}
  .path-fixed{flex:0 0 auto;padding:9px 12px;border-radius:10px;background:var(--muted);border:1px dashed var(--border);
    font-family:ui-monospace,Menlo,Consolas,monospace;font-size:.88rem;color:var(--foreground);font-weight:650}
  .path-sep{flex:0 0 auto;color:var(--muted-foreground);font-weight:700}
  .path-edit input{flex:1 1 auto;min-width:0}
  .modal-body ol{margin:10px 0 0;padding-left:20px}
  .modal-body li{margin:6px 0}
  .modal-foot{display:flex;gap:10px;justify-content:flex-end;padding:14px 20px;border-top:1px solid var(--border);flex-wrap:wrap}
  .nav-mask{display:none;position:fixed;inset:0;z-index:45;background:rgba(0,0,0,.45);border:0}
  .nav-mask.show{display:block}
  .nav-toggle{display:none;align-items:center;justify-content:center;width:40px;height:40px;padding:0;font-size:1.15rem;flex:0 0 auto}
  @media (max-width:820px){.nav-toggle{display:inline-flex}}
  .card-head{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap;margin-bottom:6px}
  .card-head h2{margin:0}
  /* 侧栏底部信息区：Star 引导 + 当前版本 + Releases（贴底） */
  .console aside{display:flex;flex-direction:column}
  .console .nav{flex:1 1 auto}
  .aside-foot{margin-top:auto;padding-top:12px;border-top:1px solid var(--border);display:grid;gap:6px;font-size:.78rem}
  .star-cta{display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:7px 10px;border-radius:9px;
    background:var(--muted);border:1px solid var(--border);color:var(--foreground);font-weight:600}
  .star-cta:hover{background:var(--primary);border-color:var(--primary);color:#fff;text-decoration:none}
  .aside-foot .ver{color:var(--muted-foreground);display:inline-flex;align-items:center;gap:6px}
  .aside-foot .ver code{font-size:.75rem}
  .aside-foot .ver-link{color:var(--muted-foreground);display:inline-flex;align-items:center;gap:6px}
  .aside-foot .ver-link:hover{color:var(--primary)}
  .aside-foot svg{flex:0 0 auto;opacity:.9}
  /* Token 表格：桌面为表格，窄屏变卡片式 */
  .badge.role-admin{background:var(--primary);color:#fff;border-color:var(--primary)}
  .ops{display:inline-flex;gap:6px;align-items:center;flex-wrap:nowrap}
  .ops .btn,.ops .danger,.tok-cell .btn{height:32px;padding:0 12px;border-radius:8px;white-space:nowrap;min-width:64px}
  .tok-cell code{white-space:nowrap}
  /* 8 列在窄卡片内会被压成"一字宽"导致中文逐字换行：给表格足够最小宽度，
     超出部分交由 .table-wrap 横向滚动（不会把整页撑破） */
  .table-wrap{overflow-x:auto}
  .table-wrap table{min-width:960px}
  .tok-table th,.tok-table td{white-space:nowrap}
  .tok-table td[data-label="备注"]{white-space:normal;min-width:120px;max-width:240px}
  .edit-row td{background:var(--muted);border-radius:10px}
  .edit-form{display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap}
  .edit-form .fld input,.edit-form .fld select{width:220px}
  /* 编辑表单里的说明文字：独占一行且必须可换行，否则窄屏会撑出横向滚动 */
  .edit-form .muted{margin:0 0 6px;flex:1 1 100%;white-space:normal;word-break:break-word;min-width:0}
  /* 全局下拉框统一外观（原生下拉在各处风格不一致，这里统一为与输入框一致的圆角+自绘箭头） */
  select{appearance:none;-webkit-appearance:none;padding:9px 34px 9px 12px;border:1px solid var(--border);border-radius:10px;
    background-color:var(--background);color:var(--foreground);font-size:.88rem;line-height:1.3;
    background-repeat:no-repeat;background-position:right 11px center;background-size:12px;
    background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M2.5 4.5L6 8l3.5-3.5' fill='none' stroke='%2371717a' stroke-width='1.6' stroke-linecap='round'/%3E%3C/svg%3E")}
  select:hover{border-color:var(--ring)}
  select:focus{outline:none;border-color:var(--ring);box-shadow:0 0 0 3px rgba(22,163,74,.16)}
  html.dark select{background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M2.5 4.5L6 8l3.5-3.5' fill='none' stroke='%23a1a1aa' stroke-width='1.6' stroke-linecap='round'/%3E%3C/svg%3E")}
  .tsel{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
  .tsel label{font-size:.78rem;color:var(--muted-foreground);font-weight:600;margin:0;white-space:nowrap}
  .tsel select{min-width:210px;max-width:100%;font-size:.84rem}
  .tok-cell{display:flex;align-items:center;gap:8px}
  .tok-cell code{flex:1;min-width:0}
  .svc-toggle{display:inline}
  .conv-row{display:flex;gap:8px;flex-wrap:wrap}
  .conv-row input{flex:1;min-width:200px}
  .fld{display:flex;flex-direction:column;gap:4px;font-size:.78rem;color:var(--muted-foreground);font-weight:600}
  .fld input{width:130px}
  @media (max-width:900px){.grid-svc{grid-template-columns:1fr}.svcgrid{grid-template-columns:1fr}}
  @media (max-width:860px){
    .auth{grid-template-columns:1fr}
    .auth .hero{display:none}
    /* 左侧品牌区在小屏隐藏，故在卡片顶部补 logo + 品牌名并整体居中，避免"看不到 logo" */
    .auth .panel{min-height:100vh;min-height:100svh;padding:30px 16px 44px;
      background:linear-gradient(180deg,rgba(22,163,74,.10),transparent 300px),var(--background)}
    .auth .box{max-width:420px;padding:26px 22px}
    .auth .box .brand{justify-content:center;margin-bottom:12px}
    .auth .box .brand>svg{width:56px;height:56px;border-radius:15px}
    .auth .box h1{text-align:center}
    .auth .box .sub{text-align:center}
  }
  @media (max-width:420px){
    .auth .panel{padding:22px 12px 32px}
    .auth .box{padding:22px 18px;border-radius:14px}
    .auth .box .brand>svg{width:52px;height:52px}
  }
  @media (max-width:820px){
    /* 小屏：侧栏改为抽屉，汉堡按钮唤出、遮罩点击或选中项后收起 */
    .console{display:block;min-height:100vh}
    .console aside{position:fixed;left:0;top:0;bottom:0;width:252px;max-width:82vw;height:100vh;z-index:50;
      transform:translateX(-100%);transition:transform .24s ease;box-shadow:0 0 40px rgba(0,0,0,.28)}
    .console aside.open{transform:none}
    .console .main{padding:16px 14px 44px}
    .console .topbar{position:sticky;top:0;z-index:30;gap:10px;margin:0 0 16px;padding:10px 0;
      background:var(--glass);backdrop-filter:saturate(180%) blur(12px);-webkit-backdrop-filter:saturate(180%) blur(12px)}
    .console .topbar .spacer{gap:6px}
    .console .topbar .btn{padding:6px 10px;font-size:.82rem}
  }
  /* Token 表在窄屏切换为卡片式：隐藏表头，每行变一张卡，字段名由 data-label 提供 */
  @media (max-width:760px){
    /* 小屏 Hero：logo / 项目名 / 一句话定位 / MCP 徽标 / 按钮组居中（正文段落保持左对齐更好读） */
    .hero#top{flex-direction:column;align-items:center;gap:12px;padding:22px 0 0;text-align:center}
    /* 关键：桌面给文案块设的 flex-basis(420px) 在纵向布局下会变成「高度基准」并撑满剩余空间，
       造成 logo 下方大片空白 —— 这里必须重置为不伸缩 */
    .hero#top>div{flex:0 0 auto;width:100%}
    /* 重置桌面为「与文案齐平」而设的拉伸；margin:auto 兜底保证 logo 一定水平居中 */
    .hero#top .mark{width:88px;height:88px;max-height:none;align-self:center;flex:0 0 auto;
      aspect-ratio:1/1;margin:0 auto;border-radius:20px}
    .hero#top .lede{margin:6px 0 2px}
    .hero#top .row{justify-content:center}
    .hero#top .sub{text-align:left}
    /* 卡片式下取消表格横向滚动与最小宽度 */
    .table-wrap{overflow:visible}
    .table-wrap table{min-width:0}
    .tok-table thead{display:none}
    .tok-table,.tok-table tbody{display:block;overflow:visible;white-space:normal}
    .tok-table tr.tok-row{display:block;border:1px solid var(--border);border-radius:12px;padding:10px 12px;margin-bottom:10px;background:var(--card)}
    .tok-table tr.tok-row:hover{background:var(--card)}
    .tok-table tr.tok-row td{display:flex;align-items:flex-start;justify-content:space-between;gap:10px;border:0;padding:5px 0;white-space:normal;text-align:left}
    .tok-table tr.tok-row td::before{content:attr(data-label);color:var(--muted-foreground);font-size:.78rem;font-weight:600;flex:0 0 auto}
    .tok-table tr.tok-row td[data-label="Token"]{display:block}
    .tok-table tr.tok-row td[data-label="Token"]::before{display:block;margin-bottom:4px}
    .tok-table tr.tok-row td[data-label="操作"]{justify-content:flex-start;margin-top:6px}
    .tok-table tr.tok-row td[data-label="操作"]::before{display:none}
    .tok-table tr.edit-row{display:block}
    /* [hidden] 必须优先于上面的 display:block，否则编辑表单会默认展开 */
    .tok-table tr.edit-row[hidden]{display:none}
    .tok-table tr.edit-row td{display:block;padding:10px}
    .tok-table td[data-label="Token"]{min-width:0}
    .tok-cell code{white-space:normal;overflow-wrap:anywhere}
    .edit-form{display:block}
    /* 窄屏也用 flex 纵排：block 布局下标签与输入框会叠在一起 */
    .edit-form .fld{display:flex;flex-direction:column;gap:6px;margin-bottom:12px}
    .edit-form .fld input,.edit-form .fld select{width:100%;margin:0}
    /* 编辑区在窄屏也是一张独立卡片，与 .tok-row 视觉一致 */
    .tok-table tr.edit-row td{border:1px solid var(--border);border-radius:12px;margin-bottom:10px;background:var(--muted)}
  }
  @media (max-width:600px){.container{padding:0 14px 44px}.topnav{padding:8px 12px;gap:10px}.topnav nav{display:none}.topnav .btn{padding:6px 10px;font-size:.82rem}.topnav .gh span{display:none}.topnav .gh{padding:6px 9px}.topnav .brand{gap:8px;font-size:.95rem}.topnav .brand>svg{width:26px;height:26px}.hero{gap:18px;padding:30px 0 6px}pre{font-size:.8rem}table{display:block;overflow-x:auto;white-space:nowrap}.conv-row input{min-width:100%}.fld input{width:100%}.stat .v{font-size:1.3rem}.stat .k,.stat .s{font-size:.74rem}.stat{word-break:break-word;overflow-wrap:anywhere}}
  /* 全局 600px 的 table 横滚规则不适用于卡片化的 Token 表 */
  @media (max-width:600px){.tok-table{overflow-x:visible;white-space:normal}}
</style>`

const docOpen = `<!doctype html>
<html lang="zh-CN">
<head>
`

// pageScript 全局脚本：主题切换、终端一键复制、登录密码显隐。内联、无外部请求。
const pageScript = `<script>
function toggleTheme(){var d=document.documentElement.classList.toggle('dark');try{localStorage.setItem('theme',d?'dark':'light')}catch(e){}var b=document.getElementById('themeToggle');if(b)b.textContent=d?'☀️ 浅色':'🌙 深色';}
function copyCode(btn){var t=btn.closest('.term');if(!t)return;var c=t.querySelector('code')||t.querySelector('pre');var txt=c?c.innerText:'';var done=function(){btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent='复制'},1500)};var fb=function(){var ta=document.createElement('textarea');ta.value=txt;ta.style.position='fixed';ta.style.opacity='0';document.body.appendChild(ta);ta.select();try{document.execCommand('copy')}catch(e){}document.body.removeChild(ta);done()};if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(txt).then(done,fb)}else{fb()}}
function togglePw(){var i=document.getElementById('pw');var b=document.getElementById('pwToggle');if(!i||!b)return;if(i.type==='password'){i.type='text';b.textContent='隐藏'}else{i.type='password';b.textContent='显示'}}
function gmConvert(){var box=document.getElementById('gmconv');var inp=document.getElementById('gmconv-in');var out=document.getElementById('gmconv-out');if(!box||!inp||!out)return;var u=(inp.value||'').trim();out.removeAttribute('data-url');if(!u){out.value='';out.removeAttribute('title');out.setAttribute('placeholder','加速链接将显示在这里');return}out.value='处理中…';var fd=new URLSearchParams();fd.set('url',u);fd.set('csrf',box.getAttribute('data-csrf')||'');var sel=document.querySelector('.gmtok');if(sel&&sel.value)fd.set('token',sel.value);fetch(box.getAttribute('data-api')||'',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:fd.toString()}).then(function(r){return r.json()}).then(function(d){if(d&&d.ok&&d.url){out.value=d.url;out.setAttribute('data-url',d.url);out.title=d.url;out.setAttribute('placeholder','')}else{out.value='';out.setAttribute('placeholder',(d&&d.hint)||'无法识别该输入')}}).catch(function(){out.value='';out.setAttribute('placeholder','请求失败，请重试')})}
function gmConvURL(){var o=document.getElementById('gmconv-out');return o?(o.getAttribute('data-url')||''):''}
function gmCopyConv(){var u=gmConvURL();if(!u)return;var b=document.getElementById('gmconv-copy');if(!b)return;var done=function(){b.textContent='已复制 ✓';setTimeout(function(){b.textContent='复制'},1500)};var fb=function(){var ta=document.createElement('textarea');ta.value=u;ta.style.position='fixed';ta.style.opacity='0';document.body.appendChild(ta);ta.select();try{document.execCommand('copy')}catch(e){}document.body.removeChild(ta);done()};if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(u).then(done,fb)}else{fb()}}
function gmDownloadConv(){var u=gmConvURL();if(!u)return;var a=document.createElement('a');a.href=u;a.download='';document.body.appendChild(a);a.click();a.remove()}
function gmOpenConv(){var u=gmConvURL();if(!u)return;window.open(u,'_blank','noopener')}
function toggleNav(force){var a=document.getElementById('gmNav');var m=document.querySelector('.nav-mask');if(!a)return;var open=(typeof force==='boolean')?force:!a.classList.contains('open');a.classList.toggle('open',open);if(m)m.classList.toggle('show',open);var b=document.querySelector('.nav-toggle');if(b)b.setAttribute('aria-expanded',open?'true':'false')}
function gmCopyText(btn,txt){var done=function(){var o=btn.textContent;btn.textContent='已复制 ✓';setTimeout(function(){btn.textContent=o},1500)};var fb=function(){var ta=document.createElement('textarea');ta.value=txt;ta.style.position='fixed';ta.style.opacity='0';document.body.appendChild(ta);ta.select();try{document.execCommand('copy')}catch(e){}document.body.removeChild(ta);done()};if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(txt).then(done,fb)}else{fb()}}
function gmApplyToken(tk){try{localStorage.setItem('gm_tok',tk||'')}catch(e){}var sels=document.querySelectorAll('.gmtok');for(var i=0;i<sels.length;i++){if(sels[i].value!==tk)sels[i].value=tk}var s=document.querySelectorAll('.tslot');for(var j=0;j<s.length;j++){/* 空值/失效 token 时回退到服务端渲染的原值，绝不显示字面 "token" */var v=tk||s[j].getAttribute('data-orig')||'';s[j].textContent=v}}
function gmToggleEdit(btn){var tr=btn.closest('tr');if(!tr)return;var row=tr.classList.contains('edit-row')?tr.previousElementSibling:tr;if(!row)return;var er=row.nextElementSibling;if(!er||!er.classList.contains('edit-row'))return;var open=er.hasAttribute('hidden');if(open){er.removeAttribute('hidden')}else{er.setAttribute('hidden','')}var t=row.querySelector('.edit-toggle');if(t){t.textContent=open?'取消':'编辑';t.setAttribute('aria-expanded',open?'true':'false')}if(open){var f=er.querySelector('input[name=note]');if(f)try{f.focus()}catch(e){}}}
function gmDismissFlash(){var f=document.getElementById('gmFlash');if(f)f.remove()}
/* Docker 自定义路径预览：用户名段固定，自定义路径实时拼接 */
function gmDockerPathPreview(v){var i=document.getElementById('gmDockerPath'),p=document.getElementById('gmDockerPathPreview');if(!p)return;var o=(i&&i.getAttribute('data-owner'))||'';v=(v||'').replace(/^\/+|\/+$/g,'');p.textContent='docker pull <本服务域名>/'+o+(v?'/'+v:'')+'/library/nginx:1.25'}
/* 反馈条：清掉 URL 上的 ok/err 参数避免刷新重复提示，并 4 秒后自动淡出 */
(function(){var f=document.getElementById('gmFlash');if(!f)return;try{var u=new URL(location.href);if(u.searchParams.has('ok')||u.searchParams.has('err')){u.searchParams.delete('ok');u.searchParams.delete('err');history.replaceState(null,'',u.pathname+u.search+u.hash)}}catch(e){}setTimeout(function(){f.classList.add('out');setTimeout(function(){if(f.parentNode)f.remove()},400)},4000)})();
/* 弹窗：formId 为空时只作提示（隐藏确认按钮）；有 formId 时确认即提交对应表单 */
var gmPendingForm=null,gmOkDefault='';
function gmOpenModal(id,formId,okLabel){var m=document.getElementById(id);if(!m)return;gmPendingForm=formId?document.getElementById(formId):null;var ok=m.querySelector('[data-modal-ok]');if(ok){if(!gmOkDefault)gmOkDefault=ok.textContent;ok.hidden=!gmPendingForm;ok.textContent=okLabel||gmOkDefault}if(id==='gmDockerModal'){var pi=document.getElementById('gmDockerPath');if(pi)gmDockerPathPreview(pi.value)}m.removeAttribute('hidden');try{var b=(ok&&!ok.hidden)?ok:m.querySelector('[data-modal-cancel]');if(b)b.focus()}catch(e){}}
/* reset=false 用于「确认提交」路径：提交前不能回滚输入值 */
function gmCloseModal(reset){var ms=document.querySelectorAll('.modal');for(var i=0;i<ms.length;i++)ms[i].setAttribute('hidden','');gmPendingForm=null;if(reset!==false){var di=document.getElementById('gmDockerPath');if(di){var sv=di.getAttribute('data-saved')||'';di.value=sv;gmDockerPathPreview(sv)}}}
function gmConfirmModal(){var f=gmPendingForm;gmCloseModal(false);if(f)if(typeof f.requestSubmit==='function'){f.requestSubmit()}else{f.submit()}}
document.addEventListener('keydown',function(e){if(e.key==='Escape')gmCloseModal()});
(function(){try{var t=localStorage.getItem('gm_tok');var sels=document.querySelectorAll('.gmtok');if(!sels.length)return;if(!t){return}var hit=false;for(var i=0;i<sels.length;i++){for(var j=0;j<sels[i].options.length;j++){if(sels[i].options[j].value===t){hit=true;break}}if(hit){break}}if(hit){gmApplyToken(t)}else{try{localStorage.removeItem('gm_tok')}catch(e){}}}catch(e){}})();
function togglePromptExpand(){var t=document.getElementById('agentPromptTerm');var b=document.querySelector('#agentPromptTerm .fold-btn');if(!t||!b)return;var folded=t.classList.toggle('folded');b.textContent=folded?'展开':'收起';if(!folded){t.scrollIntoView({behavior:'smooth',block:'nearest'})}}
(function(){var d=document.documentElement.classList.contains('dark');var b=document.getElementById('themeToggle');if(b)b.textContent=d?'☀️ 浅色':'🌙 深色';})();
</script>`

// htmlHead 组装 <head>：SEO、favicon、防闪烁深色初始化与整页样式。
func htmlHead(title, desc string) string {
	return `<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + `</title>
<meta name="description" content="` + desc + `">
<meta name="keywords" content="gh-mirror,GitHub 加速,Docker 镜像加速,HuggingFace 权重,自托管,私有化部署,镜像代理">
<meta property="og:title" content="` + title + `">
<meta property="og:description" content="` + desc + `">
<meta property="og:type" content="website">
<link rel="icon" type="image/svg+xml" href="` + faviconHref + `">
<meta name="theme-color" content="#16a34a">
<script>try{var t=localStorage.getItem('theme');if(t==='dark'||(!t&&matchMedia('(prefers-color-scheme: dark)').matches))document.documentElement.classList.add('dark')}catch(e){}</script>
` + pageStyle + `
`
}

// page 组装居中容器版整页（首页 / 404）。
func page(title, desc, body string) string {
	return docOpen + htmlHead(title, desc) + `</head>
<body>
<main class="container">` + body + `</main>
` + pageScript + `
</body>
</html>`
}

// pageApp 组装全幅版整页（登录分屏 / 后台 Console，占满视口）。
func pageApp(title, desc, body string) string {
	return docOpen + htmlHead(title, desc) + `</head>
<body>
` + body + `
` + pageScript + `
</body>
</html>`
}

//go:embed docs/agent-prompt.md
var agentPromptDefault string

// agentPrompt 返回首页「一键交给 AI Agent 部署」使用的提示词。
// 优先级：AGENT_PROMPT_FILE 环境变量指定的文件 → gm-data/agent-prompt.md → 内嵌的 docs/agent-prompt.md。
// 内嵌版本随镜像版本发布（跟随版本更新），改提示词只需改文档、不必改 Go 代码。
func (s *Server) agentPrompt() string {
	if p := os.Getenv("AGENT_PROMPT_FILE"); p != "" {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
	}
	if s.cfg != nil {
		p := filepath.Join(filepath.Dir(settingsPath(s.cfg)), "agent-prompt.md")
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
	}
	return agentPromptDefault
}

// homeHTML 生成首页（宣传/介绍页）；host 取请求实际域名，避免写死示例域名。
// 提示词中的 <你的域名> 保持通用占位符（由使用者替换为实际部署域名），仅做 HTML 转义注入。
func (s *Server) homeHTML(host string) string {
	// 示例地址统一用「部署域名或IP」占位文字：首页是公开宣传页，与访问实例无关，
	// 不注入真实访问地址（host 参数仅为兼容调用方保留）。
	_ = host
	body := strings.ReplaceAll(homeTmpl, "{{AGENT_PROMPT}}", html.EscapeString(s.agentPrompt()))
	return page("gh-mirror · 多服务加速网关", "自托管的多服务加速网关：Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速。Token 鉴权、磁盘缓存、私有化部署，并支持 MCP 接入让 AI Agent 直接调用。", body)
}

const homeTmpl = `
<div class="topnav">
  <a class="brand" href="/" title="返回首页">` + logoSVG + `<span>gh-mirror</span></a>
  <nav>
    <a href="#features">特性</a>
    <a href="#usage">使用</a>
    <a href="#deploy">部署</a>
    <a href="#updates">更新</a>
    <a href="#agent">Agent</a>
  </nav>
  <div class="spacer">
    <a class="btn outline gh" href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + `<span>GitHub</span></a>
    <button id="themeToggle" class="btn outline" type="button" onclick="toggleTheme()" aria-label="切换深色 / 浅色模式">🌙 深色</button>
  </div>
</div>

<header class="hero" id="top">
  <a class="mark" href="/" title="返回首页">` + logoSVG + `</a>
  <div>
    <h1>gh-mirror</h1>
    <p class="lede">多服务加速网关</p>
    <p class="sub">Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速。私有化部署，Token 可控，磁盘缓存 + Range 续传，<strong>并支持 MCP 接入</strong>，让 AI Agent 也能直接完成部署、调用与运维。</p>
    <div class="row" style="margin-top:10px">
      <a class="badge-link" href="#agent">🤖 MCP 接入 · AI Agent 友好</a>
    </div>
    <div class="row" style="margin-top:16px">
      <a class="btn" href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + ` ⭐ Star</a>
      <a class="btn outline" href="` + repoURL + `/fork" target="_blank" rel="noopener">🍴 Fork</a>
      <a class="btn outline" href="#deploy">快速部署</a>
    </div>
  </div>
</header>

<section class="section" id="features">
  <div class="sec-head"><span class="sec-num">01</span><div><span class="sec-en">Features</span><h2>通用特性</h2></div></div>
  <div class="alert" style="margin:0 0 18px">
    <strong>这是什么？</strong> 在国内或受限网络中拉取 GitHub / Docker Hub / HuggingFace 常常很慢、超时、CI 频繁失败？把 gh-mirror 部署在能顺畅访问这些服务的节点上，对内提供高速回源与本地缓存，浏览器与命令行（curl/wget/docker）通用。
  </div>
  <div class="grid-features">
    <div class="feature">` + tagGH + `<h3>GitHub 全场景加速</h3><p>覆盖 blob/raw、Releases、仓库打包 archive、短链 gh/，支持 Range 断点续传。</p></div>
    <div class="feature">` + tagHF + `<h3>HuggingFace 权重</h3><p>加速模型 / LFS 大文件下载，config.toml 一键开启。</p></div>
    <div class="feature">` + tagDK + `<h3>Docker 镜像加速</h3><p>registry-mirror 回源 Docker Hub / GHCR，另支持 /offline/<token>/<镜像> 产出可 docker load 的 tar（离线包需 token）。</p></div>
    <div class="feature"><span class="tag">SRC</span><h3>镜像搜索</h3><p>内置强大的镜像搜索：<code>/search</code>、<code>/tags</code> 页面与 API，并可通过 MCP 供 Agent 调用。</p></div>
    <div class="feature"><span class="tag">AI</span><h3>MCP 接入 · Agent 友好</h3><p>内置 MCP 服务端，AI Agent 可直接管理 Token、服务开关、缓存与链接转换。</p></div>
    <div class="feature"><span class="tag">AUTH</span><h3>Token 鉴权</h3><p>后台签发 Token，无效请求返回 404，访问可控、可审计。</p></div>
  </div>
  <p class="muted small" style="margin-top:16px">底层能力：<strong>流式代理 + 磁盘缓存</strong>（支持 Range 断点续传），全部由 <strong>Go 标准库实现、零第三方依赖</strong>，单二进制即可运行，提供多架构镜像。</p>
</section>

<section class="section" id="usage">
  <div class="sec-head"><span class="sec-num">02</span><div><span class="sec-en">Usage</span><h2>基本使用</h2></div></div>
  <div class="grid-svc">
    <div class="svc">
      ` + tagGH + `<h3>GitHub 资源</h3>
      <p>域名后拼接 <code>/g/&lt;token&gt;/</code> 再加完整 GitHub 地址。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -L -o file "部署域名或IP/g/&lt;token&gt;/https://github.com<wbr>/owner<wbr>/repo<wbr>/releases<wbr>/download<wbr>/v1.0<wbr>/app.zip"</code></pre></div>
    </div>
    <div class="svc">
      ` + tagHF + `<h3>HuggingFace 权重</h3>
      <p>拼接 <code>/hf/&lt;token&gt;/</code> 与 owner/repo 路径（token 模式需带 token）。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -L -o config.json "部署域名或IP/hf/&lt;token&gt;/openai-community<wbr>/gpt2<wbr>/resolve<wbr>/main<wbr>/config.json"</code></pre></div>
    </div>
    <div class="svc">
      ` + tagDK + `<h3>Docker 镜像</h3>
      <p>registry 加速走 <code>/v2/&lt;用户名&gt;/…</code>（无需 token，路径必须带用户名段，后台可再加自定义路径）；离线 tar 走 <code>/offline/&lt;token&gt;/&lt;镜像&gt;</code> 供 <code>docker load</code>。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>docker pull 部署域名或IP/&lt;用户名&gt;/library/nginx:1.25</code></pre></div>
    </div>
  </div>
  <p class="muted" style="margin-top:14px">镜像搜索：内置强大的搜索能力，<code>/search?q=nginx</code> 与 <code>/tags?repo=library/nginx</code> 页面及 JSON API 均可用，也能通过 MCP 交给 AI Agent 直接调用。</p>
</section>

<section class="section" id="deploy">
  <div class="sec-head"><span class="sec-num">03</span><div><span class="sec-en">Deploy</span><h2>快速部署</h2></div></div>
  <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>git clone -b feat/hubproxy https://github.com/yufeng8/gh-mirror.git &amp;&amp; cd gh-mirror
cp .env.example .env   # 设 ADMIN_PASS / ADMIN_SECRET
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/healthz   # 期望 ok</code></pre></div>
  <p class="muted" style="margin-top:12px">也可用预构建镜像不编译直接跑：镜像 <code>ghcr.io/yufeng8/gh-mirror</code> 搭配 <a href="` + repoURL + `/releases" target="_blank" rel="noopener">GitHub Releases</a> 里的<strong>最新正式版 tag</strong>。详见 <a href="` + repoURL + `/blob/feat/hubproxy/docs/docker-deploy.md" target="_blank" rel="noopener">docs/docker-deploy.md</a> 与 <a href="` + repoURL + `/blob/feat/hubproxy/docs/deploy-runbook.md" target="_blank" rel="noopener">docs/deploy-runbook.md</a>。</p>
</section>

<section class="section" id="updates">
  <div class="sec-head"><span class="sec-num">04</span><div><span class="sec-en">Releases</span><h2>关注更新</h2></div></div>
  <p class="muted">最新版本、特性与新功能，请前往 GitHub Releases 与 README 查看：</p>
  <div class="row" style="margin-top:12px">
    <a class="btn" href="` + repoURL + `/releases" target="_blank" rel="noopener">` + githubSVG + ` Releases</a>
    <a class="btn outline" href="` + repoURL + `" target="_blank" rel="noopener">README</a>
    <a class="btn outline" href="` + repoURL + `/fork" target="_blank" rel="noopener">🍴 Fork</a>
  </div>
</section>

<section class="section" id="agent">
  <div class="sec-head"><span class="sec-num">05</span><div><span class="sec-en">AI Agent</span><h2>一键交给 AI Agent 部署</h2></div></div>
  <div class="alert" style="margin:0 0 16px">
    <strong>它也是 AI Agent 友好型服务</strong>：内置 MCP 服务端，Agent（CodeBuddy / Cursor / Claude 等）可用标准协议直接管理 Token、开关 Docker/HF 加速、调整缓存、转换链接与搜索镜像，无需再手工拼接口。
  </div>
  <p class="muted">把下面这段提示词整体复制，交给你的 AI Agent（CodeBuddy / Cursor / Claude 等），它就能照着仓库文档自动完成部署与验证。提示词中的 <code>&lt;你的域名&gt;</code> 是通用占位符，<strong>请先替换为你要部署的实际域名</strong>（或让 Agent 先向你确认）：</p>
  <div class="term folded" id="agentPromptTerm"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">prompt · 默认折叠，展开可见全文</span><button type="button" class="fold-btn" onclick="togglePromptExpand()">展开</button><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>{{AGENT_PROMPT}}</code></pre></div>
  <p class="muted" style="margin-top:12px">本服务同时支持 <strong>MCP 接入</strong>：AI Agent 可通过标准 MCP 协议直接管理 Token、服务开关与缓存，无需再猜接口。详见 <a href="` + repoURL + `/blob/feat/hubproxy/docs/mcp.md" target="_blank" rel="noopener">docs/mcp.md</a>。提示词内容与仓库 <code>docs/agent-prompt.md</code> 保持一致，随版本更新。</p>
</section>

<div class="footer">
  <details><summary>合规与免责声明</summary><p>本项目仅用于加速你有权访问的资源。严禁用于盗用他人私有仓库、规避付费、绕过访问限制或任何违反服务条款及所在地法律法规的行为。使用者须自行承担因不当使用产生的全部责任；本项目不缓存或记录请求内容，亦不对加速资源的版权与合规性负责。</p></details>
  <p class="muted">© 2026 gh-mirror · <a href="` + repoURL + `/blob/feat/hubproxy/LICENSE" target="_blank" rel="noopener">MIT License</a> · <a href="` + repoURL + `" target="_blank" rel="noopener">GitHub</a></p>
</div>
`

var notFoundHTML = page("404 - gh-mirror", "页面未找到", `
<div class="card">
  <h1>404 未找到</h1>
  <p>请求的资源不存在，或 token 无效。</p>
  <p class="muted"><a href="/">返回首页</a></p>
</div>`)

// loginBody 为登录分屏 body 片段（不含 pageStyle 的 CSS）。经 fmt.Sprintf 填充后
// 交由 pageApp() 包裹。务必保持只有 action 与 err 两个 %s 动词、无其它字面 %，
// 否则重蹈 #14 的 %!s(MISSING)（CSS 的 % 只在 pageStyle，不在此片段）。
var loginBody = `
<div class="auth">
  <div class="hero">
    <span class="deco a" aria-hidden="true"></span>
    <span class="deco b" aria-hidden="true"></span>
    <div>
      <a class="brand" href="/" title="返回首页">` + logoSVG + `<span>gh-mirror</span></a>
      <h2>多服务加速网关</h2>
      <p>Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速，浏览器与命令行通用</p>
      <span class="mcp">🤖 MCP 接入 · AI Agent 友好</span>
      <ul class="pts">
        <li>Token 鉴权，访问可控、可审计</li>
        <li>流式代理 + 磁盘缓存，支持 Range 续传</li>
        <li>私有化部署，零第三方依赖单二进制</li>
        <li>后台可视化管理：服务开关、缓存、链接转换</li>
      </ul>
    </div>
    <div class="hero-foot">
      <a href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + `<span>GitHub</span></a>
      <a href="` + repoURL + `" target="_blank" rel="noopener">⭐ Star 支持一下</a>
    </div>
  </div>
  <div class="panel">
    <div class="box">
      <a class="brand" href="/" title="返回首页">` + logoSVGAlt + `<span>gh-mirror</span></a>
      <h1>欢迎回来</h1>
      <p class="sub">登录 gh-mirror 管理后台</p>
      <form method="post" action="%s">
        <label for="user">账号</label>
        <input id="user" name="user" placeholder="请输入账号" required autofocus>
        <label for="pw">密码</label>
        <div class="pw">
          <input id="pw" name="pass" type="password" placeholder="请输入密码" required>
          <button type="button" id="pwToggle" class="btn outline" onclick="togglePw()">显示</button>
        </div>
        <p class="err">%s</p>
        <button type="submit" class="btn btn-login">登录</button>
      </form>
      <details class="forgot">
        <summary>忘记密码？</summary>
        <div class="forgot-body">
          <p class="forgot-title">在服务器上重置密码</p>
          <p class="muted small">gh-mirror 的登录密码是部署时设置的 <code>ADMIN_PASS</code>（环境变量 / <code>.env</code>）。忘记时可在服务器上重置：</p>
          <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code># docker run 部署：以新密码重建容器（token / 缓存在 gm-data、gm-cache 卷，不丢）
docker rm -f gh-mirror
docker run -d --name gh-mirror -e ADMIN_PASS='新密码' -e ADMIN_SECRET='你的密钥' -p 127.0.0.1:8080:8080 -v gm-data:/var/lib/gh-mirror ghcr.io/yufeng8/gh-mirror:latest

# docker compose 部署：改 .env 后重建
sed -i 's#^ADMIN_PASS=.*#ADMIN_PASS=新密码#' .env &amp;&amp; docker compose up -d</code></pre></div>
          <p class="muted small">镜像 tag 请用 <strong>:latest</strong>（或你当前在用的版本）。重置仅影响登录，不影响已创建的 Token 与缓存；重置后重新登录即可。</p>
        </div>
      </details>
      <p class="muted note">🔒 登录会话通过安全的 HttpOnly Cookie 保存。</p>
      <p class="muted back"><a href="/">返回首页</a></p>
    </div>
  </div>
</div>`

// dashboardBody 为后台 Console 的 body 片段（不含 pageStyle 的 CSS）。经
// fmt.Sprintf 按下列顺序填充 12 个 %s：aside / topbarRight / warn / badges /
// stats / converter / services / cacheMgmt / examples / rows / addForm / footLinks。
// 务必无其它字面 %（CSS 的 % 只在 pageStyle，不在此片段）。
var dashboardBody = `
<div class="console" id="top">
  %s
  <div class="main">
    <div class="topbar">
      <button class="btn outline nav-toggle" type="button" onclick="toggleNav()" aria-label="打开导航菜单" aria-expanded="false">☰</button>
      <h1>管理后台</h1>
      <div class="spacer">%s</div>
    </div>
    %s
    <section class="card" id="overview">
      <h2>服务状态</h2>
      <div class="row">%s</div>
      <h2 style="margin-top:22px">概览</h2>
      <div class="stats">%s</div>
    </section>
    <section class="card">
      <div class="card-head"><h2>链接转换</h2>%s</div>
      %s
    </section>
    <section class="card" id="services">
      <h2>服务管理</h2>
      %s
    </section>
    <section class="card" id="cache">
      <h2>缓存管理</h2>
      %s
    </section>
    <section class="card" id="usage">
      <h2>用法示例</h2>
      %s
    </section>
    <section class="card" id="tokens">
      <h2>Token 管理</h2>
      %s
    </section>
  </div>
</div>`

// tokensBody 是 Token 管理独立页面的 body 片段（/admin/tokens）。
// 动词：aside / topbarRight / flash / rows / addForm / foot
var tokensBody = `
<div class="console" id="top">
  %s
  <div class="main">
    <div class="topbar">
      <button class="btn outline nav-toggle" type="button" onclick="toggleNav()" aria-label="打开导航菜单" aria-expanded="false">☰</button>
      <h1>Token 管理</h1>
      <div class="spacer">%s</div>
    </div>
    %s
    <section class="card">
      <h2>Token 列表</h2>
      <div class="table-wrap">
      <table class="tok-table">
        <thead><tr><th>Token</th><th>备注</th><th>类型</th><th>创建时间</th><th class="num">访问次数</th><th class="num">流量</th><th>最近使用</th><th>操作</th></tr></thead>
        <tbody>%s</tbody>
      </table>
      </div>
      <h2 style="margin-top:22px">新增 Token</h2>
      %s
      <p class="foot">%s</p>
    </section>
  </div>
</div>`

// mcpBody 是 MCP 接入介绍页面的 body 片段（/admin/mcp）。
// 动词：aside / topbarRight / flash / status / httpEx / stdioEx / tools
var mcpBody = `
<div class="console" id="top">
  %s
  <div class="main">
    <div class="topbar">
      <button class="btn outline nav-toggle" type="button" onclick="toggleNav()" aria-label="打开导航菜单" aria-expanded="false">☰</button>
      <h1>MCP 接入</h1>
      <div class="spacer">%s</div>
    </div>
    %s
    <section class="card">
      <h2>接入就绪状态</h2>
      %s
    </section>
    <section class="card">
      <h2>方式一 · HTTP</h2>
      %s
    </section>
    <section class="card">
      <h2>方式二 · stdio</h2>
      %s
    </section>
    <section class="card">
      <h2>可用工具（12 个）</h2>
      %s
    </section>
  </div>
</div>`
