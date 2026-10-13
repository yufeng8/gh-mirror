package main

import "fmt"

// repoURL 是本项目在 GitHub 上的地址，用于首页 Stars / Fork 引导与页脚。
const repoURL = "https://github.com/zzidcn/github-mirror"

// logoSVG 是项目 Logo（内联 SVG，便于首页零额外请求渲染，契合私有化部署）。
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="github-mirror logo">
  <defs>
    <linearGradient id="gm" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" stop-color="#22c55e"/>
      <stop offset="100%" stop-color="#0ea5e9"/>
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

// pageStyle 复刻 shadcn 设计系统：中性色板 + CSS 变量主题、圆角卡片、
// focus-visible 可访问焦点态、移动优先的响应式断点。全部内联、不依赖任何外部 CDN。
const pageStyle = `
<style>
  :root{
    --background:#ffffff; --foreground:#0a0a0a; --card:#ffffff;
    --muted:#f4f4f5; --muted-foreground:#71717a; --border:#e4e4e7;
    --primary:#16a34a; --primary-foreground:#ffffff; --ring:#16a34a;
    --danger:#dc2626; --danger-foreground:#ffffff; --radius:.75rem;
  }
  *{box-sizing:border-box}
  html{-webkit-text-size-adjust:100%}
  body{
    margin:0; color:var(--foreground); background:var(--background); line-height:1.7;
    font-family:ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,"PingFang SC","Microsoft YaHei",sans-serif;
    -webkit-font-smoothing:antialiased;
  }
  .container{max-width:960px;margin:0 auto;padding:32px 20px 56px}
  header.site{display:flex;align-items:center;gap:14px;margin-bottom:6px}
  .logo{width:46px;height:46px;flex:0 0 auto;border-radius:12px;box-shadow:0 1px 2px rgba(0,0,0,.08)}
  header.site h1{font-size:1.6rem;line-height:1.2;margin:0;font-weight:700;letter-spacing:-.01em}
  .tagline{color:var(--muted-foreground);margin:2px 0 0;font-size:1.02rem}
  .section{margin-top:30px}
  .section h2{font-size:1.15rem;margin:0 0 14px;font-weight:650}
  .muted{color:var(--muted-foreground)}
  .card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:22px 24px;box-shadow:0 1px 2px rgba(0,0,0,.04)}
  .grid-features{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:14px}
  .feature{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px}
  .feature h3{margin:0 0 6px;font-size:1rem;font-weight:600}
  .feature p{margin:0;color:var(--muted-foreground);font-size:.92rem}
  code{background:var(--muted);padding:2px 6px;border-radius:6px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.86rem;word-break:break-all}
  pre{background:#0b1020;color:#e6edf3;padding:14px 16px;border-radius:var(--radius);overflow:auto;font-size:.85rem;line-height:1.55}
  pre code{background:transparent;color:inherit;padding:0}
  a{color:var(--primary);text-decoration:none}
  a:hover{text-decoration:underline}
  .btn{display:inline-flex;align-items:center;gap:8px;padding:9px 16px;border-radius:var(--radius);background:var(--primary);color:var(--primary-foreground);font-size:.95rem;font-weight:600;border:1px solid transparent;cursor:pointer;transition:filter .15s}
  .btn:hover{filter:brightness(.95);text-decoration:none}
  .btn.outline{background:transparent;color:var(--foreground);border-color:var(--border)}
  .btn:focus-visible,button:focus-visible,input:focus-visible,a:focus-visible{outline:2px solid var(--ring);outline-offset:2px}
  .badge{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;background:var(--muted);color:var(--muted-foreground);font-size:.78rem;font-weight:600}
  .alert{border:1px solid var(--border);border-left:4px solid var(--primary);background:var(--muted);border-radius:var(--radius);padding:14px 16px;font-size:.92rem}
  .alert.warn{border-left-color:#d97706;background:#fffbeb}
  .alert.danger{border-left-color:var(--danger);background:#fef2f2}
  table{width:100%;border-collapse:collapse;margin-top:8px;font-size:.9rem;border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
  th,td{padding:10px 12px;text-align:left;border-bottom:1px solid var(--border)}
  thead th{background:var(--muted);font-weight:600}
  tbody tr:last-child td{border-bottom:0}
  input{padding:9px 11px;border:1px solid var(--border);border-radius:var(--radius);font-size:.95rem;outline:none;background:var(--background);color:var(--foreground)}
  button{padding:9px 16px;border:1px solid transparent;border-radius:var(--radius);background:var(--primary);color:var(--primary-foreground);font-size:.95rem;font-weight:600;cursor:pointer;transition:filter .15s}
  button:hover{filter:brightness(.95)}
  button.danger{background:var(--danger);color:var(--danger-foreground)}
  .row{display:flex;flex-wrap:wrap;gap:10px;align-items:center}
  .starbar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;margin-top:22px;padding:16px;border:1px dashed var(--border);border-radius:var(--radius);background:var(--muted)}
  .foot{margin-top:22px;font-size:.92rem}
  .err{color:var(--danger)}
  .num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
  .footer{margin-top:36px;padding-top:18px;border-top:1px solid var(--border);color:var(--muted-foreground);font-size:.85rem}
  @media (max-width:600px){
    .container{padding:24px 16px 40px}
    header.site h1{font-size:1.35rem}
    .grid-features{grid-template-columns:1fr}
    pre{font-size:.8rem}
    table{display:block;overflow-x:auto;white-space:nowrap}
  }
</style>`

