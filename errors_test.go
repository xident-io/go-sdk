package xident

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorResponse_Error(t *testing.T) {
	resp := &http.Response{StatusCode: 400}
	e := &ErrorResponse{
		Response:  resp,
		Code:      "INVALID_REQUEST",
		Message:   "Missing field",
		RequestID: "req_123",
	}

	got := e.Error()
	if !strings.Contains(got, "400") {
		t.Errorf("should contain status code, got: %s", got)
	}
	if !strings.Contains(got, "INVALID_REQUEST") {
		t.Errorf("should contain error code, got: %s", got)
	}
	if !strings.Contains(got, "Missing field") {
		t.Errorf("should contain message, got: %s", got)
	}
	if !strings.Contains(got, "req_123") {
		t.Errorf("should contain request ID, got: %s", got)
	}
}

func TestErrorResponse_ErrorWithoutRequestID(t *testing.T) {
	resp := &http.Response{StatusCode: 400}
	e := &ErrorResponse{
		Response: resp,
		Code:     "INVALID_REQUEST",
		Message:  "Missing field",
	}

	got := e.Error()
	if strings.Contains(got, "request_id") {
		t.Errorf("should not contain request_id when empty, got: %s", got)
	}
}

func TestNewErrorResponse_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantType   string
	}{
		{"401 -> AuthenticationError", 401, "*xident.AuthenticationError"},
		{"403 -> AuthenticationError", 403, "*xident.AuthenticationError"},
		{"400 -> ValidationError", 400, "*xident.ValidationError"},
		{"422 -> ValidationError", 422, "*xident.ValidationError"},
		{"404 -> NotFoundError", 404, "*xident.NotFoundError"},
		{"429 -> RateLimitError", 429, "*xident.RateLimitError"},
		{"500 -> ServerError", 500, "*xident.ServerError"},
		{"502 -> ServerError", 502, "*xident.ServerError"},
		{"503 -> ServerError", 503, "*xident.ServerError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{},
			}
			err := newErrorResponse(resp, "TEST", "test error", "req_test")

			// Check type using errors.As.
			switch tt.wantType {
			case "*xident.AuthenticationError":
				var target *AuthenticationError
				if !errors.As(err, &target) {
					t.Errorf("expected AuthenticationError, got %T", err)
				}
			case "*xident.ValidationError":
				var target *ValidationError
				if !errors.As(err, &target) {
					t.Errorf("expected ValidationError, got %T", err)
				}
			case "*xident.NotFoundError":
				var target *NotFoundError
				if !errors.As(err, &target) {
					t.Errorf("expected NotFoundError, got %T", err)
				}
			case "*xident.RateLimitError":
				var target *RateLimitError
				if !errors.As(err, &target) {
					t.Errorf("expected RateLimitError, got %T", err)
				}
			case "*xident.ServerError":
				var target *ServerError
				if !errors.As(err, &target) {
					t.Errorf("expected ServerError, got %T", err)
				}
			}
		})
	}
}

func TestRateLimitError_RetryAfter(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Retry-After", "60")
	resp := recorder.Result()
	resp.StatusCode = 429

	err := newErrorResponse(resp, "RATE_LIMITED", "Too many requests", "req_rl")

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rlErr.RetryAfter != 60 {
		t.Errorf("RetryAfter = %d, want 60", rlErr.RetryAfter)
	}
}

func TestRateLimitError_RetryAfterMissing(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{},
	}

	err := newErrorResponse(resp, "RATE_LIMITED", "Too many requests", "req_rl")

	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rlErr.RetryAfter != 0 {
		t.Errorf("RetryAfter = %d, want 0 when header missing", rlErr.RetryAfter)
	}
}

func TestErrorsAs_AllTypes(t *testing.T) {
	// Verify that every error type satisfies the error interface and works
	// with errors.As targeting the base ErrorResponse.
	resp := &http.Response{StatusCode: 400, Header: http.Header{}}

	errs := []error{
		&AuthenticationError{ErrorResponse{Response: resp, Code: "A", Message: "auth"}},
		&ValidationError{ErrorResponse{Response: resp, Code: "V", Message: "val"}},
		&NotFoundError{ErrorResponse{Response: resp, Code: "N", Message: "not found"}},
		&RateLimitError{ErrorResponse: ErrorResponse{Response: resp, Code: "R", Message: "rate"}},
		&ServerError{ErrorResponse{Response: resp, Code: "S", Message: "server"}},
	}

	for _, err := range errs {
		if err.Error() == "" {
			t.Errorf("Error() returned empty for %T", err)
		}
	}
}
