<p align="center">
  <img src="social-preview.png" alt="gh-mirror · self-hosted multi-service acceleration gateway" width="100%">
</p>

<p align="center">
  <b>A single-binary, zero-dependency self-hosted acceleration gateway: Docker images · GitHub resources · HuggingFace weights, all in one.</b><br>
  Built-in token auth, disk cache, image search and an <b>MCP server</b> — let AI agents take over the ops too.
</p>

<p align="center">
  <strong>English</strong> | <a href="README.md">中文</a>
</p>

<p align="center">
  <a href="https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml"><img src="https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml/badge.svg" alt="build"></a>
  <a href="https://github.com/yufeng8/gh-mirror/releases"><img src="https://img.shields.io/github/v/release/yufeng8/gh-mirror?include_prereleases&label=release" alt="release"></a>
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/deps-0-blueviolet" alt="zero dependencies">
  <img src="https://img.shields.io/badge/protocols-Docker%20%7C%20GitHub%20%7C%20HuggingFace-2ea44f" alt="protocols">
  <a href="docs/mcp.md"><img src="https://img.shields.io/badge/MCP-AI%20Agent%20ready-6f42c1" alt="MCP"></a>
  <img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="license">
</p>

<p align="center">
  <a href="#-screenshots">Screenshots</a> ·
  <a href="#-highlights">Highlights</a> ·
  <a href="#-mcp-let-ai-agents-take-over">MCP</a> ·
  <a href="#-one-click-ai-agent-deployment">AI deploy</a> ·
  <a href="#-quick-start">Quick start</a> ·
  <a href="#-configuration">Configuration</a>
</p>

---

## 📷 Screenshots

> Real screenshots from a live instance (desktop + mobile adaptive), not mockups. The admin UI is currently Chinese-only.

<table>
  <tr>
    <td width="50%"><img src="docs/images/home-hero.png" alt="Homepage"><br><sub><b>Homepage</b>: capability overview + one-click handoff to AI Agent</sub></td>
    <td width="50%">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="docs/images/admin-overview-dark.png">
        <img src="docs/images/admin-overview-light.png" alt="Admin dashboard">
      </picture>
      <br><sub><b>Admin dashboard</b>: service status / stats / link converter　<i>(light & dark themes)</i></sub>
    </td>
  </tr>
  <tr>
    <td><img src="docs/images/admin-services.png" alt="Service management"><br><sub><b>Services</b>: toggle switches, instant + persisted</sub></td>
    <td><img src="docs/images/admin-tokens.png" alt="Token management"><br><sub><b>Tokens</b>: dedicated page, type / copy / inline edit</sub></td>
  </tr>
</table>

---

## 🎯 Highlights

### 📦 Three accelerators, one gateway

| Service | Endpoint | Auth | Highlights |
|---------|----------|------|------------|
| 🐳 **Docker images** | `/v2/<username>[/<custom-path>]/…` | Path prefix (protocol cannot carry a token) | Multi-upstream (Docker Hub / GHCR / Quay / GCR / k8s), manifest+token in-memory cache, layer disk cache |
| 🐙 **GitHub resources** | `/g/<username>/<token>/https://github.com/…` | Username segment + token | blob/raw, archive, releases, API, `Range` resume |
| 🤗 **HuggingFace weights** | `/hf/<username>/<token>/<owner>/<repo>/resolve/<rev>/<file>` | Username segment + token | Follows 302 to CDN automatically, model / LFS streaming + cache |
| 📦 **Offline image tar** | `/offline/<username>/<token>/<image>` | Username segment + token | No local Docker needed, produces a `docker load`-ready tar |

> **Multi-tenant isolation**: `<username>` is your dashboard login name; `<token>` must be a token owned by that user (ownership validation — invalid or mismatched always 404). All acceleration endpoints share one uniform format.

### 👥 Multi-user & account system

- 🔀 **Dual entry points, strictly isolated**: super-admin entry `/admin` and normal-user entry `/user` (`USER_PATH` configurable / hideable), each with its own session cookie; normal users are rejected at the super-admin entry.
- ✉️ **Self-service signup / password recovery**: after the super admin configures SMTP, email-code signup can be enabled; forgotten passwords can be self-reset by email code (normal users only — the super admin password can only be changed on the server).
- 🎟️ **Invite-code signup**: optional "invite-only" mode with batch generation / custom codes / export (placeholder templates) / notes / expiry / usage tracking.
- 🔐 **Graphical captcha**: on the login, signup and admin-login pages, with separate toggles for the user entry and the admin entry.
- 👤 **Roles & ownership**: two roles (super admin / normal user); tokens and Docker custom paths are isolated per user.

