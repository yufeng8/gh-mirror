package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gh-mirror/config"
)

// settings 是可在管理后台运行时调整并持久化的设置。
// 持久化位置：gm-data/settings.json（与 tokens.json 同目录，随数据卷保留）。
// 启动加载优先级：默认 → config.toml → 环境变量 → settings.json（运行时覆盖最后生效）。
type settings struct {
	Docker bool `json:"docker"`
	HF     bool `json:"hf"`
	// DockerPath 是 /v2 的自定义路径前缀（可含 /，如 team-a/9f3k2）。
	// 非空时，Docker 拉取必须带该前缀（/v2/<DockerPath>/...），等同给 registry 端点加了一道
	// 「只有知道路径的人能用」的保护；留空则按传统的 registry-mirrors 方式工作。
	DockerPath string `json:"dockerPath"`
	Cache      struct {
		MaxFileMB  int `json:"maxFileMB"`  // 单文件缓存上限（MB），0 = 不限制
		MaxTotalMB int `json:"maxTotalMB"` // 缓存总上限（MB），0 = 不限制
		TTLMinutes int `json:"ttlMinutes"` // 缓存时效（分钟），0 = 永不过期
	} `json:"cache"`
}

// normDockerPath 规范化 Docker 自定义路径：去首尾与重复斜杠，只允许 [A-Za-z0-9-_/]，最长 64 字符。
// 返回 (规范化结果, 是否合法)；空串表示不启用前缀（合法）。
func normDockerPath(p string) (string, bool) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
	p = strings.Join(parts, "/")
	if p == "" {
		return "", true
	}
	if len(p) > 64 {
		return "", false
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '/':
		default:
			return "", false
		}
	}
	return p, true
}

// settingsPath 返回 settings.json 路径（tokens.json 同目录，即 gm-data 卷内）。
func settingsPath(cfg *config.Config) string {
	dir := filepath.Dir(cfg.TokensFile)
	if dir == "" || dir == "." {
		dir = filepath.Dir(cfg.CacheDir)
		if dir == "" {
			dir = "."
		}
	}
	return filepath.Join(dir, "settings.json")
}

// loadSettings 读取 settings.json（若存在），据此覆盖服务开关与缓存参数。
// 文件不存在或解析失败时保持部署基线（config.toml + 环境变量），不影响启动。
func loadSettings(s *Server, cfg *config.Config) {
	b, err := os.ReadFile(settingsPath(cfg))
	if err != nil {
		return
	}
	var st settings
	if err := json.Unmarshal(b, &st); err != nil {
		log.Printf("settings.json 解析失败，忽略并使用部署基线: %v", err)
		return
	}
	if p, ok := normDockerPath(st.DockerPath); ok {
		st.DockerPath = p
	} else {
		log.Printf("settings.json 的 dockerPath 不合法，已忽略: %q", st.DockerPath)
		st.DockerPath = ""
	}
	s.svcMu.Lock()
	s.dockerOn = st.Docker
	s.hfOn = st.HF
	s.dockerPrefix = st.DockerPath
	s.svcMu.Unlock()
	s.cache.SetMaxFileMB(st.Cache.MaxFileMB)
	s.cache.SetMaxTotalMB(st.Cache.MaxTotalMB)
	s.cache.SetTTLMinutes(st.Cache.TTLMinutes)
	log.Printf("已加载运行时设置: docker=%v hf=%v 缓存{单文件=%dMB 总计=%dMB ttl=%dm}",
		st.Docker, st.HF, st.Cache.MaxFileMB, st.Cache.MaxTotalMB, st.Cache.TTLMinutes)
}

// saveSettings 把当前生效的设置原子写入 settings.json（tmp + rename，权限 0600）。
func saveSettings(path string, st settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// currentSettings 汇总当前生效的服务开关与缓存参数。
func (s *Server) currentSettings() settings {
	s.svcMu.RLock()
	st := settings{Docker: s.dockerOn, HF: s.hfOn, DockerPath: s.dockerPrefix}
	s.svcMu.RUnlock()
	st.Cache.MaxFileMB = int(s.cache.MaxBytes() / (1024 * 1024))
	st.Cache.MaxTotalMB = int(s.cache.MaxTotalBytes() / (1024 * 1024))
	st.Cache.TTLMinutes = int(s.cache.TTL() / time.Minute)
	return st
}

// saveSettings 持久化当前设置；落盘失败不阻断内存变更（仅记日志）。
func (s *Server) saveSettings() {
	if err := saveSettings(settingsPath(s.cfg), s.currentSettings()); err != nil {
		log.Printf("settings.json 写入失败（内存已生效）: %v", err)
	}
}

// setService 运行时切换服务开关（持锁改内存 + 持久化）。name: docker | hf。
func (s *Server) setService(name string, on bool) error {
	s.svcMu.Lock()
	switch name {
	case "docker":
		s.dockerOn = on
	case "hf":
		s.hfOn = on
	default:
		s.svcMu.Unlock()
		return fmt.Errorf("未知服务: %s", name)
	}
	s.svcMu.Unlock()
	s.saveSettings()
	return nil
}

// dockerPath 返回 /v2 的自定义路径前缀（空表示未启用）。
func (s *Server) dockerPath() string {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.dockerPrefix
}

// setDockerPath 设置 /v2 的自定义路径前缀（持锁改内存 + 持久化）。空串表示不启用前缀。
func (s *Server) setDockerPath(p string) error {
	np, ok := normDockerPath(p)
	if !ok {
		return fmt.Errorf("自定义路径不合法（仅支持字母、数字、-、_、/，最长 64 字符）")
	}
	s.svcMu.Lock()
	s.dockerPrefix = np
	s.svcMu.Unlock()
	s.saveSettings()
	return nil
}

// setCacheLimits 运行时调整缓存参数（热生效 + 持久化）。
// 任一参数传负数表示"不修改该项"。
func (s *Server) setCacheLimits(maxFileMB, maxTotalMB, ttlMinutes int) error {
	if maxFileMB >= 0 {
		s.cache.SetMaxFileMB(maxFileMB)
	}
	if maxTotalMB >= 0 {
		s.cache.SetMaxTotalMB(maxTotalMB)
	}
	if ttlMinutes >= 0 {
		s.cache.SetTTLMinutes(ttlMinutes)
	}
	s.saveSettings()
	return nil
}
