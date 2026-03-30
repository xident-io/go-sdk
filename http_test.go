package xident

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRequest_GET(t *testing.T) {
	c := NewClient("sk_test_key")
	req, err := c.newRequest(http.MethodGet, "status/abc123", nil)
	if err != nil {
		t.Fatalf("newRequest error: %v", err)
	}

	// Check URL construction.
	wantURL := DefaultBaseURL + "/" + apiVersion + "/status/abc123"
	if req.URL.String() != wantURL {
		t.Errorf("URL = %q, want %q", req.URL.String(), wantURL)
	}

	// Check headers.
	if got := req.Header.Get("X-API-Key"); got != "sk_test_key" {
		t.Errorf("X-API-Key = %q, want %q", got, "sk_test_key")
	}
	if got := req.Header.Get("User-Agent"); got != defaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got, defaultUserAgent)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want %q", got, "application/json")
	}
	if got := req.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type should be empty for GET, got %q", got)
	}
}

func TestNewRequest_POST(t *testing.T) {
	c := NewClient("sk_test_key")

	body := map[string]string{"callback_url": "https://example.com/cb"}
	req, err := c.newRequest(http.MethodPost, "init", body)
	if err != nil {
		t.Fatalf("newRequest error: %v", err)
	}

	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	// Read and verify body.
	data, _ := io.ReadAll(req.Body)
	var decoded map[string]string
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}
	if decoded["callback_url"] != "https://example.com/cb" {
		t.Errorf("body callback_url = %q, want %q", decoded["callback_url"], "https://example.com/cb")
	}
}

func TestNewRequest_CustomBaseURL(t *testing.T) {
	c := NewClient("sk_test_key", WithBaseURL("https://staging.xident.io"))
	req, err := c.newRequest(http.MethodGet, "status/abc", nil)
	if err != nil {
		t.Fatalf("newRequest error: %v", err)
	}

	want := "https://staging.xident.io/" + apiVersion + "/status/abc"
	if req.URL.String() != want {
		t.Errorf("URL = %q, want %q", req.URL.String(), want)
	}
}

func TestDo_SuccessResponse(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"success": true,
			"data": {"name": "hello"},
			"meta": {"request_id": "req_abc"}
		}`)
	})

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	var result struct {
		Name string `json:"name"`
	}
	resp, err := client.do(context.Background(), req, &result)
	if err != nil {
		t.Fatalf("do() error: %v", err)
	}
	if result.Name != "hello" {
		t.Errorf("result.Name = %q, want %q", result.Name, "hello")
	}
	if resp.RequestID != "req_abc" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_abc")
	}
}

func TestDo_APIError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "INVALID_REQUEST", "message": "Missing callback_url"},
			"meta": {"request_id": "req_err"}
		}`)
	})

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	_, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should contain error details.
	errStr := err.Error()
	if !strings.Contains(errStr, "INVALID_REQUEST") {
		t.Errorf("error should contain code, got: %s", errStr)
	}
	if !strings.Contains(errStr, "Missing callback_url") {
		t.Errorf("error should contain message, got: %s", errStr)
	}
}

func TestDo_ContextCanceled(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	_, err := client.do(ctx, req, nil)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestDo_Retry5xx(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"success":false,"error":{"code":"SERVER_ERROR","message":"Internal error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"ok":true}}`)
	}))
	defer server.Close()

	client := NewClient("sk_test_key",
		WithBaseURL(server.URL),
		WithMaxRetries(3),
		WithTimeout(10*time.Second),
	)

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	var result struct {
		OK bool `json:"ok"`
	}
	_, err := client.do(context.Background(), req, &result)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if !result.OK {
		t.Error("result.OK should be true")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestDo_NoRetryOn4xx(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"error":{"code":"BAD_REQUEST","message":"Invalid"}}`)
	}))
	defer server.Close()

	client := NewClient("sk_test_key",
		WithBaseURL(server.URL),
		WithMaxRetries(3),
	)

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	_, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestDo_MaxRetriesExhausted(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"error":{"code":"SERVER_ERROR","message":"Down"}}`)
	}))
	defer server.Close()

	client := NewClient("sk_test_key",
		WithBaseURL(server.URL),
		WithMaxRetries(2),
	)

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	_, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	// 1 initial + 2 retries = 3 total attempts.
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestDo_HeadersSent(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "sk_test_123" {
			t.Errorf("X-API-Key = %q, want %q", got, "sk_test_123")
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want %q", got, "application/json")
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "Xident-Go/") {
			t.Errorf("User-Agent = %q, want prefix %q", got, "Xident-Go/")
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	})

	req, _ := client.newRequest(http.MethodGet, "test", nil)
	_, err := client.do(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("do() error: %v", err)
	}
}

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		minMS   int64
		maxMS   int64
	}{
		{1, 1000, 1250},  // 1s + up to 25% jitter
		{2, 2000, 2500},  // 2s + up to 25% jitter
		{3, 4000, 5000},  // 4s + up to 25% jitter
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := retryDelay(tt.attempt)
			ms := delay.Milliseconds()
			if ms < tt.minMS || ms > tt.maxMS {
				t.Errorf("delay = %dms, want between %d and %d", ms, tt.minMS, tt.maxMS)
			}
		})
	}
}
