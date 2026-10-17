package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 邀请码体系：批量生成 / 自定义 / 导出（占位符格式）/ 备注 / 有效期 / 使用追踪。

const inviteChars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 去易混淆 I/O/0/1
const inviteLen = 16

// InviteCode 一条邀请码。
type InviteCode struct {
	Code      string `json:"code"`
	Note      string `json:"note,omitempty"`
	ExpireAt  int64  `json:"expire_at,omitempty"` // 0 = 永久
	Used      bool   `json:"used"`
	UsedBy    string `json:"used_by,omitempty"`
	UsedAt    int64  `json:"used_at,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// InviteStore 以 JSON 文件持久化邀请码列表，读写加锁（复用 store/users 模式）。
type InviteStore struct {
	mu      sync.RWMutex
	path    string
	invites map[string]*InviteCode
}

func NewInviteStore(path string) *InviteStore {
	is := &InviteStore{path: path, invites: map[string]*InviteCode{}}
	is.load()
	return is
}

func (is *InviteStore) load() {
	b, err := os.ReadFile(is.path)
	if err != nil {
		return
	}
	var list []*InviteCode
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, inv := range list {
		is.invites[inv.Code] = inv
	}
}

func (is *InviteStore) saveLocked() error {
	list := make([]*InviteCode, 0, len(is.invites))
	for _, inv := range is.invites {
		list = append(list, inv)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(is.path), 0o700); err != nil {
		return err
	}
	tmp := is.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, is.path)
}

// normalizeInvite 规整邀请码（去空格/连字符 + 大写化）。
func normalizeInvite(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	return code
}

// genInviteCode 生成随机邀请码（16 位，去易混淆）。
func genInviteCode() string {
	b := make([]byte, inviteLen)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(inviteChars))))
		b[i] = inviteChars[n.Int64()]
	}
	return string(b)
}

// Generate 批量生成 n 条邀请码（随机，去重），统一打备注与有效期；返回生成的码列表。
func (is *InviteStore) Generate(n int, note string, expireAt int64) ([]string, error) {
	if n <= 0 || n > 10000 {
		return nil, fmt.Errorf("数量需在 1-10000 之间")
	}
	now := time.Now().Unix()
	is.mu.Lock()
	defer is.mu.Unlock()
	codes := make([]string, 0, n)
	for len(codes) < n {
		code := genInviteCode()
		if _, exists := is.invites[code]; exists {
			continue
		}
		is.invites[code] = &InviteCode{Code: code, Note: note, ExpireAt: expireAt, CreatedAt: now}
		codes = append(codes, code)
	}
	if err := is.saveLocked(); err != nil {
		return nil, err
	}
	return codes, nil
}

// AddCustom 添加一条自定义邀请码（规整后去重）。
func (is *InviteStore) AddCustom(code, note string, expireAt int64) error {
	code = normalizeInvite(code)
	if len(code) < 6 || len(code) > 64 {
		return fmt.Errorf("邀请码需 6-64 位字母数字")
	}
	for _, r := range code {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("邀请码含非法字符（仅字母数字）")
		}
	}
	is.mu.Lock()
	defer is.mu.Unlock()
	if _, exists := is.invites[code]; exists {
		return fmt.Errorf("邀请码已存在: %s", code)
	}
	is.invites[code] = &InviteCode{Code: code, Note: note, ExpireAt: expireAt, CreatedAt: time.Now().Unix()}
	return is.saveLocked()
}

// Delete 批量删除邀请码（按原始码，规整后匹配）。返回删除数。
func (is *InviteStore) Delete(codes []string) int {
	is.mu.Lock()
	defer is.mu.Unlock()
	removed := 0
	for _, c := range codes {
		c = normalizeInvite(c)
		if _, ok := is.invites[c]; ok {
			delete(is.invites, c)
			removed++
		}
	}
	if removed > 0 {
		_ = is.saveLocked()
	}
	return removed
}

// SetNote 批量打备注（对指定码）。返回更新数。
func (is *InviteStore) SetNote(codes []string, note string) int {
	is.mu.Lock()
	defer is.mu.Unlock()
	updated := 0
	for _, c := range codes {
		c = normalizeInvite(c)
		if inv, ok := is.invites[c]; ok {
			inv.Note = note
			updated++
		}
	}
	if updated > 0 {
		_ = is.saveLocked()
	}
	return updated
}

// SetExpire 批量设置有效期（0 = 永久）。返回更新数。
func (is *InviteStore) SetExpire(codes []string, expireAt int64) int {
	is.mu.Lock()
	defer is.mu.Unlock()
	updated := 0
	for _, c := range codes {
		c = normalizeInvite(c)
		if inv, ok := is.invites[c]; ok {
			inv.ExpireAt = expireAt
			updated++
		}
	}
	if updated > 0 {
		_ = is.saveLocked()
	}
	return updated
}

// List 返回全部邀请码（按创建时间倒序）。
func (is *InviteStore) List() []*InviteCode {
	is.mu.RLock()
	defer is.mu.RUnlock()
	list := make([]*InviteCode, 0, len(is.invites))
	for _, inv := range is.invites {
		list = append(list, inv)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
	return list
}

// Get 返回指定码（规整后匹配）。
func (is *InviteStore) Get(code string) (*InviteCode, bool) {
	code = normalizeInvite(code)
	is.mu.RLock()
	defer is.mu.RUnlock()
	inv, ok := is.invites[code]
	return inv, ok
}

// Claim 原子地校验并标记邀请码为已使用（未使用且未过期才成功）；用于注册。
func (is *InviteStore) Claim(code, user string) bool {
	code = normalizeInvite(code)
	is.mu.Lock()
	defer is.mu.Unlock()
	inv, ok := is.invites[code]
	if !ok || inv.Used {
		return false
	}
	if inv.ExpireAt > 0 && time.Now().Unix() > inv.ExpireAt {
		return false
	}
	inv.Used = true
	inv.UsedBy = user
	inv.UsedAt = time.Now().Unix()
	_ = is.saveLocked()
	return true
}

// Export 按格式模板导出指定码（每行一条）；format 支持占位符：
// {code} {note} {expire} {used} {used_by} {used_at} {created_at}
func (is *InviteStore) Export(codes []string, format string) string {
	if format == "" {
		format = "{code}"
	}
	var sb strings.Builder
	for i, c := range codes {
		c = normalizeInvite(c)
		inv, ok := is.invites[c]
		if !ok {
			continue
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(renderInviteFormat(inv, format))
	}
	return sb.String()
}

// renderInviteFormat 按模板渲染一条邀请码。
func renderInviteFormat(inv *InviteCode, format string) string {
	expire := "永久"
	if inv.ExpireAt > 0 {
		expire = time.Unix(inv.ExpireAt, 0).Format("2006-01-02 15:04")
	}
	used := "未使用"
	if inv.Used {
		used = "已使用"
	}
	usedAt := ""
	if inv.UsedAt > 0 {
		usedAt = time.Unix(inv.UsedAt, 0).Format("2006-01-02 15:04")
	}
	repl := strings.NewReplacer(
		"{code}", inv.Code,
		"{note}", inv.Note,
		"{expire}", expire,
		"{used}", used,
		"{used_by}", inv.UsedBy,
		"{used_at}", usedAt,
		"{created_at}", time.Unix(inv.CreatedAt, 0).Format("2006-01-02 15:04"),
	)
	return repl.Replace(format)
}
