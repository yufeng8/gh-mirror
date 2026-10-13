package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Token 代表一个代理访问令牌。Hits/Bytes/LastUsed 为运行时统计字段。
type Token struct {
	Token     string `json:"token"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
	Hits      int64  `json:"hits"`
	Bytes     int64  `json:"bytes"`
	LastUsed  int64  `json:"last_used"`
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

func (s *Store) Add(token, note string) {
	s.mu.Lock()
	s.tokens[token] = &Token{Token: token, Note: note, CreatedAt: time.Now().Unix()}
	_ = s.saveLocked()
	s.mu.Unlock()
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
