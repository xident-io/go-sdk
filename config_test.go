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

// TestWithTimeoutNeverMutatesTheCallersClient pins the fix for a real defect:
// WithTimeout used to assign straight to c.httpClient.Timeout, so
// `WithHTTPClient(mine), WithTimeout(d)` reached into a client the CALLER owned
// and rewrote a field on it. Anyone sharing that *http.Client with the rest of
// their program had its timeout silently changed by constructing an SDK client.
//
// Both orders are asserted because the bug only fired in one of them, which is
// also why the old doc comment ("this option will be ignored") read as true to
// whoever wrote it.
func TestWithTimeoutNeverMutatesTheCallersClient(t *testing.T) {
	const callerTimeout = 42 * time.Second

	for _, tc := range []struct {
		name string
		opts func(hc *http.Client) []Option
	}{
		{
			name: "WithHTTPClient then WithTimeout",
			opts: func(hc *http.Client) []Option {
				return []Option{WithHTTPClient(hc), WithTimeout(3 * time.Second)}
			},
		},
		{
			name: "WithTimeout then WithHTTPClient",
			opts: func(hc *http.Client) []Option {
				return []Option{WithTimeout(3 * time.Second), WithHTTPClient(hc)}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mine := &http.Client{Timeout: callerTimeout}
			_ = NewClient("sk_test_abcdefghijklmnopqrstuvwxyz", tc.opts(mine)...)

			if mine.Timeout != callerTimeout {
				t.Fatalf("the SDK modified a client it does not own: caller's Timeout is %v, want %v",
					mine.Timeout, callerTimeout)
			}
		})
	}
}

// TestWithTimeoutAppliesToWhicheverClientIsInEffect documents the ordering the
// corrected doc comment now describes: options apply in call order, so the
// timeout lands on whatever client is set at the moment it runs.
func TestWithTimeoutAppliesToWhicheverClientIsInEffect(t *testing.T) {
	mine := &http.Client{Timeout: 42 * time.Second}

	// WithHTTPClient first: the timeout applies to (a copy of) the custom client.
	c := NewClient("sk_test_abcdefghijklmnopqrstuvwxyz",
		WithHTTPClient(mine), WithTimeout(3*time.Second))
	if got := c.httpClient.Timeout; got != 3*time.Second {
		t.Fatalf("timeout did not apply to the custom client: got %v, want 3s", got)
	}
	if c.httpClient == mine {
		t.Fatal("the SDK is using the caller's client directly; it must use a copy")
	}

	// WithHTTPClient last: it replaces the client the timeout was applied to,
	// so the caller's own timeout wins. This is the case the old doc comment
	// described, and it remains true.
	c = NewClient("sk_test_abcdefghijklmnopqrstuvwxyz",
		WithTimeout(3*time.Second), WithHTTPClient(mine))
	if got := c.httpClient.Timeout; got != 42*time.Second {
		t.Fatalf("expected the custom client's own timeout to win: got %v, want 42s", got)
	}
}
