package main

import "testing"

func TestParseImageRef(t *testing.T) {
	cases := []struct {
		in     string
		domain string
		repo   string
		ref    string
	}{
		{"nginx:latest", "", "library/nginx", "latest"},
		{"nginx", "", "library/nginx", "latest"},
		{"library/redis:7", "", "library/redis", "7"},
		{"ghcr.io/owner/repo:v1", "ghcr.io", "owner/repo", "v1"},
		{"registry.k8s.io/pause:3.9", "registry.k8s.io", "pause", "3.9"},
		{"quay.io/org/img@sha256:deadbeef", "quay.io", "org/img", "sha256:deadbeef"},
	}
	for _, c := range cases {
		m, repo, ref := parseImageRef(c.in)
		if m.domain != c.domain || repo != c.repo || ref != c.ref {
			t.Errorf("parseImageRef(%q) = (%q,%q,%q), want (%q,%q,%q)",
				c.in, m.domain, repo, ref, c.domain, c.repo, c.ref)
		}
	}
}

func TestRepoTagName(t *testing.T) {
	if got := repoTagName(dockerHub, "library/nginx", "latest"); got != "nginx:latest" {
		t.Errorf("dockerhub tag = %q, want nginx:latest", got)
	}
	if got := repoTagName(extraRegistries[0], "owner/repo", "v1"); got != "owner/repo:v1" {
		t.Errorf("ghcr tag = %q, want owner/repo:v1", got)
	}
	if got := repoTagName(dockerHub, "library/nginx", "sha256:0123456789abcdef"); got != "nginx:0123456789ab" {
		t.Errorf("digest tag = %q, want nginx:0123456789ab", got)
	}
}

func TestHexOfAndSanitize(t *testing.T) {
	if got := hexOf("sha256:abc123"); got != "abc123" {
		t.Errorf("hexOf = %q, want abc123", got)
	}
	if got := sanitizeName("library/nginx:1.25"); got != "library_nginx_1.25" {
		t.Errorf("sanitize = %q", got)
	}
}

func desc(os, arch, digest string) ociDescriptor {
	d := ociDescriptor{Digest: digest, Platform: &struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	}{Architecture: arch, OS: os}}
	return d
}

func TestPickPlatform(t *testing.T) {
	list := []ociDescriptor{
		desc("linux", "arm64", "sha256:arm"),
		desc("linux", "amd64", "sha256:amd"),
		desc("windows", "amd64", "sha256:win"),
	}
	if got := pickPlatform(list); got != "sha256:amd" {
		t.Errorf("pickPlatform = %q, want sha256:amd", got)
	}
	only := []ociDescriptor{desc("linux", "arm64", "sha256:arm")}
	if got := pickPlatform(only); got != "sha256:arm" {
		t.Errorf("pickPlatform arm-only = %q, want sha256:arm", got)
	}
}
