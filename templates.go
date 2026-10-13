package main

import "fmt"

// pageStyle 是各页面的公共样式（不含 <style> 标签）。
const pageStyle = `
<style>
  :root{--accent:#2da44e;--accent-d:#22863a;--bg:#f6f8fa;--card:#fff;--bd:#e1e4e8;--fg:#24292f;--mut:#6e7781}
  *{box-sizing:border-box}
  body{font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,Helvetica,Arial,sans-serif;max-width:920px;margin:36px auto;padding:0 18px;color:var(--fg);line-height:1.65;background:var(--bg)}
  .card{background:var(--card);border:1px solid var(--bd);border-radius:12px;padding:24px 26px;box-shadow:0 1px 3px rgba(27,31,36,.06)}
  h1{font-size:24px;margin:0 0 4px}
  h2{font-size:16px;margin:26px 0 12px;padding-left:10px;border-left:4px solid var(--accent)}
  p{margin:8px 0}
  .muted{color:var(--mut)}
  code{background:#eef1f4;padding:2px 6px;border-radius:5px;word-break:break-all;font-size:13px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
  pre{background:#0d1117;color:#e6edf3;padding:14px;border-radius:8px;overflow:auto;font-size:13px}
  a{color:var(--accent-d);text-decoration:none}
  a:hover{text-decoration:underline}
  .ex{margin:10px 0}
  .num{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
  input{padding:9px 11px;margin:5px 6px 5px 0;border:1px solid var(--bd);border-radius:7px;font-size:14px;outline:none}
  input:focus{border-color:var(--accent);box-shadow:0 0 0 3px rgba(45,164,78,.15)}
  button{padding:9px 16px;border:0;border-radius:7px;background:var(--accent);color:#fff;font-size:14px;cursor:pointer;transition:background .15s}
  button:hover{background:var(--accent-d)}
  button.danger{background:#cf222e}
  button.danger:hover{background:#a40e26}
  .grid{border-collapse:collapse;width:100%;margin-top:6px;overflow:hidden;border-radius:10px;border:1px solid var(--bd)}
  .grid th,.grid td{padding:10px 12px;text-align:left;font-size:14px;border-bottom:1px solid var(--bd)}
  .grid thead th{background:#f0f3f6;font-weight:600;color:#1f2328}
  .grid tbody tr:hover{background:#fafbfc}
  .grid tbody tr:last-child td{border-bottom:0}
  .add-form{display:flex;flex-wrap:wrap;align-items:center;gap:4px}
  .foot{margin-top:22px;font-size:14px}
  .err{color:#cf222e}
  .center{max-width:380px;margin:0 auto}
</style>`

// page 组装完整 HTML 文档。
func page(title, body string) string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + title + `</title>` + pageStyle + `</head><body>` + body + `</body></html>`
}

// homeHTML 生成首页；host 取请求的实际域名，避免把示例域名写死。
func homeHTML(host string) string {
	return page("GitHub 镜像加速", fmt.Sprintf(`
<h1>GitHub 资源镜像加速</h1>
<p>本服务通过 <strong>token 鉴权</strong> 提供 GitHub 资源加速下载，浏览器与命令行（curl/wget）均可用。</p>
<h2>使用方式</h2>
<div class="ex">在域名后拼接 <code>/g/&lt;token&gt;/</code> 再加上<strong>完整 GitHub 地址</strong>：<br>
<code>%s/g/&lt;token&gt;/https://github.com/owner/repo/blob/main/path/file.txt</code></div>
<div class="ex">Raw 文件：<br><code>%s/g/&lt;token&gt;/https://raw.githubusercontent.com/owner/repo/main/file.txt</code></div>
<div class="ex">Releases 二进制：<br><code>%s/g/&lt;token&gt;/https://github.com/owner/repo/releases/download/v1.0/app.zip</code></div>
<div class="ex">仓库打包：<br><code>%s/g/&lt;token&gt;/https://github.com/owner/repo/archive/refs/heads/main.zip</code></div>
<div class="ex">短链（token 后接 /gh/）：<br><code>%s/g/&lt;token&gt;/gh/owner/repo/raw/main/file.txt</code></div>
<h2>命令行</h2>
<pre>curl -L -o app.zip "%s/g/&lt;token&gt;/https://github.com/owner/repo/releases/download/v1.0/app.zip"</pre>
<h2>说明</h2>
<ul>
  <li>未携带有效 token 的请求将返回 404。</li>
  <li>服务器高速回源 GitHub 并做本地缓存；命中缓存时响应头为 <code>X-Cache: HIT</code>。</li>
  <li>支持 <code>Range</code> 断点续传；大文件（默认 200MB）不写入缓存，仅做透传。</li>
  <li>仅允许 GitHub 相关域名回源，避免被当作开放代理滥用。</li>
  <li>管理后台默认路径 <code>/admin</code>（可用 ADMIN_PATH 自定义）。</li>
</ul>`, host, host, host, host, host, host))
}

var notFoundHTML = page("404 - 未找到", `
<h1>404 未找到</h1>
<p>请求的资源不存在，或 token 无效。</p>
<p><a href="/">返回首页</a></p>`)

var loginHTML = page("管理员登录", `
<h1>管理员登录</h1>
<form method="post" action="%s">
<div><input name="user" placeholder="账号" required></div>
<div><input name="pass" type="password" placeholder="密码" required></div>
<div><button type="submit">登录</button></div>
</form>
<p class="err">%s</p>
<p><a href="/">返回首页</a></p>`)

var dashboardHTML = page("管理后台", `
<div class="card">
<h1>管理后台</h1>
<p class="muted">代理示例（将“你的域名”替换为实际域名）：</p>
<p><code>%s</code></p>
<h2>Token 列表</h2>
<table class="grid">
<thead><tr><th>Token</th><th>备注</th><th>创建时间</th><th class="num">访问次数</th><th class="num">流量</th><th>最近使用</th><th>操作</th></tr></thead>
<tbody>%s</tbody>
</table>
<h2>新增 Token</h2>
<form method="post" action="%s/add" class="add-form">
<input type="hidden" name="csrf" value="%s">
<input name="token" placeholder="留空则自动生成">
<input name="note" placeholder="备注（可选）">
<button type="submit">添加</button>
</form>
<p class="foot"><a href="%s/logout">退出登录</a> · <a href="/">返回首页</a></p>
</div>`)
