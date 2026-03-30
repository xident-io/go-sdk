package xident

import (
	"net/http"
	"testing"
	"time"
)

func TestWithBaseURL(t *testing.T) {
	c := NewClient("sk_test_key", WithBaseURL("https://staging.xident.io"))
	if c.baseURL != "https://staging.xident.io" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://staging.xident.io")
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	c := NewClient("sk_test_key", WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Error("httpClient not set to custom client")
	}
}

func TestWithTimeout(t *testing.T) {
	c := NewClient("sk_test_key", WithTimeout(10*time.Second))
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, 10*time.Second)
	}
}

func TestWithMaxRetries(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{"positive", 5, 5},
		{"zero", 0, 0},
		{"negative clamped to zero", -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient("sk_test_key", WithMaxRetries(tt.n))
			if c.maxRetries != tt.want {
				t.Errorf("maxRetries = %d, want %d", c.maxRetries, tt.want)
			}
		})
	}
}

func TestWithUserAgent(t *testing.T) {
	c := NewClient("sk_test_key", WithUserAgent("MyApp/2.0"))
	if c.userAgent != "MyApp/2.0" {
		t.Errorf("userAgent = %q, want %q", c.userAgent, "MyApp/2.0")
	}
}

func TestOptionChaining(t *testing.T) {
	c := NewClient("sk_test_key",
		WithBaseURL("https://staging.xident.io"),
		WithMaxRetries(1),
		WithUserAgent("Chain/1.0"),
	)

	if c.baseURL != "https://staging.xident.io" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
	if c.maxRetries != 1 {
		t.Errorf("maxRetries = %d", c.maxRetries)
	}
	if c.userAgent != "Chain/1.0" {
		t.Errorf("userAgent = %q", c.userAgent)
	}
}
