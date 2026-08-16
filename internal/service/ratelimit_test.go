package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

func newTestLimiter(t *testing.T, rps float64, burst int) *RateLimiter {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewRateLimiter(ctx, rps, burst)
}

func TestAllowExhaustion(t *testing.T) {
	rl := newTestLimiter(t, 5, 3)
	for i := 0; i < 3; i++ {
		if !rl.Allow("k1") {
			t.Fatalf("Allow #%d = false, want true (burst=3)", i+1)
		}
	}
	if rl.Allow("k1") {
		t.Fatal("Allow after burst exhausted = true, want false")
	}
}

func TestKeyIsolation(t *testing.T) {
	rl := newTestLimiter(t, 5, 1)
	if !rl.Allow("a") {
		t.Fatal("first allow for a failed")
	}
	if rl.Allow("a") {
		t.Fatal("second allow for a should fail (burst=1)")
	}
	if !rl.Allow("b") {
		t.Fatal("allow for independent key b should succeed")
	}
}

func TestAllowRefill(t *testing.T) {
	rl := newTestLimiter(t, 100, 1) // 1 token / 10ms
	if !rl.Allow("k") {
		t.Fatal("first allow failed")
	}
	if rl.Allow("k") {
		t.Fatal("immediate second allow should fail")
	}
	time.Sleep(60 * time.Millisecond) // 足够补充 ~6 个 token
	if !rl.Allow("k") {
		t.Fatal("allow after refill should succeed")
	}
}

func TestCleanupBefore(t *testing.T) {
	rl := newTestLimiter(t, 100, 100)
	rl.Allow("active")
	rl.Allow("stale")

	// 人为把 stale 的 lastSeen 拨到很久以前
	rl.mu.Lock()
	rl.limiters["stale"].lastSeen = time.Now().Add(-time.Hour)
	rl.mu.Unlock()

	rl.cleanupBefore(time.Now())

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, ok := rl.limiters["active"]; !ok {
		t.Fatal("active entry should be kept")
	}
	if _, ok := rl.limiters["stale"]; ok {
		t.Fatal("stale entry should be removed")
	}
}

func TestConcurrentDistinctKeys(t *testing.T) {
	rl := newTestLimiter(t, 1000, 100)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			key := "goroutine-" + string(rune('a'+g))
			for i := 0; i < 200; i++ {
				rl.Allow(key)
			}
		}(g)
	}
	wg.Wait()
}
