package main

import (
	"net"
	"strings"
)

// AccessController 统一 IP / 仓库 黑白名单访问控制。
type AccessController struct {
	ipAllow   []*net.IPNet
	ipBlock   []*net.IPNet
	repoAllow []string
	repoBlock []string
}

func parseCIDRList(items []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if !strings.Contains(it, "/") {
			if strings.Contains(it, ":") {
				it += "/128"
			} else {
				it += "/32"
			}
		}
		if _, n, err := net.ParseCIDR(it); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func normalizeList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it = strings.ToLower(strings.TrimSpace(it)); it != "" {
			out = append(out, it)
		}
	}
	return out
}

func NewAccessController(ipAllow, ipBlock, repoAllow, repoBlock []string) *AccessController {
	return &AccessController{
		ipAllow:   parseCIDRList(ipAllow),
		ipBlock:   parseCIDRList(ipBlock),
		repoAllow: normalizeList(repoAllow),
		repoBlock: normalizeList(repoBlock),
	}
}

// CheckIP：黑名单优先，其次白名单（白名单非空时仅允许名单内）。
func (a *AccessController) CheckIP(ip string) (bool, string) {
	if a == nil {
		return true, ""
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return true, ""
	}
	if inList(parsed, a.ipBlock) {
		return false, "IP 在黑名单"
	}
	if len(a.ipAllow) > 0 && !inList(parsed, a.ipAllow) {
		return false, "IP 不在白名单"
	}
	return true, ""
}

func inList(ip net.IP, list []*net.IPNet) bool {
	for _, n := range list {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// CheckRepo：仓库黑白名单（支持通配符）。full 形如 owner/repo 或 namespace/repo。
func (a *AccessController) CheckRepo(full string) (bool, string) {
	if a == nil {
		return true, ""
	}
	full = strings.ToLower(strings.TrimSpace(full))
	if full == "" {
		return true, ""
	}
	if len(a.repoBlock) > 0 && matchAny(full, a.repoBlock) {
		return false, "仓库在黑名单"
	}
	if len(a.repoAllow) > 0 && !matchAny(full, a.repoAllow) {
		return false, "仓库不在白名单"
	}
	return true, ""
}

// matchAny 支持：全等、前缀 xxx*、裸命名空间 ns、*/repo、*/repo*。
func matchAny(full string, list []string) bool {
	ns, repo := full, ""
	if i := strings.Index(full, "/"); i >= 0 {
		ns, repo = full[:i], full[i+1:]
	}
	for _, item := range list {
		if item == full {
			return true
		}
		if strings.HasSuffix(item, "*") {
			if strings.HasPrefix(full, strings.TrimSuffix(item, "*")) {
				return true
			}
			continue
		}
		if strings.HasPrefix(item, "*/") {
			if repo == item[2:] {
				return true
			}
			continue
		}
		if !strings.Contains(item, "/") && item == ns {
			return true
		}
	}
	return false
}
