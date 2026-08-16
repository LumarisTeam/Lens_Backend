package service

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 是基于 x/time/rate 的进程内令牌桶限流器，
// 以 "IP|X-Client-ID" 为维度维护独立的令牌桶。
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	rps      rate.Limit
	burst    int
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

const (
	limiterCleanupInterval = 5 * time.Minute
	limiterIdleTimeout     = 10 * time.Minute
)

// NewRateLimiter 构造限流器并启动后台清理 Goroutine（随 ctx 退出，防 OOM）。
func NewRateLimiter(ctx context.Context, rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*limiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	go rl.cleanupLoop(ctx)
	return rl
}

// Allow 判断 key 是否被放行。
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	e, ok := rl.limiters[key]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.limiters[key] = e
	}
	e.lastSeen = time.Now()
	rl.mu.Unlock()
	return e.limiter.Allow()
}

func (rl *RateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(limiterCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.cleanup()
		}
	}
}

// cleanup 删除超过空闲时长的 Limiter，防止 Map 无限增长。
func (rl *RateLimiter) cleanup() {
	rl.cleanupBefore(time.Now().Add(-limiterIdleTimeout))
}

// cleanupBefore 删除 lastSeen 早于 cutoff 的 Limiter（抽出来便于单测）。
func (rl *RateLimiter) cleanupBefore(cutoff time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for k, e := range rl.limiters {
		if e.lastSeen.Before(cutoff) {
			delete(rl.limiters, k)
		}
	}
}
