package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// token 的角色。admin 具备运维能力（可用于 MCP 接入）；user 仅用于代理访问。
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Token 代表一个代理访问令牌。Hits/Bytes/LastUsed 为运行时统计字段。
// Role 为空（旧 tokens.json）时按 user 处理。
// Owner 为归属用户名（多用户预留）；空值表示归属默认管理员（兼容旧数据）。
type Token struct {
	Token     string `json:"token"`
	Note      string `json:"note"`
	Role      string `json:"role,omitempty"`
	Owner     string `json:"owner,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Hits      int64  `json:"hits"`
	Bytes     int64  `json:"bytes"`
	LastUsed  int64  `json:"last_used"`
}

// normRole 归一化角色：空值或非法值按 user 处理（兼容旧 tokens.json）。
func normRole(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	if r == RoleAdmin {
		return RoleAdmin
	}
	return RoleUser
}

// Store 以 JSON 文件持久化 token 列表，读写均加锁。
type Store struct {
	mu     sync.RWMutex
	path   string
	tokens map[string]*Token
}

func NewStore(path string) *Store {
	s := &Store{path: path, tokens: map[string]*Token{}}
	s.load()
	return s
}

func (s *Store) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []*Token
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, t := range list {
		t.Role = normRole(t.Role)
		s.tokens[t.Token] = t
	}
}

// saveLocked 必须在持有 s.mu 时调用。
func (s *Store) saveLocked() error {
	list := make([]*Token, 0, len(s.tokens))
	for _, t := range s.tokens {
		list = append(list, t)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Has(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tokens[token]
	return ok
}

// Get 返回指定 token（不存在返回 false）。
func (s *Store) Get(token string) (*Token, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tokens[token]
	return t, ok
}

// Add 新增 token；role 传空串按 user 处理；owner 为归属用户名（空表示默认管理员）。
func (s *Store) Add(token, note, role, owner string) {
	s.mu.Lock()
	s.tokens[token] = &Token{Token: token, Note: note, Role: normRole(role), Owner: strings.TrimSpace(owner), CreatedAt: time.Now().Unix()}
	_ = s.saveLocked()
	s.mu.Unlock()
}

// Update 修改 token 的备注与角色（token 值本身不可改，确保已有链接不失效）。
// 返回是否命中（false 表示 token 不存在）。持锁写入并落盘。
func (s *Store) Update(token, note, role string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[token]
	if !ok {
		return false
	}
	t.Note = note
	t.Role = normRole(role)
	_ = s.saveLocked()
	return true
}

// IsAdmin 判断 token 存在且具备 admin 角色（MCP 鉴权使用）。
func (s *Store) IsAdmin(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tokens[token]
	return ok && normRole(t.Role) == RoleAdmin
}

func (s *Store) Delete(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	_ = s.saveLocked()
	s.mu.Unlock()
}

// Save 将当前内存中的 token 列表（含统计）落盘，外部调用无需持锁。
func (s *Store) Save() {
	s.mu.Lock()
	_ = s.saveLocked()
	s.mu.Unlock()
}

// RecordHit 累加某 token 的访问次数与流量，并更新最近使用时间。
// 仅在内存中更新，落盘由后台定时 Save 或 Add/Delete 触发，避免每次请求都写磁盘。
func (s *Store) RecordHit(token string, n int64) {
	s.mu.Lock()
	if t, ok := s.tokens[token]; ok {
		t.Hits++
		t.Bytes += n
		t.LastUsed = time.Now().Unix()
	}
	s.mu.Unlock()
}

func (s *Store) All() []*Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*Token, 0, len(s.tokens))
	for _, t := range s.tokens {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return list
}

// GenToken 生成随机 token。
func GenToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
