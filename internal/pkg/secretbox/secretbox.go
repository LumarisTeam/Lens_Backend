// Package secretbox 使用 AES-256-GCM 加密 feedback center secret。
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const versionPrefix = "v1:"

// Cipher 提供 secret 的认证加密与解密。
type Cipher struct {
	aead cipher.AEAD
}

// New 从配置密钥派生 AES-256 密钥。
func New(key string) (*Cipher, error) {
	if len(key) < 16 {
		return nil, errors.New("FEEDBACK_SECRET_KEY must be at least 16 characters")
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt 加密明文，返回可存库的字符串。
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return versionPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decrypt 解密 Encrypt 生成的字符串。
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if len(encoded) <= len(versionPrefix) || encoded[:len(versionPrefix)] != versionPrefix {
		return "", errors.New("unsupported secret cipher format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded[len(versionPrefix):])
	if err != nil {
		return "", err
	}
	ns := c.aead.NonceSize()
	if len(payload) < ns {
		return "", errors.New("invalid secret cipher payload")
	}
	plain, err := c.aead.Open(nil, payload[:ns], payload[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}
