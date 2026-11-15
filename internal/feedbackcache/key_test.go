package feedbackcache

import (
	"strings"
	"testing"
	"time"
)

func TestDistributedKeysDoNotExposeIdentity(t *testing.T) {
	const identity = "203.0.113.99"
	rateKey := distributedRateKey("ip", identity)
	quotaKey := dailyQuotaKey("code", identity, time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))
	eventKey := riskEventClaimKey("ip_rate_limited", identity)
	for _, key := range []string{rateKey, quotaKey, eventKey} {
		if strings.Contains(key, identity) {
			t.Fatalf("key exposes raw identity: %s", key)
		}
		if !strings.HasPrefix(key, "fc:") {
			t.Fatalf("key prefix mismatch: %s", key)
		}
	}
}
