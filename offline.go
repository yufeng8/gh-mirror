package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// 离线镜像打包：无需本地 Docker，按需把镜像（manifest + config + layers）打包成
// 经典 docker-save 格式的 tar，供 `docker load` 使用。
// 说明：classic 格式要求 layer.tar 为未压缩 tar（与 config.rootfs.diff_id 对应），
// 因此这里把上游 gzip 层解压到临时文件后再写入 tar。建议用真实 docker load 校验。

type ociDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Platform  *struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform,omitempty"`
}

type ociManifest struct {
	MediaType string          `json:"mediaType"`
	Config    ociDescriptor   `json:"config"`
	Layers    []ociDescriptor `json:"layers"`
	Manifests []ociDescriptor `json:"manifests"` // index/list 时使用
}

type registryToken struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.oci.image.index.v1+json"

// parseImageRef 解析 [domain/]repo[:tag|@digest]，返回上游映射、仓库名与引用。
func parseImageRef(s string) (registryMapping, string, string) {
	m := dockerHub
	rest := strings.TrimSpace(s)
	for _, r := range extraRegistries {
		if r.domain != "" && strings.HasPrefix(rest, r.domain+"/") {
			m = r
			rest = strings.TrimPrefix(rest, r.domain+"/")
			break
		}
	}
	ref := "latest"
	if at := strings.Index(rest, "@"); at >= 0 {
		ref = rest[at+1:]
		rest = rest[:at]
	} else if c := strings.LastIndex(rest, ":"); c >= 0 && !strings.Contains(rest[c+1:], "/") {
		ref = rest[c+1:]
		rest = rest[:c]
	}
	repo := strings.Trim(rest, "/")
	if m.domain == "" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return m, repo, ref
}

// repoTagName 生成 docker load 的 RepoTags（Docker Hub 官方镜像去掉 library/ 前缀）。
func repoTagName(m registryMapping, repo, ref string) string {
	name := repo
	if m.domain == "" {
		name = strings.TrimPrefix(repo, "library/")
	}
	tag := ref
	if strings.HasPrefix(ref, "sha256:") {
		tag = ref[len("sha256:"):][:12]
	}
	return name + ":" + tag
}

func hexOf(digest string) string {
	if i := strings.Index(digest, ":"); i >= 0 {
		return digest[i+1:]
	}
	return digest
}

func pickPlatform(manifests []ociDescriptor) string {
	for _, want := range [][2]string{{"amd64", "linux"}, {"arm64", "linux"}, {"", "linux"}} {
		for _, mm := range manifests {
			if mm.Platform == nil {
				continue
			}
			if mm.Platform.OS == want[1] && (want[0] == "" || mm.Platform.Architecture == want[0]) {
				return mm.Digest
			}
		}
	}
	if len(manifests) > 0 {
		return manifests[0].Digest
	}
	return ""
}

func (s *Server) doAuthed(r *http.Request, method, target, token, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), method, target, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "github-mirror/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return dockerClient.Do(req)
}

func (s *Server) getRegistryToken(r *http.Request, m registryMapping, repo string) (string, error) {
	if m.authURL == "" {
		return "", nil // 匿名 registry
	}
	scope := "repository:" + repo + ":pull"
	target := "https://" + m.authURL + "?service=" + url.QueryEscape(m.service) + "&scope=" + url.QueryEscape(scope)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "github-mirror/1.0")
	resp, err := dockerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr registryToken
	_ = json.Unmarshal(body, &tr)
	if tr.Token == "" {
		tr.Token = tr.AccessToken
	}
	return tr.Token, nil
}

// fetchManifest 拉取 manifest，返回 (body, digest)。digest 优先取响应头 Docker-Content-Digest，
// 缺失时对 body 计算 sha256 兜底（作为内容寻址缓存的键）。
func (s *Server) fetchManifest(r *http.Request, m registryMapping, repo, ref, token string) ([]byte, string, error) {
	target := "https://" + m.upstream + "/v2/" + repo + "/manifests/" + ref
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", manifestAccept)
	req.Header.Set("User-Agent", "github-mirror/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := dockerClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("上游返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		sum := sha256.Sum256(body)
		digest = "sha256:" + hex.EncodeToString(sum[:])
	}
	return body, digest, nil
}

