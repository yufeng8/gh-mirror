package main

import (
	"net/url"
	"testing"
)

func TestBuildHFTarget(t *testing.T) {
	cases := map[string]string{
		"owner/repo/resolve/main/model.bin":    "https://huggingface.co/owner/repo/resolve/main/model.bin",
		"/owner/repo/resolve/main/model.bin":   "https://huggingface.co/owner/repo/resolve/main/model.bin",
		"https://huggingface.co/x/y/resolve/m/f": "https://huggingface.co/x/y/resolve/m/f",
	}
	for in, want := range cases {
		if got := buildHFTarget(in); got != want {
			t.Errorf("buildHFTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteHuggingFacePassthrough(t *testing.T) {
	u, _ := url.Parse("https://huggingface.co/owner/repo/resolve/main/model.bin")
	dest := rewrite(u)
	if dest == nil {
		t.Fatal("rewrite(huggingface.co) = nil, want passthrough")
	}
	if dest.Host != "huggingface.co" || dest.Path != "/owner/repo/resolve/main/model.bin" {
		t.Errorf("passthrough got host=%q path=%q", dest.Host, dest.Path)
	}
}