### 💡 Why self-host

- 🚀 **Single binary, zero deps**: `CGO_ENABLED=0` static build, multi-arch images, runs on a 1-core / 512 MB box.
- 🔒 **Controllable access**: token auth (on by default) + IP (CIDR) / repo (wildcard) allow/block lists + per-IP token bucket (IPv6 collapsed to `/64`).
- 💾 **Real disk cache**: layers / files persisted to disk, `X-Cache: HIT` on hit; total size, per-file limit and TTL are hot-editable in the dashboard, with automatic oldest-first eviction on over-quota or low disk.
- 🤖 **AI Agent friendly**: built-in MCP server (`POST /ai/<username>/mcp` and `gh-mirror mcp` stdio), 12 tools covering tokens, service switches, cache, link conversion and image search.
- 🎛️ **Web dashboard**: service status, stats, link converter, token management, MCP setup and cache management — all in the browser, switches take effect instantly and persist to `gm-data/settings.json`.

---

## 🤖 MCP: let AI agents take over the gateway

Built-in MCP (Model Context Protocol) server — agents don't have to guess APIs, they operate the gateway with a standard protocol:

```bash
# Option 1: HTTP (recommended, with an Admin Token)
curl -sX POST https://mirror.example.com/ai/<username>/mcp \
  -H "Authorization: Bearer <admin-token>" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# Option 2: stdio (only on the machine holding the gateway data; use HTTP for remote gateways)
gh-mirror mcp --token <admin-token>
```

| Category | Tools |
|----------|-------|
| Health & info | `health` |
| Token management | `list_tokens` · `create_token` · `delete_token` |
| Service switches | `get_services` · `set_service` |
| Cache | `get_cache_stats` · `set_cache_limits` · `clear_cache` |
| Links & images | `convert_link` · `search_images` · `list_tags` |

> **Multi-user permissions**: the MCP entry requires an Admin Token (the calling user is located by token ownership). Token tools are ownership-isolated — normal users can only view / create / delete their own tokens; service switches and cache management require the super admin (same as the web UI); `convert_link` generates links with your own username segment.
>
> See [docs/mcp.md](docs/mcp.md). The dashboard's "MCP setup" page auto-generates copy-ready connection commands for your current Admin Token.

## 🤖 One-click AI Agent deployment

Copy the prompt below **as a whole** to CodeBuddy / Cursor / Claude or any AI Agent — it will deploy and run an end-to-end verification: first read the repo docs to pick a deployment shape (without hijacking ports 80/443, compatible with existing reverse proxies / CDNs), then deploy, and finally report each check as `✅/❌`.

<details>
<summary><b>📋 Click to expand the full deployment prompt</b> (kept in sync with <a href="docs/agent-prompt.md">docs/agent-prompt.md</a>, updated per release)</summary>

