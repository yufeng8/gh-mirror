<p align="center">
  <a href="https://github.com/yufeng8/gh-mirror">
    <img src="social-preview.png" alt="gh-mirror cover" width="100%">
  </a>
</p>

<h1 align="center">
  <img src="logo.svg" width="40" alt="logo">
  gh-mirror
</h1>

> Self-hosted GitHub resource mirror / acceleration proxy · Token auth · Web admin dashboard · Fully private deployment

<p align="center">
  <strong>English</strong> | <a href="README.md">中文</a>
</p>

[![Release](https://img.shields.io/github/v/release/yufeng8/gh-mirror)](https://github.com/yufeng8/gh-mirror/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io-blue?logo=docker&logoColor=white)](https://github.com/yufeng8/gh-mirror/pkgs/container/gh-mirror)
[![docker workflow](https://github.com/yufeng8/gh-mirror/actions/workflows/docker.yml/badge.svg)](https://github.com/yufeng8/gh-mirror/actions/workflows/docker.yml)
[![build](https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml/badge.svg)](https://github.com/yufeng8/gh-mirror/actions/workflows/build.yml)
[![License](https://img.shields.io/github/license/yufeng8/gh-mirror)](./LICENSE)
[![GitHub Stars](https://img.shields.io/github/stars/yufeng8/gh-mirror?style=social)](https://github.com/yufeng8/gh-mirror/stargazers)

**gh-mirror** is an open-source, fully self-hostable **GitHub resource mirror / acceleration proxy**. Prefix any full GitHub URL with `/g/<token>/` on your own domain to speed up downloads for both browsers and CLI tools (`curl` / `wget`). The backend is a streaming reverse proxy built with the Go standard library, featuring on-disk caching with `Range` resume support, a built-in web admin dashboard, and per-token access control.

> Example deployment domain in this doc is `mirror.example.com`; replace it with your own domain.

### Highlights
- **Token auth** — every proxied URL carries its own token; unauthorized requests return 404.
- **Usage stats** — per-token hit counts, traffic, and last-seen, persisted to `tokens.json`.
- **Admin UI** — manage tokens and view stats from a clean web dashboard.
- **Smart cache** — caches `GET`s to reduce upstream load; serializes concurrent fetches to avoid the thundering herd.
- **Multi-protocol rewrite** — `raw.githubusercontent.com` / `github.com` / `releases` / `archive` all routed through gh-mirror.
- **One-click deploy** — ships `deploy.sh`, `Dockerfile`, a systemd unit, and an nginx reverse-proxy config.

> ⚠️ **Compliance & Disclaimer (please read first)**: This project is intended only to accelerate GitHub resources that you are **authorized to access**. Do not use it to steal private repositories, bypass payment or access restrictions, or violate the GitHub Terms of Service or any local laws. You assume full responsibility for any misuse. This project does not cache or record request content; it is not responsible for the copyright or compliance of accelerated resources. See the [Compliance & Disclaimer](#compliance--disclaimer) section below.

## Why you need it

- **Slow / timeout GitHub access**: In some networks, GitHub is slow, times out frequently, or downloads are interrupted. Deploy gh-mirror on a node with good GitHub connectivity and use it as a fast local cache.
- **Unstable CI/CD**: Build machines pulling dependencies or releases are often rate-limited or interrupted by GitHub. Use gh-mirror as a stable acceleration entry point.
- **jsDelivr limitations**: `cdn.jsdelivr.net/gh/...` only accelerates public source files (~20 MB per file) and does not support Releases / archive / API. gh-mirror covers all scenarios.
- **Anti-abuse**: Public proxies are uncontrollable. gh-mirror requires a token issued by the admin panel for every request; invalid tokens return 404, so access is controllable and auditable.
- **No third-party dependency**: All traffic and tokens stay on your own server. One-click deployment keeps everything under your control.

## Core features

- **Fully private deployment**: Deploy natively or via Docker on your own server; data stays with you, no third-party proxy.
- **Token authentication**: The admin panel issues tokens. Every proxied request must carry a valid token; invalid tokens return 404, preventing open-proxy abuse.
- **Full-scenario acceleration**: Covers blob/raw files, raw files, release binaries, archives, and short links (`gh/...`).
- **Streaming reverse proxy + disk cache + Range resume**: Built on the Go standard library; performance is close to native deployment.
- **Web admin dashboard**: Visualize, add, and delete tokens; each token page shows the instance proxy example URL.
- **One-click deploy**: Native (`native`) and Docker modes; `deploy.sh` and `install.sh` work out of the box.
- **Multi-arch Docker image**: Automatically built and published to GitHub Container Registry (amd64 / arm64); CI automatically sets the package to **Public**, so anonymous `docker pull` just works.

## Table of contents

- [Access rules](#access-rules)
- [Admin dashboard](#admin-dashboard)
- [About jsDelivr `gh`](#about-jsdelivr-gh)
- [Build locally](#build-locally)
- [One-click deploy (Ubuntu 24.04)](#one-click-deploy-ubuntu-2404)
- [One-click install (from GitHub Release)](#one-click-install-from-github-release-no-source-needed)
- [Docker image (GHCR auto-build)](#docker-image-ghcr-auto-build)
- [Manual deploy (Ubuntu 24.04)](#manual-deploy-ubuntu-2404)
- [Cache cleanup](#cache-cleanup)
- [Environment variables](#environment-variables)
- [Security notes](#security-notes)
- [Compliance & Disclaimer](#compliance--disclaimer)
- [Contributing](#contributing)
- [Acknowledgements](#acknowledgements)
- [License](#license)
- [⭐ Star & Fork guide](#-star--fork-guide)

## Access rules

All proxy requests **must carry a valid token**. The URL format is:

```
https://mirror.example.com/g/<token>/<full GitHub URL>
```

| Scenario | URL example |
|----------|-------------|
| Repository file (blob/raw page) | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/blob/main/path/file.txt` |
| Raw file | `https://mirror.example.com/g/<token>/https://raw.githubusercontent.com/owner/repo/main/file.txt` |
| Release binary | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip` |
| Repository archive | `https://mirror.example.com/g/<token>/https://github.com/owner/repo/archive/refs/heads/main.zip` |
| Short link | `https://mirror.example.com/g/<token>/gh/owner/repo/raw/main/file.txt` |

**Requests without a valid token (or a token not in the admin panel) will return 404.** The previous "append the GitHub URL directly" pattern is no longer supported.

CLI example:
```bash
curl -L -o app.zip "https://mirror.example.com/g/<token>/https://github.com/owner/repo/releases/download/v1.0/app.zip"
```

Response header `X-Cache: HIT` means the cache was hit; `MISS` means it fetched from upstream.

## Admin dashboard

- Default path `/admin` (customizable via `ADMIN_PATH`).
- Default credentials: `admin` / `admin` (set during deployment; credentials and secrets are written to `/etc/gh-mirror.env`).
- Login uses a signed session cookie (HMAC + 7-day expiry, HttpOnly + SameSite=Strict).
- Dashboard capabilities:
  - View / add / delete tokens (leave token empty to auto-generate a random value, and add a note).
  - Each page shows the instance proxy example URL.

The homepage (`/`) shows public usage instructions and does not expose anything outside the admin path.

## About jsDelivr `gh`

`cdn.jsdelivr.net/gh/user/repo@branch/path` accelerates only **public repository source files**, about 20 MB per file, and cannot handle arbitrary GitHub URLs (releases / archive / API). gh-mirror connects directly to GitHub and covers all scenarios. If you want jsDelivr edge caching to save server bandwidth, set `USE_JSDELIVR=1` (only applies to blob/raw).

## Build locally

```bash
# Cross-compile for Linux from Windows (target arm64 / amd64)
set GOOS=linux
set GOARCH=arm64
go build -o gh-mirror .
```

## One-click deploy (Ubuntu 24.04)

Upload the whole project directory to your server and run as root:

```bash
sudo bash deploy.sh
```

The script interactively asks for **domain**, **certificate email**, **deploy mode**, and **admin password** (default `admin`), and auto-generates a session secret; then it finishes the installation.

- `native` (default): install Go → build → create user/directories (including token data dir) → register systemd service → configure nginx → firewall → request certificate.
- `docker`: install Docker → build image for the host arch → start container with `--network=host` + cache volume + data volume → configure nginx → firewall → request certificate.

> Container mode uses host networking to avoid bridge NAT overhead; cache and token data are mounted as volumes to avoid writing to the overlay layer, so performance is close to native.
> For resource-constrained small-memory instances (e.g. 1 GB), `native` is recommended (saves the ~100–200 MB used by the Docker daemon).

Prerequisites: the domain A record points to the server public IP, and ports 80/443 are open.

## One-click install (from GitHub Release, no source needed)

Suitable for fresh servers where you do not want to upload project source: the script automatically downloads the latest prebuilt binary from GitHub and finishes service + reverse-proxy configuration.

```bash
# Interactive (asks for domain / email / password)
bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)

# Non-interactive / one-liner
DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='your-password' \
  bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
```

Optional environment variables:

| Variable | Description |
|----------|-------------|
| `MODE` | `native` (default) / `docker` |
| `VERSION` | Native mode: version tag (e.g. `v1.0.0`), defaults to latest. Docker mode: image tag (defaults to `latest`); when `DOCKER_BUILD=1`, it is the source branch/tag (default `main`). |
| `DOCKER_BUILD` | Docker mode only: set to `1` to build the image from source on the server instead of pulling the GHCR image. |
| `GITHUB_TOKEN` | **Required for private forks**: PAT with `repo` (read) + `read:packages` scope, used to pull scripts/releases and to log in to GHCR for private images. |
| `GHCR_USER` | GHCR login username (default `yufeng8`), should match the account that owns `GITHUB_TOKEN`. |
| `GH_PROXY` | Download acceleration prefix (e.g. `https://ghproxy.com/`), useful in networks with slow GitHub access. |
| `SKIP_NGINX=1` | Install service only; skip nginx/TLS configuration (use when you reverse-proxy manually). |
| `ADMIN_USER` / `ADMIN_PATH` | Custom admin username / admin path. |

Native mode flow: detect architecture → download `gh-mirror-linux-<arch>` → create user/directories → write `/etc/gh-mirror.env` → register systemd service → (by default) install nginx + request certificate + open firewall → configure cache cleanup cron → self-check.

> **Public vs private fork**: The upstream repository `yufeng8/gh-mirror` is **public**, so the `curl ... | bash` one-liner works without any token.
> If you **fork this project into a private repository** and deploy from there, both `raw.githubusercontent.com` and Releases require authentication. Use the token-included command below (the token will remain on the server; make sure the PAT only has the necessary scopes):
>
> ```bash
> GITHUB_TOKEN=ghp_xxx bash -c "$(curl -fsSL -H \"Authorization: Bearer $GITHUB_TOKEN\" \
>   https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)"
> ```
>
> This command fetches the script with the token and runs it with the same token; internally it queries/downloads Releases with the `Authorization` header (native mode) and uses `docker login ghcr.io` to pull private images (Docker mode).
>
> **Required PAT scopes**:
> - `repo` (content read): needed for fetching scripts from a private repo, querying/downloading Releases via `gh_curl`, and cloning source when `DOCKER_BUILD=1`.
> - `read:packages`: needed to log in to GHCR and pull **private** images (public images can be pulled anonymously, so this is not needed for the upstream repo).
>
> **Self-check**: at runtime the script calls the GitHub API to read `X-OAuth-Scopes` and warns you early if a required scope is missing; if `docker login` fails, it explicitly points out that the most likely missing scope is `read:packages` and prints the raw error. If permissions are insufficient, regenerate the PAT with the required scopes and rerun.

### Docker mode

If your server prefers Docker, add `MODE=docker` (the script installs Docker, pulls the ready-made GHCR image, and runs the container; nginx still reverse-proxies it; remember to use `GITHUB_TOKEN` for private forks):

```bash
MODE=docker DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='your-password' \
  bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
```

### Uninstall

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/uninstall.sh)
# Add --purge to also remove data (tokens/cache/user); add -y to skip confirmation.
```

## Docker image (GHCR auto-build)

The repository uses GitHub Actions (`.github/workflows/docker.yml`): pushing to `main` tags `latest`, pushing a `v*` tag (e.g. `v1.0.0`) tags the same version, and pushes a **multi-arch image (amd64/arm64)** to GitHub Container Registry:

```
ghcr.io/yufeng8/gh-mirror:latest
ghcr.io/yufeng8/gh-mirror:v1.0.0
```

After each build, CI **automatically sets the package to Public** (see the "Set package visibility to public" step in `docker.yml`), so anonymous servers can pull without logging in:

```bash
docker pull ghcr.io/yufeng8/gh-mirror:latest
```

`install.sh` with `MODE=docker` **pulls this public image by default**, so no source compilation is needed on the server:

```bash
MODE=docker DOMAIN=mirror.example.com EMAIL=you@example.com ADMIN_PASS='xxx' \
  bash <(curl -fsSL https://raw.githubusercontent.com/yufeng8/gh-mirror/main/install.sh)
# Pin version: MODE=docker VERSION=v1.0.0 ...
# Build from source (fallback): MODE=docker DOCKER_BUILD=1 ...
```

> Note: if you **fork to a private repo** and build yourself, CI will not make the package Public by default. You must manually change the package visibility to Public in the Packages settings, or use `gh api --method PATCH "user/packages/container/<your-package-name>/visibility" -f visibility=public` (requires a PAT with `write:packages` scope).

## Manual deploy (Ubuntu 24.04)

1. **DNS**: Point `mirror.example.com` A record to the server public IP; open ports 80/443.

2. **Prepare directories and user**:
   ```bash
   sudo useradd -r -s /usr/sbin/nologin ghmirror
   sudo mkdir -p /opt/gh-mirror /var/cache/gh-mirror /var/lib/gh-mirror
   sudo chown -R ghmirror:ghmirror /var/cache/gh-mirror /var/lib/gh-mirror
   ```

3. **Upload binary**: Copy `gh-mirror` to `/opt/gh-mirror/gh-mirror`, `chmod +x`, and set owner to `ghmirror`.

4. **Write config** (`/etc/gh-mirror.env`, mode 600):
   ```bash
   sudo tee /etc/gh-mirror.env >/dev/null <<'EOF'
   ADMIN_USER=admin
   ADMIN_PASS=admin
   ADMIN_PATH=/admin
   ADMIN_SECRET=$(openssl rand -hex 32)
   COOKIE_SECURE=true
   TOKENS_FILE=/var/lib/gh-mirror/tokens.json
   LISTEN=127.0.0.1:8080
   CACHE_DIR=/var/cache/gh-mirror
   CACHE_TTL_MINUTES=1440
   MAX_CACHE_MB=200
   UPSTREAM_TIMEOUT=60
   EOF
   sudo chmod 600 /etc/gh-mirror.env
   ```

5. **Register service**:
   ```bash
   sudo cp gh-mirror.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now gh-mirror
   sudo systemctl status gh-mirror
   ```

6. **nginx**:
   ```bash
   sudo cp nginx/gh-mirror.conf /etc/nginx/sites-available/
   sudo ln -s /etc/nginx/sites-available/gh-mirror.conf /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

7. **HTTPS (Let's Encrypt)**:
   ```bash
   sudo apt install -y certbot python3-certbot-nginx
   sudo certbot --nginx -d mirror.example.com
   ```

8. **Log in to admin and add a token**: Visit `https://mirror.example.com/admin`, log in with admin/admin, add a token, then use `/g/<token>/...`.

## Cache cleanup

Cache entries expire by TTL (default 1 day), but files are not deleted automatically. `deploy.sh` automatically configures cleanup during deployment:

- Native mode: writes `/etc/cron.d/gh-mirror`, every day at 04:10 it deletes cache files older than 7 days and `.tmp` leftovers older than 1 day in `${CACHE_DIR}`.
- Docker mode: also writes `/etc/cron.d/gh-mirror`, every day at 04:10 it starts a one-off `busybox` container mounting the `gh-mirror-cache` named volume and cleans files older than 7 days.

To adjust, simply edit `/etc/cron.d/gh-mirror`.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN` | `127.0.0.1:8080` | Listen address (localhost only; nginx reverse-proxies) |
| `CACHE_DIR` | `./cache` | Cache directory |
| `CACHE_TTL_MINUTES` | `1440` | Cache TTL in minutes |
| `MAX_CACHE_MB` | `200` | Files larger than this are not cached |
| `UPSTREAM_TIMEOUT` | `60` | Upstream request timeout in seconds |
| `USE_JSDELIVR` | `0` | Set to `1` to route blob/raw through jsDelivr edge cache |
| `ADMIN_PATH` | `/admin` | Admin dashboard path (must start with `/`) |
| `ADMIN_USER` | `admin` | Admin username |
| `ADMIN_PASS` | `admin` | Admin password |
| `ADMIN_SECRET` | random | Session signing secret (recommend setting via env so it survives restarts) |
| `COOKIE_SECURE` | `true` | Whether the session cookie has the Secure flag (keep `true` under HTTPS) |
| `TOKENS_FILE` | `./tokens.json` | Token persistence file path |

## Security notes

- Only upstream GitHub-related domains are allowed (whitelist in `rewrite.go`); it cannot be abused as an open proxy.
- Proxy requests must carry a token issued by the admin panel; invalid tokens return 404. **Treat the token list as a credential; keep `tokens.json` and `ADMIN_SECRET` safe.**
- Admin login uses constant-time comparison; session cookies are HMAC-signed, HttpOnly, and SameSite=Strict.
- For further hardening, add nginx-level access restrictions or Basic Auth on `/admin`, and rotate the admin password regularly.

## Compliance & Disclaimer

- This project is a **neutral technical tool** intended only to accelerate GitHub resources that you are **authorized to access**.
- **Strictly prohibited** — you assume full legal responsibility for any of the following:
  - Stealing, scraping, or accessing private repositories / private resources you do not own or are not authorized to access;
  - Circumventing payment, bypassing vendor access restrictions or paywalls;
  - Any behavior that violates the GitHub [Acceptable Use Policies](https://docs.github.com/site-policy/github-acceptable-use-policies) or local laws;
  - Using this service as an open proxy for attacks or abuse.
- This project **does not cache or record** the actual content of requests; the copyright and compliance of accelerated resources are the responsibility of the user.
- If your network/region restricts access to GitHub, please make sure local regulations permit such use before deploying.

## Contributing

Please develop new features or fixes on a `feat/` / `fix/` branch and open a PR to `main`. It merges after CI passes (+ 1 review). See [CONTRIBUTING.md](./CONTRIBUTING.md).

## Acknowledgements

Parts of the design are **inspired by [sky22333/hubproxy](https://github.com/sky22333/hubproxy) (MIT License · © 2025 sky22333)** — a multi-purpose acceleration service that also covers Docker image acceleration, GitHub acceleration, and offline images.

gh-mirror takes that inspiration and is **independently implemented in Go**, extending it with a web admin dashboard, token-based auth, on-disk caching, image search, link conversion, and MCP support. If the original project fits your scenario better, it is worth a try as well.

This project is **not affiliated** with hubproxy. Any reuse of its code follows the MIT license and keeps the corresponding notices.

### Ecosystem recommendation: [KPanel](https://github.com/kejilion/KPanel)

If you also want to manage **host operations together with automated project deployment** on the same machine, take a look at **[kejilion/KPanel](https://github.com/kejilion/KPanel)**:

- **Server-side AI Agent**: drive real operations on a real host in natural language — no more jumping between the panel, SSH, and the shell.
- **Friendly to small hosts**: **runs fine on a 1 GB RAM box**, which suits low-end VPS instances, home servers, and lightweight production setups.
- **Automated project deployment**: hand the deployment flow to the agent and cut down manual steps (and human error).
- **One real host, working together**: system, websites, Docker, files, clusters, and the AI assistant are wired to the same machine instead of being separate silos.

It complements gh-mirror nicely: gh-mirror makes **upstream resources faster** (with built-in MCP so an agent can manage tokens, service toggles, and cache directly), while KPanel handles **orchestration for the host and the projects on it**. Both share the same idea — let an AI agent actually get work done on a real server.

> Community recommendation only — not affiliated with this project, and provided with no warranty of any kind. Evaluate it yourself before use.

## License

This project is open source under the **MIT License**. See [LICENSE](./LICENSE) for details.

You are free to use, modify, and distribute this software, including for commercial purposes; please retain the copyright and license notices. The software is provided "as is", without warranty.

## ⭐ Star & Fork guide

If this project saves you time or solves a real download pain point, please consider supporting it on GitHub:

- ⭐ **Star**: <https://github.com/yufeng8/gh-mirror> — helps others discover it and motivates continued maintenance.
- 🍴 **Fork**: <https://github.com/yufeng8/gh-mirror/fork> — add features, customize the UI, or adapt it for your internal network.
- 🐛 **Issue / PR**: Find a bug or have an idea? Share it at <https://github.com/yufeng8/gh-mirror/issues>.

> Tip: the deployed homepage (`/`) also has a "Star on GitHub" button at the bottom for easy access.

---

© 2026 gh-mirror · MIT License
