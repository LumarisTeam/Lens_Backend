package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/pkg/apperr"
)

func newCtx(w *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(w)
	return c
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	return m
}

func TestOK_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	OK(newCtx(w), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	m := decode(t, w)
	if m["code"].(float64) != 0 || m["message"] != "ok" {
		t.Fatalf("unexpected body: %v", m)
	}
	if _, ok := m["data"]; !ok {
		t.Fatalf("data field missing: %v", m)
	}
}

func TestOK_Data(t *testing.T) {
	w := httptest.NewRecorder()
	OK(newCtx(w), map[string]string{"a": "b"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	m := decode(t, w)
	data, ok := m["data"].(map[string]interface{})
	if !ok || data["a"] != "b" {
		t.Fatalf("data not serialized correctly: %v", m)
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	Error(newCtx(w), http.StatusNotFound, CodeNotFound, "资源不存在")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	m := decode(t, w)
	if m["code"].(float64) != 40401 || m["message"] != "资源不存在" {
		t.Fatalf("unexpected body: %v", m)
	}
	if _, ok := m["data"]; ok {
		t.Fatalf("error response should not carry data: %v", m)
	}
}

func TestWriteError_AppErrPassthrough(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(newCtx(w), apperr.New(http.StatusRequestEntityTooLarge, CodeTooLarge, "文件过大"))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
	m := decode(t, w)
	if m["code"].(float64) != 41301 || m["message"] != "文件过大" {
		t.Fatalf("unexpected body: %v", m)
	}
}

func TestWriteError_PlainErrorInternal(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(newCtx(w), errors.New("db exploded"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	m := decode(t, w)
	if m["code"].(float64) != 50001 || m["message"] != "服务内部错误" {
		t.Fatalf("unexpected body: %v", m)
	}
}
