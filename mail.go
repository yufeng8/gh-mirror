package main

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"math/big"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// smtpConfig SMTP 发信配置（存 settings.json，超管在用户管理页设置）。
type smtpConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	TLS      bool   `json:"tls"` // true=隐式 TLS（465）；false=明文（25/587，无 StartTLS）
}

// ready 判断 SMTP 配置是否可用（host/from/port 必填）。
func (c smtpConfig) ready() bool {
	return c.Host != "" && c.From != "" && c.Port > 0
}

// sendMail 发送一封纯文本 UTF-8 邮件（标准库 net/smtp）。
func sendMail(cfg smtpConfig, to, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	// 固定顺序拼 header，避免 map 遍历顺序不确定
	msg := "From: " + cfg.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body

	if cfg.TLS {
		tlsCfg := &tls.Config{ServerName: cfg.Host}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		defer conn.Close()
		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if cfg.Username != "" {
			if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
				return err
			}
		}
		if err := client.Mail(cfg.From); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		wc, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := wc.Write([]byte(msg)); err != nil {
			return err
		}
		if err := wc.Close(); err != nil {
			return err
		}
		return client.Quit()
	}
	// 明文（无加密）
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	return smtp.SendMail(addr, auth, cfg.From, []string{to}, []byte(msg))
}

// 邮箱验证码参数：6 位、10 分钟有效、同邮箱 60 秒限流、最多尝试 5 次。
const (
	codeTTL      = 10 * time.Minute
	codeResend   = 60 * time.Second
	codeMaxTries = 5
	codeLen      = 6
)

type codeEntry struct {
	Code      string
	ExpiresAt time.Time
	SentAt    time.Time
	Attempts  int
}

// CodeStore 内存验证码存储（不持久化，进程重启即失效；惰性过期清理）。
type CodeStore struct {
	mu    sync.Mutex
	codes map[string]*codeEntry
}

func NewCodeStore() *CodeStore {
	return &CodeStore{codes: map[string]*codeEntry{}}
}

// Issue 为某邮箱签发验证码；同邮箱 60 秒内再次请求返回 ok=false（限流）。
func (c *CodeStore) Issue(email string) (code string, ok bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.codes {
		if now.After(e.ExpiresAt) {
			delete(c.codes, k)
		}
	}
	if e, exists := c.codes[email]; exists && now.Sub(e.SentAt) < codeResend {
		return "", false
	}
	code = genCode()
	c.codes[email] = &codeEntry{Code: code, ExpiresAt: now.Add(codeTTL), SentAt: now}
	return code, true
}

// Verify 校验验证码：不存在 / 过期 / 错误 / 尝试超限均返回 false；校验成功即删除。
func (c *CodeStore) Verify(email, code string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	c.mu.Lock()
	defer c.mu.Unlock()
	e, exists := c.codes[email]
	if !exists || time.Now().After(e.ExpiresAt) {
		return false
	}
	if e.Attempts >= codeMaxTries {
		delete(c.codes, email)
		return false
	}
	e.Attempts++
	if subtle.ConstantTimeCompare([]byte(e.Code), []byte(code)) == 1 {
		delete(c.codes, email)
		return true
	}
	return false
}

// genCode 生成 6 位数字验证码（crypto/rand，均匀分布）。
func genCode() string {
	var sb strings.Builder
	for i := 0; i < codeLen; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		sb.WriteString(strconv.Itoa(int(n.Int64())))
	}
	return sb.String()
}
