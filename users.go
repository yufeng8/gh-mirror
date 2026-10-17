package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 后台账号角色：super 可管理所有用户并看全局；normal 只管理自己的资源。
const (
	RoleSuper  = "super"
	RoleNormal = "normal"
)

// pbkdf2 参数：迭代 10 万次（标准库手写，零第三方依赖）。
const (
	pbkdf2Iterations = 100_000
	pbkdf2SaltLen    = 16
	pbkdf2KeyLen     = 32
)

// User 代表一个后台登录账号。PassHash/Salt 为 hex 编码。
// DockerPath 是该用户自己的 Docker 拉取路径后缀（多用户隔离；空=只用用户名段）。
type User struct {
	Username   string `json:"username"`
	PassHash   string `json:"pass_hash"`
	Salt       string `json:"salt"`
	Role       string `json:"role"` // super / normal
	DockerPath string `json:"docker_path,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// normUserRole 归一化用户角色：空值或非法值按 normal 处理（super 需显式指定）。
func normUserRole(r string) string {
	if r == RoleSuper {
		return RoleSuper
	}
	return RoleNormal
}

// validUsername 校验用户名：2-32 位，仅字母、数字、`.`、`_`、`-`。
func validUsername(name string) bool {
	if len(name) < 2 || len(name) > 32 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// UserStore 以 JSON 文件持久化用户列表，读写加锁（复用 Store 的原子写模式）。
type UserStore struct {
	mu    sync.RWMutex
	path  string
	users map[string]*User
}

func NewUserStore(path string) *UserStore {
	us := &UserStore{path: path, users: map[string]*User{}}
	us.load()
	return us
}

func (us *UserStore) load() {
	b, err := os.ReadFile(us.path)
	if err != nil {
		return
	}
	var list []*User
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	for _, u := range list {
		u.Role = normUserRole(u.Role)
		us.users[u.Username] = u
	}
}

func (us *UserStore) saveLocked() error {
	list := make([]*User, 0, len(us.users))
	for _, u := range us.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(us.path), 0o700); err != nil {
		return err
	}
	tmp := us.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, us.path)
}

// Get 返回指定用户（不存在返回 false）。
func (us *UserStore) Get(username string) (*User, bool) {
	us.mu.RLock()
	defer us.mu.RUnlock()
	u, ok := us.users[username]
	return u, ok
}

func (us *UserStore) Has(username string) bool {
	_, ok := us.Get(username)
	return ok
}

// Add 新增用户（用户名去空格；密码哈希后落盘）。用户名已存在返回错误。
func (us *UserStore) Add(username, pass, role string) error {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return fmt.Errorf("用户名不合法（2-32 位，仅字母、数字、.、_、-）")
	}
	if len(pass) < 4 {
		return fmt.Errorf("密码至少 4 位")
	}
	salt, hash, err := hashPassword(pass)
	if err != nil {
		return err
	}
	us.mu.Lock()
	defer us.mu.Unlock()
	if _, ok := us.users[username]; ok {
		return fmt.Errorf("用户已存在: %s", username)
	}
	us.users[username] = &User{
		Username:  username,
		PassHash:  hash,
		Salt:      salt,
		Role:      normUserRole(role),
		CreatedAt: time.Now().Unix(),
	}
	return us.saveLocked()
}

// Delete 删除用户。不允许删除最后一个 super（防止把自己锁死）。
func (us *UserStore) Delete(username string) error {
	us.mu.Lock()
	defer us.mu.Unlock()
	u, ok := us.users[username]
	if !ok {
		return fmt.Errorf("用户不存在: %s", username)
	}
	if u.Role == RoleSuper {
		cnt := 0
		for _, x := range us.users {
			if x.Role == RoleSuper {
				cnt++
			}
		}
		if cnt <= 1 {
			return fmt.Errorf("不能删除最后一个超级管理员")
		}
	}
	delete(us.users, username)
	return us.saveLocked()
}

// UpdateRole 修改用户角色。不允许把最后一个 super 降级为 normal。
func (us *UserStore) UpdateRole(username, role string) error {
	role = normUserRole(role)
	us.mu.Lock()
	defer us.mu.Unlock()
	u, ok := us.users[username]
	if !ok {
		return fmt.Errorf("用户不存在: %s", username)
	}
	if u.Role == RoleSuper && role != RoleSuper {
		cnt := 0
		for _, x := range us.users {
			if x.Role == RoleSuper {
				cnt++
			}
		}
		if cnt <= 1 {
			return fmt.Errorf("不能降级最后一个超级管理员")
		}
	}
	u.Role = role
	return us.saveLocked()
}

// SetDockerPath 设置某用户的 Docker 自定义路径后缀（规范化后落盘）。
func (us *UserStore) SetDockerPath(username, path string) error {
	np, ok := normDockerPath(path)
	if !ok {
		return fmt.Errorf("自定义路径不合法（仅支持字母、数字、-、_、/，最长 64 字符）")
	}
	us.mu.Lock()
	defer us.mu.Unlock()
	u, ok := us.users[username]
	if !ok {
		return fmt.Errorf("用户不存在: %s", username)
	}
	u.DockerPath = np
	return us.saveLocked()
}

// ResetPassword 重置用户密码。
func (us *UserStore) ResetPassword(username, pass string) error {
	if len(pass) < 4 {
		return fmt.Errorf("密码至少 4 位")
	}
	salt, hash, err := hashPassword(pass)
	if err != nil {
		return err
	}
	us.mu.Lock()
	defer us.mu.Unlock()
	u, ok := us.users[username]
	if !ok {
		return fmt.Errorf("用户不存在: %s", username)
	}
	u.Salt = salt
	u.PassHash = hash
	return us.saveLocked()
}

// Verify 校验用户名与密码是否匹配（恒定时间比较，防时序侧信道）。
func (us *UserStore) Verify(username, pass string) bool {
	us.mu.RLock()
	defer us.mu.RUnlock()
	u, ok := us.users[username]
	if !ok {
		return false
	}
	return verifyPassword(pass, u.Salt, u.PassHash)
}

// All 返回全部用户（按用户名排序）。
func (us *UserStore) All() []*User {
	us.mu.RLock()
	defer us.mu.RUnlock()
	list := make([]*User, 0, len(us.users))
	for _, u := range us.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })
	return list
}

// hashPassword 生成随机盐并做 PBKDF2-HMAC-SHA256，返回 (saltHex, hashHex)。
func hashPassword(pass string) (salt, hash string, err error) {
	saltBytes := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", err
	}
	dk := pbkdf2SHA256([]byte(pass), saltBytes, pbkdf2Iterations, pbkdf2KeyLen)
	return hex.EncodeToString(saltBytes), hex.EncodeToString(dk), nil
}

// verifyPassword 重算 PBKDF2 并恒定时间比较。
func verifyPassword(pass, saltHex, hashHex string) bool {
	salt, err1 := hex.DecodeString(saltHex)
	want, err2 := hex.DecodeString(hashHex)
	if err1 != nil || err2 != nil || len(want) != pbkdf2KeyLen {
		return false
	}
	got := pbkdf2SHA256([]byte(pass), salt, pbkdf2Iterations, pbkdf2KeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 标准 PBKDF2（RFC 2898）：DK = U1 ^ U2 ^ ...，U_i = HMAC(password, U_{i-1})。
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hLen := sha256.Size
	blocks := (keyLen + hLen - 1) / hLen
	dk := make([]byte, 0, blocks*hLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := make([]byte, hLen)
		copy(t, u)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
