// Package idgen 提供基于 crypto/rand 的 ULID 生成能力。
// 采用 Crockford Base32（无填充），26 字符，前 48 bit 为毫秒时间戳，
// 后 80 bit 为密码学安全随机数，保证高并发下全局唯一且单调可排序。
package idgen

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var encoding = base32.NewEncoding(crockford).WithPadding(base32.NoPadding)

// NewULID 生成一个 26 字符的 ULID。
func NewULID() string {
	now := time.Now().UnixMilli()
	var ulid [16]byte
	ulid[0] = byte(now >> 40)
	ulid[1] = byte(now >> 32)
	ulid[2] = byte(now >> 24)
	ulid[3] = byte(now >> 16)
	ulid[4] = byte(now >> 8)
	ulid[5] = byte(now)
	if _, err := rand.Read(ulid[6:]); err != nil {
		panic("idgen: crypto/rand failed: " + err.Error())
	}
	return encoding.EncodeToString(ulid[:])
}

// FeedbackNo 生成反馈编号，形如 "FB" + 26 位 ULID（共 28 字符）。
func FeedbackNo() string {
	return "FB" + NewULID()
}
