package xident

import (
	"net/http"
	"time"
)

const (
	// DefaultBaseURL is the default Xident API base URL.
	DefaultBaseURL = "https://api.xident.io"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxRetries is the default number of retries on 5xx errors.
	DefaultMaxRetries = 3

	// SDKVersion is the current SDK version string.
	//
	// 2.0.0 was a breaking change: SessionResult was migrated to the frozen
	// v1 tenant result contract (see README.md "v2 breaking changes" at the
	// top). 2.1.0 is additive-only: SessionResult gained IPCountry, the v1
	// contract's first additive field since the freeze. Do not tag either
	// release until all four SDKs (Go, Node, PHP, Python) are ready -- the
	// Go module proxy pins a tag immutably, so a tag pushed early can never
	// be re-cut.
	SDKVersion = "3.0.0"

	// apiVersion is the API version path prefix.
	apiVersion = "verify/v1"

	// defaultUserAgent is the User-Agent header value.
	defaultUserAgent = "Xident-Go/" + SDKVersion
)

// Option configures a Client. Pass options to NewClient to customize behavior.
type Option func(*Client)

// WithBaseURL sets the API base URL. Useful for testing or on-premise deployments.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom *http.Client for full control over transport,
// TLS configuration, proxies, etc.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithTimeout sets the HTTP request timeout.
//
// It applies to whichever *http.Client is in effect when it runs, so ORDER
// MATTERS when combined with WithHTTPClient: options are applied in the order
// they are passed to NewClient.
//
//	// timeout applies to the custom client
//	xident.NewClient(key, xident.WithHTTPClient(hc), xident.WithTimeout(3*time.Second))
//
//	// timeout applies to the default client, then is discarded with it
//	xident.NewClient(key, xident.WithTimeout(3*time.Second), xident.WithHTTPClient(hc))
//
// Your *http.Client is never modified. The doc comment here previously claimed
// this option was "ignored" when WithHTTPClient was used, which was wrong in
// one of the two orders — and worse, the implementation assigned straight to
// c.httpClient.Timeout, so `WithHTTPClient(mine), WithTimeout(d)` reached into
// a client the CALLER owned and rewrote a field on it. A client shared with
// the rest of the caller's program would have had its timeout silently changed
// by constructing an SDK client. It now copies first.
//
// The copy is shallow, which is deliberate: Transport, Jar and CheckRedirect
// are shared with the original, so connection pooling and TLS configuration
// are preserved and only Timeout diverges.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		hc := *c.httpClient
		hc.Timeout = d
		c.httpClient = &hc
	}
}

// WithMaxRetries sets the maximum number of retries on 5xx server errors.
// Set to 0 to disable retries.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}
