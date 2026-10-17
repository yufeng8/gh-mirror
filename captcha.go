package main

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 图形验证码：零依赖，5×7 点阵字体 + 干扰线 + 噪点输出 PNG。
// 用于注册/登录页与管理员登录页（开关分别可控），防脚本暴力破解。

const (
	captchaTTL  = 5 * time.Minute
	captchaLen  = 4
	captchaChars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 去易混淆 I/O/0/1
)

type captchaEntry struct {
	Value     string
	ExpiresAt time.Time
}

// CaptchaStore 内存验证码存储（cid → 值，5 分钟有效、一次性使用）。
type CaptchaStore struct {
	mu    sync.Mutex
	items map[string]*captchaEntry
}

func NewCaptchaStore() *CaptchaStore {
	return &CaptchaStore{items: map[string]*captchaEntry{}}
}

// New 生成一条验证码，返回 cid（值只存内存，由图片端点按 cid 渲染）。
func (c *CaptchaStore) New() string {
	value := randStr(captchaLen, captchaChars)
	cid := randStr(16, "abcdefghijklmnopqrstuvwxyz0123456789")
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.items {
		if now.After(e.ExpiresAt) {
			delete(c.items, k)
		}
	}
	c.items[cid] = &captchaEntry{Value: value, ExpiresAt: now.Add(captchaTTL)}
	return cid
}

// Value 返回某 cid 的验证码值（图片渲染用；不存在/过期返回空）。
func (c *CaptchaStore) Value(cid string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[cid]
	if !ok || time.Now().After(e.ExpiresAt) {
		return ""
	}
	return e.Value
}

// Verify 校验并删除（一次性；大小写不敏感）。
func (c *CaptchaStore) Verify(cid, submitted string) bool {
	if cid == "" || submitted == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[cid]
	if !ok || time.Now().After(e.ExpiresAt) {
		delete(c.items, cid)
		return false
	}
	delete(c.items, cid) // 一次性，无论对错都作废（防爆破）
	return strings.EqualFold(e.Value, strings.TrimSpace(submitted))
}

// randStr 生成指定长度的随机字符串（字符集 chars）。
func randStr(n int, chars string) string {
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[idx.Int64()]
	}
	return string(b)
}

// handleCaptchaNew 生成新验证码并返回 cid（前端点击刷新用）。
func (s *Server) handleCaptchaNew(w http.ResponseWriter, r *http.Request) {
	cid := s.captcha.New()
	s.writeJSON(w, http.StatusOK, map[string]string{"cid": cid})
}

// captchaHTML 生成图形验证码表单片段（hidden cid + 输入框 + 可点击刷新的图片）。
func (s *Server) captchaHTML() string {
	cid := s.captcha.New()
	return `<div class="captcha-row">` +
		`<input type="hidden" name="captchaId" value="` + cid + `">` +
		`<input name="captcha" placeholder="图形验证码" required autocomplete="off" aria-label="图形验证码">` +
		`<img src="/captcha?cid=` + cid + `" alt="图形验证码" width="100" height="36" style="cursor:pointer" onclick="gmRefreshCaptcha(this)" title="点击刷新">` +
		`</div>`
}

// handleCaptcha 渲染图形验证码 PNG（GET /captcha?cid=xxx）。
func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	cid := r.URL.Query().Get("cid")
	value := s.captcha.Value(cid)
	if value == "" {
		http.Error(w, "验证码已过期，请刷新", http.StatusBadRequest)
		return
	}
	img := renderCaptcha(value)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// renderCaptcha 画一张验证码图片（5×7 点阵字符放大 + 干扰线 + 噪点）。
