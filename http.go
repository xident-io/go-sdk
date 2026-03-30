package xident

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// apiEnvelope is the standard Xident API response wrapper.
//
//	{ "success": true, "data": {...}, "meta": {"request_id": "..."} }
//	{ "success": false, "error": {"code": "...", "message": "..."}, "meta": {...} }
type apiEnvelope struct {
	Success bool             `json:"success"`
	Data    json.RawMessage  `json:"data,omitempty"`
	Error   *apiEnvelopeErr  `json:"error,omitempty"`
	Meta    *apiEnvelopeMeta `json:"meta,omitempty"`
}

type apiEnvelopeErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiEnvelopeMeta struct {
	RequestID string `json:"request_id"`
}

// newRequest creates an *http.Request with the proper headers set.
//
// method is the HTTP method (GET, POST, etc.).
// path is the API path (e.g., "/init") -- it will be prefixed with the
// base URL and API version.
// body is an optional struct to marshal as JSON. Pass nil for GET requests.
func (c *Client) newRequest(method, path string, body any) (*http.Request, error) {
	url := strings.TrimRight(c.baseURL, "/") + "/" + apiVersion + "/" + strings.TrimLeft(path, "/")

	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("xident: failed to marshal request body: %w", err)
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, fmt.Errorf("xident: failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// do sends an HTTP request with retries and parses the response.
//
// ctx controls cancellation and deadlines. v is the target struct for
// unmarshalling the response data field (from the API envelope). If v is nil,
// the data field is ignored.
//
// Returns a *Response (wrapping the last http.Response) and any error.
// On API errors (non-2xx), the error is a typed error (AuthenticationError,
// ValidationError, etc.) that can be matched with errors.As.
func (c *Client) do(ctx context.Context, req *http.Request, v any) (*Response, error) {
	var (
		resp     *http.Response
		lastErr  error
		bodyData []byte
	)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Clone the request for each attempt so the body can be re-read.
		// We need to buffer the body on first read.
		var reqCopy *http.Request
		if bodyData != nil {
			reqCopy = req.Clone(ctx)
			reqCopy.Body = io.NopCloser(bytes.NewReader(bodyData))
		} else if req.Body != nil && attempt == 0 {
			// First attempt: read and buffer the body.
			data, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("xident: failed to read request body: %w", err)
			}
			req.Body.Close()
			bodyData = data
			reqCopy = req.Clone(ctx)
			reqCopy.Body = io.NopCloser(bytes.NewReader(bodyData))
		} else if req.Body == nil {
			reqCopy = req.Clone(ctx)
		} else {
			// Retry with buffered body.
			reqCopy = req.Clone(ctx)
			reqCopy.Body = io.NopCloser(bytes.NewReader(bodyData))
		}

		var err error
		resp, err = c.httpClient.Do(reqCopy)
		if err != nil {
			lastErr = fmt.Errorf("xident: request failed: %w", err)
			if attempt < c.maxRetries {
				continue
			}
			return nil, lastErr
		}

		// Only retry on 5xx server errors.
		if resp.StatusCode >= 500 && attempt < c.maxRetries {
			resp.Body.Close()
			lastErr = fmt.Errorf("xident: server error %d", resp.StatusCode)
			continue
		}

		break
	}

	if resp == nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("xident: no response received")
	}

	defer resp.Body.Close()

	response := newResponse(resp)

	// Read the entire body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return response, fmt.Errorf("xident: failed to read response body: %w", err)
	}

	// Parse the API envelope.
	var envelope apiEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		// If we can't parse the envelope and the status is an error,
		// return a generic error.
		if resp.StatusCode >= 400 {
			return response, newErrorResponse(resp, "PARSE_ERROR", "Failed to parse API response", "")
		}
		return response, fmt.Errorf("xident: failed to parse response: %w", err)
	}

	// Extract request ID.
	if envelope.Meta != nil {
		response.RequestID = envelope.Meta.RequestID
	}

	// Handle API errors.
	if !envelope.Success || resp.StatusCode >= 400 {
		code := ""
		message := "Unknown error"
		requestID := ""
		if envelope.Error != nil {
			code = envelope.Error.Code
			message = envelope.Error.Message
		}
		if envelope.Meta != nil {
			requestID = envelope.Meta.RequestID
		}
		return response, newErrorResponse(resp, code, message, requestID)
	}

	// Unmarshal data into the target struct.
	if v != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, v); err != nil {
			return response, fmt.Errorf("xident: failed to unmarshal response data: %w", err)
		}
	}

	return response, nil
}

// retryDelay returns the delay before retry attempt n (1-indexed).
// Uses exponential backoff with jitter: base * 2^(n-1) + random jitter.
func retryDelay(attempt int) time.Duration {
	base := time.Second
	backoff := base * time.Duration(math.Pow(2, float64(attempt-1)))

	// Add up to 25% jitter to prevent thundering herd.
	// Uses crypto/rand for the jitter value.
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := int64(binary.LittleEndian.Uint64(b[:]))
	if n < 0 {
		n = -n
	}
	jitter := time.Duration(n % (int64(backoff) / 4))

	return backoff + jitter
}
