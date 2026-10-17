package main

import (
	"strings"
	"testing"
	"time"
)

func TestCaptchaNewVerify(t *testing.T) {
	c := NewCaptchaStore()
	cid := c.New()
	v := c.Value(cid)
	if len(v) != captchaLen {
		t.Fatalf("验证码长度应为 %d，实际 %d", captchaLen, len(v))
	}
	if !c.Verify(cid, v) {
		t.Fatal("正确验证码应通过")
	}
	// 一次性：校验后失效
	if c.Verify(cid, v) {
		t.Fatal("验证码应一次性，二次校验应失败")
	}
}

func TestCaptchaCaseInsensitive(t *testing.T) {
	c := NewCaptchaStore()
	cid := c.New()
	v := c.Value(cid)
	if !c.Verify(cid, strings.ToLower(v)) {
		t.Fatal("图形验证码应大小写不敏感")
	}
}

func TestCaptchaExpire(t *testing.T) {
	c := NewCaptchaStore()
	cid := c.New()
	c.mu.Lock()
	c.items[cid].ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if c.Value(cid) != "" {
		t.Fatal("过期后 Value 应返回空")
	}
	if c.Verify(cid, "xxxx") {
		t.Fatal("过期后 Verify 应失败")
	}
}

func TestCaptchaWrongValue(t *testing.T) {
	c := NewCaptchaStore()
	cid := c.New()
	v := c.Value(cid)
	// 每个字符映射到字符集的下一个字符，构造一个必然不同的错误值
	var sb strings.Builder
	for _, r := range v {
		idx := strings.IndexRune(captchaChars, r)
		sb.WriteByte(captchaChars[(idx+1)%len(captchaChars)])
	}
	if c.Verify(cid, sb.String()) {
		t.Fatal("错误验证码不应通过")
	}
}

func TestRandStr(t *testing.T) {
	for _, n := range []int{4, 16} {
		s := randStr(n, captchaChars)
		if len(s) != n {
			t.Fatalf("randStr(%d) 长度 = %d", n, len(s))
		}
		for _, r := range s {
			if !strings.ContainsRune(captchaChars, r) {
				t.Fatalf("randStr 含非法字符 %q", r)
			}
		}
	}
}

func TestRenderCaptcha(t *testing.T) {
	img := renderCaptcha("AB3K")
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("验证码图片尺寸异常: %v", b)
	}
}
