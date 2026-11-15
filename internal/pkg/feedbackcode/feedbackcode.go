// Package feedbackcode 实现反馈中心校验码的 HMAC-SHA256 算法。
package feedbackcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
)

// Sign 计算 Base64URL(HMAC_SHA256(secret, centerID|timestamp|sn|nonce))。
func Sign(secret, centerID string, timestamp int64, sn, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(centerID))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(sn))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Equal 使用常数时间比较校验码。
func Equal(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}