// resolveManifest 解析镜像 → 平台选定后的 manifest digest 与具体 manifest（内容寻址缓存键）。
// 多架构 index 会挑选一个平台并取其具体 manifest。供实时下载与后台异步重拉复用。
func (s *Server) resolveManifest(req *http.Request, m registryMapping, repo, ref, token string) (string, ociManifest, error) {
	manifestBytes, digest, err := s.fetchManifest(req, m, repo, ref, token)
	if err != nil {
		return "", ociManifest{}, fmt.Errorf("获取 manifest 失败: %w", err)
	}
	var mf ociManifest
	if err := json.Unmarshal(manifestBytes, &mf); err != nil {
		return "", ociManifest{}, fmt.Errorf("解析 manifest 失败: %w", err)
	}
	// 多架构 index：挑选一个平台后取其具体 manifest（digest 改为平台 manifest 的 digest）
	if len(mf.Manifests) > 0 {
		pick := pickPlatform(mf.Manifests)
		if pick == "" {
			return "", ociManifest{}, fmt.Errorf("未找到可用平台镜像")
		}
		digest = pick
		manifestBytes, _, err = s.fetchManifest(req, m, repo, pick, token)
		if err != nil {
			return "", ociManifest{}, fmt.Errorf("获取平台 manifest 失败: %w", err)
		}
		mf = ociManifest{}
		if err := json.Unmarshal(manifestBytes, &mf); err != nil {
			return "", ociManifest{}, fmt.Errorf("解析平台 manifest 失败: %w", err)
		}
	}
	if mf.Config.Digest == "" || len(mf.Layers) == 0 {
		return "", ociManifest{}, fmt.Errorf("manifest 缺少 config 或 layers")
	}
	return digest, mf, nil
}

