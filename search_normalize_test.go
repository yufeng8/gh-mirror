package main

import "testing"

// TestNormalizeSearchResults 验证 Docker Hub 搜索结果的字段映射：
// name 优先 repo_name、回退 name；updated 优先 last_updated、回退 update_at。
func TestNormalizeSearchResults(t *testing.T) {
	sr := hubSearchResp{
		Count: 2,
		Results: []hubSearchResult{
			{
				Name: "nginx", RepoName: "library/nginx", Description: "Official build of Nginx.",
				StarCount: 21411, PullCount: 13450487002, IsOfficial: true,
				LastUpdated: "2026-10-09T10:00:00Z",
			},
			{
				Name: "nginx-proxy", RepoName: "", Description: "auto proxy",
				StarCount: 124, PullCount: 500, IsOfficial: false,
				UpdatedAt: "2026-10-01T08:30:00Z",
			},
		},
	}
	out := normalizeSearchResults(sr)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Name != "library/nginx" {
		t.Fatalf("name = %q, want library/nginx", out[0].Name)
	}
	if out[0].Pulls != 13450487002 || out[0].Stars != 21411 || !out[0].Official {
		t.Fatalf("first item mismatch: %+v", out[0])
	}
	if out[0].Updated != "2026-10-09T10:00:00Z" {
		t.Fatalf("updated = %q", out[0].Updated)
	}
	if out[1].Name != "nginx-proxy" {
		t.Fatalf("fallback name = %q, want nginx-proxy", out[1].Name)
	}
	if out[1].Updated != "2026-10-01T08:30:00Z" {
		t.Fatalf("fallback updated = %q", out[1].Updated)
	}
}

// TestParseConfigCreatedRFC3339 覆盖带纳秒与无纳秒两种形式。
func TestParseConfigCreatedRFC3339(t *testing.T) {
	cases := []string{
		`{"created":"2026-10-09T12:34:56Z"}`,
		`{"created":"2026-10-09T12:34:56.123456789Z"}`,
	}
	for _, c := range cases {
		if ts := parseConfigCreated([]byte(c)); ts == 0 {
			t.Fatalf("parse %s => 0", c)
		}
	}
}
