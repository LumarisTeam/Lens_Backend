package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeExtraUsesAuthoritativeAppName(t *testing.T) {
	got, err := normalizeExtra(
		json.RawMessage(`{"app_name":"spoofed","app_version":"1.0.0"}`),
		"lumaris",
	)
	if err != nil {
		t.Fatalf("normalizeExtra() error = %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("normalizeExtra() returned invalid JSON: %v", err)
	}
	if obj["app_name"] != "lumaris" {
		t.Fatalf("app_name = %v, want lumaris", obj["app_name"])
	}
	if obj["app_version"] != "1.0.0" {
		t.Fatalf("app_version = %v, want 1.0.0", obj["app_version"])
	}
}

func TestNormalizeExtraDefaultsToObject(t *testing.T) {
	got, err := normalizeExtra(nil, "lumalis")
	if err != nil {
		t.Fatalf("normalizeExtra() error = %v", err)
	}
	if string(got) != `{"app_name":"lumalis"}` {
		t.Fatalf("normalizeExtra() = %s, want app_name object", got)
	}
}

func TestNormalizeExtraRejectsNonObject(t *testing.T) {
	if _, err := normalizeExtra(json.RawMessage(`["not","object"]`), "lumaris"); err == nil {
		t.Fatal("normalizeExtra() accepted non-object extra")
	}
}

func TestNormalizeExtraRejectsOversize(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"note": strings.Repeat("x", 8192)})
	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}
	if _, err := normalizeExtra(raw, "lumaris"); err == nil {
		t.Fatal("normalizeExtra() accepted oversize extra")
	}
}
