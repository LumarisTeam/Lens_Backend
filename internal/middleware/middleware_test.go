package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"lens-backend/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func doRequest(t *testing.T, handler gin.HandlerFunc, headerKey, headerVal string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.Use(handler)
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if headerKey != "" {
		req.Header.Set(headerKey, headerVal)
	}
	r.ServeHTTP(w, req)

	var m map[string]interface{}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			m = nil // 非 JSON 响应（如成功时的 "ok"）
		}
	}
	return w, m
}

func TestClientID_Missing(t *testing.T) {
	w, m := doRequest(t, ClientID(), "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if m["code"].(float64) != 40002 {
		t.Fatalf("code = %v, want 40002", m["code"])
	}
}

func TestClientID_Valid(t *testing.T) {
	w, _ := doRequest(t, ClientID(), "X-Client-ID", "test-client-001")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestClientID_TooLong(t *testing.T) {
	w, m := doRequest(t, ClientID(), "X-Client-ID", strings.Repeat("x", MaxClientIDLength+1))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if m["code"].(float64) != 40001 {
		t.Fatalf("code = %v, want 40001", m["code"])
	}
}

func TestClientID_MaxLengthAccepted(t *testing.T) {
	w, _ := doRequest(t, ClientID(), "X-Client-ID", strings.Repeat("x", MaxClientIDLength))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (max length should pass)", w.Code)
	}
}

func TestAdminAuth_MissingHeader(t *testing.T) {
	w, m := doRequest(t, AdminAuth("secret-token"), "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if m["code"].(float64) != 40101 {
		t.Fatalf("code = %v, want 40101", m["code"])
	}
}

func TestAdminAuth_WrongToken(t *testing.T) {
	w, _ := doRequest(t, AdminAuth("secret-token"), "Authorization", "Bearer wrong-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAdminAuth_NonBearerScheme(t *testing.T) {
	w, _ := doRequest(t, AdminAuth("secret-token"), "Authorization", "Basic c2VjcmV0LXRva2Vu")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAdminAuth_CorrectToken(t *testing.T) {
	w, _ := doRequest(t, AdminAuth("secret-token"), "Authorization", "Bearer secret-token")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestAdminAuth_WhitespaceTolerated(t *testing.T) {
	w, _ := doRequest(t, AdminAuth("secret-token"), "Authorization", "Bearer  secret-token ")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (middleware trims token)", w.Code)
	}
}

func TestCORS_AllowsFeedbackCodeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS([]string{"https://admin.example.com"}))
	r.POST("/api/v1/feedbacks", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/feedbacks", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set(
		"Access-Control-Request-Headers",
		"X-Feedback-Center-Id, X-Timestamp, X-SN, X-Nonce, X-Code, X-Captcha-Token",
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	allowed := w.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Errorf("Access-Control-Allow-Methods = %q, missing PATCH", w.Header().Get("Access-Control-Allow-Methods"))
	}
	for _, header := range []string{
		"X-Feedback-Center-Id", "X-Timestamp", "X-SN", "X-Nonce", "X-Code", "X-Captcha-Token",
	} {
		if !strings.Contains(allowed, header) {
			t.Errorf("Access-Control-Allow-Headers = %q, missing %s", allowed, header)
		}
	}
}

func TestFeedbackCodeAuthOptional_AllowsLegacyRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(FeedbackCodeAuthOptional(&service.FeedbackCenterService{}))
	r.POST("/api/v1/feedbacks", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedbacks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for legacy request", w.Code)
	}
}

func TestFeedbackCodeAuthOptional_RejectsPartialHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(FeedbackCodeAuthOptional(&service.FeedbackCenterService{}))
	r.POST("/api/v1/feedbacks", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedbacks", nil)
	req.Header.Set("X-Code", "partial")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for partial headers", w.Code)
	}
}
