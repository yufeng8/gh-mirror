package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMatchRegistry(t *testing.T) {
	cases := []struct {
		rest       string
		wantDomain string
		wantPath   string
	}{
		{"library/nginx/manifests/latest", "", "library/nginx/manifests/latest"},
		{"nginx/manifests/latest", "", "nginx/manifests/latest"},
		{"ghcr.io/owner/repo/manifests/v1", "ghcr.io", "owner/repo/manifests/v1"},
		{"quay.io/org/img/blobs/sha256:abc", "quay.io", "org/img/blobs/sha256:abc"},
		{"registry.k8s.io/pause/manifests/3.9", "registry.k8s.io", "pause/manifests/3.9"},
	}
	for _, c := range cases {
		m, p := matchRegistry(c.rest)
		if m.domain != c.wantDomain || p != c.wantPath {
			t.Errorf("matchRegistry(%q) = (%q,%q), want (%q,%q)", c.rest, m.domain, p, c.wantDomain, c.wantPath)
		}
	}
}

func TestResolveTokenHost(t *testing.T) {
	cases := map[string]string{
		"":                   "auth.docker.io/token",
		"registry.docker.io": "auth.docker.io/token",
		"ghcr.io":            "ghcr.io/token",
		"quay.io":            "quay.io/v2/auth",
		"gcr.io":             "gcr.io/v2/auth",
	}
	for svc, want := range cases {
		if got := resolveTokenHost(svc); got != want {
			t.Errorf("resolveTokenHost(%q) = %q, want %q", svc, got, want)
		}
	}
}

func TestManifestTTL(t *testing.T) {
	if got := manifestTTL("/v2/library/nginx/manifests/sha256:deadbeef"); got != 24*time.Hour {
		t.Errorf("digest ttl = %v, want 24h", got)
	}
	if got := manifestTTL("/v2/library/nginx/manifests/latest"); got != 10*time.Minute {
		t.Errorf("latest ttl = %v, want 10m", got)
	}
	if got := manifestTTL("/v2/library/nginx/manifests/1.25"); got != 30*time.Minute {
		t.Errorf("tag ttl = %v, want 30m", got)
	}
}

func TestTokenTTL(t *testing.T) {
	if got := tokenTTL([]byte(`{"expires_in":1800}`)); got != 25*time.Minute {
		t.Errorf("ttl = %v, want 25m", got)
	}
	if got := tokenTTL([]byte(`{"expires_in":300}`)); got != 30*time.Minute {
		t.Errorf("short ttl = %v, want 30m fallback", got)
	}
	if got := tokenTTL([]byte(`not json`)); got != 30*time.Minute {
		t.Errorf("bad json ttl = %v, want 30m fallback", got)
	}
}

func TestRewriteAuthRealm(t *testing.T) {
	r := httptest.NewRequest("GET", "/v2/", nil)
	r.Host = "mirror.example.com"
	r.Header.Set("X-Forwarded-Proto", "https")
	hdr := http.Header{}
	hdr.Set("Www-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
	rewriteAuthRealm(hdr, clientBaseURL(r)+"/token")
	want := `Bearer realm="https://mirror.example.com/token",service="registry.docker.io"`
	if got := hdr.Get("Www-Authenticate"); got != want {
		t.Errorf("realm = %q, want %q", got, want)
	}
}