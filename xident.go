// Package xident provides a Go client for the Xident age verification API.
//
// Create a client with your API key, then use the service objects to interact
// with the API:
//
//	client := xident.NewClient("sk_live_xxx") // or an agent key, ak_live_xxx
//
//	// Start a verification session for the signed-in user. The user id and
//	// the age come from your server, never from the request.
//	result, _, err := client.Verification.Init(ctx, &xident.InitParams{
//	    CallbackURL: "https://example.com/callback",
//	    UserID:      userID,         // required: your own id for the person
//	    MinAge:      requiredMinAge, // 12 to 25, rounded up to the next band
//	})
//	// Redirect the browser to result.VerifyURL.
//
//	// Retrieve the result after the user completes verification. The
//	// widget appends the xtk_ result token to your callback URL as ?token=;
//	// result.Token is the xit_ init token and is not used here.
//	session, _, err := client.Verification.GetResult(ctx, resultToken)
//	if session.ProvesAge(requiredMinAge) && session.ExternalUserID == userID {
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
	// apiVersion is the dated API version sent as X-API-Version. Defaults to
	// PinnedAPIVersion — the version this SDK release was built against — so the
	// result types and the payload always agree.
	apiVersion string

	// common service shared by all resource services. This is the go-github
	// "common service" pattern: each resource service is a type alias of this
	// struct, so they all share the same client pointer without separate fields.
	common service

	// Verification provides methods to init sessions and retrieve results.
	Verification *VerificationService

	// Webhooks provides methods to verify webhook signatures and parse events.
	Webhooks *WebhookService

	// Face2FA provides methods to enroll and verify faces as a second factor.
	Face2FA *Face2FAService

	// Blacklist provides methods to manage the tenant's face blacklist.
	Blacklist *BlacklistService
}

// service is the base struct shared by all resource services. It holds a
// pointer back to the Client, giving each service access to the HTTP layer.
type service struct {
	client *Client
}

// NewClient creates a new Xident API client.
//
// apiKey is a Xident server key: a secret key (sk_live_xxx or sk_test_xxx)
// or an agent key (ak_live_xxx or ak_test_xxx). The API accepts both on
// POST /verify/v1/init; what else an agent key may call depends on the
// scopes it was given. A public key (pk_) belongs in the browser and panics
// here.
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
		apiVersion: PinnedAPIVersion,
	}

	if strings.HasPrefix(apiKey, "pk_") {
		panic("xident: public keys (pk_*) cannot be used with the server SDK. Use your secret key (sk_live_* or sk_test_*) or an agent key (ak_live_* or ak_test_*)")
	}
	if !hasServerKeyPrefix(apiKey) {
		panic("xident: invalid API key format. Must start with \"sk_live_\", \"sk_test_\", \"ak_live_\" or \"ak_test_\"")
	}

	for _, opt := range opts {
		opt(c)
	}

	c.common.client = c
	c.Verification = (*VerificationService)(&c.common)
	c.Webhooks = (*WebhookService)(&c.common)
	c.Face2FA = (*Face2FAService)(&c.common)
	c.Blacklist = (*BlacklistService)(&c.common)

	return c
}

// serverKeyPrefixes are the key kinds the API accepts from a server: secret
// keys and agent keys, live and test.
var serverKeyPrefixes = []string{"sk_live_", "sk_test_", "ak_live_", "ak_test_"}

func hasServerKeyPrefix(apiKey string) bool {
	for _, prefix := range serverKeyPrefixes {
		if strings.HasPrefix(apiKey, prefix) {
			return true
		}
	}
	return false
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

	// Pagination holds list pagination metadata from the response body's
	// meta.pagination field. Nil for non-list endpoints.
	Pagination *Pagination
}

// Pagination is the pagination metadata returned by list endpoints in the
// API envelope's meta.pagination field.
type Pagination struct {
	// Page is the 1-based page number of this response.
	Page int `json:"page"`

	// PerPage is the number of items per page.
	PerPage int `json:"per_page"`

	// Total is the total number of items across all pages.
	Total int64 `json:"total"`

	// TotalPages is the total number of pages.
	TotalPages int `json:"total_pages"`
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
