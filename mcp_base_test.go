package main

import "testing"

// TestDefaultBase 覆盖 LISTEN 的常见形式：
// 显式回环、缺省主机段（":端口"）、容器部署常见的 0.0.0.0 —— 后两者都应归一为 127.0.0.1，
// 避免 stdio 模式下 convert_link 生成 http://0.0.0.0:8080 这类无效链接。
func TestDefaultBase(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080":   "http://127.0.0.1:8080",
		":8080":            "http://127.0.0.1:8080",
		"0.0.0.0:8080":     "http://127.0.0.1:8080",
		"192.168.1.5:8080": "http://192.168.1.5:8080",
	}
	for in, want := range cases {
		if got := defaultBase(in); got != want {
			t.Errorf("defaultBase(%q) = %q, want %q", in, got, want)
		}
	}
}
