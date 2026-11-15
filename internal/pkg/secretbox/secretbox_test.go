package secretbox

import "testing"

func TestCipherRoundTrip(t *testing.T) {
	c, err := New("test-feedback-secret-key")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encoded, err := c.Encrypt("sk_plain_secret")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if encoded == "sk_plain_secret" || len(encoded) < 10 {
		t.Fatalf("Encrypt() returned invalid ciphertext %q", encoded)
	}
	got, err := c.Decrypt(encoded)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got != "sk_plain_secret" {
		t.Fatalf("Decrypt() = %q, want sk_plain_secret", got)
	}
}

func TestCipherRejectsTamperedPayload(t *testing.T) {
	c, err := New("test-feedback-secret-key")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	encoded, err := c.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	encoded = encoded[:len(encoded)-1] + "A"
	if _, err := c.Decrypt(encoded); err == nil {
		t.Fatal("Decrypt() accepted tampered payload")
	}
}
