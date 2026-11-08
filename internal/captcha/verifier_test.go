package captcha

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("secret") != "secret" || r.Form.Get("response") != "token" {
			t.Fatalf("unexpected form: %v", r.Form)
		}
		if r.Form.Get("remoteip") != "203.0.113.9" {
			t.Fatalf("remoteip = %q", r.Form.Get("remoteip"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	v := New(Config{
		Provider:  "turnstile",
		SiteKey:   "site",
		Secret:    "secret",
		VerifyURL: server.URL,
		Timeout:   time.Second,
	})
	if err := v.Verify(context.Background(), "token", "203.0.113.9"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsInvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer server.Close()

	v := New(Config{
		Provider:  "hcaptcha",
		SiteKey:   "site",
		Secret:    "secret",
		VerifyURL: server.URL,
		Timeout:   time.Second,
	})
	if err := v.Verify(context.Background(), "bad", ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
	}
}

func TestOffIsDisabled(t *testing.T) {
	v := New(Config{Provider: "off"})
	if v.Enabled() {
		t.Fatal("off provider should be disabled")
	}
}
