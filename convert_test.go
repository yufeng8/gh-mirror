package main

import "testing"

// TestBuildAccelURLSchemeLess 覆盖「从浏览器地址栏复制、省略 https://」的常见输入。
// 这类输入历史上被误判成 Docker 镜像名（生成 /offline/ 路径），而后台说明里
// 明确写着支持 github.com/owner/repo/... 这种写法，属于文案与实现不一致。
func TestBuildAccelURLSchemeLess(t *testing.T) {
	s := &Server{adminUser: "admin"}
	s.dockerOn = true
	s.hfOn = true
	const (
		base = "https://mirror.example.com"
		tk   = "TOKEN123"
		user = "alice"
	)
	cases := []struct {
		name     string
		in       string
		wantKind string
		wantPath string // URL 必须包含的片段
	}{
		{"GitHub releases 无 scheme", "github.com/yufeng8/gh-mirror/releases/download/v1.4.4/gh-mirror-linux-amd64", "github", "/g/alice/TOKEN123/"},
		{"GitHub blob 无 scheme", "github.com/yufeng8/gh-mirror/blob/main/README.md", "github", "/g/alice/TOKEN123/"},
		{"raw 无 scheme", "raw.githubusercontent.com/a/b/main/f.bin", "github", "/g/alice/TOKEN123/"},
		{"GitHub 带 scheme", "https://github.com/yufeng8/gh-mirror/blob/main/README.md", "github", "/g/alice/TOKEN123/"},
		{"HuggingFace 无 scheme", "huggingface.co/openai-community/gpt2/resolve/main/config.json", "hf", "/hf/TOKEN123/openai-community/gpt2/resolve/main/config.json"},
		{"HF blob 无 scheme 规范化", "huggingface.co/openai-community/gpt2/blob/main/config.json", "hf", "/hf/TOKEN123/openai-community/gpt2/resolve/main/config.json"},
		{"Docker 官方镜像名", "library/nginx:1.25", "docker", "/offline/alice/TOKEN123/library/nginx:1.25"},
		{"Docker 短名", "nginx:latest", "docker", "/offline/alice/TOKEN123/nginx:latest"},
		{"带域名的镜像不应被当 URL", "docker.io/library/nginx:1.25", "docker", "/offline/alice/TOKEN123/docker.io/library/nginx:1.25"},
		{"GHCR 镜像不应被当 URL", "ghcr.io/owner/app:1.2", "docker", "/offline/alice/TOKEN123/ghcr.io/owner/app:1.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := s.buildAccelURL(base, c.in, tk, user)
			if got.Kind != c.wantKind {
				t.Fatalf("kind = %q, want %q (url=%q hint=%q)", got.Kind, c.wantKind, got.URL, got.Hint)
			}
			if !contains(got.URL, c.wantPath) {
				t.Fatalf("url = %q, want contains %q", got.URL, c.wantPath)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
