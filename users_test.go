package main

import (
	"path/filepath"
	"testing"
)

func newTestUserStore(t *testing.T) *UserStore {
	t.Helper()
	return NewUserStore(filepath.Join(t.TempDir(), "users.json"))
}

func TestUserStoreAddVerify(t *testing.T) {
	us := newTestUserStore(t)
	if err := us.Add("alice", "secret123", RoleSuper); err != nil {
		t.Fatalf("新增用户失败: %v", err)
	}
	if !us.Verify("alice", "secret123") {
		t.Fatal("正确密码应校验通过")
	}
	if us.Verify("alice", "wrong") {
		t.Fatal("错误密码不应通过")
	}
	if us.Verify("bob", "secret123") {
		t.Fatal("不存在的用户不应通过")
	}
}

func TestUserStoreDuplicateAndRoles(t *testing.T) {
	us := newTestUserStore(t)
	if err := us.Add("alice", "secret123", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := us.Add("alice", "other", RoleNormal); err == nil {
		t.Fatal("重复用户名应报错")
	}
	if err := us.Add("bob", "secret123", ""); err != nil {
		t.Fatal(err)
	}
	u, _ := us.Get("bob")
	if u.Role != RoleNormal {
		t.Fatalf("空角色应归一化为 normal，实际 %q", u.Role)
	}
	if err := us.UpdateRole("bob", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if u2, _ := us.Get("bob"); u2.Role != RoleSuper {
		t.Fatalf("改角色失败，实际 %q", u2.Role)
	}
}

func TestUserStoreDeleteLastSuper(t *testing.T) {
	us := newTestUserStore(t)
	if err := us.Add("root", "secret123", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := us.Add("bob", "secret123", RoleNormal); err != nil {
		t.Fatal(err)
	}
	if err := us.Delete("root"); err == nil {
		t.Fatal("删除最后一个 super 应被拒绝")
	}
	if err := us.UpdateRole("root", RoleNormal); err == nil {
		t.Fatal("降级最后一个 super 应被拒绝")
	}
	// 再加一个 super 后即可删除
	if err := us.Add("root2", "secret123", RoleSuper); err != nil {
		t.Fatal(err)
	}
	if err := us.Delete("root"); err != nil {
		t.Fatalf("存在两个 super 时应可删除: %v", err)
	}
}

func TestValidUsername(t *testing.T) {
	for name, want := range map[string]bool{
		"admin":        true,
		"team.a-1":     true,
		"a":            false, // 太短
		"":             false,
		"has space":    false,
		"中文":           false,
		"x/../etc":     false,
		"abcdefghijklmnopqrstuvwxyz012345": true,  // 32 位
		"abcdefghijklmnopqrstuvwxyz0123456": false, // 33 位
	} {
		if got := validUsername(name); got != want {
			t.Errorf("validUsername(%q)=%v, want %v", name, got, want)
		}
	}
}

func TestHashPasswordRoundtrip(t *testing.T) {
	salt, hash, err := hashPassword("p@ssw0rd")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword("p@ssw0rd", salt, hash) {
		t.Fatal("校验应通过")
	}
	if verifyPassword("p@ssw0rdx", salt, hash) {
		t.Fatal("错误密码不应通过")
	}
	// 两次哈希盐不同（随机）
	salt2, hash2, _ := hashPassword("p@ssw0rd")
	if salt == salt2 && hash == hash2 {
		t.Fatal("随机盐应使两次哈希不同")
	}
}
