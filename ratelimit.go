package main

import (
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// RateLimiter 按归一化客户端 IP 的令牌桶限流（容量=quota，补充速率=quota/periodSeconds）。
type RateLimiter struct {
	mu      sync.Mutex
	m       map[string]*bucket
	rate    float64 // tokens/秒
	burst   float64
	cleanup time.Duration
}

func NewRateLimiter(periodSeconds, quota int) *RateLimiter {
	if periodSeconds <= 0 {
		periodSeconds = 3600
	}
	if quota <= 0 {
		quota = 1
	}
	rl := &RateLimiter{
		m:       map[string]*bucket{},
		burst:   float64(quota),
		rate:    float64(quota) / float64(periodSeconds),
		cleanup: 10 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// Allow 消耗一个令牌，返回是否放行。
func (rl *RateLimiter) Allow(ip string) bool {
	key := normalizeIPv6(ip)
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.m[key]
	if !ok {
		rl.m[key] = &bucket{tokens: rl.burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func (rl *RateLimiter) cleanupLoop() {
	t := time.NewTicker(rl.cleanup)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		rl.mu.Lock()
		for k, b := range rl.m {
			if b.last.Before(cutoff) {
				delete(rl.m, k)
			}
		}
		rl.mu.Unlock()
	}
}
