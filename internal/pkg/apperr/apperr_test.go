package apperr

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewFields(t *testing.T) {
	e := New(413, 41301, "文件过大")
	if e.HTTPStatus != 413 || e.Code != 41301 || e.Message != "文件过大" {
		t.Fatalf("New() fields mismatch: %+v", e)
	}
}

func TestErrorMethod(t *testing.T) {
	e := New(400, 40001, "参数错误")
	if e.Error() != "参数错误" {
		t.Fatalf("Error() = %q, want %q", e.Error(), "参数错误")
	}
}

func TestErrorsAsUnwrap(t *testing.T) {
	inner := New(415, 41501, "文件类型不支持")
	wrapped := fmt.Errorf("service: %w", inner)

	var ae *Error
	if !errors.As(wrapped, &ae) {
		t.Fatal("errors.As failed to find *apperr.Error")
	}
	if ae.Code != 41501 || ae.HTTPStatus != 415 || ae.Message != "文件类型不支持" {
		t.Fatalf("unwrapped error mismatch: %+v", ae)
	}
}

func TestErrorsAsPlainErrorNegative(t *testing.T) {
	var ae *Error
	if errors.As(errors.New("plain"), &ae) {
		t.Fatal("plain error should not match *apperr.Error")
	}
	if ae != nil {
		t.Fatalf("ae should remain nil, got %+v", ae)
	}
}
