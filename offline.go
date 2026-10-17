package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// 离线镜像打包：无需本地 Docker，按需把镜像（manifest + config + layers）打包成
// 经典 docker-save 格式的 tar，供 `docker load` 使用。
// 说明：classic 格式要求 layer.tar 为未压缩 tar（与 config.rootfs.diff_id 对应），
// 因此这里把上游 gzip 层解压到临时文件后再写入 tar。建议用真实 docker load 校验。

// offlineInflight 同镜像并发防抖：已在打包则直接返回 429，避免重复打包风暴。
var offlineInflight sync.Map

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

func (s *Server) fetchManifest(r *http.Request, m registryMapping, repo, ref, token string) ([]byte, error) {
	target := "https://" + m.upstream + "/v2/" + repo + "/manifests/" + ref
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", manifestAccept)
	req.Header.Set("User-Agent", "github-mirror/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := dockerClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("上游返回 %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// handleOffline /offline/<用户名>/<token>/<镜像> 打包镜像为 docker-load 可用 tar 并流式下载。
// 多用户隔离：第一段是用户名段（必须对应真实用户），第二段是 token（必须归属该用户）。
// 缺失或无效一律按 404 处理，避免网关暴露后被白嫖带宽。
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
	if _, running := offlineInflight.LoadOrStore(image, struct{}{}); running {
		http.Error(w, "该镜像正在打包中，请稍候重试", http.StatusTooManyRequests)
		return
	}
	defer offlineInflight.Delete(image)

	token, err := s.getRegistryToken(r, m, repo)
	if err != nil {
		http.Error(w, "获取认证失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	manifestBytes, err := s.fetchManifest(r, m, repo, ref, token)
	if err != nil {
		http.Error(w, "获取 manifest 失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	var mf ociManifest
	if err := json.Unmarshal(manifestBytes, &mf); err != nil {
		http.Error(w, "解析 manifest 失败", http.StatusBadGateway)
		return
	}
	// 多架构 index：挑选一个平台后取其具体 manifest
	if len(mf.Manifests) > 0 {
		pick := pickPlatform(mf.Manifests)
		if pick == "" {
			http.Error(w, "未找到可用平台镜像", http.StatusBadGateway)
			return
		}
		manifestBytes, err = s.fetchManifest(r, m, repo, pick, token)
		if err != nil {
			http.Error(w, "获取平台 manifest 失败", http.StatusBadGateway)
			return
		}
		mf = ociManifest{}
		if err := json.Unmarshal(manifestBytes, &mf); err != nil {
			http.Error(w, "解析平台 manifest 失败", http.StatusBadGateway)
			return
		}
	}
	if mf.Config.Digest == "" || len(mf.Layers) == 0 {
		http.Error(w, "manifest 缺少 config 或 layers", http.StatusBadGateway)
		return
	}

	// 先取 config（写头之前完成，避免半途失败）
	cfgBytes, err := s.fetchBlob(r, m, repo, mf.Config.Digest, token)
	if err != nil {
		http.Error(w, "获取镜像 config 失败", http.StatusBadGateway)
		return
	}

	repoTag := repoTagName(m, repo, ref)
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeName(repoTag)+`.tar"`)
	w.WriteHeader(http.StatusOK)

	tw := tar.NewWriter(w)
	if err := writeTarBytes(tw, hexOf(mf.Config.Digest)+".json", cfgBytes); err != nil {
		log.Printf("offline: 写 config 失败: %v", err)
		return
	}
	for _, l := range mf.Layers {
		if err := s.writeLayerTar(tw, r, m, repo, l.Digest, token); err != nil {
			log.Printf("offline: 写 layer %s 失败: %v", l.Digest, err)
			return
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
		log.Printf("offline: 写 manifest.json 失败: %v", err)
		return
	}
	_ = tw.Close()
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
