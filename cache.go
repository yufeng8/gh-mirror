package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// CacheMeta 保存缓存文件的内容类型与下载文件名，回源时一并写入 .meta 旁路文件。
type CacheMeta struct {
	ContentType string `json:"ct"`
	Disposition string `json:"cd"`
}

// Cache 简单的磁盘缓存：以 URL 的 sha256 为文件名，附带 TTL 与单文件/总大小上限。
// ttl / maxBytes / maxTotalBytes 均可在管理后台运行时热改（atomic，-race 安全）；
// sizeApprox 为近似占用，用于 Commit 后轻量判断是否触发清理（精确值见 UsageBytes）。
type Cache struct {
	dir           string
	ttl           atomic.Int64 // 缓存时效（纳秒），0 = 永不过期
	maxBytes      atomic.Int64 // 单文件缓存上限（字节），0 = 不限制
	maxTotalBytes atomic.Int64 // 缓存总上限（字节），0 = 不限制
	sizeApprox    atomic.Int64 // 近似占用字节（Commit 累加，Evict / UsageBytes 重算）
	mu            sync.Map     // key -> *sync.Mutex，保证同一 URL 回源串行（防惊群）
}

func NewCache(dir string, ttl time.Duration, maxMB int) *Cache {
	_ = os.MkdirAll(dir, 0o755)
	c := &Cache{dir: dir}
	c.ttl.Store(int64(ttl))
	c.maxBytes.Store(int64(maxMB) * 1024 * 1024)
	return c
}

// TTL 返回当前缓存时效；0 表示永不过期。
func (c *Cache) TTL() time.Duration { return time.Duration(c.ttl.Load()) }

// SetTTLMinutes 运行时设置缓存时效（分钟）；0 表示永不过期，负数按 0 处理。
func (c *Cache) SetTTLMinutes(minutes int) {
	if minutes < 0 {
		minutes = 0
	}
	c.ttl.Store(int64(time.Duration(minutes) * time.Minute))
}

// MaxBytes 返回单文件缓存上限（字节），0 表示不限制。
func (c *Cache) MaxBytes() int64 { return c.maxBytes.Load() }

// SetMaxFileMB 运行时设置单文件缓存上限（MB），0 = 不限制。
func (c *Cache) SetMaxFileMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	c.maxBytes.Store(int64(mb) * 1024 * 1024)
}

// MaxTotalBytes 返回缓存总上限（字节），0 表示不限制。
func (c *Cache) MaxTotalBytes() int64 { return c.maxTotalBytes.Load() }

// SetMaxTotalMB 运行时设置缓存总上限（MB），0 = 不限制。
func (c *Cache) SetMaxTotalMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	c.maxTotalBytes.Store(int64(mb) * 1024 * 1024)
}

// diskFreeMin 是缓存所在磁盘的可用空间下限（1 GiB）：低于该值时，
// 即使没超出缓存总量上限，也会按最旧优先清理缓存，避免磁盘被写满影响整机。
// 声明为变量以便单元测试置 0 关闭该规则（避免依赖跑测机器的磁盘余量）。
var diskFreeMin int64 = 1 << 30

// DiskFree 返回缓存所在磁盘的当前可用字节数（探测失败返回 0，表示未知）。
func (c *Cache) DiskFree() int64 { return diskFree(c.dir) }

// UsageBytes 遍历缓存目录，返回当前已占用字节数（含 .meta/.tmp 旁路文件），并刷新近似值。
func (c *Cache) UsageBytes() int64 {
	var total int64
	_ = filepath.Walk(c.dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	c.sizeApprox.Store(total)
	return total
}

// Clear 清空缓存目录内的所有文件（保留目录本身与 tokens.json / settings.json，
// 因为它们不在 cacheDir 内）。返回删除的文件数与字节数。
func (c *Cache) Clear() (files int, bytes int64) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		c.sizeApprox.Store(0)
		return 0, 0
	}
	for _, e := range entries {
		p := filepath.Join(c.dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		bytes += info.Size()
		if err := os.Remove(p); err == nil {
			files++
		}
	}
	c.sizeApprox.Store(0)
	return files, bytes
}

// Evict 执行一次清理，返回 (过期删除数, 超限删除数, 磁盘余量删除数)：
//  1. 删除过期文件（ModTime 早于 now-TTL；TTL=0 表示永不过期，跳过）
//  2. 缓存总量超出总上限时，按最旧优先删除至低于上限
//  3. 所在磁盘可用空间低于 diskFreeMin（1 GiB）时，同样按最旧优先清理，
//     直到余量回到阈值以上（缓存是临时的，磁盘写满会拖垮整机）
func (c *Cache) Evict() (expired, over, lowDisk int) {
	ttl := c.TTL()
	type item struct {
		path  string
		size  int64
		mtime time.Time
	}
	var items []item
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return 0, 0, 0
	}
	now := time.Now()
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.IsDir() {
			continue
		}
		p := filepath.Join(c.dir, e.Name())
		if ttl > 0 && now.Sub(info.ModTime()) > ttl {
			if os.Remove(p) == nil {
				expired++
				continue
			}
		}
		items = append(items, item{path: p, size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
	}
	// 超限：最旧优先删至低于上限
	if max := c.MaxTotalBytes(); max > 0 && total > max {
		sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
		for _, it := range items {
			if total <= max {
				break
			}
			if os.Remove(it.path) == nil {
				total -= it.size
				over++
			}
		}
	}
	// 磁盘余量保护：可用空间不足阈值时继续按最旧优先清理
	if free := diskFree(c.dir); free > 0 && free < diskFreeMin {
		need := diskFreeMin - free
		sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
		for _, it := range items {
			if need <= 0 {
				break
			}
			// 已在上一步删掉的文件会被跳过（os.Remove 返回错误）
			if os.Remove(it.path) == nil {
				total -= it.size
				need -= it.size
				lowDisk++
			}
		}
	}
	if total < 0 {
		total = 0
	}
	c.sizeApprox.Store(total)
	return expired, over, lowDisk
}

// maybeEvictAsync 在 Commit 后轻量触发清理：仅在「缓存近似占用超总上限」或
// 「磁盘可用空间低于阈值」时异步执行 Evict（避免每次回源都全盘遍历，也不阻塞请求）。
func (c *Cache) maybeEvictAsync() {
	max := c.MaxTotalBytes()
	overLimit := max > 0 && c.sizeApprox.Load() >= max
	lowFree := false
	if free := diskFree(c.dir); free > 0 && free < diskFreeMin {
		lowFree = true
	}
	if !overLimit && !lowFree {
		return
	}
	go func() {
		_, over, low := c.Evict()
		if over > 0 || low > 0 {
			log.Printf("缓存自动清理: 超限 %d 个、磁盘余量 %d 个，当前占用 %s（磁盘可用 %s）",
				over, low, formatBytes(c.UsageBytes()), formatBytes(diskFree(c.dir)))
		}
	}()
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
	if ttl := c.TTL(); ttl > 0 && time.Since(info.ModTime()) > ttl {
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
	tmp := c.path(key) + ".tmp"
	if info, err := os.Stat(tmp); err == nil {
		c.sizeApprox.Add(info.Size())
	}
	_ = os.Rename(tmp, c.path(key))
	c.maybeEvictAsync()
}

func (c *Cache) Abort(key string) {
	_ = os.Remove(c.path(key) + ".tmp")
}