// page 组装完整 HTML 文档，含 SEO 元信息（description / Open Graph / theme-color）与语义化骨架。
func page(title, desc, body string) string {
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + `</title>
<meta name="description" content="` + desc + `">
<meta property="og:title" content="` + title + `">
<meta property="og:description" content="` + desc + `">
<meta property="og:type" content="website">
<meta name="theme-color" content="#16a34a">
` + pageStyle + `
</head>
<body>
<main class="container">` + body + `</main>
</body>
</html>`
}

// homeHTML 生成首页；host 取请求的实际域名，避免把示例域名写死。
func homeHTML(host string) string {
	body := fmt.Sprintf(homeTmpl, host, host)
	return page("github-mirror · GitHub 资源镜像加速", "自托管的 GitHub 资源镜像加速服务，支持 Token 鉴权与管理后台，完全私有化部署，一键原生/Docker 部署。", body)
}

const homeTmpl = `
<header class="site">
  ` + logoSVG + `
  <div>
    <h1>github-mirror</h1>
    <p class="tagline">自托管的 GitHub 资源镜像加速服务 · Token 鉴权 · 管理后台 · 完全私有化部署</p>
  </div>
</header>

<div class="alert">
  <strong>这是什么？</strong> 在国内或受限网络中拉取 GitHub 常常很慢、超时、CI 频繁失败？本项目让你在自己部署的服务器上做 GitHub 资源加速与缓存，浏览器与命令行（curl/wget）通用。
</div>

<section class="section">
  <h2>🎯 解决的实际痛点</h2>
  <div class="grid-features">
    <div class="feature"><h3>拉取慢 / 超时</h3><p>把服务器部署在能顺畅访问 GitHub 的节点，对内提供高速回源与本地缓存。</p></div>
    <div class="feature"><h3>CI 频繁失败</h3><p>构建机拉依赖、下载 Release 不再被 GitHub 限流或超时打断。</p></div>
    <div class="feature"><h3>jsDelivr 有局限</h3><p>jsDelivr 仅加速公开仓库源码且单文件约 20MB；本项目覆盖 release / archive / API 全场景。</p></div>
    <div class="feature"><h3>担心被滥用</h3><p>所有请求必须携带后台下发的 Token，无效即 404，不会被当作开放代理。</p></div>
  </div>
</section>

<section class="section">
  <h2>✨ 核心特性</h2>
  <div class="grid-features">
    <div class="feature"><h3>全场景加速</h3><p>覆盖 blob/raw、Releases、仓库打包（archive）、短链 gh/。</p></div>
    <div class="feature"><h3>Token 鉴权</h3><p>后台签发 Token，无效请求返回 404，访问可控、可审计。</p></div>
    <div class="feature"><h3>Web 管理后台</h3><p>可视化增删 Token、查看流量与访问示例地址。</p></div>
    <div class="feature"><h3>流式代理 + 缓存</h3><p>Go 标准库实现，支持 Range 断点续传与磁盘缓存。</p></div>
    <div class="feature"><h3>一键部署</h3><p>原生 / Docker 两种模式，deploy.sh 与 install.sh 开箱即用。</p></div>
    <div class="feature"><h3>多架构镜像</h3><p>GHCR 自动构建 amd64/arm64，CI 自动公开，匿名 docker pull。</p></div>
  </div>
</section>

<section class="section">
  <h2>🚀 使用方式</h2>
  <div class="card">
    <p>在域名后拼接 <code>/g/&lt;token&gt;/</code> 再加上<strong>完整 GitHub 地址</strong>：</p>
    <p><code>%s/g/&lt;token&gt;/https://github.com/owner/repo/blob/main/path/file.txt</code></p>
    <p class="muted">Raw 文件、Releases、archive、短链 gh/ 同理替换后面的 GitHub 地址即可。</p>
    <pre>curl -L -o app.zip "%s/g/&lt;token&gt;/https://github.com/owner/repo/releases/download/v1.0/app.zip"</pre>
  </div>
</section>

<div class="starbar">
  <span>🌟 如果这个项目帮你省下了时间，欢迎到 GitHub <strong>点个 Star</strong>，让更多人发现它：</span>
  <a class="btn" href="` + repoURL + `" target="_blank" rel="noopener">⭐ Star on GitHub</a>
  <a class="btn outline" href="` + repoURL + `/fork" target="_blank" rel="noopener">🍴 Fork</a>
</div>

<div class="alert warn">
  <strong>合规与免责声明</strong>：本项目仅用于加速你<strong>有权访问</strong>的 GitHub 资源。严禁用于盗用他人私有仓库、规避付费、绕过访问限制或任何违反 GitHub 服务条款及所在地法律法规的行为。使用者须自行承担因不当使用产生的全部责任；本项目不缓存或记录请求内容，亦不对加速资源的版权与合规性负责。
</div>

<div class="footer">
  <p>管理后台默认路径 <code>/admin</code>（可用 <code>ADMIN_PATH</code> 自定义）。本服务仅回源 GitHub 相关域名，不会被当作开放代理。</p>
  <p class="muted">© 2026 github-mirror · <a href="` + repoURL + `/blob/main/LICENSE">MIT License</a></p>
</div>
`

