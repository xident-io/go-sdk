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
	SDKVersion = "1.0.0"

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

// WithTimeout sets the HTTP request timeout. This replaces the Timeout on the
// default http.Client. If you use WithHTTPClient, set the timeout on that client
// instead -- this option will be ignored.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
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
