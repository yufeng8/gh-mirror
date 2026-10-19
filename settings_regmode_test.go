package main

import "testing"

// TestNormRegMode 覆盖合法/非法/大小写/空白输入，非法值一律归 closed。
func TestNormRegMode(t *testing.T) {
	cases := map[string]string{
		"":         RegClosed,
		"   ":      RegClosed,
		"whatever": RegClosed,
		"closed":   RegClosed,
		"invite":   RegInvite,
		"INVITE":   RegInvite,
		" Invite ": RegInvite,
		"open":     RegOpen,
		" Open ":   RegOpen,
	}
	for in, want := range cases {
		if got := normRegMode(in); got != want {
			t.Errorf("normRegMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDeriveRegMode 验证旧 settings.json（regOpen + inviteOnly）到三态的迁移规则。
func TestDeriveRegMode(t *testing.T) {
	cases := []struct {
		open, invite bool
		want         string
	}{
		{false, false, RegClosed},
		{false, true, RegClosed}, // 注册关闭优先
		{true, true, RegInvite},
		{true, false, RegOpen},
	}
	for _, c := range cases {
		if got := deriveRegMode(c.open, c.invite); got != c.want {
			t.Errorf("deriveRegMode(%v,%v) = %q, want %q", c.open, c.invite, got, c.want)
		}
	}
}
