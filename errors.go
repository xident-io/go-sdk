package xident

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrorResponse represents an error returned by the Xident API.
//
// It implements the error interface and carries the HTTP response, API error
// code, human-readable message, and request ID for debugging.
//
// Use errors.As to match specific error types:
//
//	var authErr *xident.AuthenticationError
//	if errors.As(err, &authErr) {
//	    log.Fatal("Invalid API key")
//	}
type ErrorResponse struct {
	// Response is the HTTP response that caused this error. The body is
	// already consumed and closed.
	//
	// When the SDK refuses a call before sending it, as Verification.Init
	// does for a missing UserID or a MinAge outside 12 to 25, Response is a
	// synthetic 400 Bad Request with an empty body and no Request, so code
	// that reads Response.StatusCode works the same for both. Local reports
	// whether that was the case.
	Response *http.Response `json:"-"`

	// Code is the machine-readable error code (e.g., "UNAUTHORIZED",
	// "INVALID_REQUEST", "NOT_FOUND").
	Code string `json:"code"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// RequestID is the unique request identifier for this API call.
	// Include it when contacting support.
	RequestID string `json:"request_id"`

	// local is true when the SDK refused the call itself and sent nothing.
	local bool
}

// Local reports whether the SDK refused the call itself, before sending any
// request. The Code and Message are then the ones the API would have given.
func (e *ErrorResponse) Local() bool {
	return e.local
}

// Error implements the error interface.
func (e *ErrorResponse) Error() string {
	if e.local {
		return fmt.Sprintf("%d %s: %s (refused by the SDK, no request was sent)",
			http.StatusBadRequest, e.Code, e.Message)
	}
	if e.Response == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("%d %s: %s (request_id: %s)",
			e.Response.StatusCode, e.Code, e.Message, e.RequestID)
	}
	return fmt.Sprintf("%d %s: %s", e.Response.StatusCode, e.Code, e.Message)
}

// AuthenticationError is returned when the API key is invalid or missing
// (HTTP 401 or 403).
type AuthenticationError struct {
	ErrorResponse
}

// ValidationError is returned when the request parameters are invalid
// (HTTP 400 or other 4xx not covered by specific types).
type ValidationError struct {
	ErrorResponse
}

// NotFoundError is returned when the requested resource does not exist
// (HTTP 404).
type NotFoundError struct {
	ErrorResponse
}

// RateLimitError is returned when the API rate limit is exceeded (HTTP 429).
type RateLimitError struct {
	ErrorResponse

	// RetryAfter is the number of seconds to wait before retrying, from the
	// Retry-After response header. Zero if the header was not present.
	RetryAfter int
}

// ServerError is returned when the Xident server encounters an internal
// error (HTTP 5xx).
type ServerError struct {
	ErrorResponse
}

// newLocalValidationError builds the error for a request the SDK refuses
// before sending it. It is the same *ValidationError type an API 400 gives,
// so errors.As works the same way, and it carries a synthetic 400 response so
// that caller code reading Response.StatusCode does not dereference nil.
// RequestID stays empty because no request was made.
func newLocalValidationError(code, message string) error {
	return &ValidationError{ErrorResponse: ErrorResponse{
		Response: &http.Response{
			Status:     "400 Bad Request",
			StatusCode: http.StatusBadRequest,
			Proto:      "HTTP/1.1",
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		},
		Code:    code,
		Message: message,
		local:   true,
	}}
}

// newErrorResponse creates the appropriate typed error based on the HTTP
// status code. This mirrors the PHP SDK's throwException mapping.
func newErrorResponse(resp *http.Response, code, message, requestID string) error {
	base := ErrorResponse{
		Response:  resp,
		Code:      code,
		Message:   message,
		RequestID: requestID,
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &AuthenticationError{ErrorResponse: base}

	case resp.StatusCode == http.StatusNotFound:
		return &NotFoundError{ErrorResponse: base}

	case resp.StatusCode == http.StatusTooManyRequests:
		retryAfter := 0
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			fmt.Sscanf(ra, "%d", &retryAfter)
		}
		return &RateLimitError{ErrorResponse: base, RetryAfter: retryAfter}

	case resp.StatusCode >= 500:
		return &ServerError{ErrorResponse: base}

	default:
		return &ValidationError{ErrorResponse: base}
	}
}
