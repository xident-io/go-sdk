package xident

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNewClient_RejectsPublicKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for public key, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "pk_") {
			t.Errorf("unexpected panic message: %v", r)
		}
	}()
	NewClient("pk_live_abc123")
}

func TestNewClient_RejectsInvalidFormat(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for invalid key format, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "invalid API key format") {
			t.Errorf("unexpected panic message: %v", r)
		}
	}()
	NewClient("some_random_key")
}

func TestNewClient_AcceptsLiveKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic for sk_live_ key: %v", r)
		}
	}()
	c := NewClient("sk_live_abc123")
	if c.apiKey != "sk_live_abc123" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "sk_live_abc123")
	}
}

// TestNewClient_AcceptsAgentKeys pins that an agent key builds a client.
// The API accepts ak_live_ and ak_test_ on POST /verify/v1/init, and the
// constructor used to panic on them, so an agent integration could not make
// a single request.
//
// Mutation caught: dropping either ak_ prefix from serverKeyPrefixes.
func TestNewClient_AcceptsAgentKeys(t *testing.T) {
	for _, key := range []string{"ak_live_abc123", "ak_test_abc123"} {
		t.Run(key, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("unexpected panic for %s: %v", key, r)
				}
			}()
			c := NewClient(key)
			if c.apiKey != key {
				t.Errorf("apiKey = %q, want %q", c.apiKey, key)
			}
		})
	}
}

// TestNewClient_RejectsOtherPrefixes pins that only the four server key
// kinds are accepted: a near miss such as "ak_" without a mode, "sk_" alone
// or a webhook secret still panics.
func TestNewClient_RejectsOtherPrefixes(t *testing.T) {
	for _, key := range []string{"ak_abc", "sk_abc", "ak_prod_abc", "whsec_abc", "xat_abc", ""} {
		t.Run(key, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for %q, got none", key)
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, "ak_live_") {
					t.Errorf("panic message = %v, want it to list the accepted prefixes", r)
				}
			}()
			NewClient(key)
		})
	}
}

// TestVerification_Init_WithAgentKey checks that a client built from an
// agent key sends that key and reaches the API.
func TestVerification_Init_WithAgentKey(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	var gotKey string
	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"token":"xit_ak","verify_url":"https://verify.xident.io?t=xit_ak"}}`)
	})

	client := NewClient("ak_test_agent", WithBaseURL(server.URL), WithMaxRetries(0))
	result, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
		UserID:      "usr_1",
		MinAge:      18,
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if result.Token != "xit_ak" {
		t.Errorf("Token = %q, want xit_ak", result.Token)
	}
	if gotKey != "ak_test_agent" {
		t.Errorf("X-API-Key = %q, want the agent key", gotKey)
	}
}

func TestNewClient_AcceptsTestKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic for sk_test_ key: %v", r)
		}
	}()
	c := NewClient("sk_test_abc123")
	if c.apiKey != "sk_test_abc123" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "sk_test_abc123")
	}
}
