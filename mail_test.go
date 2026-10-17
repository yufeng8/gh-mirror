package main

import (
	"testing"
	"time"
)

func TestCodeStoreIssueVerify(t *testing.T) {
	c := NewCodeStore()
	code, ok := c.Issue("user@example.com")
	if !ok {
		t.Fatal("首次签发应成功")
	}
	if len(code) != 6 {
		t.Fatalf("验证码长度应为 6，实际 %d", len(code))
	}
	if !c.Verify("user@example.com", code) {
		t.Fatal("正确验证码应通过")
	}
	if c.Verify("user@example.com", code) {
		t.Fatal("验证码用一次后应失效")
	}
}

func TestCodeStoreResendLimit(t *testing.T) {
	c := NewCodeStore()
	if _, ok := c.Issue("a@b.com"); !ok {
		t.Fatal("首次应成功")
	}
	if _, ok := c.Issue("a@b.com"); ok {
		t.Fatal("60 秒内重复签发应被限流")
	}
}

func TestCodeStoreExpire(t *testing.T) {
	c := NewCodeStore()
	code, _ := c.Issue("a@b.com")
	// 手动把过期时间改到过去，模拟过期
	c.mu.Lock()
	c.codes["a@b.com"].ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if c.Verify("a@b.com", code) {
		t.Fatal("过期验证码不应通过")
	}
}

func TestCodeStoreMaxTries(t *testing.T) {
	c := NewCodeStore()
	code, _ := c.Issue("a@b.com")
	for i := 0; i < codeMaxTries; i++ {
		if c.Verify("a@b.com", "000000") {
			t.Fatal("错误验证码不应通过")
		}
	}
	// 尝试超限后，即使正确验证码也失效
	if c.Verify("a@b.com", code) {
		t.Fatal("尝试超限后正确验证码也应失效")
	}
}

func TestCodeStoreCaseInsensitiveEmail(t *testing.T) {
	c := NewCodeStore()
	code, _ := c.Issue("User@Example.COM")
	if !c.Verify("user@example.com", code) {
		t.Fatal("邮箱大小写不敏感")
	}
}

func TestSMTPReady(t *testing.T) {
	if (smtpConfig{}).ready() {
		t.Fatal("空配置不应就绪")
	}
	if !(smtpConfig{Host: "smtp.x.com", Port: 465, From: "a@x.com"}).ready() {
		t.Fatal("host/port/from 齐全应就绪")
	}
}
