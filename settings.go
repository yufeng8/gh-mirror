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
	// DockerPath 是 /v2 的「自定义路径」，只存用户填写部分（可含 /，如 team-a/9f3k2）。
	// 实际路径前缀固定为 <用户名>[/<DockerPath>]（用户名段强制存在，见 dockerV2Prefix），
	// 等同给 registry 端点加了一道「只有知道路径的人能用」的保护，也为多用户各自独立路径预留规则。
	DockerPath string `json:"dockerPath"`
	Cache      struct {
		MaxFileMB  int `json:"maxFileMB"`  // 单文件缓存上限（MB），0 = 不限制
		MaxTotalMB int `json:"maxTotalMB"` // 缓存总上限（MB），0 = 不限制
		TTLMinutes int `json:"ttlMinutes"` // 缓存时效（分钟），0 = 永不过期
	} `json:"cache"`
	Offline struct {
		AutoPurge   bool `json:"autoPurge"`   // 离线包磁盘满自动清理开关（默认关）
		PurgeFreeGB int  `json:"purgeFreeGB"` // 触发阈值：磁盘可用低于该 GB 数时清理
		MaxFileMB   int  `json:"maxFileMB"`   // 离线包单文件上限（MB），超过则不落盘缓存（仅实时下载），0 = 不限
	} `json:"offline"`
	ContactEmail string     `json:"contactEmail"` // 超管联系邮箱（忘记密码引导 / 用户紧急联系）
	SMTP         smtpConfig `json:"smtp"`         // SMTP 发信配置（注册/找回密码验证码）
	RegOpen      bool       `json:"regOpen"`      // 用户自主注册开关
	UserCaptcha  bool       `json:"userCaptcha"`  // 用户入口（登录+注册）图形验证码开关
	AdminCaptcha bool       `json:"adminCaptcha"` // 管理入口登录图形验证码开关
	InviteOnly   bool       `json:"inviteOnly"`   // 仅邀请码注册开关
	HideLogin    bool       `json:"hideLogin"`    // 首页隐藏登录/注册按钮（仅隐藏入口，/admin /user 仍可直达）
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
	// 旧全局 dockerPath 迁移：多用户化后自定义路径按用户存储，旧全局值继承给默认管理员（避免 /v2 路径失效）
	migrated := false
	if st.DockerPath != "" && s.adminUser != "" {
		if u, ok := s.users.Get(s.adminUser); ok && u.DockerPath == "" {
			if err := s.users.SetDockerPath(s.adminUser, st.DockerPath); err == nil {
				log.Printf("已将旧全局 dockerPath %q 迁移给默认管理员 %s", st.DockerPath, s.adminUser)
				migrated = true
			}
		}
		st.DockerPath = ""
	}
	s.svcMu.Lock()
	s.dockerOn = st.Docker
	s.hfOn = st.HF
	s.dockerPrefix = st.DockerPath
	s.offlineAutoPurge = st.Offline.AutoPurge
	s.offlinePurgeFreeGB = st.Offline.PurgeFreeGB
	s.offlineMaxFileMB = st.Offline.MaxFileMB
	s.contactEmailStr = st.ContactEmail
	s.smtpCfg = st.SMTP
	s.regOpen = st.RegOpen
	s.userCaptcha = st.UserCaptcha
	s.adminCaptcha = st.AdminCaptcha
	s.inviteOnly = st.InviteOnly
	s.hideLogin = st.HideLogin
	s.svcMu.Unlock()
	if migrated {
		s.saveSettings()
	}
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
	st.Offline.AutoPurge = s.offlineAutoPurge
	st.Offline.PurgeFreeGB = s.offlinePurgeFreeGB
	st.Offline.MaxFileMB = s.offlineMaxFileMB
	st.ContactEmail = s.contactEmailStr
	st.SMTP = s.smtpCfg
	st.RegOpen = s.regOpen
	st.UserCaptcha = s.userCaptcha
	st.AdminCaptcha = s.adminCaptcha
	st.InviteOnly = s.inviteOnly
	st.HideLogin = s.hideLogin
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

// userDockerPath 返回某用户自己的 Docker 自定义路径（空表示只用用户名段；多用户隔离）。
func (s *Server) userDockerPath(user string) string {
	if u, ok := s.users.Get(user); ok {
		return u.DockerPath
	}
	return ""
}

