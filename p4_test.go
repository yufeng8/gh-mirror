package main

import (
	"net/http/httptest"
	"testing"
)

func TestNormalizeIPv6(t *testing.T) {
	if got := normalizeIPv6("1.2.3.4"); got != "1.2.3.4" {
		t.Errorf("ipv4 normalize = %q, want 1.2.3.4", got)
	}
	if got := normalizeIPv6("2001:db8:1:2:3:4:5:6"); got != "2001:db8:1:2::/64" {
		t.Errorf("ipv6 normalize = %q, want 2001:db8:1:2::/64", got)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/g/x/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Errorf("remote = %q, want 203.0.113.9", got)
	}
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if got := ClientIP(r); got != "198.51.100.7" {
		t.Errorf("xff = %q, want 198.51.100.7", got)
	}
	r.Header.Del("X-Forwarded-For")
	r.Header.Set("X-Real-IP", "198.51.100.8")
	if got := ClientIP(r); got != "198.51.100.8" {
		t.Errorf("xri = %q, want 198.51.100.8", got)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(3600, 2) // 每小时 2 次，突发 2
	if !rl.Allow("1.1.1.1") || !rl.Allow("1.1.1.1") {
		t.Fatal("前两次应放行")
	}
	if rl.Allow("1.1.1.1") {
		t.Error("第三次应被限流")
	}
	if !rl.Allow("2.2.2.2") {
		t.Error("不同 IP 应放行")
	}
}

func TestAccessControllerIP(t *testing.T) {
	ac := NewAccessController([]string{"192.168.1.0/24"}, []string{"10.0.0.0/8"}, nil, nil)
	if ok, _ := ac.CheckIP("10.1.2.3"); ok {
		t.Error("10.x 应被黑名单拒绝")
	}
	if ok, _ := ac.CheckIP("192.168.1.5"); !ok {
		t.Error("192.168.1.5 应在白名单放行")
	}
	if ok, _ := ac.CheckIP("8.8.8.8"); ok {
		t.Error("8.8.8.8 不在白名单应拒绝")
	}
}

func TestCheckRepo(t *testing.T) {
	ac := NewAccessController(nil, nil, []string{"library/*", "grafana"}, nil)
	for _, name := range []string{"library/nginx", "library/redis", "grafana/grafana"} {
		if ok, _ := ac.CheckRepo(name); !ok {
			t.Errorf("%s 应在白名单放行", name)
		}
	}
	if ok, _ := ac.CheckRepo("evil/app"); ok {
		t.Error("evil/app 不在白名单应拒绝")
	}
	block := NewAccessController(nil, nil, nil, []string{"evil/*"})
	if ok, _ := block.CheckRepo("evil/app"); ok {
		t.Error("evil/app 应在黑名单拒绝")
	}
	if ok, _ := block.CheckRepo("good/app"); !ok {
		t.Error("good/app 无黑名单命中应放行")
	}
}

func TestParseSocks5(t *testing.T) {
	addr, user, pass := parseSocks5("socks5://user:pass@1.2.3.4:1080")
	if addr != "1.2.3.4:1080" || user != "user" || pass != "pass" {
		t.Errorf("got addr=%q user=%q pass=%q", addr, user, pass)
	}
	addr, _, _ = parseSocks5("1.2.3.4")
	if addr != "1.2.3.4:1080" {
		t.Errorf("default port got %q", addr)
	}
}