var notFoundHTML = page("404 - github-mirror", "页面未找到", `
<div class="card">
  <h1>404 未找到</h1>
  <p>请求的资源不存在，或 token 无效。</p>
  <p class="muted"><a href="/">返回首页</a></p>
</div>`)

var loginHTML = page("管理员登录 - github-mirror", "管理员登录", `
<div class="card" style="max-width:360px">
  <h1>管理员登录</h1>
  <form method="post" action="%s" class="row" style="flex-direction:column;align-items:stretch">
    <input name="user" placeholder="账号" required>
    <input name="pass" type="password" placeholder="密码" required>
    <button type="submit" class="btn">登录</button>
  </form>
  <p class="err">%s</p>
  <p class="muted"><a href="/">返回首页</a></p>
</div>`)

var dashboardHTML = page("管理后台 - github-mirror", "管理后台", `
<div class="card">
  <h1>管理后台</h1>
  <p class="muted">代理示例（将“你的域名”替换为实际域名）：</p>
  <p><code>%s</code></p>
  <h2>Token 列表</h2>
  <table>
    <thead><tr><th>Token</th><th>备注</th><th>创建时间</th><th class="num">访问次数</th><th class="num">流量</th><th>最近使用</th><th>操作</th></tr></thead>
    <tbody>%s</tbody>
  </table>
  <h2>新增 Token</h2>
  <form method="post" action="%s/add" class="row">
    <input type="hidden" name="csrf" value="%s">
    <input name="token" placeholder="留空则自动生成">
    <input name="note" placeholder="备注（可选）">
    <button type="submit" class="btn">添加</button>
  </form>
  <p class="foot"><a href="%s/logout">退出登录</a> · <a href="/">返回首页</a></p>
</div>`)