func renderCaptcha(value string) image.Image {
	const (
		scale  = 3   // 点阵像素放大倍数
		gap    = 6   // 字符间距（px）
		padX   = 10  // 左右边距
		padY   = 8   // 上下边距
		dotW   = 5   // 点阵宽
		dotH   = 7   // 点阵高
	)
	chW := dotW * scale
	chH := dotH * scale
	width := padX*2 + chW*len(value) + gap*(len(value)-1)
	height := padY*2 + chH
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 随机浅色背景
	bg := randLightColor()
	fill(img, bg)

	// 画字符
	fg := randDarkColor()
	for i, ch := range value {
		glyph, ok := captchaFont[ch]
		if !ok {
			glyph = captchaFont['?']
		}
		ox := padX + i*(chW+gap)
		oy := padY
		for y := 0; y < dotH; y++ {
			for x := 0; x < dotW; x++ {
				if glyph[y]&(1<<uint(dotW-1-x)) != 0 {
					fillRect(img, ox+x*scale, oy+y*scale, scale, scale, fg)
				}
			}
		}
	}

	// 干扰线
	for i := 0; i < 4; i++ {
		drawLine(img, randInt(width), randInt(height), randInt(width), randInt(height), randColor())
	}
	// 噪点
	for i := 0; i < 40; i++ {
		img.Set(randInt(width), randInt(height), randColor())
	}
	return img
}

func fill(img *image.RGBA, c color.RGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}

func fillRect(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if image.Pt(xx, yy).In(img.Bounds()) {
				img.Set(xx, yy, c)
			}
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		if image.Pt(x0, y0).In(img.Bounds()) {
			img.Set(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

func randLightColor() color.RGBA {
	return color.RGBA{uint8(200 + randInt(56)), uint8(200 + randInt(56)), uint8(200 + randInt(56)), 255}
}

func randDarkColor() color.RGBA {
	return color.RGBA{uint8(20 + randInt(120)), uint8(20 + randInt(120)), uint8(20 + randInt(120)), 255}
}

func randColor() color.RGBA {
	return color.RGBA{uint8(randInt(256)), uint8(randInt(256)), uint8(randInt(256)), 255}
}

// captchaFont 5×7 点阵字体（每字符 7 行，每行低 5 bit 有效，1 为实心）。
var captchaFont = map[rune][7]byte{
	'2': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
	'3': {0x1E, 0x01, 0x01, 0x0E, 0x01, 0x01, 0x1E},
	'4': {0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	'5': {0x1F, 0x10, 0x10, 0x1E, 0x01, 0x01, 0x1E},
	'6': {0x0E, 0x10, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x01, 0x0E},
	'A': {0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'B': {0x1E, 0x11, 0x11, 0x1E, 0x11, 0x11, 0x1E},
	'C': {0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E},
	'D': {0x1E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x1E},
	'E': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F},
	'F': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x10},
	'G': {0x0E, 0x11, 0x10, 0x17, 0x11, 0x11, 0x0F},
	'H': {0x11, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'J': {0x07, 0x02, 0x02, 0x02, 0x02, 0x12, 0x0C},
	'K': {0x11, 0x12, 0x14, 0x18, 0x14, 0x12, 0x11},
	'L': {0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x1F},
	'M': {0x11, 0x1B, 0x15, 0x15, 0x11, 0x11, 0x11},
	'N': {0x11, 0x19, 0x15, 0x13, 0x11, 0x11, 0x11},
	'P': {0x1E, 0x11, 0x11, 0x1E, 0x10, 0x10, 0x10},
	'Q': {0x0E, 0x11, 0x11, 0x11, 0x15, 0x12, 0x0D},
	'R': {0x1E, 0x11, 0x11, 0x1E, 0x14, 0x12, 0x11},
	'S': {0x0F, 0x10, 0x10, 0x0E, 0x01, 0x01, 0x1E},
	'T': {0x1F, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04},
	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	'V': {0x11, 0x11, 0x11, 0x11, 0x11, 0x0A, 0x04},
	'W': {0x11, 0x11, 0x11, 0x15, 0x15, 0x15, 0x0A},
	'X': {0x11, 0x11, 0x0A, 0x04, 0x0A, 0x11, 0x11},
	'Y': {0x11, 0x11, 0x0A, 0x04, 0x04, 0x04, 0x04},
	'Z': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x10, 0x1F},
	'?': {0x0E, 0x11, 0x01, 0x02, 0x04, 0x00, 0x04},
}
