package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheMeta 保存缓存文件的内容类型与下载文件名，回源时一并写入 .meta 旁路文件。
type CacheMeta struct {
	ContentType string `json:"ct"`
	Disposition string `json:"cd"`
}

// Cache 简单的磁盘缓存：以 URL 的 sha256 为文件名，附带 TTL 与单文件大小上限。
type Cache struct {
	dir      string
	ttl      time.Duration
	maxBytes int64
	mu       sync.Map // key -> *sync.Mutex，保证同一 URL 回源串行（防惊群）
}

func NewCache(dir string, ttl time.Duration, maxMB int) *Cache {
	_ = os.MkdirAll(dir, 0o755)
	return &Cache{
		dir:      dir,
		ttl:      ttl,
		maxBytes: int64(maxMB) * 1024 * 1024,
	}
}

// MaxBytes 返回缓存目录上限（字节）。
func (c *Cache) MaxBytes() int64 { return c.maxBytes }

// UsageBytes 遍历缓存目录，返回当前已占用字节数（含 .meta/.tmp 旁路文件）。
func (c *Cache) UsageBytes() int64 {
	var total int64
	_ = filepath.Walk(c.dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func cacheKey(method, target string) string {
	sum := sha256.Sum256([]byte(method + " " + target))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, key)
}

func (c *Cache) lock(key string) *sync.Mutex {
	m, _ := c.mu.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Get 命中且未过期时返回缓存文件路径与元数据。
func (c *Cache) Get(key string) (string, CacheMeta, bool) {
	p := c.path(key)
	info, err := os.Stat(p)
	if err != nil {
		return "", CacheMeta{}, false
	}
	if time.Since(info.ModTime()) > c.ttl {
		return "", CacheMeta{}, false
	}
	var meta CacheMeta
	if b, err := os.ReadFile(p + ".meta"); err == nil {
		_ = json.Unmarshal(b, &meta)
	}
	return p, meta, true
}

// Begin 写入 .meta 并返回临时数据文件（写完后由 Commit 原子改名）。
func (c *Cache) Begin(key string, meta CacheMeta) (*os.File, error) {
	_ = os.MkdirAll(filepath.Dir(c.path(key)), 0o755)
	if b, err := json.Marshal(meta); err == nil {
		_ = os.WriteFile(c.path(key)+".meta", b, 0o644)
	}
	return os.Create(c.path(key) + ".tmp")
}

func (c *Cache) Commit(key string) {
	_ = os.Rename(c.path(key)+".tmp", c.path(key))
}

func (c *Cache) Abort(key string) {
	_ = os.Remove(c.path(key) + ".tmp")
}
