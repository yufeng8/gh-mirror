package main

import (
	"encoding/json"
	"testing"
)

func TestValidRepo(t *testing.T) {
	for _, ok := range []string{"nginx", "library/nginx", "ghcr.io/owner/repo", "a_b.c-d/e"} {
		if !validRepo(ok) {
			t.Errorf("validRepo(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a?b", "a/b?x=1", "../etc"} {
		if validRepo(bad) {
			t.Errorf("validRepo(%q) = true, want false", bad)
		}
	}
}

func TestHumanSize(t *testing.T) {
	if got := humanSize(500); got != "500 B" {
		t.Errorf("humanSize(500)=%q, want 500 B", got)
	}
	if got := humanSize(2048); got != "2.0 KB" {
		t.Errorf("humanSize(2048)=%q, want 2.0 KB", got)
	}
	if got := humanSize(5*1024*1024); got != "5.0 MB" {
		t.Errorf("humanSize(5MB)=%q, want 5.0 MB", got)
	}
}

func TestHubSearchParse(t *testing.T) {
	var sr hubSearchResp
	if err := json.Unmarshal([]byte(`{"count":1,"results":[{"name":"nginx","description":"web","star_count":10,"is_official":true}]}`), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.Count != 1 || len(sr.Results) != 1 || sr.Results[0].Name != "nginx" || !sr.Results[0].IsOfficial {
		t.Errorf("parse got %+v", sr)
	}
	var tr hubTagsResp
	if err := json.Unmarshal([]byte(`{"count":1,"results":[{"name":"latest","full_size":142606336,"last_updated":"2024-01-01T00:00:00Z"}]}`), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Results[0].Name != "latest" || tr.Results[0].FullSize != 142606336 {
		t.Errorf("tags parse got %+v", tr)
	}
}