// dockerSegment 把用户名规范化为 Docker 路径段：小写化 + 非法字符替换为 -（Docker 仓库路径必须小写）。
func dockerSegment(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	seg := strings.Trim(b.String(), "-")
	if seg == "" {
		seg = "user"
	}
	return seg
}

// userByDockerSegment 反查 Docker 段对应的用户名（大小写不敏感），不存在返回 false。
func (s *Server) userByDockerSegment(seg string) (string, bool) {
	for _, u := range s.users.All() {
		if dockerSegment(u.Username) == seg {
			return u.Username, true
		}
	}
	return "", false
}

// dockerV2PrefixFor 返回某用户的 /v2 **完整**路径前缀：<用户名段>[/<该用户的自定义路径>]。
// 用户名段强制存在、不可省略——每个用户天然拥有独立路径；自定义路径按用户隔离。
func (s *Server) dockerV2PrefixFor(user string) string {
	owner := dockerSegment(user)
	custom := s.userDockerPath(user)
	// 兼容历史数据：若自定义路径里已包含用户名段，剥掉避免出现 <owner>/<owner>/...
	if strings.HasPrefix(custom, owner+"/") {
		custom = strings.TrimPrefix(custom, owner+"/")
	}
	if custom == owner {
		custom = ""
	}
	if custom != "" {
		return owner + "/" + custom
	}
	return owner
}

// setOfflinePurge 设置离线包磁盘满自动清理开关、阈值与单文件上限（热生效 + 持久化）。
func (s *Server) setOfflinePurge(autoPurge bool, purgeFreeGB, maxFileMB int) {
	s.svcMu.Lock()
	s.offlineAutoPurge = autoPurge
	if purgeFreeGB < 0 {
		purgeFreeGB = 0
	}
	s.offlinePurgeFreeGB = purgeFreeGB
	if maxFileMB < 0 {
		maxFileMB = 0
	}
	s.offlineMaxFileMB = maxFileMB
	s.svcMu.Unlock()
	s.saveSettings()
}

// offlineMaxFileBytes 返回离线包单文件上限（字节；0 表示不限制）。
func (s *Server) offlineMaxFileBytes() int64 {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	if s.offlineMaxFileMB <= 0 {
		return 0
	}
	return int64(s.offlineMaxFileMB) * 1024 * 1024
}

// setPortalSettings 保存 SMTP 配置、注册开关、联系邮箱与图形验证码/邀请码开关（热生效 + 持久化）。
func (s *Server) setPortalSettings(cfg smtpConfig, regOpen bool, contactEmail string, userCaptcha, adminCaptcha, inviteOnly, hideLogin bool) {
	s.svcMu.Lock()
	s.smtpCfg = cfg
	s.regOpen = regOpen
	s.contactEmailStr = strings.TrimSpace(contactEmail)
	s.userCaptcha = userCaptcha
	s.adminCaptcha = adminCaptcha
	s.inviteOnly = inviteOnly
	s.hideLogin = hideLogin
	s.svcMu.Unlock()
	s.saveSettings()
}

// userCaptchaEnabled 返回用户入口图形验证码开关状态。
func (s *Server) userCaptchaEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.userCaptcha
}

// adminCaptchaEnabled 返回管理入口图形验证码开关状态。
func (s *Server) adminCaptchaEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.adminCaptcha
}

// inviteOnlyEnabled 返回仅邀请码注册开关状态。
func (s *Server) inviteOnlyEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.inviteOnly
}

// hideLoginEnabled 返回首页是否隐藏登录/注册入口（仅影响首页导航展示，不封禁入口路径）。
func (s *Server) hideLoginEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.hideLogin
}

// smtpSettings 返回当前 SMTP 配置。
func (s *Server) smtpSettings() smtpConfig {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.smtpCfg
}

// regEnabled 返回用户注册开关状态。
func (s *Server) regEnabled() bool {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.regOpen
}

// offlinePurgeBytes 返回离线包自动清理阈值（字节；0 表示未启用）。
func (s *Server) offlinePurgeBytes() int64 {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	if !s.offlineAutoPurge || s.offlinePurgeFreeGB <= 0 {
		return 0
	}
	return int64(s.offlinePurgeFreeGB) * 1024 * 1024 * 1024
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
