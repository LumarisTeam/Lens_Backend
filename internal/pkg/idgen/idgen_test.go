package idgen

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

const crockfordChars = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func containsOnly(s, chars string) bool {
	for _, r := range s {
		if !strings.ContainsRune(chars, r) {
			return false
		}
	}
	return true
}

func TestNewULIDFormat(t *testing.T) {
	u := NewULID()
	if len(u) != 26 {
		t.Fatalf("ULID length = %d, want 26: %q", len(u), u)
	}
	if !containsOnly(u, crockfordChars) {
		t.Fatalf("ULID contains non-Crockford chars: %q", u)
	}
}

func TestFeedbackNoFormat(t *testing.T) {
	no := FeedbackNo()
	if len(no) != 28 {
		t.Fatalf("FeedbackNo length = %d, want 28: %q", len(no), no)
	}
	if !strings.HasPrefix(no, "FB") {
		t.Fatalf("FeedbackNo missing FB prefix: %q", no)
	}
	re := regexp.MustCompile("^FB[0-9A-HJKMNP-TV-Z]{26}$")
	if !re.MatchString(no) {
		t.Fatalf("FeedbackNo format mismatch: %q", no)
	}
}

func TestUniqueness(t *testing.T) {
	const n = 20000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		u := NewULID()
		if _, dup := seen[u]; dup {
			t.Fatalf("duplicate ULID generated: %s", u)
		}
		seen[u] = struct{}{}
	}
}

func TestULIDTimestampEncoding(t *testing.T) {
	before := time.Now().UnixMilli()
	u := NewULID()
	after := time.Now().UnixMilli()

	b, err := encoding.DecodeString(u)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(b) != 16 {
		t.Fatalf("decoded length = %d, want 16", len(b))
	}
	ts := int64(b[0])<<40 | int64(b[1])<<32 | int64(b[2])<<24 | int64(b[3])<<16 | int64(b[4])<<8 | int64(b[5])
	if ts < before || ts > after {
		t.Errorf("ULID timestamp %d not in [%d, %d]", ts, before, after)
	}
}

func TestULIDPrefixMonotonic(t *testing.T) {
	var prev string
	for i := 0; i < 2000; i++ {
		u := NewULID()
		prefix := u[:9] // 前 45 bit 全为时间戳（第 10 个字符混入 2 bit 随机位，不可参与比较）
		if prefix < prev {
			t.Fatalf("timestamp prefix went backwards at %d: %s < %s", i, prefix, prev)
		}
		prev = prefix
	}
}
