package xident

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// setup creates a test server, a client pointed at it, and a mux for routing.
// Call teardown() when done.
func setup() (client *Client, mux *http.ServeMux, teardown func()) {
	mux = http.NewServeMux()
	server := httptest.NewServer(mux)

	client = NewClient("sk_test_123",
		WithBaseURL(server.URL),
		WithMaxRetries(0), // No retries in tests by default.
	)

	return client, mux, server.Close
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("sk_test_key")

	if c.apiKey != "sk_test_key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "sk_test_key")
	}
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.maxRetries != DefaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", c.maxRetries, DefaultMaxRetries)
	}
	if c.userAgent != defaultUserAgent {
		t.Errorf("userAgent = %q, want %q", c.userAgent, defaultUserAgent)
	}
	if c.httpClient.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, DefaultTimeout)
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	customHTTP := &http.Client{Timeout: 60 * time.Second}

	c := NewClient("sk_test_key",
		WithBaseURL("https://custom.api.io"),
		WithHTTPClient(customHTTP),
		WithMaxRetries(5),
		WithUserAgent("Custom/1.0"),
	)

	if c.baseURL != "https://custom.api.io" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://custom.api.io")
	}
	if c.httpClient != customHTTP {
		t.Error("httpClient not set to custom client")
	}
	if c.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", c.maxRetries)
	}
	if c.userAgent != "Custom/1.0" {
		t.Errorf("userAgent = %q, want %q", c.userAgent, "Custom/1.0")
	}
}

func TestNewClient_ServicesInitialized(t *testing.T) {
	c := NewClient("sk_test_key")

	if c.Verification == nil {
		t.Error("Verification service is nil")
	}
	if c.Webhooks == nil {
		t.Error("Webhooks service is nil")
	}
}

func TestNewClient_ServicesSameClient(t *testing.T) {
	c := NewClient("sk_test_key")

	// Both services should point back to the same client.
	if c.Verification.client != c {
		t.Error("Verification service does not point to client")
	}
	if c.Webhooks.client != c {
		t.Error("Webhooks service does not point to client")
	}
}

func TestVersion(t *testing.T) {
	v := Version()
	if v != SDKVersion {
		t.Errorf("Version() = %q, want %q", v, SDKVersion)
	}
}
