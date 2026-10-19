package main

import (
	"path/filepath"
	"testing"
)

func newScopeTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return &Server{
		store:     NewStore(filepath.Join(dir, "tokens.json")),
		users:     NewUserStore(filepath.Join(dir, "users.json")),
		adminUser: "admin",
	}
}

// TestTokensForUserOnlyOwn 验证任何角色都只能看到自己的 Token（空 owner 归默认管理员）。
func TestTokensForUserOnlyOwn(t *testing.T) {
	s := newScopeTestServer(t)
	s.store.Add("tokAdmin", "", RoleAdmin, "") // 空 owner 归 admin
	s.store.Add("tokBob", "", RoleUser, "bobby")
	s.store.Add("tokMgr", "", RoleUser, "mgr")

	check := func(user, role, wantTok string) {
		t.Helper()
		got := s.tokensForUser(s.store.All(), user, role)
		if len(got) != 1 || got[0].Token != wantTok {
			names := make([]string, 0, len(got))
			for _, x := range got {
				names = append(names, x.Token)
			}
			t.Errorf("%s(%s) 应只看到 %s，实际 %v", user, role, wantTok, names)
		}
	}
	check("admin", RoleSuper, "tokAdmin") // 超管不再全量
	check("mgr", RoleSuper, "tokMgr")     // 管理员（super 非种子）只看自己的
	check("bobby", RoleNormal, "tokBob")
}

// TestSetLastLogin 验证登录时间写入与未知用户静默处理。
func TestSetLastLogin(t *testing.T) {
	dir := t.TempDir()
	us := NewUserStore(filepath.Join(dir, "users.json"))
	if err := us.Add("alice", "secret123", RoleNormal); err != nil {
		t.Fatal(err)
	}
	if err := us.SetLastLogin("alice"); err != nil {
		t.Fatal(err)
	}
	u, ok := us.Get("alice")
	if !ok || u.LastLoginAt <= 0 {
		t.Fatalf("LastLoginAt 未更新: %+v", u)
	}
	if err := us.SetLastLogin("nobody"); err != nil {
		t.Fatalf("不存在的用户应静默返回: %v", err)
	}
}
