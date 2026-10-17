package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestCache 在 t.TempDir 中创建一个缓存实例。
func newTestCache(t *testing.T, ttl time.Duration) *Cache {
	t.Helper()
	// 关闭磁盘余量清理规则：避免单测结果依赖跑测机器当时的磁盘可用空间
	old := diskFreeMin
	diskFreeMin = 0
	t.Cleanup(func() { diskFreeMin = old })
	return NewCache(filepath.Join(t.TempDir(), "cache"), ttl, 200)
}

// writeCacheFile 通过 Begin/Commit 写入一个缓存文件（模拟一次回源落盘）。
func writeCacheFile(t *testing.T, c *Cache, key string, size int) {
	t.Helper()
	f, err := c.Begin(key, CacheMeta{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("Begin(%s): %v", key, err)
	}
	if _, err := f.Write(make([]byte, size)); err != nil {
		t.Fatalf("写入 %s: %v", key, err)
	}
	f.Close()
	c.Commit(key)
}

// TestCacheEvictExpired 验证「自动失效」：TTL 到期后 Evict 会删除过期文件。
func TestCacheEvictExpired(t *testing.T) {
	c := newTestCache(t, 20*time.Millisecond)
	writeCacheFile(t, c, "k1", 256)
	if _, _, ok := c.Get("k1"); !ok {
		t.Fatal("刚写入的缓存未命中")
	}
	time.Sleep(60 * time.Millisecond)
	// 过期清理会删除数据文件及其 .meta 旁路文件，故至少应删除 1 个
	expired, over, _ := c.Evict()
	if expired < 1 {
		t.Fatalf("期望至少删除 1 个过期文件，实际 expired=%d over=%d", expired, over)
	}
	if over != 0 {
		t.Fatalf("未设置总上限时不应触发超限清理，实际 over=%d", over)
	}
	if _, _, ok := c.Get("k1"); ok {
		t.Fatal("过期文件仍可命中")
	}
}

// TestCacheTTLZeroNeverExpires 验证 TTL=0（永不过期）时 Evict 不会误删。
func TestCacheTTLZeroNeverExpires(t *testing.T) {
	c := newTestCache(t, 0)
	writeCacheFile(t, c, "k1", 128)
	time.Sleep(30 * time.Millisecond)
	expired, _, _ := c.Evict()
	if expired != 0 {
		t.Fatalf("TTL=0 不应删除过期文件，实际 expired=%d", expired)
	}
	if _, _, ok := c.Get("k1"); !ok {
		t.Fatal("TTL=0 时缓存被误删")
	}
}

// TestCacheEvictOverQuota 验证超出总上限时按最旧优先清理。
func TestCacheEvictOverQuota(t *testing.T) {
	c := newTestCache(t, time.Hour)
	for i, k := range []string{"old", "mid", "new"} {
		writeCacheFile(t, c, k, 1024)
		if i < 2 {
			// 人为拉开 mtime，确保 "old" 最旧
			time.Sleep(20 * time.Millisecond)
		}
	}
	// 总上限 2KB：当前 3KB（含 .meta 旁路文件），应触发超限清理
	c.SetMaxTotalMB(0) // MB 粒度不支持 2KB，直接按字节设置
	c.maxTotalBytes.Store(2 * 1024)
	_, over, _ := c.Evict()
	if over == 0 {
		t.Fatal("超出总上限时应删除文件，实际 over=0")
	}
	if used := c.UsageBytes(); used > 2*1024 {
		t.Fatalf("清理后仍超出上限: %d > 2048", used)
	}
}

// TestCacheEvictLowDisk 验证「磁盘可用空间低于阈值时也会自动清理」：
// 通过把阈值抬到极大值模拟磁盘余量不足，即使未超总上限也应清理到尽量腾出空间。
func TestCacheEvictLowDisk(t *testing.T) {
	c := newTestCache(t, time.Hour)
	for _, k := range []string{"old", "mid", "new"} {
		writeCacheFile(t, c, k, 1024)
		time.Sleep(20 * time.Millisecond) // 人为拉开 mtime，验证按最旧优先
	}
	before := c.UsageBytes()
	if before == 0 {
		t.Fatal("前置条件失败：缓存目录应有文件")
	}
	// 写入完成后再模拟「磁盘余量不足」，否则 Commit 的异步清理会提前把文件删掉
	diskFreeMin = 1 << 50
	expired, over, low := c.Evict()
	if low == 0 {
		t.Fatalf("磁盘余量低于阈值时应触发清理，实际 expired=%d over=%d low=%d", expired, over, low)
	}
	if used := c.UsageBytes(); used != 0 {
		t.Fatalf("磁盘余量不足时应尽量清理，剩余 %d 字节（清理前 %d）", used, before)
	}
}

// TestCacheClear 验证一键清除缓存。
func TestCacheClear(t *testing.T) {
	c := newTestCache(t, time.Hour)
	writeCacheFile(t, c, "k1", 1024)
	writeCacheFile(t, c, "k2", 2048)
	files, bytes := c.Clear()
	if files == 0 || bytes == 0 {
		t.Fatalf("清除应返回删除的文件数与字节数，实际 files=%d bytes=%d", files, bytes)
	}
	if used := c.UsageBytes(); used != 0 {
		t.Fatalf("清除后占用应为 0，实际 %d", used)
	}
	if entries, err := os.ReadDir(c.dir); err != nil || len(entries) != 0 {
		t.Fatalf("清除后目录应为空，实际 %d 个条目（err=%v）", len(entries), err)
	}
}
