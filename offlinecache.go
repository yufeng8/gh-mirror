package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 离线镜像包的状态：ready 可下载命中；building 打包中；failed 上次打包失败。
const (
	OfflineReady    = "ready"
	OfflineBuilding = "building"
	OfflineFailed   = "failed"
)

// 异步拉取任务状态：queued 排队、building 打包中、done 完成、failed 失败。
const (
	OfflineTaskQueued   = "queued"
	OfflineTaskBuilding = "building"
	OfflineTaskDone     = "done"
	OfflineTaskFailed   = "failed"
)

// OfflinePkg 表示一个已缓存（或正在打包）的离线镜像包。以平台选定后的 manifest digest 为键（内容寻址）。
type OfflinePkg struct {
	Digest string `json:"digest"` // 平台选定后的 manifest digest（如 sha256:...）
	Image  string `json:"image"`  // 镜像名（含 tag，如 library/nginx:1.25）
	Ref    string `json:"ref"`    // 原始 tag 或 digest
	Size   int64  `json:"size"`   // 落盘 tar 大小（字节）
	Mtime  int64  `json:"mtime"`  // 最近打包完成时间（unix 秒）
	State  string `json:"state"`  // ready / building / failed
}

// OfflineTask 表示一次异步拉取任务（内存态，不落盘；用于管理页展示进度）。
type OfflineTask struct {
	Digest  string `json:"digest"`
	Image   string `json:"image"`
	State   string `json:"state"` // queued / building / done / failed
	Err     string `json:"err,omitempty"`
	Started int64  `json:"started"`
}

// OfflineCache 管理离线镜像 tar 的内容寻址缓存：目录、清单、每 digest 锁、异步任务状态。
type OfflineCache struct {
	dir   string
	path  string // manifest.json 路径
	mu    sync.RWMutex
	pkgs  map[string]*OfflinePkg
	locks map[string]*sync.RWMutex // 每 digest 一把锁（下载读锁、重拉写锁）
	tasks map[string]*OfflineTask
}

// NewOfflineCache 创建缓存实例：确保目录存在并加载清单。
func NewOfflineCache(dir string) *OfflineCache {
	c := &OfflineCache{
		dir:   dir,
		path:  filepath.Join(dir, "manifest.json"),
		pkgs:  map[string]*OfflinePkg{},
		locks: map[string]*sync.RWMutex{},
		tasks: map[string]*OfflineTask{},
	}
	_ = os.MkdirAll(dir, 0o700)
	c.load()
	return c
}

func (c *OfflineCache) load() {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var list []*OfflinePkg
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, p := range list {
		if p.State == OfflineBuilding {
			// 上次进程在打包中退出，视为失败（避免留下永远 building 的幽灵包）
			p.State = OfflineFailed
		}
		c.pkgs[p.Digest] = p
	}
}

// saveLocked 必须在持有 c.mu 时调用。原子写（临时文件 + rename）。
func (c *OfflineCache) saveLocked() error {
	list := make([]*OfflinePkg, 0, len(c.pkgs))
	for _, p := range c.pkgs {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Mtime > list[j].Mtime })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Get 返回指定 digest 的包（不存在返回 false）。
func (c *OfflineCache) Get(digest string) (*OfflinePkg, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.pkgs[digest]
	return p, ok
}

// List 返回全部包（按 Mtime 降序，最新在前）。
func (c *OfflineCache) List() []*OfflinePkg {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*OfflinePkg, 0, len(c.pkgs))
	for _, p := range c.pkgs {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Mtime > list[j].Mtime })
	return list
}

// LockFor 返回某 digest 的读写锁（惰性创建）。下载命中读锁、重拉/删除写锁。
func (c *OfflineCache) LockFor(digest string) *sync.RWMutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.locks[digest]; ok {
		return l
	}
	l := &sync.RWMutex{}
	c.locks[digest] = l
	return l
}

// TarPath 返回某 digest 的 tar 文件路径（digest 只取 hex 部分，避免路径穿越）。
func (c *OfflineCache) TarPath(digest string) string {
	return filepath.Join(c.dir, hexOf(digest)+".tar")
}