// handleOffline /offline/<用户名>/<token>/<镜像> 打包镜像为 docker-load 可用 tar 并下载。
// 内容寻址缓存：以平台选定后的 manifest digest 为键，命中直接返回缓存 tar；未命中回源打包并落盘。
// 多用户隔离：第一段用户名段（对应真实用户），第二段 token（归属该用户），缺失或无效 404。
// 也兼容 /offline/<用户名>/<token>?image=<镜像名> 的查询串写法。
func (s *Server) handleOffline(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/offline/"))
	seg, tok, image := "", "", ""
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) >= 2 {
		seg, tok = parts[0], parts[1]
		if len(parts) == 3 {
			image = strings.TrimSpace(parts[2])
		}
	}
	if image == "" {
		image = strings.TrimSpace(r.URL.Query().Get("image"))
	}
	// 用户名段反查用户表：不存在统一 404
	user, userOK := s.userByDockerSegment(seg)
	if !userOK {
		s.render404(w, r)
		return
	}
	// token 必须存在且归属该用户（空 owner 归默认管理员）
	t, exists := s.store.Get(tok)
	if !exists || (t.Owner != user && !(t.Owner == "" && user == s.adminUser)) {
		s.render404(w, r)
		return
	}
	if image == "" {
		http.Error(w, "缺少镜像名，用法：/offline/<用户名>/<token>/library/nginx:1.25", http.StatusBadRequest)
		return
	}
	m, repo, ref := parseImageRef(image)
	if ok, reason := s.acl.CheckRepo(repo); !ok {
		http.Error(w, "镜像访问被限制: "+reason, http.StatusForbidden)
		return
	}

	token, err := s.getRegistryToken(r, m, repo)
	if err != nil {
		http.Error(w, "获取认证失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	// 解析 manifest → 得到平台选定后的 manifest digest（内容寻址缓存键）
	digest, mf, err := s.resolveManifest(r, m, repo, ref, token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	repoTag := repoTagName(m, repo, ref)

	// 内容寻址缓存：命中（ready 且 tar 文件存在）直接返回缓存文件
	l := s.offlineCache.LockFor(digest)
	l.RLock()
	if p, ok := s.offlineCache.Get(digest); ok && p.State == OfflineReady {
		if _, statErr := os.Stat(s.offlineCache.TarPath(digest)); statErr == nil {
			l.RUnlock()
			s.serveOfflineTar(w, r, s.offlineCache.TarPath(digest), repoTag)
			return
		}
	}
	l.RUnlock()

	// 未命中：回源打包 + tee 落盘（写锁避免同一 digest 并发重复打包）
	l.Lock()
	defer l.Unlock()
	// 双重检查：等锁期间可能已被其它请求打包完成
	if p, ok := s.offlineCache.Get(digest); ok && p.State == OfflineReady {
		if _, statErr := os.Stat(s.offlineCache.TarPath(digest)); statErr == nil {
			s.serveOfflineTar(w, r, s.offlineCache.TarPath(digest), repoTag)
			return
		}
	}
	s.offlineCache.MarkBuilding(digest, image, ref)

	// 先取 config（写头与落盘之前完成，避免半途失败）
	cfgBytes, err := s.fetchBlob(r, m, repo, mf.Config.Digest, token)
	if err != nil {
		s.offlineCache.MarkFailed(digest)
		http.Error(w, "获取镜像 config 失败", http.StatusBadGateway)
		return
	}

	tmpPath := s.offlineCache.TarPath(digest) + ".tmp"
	tmp, err := os.Create(tmpPath)
	if err != nil {
		s.offlineCache.MarkFailed(digest)
		http.Error(w, "创建缓存文件失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeName(repoTag)+`.tar"`)
	w.WriteHeader(http.StatusOK)
	// 同时写响应与缓存临时文件
	if err := s.buildOfflineTar(io.MultiWriter(w, tmp), r, m, repo, ref, token, mf, cfgBytes); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		s.offlineCache.MarkFailed(digest)
		log.Printf("offline: 打包 %s 失败: %v", image, err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		s.offlineCache.MarkFailed(digest)
		return
	}
	// 原子替换为正式缓存文件并记录清单
	if fi, statErr := os.Stat(tmpPath); statErr == nil {
		_ = os.Rename(tmpPath, s.offlineCache.TarPath(digest))
		s.offlineCache.MarkReady(digest, fi.Size())
	}
}

// serveOfflineTar 返回缓存好的 tar 文件（响应头与实时打包一致）。
func (s *Server) serveOfflineTar(w http.ResponseWriter, r *http.Request, path, repoTag string) {
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeName(repoTag)+`.tar"`)
	http.ServeFile(w, r, path)
}

// repullOffline 后台异步重新打包镜像（含 tag）到离线缓存，由超管在缓存管理页触发。
// 重新回源解析 manifest 得到 digest；digest 变则新增缓存、不变则覆盖（内容寻址自然语义）。
func (s *Server) repullOffline(image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://offline-internal", nil)
		m, repo, ref := parseImageRef(image)
		if ok, reason := s.acl.CheckRepo(repo); !ok {
			log.Printf("offline: 重拉被拒绝 %s: %s", image, reason)
			return
		}
		token, err := s.getRegistryToken(req, m, repo)
		if err != nil {
			log.Printf("offline: 重拉 %s 获取认证失败: %v", image, err)
			return
		}
		digest, mf, err := s.resolveManifest(req, m, repo, ref, token)
		if err != nil {
			log.Printf("offline: 重拉 %s 解析失败: %v", image, err)
			return
		}
		s.offlineCache.StartTask(digest, image, OfflineTaskBuilding)
		l := s.offlineCache.LockFor(digest)
		l.Lock()
		defer l.Unlock()
		s.offlineCache.MarkBuilding(digest, image, ref)
		cfgBytes, err := s.fetchBlob(req, m, repo, mf.Config.Digest, token)
		if err != nil {
			s.offlineCache.MarkFailed(digest)
			s.offlineCache.FinishTask(digest, OfflineTaskFailed, err.Error())
			return
		}
		tmpPath := s.offlineCache.TarPath(digest) + ".tmp"
		tmp, err := os.Create(tmpPath)
		if err != nil {
			s.offlineCache.MarkFailed(digest)
			s.offlineCache.FinishTask(digest, OfflineTaskFailed, err.Error())
			return
		}
		if err := s.buildOfflineTar(tmp, req, m, repo, ref, token, mf, cfgBytes); err != nil {
			tmp.Close()
			_ = os.Remove(tmpPath)
			s.offlineCache.MarkFailed(digest)
			s.offlineCache.FinishTask(digest, OfflineTaskFailed, err.Error())
			return
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			s.offlineCache.MarkFailed(digest)
			s.offlineCache.FinishTask(digest, OfflineTaskFailed, err.Error())
			return
		}
		if fi, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Rename(tmpPath, s.offlineCache.TarPath(digest))
			s.offlineCache.MarkReady(digest, fi.Size())
		}
		s.offlineCache.FinishTask(digest, OfflineTaskDone, "")
		log.Printf("offline: 后台重拉完成 %s -> %s", image, digest)
	}()
}

// buildOfflineTar 把 manifest（config + layers）打包成 docker-save 格式的 tar 写入 w。
func (s *Server) buildOfflineTar(w io.Writer, r *http.Request, m registryMapping, repo, ref, token string, mf ociManifest, cfgBytes []byte) error {
	repoTag := repoTagName(m, repo, ref)
	tw := tar.NewWriter(w)
	if err := writeTarBytes(tw, hexOf(mf.Config.Digest)+".json", cfgBytes); err != nil {
		return err
	}
	for _, l := range mf.Layers {
		if err := s.writeLayerTar(tw, r, m, repo, l.Digest, token); err != nil {
			return err
		}
	}
	// manifest.json（Layers 顺序即 manifest layers 顺序）
	type mjEntry struct {
		Config   string   `json:"Config"`
		RepoTags []string `json:"RepoTags"`
		Layers   []string `json:"Layers"`
	}
	layers := make([]string, 0, len(mf.Layers))
	for _, l := range mf.Layers {
		layers = append(layers, hexOf(l.Digest)+"/layer.tar")
	}
	mj, _ := json.Marshal([]mjEntry{{Config: hexOf(mf.Config.Digest) + ".json", RepoTags: []string{repoTag}, Layers: layers}})
	if err := writeTarBytes(tw, "manifest.json", mj); err != nil {
		return err
	}
	return tw.Close()
}

func (s *Server) fetchBlob(r *http.Request, m registryMapping, repo, digest, token string) ([]byte, error) {
	target := "https://" + m.upstream + "/v2/" + repo + "/blobs/" + digest
	resp, err := s.doAuthed(r, http.MethodGet, target, token, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("上游返回 %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// writeLayerTar 拉取层（可能 gzip）→ 解压到临时文件 → 以未压缩 tar 写入（匹配 diff_id）。
func (s *Server) writeLayerTar(tw *tar.Writer, r *http.Request, m registryMapping, repo, digest, token string) error {
	target := "https://" + m.upstream + "/v2/" + repo + "/blobs/" + digest
	resp, err := s.doAuthed(r, http.MethodGet, target, token, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("层上游返回 %d", resp.StatusCode)
	}

	dir := s.cfg.CacheDir
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "offline-layer-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	br := bufio.NewReader(resp.Body)
	var src io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, gerr := gzip.NewReader(br)
		if gerr != nil {
			tmp.Close()
			return gerr
		}
		defer gz.Close()
		src = gz
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return err
	}
	f, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeTarStream(tw, hexOf(digest)+"/layer.tar", f, info.Size())
}

func writeTarBytes(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func writeTarStream(tw *tar.Writer, name string, r io.Reader, size int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: size, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.Copy(tw, r)
	return err
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	if s == "" {
		s = "image"
	}
	return s
}