```text
You are a deployment assistant. Deploy gh-mirror (a self-hosted multi-service acceleration gateway: Docker image / GitHub resource / HuggingFace weight acceleration + image search + MCP) on this Linux server, exposed at https://<your-domain>.

[Step 0: anti-hallucination rules (highest priority — violating them means a wrong deployment)]
1. **Whitelist only**: env vars, docs, endpoints and image tags must come from the explicit lists in this prompt.
   Anything not listed that you "think should exist" does not exist — **never generate it from memory**.
2. **Verify before use**: unsure whether a variable / file / endpoint exists? clone the repo and check with
   grep / ls (e.g. `grep -rn "VAR" config/config.go`, `ls docs/`). If you can't find it, follow this
   whitelist — **never fill the gap with a name you invented**.
3. **Never invent prefixed variables**: do not emit non-whitelisted `GM_*`, `GHW_*`, `GHW-*`, `GH_*`
   variables — the service reads none of them; they are dead config (and may leak plaintext passwords
   into compose). The only `GM_`-prefixed one is `GM_MCP_TOKEN`, used solely by the `gh-mirror mcp` subcommand.
4. **Don't invent "plausible" switches**: gh-mirror has **no** env var for concurrency / threads /
   connections / run mode / database / max users. Use `RATELIMIT_ENABLED` for throttling; limit memory/CPU
   at the **Docker layer** (`mem_limit: 900m` / `cpus: 1` in compose), not via env vars.
5. **Only read docs that exist**: there is **no** `COMPOSE.md`, `RELEASES.md`, `INSTALL.md` or
   `releases.atom` in this repo — don't read or cite imagined filenames. Real docs are listed below.

[Step 1: confirm the public domain]
<your-domain> is a generic placeholder, not any specific site. Before doing anything, ask the user: "What is the public domain?"; if the user does not provide one, do not guess or reuse the example domain — ask first, then continue. Replace every <your-domain> below with the confirmed domain.

[Hard requirements]
1. Read the repo docs first (**only these real ones**):
   git clone https://github.com/yufeng8/gh-mirror.git
   - README.md (overview)
   - docs/docker-deploy.md: generic Docker deployment, correct setup for domain-fronting CDNs (EdgeOne/Cloudflare), Docker acceleration security, known pitfalls
   - docs/deploy-runbook.md: the full flow (probe → choose → execute → end-to-end verify)
   - docs/mcp.md: MCP integration
   - .env.example: **the single authoritative whitelist of env vars**
   Then follow the environment adaptation / conflict checklist in docs/deploy-runbook.md strictly.
2. Prefer the prebuilt image, do not compile: ghcr.io/yufeng8/gh-mirror:<latest stable tag> (multi-arch, all features).
   - Get the tag from the GitHub Releases page (https://github.com/yufeng8/gh-mirror/releases), or
     `gh release list --exclude-pre-releases --limit 1`;
     **do not use `:latest`** (uncontrollable upgrades) and **do not look for `releases.atom`** (no such file).
3. Persist data with named volumes: gm-data (tokens & settings), gm-cache (cache). Recreating the container must not lose data.
4. Generate strong random values for ADMIN_PASS and ADMIN_SECRET on the spot and keep them safe (never commit them or write them to logs).
   ⚠️ Important (a top cause of login failures): ADMIN_PASS is the **seed password used on first start** —
   on startup the program writes ADMIN_USER and the password hash into gm-data/users.json; **changing the
   ADMIN_PASS env var later will NOT update an existing user's password**. If you changed the password and
   can't log in: log in with the old password and change it under "User management", or delete users.json
   inside the volume and restart so it is re-seeded with the current ADMIN_PASS.
   ADMIN_PATH defaults to /admin, LISTEN defaults to 127.0.0.1:8080.
5. Pick one of two deployment shapes based on the machine; do not hijack host ports 80/443:
   A. Machine already runs nginx / KPanel: **use the repo's `docker-compose.standalone.yml`** (single file, exposes only 127.0.0.1:8080, prebuilt image, real env vars) — `docker compose -f docker-compose.standalone.yml up -d`, then let the existing reverse proxy forward; **don't** hand-edit `ports` / `environment` on the root `docker-compose.yml` to cobble one together.
   B. Clean host: use the repo's bundled nginx (--profile proxy) or reuse an existing host certificate.
6. Environment variables: **only** these real ones (identical to `.env.example`; nothing else exists):
   - required: ADMIN_USER, ADMIN_PASS, ADMIN_SECRET
   - entries: ADMIN_PATH (default /admin), USER_PATH (default /user)
   - network: LISTEN (default 127.0.0.1:8080)
   - cache/storage: CACHE_DIR, CACHE_TTL_MINUTES, MAX_CACHE_MB, UPSTREAM_TIMEOUT, TOKENS_FILE
   - switches: DOCKER_ENABLED, HF_ENABLED, RATELIMIT_ENABLED, USE_JSDELIVR, COOKIE_SECURE
   - egress: UPSTREAM_SOCKS5
   **Never invent any non-whitelisted variable** (`GM_*`, `GHW_*`, `GHW-*`, `GH_*`…): `GM_DATA_DIR`,
   `GM_PROXY_MAX_*`, `GM_ALLOWED_NETWORKS`, `GM_ADMIN_PASSWORD`, `GHW-MODE`, `GHW-DB`, `GHW-DATA-DIR`,
   `GHW-USER-MAX-CONC`, etc. are **not read** by the main service (the only `GM_`-prefixed one is
   `GM_MCP_TOKEN`, used only by the `gh-mirror mcp` subcommand). Writing them is dead config and may
   leak a plaintext password into compose.
   ⚠️ With plain HTTP (no TLS) you must set `COOKIE_SECURE=false`, otherwise login silently fails / loops.
7. The reverse-proxy block must be configured exactly like this (otherwise proxy paths return 404):
   merge_slashes off;
   proxy_pass http://127.0.0.1:8080$request_uri;   # $request_uri is mandatory
   proxy_set_header Host $host;
   proxy_set_header X-Forwarded-Proto https;        # pin https behind a CDN
   and do not stack an http→https redirect on top (it creates a loop).
8. If a domain-fronting CDN (EdgeOne/Cloudflare) is in play: let the CDN hold the public certificate and terminate TLS, with the origin doing 80-only reverse proxy;
   disable caching and URL normalization/rewrite on the CDN side; do not run certbot behind the CDN (it reports 521, re-issuing won't help);
   use DNS-01 validation if the origin must hold its own certificate.
9. Open inbound port 80 on the firewall / cloud security group; public HTTPS is provided by the CDN or the host.

[After completion, verify each item and report]
- curl -fsS http://127.0.0.1:8080/healthz                → ok
- curl -fsS -H 'Host: <your-domain>' http://127.0.0.1/healthz → ok (origin reverse-proxy block works)
- curl -fsS https://<your-domain>/healthz                     → ok (full chain works)
- Browser opens https://<your-domain>/ (multi-service landing page)
- https://<your-domain>/admin logs in (ADMIN_USER / ADMIN_PASS)
- After adding a token in the dashboard: curl -fsSI "https://<your-domain>/g/<ADMIN_USER>/<ADMIN_TOKEN>/https://raw.githubusercontent.com/octocat/Hello-World/master/README" → 200
- Image search: curl -fsS "https://<your-domain>/search?q=nginx" returns results
- If Docker acceleration is enabled: docker pull <your-domain>/<ADMIN_USER>/library/nginx:1.25 pulls and hits cache (the path must carry the username segment; an optional custom path can be added in the dashboard);
  curl -fsS "https://<your-domain>/offline/<ADMIN_USER>/<ADMIN_TOKEN>/library/nginx:1.25" -o nginx.tar and docker load -i nginx.tar loads it (the offline path must carry both the username segment and the token)
- If HuggingFace is enabled: curl -fsS "https://<your-domain>/hf/<ADMIN_USER>/<ADMIN_TOKEN>/openai-community/gpt2/resolve/main/config.json" returns content
- If MCP is used: POST https://<your-domain>/ai/<ADMIN_USER>/mcp works (the legacy `/mcp` endpoint is deprecated and always returns 401 — expected)

⚠️ **Never verify endpoints gh-mirror does not proxy** (**do not enumerate beyond this list**):
`/hf/*/discussions`, `/hf/*/pulls/*/diff`, `/hf/*/compare/*/diff` and similar GitHub social / collaboration
features are **out of scope** and will always 404. A 404 there does not mean the install failed — do not
retry or change config because of it. Verify only the items listed above.

[On errors]
Do not retry blindly. Read the "known pitfalls / troubleshooting" section of docs/docker-deploy.md first; if still unresolved,
paste the key error and the relevant config verbatim for analysis.

[Report format]
Deployment shape (A/B), image tag, each verification result as ✅/❌, and any deviation from the requirements above with reasons.
```

</details>

> The prompt includes the hard constraint "**confirm the public domain before touching anything**", the correct reverse-proxy / CDN setup, and an **anti-hallucination whitelist** (real env vars / docs / endpoints plus a "verify before use" rule) — so agents won't guess domains, invent `GM_*`/`GHW_*` variables, read non-existent docs, or treat endpoints gh-mirror doesn't proxy as a failed install. Once the service is up, you can also hand the MCP connection commands from [docs/mcp.md](docs/mcp.md) to an agent and let it take over tokens, service switches and cache.

---

## ✨ Features

<p align="center"><img src="docs/images/home-features.png" alt="General features"></p>

- 🐳 **Docker image acceleration**: Registry API v2 compatible, built-in Docker Hub / GHCR / Quay / GCR / registry.k8s.io upstreams; manifest/token in-memory cache, layer write-through disk cache, streaming, `Range` resume.
- 🐙 **GitHub resource acceleration**: `/g/<username>/<token>/` + a full GitHub URL — covers blob/raw, archive, releases and API, with disk cache.
- 🤗 **HuggingFace acceleration**: `/hf/<username>/<token>/<owner>/<repo>/resolve/<rev>/<file>`, follows 302 to the CDN automatically, model/LFS streaming + cache.
- 📦 **Offline image tar**: `/offline/<username>/<token>/<image>` — no local Docker needed, packages an image into a `docker load`-ready tar (streaming + concurrency de-duplication; **requires the username segment and token** to avoid abuse of your bandwidth).
- 🔍 **Image search**: Docker Hub search / tag browsing, lightweight server-rendered pages (`/search`, `/tags`) and a JSON API (`/api/search`, `/api/tags`).
- 🛡️ **Security controls**: token auth (default) + optional open mode; IP (CIDR) / repo (wildcard) allow/block lists; per-real-client-IP token bucket (IPv6 collapsed to `/64`).
- ⚙️ **Unified configuration**: `config.toml` + environment variable overrides, smoothly compatible with legacy env-only deployments.
- 🧦 **Optional SOCKS5 egress**: route all GitHub/HF/Docker upstream traffic through a SOCKS5 proxy.
- 🤖 **MCP (AI Agent friendly)**: built-in MCP server — agents can manage tokens, toggle Docker/HuggingFace acceleration, view and adjust cache, convert links, health-check, and search images/tags through the standard protocol. Supports `POST /ai/<username>/mcp` and `gh-mirror mcp` stdio (with your own Admin Token), see `docs/mcp.md`.
- 🎛️ **Web dashboard ops**: service status badges, overview stats (token count / hits / traffic / cache usage / uptime), instant service switches, editable cache limits and TTL with automatic eviction, link converter.

> The example domain in the docs is `mirror.example.com` — replace it with your own domain.
> The design is partly inspired by [sky22333/hubproxy](https://github.com/sky22333/hubproxy)'s multi-service architecture, rewritten with the Go **standard library** and zero third-party dependencies, for easy auditing and offline builds.

---

## 🚀 Quick start

### 🐳 Option 1: Docker (recommended)

```bash
docker run -d --name gh-mirror \
  -p 8080:8080 \
  -e LISTEN=0.0.0.0:8080 \
  -e CACHE_DIR=/data/cache \
  -e TOKENS_FILE=/data/tokens.json \
  -e ADMIN_USER=admin \
  -e ADMIN_PASS='change-me' \
  -e ADMIN_SECRET="$(openssl rand -hex 32)" \
  -e USER_PATH=/user \
  -e DOCKER_ENABLED=true \
  -e HF_ENABLED=true \
  -v gm-data:/data \
  ghcr.io/yufeng8/gh-mirror:v1.3.0
```

After startup: the **super admin** logs in at `http://<domain>/admin` (super accounts only); **normal users** log in at `http://<domain>/user` (`USER_PATH` is configurable — make sure it does not collide with reserved paths such as `/g` `/hf` `/offline` `/v2` `/ai` `/mcp` `/search` `/healthz`, or startup fails fast with an error).

### 🧩 Option 2: docker-compose

```bash
ADMIN_SECRET="$(openssl rand -hex 32)" docker compose up -d
```

See [`docker-compose.yml`](docker-compose.yml): the data volume `gm-data` (cache + tokens) is mounted by default; enable `DOCKER_ENABLED` / `HF_ENABLED` / `RATELIMIT_ENABLED` / `UPSTREAM_SOCKS5` in `environment` as needed.

### 🧰 Option 3: build locally

```bash
go build -o gh-mirror .
./gh-mirror   # reads ./config.toml (falls back to defaults + env vars if missing)
```

> For production, put it behind nginx + HTTPS and keep `LISTEN` at `127.0.0.1:8080` behind the reverse proxy. One-click deploy/install scripts are in "Deploy scripts" at the end.

---

## 🔧 Configuration

Priority: **environment variables > `config.toml` > built-in defaults**. When `config.toml` is missing it degrades gracefully to defaults + env vars (container deployments can rely on env vars alone).

### 📝 config.toml sample

```toml
listen = "127.0.0.1:8080"
cache_dir = "./cache"
cache_ttl_minutes = 1440
max_cache_mb = 200
upstream_timeout = 60
use_jsdelivr = false
tokens_file = "./tokens.json"

[admin]
path = "/admin"
user_path = "/user"    # normal-user entry path (colliding with the super-admin entry or reserved paths fails at startup)
user = "admin"
pass = "admin"
secret = ""            # session signing key, recommend pinning
cookie_secure = true   # keep true under HTTPS

[auth]
mode = "token"         # token=require token (default); open=open proxy (use with [acl])

[docker]
enabled = false
upstreams = ["https://registry-1.docker.io"]
cache_ttl_minutes = 1440

[hf]
enabled = false
cache_ttl_minutes = 1440

[ratelimit]
enabled = false
period_seconds = 3600  # stats period
quota = 1000           # per-IP quota per period
ipv6_prefix = 64       # IPv6 collapse prefix

[acl]
ip_allow = []          # e.g. ["10.0.0.0/8"], when non-empty only listed IPs are allowed
ip_block = []            # e.g. ["1.2.3.4"]
repo_allow = []          # e.g. ["library/*","grafana"], wildcards supported
repo_block = []          # e.g. ["evil/*"]

[upstream]
socks5 = ""            # e.g. "socks5://user:pass@127.0.0.1:1080"
```

### 🌱 Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN` | `127.0.0.1:8080` | Listen address |
| `CACHE_DIR` | `./cache` | Cache directory |
| `CACHE_TTL_MINUTES` | `1440` | Cache TTL (minutes) |
| `MAX_CACHE_MB` | `200` | Responses larger than this are not cached |
| `UPSTREAM_TIMEOUT` | `60` | Upstream timeout (seconds) |
| `USE_JSDELIVER` | `0` | `1` routes GitHub blob/raw through the jsDelivr edge cache |
| `TOKENS_FILE` | `./tokens.json` | Token persistence file |
| `ADMIN_PATH` | `/admin` | Dashboard (super admin) path |
| `USER_PATH` | `/user` | Normal-user entry path (colliding with `ADMIN_PATH` or reserved paths such as `/g` `/hf` `/offline` `/v2` fails at startup) |
| `ADMIN_USER` | `admin` | Admin username |
| `ADMIN_PASS` | `admin` | Admin password |
| `ADMIN_SECRET` | random | Session signing key (recommend pinning so sessions survive restarts) |
| `COOKIE_SECURE` | `true` | Whether the session cookie carries Secure |
| `DOCKER_ENABLED` | `false` | Enable Docker image acceleration |
| `HF_ENABLED` | `false` | Enable HuggingFace acceleration |
| `RATELIMIT_ENABLED` | `false` | Enable per-IP rate limiting |
| `UPSTREAM_SOCKS5` | empty | SOCKS5 egress proxy, e.g. `socks5://user:pass@host:1080` |

---

## 🧭 Usage per service

<p align="center"><img src="docs/images/home-usage.png" alt="Basic usage: copy-paste commands for all three services"></p>

### 🐙 GitHub resources (`/g/<username>/<token>/`)

```
https://mirror.example.com/g/<username>/<token>/<full GitHub URL>
```

| Scenario | URL |
|----------|-----|
| Repo file | `.../g/<username>/<token>/https://github.com/owner/repo/blob/main/path/file.txt` |
| Raw file | `.../g/<username>/<token>/https://raw.githubusercontent.com/owner/repo/main/file.txt` |
| Releases | `.../g/<username>/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip` |
| Repo archive | `.../g/<username>/<token>/https://github.com/owner/repo/archive/refs/heads/main.zip` |
| Short link | `.../g/<username>/<token>/gh/owner/repo/raw/main/file.txt` |

```bash
curl -L -o app.zip "https://mirror.example.com/g/<username>/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
```

`<username>` is your dashboard login name and `<token>` must be a token owned by that user (ownership validation — invalid or mismatched always 404). Response header `X-Cache: HIT` means cache hit, `MISS` means upstream fetch.

### 🐳 Docker images (`/v2/<username>[/<custom-path>]/…`, `/token`)

<p align="center"><img src="docs/images/admin-docker-modal.png" alt="Security confirmation and custom path before enabling Docker" width="52%"><br>
<sub>A security confirmation pops up before enabling: explains the risks and lets you set the "username + custom path" prefix</sub></p>

After enabling `[docker] enabled=true` (or `DOCKER_ENABLED=true`), the `/v2` pull path **always carries the username segment** (an optional custom path can be added in the dashboard):

```bash
# username segment = your dashboard login name; the custom path is optional under "Services → Docker image acceleration"
docker pull mirror.example.com/admin/library/nginx:1.25
```

> Because the path carries a prefix, `registry-mirrors` **no longer applies** (the docker daemon does not support path-prefixed mirrors). In a `docker pull` command you **do not write `/v2/` yourself** (the client adds it automatically); the full server-side path is `/v2/<username>[/<custom-path>]/…`.
>
> **Auth model**: registry pulls go through the Docker protocol transparently and are **not token-checked** — "username segment + custom path" is itself the credential (only someone who knows the path can use it); only the **`/offline`** package endpoint does the `<username>/<token>` double check.

Multi-registry (GHCR/Quay/GCR/k8s etc.) uses the domain-prefixed form (username segment first):

```
https://mirror.example.com/v2/<username>/ghcr.io/<owner>/<repo>/manifests/<tag>
```

### 🤗 HuggingFace (`/hf/<username>/<token>/`)

After enabling `[hf] enabled=true`:

```bash
# token mode (default, username segment + token)
curl -L -o model.bin "https://mirror.example.com/hf/<username>/<token>/<owner>/<repo>/resolve/main/model.bin"
# open mode ([auth] mode=open) can omit the username segment and token
curl -L -o model.bin "https://mirror.example.com/hf/<owner>/<repo>/resolve/main/model.bin"
```

### 🔍 Image search / tag browsing

- Pages: `https://mirror.example.com/search?q=nginx`, `https://mirror.example.com/tags?repo=library/nginx`
- JSON: `/api/search?q=nginx`, `/api/tags?repo=library/nginx`

### 📦 Offline image packaging (`/offline/`)

After enabling `[docker] enabled=true`, package an image into a `docker load`-ready tar on demand, **with no local Docker**:

```bash
curl -L -o nginx.tar "https://mirror.example.com/offline/<username>/<token>/library/nginx:1.25"
docker load -i nginx.tar
```

- Multi-arch images are handled automatically (picks `linux/amd64` by default, falls back to `linux/arm64`).
- Honors the `[acl]` repo allow/block lists; concurrent packaging of the same image returns `429` (de-duplication).
- Note: the output is the classic `docker-save` format; please validate compatibility with a real `docker load` on first use.

---

## 💻 Admin dashboard

**Dual entry points**: super-admin entry `/admin` (super admins only) + normal-user entry `/user` (`USER_PATH` configurable / hideable), each with its own session cookie; normal users are rejected at the super-admin entry. Credentials come from the `ADMIN_USER` / `ADMIN_PASS` environment variables (change the defaults!); sessions are kept by a signed session cookie (HMAC + HttpOnly + SameSite=Strict).

**Super-admin capabilities** (`/admin`): service management (Docker / HuggingFace switches + Docker custom path), cache management (limits / TTL / purge + offline package file management + auto-purge on low disk), user management (create / delete / change role / reset password + SMTP & signup toggles + invite-code management), full token management, MCP setup.

**Normal-user capabilities** (`/user`): manage your own tokens, set your own Docker custom path (when Docker is enabled), MCP setup; when a service is disabled the corresponding entry shows "please ask the super admin to enable it".

The dashboard is mobile-friendly (393px): drawer navigation, card-style token lists, and inline forms when you tap "Edit".

---

## 🔐 Security notes

- **Domain allowlist**: upstream fetches are restricted to GitHub/HuggingFace-related domains (see `rewrite.go`), so it cannot be abused as an open proxy (SSRF-safe).
- **Token auth**: by default every proxied request must carry a dashboard-issued token; invalid tokens return 404. `tokens.json` and `ADMIN_SECRET` are credentials — keep them safe.
- **Access control**: `[acl]` supports IP (CIDR) and repo (wildcard) allow/block lists, enforced at the entry; a match returns 403.
- **Rate limiting**: `[ratelimit]` is a per-real-client-IP token bucket; exceeding it returns 429.
- **Real IP**: taken from `X-Forwarded-For` / `X-Real-IP` / `RemoteAddr`. **These headers can be spoofed — only enable them behind a trusted reverse proxy**; for direct exposure, overwrite these headers at the proxy layer.
- You can further add IP restrictions or Basic Auth for `/admin` at the nginx layer.

---

## 🏭 Production tips

- **Reverse proxy + HTTPS**: nginx reverse proxy to `127.0.0.1:8080`, certificate from Let's Encrypt; `COOKIE_SECURE=true`.
- **Persistent data**: point `CACHE_DIR` and `TOKENS_FILE` at a persistent volume/directory instead of the container overlay layer.
- **Cache eviction**: cache entries are **automatically invalidated** by a background task — every 10 minutes a three-tier strategy (TTL expiry / over total limit / low disk space (<1 GB)) evicts oldest-first, no extra cron needed; the deploy scripts also generate a cron job that cleans cache files older than 7 days daily as a fallback.
- **Pin `ADMIN_SECRET`**: avoids invalidating all sessions on restart.
- **Memory**: the single binary has a very low resident footprint; for Docker mode mind the daemon overhead — small instances can use the native deployment.

---

## 📜 Deploy scripts

The original one-click deploy / install / uninstall scripts are kept (Ubuntu 24.04, with nginx + TLS + systemd):

```bash
# One-click deploy (after uploading the source, run as root)
sudo bash deploy.sh

# One-click install (pulls a prebuilt binary from Releases, no source needed)
DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='your-password' \
  bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)

# Uninstall
bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/uninstall.sh)
```

> The one-click scripts (`deploy.sh` / `install.sh` / `uninstall.sh`) and the GHCR image all point to this fork: `yufeng8/gh-mirror` (image `ghcr.io/yufeng8/gh-mirror`).

---

## 📁 Project layout

```
main.go        # entry, config loading, service group routing, entry middleware
config/        # config.go (TOML parsing + env overrides) + config.toml sample
proxy.go       # GitHub proxy (/g/<username>/<token>/) + shared serveTarget/upstream/cache
docker.go      # Docker Registry v2 caching reverse proxy
hf.go          # HuggingFace acceleration (/hf/<username>/<token>/)
search.go      # image search/tag API + server-rendered pages
offline.go     # offline image tar packaging (content-addressable cache)
offlinecache.go # offline package cache data layer (manifest JSON / task state / disk purge hook)
clientip.go    # real client IP + IPv6 /64 collapsing
ratelimit.go   # token bucket rate limiting
acl.go         # IP/repo allow/block lists
socks5.go      # minimal SOCKS5 dialer
cache.go       # disk cache (TTL / per-file limit / total limit all hot-editable + auto eviction)
store.go       # token persistence + stats (owner attribution)
users.go       # user persistence (users.json, role + email + Docker path)
mail.go        # SMTP sending + email verification codes
captcha.go     # graphical captcha (dot-matrix rendering + validation)
invite.go      # invite-code data layer (generation / export / expiry / usage tracking)
mcp.go         # MCP server (HTTP / stdio, multi-user permission boundaries)
rewrite.go     # domain allowlist + URL rewriting
admin.go/templates.go  # admin dashboard (dual entries /admin + /user)
settings.go    # runtime settings persistence (switches + cache + SMTP/signup/captcha, gm-data/settings.json)
*_test.go      # unit tests (cache / users / invites / captcha / MCP / proxy username segment etc.)
```

## 📚 Docs

- `docs/docker-deploy.md`: generic Docker deployment, environment adaptation & conflict checklist, **correct setup for domain-fronting CDNs (EdgeOne/Cloudflare)**, **Docker acceleration security**
- `docs/deploy-runbook.md`: an on-target-machine deployment runbook (probe → choose → execute → end-to-end verify → update/rollback)
- `docs/upgrade-runbook.md`: one-click image upgrade / rollback script
- `docs/agent-prompt.md`: **deployment prompt for AI Agents** (rendered by the homepage "One-click AI Agent deployment", updated per release; can be overridden via `AGENT_PROMPT_FILE` or `gm-data/agent-prompt.md`)
- `docs/mcp.md`: **MCP setup guide** (HTTP `POST /ai/<username>/mcp` and `gh-mirror mcp` stdio, tool list, auth, security advice, troubleshooting)

## 🙏 Acknowledgements

This project is **partly inspired by [sky22333/hubproxy](https://github.com/sky22333/hubproxy) (MIT License · © 2025 sky22333)** — a multi-purpose acceleration service that likewise supports Docker image acceleration, GitHub acceleration and offline images.

gh-mirror was independently implemented in Go, inspired by its architecture, and extended with a web admin dashboard, token auth, disk cache, image search, link conversion and MCP. If the original project suits your scenario better, give hubproxy a try too.

This project is **not affiliated** with hubproxy. Any code reuse follows its MIT license with the corresponding notices retained.

### 🔗 Ecosystem recommendation: [KPanel](https://github.com/kejilion/KPanel)

If you also want to manage "host ops + automated project deployment" on the same machine, take a look at **[kejilion/KPanel](https://github.com/kejilion/KPanel)**:

- **Server AI Agent**: drive real operations on the host in natural language, without bouncing between panel, SSH and shell.
- **Small-host friendly**: **runs fine on a 1 GB RAM machine** — great for low-spec VPS, home servers and light production.
- **Automated project deployment**: hand the deployment flow to the agent, reducing manual steps and human error.
- **Orchestrated around one real host**: system, websites, Docker, files, clusters and the AI assistant all work on the same machine instead of each minding its own.

It complements gh-mirror nicely: gh-mirror makes "upstream access faster" (with a built-in MCP so agents can manage tokens, service switches and cache), while KPanel handles "the ops of the host and the projects themselves". Both aim to put AI agents to real work on a real server.

> Ecosystem recommendation only — not affiliated with this project and no warranty of any kind; evaluate before use.

## 📄 License

MIT (same license as the upstream project).

## ⭐ Star & Fork

If this project saves you time or solves a real download pain point, please consider supporting it on GitHub:

- ⭐ **Star**: <https://github.com/yufeng8/gh-mirror> — helps others discover it and motivates continued maintenance.
- 🍴 **Fork**: <https://github.com/yufeng8/gh-mirror/fork> — add features, customize the UI, or adapt it for your internal network.
- 🐛 **Issue / PR**: find a bug or have an idea? Share it at <https://github.com/yufeng8/gh-mirror/issues>.

> Your support keeps me motivated — thank you!

---

© 2026 gh-mirror · MIT License
