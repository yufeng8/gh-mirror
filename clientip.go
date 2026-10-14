package main

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP 尽力获取真实客户端 IP：优先 X-Forwarded-For 第一个，其次 X-Real-IP，最后 RemoteAddr。
// 安全提示：XFF/X-Real-IP 可被伪造，生产环境应仅在受信反代之后启用（详见 README 建议）。
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// normalizeIPv6 将 IPv6 归并到 /64（限流按 /64 聚合，避免单用户海量 IPv6 绕过），IPv4 原样返回。
func normalizeIPv6(ipStr string) string {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return ipStr
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	v6 := ip.To16()
	for i := 8; i < 16; i++ {
		v6[i] = 0
	}
	return v6.String() + "/64"
}
