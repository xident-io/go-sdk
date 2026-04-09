// Package xident provides a Go client for the Xident age verification API.
//
// Create a client with your API key, then use the service objects to interact
// with the API:
//
//	client := xident.NewClient("sk_live_xxx")
//
//	// Start a verification session
//	result, _, err := client.Verification.Init(ctx, &xident.InitParams{
//	    CallbackURL: "https://example.com/callback",
//	    MinAge:      18,
//	})
//
//	// Retrieve the result after the user completes verification
//	session, _, err := client.Verification.GetResult(ctx, result.Token)
//	if session.IsVerified() {
//	    fmt.Println("User is verified!")
//	}
//
// The SDK uses functional options for configuration:
//
//	client := xident.NewClient("sk_live_xxx",
//	    xident.WithTimeout(10 * time.Second),
//	    xident.WithMaxRetries(5),
//	)
//
// All methods accept a context.Context for cancellation and deadlines.
// All methods return (result, *Response, error) triples following the
// go-github convention.
package xident

import (
	"net/http"
	"strings"
	"time"
)

// Client manages communication with the Xident API.
//
// Create one with NewClient, then access services through the exported fields.
// A single Client should be reused across goroutines -- it is safe for
// concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	userAgent  string

	// common service shared by all resource services. This is the go-github
	// "common service" pattern: each resource service is a type alias of this
	// struct, so they all share the same client pointer without separate fields.
	common service

	// Verification provides methods to init sessions and retrieve results.
	Verification *VerificationService

	// Webhooks provides methods to verify webhook signatures and parse events.
	Webhooks *WebhookService
}

// service is the base struct shared by all resource services. It holds a
// pointer back to the Client, giving each service access to the HTTP layer.
type service struct {
	client *Client
}

// NewClient creates a new Xident API client.
//
// apiKey is your Xident secret API key (sk_live_xxx or sk_test_xxx).
// Pass Option values to customize the client behavior.
//
// Example:
//
//	client := xident.NewClient("sk_live_xxx",
//	    xident.WithBaseURL("https://staging-api.xident.io"),
//	    xident.WithTimeout(15 * time.Second),
//	)
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		maxRetries: DefaultMaxRetries,
		userAgent:  defaultUserAgent,
	}

	if strings.HasPrefix(apiKey, "pk_") {
		panic("xident: public keys (pk_*) cannot be used with the server SDK. Use your secret key (sk_live_* or sk_test_*)")
	}
	if !strings.HasPrefix(apiKey, "sk_live_") && !strings.HasPrefix(apiKey, "sk_test_") {
		panic("xident: invalid API key format. Must start with \"sk_live_\" or \"sk_test_\"")
	}

	for _, opt := range opts {
		opt(c)
	}

	c.common.client = c
	c.Verification = (*VerificationService)(&c.common)
	c.Webhooks = (*WebhookService)(&c.common)

	return c
}

// Version returns the SDK version string.
func Version() string {
	return SDKVersion
}

// Response wraps an *http.Response and adds the API request ID for
// correlation in support tickets and debugging.
type Response struct {
	*http.Response

	// RequestID is the unique identifier for this API request, returned in
	// the response body's meta.request_id field. Include it in support tickets.
	RequestID string
}

// newResponse creates a Response from an http.Response.
func newResponse(r *http.Response) *Response {
	return &Response{Response: r}
}

// Timestamp represents a time value from the API. The zero value means the
// field was not present in the response.
type Timestamp struct {
	time.Time
}
