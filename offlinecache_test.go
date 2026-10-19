package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// newTestOfflineCache 在临时目录创建缓存实例，返回实例与目录。
func newTestOfflineCache(t *testing.T) (*OfflineCache, string) {
	t.Helper()
	dir := t.TempDir()
	return NewOfflineCache(filepath.Join(dir, "offline-cache")), dir
}

func TestOfflineCacheMarkAndGet(t *testing.T) {
	c, _ := newTestOfflineCache(t)
	c.MarkBuilding("sha256:abc123", "library/nginx:1.25", "1.25")
	p, ok := c.Get("sha256:abc123")
	if !ok {
		t.Fatal("expected pkg exists after MarkBuilding")
	}
	if p.State != OfflineBuilding {
		t.Fatalf("state = %q, want building", p.State)
	}
	c.MarkReady("sha256:abc123", 1024)
	p, _ = c.Get("sha256:abc123")
	if p.State != OfflineReady || p.Size != 1024 {
		t.Fatalf("after MarkReady: state=%q size=%d", p.State, p.Size)
	}
	if p.Mtime == 0 {
		t.Fatal("Mtime should be set on MarkReady")
	}
}

func TestOfflineCachePersist(t *testing.T) {
	c, dir := newTestOfflineCache(t)
	c.MarkBuilding("sha256:def456", "alpine:latest", "latest")
	c.MarkReady("sha256:def456", 2048)

	// 重新加载，验证清单持久化
	c2 := NewOfflineCache(filepath.Join(dir, "offline-cache"))
	p, ok := c2.Get("sha256:def456")
	if !ok || p.State != OfflineReady || p.Size != 2048 {
		t.Fatalf("reload mismatch: ok=%v pkg=%+v", ok, p)
	}
}

func TestOfflineCacheRemove(t *testing.T) {
	c, _ := newTestOfflineCache(t)
	c.MarkBuilding("sha256:xyz", "busybox:1.36", "1.36")
	c.MarkReady("sha256:xyz", 512)
	// 落一个真实 tar 文件
	if err := os.WriteFile(c.TarPath("sha256:xyz"), []byte("tar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("sha256:xyz"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := c.Get("sha256:xyz"); ok {
		t.Fatal("expected pkg gone after Remove")
	}
	if _, err := os.Stat(c.TarPath("sha256:xyz")); !os.IsNotExist(err) {
		t.Fatal("expected tar file removed")
	}
}

func TestOfflineCacheBuildingResetOnLoad(t *testing.T) {
	c, dir := newTestOfflineCache(t)
	c.MarkBuilding("sha256:ghost", "ghost:1", "1")
	// 模拟进程在打包中退出，重新加载应把 building 修正为 failed
	c2 := NewOfflineCache(filepath.Join(dir, "offline-cache"))
	p, ok := c2.Get("sha256:ghost")
	if !ok || p.State != OfflineFailed {
		t.Fatalf("expected building reset to failed, got %+v", p)
	}
}

func TestOfflineCachePurgeUntil(t *testing.T) {
	c, _ := newTestOfflineCache(t)
	// 三个 ready 包，Mtime 依次递增（先建的最旧）
	for i, d := range []string{"sha256:old", "sha256:mid", "sha256:new"} {
		c.MarkBuilding(d, "img:1", "1")
		c.MarkReady(d, int64(i+1)*100)
	}

	// freeBytes <= 0：不清理
	if n := c.PurgeUntil(0); n != 0 {
		t.Fatalf("PurgeUntil(0) removed %d, want 0", n)
	}
	// freeBytes 极大：磁盘永远不够，删除全部 ready
	if n := c.PurgeUntil(math.MaxInt64); n != 3 {
		t.Fatalf("PurgeUntil(MaxInt64) removed %d, want 3", n)
	}
	if len(c.List()) != 0 {
		t.Fatal("expected all ready pkgs purged")
	}
}

func TestOfflineCacheTarPath(t *testing.T) {
	c, _ := newTestOfflineCache(t)
	if got := c.TarPath("sha256:abc123"); filepath.Base(got) != "abc123.tar" {
		t.Fatalf("TarPath base = %q, want abc123.tar", filepath.Base(got))
	}
}

func TestOfflineCacheTasks(t *testing.T) {
	c, _ := newTestOfflineCache(t)
	c.StartTask("sha256:t1", "img:1", OfflineTaskBuilding)
	c.FinishTask("sha256:t1", OfflineTaskDone, "")
	tasks := c.Tasks()
	if len(tasks) != 1 || tasks[0].State != OfflineTaskDone {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestParseConfigCreated(t *testing.T) {
	ok := []byte(`{"created":"2026-10-09T12:34:56.123456789Z","architecture":"amd64"}`)
	if ts := parseConfigCreated(ok); ts == 0 {
		t.Fatal("expected parsed created timestamp")
	}
	if ts := parseConfigCreated([]byte(`{"created":""}`)); ts != 0 {
		t.Fatalf("empty created = %d, want 0", ts)
	}
	if ts := parseConfigCreated([]byte(`not-json`)); ts != 0 {
		t.Fatalf("invalid json = %d, want 0", ts)
	}
}

func TestOfflineCacheSetUpstream(t *testing.T) {
	c, dir := newTestOfflineCache(t)
	c.MarkBuilding("sha256:up1", "library/redis:7.2", "7.2")
	c.SetUpstream("sha256:up1", 1700000000)
	c.MarkReady("sha256:up1", 100)
	p, ok := c.Get("sha256:up1")
	if !ok || p.Upstream != 1700000000 {
		t.Fatalf("upstream not set: %+v", p)
	}
	// 重新加载验证持久化
	c2 := NewOfflineCache(filepath.Join(dir, "offline-cache"))
	p2, ok := c2.Get("sha256:up1")
	if !ok || p2.Upstream != 1700000000 {
		t.Fatalf("upstream not persisted: %+v", p2)
	}
}
