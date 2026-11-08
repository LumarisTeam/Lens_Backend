package cos

import (
	"fmt"
	"net/http"
	"testing"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

func TestIsNotFoundError(t *testing.T) {
	notFound := &cossdk.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusNotFound},
	}
	serverError := &cossdk.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusServiceUnavailable},
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "not found", err: notFound, want: true},
		{name: "wrapped not found", err: fmt.Errorf("head: %w", notFound), want: true},
		{name: "server error", err: serverError, want: false},
		{name: "network error", err: fmt.Errorf("dial failed"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNotFoundError(tc.err); got != tc.want {
				t.Fatalf("IsNotFoundError() = %v, want %v", got, tc.want)
			}
		})
	}
}
