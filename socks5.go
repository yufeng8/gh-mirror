package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// socks5Dialer 通过 SOCKS5 代理建立出站连接（支持无认证与用户名/密码认证）。
type socks5Dialer struct {
	proxyAddr string
	user      string
	pass      string
	timeout   time.Duration
}

// parseSocks5 解析 socks5://[user:pass@]host:port 形式。
func parseSocks5(proxy string) (addr, user, pass string) {
	p := strings.TrimPrefix(proxy, "socks5://")
	p = strings.TrimPrefix(p, "socks5h://")
	if at := strings.LastIndex(p, "@"); at >= 0 {
		cred := p[:at]
		p = p[at+1:]
		if c := strings.IndexByte(cred, ':'); c >= 0 {
			user, pass = cred[:c], cred[c+1:]
		} else {
			user = cred
		}
	}
	if !strings.Contains(p, ":") {
		p = net.JoinHostPort(p, "1080")
	}
	return p, user, pass
}

func newSocks5Dialer(proxy string) *socks5Dialer {
	addr, user, pass := parseSocks5(proxy)
	return &socks5Dialer{proxyAddr: addr, user: user, pass: pass, timeout: 30 * time.Second}
}

func (d *socks5Dialer) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", d.proxyAddr, d.timeout)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err = d.handshake(conn); err != nil {
		conn.Close()
		return nil, err
	}
	if err = d.connect(conn, host, portStr); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{}) // 清除超时，进入长连接传输
	return conn, nil
}

func (d *socks5Dialer) handshake(c net.Conn) error {
	methods := []byte{0x00}
	if d.user != "" {
		methods = []byte{0x00, 0x02}
	}
	if _, err := c.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		return err
	}
	if resp[0] != 0x05 {
		return errors.New("socks5: 非 SOCKS5 响应")
	}
	switch resp[1] {
	case 0x00:
		return nil
	case 0x02:
		return d.userPassAuth(c)
	case 0xFF:
		return errors.New("socks5: 无可接受认证方法")
	default:
		return fmt.Errorf("socks5: 不支持的认证方法 %d", resp[1])
	}
}

func (d *socks5Dialer) userPassAuth(c net.Conn) error {
	if d.user == "" {
		return errors.New("socks5: 需要用户名/密码")
	}
	buf := []byte{0x01, byte(len(d.user))}
	buf = append(buf, d.user...)
	buf = append(buf, byte(len(d.pass)))
	buf = append(buf, d.pass...)
	if _, err := c.Write(buf); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		return err
	}
	if resp[1] != 0x00 {
		return errors.New("socks5: 用户名/密码认证失败")
	}
	return nil
}

func (d *socks5Dialer) connect(c net.Conn, host, port string) error {
	portNum, err := net.LookupPort("tcp", port)
	if err != nil {
		return err
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("socks5: 域名过长")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	pb := make([]byte, 2)
	binary.BigEndian.PutUint16(pb, uint16(portNum))
	req = append(req, pb...)
	if _, err := c.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5: 连接失败 rep=%d", head[1])
	}
	var addrLen int
	switch head[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return err
		}
		addrLen = int(l[0])
	default:
		return errors.New("socks5: 未知 ATYP")
	}
	if _, err := io.ReadFull(c, make([]byte, addrLen+2)); err != nil {
		return err
	}
	return nil
}

// transportWithDial 基于给定拨号器构建出站 Transport（用于 SOCKS5 场景）。
func transportWithDial(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dial,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}
