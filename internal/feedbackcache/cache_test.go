package feedbackcache

import (
	"strings"
	"testing"
	"time"
)

func TestRateLimitKeyIncludesClientDimension(t *testing.T) {
	minute := time.Date(2026, 10, 11, 12, 30, 0, 0, time.UTC)
	a := rateLimitKey("fc_demo", "lumaris-prod", "client-a", minute)
	b := rateLimitKey("fc_demo", "lumaris-prod", "client-b", minute)
	if a == b {
		t.Fatalf("different clients share rate limit key: %s", a)
	}
	if !strings.HasPrefix(a, "fc:rate:fc_demo:lumaris-prod:") {
		t.Fatalf("unexpected rate limit key prefix: %s", a)
	}
	if strings.Contains(a, "client-a") {
		t.Fatalf("client id should not appear in plaintext: %s", a)
	}

	if again := rateLimitKey("fc_demo", "lumaris-prod", "client-a", minute); again != a {
		t.Fatalf("rate limit key is not deterministic: %s != %s", again, a)
	}
}
