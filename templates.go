package main

import "strings"

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
  .logo svg,.mark svg,.brand svg{display:block;width:100%;height:100%}
  .muted{color:var(--muted-foreground)}
  code{background:var(--muted);padding:2px 6px;border-radius:6px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.86rem;word-break:break-all}
  pre{background:#0b1020;color:#e6edf3;padding:14px 16px;border-radius:var(--radius);overflow:auto;font-size:.85rem;line-height:1.55;margin:0}
  pre code{background:transparent;color:inherit;padding:0}
  a{color:var(--primary);text-decoration:none}
  a:hover{text-decoration:underline}
  .btn{display:inline-flex;align-items:center;gap:8px;padding:9px 16px;border-radius:var(--radius);background:var(--primary);color:var(--primary-foreground);font-size:.95rem;font-weight:600;border:1px solid transparent;cursor:pointer;transition:filter .15s,transform .15s}
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
  .foot{margin-top:18px;font-size:.9rem;color:var(--muted-foreground)}
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
  .topnav .brand{display:flex;align-items:center;gap:10px;font-weight:800}
  .topnav .brand .logo{width:30px;height:30px;border-radius:9px}
  .topnav nav{display:flex;gap:18px}
  .topnav nav a{color:var(--muted-foreground);font-weight:500;font-size:.92rem}
  .topnav nav a:hover{color:var(--foreground);text-decoration:none}
  .topnav .spacer{margin-left:auto;display:flex;gap:8px;align-items:center}
  .topnav .gh{display:inline-flex;align-items:center;gap:7px;font-weight:600}
  .hero{display:flex;gap:26px;align-items:center;flex-wrap:wrap;padding:44px 0 10px}
  .hero .mark{width:84px;height:84px;flex:0 0 auto;border-radius:20px;overflow:hidden;box-shadow:0 14px 34px rgba(22,163,74,.28)}
  .hero h1{font-size:2.9rem;line-height:1.02;margin:0;font-weight:800;letter-spacing:-.02em;color:var(--primary)}
  @supports(-webkit-background-clip:text){.hero h1{background:linear-gradient(120deg,var(--primary),#0ea5e9);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}}
  .hero .lede{font-size:1.35rem;font-weight:750;margin:8px 0 4px}
  .hero .sub{color:var(--muted-foreground);margin:0;max-width:60ch}
  .section{margin-top:52px}
  .sec-head{display:flex;align-items:flex-start;gap:14px;margin-bottom:20px}
  .sec-num{font-size:2.2rem;font-weight:800;line-height:1;color:var(--primary);opacity:.85;font-variant-numeric:tabular-nums}
  .sec-en{display:block;font-size:.72rem;letter-spacing:.16em;text-transform:uppercase;color:var(--muted-foreground);font-weight:700}
  .sec-head h2{font-size:1.4rem;margin:0;font-weight:750}
  .grid-features{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px}
  .feature{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px;transition:transform .15s ease,box-shadow .15s ease}
  .feature:hover{transform:translateY(-3px);box-shadow:0 10px 28px rgba(0,0,0,.07)}
  .feature h3{margin:8px 0 6px;font-size:1rem;font-weight:650}
  .feature p{margin:0;color:var(--muted-foreground);font-size:.9rem}
  .tag{display:inline-flex;min-width:40px;justify-content:center;padding:2px 9px;border-radius:8px;background:var(--muted);color:var(--foreground);font-weight:800;font-size:.76rem;letter-spacing:.04em}
  .grid-svc{display:grid;grid-template-columns:repeat(3,1fr);gap:16px}
  .svc{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:18px;transition:transform .15s ease,box-shadow .15s ease}
  .svc:hover{transform:translateY(-3px);box-shadow:0 10px 28px rgba(0,0,0,.07)}
  .svc h3{margin:10px 0 4px;font-size:1.08rem}
  .svc p{color:var(--muted-foreground);font-size:.9rem;margin:0 0 4px}
  .term{border:1px solid var(--border);border-radius:12px;overflow:hidden;background:#0b1020;margin-top:12px}
  .term-bar{display:flex;align-items:center;gap:6px;padding:8px 12px;background:rgba(255,255,255,.05);border-bottom:1px solid rgba(255,255,255,.07)}
  .term-bar .dot{width:11px;height:11px;border-radius:50%;flex:0 0 auto}
  .term-bar .dot.r{background:#ff5f56}.term-bar .dot.y{background:#ffbd2e}.term-bar .dot.g{background:#27c93f}
  .term-bar .title{color:#8b98a9;font-size:.76rem;margin-left:8px;font-family:ui-monospace,Menlo,Consolas,monospace}
  .copy-btn{margin-left:auto;background:transparent;border:1px solid rgba(255,255,255,.2);color:#cbd5e1;padding:3px 11px;font-size:.76rem;border-radius:8px;cursor:pointer}
  .copy-btn:hover{background:rgba(255,255,255,.09)}
  .term pre{padding:14px 16px}
  .term code{background:transparent;color:inherit;padding:0;word-break:break-all;white-space:pre-wrap}
  .console{display:flex;min-height:100vh}
  .console aside{width:232px;flex:0 0 auto;border-right:1px solid var(--border);padding:16px 14px;position:sticky;top:0;height:100vh;overflow-y:auto;background:var(--card)}
  .console aside .brand{display:flex;align-items:center;gap:10px;font-weight:800;padding:4px 8px 16px}
  .console aside .brand .logo{width:30px;height:30px;border-radius:9px}
  .console .nav{display:grid;gap:4px}
  .navitem{display:flex;align-items:center;gap:10px;padding:9px 10px;border-radius:10px;color:var(--muted-foreground);font-weight:500;font-size:.92rem}
  .navitem:hover{background:var(--muted);color:var(--foreground);text-decoration:none}
  .navitem.active{background:var(--muted);color:var(--foreground);font-weight:650}
  .navitem .ic{width:18px;text-align:center;flex:0 0 auto}
  .console .main{flex:1;min-width:0;padding:22px 26px 56px}
  .console .topbar{display:flex;align-items:center;gap:12px;margin-bottom:22px}
  .console .topbar .brand .logo{width:32px;height:32px;border-radius:9px}
  .console .topbar h1{font-size:1.28rem;margin:0;font-weight:750}
  .console .topbar .spacer{margin-left:auto;display:flex;gap:8px;align-items:center}
  .console .card{margin-bottom:18px}
  .stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(148px,1fr));gap:14px}
  .stat{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:14px 16px}
  .stat .k{color:var(--muted-foreground);font-size:.8rem;margin:0}
  .stat .v{font-size:1.55rem;font-weight:750;margin:3px 0 0;font-variant-numeric:tabular-nums;line-height:1.1}
  .stat .s{color:var(--muted-foreground);font-size:.76rem;margin:3px 0 0}
  .badge{display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border-radius:999px;background:var(--muted);color:var(--muted-foreground);font-size:.8rem;font-weight:600}
  .badge.on{background:rgba(34,197,94,.15);color:#15803d}
  .badge.off{background:rgba(245,158,11,.18);color:#b45309}
  html.dark .badge.on{color:#4ade80}
  html.dark .badge.off{color:#fbbf24}
  .auth{display:grid;grid-template-columns:1.05fr .95fr;min-height:100vh}
  .auth .hero{margin:0;padding:48px 44px;background:linear-gradient(150deg,#15803d,#16a34a 46%,#0ea5e9);color:#fff;display:flex;flex-direction:column;justify-content:space-between}
  .auth .hero h2{font-size:2.1rem;margin:20px 0 10px;font-weight:800;line-height:1.15}
  .auth .hero p{opacity:.92;max-width:42ch;margin:0}
  .auth .hero .pts{list-style:none;padding:0;margin:22px 0 0;display:grid;gap:12px}
  .auth .hero .pts li{display:flex;gap:10px;align-items:flex-start;opacity:.95}
  .auth .hero .pts li::before{content:"✓";font-weight:800}
  .auth .hero-foot a{color:#fff;opacity:.9;font-weight:600}
  .auth .brand{display:flex;align-items:center;gap:12px;font-weight:800;font-size:1.18rem}
  .auth .brand .logo{width:40px;height:40px;border-radius:11px}
  .auth .panel{display:flex;align-items:center;justify-content:center;padding:40px 24px;background:var(--background)}
  .auth .box{width:100%;max-width:388px}
  .auth .box .brand{margin-bottom:10px}
  .auth .box .brand .logo{width:34px;height:34px}
  .auth .box h1{font-size:1.7rem;margin:6px 0 2px;font-weight:800}
  .auth label{display:block;font-size:.86rem;font-weight:600;margin:16px 0 6px}
  .auth .pw{display:flex;gap:8px}
  .auth .pw input{flex:1}
  .auth .btn-block{width:100%;margin-top:12px;padding:11px 16px}
  .auth .err{min-height:1.3em;margin:8px 0 0;font-size:.86rem}
  .auth .note{margin-top:18px;font-size:.82rem}
  .forgot{margin-top:14px;border:1px solid var(--border);border-radius:var(--radius);background:var(--muted)}
  .forgot summary{cursor:pointer;font-weight:600;padding:10px 14px;list-style:none;color:var(--primary)}
  .forgot summary::-webkit-details-marker{display:none}
  .forgot[open] summary{border-bottom:1px solid var(--border)}
  .forgot-body{padding:4px 14px 14px}
  .forgot-title{font-weight:700;margin:10px 0 6px}
  .small{font-size:.82rem}
  @media (max-width:900px){.grid-svc{grid-template-columns:1fr}.hero h1{font-size:2.3rem}}
  @media (max-width:860px){.auth{grid-template-columns:1fr}.auth .hero{display:none}}
  @media (max-width:820px){
    .console{flex-direction:column}
    .console aside{width:100%;height:auto;position:static;display:flex;gap:6px;overflow-x:auto;border-right:0;border-bottom:1px solid var(--border);padding:10px 12px}
    .console aside .brand{display:none}
    .console .nav{display:flex;gap:6px}
    .navitem{white-space:nowrap}
    .console .main{padding:18px 16px 48px}
  }
  @media (max-width:600px){.container{padding:0 14px 44px}.topnav nav{display:none}pre{font-size:.8rem}table{display:block;overflow-x:auto;white-space:nowrap}}
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

// homeHTML 生成首页（宣传/介绍页）；host 取请求实际域名，避免写死示例域名。
func homeHTML(host string) string {
	body := strings.ReplaceAll(homeTmpl, "{{HOST}}", host)
	return page("gh-mirror · 多服务加速网关", "自托管的多服务加速网关：Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速。Token 鉴权、磁盘缓存、私有化部署。", body)
}

const homeTmpl = `
<div class="topnav">
  <a class="brand" href="#top">` + logoSVG + `<span>gh-mirror</span></a>
  <nav>
    <a href="#features">特性</a>
    <a href="#usage">使用</a>
    <a href="#deploy">部署</a>
    <a href="#updates">更新</a>
  </nav>
  <div class="spacer">
    <a class="btn outline gh" href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + `<span>GitHub</span></a>
    <a class="btn" href="` + repoURL + `" target="_blank" rel="noopener">⭐ Star</a>
    <button id="themeToggle" class="btn outline" type="button" onclick="toggleTheme()" aria-label="切换深色 / 浅色模式">🌙 深色</button>
  </div>
</div>

<header class="hero" id="top">
  <div class="mark">` + logoSVG + `</div>
  <div>
    <h1>gh-mirror</h1>
    <p class="lede">多服务加速网关</p>
    <p class="sub">Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速。私有化部署，Token 可控，磁盘缓存 + Range 续传。</p>
    <div class="row" style="margin-top:16px">
      <a class="btn" href="` + repoURL + `" target="_blank" rel="noopener">` + githubSVG + ` ⭐ Star on GitHub</a>
      <a class="btn outline" href="` + repoURL + `/fork" target="_blank" rel="noopener">🍴 Fork</a>
      <a class="btn outline" href="#deploy">快速部署</a>
    </div>
  </div>
</header>

<div class="alert" style="margin-top:26px">
  <strong>这是什么？</strong> 在国内或受限网络中拉取 GitHub / Docker Hub / HuggingFace 常常很慢、超时、CI 频繁失败？把 gh-mirror 部署在能顺畅访问这些服务的节点上，对内提供高速回源与本地缓存，浏览器与命令行（curl/wget/docker）通用。
</div>

<section class="section" id="features">
  <div class="sec-head"><span class="sec-num">01</span><div><span class="sec-en">Features</span><h2>通用特性</h2></div></div>
  <div class="grid-features">
    <div class="feature"><span class="tag">GH</span><h3>GitHub 全场景加速</h3><p>覆盖 blob/raw、Releases、仓库打包 archive、短链 gh/，支持 Range 断点续传。</p></div>
    <div class="feature"><span class="tag">HF</span><h3>HuggingFace 权重</h3><p>加速模型 / LFS 大文件下载，config.toml 一键开启。</p></div>
    <div class="feature"><span class="tag">DK</span><h3>Docker 镜像加速</h3><p>registry-mirror 回源 Docker Hub / GHCR，另支持 /offline 产出可 docker load 的 tar。</p></div>
    <div class="feature"><span class="tag">SRC</span><h3>镜像搜索</h3><p>/search 与 /tags 页面及 API，快速查找镜像与版本。</p></div>
    <div class="feature"><span class="tag">AUTH</span><h3>Token 鉴权</h3><p>后台签发 Token，无效请求返回 404，访问可控、可审计。</p></div>
    <div class="feature"><span class="tag">CACHE</span><h3>流式代理 + 磁盘缓存</h3><p>Go 标准库实现，零第三方依赖，单二进制，多架构镜像。</p></div>
  </div>
</section>

<section class="section" id="usage">
  <div class="sec-head"><span class="sec-num">02</span><div><span class="sec-en">Usage</span><h2>基本使用</h2></div></div>
  <div class="grid-svc">
    <div class="svc">
      <span class="tag">GH</span><h3>GitHub 资源</h3>
      <p>域名后拼接 <code>/g/&lt;token&gt;/</code> 再加完整 GitHub 地址。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -L -o file "{{HOST}}/g/&lt;token&gt;/https://github.com/owner/repo/releases/download/v1.0/app.zip"</code></pre></div>
    </div>
    <div class="svc">
      <span class="tag">HF</span><h3>HuggingFace 权重</h3>
      <p>拼接 <code>/hf/&lt;token&gt;/</code> 与 owner/repo 路径（token 模式需带 token）。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>curl -L -o config.json "{{HOST}}/hf/&lt;token&gt;/openai-community/gpt2/resolve/main/config.json"</code></pre></div>
    </div>
    <div class="svc">
      <span class="tag">DK</span><h3>Docker 镜像</h3>
      <p>配置 registry 加速走 <code>/v2</code>；或 <code>/offline</code> 产出 tar 供 <code>docker load</code>。</p>
      <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">/etc/docker/daemon.json</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>{ "registry-mirrors": ["{{HOST}}"] }</code></pre></div>
    </div>
  </div>
  <p class="muted" style="margin-top:14px">镜像搜索：<code>/search?q=nginx</code> 与 <code>/tags?image=library/nginx</code>。</p>
</section>

<section class="section" id="deploy">
  <div class="sec-head"><span class="sec-num">03</span><div><span class="sec-en">Deploy</span><h2>快速部署</h2></div></div>
  <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code>git clone -b feat/hubproxy https://github.com/yufeng8/gh-mirror.git &amp;&amp; cd gh-mirror
cp .env.example .env   # 设 ADMIN_PASS / ADMIN_SECRET
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/healthz   # 期望 ok</code></pre></div>
  <p class="muted" style="margin-top:12px">也可用预构建镜像 <code>ghcr.io/yufeng8/gh-mirror:v0.1.0</code> 不编译直接跑。详见 <a href="` + repoURL + `/blob/feat/hubproxy/docs/docker-deploy.md" target="_blank" rel="noopener">docs/docker-deploy.md</a> 与 <a href="` + repoURL + `/blob/feat/hubproxy/docs/deploy-runbook.md" target="_blank" rel="noopener">docs/deploy-runbook.md</a>。</p>
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
    <div>
      <div class="brand">` + logoSVG + `<span>gh-mirror</span></div>
      <h2>多服务加速网关</h2>
      <p>Docker 镜像 · GitHub 资源 · HuggingFace 权重，一个网关全加速。</p>
      <ul class="pts">
        <li>Token 鉴权，访问可控、可审计</li>
        <li>流式代理 + 磁盘缓存，支持 Range 续传</li>
        <li>私有化部署，零第三方依赖单二进制</li>
      </ul>
    </div>
    <div class="hero-foot"><a href="` + repoURL + `" target="_blank" rel="noopener">GitHub ↗</a></div>
  </div>
  <div class="panel">
    <div class="box">
      <div class="brand">` + logoSVG + `<span>gh-mirror</span></div>
      <h1>欢迎回来</h1>
      <p class="muted">登录 gh-mirror 管理后台</p>
      <form method="post" action="%s">
        <label for="user">账号</label>
        <input id="user" name="user" placeholder="请输入账号" required autofocus>
        <label for="pw">密码</label>
        <div class="pw">
          <input id="pw" name="pass" type="password" placeholder="请输入密码" required>
          <button type="button" id="pwToggle" class="btn outline" onclick="togglePw()">显示</button>
        </div>
        <p class="err">%s</p>
        <button type="submit" class="btn btn-block">登录</button>
      </form>
      <details class="forgot">
        <summary>忘记密码？</summary>
        <div class="forgot-body">
          <p class="forgot-title">在服务器上重置密码</p>
          <p class="muted">gh-mirror 的登录密码是部署时设置的 <code>ADMIN_PASS</code>（环境变量 / <code>.env</code>）。忘记时可在服务器上重置：</p>
          <div class="term"><div class="term-bar"><span class="dot r"></span><span class="dot y"></span><span class="dot g"></span><span class="title">bash</span><button type="button" class="copy-btn" onclick="copyCode(this)">复制</button></div><pre><code># docker run 部署：以新密码重建容器（token / 缓存在 gm-data、gm-cache 卷，不丢）
docker rm -f gh-mirror
docker run -d --name gh-mirror -e ADMIN_PASS='新密码' -e ADMIN_SECRET='你的密钥' -p 127.0.0.1:8080:8080 -v gm-data:/var/lib/gh-mirror ghcr.io/yufeng8/gh-mirror:v0.1.0

# docker compose 部署：改 .env 后重建
sed -i 's#^ADMIN_PASS=.*#ADMIN_PASS=新密码#' .env &amp;&amp; docker compose up -d</code></pre></div>
          <p class="muted small">重置仅影响登录，不影响已创建的 Token 与缓存；重置后重新登录即可。登录会话通过安全的 HttpOnly Cookie 保存。</p>
        </div>
      </details>
      <p class="muted note">🔒 登录会话通过安全的 HttpOnly Cookie 保存。</p>
      <p class="muted"><a href="/">返回首页</a></p>
    </div>
  </div>
</div>`

// dashboardBody 为后台 Console 的 body 片段（不含 pageStyle 的 CSS）。经
// fmt.Sprintf 按下列顺序填充 9 个 %s：aside / topbarRight / warn / badges /
// stats / examples / rows / addForm / footLinks。务必无其它字面 %。
var dashboardBody = `
<div class="console" id="top">
  %s
  <div class="main">
    <div class="topbar">
      <div class="brand">` + logoSVG + `<span>gh-mirror</span></div>
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
      <h2>代理用法示例</h2>
      %s
    </section>
    <section class="card" id="tokens">
      <h2>Token 管理</h2>
      <table>
        <thead><tr><th>Token</th><th>备注</th><th>创建时间</th><th class="num">访问次数</th><th class="num">流量</th><th>最近使用</th><th>操作</th></tr></thead>
        <tbody>%s</tbody>
      </table>
      <h2 style="margin-top:22px">新增 Token</h2>
      %s
      <p class="foot">%s</p>
    </section>
  </div>
</div>`
