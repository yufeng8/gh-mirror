package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestInviteStore(t *testing.T) *InviteStore {
	t.Helper()
	return NewInviteStore(filepath.Join(t.TempDir(), "invites.json"))
}

func TestInviteGenerateAndClaim(t *testing.T) {
	is := newTestInviteStore(t)
	codes, err := is.Generate(5, "测试批次", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 5 {
		t.Fatalf("生成数量 = %d, want 5", len(codes))
	}
	// 去重
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != inviteLen {
			t.Fatalf("邀请码长度 = %d, want %d", len(c), inviteLen)
		}
		if seen[c] {
			t.Fatal("邀请码重复")
		}
		seen[c] = true
	}
	// Claim：首次成功，二次失败
	if !is.Claim(codes[0], "alice") {
		t.Fatal("首次 Claim 应成功")
	}
	if is.Claim(codes[0], "bob") {
		t.Fatal("已使用的邀请码不应再成功")
	}
	// 使用追踪
	inv, _ := is.Get(codes[0])
	if !inv.Used || inv.UsedBy != "alice" || inv.UsedAt == 0 {
		t.Fatalf("使用追踪缺失: %+v", inv)
	}
}

func TestInviteExpire(t *testing.T) {
	is := newTestInviteStore(t)
	codes, _ := is.Generate(1, "", time.Now().Add(-time.Hour).Unix())
	if is.Claim(codes[0], "alice") {
		t.Fatal("过期邀请码不应成功")
	}
}

func TestInviteAddCustomDuplicate(t *testing.T) {
	is := newTestInviteStore(t)
	if err := is.AddCustom("WELCOME2026", "欢迎", 0); err != nil {
		t.Fatal(err)
	}
	if err := is.AddCustom("welcome-2026", "重复", 0); err == nil {
		t.Fatal("重复邀请码（含连字符/大小写）应报错")
	}
	// 规整后一致
	if _, ok := is.Get("WELCOME2026"); !ok {
		t.Fatal("规整后应能查到")
	}
}

func TestInviteDeleteAndNote(t *testing.T) {
	is := newTestInviteStore(t)
	codes, _ := is.Generate(3, "", 0)
	if n := is.Delete(codes[:2]); n != 2 {
		t.Fatalf("删除数量 = %d, want 2", n)
	}
	if n := is.SetNote([]string{codes[2]}, "新备注"); n != 1 {
		t.Fatalf("打备注数量 = %d, want 1", n)
	}
	inv, _ := is.Get(codes[2])
	if inv.Note != "新备注" {
		t.Fatalf("备注未更新: %q", inv.Note)
	}
}

func TestInviteExport(t *testing.T) {
	is := newTestInviteStore(t)
	codes, _ := is.Generate(2, "批次A", 0)
	out := is.Export(codes, "{code}|{note}|{expire}|{used}")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("导出行数 = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "|批次A|永久|未使用") {
		t.Fatalf("导出格式错误: %q", lines[0])
	}
}

func TestInviteClaimNormalize(t *testing.T) {
	is := newTestInviteStore(t)
	codes, _ := is.Generate(1, "", 0)
	// 输入带连字符/小写也能 Claim
	if !is.Claim(strings.ToLower(codes[0][:4]+"-"+codes[0][4:8]+"-"+codes[0][8:12]+"-"+codes[0][12:]), "alice") {
		t.Fatal("规整后的邀请码应能 Claim")
	}
}