// MarkBuilding 记录「开始打包」：加入/更新清单为 building 状态（不落盘磁盘文件，仅清单）。
func (c *OfflineCache) MarkBuilding(digest, image, ref string) {
	c.mu.Lock()
	p, ok := c.pkgs[digest]
	if !ok {
		p = &OfflinePkg{Digest: digest}
		c.pkgs[digest] = p
	}
	p.Image = image
	p.Ref = ref
	p.State = OfflineBuilding
	_ = c.saveLocked()
	c.mu.Unlock()
}

// MarkReady 记录「打包完成」：状态 ready、记录大小与时间。
func (c *OfflineCache) MarkReady(digest string, size int64) {
	c.mu.Lock()
	if p, ok := c.pkgs[digest]; ok {
		p.State = OfflineReady
		p.Size = size
		p.Mtime = time.Now().Unix()
		_ = c.saveLocked()
	}
	c.mu.Unlock()
}

// MarkFailed 记录「打包失败」：状态 failed（保留清单供管理页提示重试）。
func (c *OfflineCache) MarkFailed(digest string) {
	c.mu.Lock()
	if p, ok := c.pkgs[digest]; ok {
		p.State = OfflineFailed
		_ = c.saveLocked()
	}
	c.mu.Unlock()
}

// Remove 删除某 digest 的 tar 文件与清单条目（文件路径由 digest 推导，来自清单，无路径穿越）。
func (c *OfflineCache) Remove(digest string) error {
	c.mu.Lock()
	delete(c.pkgs, digest)
	_ = c.saveLocked()
	c.mu.Unlock()
	return os.Remove(c.TarPath(digest))
}

// TotalBytes 返回全部缓存包落盘大小之和。
func (c *OfflineCache) TotalBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var n int64
	for _, p := range c.pkgs {
		n += p.Size
	}
	return n
}

// PurgeUntil 按 Mtime 最旧优先删除离线包，直到磁盘可用空间 >= freeBytes 或清空；返回删除数。
// 只删除 ready 状态的包（building/failed 不占完整 tar，且不应被并发清理误删）。
// freeBytes <= 0 表示未设置阈值，不清理。
func (c *OfflineCache) PurgeUntil(freeBytes int64) int {
	if freeBytes <= 0 {
		return 0
	}
	c.mu.Lock()
	list := make([]*OfflinePkg, 0, len(c.pkgs))
	for _, p := range c.pkgs {
		if p.State == OfflineReady {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Mtime < list[j].Mtime }) // 最旧在前
	removed := 0
	for _, p := range list {
		if diskFree(c.dir) >= freeBytes && freeBytes > 0 {
			break
		}
		delete(c.pkgs, p.Digest)
		removed++
	}
	if removed > 0 {
		_ = c.saveLocked()
	}
	c.mu.Unlock()
	for _, p := range list[:removed] {
		_ = os.Remove(c.TarPath(p.Digest))
	}
	return removed
}

// --- 异步拉取任务（内存态） ---

// StartTask 记录一个异步拉取任务为排队/打包中。
func (c *OfflineCache) StartTask(digest, image, state string) {
	c.mu.Lock()
	c.tasks[digest] = &OfflineTask{Digest: digest, Image: image, State: state, Started: time.Now().Unix()}
	c.mu.Unlock()
}

// FinishTask 标记任务完成或失败。
func (c *OfflineCache) FinishTask(digest, state, errMsg string) {
	c.mu.Lock()
	if t, ok := c.tasks[digest]; ok {
		t.State = state
		t.Err = errMsg
	}
	c.mu.Unlock()
}

// Tasks 返回全部异步任务（含历史，按 Started 降序）。
func (c *OfflineCache) Tasks() []*OfflineTask {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]*OfflineTask, 0, len(c.tasks))
	for _, t := range c.tasks {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Started > list[j].Started })
	return list
}

// PurgeTasks 清理已结束（done/failed）且较早的历史任务，避免内存无限增长。
func (c *OfflineCache) PurgeTasks(keep int) {
	c.mu.Lock()
	list := make([]*OfflineTask, 0, len(c.tasks))
	for _, t := range c.tasks {
		if t.State == OfflineTaskDone || t.State == OfflineTaskFailed {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Started > list[j].Started })
	for i := keep; i < len(list); i++ {
		delete(c.tasks, list[i].Digest)
	}
	c.mu.Unlock()
}
